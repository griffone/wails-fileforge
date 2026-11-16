import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoMerge } from './video-merge';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('VideoMerge', () => {
  let fixture: ComponentFixture<VideoMerge>;
  let component: VideoMerge;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const format = String(request.options['targetFormat'] ?? 'mp4');
    const first = request.inputPaths[0] ?? '/tmp/video';
    const stem = (first.split(/[\\/]/).pop() ?? 'video').replace(
      /\.[^./\\]+$/,
      '',
    );
    const directory = request.outputDir || '/tmp';
    return {
      success: true,
      message: 'ok',
      outputs: [
        {
          inputPath: request.inputPaths.join(','),
          outputPath: `${directory}/${stem}_merged.${format}`,
          outputKind: 'file',
          exists: false,
          adjusted: false,
        },
      ],
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string): FileInfoV1 {
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

  function runningStatus(jobId: string): JobStatusResponseV1 {
    const result: JobResultV1 = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.video.merge',
      status: 'running',
      progress: {
        current: 0,
        total: 2,
        stage: 'running',
        message: 'Merging videos',
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
  ): JobStatusResponseV1 {
    const result: JobResultV1 = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.video.merge',
      status: 'success',
      progress: {
        current: 2,
        total: 2,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          success: true,
          message: 'Video merge successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  function checkedEvent(checked: boolean): Event {
    return { target: { checked } } as unknown as Event;
  }

  function valueEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJobV1',
      'inspectFilesV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listToolsV1',
      'validateJobV1',
      'getThumbnail',
    ]);

    jobProgressSubject = new Subject<JobProgressEventV1>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();

    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-default',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) =>
      Promise.resolve(runningStatus(jobId)),
    );
    wailsSpy.cancelJobV1.and.resolveTo({
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
    wailsSpy.listToolsV1.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });
    wailsSpy.validateJobV1.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [VideoMerge],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('requires at least two videos', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.mp4']);
    expect(component.canRun()).toBeFalse();
    expect(component.primaryLabel()).toBe('Merge videos');
    expect(component.actionHelper()).toBe('Add one more video to merge.');
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));

  it('plans a merge with the default preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mov']);
    expect(request.outputDir).toBe('');
    expect(request.options['targetFormat']).toBe('mp4');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/a_merged.mp4');
    expect(component.primaryLabel()).toBe('Merge 2 videos');
    expect(component.fileItems()[0].meta).toBe('2 KB · 1:23');
  }));

  it('reorders the queue and refreshes the plan input order', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov', '/tmp/c.mkv']);
    flushMicrotasks();

    component.moveDown('/tmp/a.mp4');
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([
      '/tmp/b.mov',
      '/tmp/a.mp4',
      '/tmp/c.mkv',
    ]);

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.inputPaths).toEqual([
      '/tmp/b.mov',
      '/tmp/a.mp4',
      '/tmp/c.mkv',
    ]);
    expect(component.primaryLabel()).toBe('Merge 3 videos');
  }));

  it('uses the selected preset and the chosen folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.selectPreset('webm-balanced');
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('medium');

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/a_merged.webm',
    );

    component.useFirstVideoFolder();
    flushMicrotasks();
    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to the first video');
  }));

  it('sends removeAudio only when audio is removed', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect('removeAudio' in request.options).toBeFalse();

    component.onRemoveAudioToggle(checkedEvent(true));
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options['removeAudio']).toBeTrue();
    expect(component.removeAudio()).toBeTrue();

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(runRequest.options['removeAudio']).toBeTrue();

    component.onRemoveAudioToggle(checkedEvent(false));
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect('removeAudio' in request.options).toBeFalse();
  }));

  it('omits transition options when the transition is None', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect('transition' in request.options).toBeFalse();
    expect('transitionDuration' in request.options).toBeFalse();

    fixture.detectChanges();
    const durationInput = fixture.nativeElement.querySelector(
      '#video-merge-transition-duration',
    );
    expect(durationInput).toBeNull();
  }));

  it('sends both transition options for a crossfade', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.onTransitionChange(valueEvent('crossfade'));
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['transition']).toBe('crossfade');
    expect(request.options['transitionDuration']).toBe(0.5);

    component.onTransitionDurationInput(valueEvent('1.5'));
    tick(200);
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options['transitionDuration']).toBe(1.5);

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(runRequest.options['transition']).toBe('crossfade');
    expect(runRequest.options['transitionDuration']).toBe(1.5);
  }));

  it('blocks the action and shows an inline error for an invalid crossfade duration', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.onTransitionChange(valueEvent('crossfade'));
    flushMicrotasks();
    fixture.detectChanges();

    component.onTransitionDurationInput(valueEvent('5'));
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.transitionDurationInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();

    const error = fixture.nativeElement.querySelector(
      '.field-error',
    ) as HTMLElement | null;
    expect(error?.textContent).toContain('between 0.1 and 3.0 seconds');

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('refreshes the plan when the transition changes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();
    const plannedOnce = wailsSpy.planJobV1.calls.count();

    component.onTransitionChange(valueEvent('crossfade'));
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);

    component.onTransitionDurationInput(valueEvent('1.2'));
    tick(199);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 2);

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['transitionDuration']).toBe(1.2);

    component.onTransitionChange(valueEvent('none'));
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 3);

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect('transition' in request.options).toBeFalse();
    expect('transitionDuration' in request.options).toBeFalse();
  }));

  it('sends crossfade and removeAudio together', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.onTransitionChange(valueEvent('crossfade'));
    flushMicrotasks();
    component.onRemoveAudioToggle(checkedEvent(true));
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['transition']).toBe('crossfade');
    expect(request.options['transitionDuration']).toBe(0.5);
    expect(request.options['removeAudio']).toBeTrue();
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

  it('runs without a validate step and shows the terminal result', fakeAsync(() => {
    const outputPath = '/tmp/a_merged.mp4';

    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(
      terminalStatus('job-2', '/tmp/a.mp4', outputPath),
    );

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mov']);
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeFalse();
    expect(component.terminalJob()?.status).toBe('success');
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 video converted');

    void component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('rejects non-video files with a plain notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt', '/tmp/photo.png']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "notes.txt, photo.png weren't added because they aren't videos.",
    );
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));
});
