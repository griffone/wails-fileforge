import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import * as pdfjsLib from 'pdfjs-dist';

import {
  PdfCrop,
  computePreviewOverlayRect,
  setPreviewWorkerInitializerForTests,
} from './pdf-crop';
import {
  JobRequestV1,
  JobStatusResponseV1,
  RunJobResponseV1,
  ValidateJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfCrop', () => {
  let component: PdfCrop;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    setPreviewWorkerInitializerForTests(null);
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
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    await TestBed.configureTestingModule({
      imports: [PdfCrop],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfCrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    setPreviewWorkerInitializerForTests(null);
  });

  it('shapes payload with preset and optional pageSelection', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/in.pdf';
    component.form.patchValue({
      outputPath: '/tmp/out/cropped.pdf',
      cropPreset: 'medium',
      pageSelection: '1-3,5',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.pdf.crop');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/in.pdf']);
    expect(req.options['outputPath']).toBe('/tmp/out/cropped.pdf');
    expect(req.options['cropPreset']).toBe('medium');
    expect(req.options['pageSelection']).toBe('1-3,5');
    expect(req.options['margins']).toBeUndefined();
  });

  it('shapes payload with custom margins', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/in.pdf';
    component.form.patchValue({
      outputPath: '/tmp/out/cropped.pdf',
      cropPreset: 'custom',
      marginTop: '12',
      marginRight: '8',
      marginBottom: '6',
      marginLeft: '4',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.options['cropPreset']).toBe('custom');
    expect(req.options['margins']).toEqual({
      top: 12,
      right: 8,
      bottom: 6,
      left: 4,
    });
  });

  it('shapes payload for tool.pdf.crop in batch mode', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPaths = ['/tmp/in-a.pdf', '/tmp/in-b.pdf'];
    component.form.patchValue({
      jobMode: 'batch',
      outputDir: '/tmp/out',
      cropPreset: 'small',
      pageSelection: '1-3',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.pdf.crop');
    expect(req.mode).toBe('batch');
    expect(req.inputPaths).toEqual(['/tmp/in-a.pdf', '/tmp/in-b.pdf']);
    expect(req.outputDir).toBe('/tmp/out');
    expect(req.options['outputDir']).toBe('/tmp/out');
    expect(req.options['cropPreset']).toBe('small');
    expect(req.options['pageSelection']).toBe('1-3');
    expect(req.options['outputPath']).toBeUndefined();
  });

  it('blocks locally when custom preset has missing margins', async () => {
    component.selectedInputPath = '/tmp/in.pdf';
    component.form.patchValue({
      outputPath: '/tmp/out/cropped.pdf',
      cropPreset: 'custom',
      marginTop: '10',
      marginRight: '',
      marginBottom: '10',
      marginLeft: '10',
    });

    await component.validate();

    expect(component.validationMessage).toContain('requires all margins');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('maps crop error codes to actionable message', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'invalid',
      valid: false,
      error: {
        code: 'PDF_CROP_PAGE_SELECTION_INVALID',
        message: 'ranges contains empty token',
      },
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/in.pdf';
    component.form.patchValue({ outputPath: '/tmp/out/cropped.pdf', cropPreset: 'small' });

    await component.validate();

    expect(component.validationMessage).toContain('Page selection format is invalid');
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
      jobId: 'crop-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'crop-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.pdf.crop',
        status: 'running',
        progress: { current: 0, total: 1, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };
    const completed: JobStatusResponseV1 = {
      success: true,
      message: 'completed',
      found: true,
      result: {
        jobId: 'crop-job-1',
        success: true,
        message: 'done',
        toolId: 'tool.pdf.crop',
        status: 'completed',
        progress: { current: 1, total: 1, stage: 'completed', message: 'done' },
        items: [
          {
            inputPath: '/tmp/in.pdf',
            outputPath: '/tmp/out/cropped.pdf',
            outputs: ['/tmp/out/cropped.pdf'],
            outputCount: 1,
            success: true,
            message: 'PDF crop successful',
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValues(
      Promise.resolve(running),
      Promise.resolve(completed)
    );

    component.selectedInputPath = '/tmp/in.pdf';
    component.form.patchValue({ outputPath: '/tmp/out/cropped.pdf', cropPreset: 'small' });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('crop-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('completed');
    expect(component.jobResult?.items.length).toBe(1);
    expect(component.jobResult?.items[0].outputPath).toBe('/tmp/out/cropped.pdf');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('runs batch and transitions state through polling with itemized results', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'crop-batch-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'crop-batch-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.pdf.crop',
        status: 'running',
        progress: { current: 1, total: 2, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };
    const completed: JobStatusResponseV1 = {
      success: true,
      message: 'completed',
      found: true,
      result: {
        jobId: 'crop-batch-job-1',
        success: true,
        message: 'done',
        toolId: 'tool.pdf.crop',
        status: 'completed',
        progress: { current: 2, total: 2, stage: 'completed', message: 'done' },
        items: [
          {
            inputPath: '/tmp/in-a.pdf',
            outputPath: '/tmp/out/in-a_cropped.pdf',
            outputs: ['/tmp/out/in-a_cropped.pdf'],
            outputCount: 1,
            success: true,
            message: 'PDF crop successful',
          },
          {
            inputPath: '/tmp/in-b.pdf',
            outputPath: '/tmp/out/in-b_cropped.pdf',
            outputs: ['/tmp/out/in-b_cropped.pdf'],
            outputCount: 1,
            success: true,
            message: 'PDF crop successful',
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValues(
      Promise.resolve(running),
      Promise.resolve(completed)
    );

    component.selectedInputPaths = ['/tmp/in-a.pdf', '/tmp/in-b.pdf'];
    component.form.patchValue({ jobMode: 'batch', outputDir: '/tmp/out', cropPreset: 'small' });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('crop-batch-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('completed');
    expect(component.jobResult?.items.length).toBe(2);
    expect(component.jobResult?.items[0].outputPath).toBe('/tmp/out/in-a_cropped.pdf');
    expect(component.jobResult?.items[1].outputPath).toBe('/tmp/out/in-b_cropped.pdf');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('maps crop batch collision error code to actionable message', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'invalid',
      valid: false,
      error: {
        code: 'PDF_CROP_BATCH_OUTPUT_COLLISION',
        message: 'batch planned outputs collide: /tmp/out/a_cropped.pdf and /tmp/out/A_cropped.pdf',
      },
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPaths = ['/tmp/a.pdf', '/tmp/A.pdf'];
    component.form.patchValue({ jobMode: 'batch', outputDir: '/tmp/out', cropPreset: 'small' });

    await component.validate();

    expect(component.validationMessage).toContain('Batch output collision detected');
  });

  it('computes preview overlay rectangle from margins and scale', () => {
    const overlay = computePreviewOverlayRect(500, 700, 2, {
      top: 10,
      right: 15,
      bottom: 20,
      left: 5,
    });

    expect(overlay).toEqual({
      left: 10,
      top: 20,
      width: 460,
      height: 640,
    });
  });

  it('updates overlay style when preset/margins change', async () => {
    (component as any).previewMeta = {
      pointsToPxScale: 1,
      canvasWidthPx: 200,
      canvasHeightPx: 300,
    };

    component.form.patchValue({ cropPreset: 'small' });
    await fixture.whenStable();

    expect(component.previewOverlayStyle['left']).toBe('10px');
    expect(component.previewOverlayStyle['top']).toBe('10px');
    expect(component.previewOverlayStyle['width']).toBe('180px');
    expect(component.previewOverlayStyle['height']).toBe('280px');

    component.form.patchValue({
      cropPreset: 'custom',
      marginTop: '12',
      marginRight: '8',
      marginBottom: '6',
      marginLeft: '4',
    });
    await fixture.whenStable();

    expect(component.previewOverlayStyle['left']).toBe('4px');
    expect(component.previewOverlayStyle['top']).toBe('12px');
    expect(component.previewOverlayStyle['width']).toBe('188px');
    expect(component.previewOverlayStyle['height']).toBe('282px');
  });

  it('handles preview states empty/loading/error', async () => {
    component.clearSelectedInput();
    expect(component.previewStatus).toBe('empty');

    component.selectedInputPath = '/tmp/sample.pdf';
    wailsSpy.getPdfPreviewSource.and.returnValue(
      Promise.resolve({ success: false, message: 'read failed' })
    );

    const promise = component.refreshPreview();
    expect(component.previewStatus).toBe('loading');
    await promise;

    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Could not read the PDF preview source');
  });

  it('configures pdf.js worker source before preview rendering', async () => {
    component.selectedInputPath = '/tmp/sample.pdf';
    wailsSpy.getPdfPreviewSource.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', dataBase64: 'AA==' })
    );

    await component.refreshPreview();

    expect(pdfjsLib.GlobalWorkerOptions.workerSrc).toBe('assets/pdfjs/pdf.worker.min.mjs');
    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Preview failed');
    expect(component.previewMessage).toContain('PDF renderer failed to generate preview');
  });

  it('maps worker init failure into recoverable preview error', async () => {
    setPreviewWorkerInitializerForTests(() => ({
      ok: false,
      error: {
        code: 'PDF_PREVIEW_WORKER_MISCONFIG',
        message: 'worker unavailable',
        stage: 'worker-init',
        recoverable: true,
      } as any,
    }));

    component.selectedInputPath = '/tmp/sample.pdf';
    wailsSpy.getPdfPreviewSource.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', dataBase64: 'AA==' })
    );

    await component.refreshPreview();

    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Preview worker failed to initialize');
  });

  it('keeps latest preview result on rapid input changes (stale guard)', async () => {
    component.selectedInputPath = '/tmp/old.pdf';

    let resolveOld: ((value: any) => void) | undefined;
    const oldPromise = new Promise((resolve) => {
      resolveOld = resolve;
    });

    wailsSpy.getPdfPreviewSource.and.returnValues(
      oldPromise as Promise<any>,
      Promise.resolve({ success: false, message: 'latest failed', error: { code: 'PDF_PREVIEW_READ_FAILED', message: 'latest failed' } })
    );

    const first = component.refreshPreview();
    component.selectedInputPath = '/tmp/new.pdf';
    const second = component.refreshPreview();

    resolveOld?.({ success: true, message: 'ok', dataBase64: 'AA==' });

    await Promise.all([first, second]);

    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Could not read the PDF preview source');
  });

  it('supports preview retry after recoverable backend error', async () => {
    component.selectedInputPath = '/tmp/sample.pdf';

    wailsSpy.getPdfPreviewSource.and.returnValues(
      Promise.resolve({
        success: false,
        message: 'first fail',
        error: { code: 'PDF_PREVIEW_READ_FAILED', message: 'first fail' },
      }),
      Promise.resolve({
        success: false,
        message: 'second fail',
        error: { code: 'PDF_PREVIEW_NOT_PDF', message: 'second fail' },
      })
    );

    await component.refreshPreview();
    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Could not read the PDF preview source');

    await component.refreshPreview();
    expect(component.previewStatus).toBe('error');
    expect(component.previewMessage).toContain('Only .pdf files are supported for preview');
  });
});
