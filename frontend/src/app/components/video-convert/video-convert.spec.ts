import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoConvert } from './video-convert';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('VideoConvert', () => {
  let fixture: ComponentFixture<VideoConvert>;
  let component: VideoConvert;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    const format = String(request.options['targetFormat'] ?? 'mp4');
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, `_converted.${format}`),
        outputKind: 'file',
        exists: true,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: name.split('.').pop() ?? '',
      sizeBytes: 2048,
      durationSeconds: 83,
      kind: 'video',
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.video.convert',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'running',
        message: 'Converting video',
      },
      items: [],
      startedAt: 1,
    };
    return { success: true, message: 'running', found: true, result };
  }

  function terminalStatus(
    jobId: string,
    inputPath: string,
    outputPath: string,
  ): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.video.convert',
      status: 'success',
      progress: {
        current: 1,
        total: 1,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          success: true,
          message: 'Video conversion successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  function changeEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function checkedEvent(checked: boolean): Event {
    return { target: { checked } } as unknown as Event;
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listTools',
      'validateJob',
      'getThumbnail',
    ]);

    jobProgressSubject = new Subject<JobProgressEvent>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();

    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-default',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(runningStatus(jobId)),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'job-1',
    });
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.revealInFolder.and.resolveTo({
      success: true,
      message: 'revealed',
    });
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.isRuntimeAvailable.and.returnValue(true);
    wailsSpy.listTools.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });
    wailsSpy.validateJob.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [VideoConvert],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoConvert);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('adds one video and plans with the default MP4 preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.video.convert');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/clip.mov']);
    expect(request.outputDir).toBe('');
    expect(request.options['targetFormat']).toBe('mp4');
    expect(request.options['qualityPreset']).toBe('medium');

    expect(component.fileItems()[0].name).toBe('clip.mov');
    expect(component.fileItems()[0].meta).toBe('2 KB · 1:23');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/clip_converted.mp4',
    );
    expect(component.primaryLabel()).toBe('Convert 1 video');
    expect(component.canRun()).toBeTrue();
  }));

  it('adds multiple videos automatically as a batch', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mkv']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mkv']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.primaryLabel()).toBe('Convert 2 videos');
    expect(component.fileCountLabel()).toBe('2 videos');
  }));

  it('uses the selected preset in the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.selectPreset('webm-balanced');
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(component.selectedPresetId()).toBe('webm-balanced');
    expect(component.outputSummary()).toBe('WebM · Balanced');
  }));

  it('lets advanced controls override the preset and refresh the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onTargetFormatChange(changeEvent('webm'));
    component.onQualityChange(changeEvent('high'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('high');
    expect(component.hasAdvancedOverrides()).toBeTrue();

    component.selectPreset('mp4-small');
    flushMicrotasks();

    const presetRequest = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(presetRequest.options['targetFormat']).toBe('mp4');
    expect(presetRequest.options['qualityPreset']).toBe('low');
    expect(component.hasAdvancedOverrides()).toBeFalse();
  }));

  it('sends resolution, bitrate and audio options in the advanced plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onResolutionChange(changeEvent('720p'));
    component.onBitrateChange(changeEvent('5000'));
    component.onRemoveAudioToggle(checkedEvent(true));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['resolution']).toBe('720p');
    expect(request.options['bitrateKbps']).toBe(5000);
    expect(request.options['removeAudio']).toBeTrue();
    expect(component.hasAdvancedOverrides()).toBeTrue();
  }));

  it('omits Auto bitrate and a kept audio track from the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['resolution']).toBe('original');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect('removeAudio' in request.options).toBeFalse();
  }));

  it('resets resolution, bitrate and audio to preset defaults', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onResolutionChange(changeEvent('480p'));
    component.onBitrateChange(changeEvent('12000'));
    component.onRemoveAudioToggle(checkedEvent(true));
    tick(200);
    flushMicrotasks();

    component.resetAdvanced();
    tick(200);
    flushMicrotasks();

    expect(component.resolution()).toBe('original');
    expect(component.bitrateKbps()).toBeNull();
    expect(component.removeAudio()).toBeFalse();
    expect(component.hasAdvancedOverrides()).toBeFalse();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['targetFormat']).toBe('mp4');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(request.options['resolution']).toBe('original');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect('removeAudio' in request.options).toBeFalse();
  }));

  it('loads video thumbnails for the queue and keeps the icon on failure', fakeAsync(() => {
    wailsSpy.getThumbnail.and.callFake((path: string) =>
      Promise.resolve(
        path === '/tmp/a.mp4' ? 'data:image/webp;base64,abc' : null,
      ),
    );

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    expect(wailsSpy.getThumbnail).toHaveBeenCalledWith('/tmp/a.mp4', 160);
    expect(wailsSpy.getThumbnail).toHaveBeenCalledWith('/tmp/b.mov', 160);
    expect(component.fileItems()[0].thumbnailUrl).toBe(
      'data:image/webp;base64,abc',
    );
    expect(component.fileItems()[1].thumbnailUrl).toBeUndefined();
  }));

  it('runs directly from the plan without a validate step', fakeAsync(() => {
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-1',
      status: 'queued',
    });

    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.video.convert');
    expect(request.options['targetFormat']).toBe('mp4');
    expect(wailsSpy.validateJob).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');
    fixture.destroy();
  }));

  it('shows the terminal result, loads output sizes and opens the file', fakeAsync(() => {
    const inputPath = '/tmp/in/clip.mov';
    const outputPath = '/tmp/in/clip_converted.mp4';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-2', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 video converted');

    void component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);

    void component.onReveal(outputPath);
    flushMicrotasks();
    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith(outputPath);
  }));

  it('uses the chosen output folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(component.destinationLabel()).toBe('/tmp/out');

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.outputDir).toBe('/tmp/out');

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each original');
  }));

  it('rejects unsupported files with a plain notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt', '/tmp/photo.png']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "2 files weren't added. TXT and PNG aren't supported.",
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));
});
