import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoConvert } from './video-convert';
import { DESTINATION_PRESETS } from '../../config/destination-presets';
import { I18nService } from '../../services/i18n.service';
import {
  toolOptionsStorageKey,
} from '../../services/preferences.service';
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
    details?: Record<string, unknown>,
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
          ...(details ? { details } : {}),
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

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function checkedEvent(checked: boolean): Event {
    return { target: { checked } } as unknown as Event;
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function storedValues(): Record<string, unknown> {
    const raw = localStorage.getItem(
      toolOptionsStorageKey('tool.video.convert'),
    );
    if (!raw) {
      return {};
    }
    return (
      (JSON.parse(raw) as { values?: Record<string, unknown> }).values ?? {}
    );
  }

  function seedOptions(values: Record<string, unknown>): void {
    localStorage.setItem(
      toolOptionsStorageKey('tool.video.convert'),
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
      imports: [VideoConvert],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(VideoConvert);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
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

  it('round-trips the advanced disclosure state', () => {
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const details = element.querySelector(
      'details.advanced',
    ) as HTMLDetailsElement;
    expect(details).toBeTruthy();
    expect(details.open).toBeFalse();
    expect(component.advancedOpen()).toBeFalse();

    details.open = true;
    details.dispatchEvent(new Event('toggle'));
    fixture.detectChanges();
    expect(component.advancedOpen()).toBeTrue();

    component.advancedOpen.set(false);
    fixture.detectChanges();
    expect(details.open).toBeFalse();

    component.advancedOpen.set(true);
    fixture.detectChanges();
    expect(details.open).toBeTrue();
  });

  it('fills the target from a preset chip and re-plans in bytes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();
    fixture.detectChanges();

    const chip = fixture.nativeElement.querySelector(
      '[data-preset="discord"]',
    ) as HTMLButtonElement;
    chip.click();
    tick(200);
    flushMicrotasks();

    expect(component.target.targetMb()).toBe(9.5);
    expect(component.target.activePresetId()).toBe('discord');
    expect(component.target.sizeBytes()).toBe(9_961_472);
    expect(component.target.summary()).toBe('Target 9.5 MB per video');
    expect(lastPlanRequest().options['targetSizeBytes']).toBe(9_961_472);
  }));

  it('deselects the chip for a custom target and matches a typed preset value', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onTargetInput(inputEvent('9.5'));
    tick(200);
    flushMicrotasks();
    expect(component.target.activePresetId()).toBe('discord');

    component.onTargetInput(inputEvent('12'));
    tick(200);
    flushMicrotasks();
    expect(component.target.activePresetId()).toBeNull();
    expect(lastPlanRequest().options['targetSizeBytes']).toBe(12_582_912);

    component.onTargetInput(inputEvent('16'));
    tick(200);
    flushMicrotasks();
    expect(component.target.activePresetId()).toBe('whatsapp');
    expect(component.target.sizeBytes()).toBe(16_777_216);
  }));

  it('shows the preset and the target on the plan card', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.selectTargetPreset(
      DESTINATION_PRESETS.find((preset) => preset.id === 'email')!,
    );
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    const row = fixture.nativeElement.querySelector(
      '.output-plan-target',
    ) as HTMLElement;
    expect(row.textContent).toContain('25 MB');
    expect(row.textContent).toContain('Email');
  }));

  it('blocks the plan and the run for an unusable target', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    component.onTargetInput(inputEvent('0'));
    tick(200);
    flushMicrotasks();

    expect(component.target.valid()).toBeFalse();
    expect(component.target.hasTarget()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Enter a target size greater than 0 MB.',
    );
  }));

  it('disables and clears the bitrate while a target is set', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onBitrateChange(changeEvent('5000'));
    tick(200);
    flushMicrotasks();
    expect(component.bitrateKbps()).toBe(5000);
    expect(lastPlanRequest().options['bitrateKbps']).toBe(5000);

    component.selectTargetPreset(DESTINATION_PRESETS[0]);
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.bitrateKbps()).toBeNull();
    expect(lastPlanRequest().options['targetSizeBytes']).toBe(9_961_472);
    expect('bitrateKbps' in lastPlanRequest().options).toBeFalse();
    const bitrate = fixture.nativeElement.querySelector(
      '#video-convert-bitrate',
    ) as HTMLSelectElement;
    expect(bitrate.disabled).toBeTrue();

    component.onTargetInput(inputEvent(''));
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.target.hasTarget()).toBeFalse();
    expect('targetSizeBytes' in lastPlanRequest().options).toBeFalse();
    expect(bitrate.disabled).toBeFalse();
  }));

  it('remembers and restores the target with the other options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onResolutionChange(changeEvent('720p'));
    component.selectTargetPreset(DESTINATION_PRESETS[0]);
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(storedValues()['targetMb']).toBe(9.5);
    expect(storedValues()['resolution']).toBe('720p');

    const recreated = TestBed.createComponent(VideoConvert);
    recreated.detectChanges();
    flushMicrotasks();
    const page = recreated.componentInstance;

    expect(page.target.targetMb()).toBe(9.5);
    expect(page.bitrateKbps()).toBeNull();

    page.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();
    const planned = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(planned.options['targetSizeBytes']).toBe(9_961_472);

    recreated.destroy();
  }));

  it('restores a remembered target and clears a conflicting bitrate', fakeAsync(() => {
    seedOptions({ targetMb: 16, bitrateKbps: 8000 });

    const recreated = TestBed.createComponent(VideoConvert);
    recreated.detectChanges();
    flushMicrotasks();
    const page = recreated.componentInstance;

    expect(page.target.targetMb()).toBe(16);
    expect(page.bitrateKbps()).toBeNull();

    recreated.destroy();
  }));

  it('renders the target report for a result with details', fakeAsync(() => {
    const inputPath = '/tmp/in/clip.mov';
    const outputPath = '/tmp/in/clip_converted.mp4';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-report',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-report', inputPath, outputPath, {
        targetSizeBytes: 9_961_472,
        actualSizeBytes: 8_900_000,
        targetBitrateKbps: 1480,
        floorKbps: 800,
        floorApplied: false,
        targetMet: true,
        attempts: 1,
      }),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();
    component.selectTargetPreset(DESTINATION_PRESETS[0]);
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    const text =
      (fixture.nativeElement.querySelector('.video-target-panel') as HTMLElement)
        ?.textContent ?? '';
    expect(text).toContain('Target result');
    expect(text).toContain('8.5 MB of 9.5 MB target · 1480 kbps');
    expect(text).toContain('Target met');
  }));

  it('renders no target report when the result has no details', fakeAsync(() => {
    const inputPath = '/tmp/in/clip.mov';
    const outputPath = '/tmp/in/clip_converted.mp4';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-nodetails',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-nodetails', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelector('.video-target-panel'),
    ).toBeNull();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Convertir');
    expect(title).toContain('video');

    expect(
      element.querySelector('#video-convert-quality option[value="high"]')
        ?.textContent,
    ).toContain('Alta');
    expect(
      element.querySelector('.toolbar .btn-secondary')?.textContent,
    ).toContain('Elegir vídeos');
    expect(
      element.querySelector('label[for="video-convert-target"]')?.textContent,
    ).toContain('Tamaño objetivo (MB)');
    expect(
      element.querySelector('.target-presets')?.getAttribute('aria-label'),
    ).toContain('Preajustes de destino');
  });
});
