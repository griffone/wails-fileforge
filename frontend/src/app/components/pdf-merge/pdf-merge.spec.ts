import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { PdfMerge } from './pdf-merge';
import {
  JobRequestV1,
  JobStatusResponseV1,
  RunJobResponseV1,
  ValidateJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfMerge', () => {
  let component: PdfMerge;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'listToolsV1',
      'convertFile',
      'convertBatch',
      'getSupportedFormats',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
    ]);

    await TestBed.configureTestingModule({
      imports: [PdfMerge],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes payload preserving explicit reorder', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPaths = ['/tmp/3.pdf', '/tmp/1.pdf', '/tmp/2.pdf'];
    component.moveDown(0);
    component.form.patchValue({ outputPath: '/tmp/out/merged.pdf' });

    await component.validate();

    const request = wailsSpy.validateJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/1.pdf', '/tmp/3.pdf', '/tmp/2.pdf']);
    expect(request.outputDir).toBe('/tmp/out');
    expect(request.options['outputPath']).toBe('/tmp/out/merged.pdf');
  });

  it('starts with empty initial state (no demo defaults)', () => {
    expect(component.selectedInputPaths).toEqual([]);
    expect(component.form.controls.outputPath.value).toBe('');
  });

  it('applies local validation for minimum files before backend calls', async () => {
    component.selectedInputPaths = ['/tmp/only-one.pdf'];
    component.form.patchValue({ outputPath: '/tmp/out/merged.pdf' });

    await component.validate();

    expect(component.validationMessage).toContain('al menos 2 PDFs');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('blocks duplicates and invalid extension locally', async () => {
    component.selectedInputPaths = ['/tmp/a.pdf', '/tmp/a.pdf', '/tmp/b.txt'];
    component.form.patchValue({ outputPath: '/tmp/out/merged.pdf' });

    await component.validate();

    expect(component.validationMessage).toContain('inválido');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();

    component.selectedInputPaths = ['/tmp/a.pdf', '/tmp/a.pdf'];
    await component.validate();
    expect(component.validationMessage).toContain('duplicado');
  });

  it('blocks output path collision with input path locally', async () => {
    component.selectedInputPaths = ['/tmp/a.pdf', '/tmp/b.pdf'];
    component.form.patchValue({ outputPath: '/tmp/A.PDF' });

    await component.validate();

    expect(component.validationMessage).toContain('colisiona con un input');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('detects collisions and duplicates for windows-like and UNC paths', async () => {
    component.selectedInputPaths = ['C:\\Docs\\A.pdf', 'c:/docs/a.PDF'];
    component.form.patchValue({ outputPath: 'C:/Docs/out.pdf' });

    await component.validate();

    expect(component.validationMessage).toContain('duplicado');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();

    component.selectedInputPaths = ['\\\\Server\\Share\\A.pdf', '\\\\Server\\Share\\B.pdf'];
    component.form.patchValue({ outputPath: '//server/share/a.PDF' });

    await component.validate();

    expect(component.validationMessage).toContain('colisiona con un input');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('blocks outputPath that points to a directory without filename', async () => {
    component.selectedInputPaths = ['/tmp/a.pdf', '/tmp/b.pdf'];
    component.form.patchValue({ outputPath: '/tmp/out/' });

    await component.validate();

    expect(component.validationMessage).toContain('nombre de archivo');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('assists outputPath using selected output folder', async () => {
    wailsSpy.openDirectoryDialog.and.returnValue(Promise.resolve('C:\\OutDir'));
    component.form.patchValue({ outputPath: 'existing.pdf' });

    await component.selectOutputDirectory();

    expect(component.form.controls.outputPath.value).toBe('C:\\OutDir\\existing.pdf');
    expect(component.validationMessage).toContain('Carpeta de salida seleccionada');
  });

  it('appends valid files and ignores duplicates/extensions', () => {
    component.selectedInputPaths = ['/tmp/a.pdf'];
    component.appendInputPaths(['/tmp/a.pdf', '/tmp/b.pdf', '/tmp/c.txt']);

    expect(component.selectedInputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(component.submitMessage).toContain('ignoraron');
  });

  it('supports drag-drop reorder via index move', () => {
    component.selectedInputPaths = ['/tmp/1.pdf', '/tmp/2.pdf', '/tmp/3.pdf'];

    component.onReorderDragStart(0);
    component.onReorderDrop(2);

    expect(component.selectedInputPaths).toEqual([
      '/tmp/2.pdf',
      '/tmp/3.pdf',
      '/tmp/1.pdf',
    ]);
  });

  it('runs and transitions basic state through polling', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'pdf-job-123',
      status: 'queued',
    };
    const runningStatus: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'pdf-job-123',
        success: false,
        message: 'working',
        toolId: 'tool.pdf.merge',
        status: 'running',
        progress: { current: 1, total: 2, stage: 'run', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };
    const completedStatus: JobStatusResponseV1 = {
      success: true,
      message: 'completed',
      found: true,
      result: {
        jobId: 'pdf-job-123',
        success: true,
        message: 'done',
        toolId: 'tool.pdf.merge',
        status: 'completed',
        progress: { current: 2, total: 2, stage: 'done', message: 'completed' },
        items: [
          {
            inputPath: '/tmp/1.pdf,/tmp/2.pdf',
            outputPath: '/tmp/out/merged.pdf',
            success: true,
            message: 'PDF merge successful',
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValues(
      Promise.resolve(runningStatus),
      Promise.resolve(completedStatus)
    );

    component.form.patchValue({
      outputPath: '/tmp/out/merged.pdf',
    });
    component.selectedInputPaths = ['/tmp/1.pdf', '/tmp/2.pdf'];

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('pdf-job-123');
    expect(component.isPolling).toBeTrue();
    expect(component.statusMessage).toContain('running');

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('completed');
    expect(component.statusMessage).toContain('completed');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('maps job error codes to actionable status message', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'pdf-job-err',
      status: 'queued',
    };
    const failedStatus: JobStatusResponseV1 = {
      success: true,
      message: 'failed',
      found: true,
      result: {
        jobId: 'pdf-job-err',
        success: false,
        message: 'failed',
        toolId: 'tool.pdf.merge',
        status: 'failed',
        progress: { current: 1, total: 1, stage: 'run', message: 'failed' },
        items: [],
        error: { code: 'PDF_MERGE_FAILED', message: 'io error' },
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValue(Promise.resolve(failedStatus));

    component.selectedInputPaths = ['/tmp/1.pdf', '/tmp/2.pdf'];
    component.form.patchValue({ outputPath: '/tmp/out/merged.pdf' });

    void component.run();
    flushMicrotasks();

    expect(component.statusMessage).toContain('No se pudo completar el merge');
    expect(component.isPolling).toBeFalse();
  }));

  it('maps new backend hardening error codes to actionable text', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'pdf-job-dir',
      status: 'queued',
    };
    const failedStatus: JobStatusResponseV1 = {
      success: true,
      message: 'failed',
      found: true,
      result: {
        jobId: 'pdf-job-dir',
        success: false,
        message: 'failed',
        toolId: 'tool.pdf.merge',
        status: 'failed',
        progress: { current: 1, total: 1, stage: 'run', message: 'failed' },
        items: [],
        error: {
          code: 'PDF_OUTPUT_DIR_NOT_FOUND',
          message: 'output directory does not exist: /missing',
        },
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValue(Promise.resolve(failedStatus));

    component.selectedInputPaths = ['/tmp/1.pdf', '/tmp/2.pdf'];
    component.form.patchValue({ outputPath: '/missing/out.pdf' });

    void component.run();
    flushMicrotasks();

    expect(component.statusMessage).toContain('La carpeta de salida no existe');
    expect(component.isPolling).toBeFalse();
  }));
});
