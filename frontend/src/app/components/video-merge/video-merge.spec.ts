import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoMerge } from './video-merge';
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

describe('VideoMerge', () => {
  let component: VideoMerge;
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
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    await TestBed.configureTestingModule({
      imports: [VideoMerge],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes payload for tool.video.merge with mergeMode and ordered inputs', async () => {
    const validation: ValidateJobResponseV1 = {
      success: true,
      message: 'ok',
      valid: true,
    };
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve(validation));

    component.selectedInputPaths = ['/tmp/a.mp4', '/tmp/b.mov', '/tmp/c.mkv'];
    component.moveInputDown(0);
    component.form.patchValue({
      outputPath: '/tmp/out/merged.webm',
      targetFormat: 'webm',
      qualityPreset: 'high',
      mergeMode: 'copy',
    });

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.merge');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/b.mov', '/tmp/a.mp4', '/tmp/c.mkv']);
    expect(req.options['outputPath']).toBe('/tmp/out/merged.webm');
    expect(req.options['targetFormat']).toBe('webm');
    expect(req.options['qualityPreset']).toBe('high');
    expect(req.options['mergeMode']).toBe('copy');
  });

  it('blocks locally when fewer than 2 inputs are selected', async () => {
    component.selectedInputPaths = ['/tmp/a.mp4'];
    component.form.patchValue({
      outputPath: '/tmp/out/merged.mp4',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
      mergeMode: 'auto',
    });

    await component.validate();

    expect(component.validationMessage).toContain('Select at least 2 input video files');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('maps merge error codes to actionable messages', () => {
    const baseFallback = 'fallback';
    const cases: Array<{ code: string; contains: string }> = [
      { code: 'VALIDATION_INVALID_INPUT', contains: 'At least 2 input videos are required' },
      { code: 'UNSUPPORTED_FORMAT', contains: 'Output format mismatch' },
      { code: 'EXEC_IO_TRANSIENT', contains: 'Video merge execution failed' },
      { code: 'EXEC_TIMEOUT_TRANSIENT', contains: 'execution timeout' },
      { code: 'CANCELLED_BY_USER', contains: 'Job was canceled' },
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
      jobId: 'video-merge-job-1',
      status: 'queued',
    };
    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'video-merge-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.video.merge',
        status: 'running',
        progress: { current: 0, total: 1, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };
    const completed: JobStatusResponseV1 = {
      success: true,
      message: 'success',
      found: true,
      result: {
        jobId: 'video-merge-job-1',
        success: true,
        message: 'done',
        toolId: 'tool.video.merge',
        status: 'success',
        progress: { current: 1, total: 1, stage: 'success', message: 'done' },
        items: [
          {
            inputPath: '/tmp/a.mp4',
            outputPath: '/tmp/out/merged.mp4',
            outputs: ['/tmp/out/merged.mp4'],
            outputCount: 1,
            success: true,
            message: 'Video merge successful',
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

    component.selectedInputPaths = ['/tmp/a.mp4', '/tmp/b.mp4'];
    component.form.patchValue({
      outputPath: '/tmp/out/merged.mp4',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
      mergeMode: 'auto',
    });

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('video-merge-job-1');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('success');
    expect(component.jobResult?.items.length).toBe(1);
    expect(component.jobResult?.items[0].outputPath).toBe('/tmp/out/merged.mp4');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));

  it('cancel transition reports backend failure explicitly', async () => {
    component.activeJobId = 'video-merge-job-cancel';
    wailsSpy.cancelJobV1.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'cancel failed',
        jobId: 'video-merge-job-cancel',
        error: { code: 'VALIDATION_INVALID_INPUT', message: 'cannot cancel' },
      })
    );

    await component.cancel();
    await waitMicrotask();

    expect(component.statusMessage).toContain('At least 2 input videos are required to merge');
  });
});
