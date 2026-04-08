import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { ImageCrop } from './image-crop';
import { JobRequestV1, JobStatusResponseV1, Wails } from '../../services/wails';

describe('ImageCrop', () => {
  let component: ImageCrop;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'listToolsV1',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'getImagePreviewSourceV1',
      'getImageCropPreviewV1',
      'isRuntimeAvailable',
    ]);

    wailsSpy.getImagePreviewSourceV1.and.returnValue(
      Promise.resolve({
        success: true,
        message: 'ok',
        dataBase64: 'AA==',
        mimeType: 'image/png',
        width: 100,
        height: 80,
      })
    );
    wailsSpy.getImageCropPreviewV1.and.returnValue(
      Promise.resolve({
        success: true,
        message: 'ok',
        dataBase64: 'AA==',
        mimeType: 'image/png',
        width: 20,
        height: 20,
      })
    );

    await TestBed.configureTestingModule({
      imports: [ImageCrop],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(ImageCrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('builds single crop request payload with manual coordinates', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.selectedInputPath = '/tmp/in.png';
    component.form.patchValue({
      jobMode: 'single',
      outputPath: '/tmp/out.png',
      ratioPreset: '1:1',
      format: 'png',
      x: '10',
      y: '12',
      width: '20',
      height: '20',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.image.crop');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/in.png']);
    expect(req.options['x']).toBe(10);
    expect(req.options['y']).toBe(12);
    expect(req.options['width']).toBe(20);
    expect(req.options['height']).toBe(20);
    expect(req.options['ratioPreset']).toBe('1:1');
    expect(req.options['outputPath']).toBe('/tmp/out.png');
    expect(req.options['format']).toBe('png');
  });

  it('builds batch crop request payload with same area for all inputs', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.selectedInputPaths = ['/tmp/a.jpg', '/tmp/b.jpg'];
    component.form.patchValue({
      jobMode: 'batch',
      outputDir: '/tmp/out',
      ratioPreset: '4:3',
      x: '0',
      y: '0',
      width: '40',
      height: '30',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.image.crop');
    expect(req.mode).toBe('batch');
    expect(req.inputPaths).toEqual(['/tmp/a.jpg', '/tmp/b.jpg']);
    expect(req.outputDir).toBe('/tmp/out');
    expect(req.options['x']).toBe(0);
    expect(req.options['y']).toBe(0);
    expect(req.options['width']).toBe(40);
    expect(req.options['height']).toBe(30);
    expect(req.options['ratioPreset']).toBe('4:3');
  });

  it('validates local ratio mismatch when preset is locked', async () => {
    component.selectedInputPath = '/tmp/in.png';
    component.form.patchValue({
      ratioPreset: '16:9',
      x: '0',
      y: '0',
      width: '10',
      height: '10',
    });

    await component.validate();

    expect(component.validationMessage).toContain('Ratio preset 16:9 requires locked width/height proportion');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('runs and ends in partial_success for mixed batch results', fakeAsync(() => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );
    wailsSpy.runJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'submitted', jobId: 'crop-job-1', status: 'queued' })
    );

    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'crop-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.image.crop',
        status: 'running',
        progress: { current: 1, total: 2, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };

    const done: JobStatusResponseV1 = {
      success: true,
      message: 'partial',
      found: true,
      result: {
        jobId: 'crop-job-1',
        success: false,
        message: 'job partial success',
        toolId: 'tool.image.crop',
        status: 'partial_success',
        progress: { current: 2, total: 2, stage: 'running', message: 'done' },
        items: [
          {
            inputPath: '/tmp/a.jpg',
            outputPath: '/tmp/a_cropped.jpg',
            success: true,
            message: 'image crop successful',
          },
          {
            inputPath: '/tmp/b.jpg',
            outputPath: '/tmp/b_cropped.jpg',
            success: false,
            message: 'out-of-bounds',
            error: {
              code: 'VALIDATION_INVALID_INPUT',
              detail_code: 'IMAGE_CROP_OUT_OF_BOUNDS',
              message: 'crop area is outside image bounds',
            },
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.getJobStatusV1.and.returnValues(Promise.resolve(running), Promise.resolve(done));

    component.selectedInputPaths = ['/tmp/a.jpg', '/tmp/b.jpg'];
    component.form.patchValue({
      jobMode: 'batch',
      ratioPreset: '1:1',
      x: '0',
      y: '0',
      width: '20',
      height: '20',
    });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('crop-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('partial_success');
    expect(component.jobResult?.items.length).toBe(2);
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('does not call form handlers after destroy (unsubscribed)', () => {
    // spy on the handler that's called from valueChanges subscriptions
    spyOn(component as any, 'onManualCoordinatesChanged');

    // destroy the fixture (triggers ngOnDestroy and should complete destroy$)
    fixture.destroy();

    // trigger value change after destroy
    component.form.patchValue({ x: '1' });

    // handler should not have been called after destroy
    expect((component as any).onManualCoordinatesChanged).not.toHaveBeenCalled();
  });
});
