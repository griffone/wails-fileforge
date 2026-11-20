import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EMPTY, Subject } from 'rxjs';

import { VideoTranscribe } from './video-transcribe';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import {
  FileInfo,
  JobProgress,
  JobRequest,
  JobResultItem,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  TranscriptionCapabilitiesResponse,
  Wails,
} from '../../services/wails';

describe('VideoTranscribe', () => {
  let fixture: ComponentFixture<VideoTranscribe>;
  let component: VideoTranscribe;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobStateSubject: Subject<JobResult>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function capabilities(
    overrides: Partial<TranscriptionCapabilitiesResponse> = {},
  ): TranscriptionCapabilitiesResponse {
    return {
      success: true,
      message: 'Local transcription is available.',
      available: true,
      whisperPath: '/home/user/.local/share/fileforge/whisper/whisper-cli',
      whisperPresent: true,
      modelPath: '/home/user/.local/share/fileforge/models/ggml-base.bin',
      modelPresent: true,
      modelName: 'ggml-base',
      modelSizeBytes: 142 * 1024 * 1024,
      languages: ['auto', 'en', 'es'],
      defaultLanguage: 'auto',
      ...overrides,
    };
  }

  function fileInfo(path: string): FileInfo {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'mp4',
      sizeBytes: 2 * 1024 * 1024,
      kind: 'video',
      durationSeconds: 65,
      exists: true,
    };
  }

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function fileStem(path: string): string {
    const name = path.split(/[\\/]/).pop() ?? 'media';
    return name.replace(/\.[^.]+$/, '');
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const formats = Array.isArray(request.options['formats'])
      ? (request.options['formats'] as string[])
      : [];
    const outputs = request.inputPaths.flatMap((inputPath) => {
      const dir = request.outputDir || firstDir(inputPath);
      const stem = fileStem(inputPath);
      return formats.map((format) => ({
        inputPath,
        outputPath: dir ? `${dir}/${stem}.${format}` : `${stem}.${format}`,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      }));
    });

    return {
      success: true,
      message: 'Ready to transcribe 1 file.',
      outputs,
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResult['status'],
    items: JobResultItem[],
    progress: Partial<JobProgress> = {},
  ): JobStatusResponse {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.video.transcribe',
        status: jobStatus,
        progress: {
          current: items.length,
          total: items.length || 1,
          stage: jobStatus,
          message: jobStatus,
          ...progress,
        },
        items,
        startedAt: 1,
        endedAt: jobStatus === 'running' ? undefined : 2,
      },
    };
  }

  /** Creates the page with the given capabilities and renders the first frame. */
  function create(
    caps: TranscriptionCapabilitiesResponse = capabilities(),
  ): void {
    wailsSpy.getTranscriptionCapabilities.and.resolveTo(caps);
    fixture = TestBed.createComponent(VideoTranscribe);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'getTranscriptionCapabilities',
      'inspectFiles',
      'planJob',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openDirectoryDialog',
      'openMultipleFilesDialog',
      'openPath',
      'revealInFolder',
    ]);
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ = EMPTY;
    jobStateSubject = new Subject<JobResult>();
    (wailsSpy as unknown as { jobState$: unknown }).jobState$ =
      jobStateSubject.asObservable();

    wailsSpy.getTranscriptionCapabilities.and.resolveTo(capabilities());
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'transcribe-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'transcribe-job-1',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [VideoTranscribe],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('loads capabilities, applies the default language and renders the page', fakeAsync(() => {
    create(capabilities({ defaultLanguage: 'en' }));
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledWith('', '');
    expect(component.transcribeAvailable()).toBeTrue();
    expect(component.transcriptionBlocked()).toBeFalse();
    expect(component.language()).toBe('en');
    expect(component.formats()).toEqual(['srt', 'vtt', 'txt']);
    expect(component.modelSummary()).toBe(
      'Whisper ready · ggml-base · 142 MB · Auto/English/Spanish',
    );

    const select = query<HTMLSelectElement>('#video-transcribe-language');
    expect(select).not.toBeNull();
    expect(select?.value).toBe('en');
    expect(select?.options.length).toBe(3);

    expect(
      (query<HTMLInputElement>('#video-transcribe-format-srt') as HTMLInputElement)
        .checked,
    ).toBeTrue();
    expect(
      (query<HTMLInputElement>('#video-transcribe-format-vtt') as HTMLInputElement)
        .checked,
    ).toBeTrue();
    expect(
      (query<HTMLInputElement>('#video-transcribe-format-txt') as HTMLInputElement)
        .checked,
    ).toBeTrue();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Video Transcribe');
    expect(text).toContain('Drop video or audio here');
    expect(text).toContain('Whisper ready');
    expect(text).toContain(
      'Whisper runs locally. Long files take longer on CPU.',
    );
    expect(text).not.toContain('Local transcription is unavailable.');
    expect(component.actionHelper()).toBe(
      'Add at least one video or audio file.',
    );
  }));

  it('shows install guidance, a Settings link and blocks the run when Whisper is unavailable', fakeAsync(() => {
    create(
      capabilities({
        message: 'Local transcription is unavailable: whisper-cli was not found.',
        available: false,
        whisperPresent: false,
        modelPresent: false,
        modelName: '',
        modelSizeBytes: 0,
        defaultLanguage: '',
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.transcribeAvailable()).toBeFalse();
    expect(component.transcriptionBlocked()).toBeTrue();
    expect(component.modelSummary()).toBe('');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Local transcription is unavailable.');
    expect(text).toContain(
      '/home/user/.local/share/fileforge/whisper/whisper-cli',
    );
    expect(text).toContain(
      '/home/user/.local/share/fileforge/models/ggml-base.bin',
    );
    expect(text).toContain(
      'Install the whisper.cpp binary and a Whisper model, then choose the paths in Settings.',
    );

    const link = query<HTMLAnchorElement>('.banner-link');
    expect(link?.textContent).toContain('Choose paths in Settings');
    expect(link?.getAttribute('href')).toBe('/settings');

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Local transcription is unavailable on this machine.',
    );
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('plans with the chosen language, formats and saved whisper paths', fakeAsync(() => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({
        defaultOutputDir: '/tmp/default',
        whisperPath: '/opt/whisper-cli',
        whisperModelPath: '/models/ggml-base.bin',
      }),
    );

    create(capabilities({ defaultLanguage: 'auto' }));
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    let request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.video.transcribe');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/in/talk.mp4']);
    expect(request.outputDir).toBe('/tmp/default');
    expect(request.options).toEqual({
      language: 'auto',
      formats: ['srt', 'vtt', 'txt'],
      whisperPath: '/opt/whisper-cli',
      modelPath: '/models/ggml-base.bin',
    });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/default/talk.srt',
      '/tmp/default/talk.vtt',
      '/tmp/default/talk.txt',
    ]);
    expect(component.primaryLabel()).toBe('Transcribe 1 file');

    component.onLanguageChange({ target: { value: 'es' } } as unknown as Event);
    component.onFormatToggle('txt', {
      target: { checked: false },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options).toEqual({
      language: 'es',
      formats: ['srt', 'vtt'],
      whisperPath: '/opt/whisper-cli',
      modelPath: '/models/ggml-base.bin',
    });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/default/talk.srt',
      '/tmp/default/talk.vtt',
    ]);
  }));

  it('uses batch mode for several files and rejects non-media drops', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected([
      '/tmp/in/a.mp4',
      '/tmp/in/b.mp3',
      '/tmp/in/notes.txt',
    ]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/in/a.mp4', '/tmp/in/b.mp3']);
    expect(component.notice()?.text).toContain(
      "isn't a video or audio file",
    );
    expect(component.primaryLabel()).toBe('Transcribe 2 files');
    expect(component.plan()?.outputs.length).toBe(6);
  }));

  it('blocks the run when no output format is selected', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();

    for (const id of ['srt', 'vtt', 'txt']) {
      component.onFormatToggle(id, {
        target: { checked: false },
      } as unknown as Event);
    }
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.formats()).toEqual([]);
    expect(component.formatsMissing()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe('Choose at least one output format.');
    expect(fixture.nativeElement.textContent).toContain(
      'Choose at least one output format.',
    );

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('runs, observes to terminal and shows the transcript summary and result', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.getJobStatus.and.resolveTo(
      status('transcribe-job-1', 'running', [], {
        stage: 'transcribe',
        current: 40,
        total: 100,
        message: 'Transcribing audio with Whisper…',
      }),
    );
    const completed = status('transcribe-job-1', 'success', [
      {
        inputPath: '/tmp/in/talk.mp4',
        outputPath: '/tmp/in/talk.srt',
        outputs: ['/tmp/in/talk.srt', '/tmp/in/talk.vtt', '/tmp/in/talk.txt'],
        outputCount: 3,
        success: true,
        message: 'Transcribed: 8 segments, 412 characters.',
      },
    ]);

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    expect(component.activeJobId()).toBe('transcribe-job-1');
    expect(component.isPolling()).toBeTrue();
    expect(component.primaryLabel()).toBe('Transcribing…');
    expect(fixture.nativeElement.textContent).toContain(
      'Transcribing audio with Whisper…',
    );

    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.transcriptSummary()?.tone).toBe('success');
    expect(component.transcriptSummary()?.text).toBe(
      'Transcribed: 8 segments, 412 characters.',
    );

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Transcribed: 8 segments, 412 characters.');
    expect(text).toContain('1 file transcribed');
    expect(query('app-result-card')).not.toBeNull();
    expect(text).toContain('talk.srt');
    expect(text).toContain('talk.vtt');
    expect(text).toContain('talk.txt');

    const inspectedPaths = wailsSpy.inspectFiles.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some(
        (paths) =>
          paths.includes('/tmp/in/talk.srt') &&
          paths.includes('/tmp/in/talk.vtt') &&
          paths.includes('/tmp/in/talk.txt'),
      ),
    ).toBeTrue();

    (query<HTMLButtonElement>('.result-open') as HTMLButtonElement).click();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/in/talk.srt');
  }));

  it('uses the warning tone when no speech was recognized', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.getJobStatus.and.resolveTo(
      status('transcribe-job-1', 'success', [
        {
          inputPath: '/tmp/in/silent.mp4',
          outputPath: '/tmp/in/silent.srt',
          outputs: ['/tmp/in/silent.srt', '/tmp/in/silent.vtt', '/tmp/in/silent.txt'],
          outputCount: 3,
          success: true,
          message:
            'Transcribed: 0 segments, 0 characters. No speech was recognized.',
        },
      ]),
    );

    component.onPathsSelected(['/tmp/in/silent.mp4']);
    tick(250);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.transcriptSummary()?.tone).toBe('warning');
    const summary = query<HTMLElement>('.transcript-summary');
    expect(summary?.classList.contains('banner-warning')).toBeTrue();
    expect(summary?.classList.contains('banner-success')).toBeFalse();
  }));

  it('cancels the running job', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isPolling()).toBeTrue();

    void component.cancel();
    flushMicrotasks();

    expect(wailsSpy.cancelJob).toHaveBeenCalledWith('transcribe-job-1');

    component.reset();
    flushMicrotasks();
    expect(component.isPolling()).toBeFalse();
  }));

  it('uses the chosen output folder in the plan', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/in/talk.mp4']);
    tick(250);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(wailsSpy.planJob.calls.mostRecent().args[0].outputDir).toBe(
      '/tmp/out',
    );
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/out/talk.srt');

    component.useDefaultOutputFolder();
    tick(250);
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(wailsSpy.planJob.calls.mostRecent().args[0].outputDir).toBe('');
  }));

  it('seeds the destination from the saved preferences', fakeAsync(() => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({
        defaultOutputDir: '/tmp/default',
        whisperPath: '',
        whisperModelPath: '',
      }),
    );

    create();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.outputDir()).toBe('/tmp/default');
    expect(component.destinationLabel()).toBe('/tmp/default');
  }));
});
