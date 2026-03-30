import { fakeAsync, flushMicrotasks, TestBed, tick } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { DocMdToPdf } from './doc-md-to-pdf';
import { JobRequestV1, JobStatusResponseV1, RunJobResponseV1, ValidateJobResponseV1, Wails } from '../../services/wails';

describe('DocMdToPdf', () => {
  let component: DocMdToPdf;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openFileDialog',
      'openDirectoryDialog',
    ]);

    await TestBed.configureTestingModule({
      imports: [DocMdToPdf],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(DocMdToPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('builds payload with header/footer configuration', async () => {
    const validation: ValidateJobResponseV1 = { success: true, message: 'ok', valid: true };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.form.patchValue({
      inputPath: '/tmp/doc.md',
      outputDir: '/tmp/out',
      outputPath: '/tmp/out/doc.pdf',
    });
    component.form.controls.header.patchValue({
      enabled: true,
      text: 'H {page}/{totalPages}',
      align: 'center',
      font: 'times',
      marginTop: 4,
      marginBottom: 2,
      color: '#112233',
    });

    await component.validate();

    const request = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.md_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/doc.md']);
    expect(request.options['header']).toEqual({
      enabled: true,
      text: 'H {page}/{totalPages}',
      align: 'center',
      font: 'times',
      marginTop: 4,
      marginBottom: 2,
      color: '#112233',
    });
  });

  it('applies local validation for placeholders/color and extension', async () => {
    component.form.patchValue({ inputPath: '/tmp/doc.txt' });

    await component.validate();
    expect(component.validationMessage).toContain('.md or .markdown');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();

    component.form.patchValue({ inputPath: '/tmp/doc.md' });
    component.form.controls.header.patchValue({ text: 'bad {unknown}', color: '#000000' });
    await component.validate();
    expect(component.validationMessage).toContain('unsupported placeholder');
  });

  it('renders approximate preview placeholders', () => {
    component.form.patchValue({ inputPath: '/tmp/notes.md' });
    component.form.controls.header.patchValue({ text: 'File {fileName} P{page}/{totalPages}', enabled: true });

    const preview = component.previewHeaderText();
    expect(preview).toContain('notes.md');
    expect(preview).toContain('P1/?');
  });

  it('runs job and polls until terminal status', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = { success: true, message: 'ok', valid: true };
    const run: RunJobResponseV1 = { success: true, message: 'submitted', jobId: 'doc-job-1', status: 'queued' };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'doc-job-1',
        success: false,
        message: 'running',
        toolId: 'tool.doc.md_to_pdf',
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
        jobId: 'doc-job-1',
        success: true,
        message: 'ok',
        toolId: 'tool.doc.md_to_pdf',
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

    component.form.patchValue({ inputPath: '/tmp/doc.md' });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('doc-job-1');

    tick(1000);
    flushMicrotasks();

    expect(component.activeJobId).toBe('');
    expect(component.isPolling).toBeFalse();
    expect(component.jobResult?.status).toBe('success');
  }));
});
