import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoTrim } from './video-trim';
import {
  JobRequestV1,
  JobStatusResponseV1,
  RunJobResponseV1,
  ValidateJobResponseV1,
  Wails,
} from '../../services/wails';

function waitMicrotask(): Promise<void> {
  return Promise.resolve();
}

describe('VideoTrim', () => {
  let component: VideoTrim;
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
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    await TestBed.configureTestingModule({
      imports: [VideoTrim],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoTrim);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes payload for tool.video.trim', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/input.mov';
    component.form.patchValue({
      outputPath: '/tmp/out/video.webm',
      startTime: '1.25',
      endTime: '5.75',
      targetFormat: 'webm',
      qualityPreset: 'high',
      trimMode: 'copy',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.trim');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/input.mov']);
    expect(req.options['outputPath']).toBe('/tmp/out/video.webm');
    expect(req.options['startTime']).toBe(1.25);
    expect(req.options['endTime']).toBe(5.75);
    expect(req.options['targetFormat']).toBe('webm');
    expect(req.options['qualityPreset']).toBe('high');
    expect(req.options['trimMode']).toBe('copy');
  });

  it('blocks locally when time range is invalid', async () => {
    component.selectedInputPath = '/tmp/input.mp4';
    component.form.patchValue({
      outputPath: '/tmp/out/video.mp4',
      startTime: '10',
      endTime: '9',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
    });

    await component.validate();

    expect(component.validationMessage).toContain('End time must be greater than start time');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('maps trim error codes to actionable messages', () => {
    const baseFallback = 'fallback';
    const cases: Array<{ code: string; contains: string }> = [
      { code: 'VIDEO_TRIM_INVALID_TIME_RANGE', contains: 'Invalid trim time range' },
      { code: 'VIDEO_TRIM_OUTPUT_EXISTS', contains: 'already exists' },
      { code: 'VIDEO_TRIM_OUTPUT_COLLIDES_INPUT', contains: 'collides with input path' },
      { code: 'VIDEO_TRIM_OUTPUT_DIR_NOT_FOUND', contains: 'does not exist' },
      { code: 'VIDEO_TRIM_OUTPUT_DIR_NOT_DIRECTORY', contains: 'not a directory' },
      { code: 'VIDEO_TRIM_OUTPUT_DIR_NOT_WRITABLE', contains: 'not writable' },
      { code: 'VIDEO_TRIM_INPUT_OPEN_FAILED', contains: 'could not read input file' },
      { code: 'VIDEO_TRIM_OUTPUT_WRITE_FAILED', contains: 'could not write output file' },
      { code: 'VIDEO_TRIM_CODEC_UNAVAILABLE', contains: 'codec is unavailable' },
      { code: 'VIDEO_TRIM_MODE_INVALID', contains: 'Trim mode is invalid' },
      { code: 'VIDEO_TRIM_COPY_FAILED', contains: 'copy mode failed' },
      {
        code: 'VIDEO_TRIM_AUTO_FALLBACK_REENCODE_FAILED',
        contains: 'failed after fallback reencode',
      },
    ];

    for (const testCase of cases) {
      const message = (component as any).mapJobError(
        { code: testCase.code, message: 'detail' },
        baseFallback
      ) as string;
      expect(message).withContext(testCase.code).toContain(testCase.contains);
    }
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
      jobId: 'video-trim-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'video-trim-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.video.trim',
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
        jobId: 'video-trim-job-1',
        success: true,
        message: 'done',
        toolId: 'tool.video.trim',
        status: 'completed',
        progress: { current: 1, total: 1, stage: 'completed', message: 'done' },
        items: [
          {
            inputPath: '/tmp/input.mp4',
            outputPath: '/tmp/out/video_trimmed.mp4',
            outputs: ['/tmp/out/video_trimmed.mp4'],
            outputCount: 1,
            success: true,
            message: 'Video trim successful',
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

    component.selectedInputPath = '/tmp/input.mp4';
    component.form.patchValue({
      outputPath: '/tmp/out/video_trimmed.mp4',
      startTime: '0',
      endTime: '3',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
      trimMode: 'auto',
    });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('video-trim-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('completed');
    expect(component.jobResult?.items.length).toBe(1);
    expect(component.jobResult?.items[0].outputPath).toBe('/tmp/out/video_trimmed.mp4');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('shows fallback info when backend reports fallback path', async () => {
    const status: JobStatusResponseV1 = {
      success: true,
      message: 'completed with fallback',
      found: true,
      result: {
        jobId: 'video-trim-job-fallback',
        success: true,
        message: 'Video trim successful (fallback re-encode used)',
        toolId: 'tool.video.trim',
        status: 'completed',
        progress: {
          current: 100,
          total: 100,
          stage: 'completed',
          message: 'Trim completed after auto fallback to re-encode',
        },
        items: [],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };
    wailsSpy.getJobStatusV1.and.returnValue(Promise.resolve(status));

    component['activeJobId'] = 'video-trim-job-fallback';
    await (component as any).pollJobStatus();

    expect(component.fallbackInfoMessage).toContain('Auto fallback used');
    expect(component.progressPercentLabel).toBe('100%');
  });

  it('clears active job id when poll result is not found', async () => {
    wailsSpy.getJobStatusV1.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'not found',
        found: false,
        error: { code: 'NOT_FOUND', message: 'missing' },
      })
    );

    component['activeJobId'] = 'missing-job';
    await (component as any).pollJobStatus();

    expect(component.activeJobId).toBe('');
    expect(component.isPolling).toBeFalse();
    expect(component.statusMessage).toContain('Job not found');
  });

  it('cancel transition reports backend failure explicitly', async () => {
    component.activeJobId = 'video-trim-job-cancel';
    wailsSpy.cancelJobV1.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'cancel failed',
        jobId: 'video-trim-job-cancel',
        error: { code: 'VALIDATION_ERROR', message: 'cannot cancel' },
      })
    );

    await component.cancel();
    await waitMicrotask();

    expect(component.statusMessage).toContain('Validation: cannot cancel');
  });
});
