import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { AudioConvert } from './audio-convert';
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

describe('AudioConvert', () => {
  let fixture: ComponentFixture<AudioConvert>;
  let component: AudioConvert;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    const format = String(request.options['format'] ?? 'mp3');
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(
          /\.[^./\\]+$/,
          `_converted.${format}`,
        ),
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
      sizeBytes: 1024,
      durationSeconds: 180,
      kind: 'audio',
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.audio.convert',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'audio.convert',
        message: 'Converting audio',
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
      toolId: 'tool.audio.convert',
      status: 'success',
      progress: {
        current: 100,
        total: 100,
        stage: 'audio.convert',
        message: 'Finished',
      },
      items: [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          success: true,
          message: 'Audio conversion successful',
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
      imports: [AudioConvert],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(AudioConvert);
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
    component.onPathsSelected(['/tmp/song.flac']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.audio.convert');
    expect(request.mode).toBe('single');
    expect(request.options['format']).toBe('mp3');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/song_converted.mp3',
    );
    expect(component.primaryLabel()).toBe('Convert 1 audio file');
  }));

  it('sends a chosen lossy format and bitrate', fakeAsync(() => {
    component.onPathsSelected(['/tmp/song.flac']);
    flushMicrotasks();

    component.onFormatChange(changeEvent('opus'));
    component.onBitrateChange(changeEvent('256'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['format']).toBe('opus');
    expect(request.options['bitrateKbps']).toBe(256);
    expect(component.outputBadge()).toBe('OPUS · 256 kbps');
  }));

  it('disables bitrate for a lossless format and states the warning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/song.flac']);
    flushMicrotasks();

    component.onBitrateChange(changeEvent('320'));
    component.onFormatChange(changeEvent('flac'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['format']).toBe('flac');
    expect('bitrateKbps' in request.options).toBeFalse();
    expect(component.bitrateKbps()).toBeNull();
    expect(component.outputBadge()).toBe('FLAC · Lossless');

    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;
    expect(
      (element.querySelector('#audio-tool-bitrate') as HTMLSelectElement)
        .disabled,
    ).toBeTrue();
    expect(element.querySelector('.lossless-warning')?.textContent).toContain(
      'FLAC',
    );
  }));

  it('stores the accepted options and renders the terminal job', fakeAsync(() => {
    const inputPath = '/tmp/in/song.flac';
    const outputPath = '/tmp/in/song_converted.m4a';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-3',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-3', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    component.onFormatChange(changeEvent('m4a'));
    component.onBitrateChange(changeEvent('128'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.audio.convert',
      1,
      ['format', 'bitrateKbps'],
    );
    expect(stored).toEqual({ format: 'm4a', bitrateKbps: 128 });

    expect(component.terminalJob()?.status).toBe('success');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 file converted');
  }));

  it('rejects a video with the shared notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mp4']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toContain("MP4 isn't supported");
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Convertir',
    );
    expect(element.querySelector('.page-title')?.textContent).toContain('audio');
  });
});
