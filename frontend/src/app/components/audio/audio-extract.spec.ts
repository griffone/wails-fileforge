import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { AudioExtract } from './audio-extract';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
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

describe('AudioExtract', () => {
  let fixture: ComponentFixture<AudioExtract>;
  let component: AudioExtract;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    const format = String(request.options['format'] ?? 'mp3');
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, `_audio.${format}`),
        outputKind: 'file',
        exists: false,
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
      kind: 'audio',
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.audio.extract',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'audio.extract',
        message: 'Extracting audio',
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
      toolId: 'tool.audio.extract',
      status: 'success',
      progress: {
        current: 100,
        total: 100,
        stage: 'audio.extract',
        message: 'Finished',
      },
      items: [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          success: true,
          message: 'Audio extraction successful',
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
      imports: [AudioExtract],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(AudioExtract);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders the five manifest formats in the option form', () => {
    const element = fixture.nativeElement as HTMLElement;
    const options = Array.from(
      element.querySelectorAll('#audio-tool-format option'),
    ).map((option) => (option as HTMLOptionElement).value);

    expect(options).toEqual(['mp3', 'm4a', 'opus', 'wav', 'flac']);
    expect(
      (element.querySelector('#audio-tool-format') as HTMLSelectElement).value,
    ).toBe('mp3');
    expect(
      (element.querySelector('#audio-tool-bitrate') as HTMLSelectElement).value,
    ).toBe('');
  });

  it('plans with the manifest defaults', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mp4']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.audio.extract');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/clip.mp4']);
    expect(request.options['format']).toBe('mp3');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/clip_audio.mp3',
    );
    expect(component.primaryLabel()).toBe('Extract audio from 1 file');
    expect(component.canRun()).toBeTrue();
  }));

  it('sends a chosen lossy format and bitrate', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mp4']);
    flushMicrotasks();

    component.onFormatChange(changeEvent('m4a'));
    component.onBitrateChange(changeEvent('320'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['format']).toBe('m4a');
    expect(request.options['bitrateKbps']).toBe(320);
    expect(component.outputBadge()).toBe('M4A · 320 kbps');
  }));

  it('disables bitrate for a lossless format and states the warning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mp4']);
    flushMicrotasks();

    component.onBitrateChange(changeEvent('192'));
    component.onFormatChange(changeEvent('wav'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['format']).toBe('wav');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect(component.bitrateKbps()).toBeNull();
    expect(component.outputBadge()).toBe('WAV · Lossless');
    expect(component.losslessWarning()).toContain('WAV');

    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;
    expect(
      (element.querySelector('#audio-tool-bitrate') as HTMLSelectElement)
        .disabled,
    ).toBeTrue();
    expect(element.querySelector('.lossless-warning')?.textContent).toContain(
      'because the format is lossless',
    );
  }));

  it('stores the accepted options and renders the terminal job', fakeAsync(() => {
    const inputPath = '/tmp/in/clip.mp4';
    const outputPath = '/tmp/in/clip_audio.mp3';

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

    component.onFormatChange(changeEvent('flac'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.audio.extract',
      1,
      ['format', 'bitrateKbps'],
    );
    expect(stored).toEqual({ format: 'flac', bitrateKbps: null });

    expect(component.terminalJob()?.status).toBe('success');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 file extracted');
  }));

  it('rejects unsupported files with the shared notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "1 file wasn't added. TXT isn't supported.",
    );
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Extraer',
    );
    expect(element.querySelector('.toolbar .btn-secondary')?.textContent).toContain(
      'Elegir archivos',
    );
  });
});
