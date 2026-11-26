import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { AudioNormalize } from './audio-normalize';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobResultItem,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('AudioNormalize', () => {
  let fixture: ComponentFixture<AudioNormalize>;
  let component: AudioNormalize;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_normalized.wav'),
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
      sizeBytes: 4096,
      durationSeconds: 240,
      kind: 'audio',
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.audio.normalize',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'audio.normalize',
        message: 'Normalizing audio',
      },
      items: [],
      startedAt: 1,
    };
    return { success: true, message: 'running', found: true, result };
  }

  function terminalStatus(
    jobId: string,
    item: JobResultItem,
  ): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.audio.normalize',
      status: 'success',
      progress: {
        current: 100,
        total: 100,
        stage: 'audio.normalize',
        message: 'Finished',
      },
      items: [item],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  function successItem(
    inputPath: string,
    outputPath: string,
    details?: Record<string, unknown>,
  ): JobResultItem {
    return {
      inputPath,
      outputPath,
      outputs: [outputPath],
      success: true,
      message: 'Normalized to -14 LUFS.',
      details,
    };
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
      imports: [AudioNormalize],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(AudioNormalize);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders the manifest loudness targets and true peak default', () => {
    const element = fixture.nativeElement as HTMLElement;
    const targets = Array.from(
      element.querySelectorAll('#audio-tool-target option'),
    ).map((option) => (option as HTMLOptionElement).value);
    expect(targets).toEqual(['streaming', 'broadcast', 'podcast']);

    expect(
      (element.querySelector('#audio-tool-target') as HTMLSelectElement).value,
    ).toBe('streaming');
    expect(element.querySelector('#audio-tool-target')?.textContent).toContain(
      'Streaming (-14 LUFS)',
    );
    expect(
      (element.querySelector('#audio-tool-true-peak') as HTMLInputElement)
        .value,
    ).toBe('-1.5');
  });

  it('plans only the two backend option keys with their defaults', fakeAsync(() => {
    component.onPathsSelected(['/tmp/voice.wav']);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.audio.normalize');
    expect(request.options['targetLufs']).toBe('streaming');
    expect(request.options['truePeak']).toBe(-1.5);
    expect('format' in request.options).toBeFalse();
    expect('bitrateKbps' in request.options).toBeFalse();
    expect(component.primaryLabel()).toBe('Normalize 1 audio file');
  }));

  it('sends the chosen target and true peak', fakeAsync(() => {
    component.onPathsSelected(['/tmp/voice.wav']);
    flushMicrotasks();

    component.onTargetChange(changeEvent('podcast'));
    component.onTruePeakInput(changeEvent('-2.5'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options['targetLufs']).toBe('podcast');
    expect(request.options['truePeak']).toBe(-2.5);
    expect(component.outputBadge()).toBe('Podcast (-16 LUFS) · -2.5 dBTP');
  }));

  it('blocks the run and clears the plan for an out-of-range true peak', fakeAsync(() => {
    component.onPathsSelected(['/tmp/voice.wav']);
    flushMicrotasks();
    expect(component.plan()).not.toBeNull();

    component.onTruePeakInput(changeEvent('2'));
    tick(200);
    flushMicrotasks();

    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(component.truePeakError()).toContain('-9');
    expect(component.truePeakError()).toContain('0');

    fixture.detectChanges();
    expect(
      fixture.nativeElement.textContent,
    ).toContain('Enter a true peak between -9 and 0 dBTP.');
  }));

  it('stores the accepted options and renders the measured loudness', fakeAsync(() => {
    const inputPath = '/tmp/in/voice.wav';
    const outputPath = '/tmp/in/voice_normalized.wav';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-4',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus(
        'job-4',
        successItem(inputPath, outputPath, {
          targetName: 'streaming',
          targetLufs: -14,
          truePeak: -1.5,
          inputLufs: -18.4,
          inputTruePeak: -2.1,
          outputLufs: -14.2,
          outputTruePeak: -1.5,
        }),
      ),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    component.onTargetChange(changeEvent('podcast'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.audio.normalize',
      1,
      ['targetLufs', 'truePeak'],
    );
    expect(stored).toEqual({ targetLufs: 'podcast', truePeak: -1.5 });

    expect(component.terminalJob()?.status).toBe('success');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.textContent).toContain('1 file normalized');
    const row = element.querySelector('.loudness-row');
    expect(row?.textContent).toContain('voice_normalized.wav');
    expect(row?.textContent).toContain(
      'Input -18.4 LUFS → Output -14.2 LUFS',
    );
  }));

  it('renders no loudness block when the result has no details', fakeAsync(() => {
    const inputPath = '/tmp/in/voice.wav';
    const outputPath = '/tmp/in/voice_normalized.wav';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-5',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-5', successItem(inputPath, outputPath)),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('success');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.result-card')).not.toBeNull();
    expect(element.querySelector('.loudness-panel')).toBeNull();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Normalizar',
    );
    expect(
      element.querySelector('#audio-tool-target option[value="streaming"]')
        ?.textContent,
    ).toContain('Streaming (-14 LUFS)');
    expect(element.textContent).toContain('Objetivo de sonoridad');
  });
});
