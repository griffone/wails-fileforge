import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoTrim } from './video-trim';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('VideoTrim', () => {
  let fixture: ComponentFixture<VideoTrim>;
  let component: VideoTrim;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const format = String(request.options['targetFormat'] ?? 'mp4');
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, `_trimmed.${format}`),
        outputKind: 'file',
        exists: true,
        adjusted: false,
      })),
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
      toolId: 'tool.video.trim',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'running',
        message: 'Trimming video',
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
      toolId: 'tool.video.trim',
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
          message: 'Video trim successful',
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
      imports: [VideoTrim],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoTrim);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('starts with a zero start, an empty end and no plan', () => {
    expect(component.startInput()).toBe('0');
    expect(component.endInput()).toBe('');
    expect(component.timeError()).toBe('');
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe('Add at least one video to trim.');
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  });

  it('shows the selected video duration and size in the queue', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    expect(component.fileItems()[0].meta).toBe('2 KB · 1:23');
    expect(component.sourceDurationLabel()).toBe('1:23');
    expect(component.isBatch()).toBeFalse();
    expect(component.actionHelper()).toBe('Set an end time to trim.');
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));

  it('parses time inputs and sends the trim range in the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onEndInput(changeEvent('00:30'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.trim');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/clip.mov']);
    expect(request.options['startTime']).toBe(0);
    expect(request.options['endTime']).toBe(30);
    expect(request.options['targetFormat']).toBe('mp4');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(request.options['trimMode']).toBe('auto');
    expect(component.primaryLabel()).toBe('Trim 1 video');
    expect(component.canRun()).toBeTrue();
  }));

  it('accepts MM:SS and HH:MM:SS on input and blur', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onEndInput(changeEvent('1:23'));
    component.onEndBlur(changeEvent('1:23'));
    tick(200);
    flushMicrotasks();

    expect(component.endSeconds()).toBe(83);
    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['endTime']).toBe(83);

    component.onStartInput(changeEvent('00:10'));
    component.onEndInput(changeEvent('00:00:30'));
    tick(200);
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options['startTime']).toBe(10);
    expect(request.options['endTime']).toBe(30);
  }));

  it('blocks invalid ranges with inline errors', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onEndInput(changeEvent('0'));
    expect(component.timeError()).toBe('End must be after start.');
    expect(component.canRun()).toBeFalse();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();

    component.onEndInput(changeEvent('abc'));
    expect(component.timeError()).toBe('Use a time like 00:30.');
    expect(component.canRun()).toBeFalse();

    component.onStartInput(changeEvent('2:00'));
    component.onEndInput(changeEvent('2:30'));
    expect(component.timeError()).toBe('Start must be before the video ends.');
    expect(component.canRun()).toBeFalse();
  }));

  it('uses the preset and advanced trim mode in the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();

    component.onEndInput(changeEvent('00:30'));
    tick(200);
    flushMicrotasks();

    component.selectPreset('webm-balanced');
    flushMicrotasks();

    component.onTrimModeChange(changeEvent('copy'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(request.options['trimMode']).toBe('copy');
  }));

  it('notes that a batch shares one time range', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mkv']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isBatch()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'The time range applies to every video.',
    );
  }));

  it('runs without a validate step and shows the terminal result', fakeAsync(() => {
    const inputPath = '/tmp/in/clip.mov';
    const outputPath = '/tmp/in/clip_trimmed.mp4';

    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(
      terminalStatus('job-2', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();
    component.onEndInput(changeEvent('00:30'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.trim');
    expect(request.options['endTime']).toBe(30);
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

  it('uses the chosen output folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/clip.mov']);
    flushMicrotasks();
    component.onEndInput(changeEvent('00:30'));
    tick(200);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
  }));
});
