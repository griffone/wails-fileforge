import { fakeAsync, flushMicrotasks, TestBed, tick } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { DocDocxToPdf } from './doc-docx-to-pdf';
import {
  JobRequestV1,
  JobStatusResponseV1,
  RunJobResponseV1,
  ValidateJobResponseV1,
  Wails,
} from '../../services/wails';

describe('DocDocxToPdf', () => {
  let component: DocDocxToPdf;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
    ]);

    await TestBed.configureTestingModule({
      imports: [DocDocxToPdf],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(DocDocxToPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('builds single payload for docx tool', async () => {
    const validation: ValidateJobResponseV1 = { success: true, message: 'ok', valid: true };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.form.patchValue({
      mode: 'single',
      inputPath: '/tmp/sample.docx',
      outputDir: '/tmp/out',
      outputPath: '/tmp/out/sample.pdf',
    });

    await component.validate();

    const request = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.docx_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/sample.docx']);
    expect(request.options['outputPath']).toBe('/tmp/out/sample.pdf');
  });

  it('validates batch mode and rejects non-docx files locally', async () => {
    component.form.patchValue({ mode: 'batch', outputDir: '/tmp/out' });
    component.selectedInputPaths = ['/tmp/a.docx', '/tmp/b.txt'];

    await component.validate();

    expect(component.validationMessage).toContain('only .docx');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('requires explicit warning confirmation before run', async () => {
    const confirmSpy = spyOn(window, 'confirm').and.returnValue(false);

    component.form.patchValue({ mode: 'single', inputPath: '/tmp/input.docx' });
    await component.run();

    expect(confirmSpy).toHaveBeenCalledWith(
      'Esta conversión usa fidelidad estándar. En documentos complejos puede haber diferencias de diseño (tablas, fuentes, espaciado). ¿Querés continuar?'
    );
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('runs and polls until terminal status', fakeAsync(() => {
    const confirmSpy = spyOn(window, 'confirm').and.returnValue(true);

    const validation: ValidateJobResponseV1 = { success: true, message: 'ok', valid: true };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'docx-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'docx-job-1',
        success: false,
        message: 'running',
        toolId: 'tool.doc.docx_to_pdf',
        status: 'running',
        progress: { current: 0, total: 1, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };
    const done: JobStatusResponseV1 = {
      success: true,
      message: 'done',
      found: true,
      result: {
        jobId: 'docx-job-1',
        success: true,
        message: 'ok',
        toolId: 'tool.doc.docx_to_pdf',
        status: 'success',
        progress: { current: 1, total: 1, stage: 'success', message: 'done' },
        items: [],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValues(Promise.resolve(running), Promise.resolve(done));

    component.form.patchValue({ mode: 'single', inputPath: '/tmp/input.docx' });

    void component.run();
    flushMicrotasks();

    expect(confirmSpy).toHaveBeenCalled();
    expect(component.activeJobId).toBe('docx-job-1');

    tick(1000);
    flushMicrotasks();

    expect(component.activeJobId).toBe('');
    expect(component.isPolling).toBeFalse();
    expect(component.jobResult?.status).toBe('success');
  }));
});
