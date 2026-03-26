import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoConvert } from './video-convert';
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

describe('VideoConvert', () => {
  let component: VideoConvert;
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
      imports: [VideoConvert],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoConvert);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes payload for tool.video.convert', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/input.mov';
    component.form.patchValue({
      outputPath: '/tmp/out/video.webm',
      targetFormat: 'webm',
      qualityPreset: 'high',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.convert');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/input.mov']);
    expect(req.options['outputPath']).toBe('/tmp/out/video.webm');
    expect(req.options['targetFormat']).toBe('webm');
    expect(req.options['qualityPreset']).toBe('high');
  });

  it('blocks locally when output extension mismatches target format', async () => {
    component.selectedInputPath = '/tmp/input.mp4';
    component.form.patchValue({
      outputPath: '/tmp/out/video.webm',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
    });

    await component.validate();

    expect(component.validationMessage).toContain('Output extension must match target format');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('maps runtime unavailable error to actionable message', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'invalid',
      valid: false,
      error: {
        code: 'VIDEO_RUNTIME_UNAVAILABLE',
        message: 'ffmpeg runtime unavailable',
      },
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPath = '/tmp/input.mkv';
    component.form.patchValue({
      outputPath: '/tmp/out/video.webm',
      targetFormat: 'webm',
      qualityPreset: 'low',
    });

    await component.validate();

    expect(component.validationMessage).toContain('FFmpeg runtime is unavailable');
  });

  it('maps new backend hardening error codes to actionable messages', async () => {
    const baseFallback = 'fallback';
    const cases: Array<{ code: string; contains: string }> = [
      { code: 'VIDEO_RUNTIME_FFMPEG_NOT_FOUND', contains: 'FFmpeg runtime is unavailable' },
      { code: 'VIDEO_RUNTIME_FFPROBE_NOT_FOUND', contains: 'FFmpeg runtime is unavailable' },
      { code: 'VIDEO_CONVERT_OUTPUT_COLLIDES_INPUT', contains: 'collides with input path' },
      { code: 'VIDEO_CONVERT_OUTPUT_DIR_NOT_FOUND', contains: 'does not exist' },
      { code: 'VIDEO_CONVERT_OUTPUT_DIR_NOT_DIRECTORY', contains: 'not a directory' },
      { code: 'VIDEO_CONVERT_OUTPUT_DIR_NOT_WRITABLE', contains: 'not writable' },
      { code: 'VIDEO_CONVERT_INPUT_OPEN_FAILED', contains: 'could not read input file' },
      { code: 'VIDEO_CONVERT_OUTPUT_WRITE_FAILED', contains: 'could not write output file' },
      { code: 'VIDEO_CONVERT_CODEC_UNAVAILABLE', contains: 'codec is unavailable' },
    ];

    for (const testCase of cases) {
      const message = (component as any).mapJobError(
        { code: testCase.code, message: 'detail' },
        baseFallback
      ) as string;
      expect(message).withContext(testCase.code).toContain(testCase.contains);
    }
  });

  it('keeps convert progress labels consistent with trim-style details', async () => {
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'video-job-progress',
        success: false,
        message: 'processing',
        toolId: 'tool.video.convert',
        status: 'running',
        progress: { current: 2, total: 5, stage: 'encoding', message: 'encoding frames' },
        items: [],
        startedAt: Date.now(),
      },
    };

    wailsSpy.getJobStatusV1.and.returnValue(Promise.resolve(running));
    component['activeJobId'] = 'video-job-progress';

    await (component as any).pollJobStatus();

    expect(component.progressStageLabel).toBe('encoding');
    expect(component.progressPercentLabel).toBe('40%');
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
      jobId: 'video-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'video-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.video.convert',
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
        jobId: 'video-job-1',
        success: true,
        message: 'done',
        toolId: 'tool.video.convert',
        status: 'completed',
        progress: { current: 1, total: 1, stage: 'completed', message: 'done' },
        items: [
          {
            inputPath: '/tmp/input.mp4',
            outputPath: '/tmp/out/video.mp4',
            outputs: ['/tmp/out/video.mp4'],
            outputCount: 1,
            success: true,
            message: 'Video conversion successful',
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
      outputPath: '/tmp/out/video.mp4',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
    });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('video-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('completed');
    expect(component.jobResult?.items.length).toBe(1);
    expect(component.jobResult?.items[0].outputPath).toBe('/tmp/out/video.mp4');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('keeps polling state unambiguous on terminal response races', fakeAsync(() => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    const run: RunJobResponseV1 = {
      success: true,
      message: 'submitted',
      jobId: 'video-job-race',
      status: 'queued',
    };

    let resolveFirst!: (value: JobStatusResponseV1) => void;
    const firstPoll = new Promise<JobStatusResponseV1>((resolve) => {
      resolveFirst = resolve;
    });

    const terminal: JobStatusResponseV1 = {
      success: true,
      message: 'done',
      found: true,
      result: {
        jobId: 'video-job-race',
        success: true,
        message: 'done',
        toolId: 'tool.video.convert',
        status: 'completed',
        progress: { current: 1, total: 1, stage: 'completed', message: 'done' },
        items: [],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));
    wailsSpy.runJobV1.and.returnValue(Promise.resolve(run));
    wailsSpy.getJobStatusV1.and.returnValues(firstPoll, Promise.resolve(terminal));

    component.selectedInputPath = '/tmp/input.mp4';
    component.form.patchValue({
      outputPath: '/tmp/out/video.mp4',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
    });

    void component.run();
    flushMicrotasks();

    tick(1000);
    flushMicrotasks();

    resolveFirst(terminal);
    flushMicrotasks();

    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
    expect(component.jobResult?.status).toBe('completed');
  }));

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
    component.activeJobId = 'video-job-cancel';
    wailsSpy.cancelJobV1.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'cancel failed',
        jobId: 'video-job-cancel',
        error: { code: 'VALIDATION_ERROR', message: 'cannot cancel' },
      })
    );

    await component.cancel();
    await waitMicrotask();

    expect(component.statusMessage).toContain('Validation: cannot cancel');
  });
});
