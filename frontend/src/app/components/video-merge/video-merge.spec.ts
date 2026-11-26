import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoMerge } from './video-merge';
import { DESTINATION_PRESETS } from '../../config/destination-presets';
import { I18nService } from '../../services/i18n.service';
import { toolOptionsStorageKey } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('VideoMerge', () => {
  let fixture: ComponentFixture<VideoMerge>;
  let component: VideoMerge;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
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
  ): JobStatusResponse {
    const result: JobResult = {
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

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function storedValues(): Record<string, unknown> {
    const raw = localStorage.getItem(toolOptionsStorageKey('tool.video.merge'));
    if (!raw) {
      return {};
    }
    return (
      (JSON.parse(raw) as { values?: Record<string, unknown> }).values ?? {}
    );
  }

  function seedOptions(values: Record<string, unknown>): void {
    localStorage.setItem(
      toolOptionsStorageKey('tool.video.merge'),
      JSON.stringify({ schemaVersion: 1, values }),
    );
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
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
      imports: [VideoMerge],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(VideoMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('requires at least two videos', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.mp4']);
    expect(component.canRun()).toBeFalse();
    expect(component.primaryLabel()).toBe('Merge videos');
    expect(component.actionHelper()).toBe('Add one more video to merge.');
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('plans a merge with the default preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('medium');

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
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

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect('removeAudio' in request.options).toBeFalse();

    component.onRemoveAudioToggle(checkedEvent(true));
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['removeAudio']).toBeTrue();
    expect(component.removeAudio()).toBeTrue();

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(runRequest.options['removeAudio']).toBeTrue();

    component.onRemoveAudioToggle(checkedEvent(false));
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect('removeAudio' in request.options).toBeFalse();
  }));

  it('omits transition options when the transition is None', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['transition']).toBe('crossfade');
    expect(request.options['transitionDuration']).toBe(0.5);

    component.onTransitionDurationInput(valueEvent('1.5'));
    tick(200);
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['transitionDuration']).toBe(1.5);

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
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
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('refreshes the plan when the transition changes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();
    const plannedOnce = wailsSpy.planJob.calls.count();

    component.onTransitionChange(valueEvent('crossfade'));
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 1);

    component.onTransitionDurationInput(valueEvent('1.2'));
    tick(199);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 1);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 2);

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['transitionDuration']).toBe(1.2);

    component.onTransitionChange(valueEvent('none'));
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 3);

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
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

    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-2', '/tmp/a.mp4', outputPath),
    );

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.video.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mov']);
    expect(wailsSpy.validateJob).not.toHaveBeenCalled();
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
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('fills the target from a chip and forces a re-encode', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();
    fixture.detectChanges();

    component.onMergeModeChange(valueEvent('copy'));
    tick(200);
    flushMicrotasks();
    expect(component.effectiveMergeMode()).toBe('copy');

    const chip = fixture.nativeElement.querySelector(
      '[data-preset="discord"]',
    ) as HTMLButtonElement;
    chip.click();
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.target.targetMb()).toBe(9.5);
    expect(component.target.sizeBytes()).toBe(9_961_472);
    expect(component.effectiveMergeMode()).toBe('reencode');

    const request = lastPlanRequest();
    expect(request.options['targetSizeBytes']).toBe(9_961_472);
    expect(request.options['mergeMode']).toBe('reencode');

    const select = fixture.nativeElement.querySelector(
      '#video-merge-mode',
    ) as HTMLSelectElement;
    expect(select.disabled).toBeTrue();
    expect(select.value).toBe('reencode');
  }));

  it('restores the chosen merge mode when the target is cleared', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.onMergeModeChange(valueEvent('copy'));
    component.selectTargetPreset(DESTINATION_PRESETS[0]);
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    component.onTargetInput(valueEvent(''));
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.target.hasTarget()).toBeFalse();
    expect(component.mergeMode()).toBe('copy');
    expect(component.effectiveMergeMode()).toBe('copy');
    expect(lastPlanRequest().options['mergeMode']).toBe('copy');
    expect('targetSizeBytes' in lastPlanRequest().options).toBeFalse();

    const select = fixture.nativeElement.querySelector(
      '#video-merge-mode',
    ) as HTMLSelectElement;
    expect(select.disabled).toBeFalse();
    expect(select.value).toBe('copy');
  }));

  it('remembers and restores the target with the merge mode', fakeAsync(() => {
    seedOptions({ mergeMode: 'copy', targetMb: 16 });

    const recreated = TestBed.createComponent(VideoMerge);
    recreated.detectChanges();
    flushMicrotasks();
    const page = recreated.componentInstance;

    expect(page.target.targetMb()).toBe(16);
    expect(page.mergeMode()).toBe('copy');
    expect(page.effectiveMergeMode()).toBe('reencode');

    page.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();
    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['targetSizeBytes']).toBe(16_777_216);
    expect(request.options['mergeMode']).toBe('reencode');

    recreated.destroy();
  }));

  it('saves the target and the chosen merge mode after a run', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.onMergeModeChange(valueEvent('copy'));
    component.selectTargetPreset(DESTINATION_PRESETS[2]);
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(storedValues()['targetMb']).toBe(25);
    expect(storedValues()['mergeMode']).toBe('copy');
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Unir');
    expect(title).toContain('video');

    expect(
      element.querySelector('#video-merge-transition option[value="crossfade"]')
        ?.textContent,
    ).toContain('Fundido cruzado');
    expect(
      element.querySelector('#video-merge-mode option[value="copy"]')
        ?.textContent,
    ).toContain('Copia');
    expect(
      element.querySelector('.toolbar .btn-secondary')?.textContent,
    ).toContain('Elegir vídeos');
    expect(
      element.querySelector('label[for="video-merge-target"]')?.textContent,
    ).toContain('Tamaño objetivo (MB)');
  });
});
