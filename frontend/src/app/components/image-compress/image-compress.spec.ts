import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageCompress } from './image-compress';
import { DropIntakeService } from '../../services/drop-intake.service';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('ImageCompress', () => {
  let fixture: ComponentFixture<ImageCompress>;
  let component: ImageCompress;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_compressed.webp'),
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string, sizeBytes = 2048): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: name.split('.').pop() ?? '',
      sizeBytes,
      kind: 'image',
      width: 640,
      height: 480,
      exists: true,
    };
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

  function terminalStatus(
    jobId: string,
    inputPath: string,
    outputPath: string,
  ): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.image.compress',
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
          message: '2 MB → 400 KB (target 1 MB)',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'verifyJob',
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
        files: paths.map((path) => fileInfo(path)),
      }),
    );
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-default',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo({
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'job-default',
        success: false,
        message: 'running',
        toolId: 'tool.image.compress',
        status: 'running',
        progress: {
          current: 0,
          total: 1,
          stage: 'running',
          message: 'compressing 1/1',
        },
        items: [],
        startedAt: 1,
      },
    });
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
    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'unavailable',
      outputs: [],
    });

    await TestBed.configureTestingModule({
      imports: [ImageCompress],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(ImageCompress);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    localStorage.removeItem('ff-preferences');
  });

  it('plans with the default 1 MB target and full option set', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.compress');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/photo.png']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({
      targetSizeBytes: 1024 * 1024,
      format: 'auto',
      allowDownscale: true,
      stripMetadata: true,
    });
    expect(component.targetSummary()).toBe('Target 1 MB per image');
    expect(component.fileItems()[0].name).toBe('photo.png');
  }));

  it('converts the MB target into whole bytes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onTargetInput(inputEvent('2.5'));
    tick(200);
    flushMicrotasks();

    expect(component.targetSizeBytes()).toBe(Math.round(2.5 * 1024 * 1024));
    expect(component.targetSizeBytes()).toBe(2621440);
    expect(lastPlanRequest().options['targetSizeBytes']).toBe(2621440);

    component.onTargetInput(inputEvent('0.5'));
    tick(200);
    flushMicrotasks();

    expect(lastPlanRequest().options['targetSizeBytes']).toBe(524288);
  }));

  it('blocks the run and the plan for an invalid target', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    component.onTargetInput(inputEvent('0'));
    tick(200);
    flushMicrotasks();

    expect(component.targetValid()).toBeFalse();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Enter a target size greater than 0 MB.',
    );
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('omits empty optional values and sends provided ones', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    expect(lastPlanRequest().options['maxDimension']).toBeUndefined();
    expect(lastPlanRequest().options['quality']).toBeUndefined();

    component.onMaxDimensionInput(inputEvent('1600'));
    component.onQualityInput(inputEvent('80'));
    tick(200);
    flushMicrotasks();

    let request = lastPlanRequest();
    expect(request.options['maxDimension']).toBe(1600);
    expect(request.options['quality']).toBe(80);

    component.resetAdvanced();
    tick(200);
    flushMicrotasks();

    request = lastPlanRequest();
    expect(request.options['maxDimension']).toBeUndefined();
    expect(request.options['quality']).toBeUndefined();
    expect(request.options['stripMetadata']).toBeTrue();
  }));

  it('clamps the quality ceiling to the supported range', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onQualityInput(inputEvent('10'));
    tick(200);
    flushMicrotasks();
    expect(component.quality()).toBe(40);

    component.onQualityInput(inputEvent('120'));
    tick(200);
    flushMicrotasks();
    expect(component.quality()).toBe(95);
    expect(lastPlanRequest().options['quality']).toBe(95);
  }));

  it('rejects an extreme target that would break the byte conversion', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    component.onTargetInput(inputEvent('1e308'));
    tick(200);
    flushMicrotasks();

    expect(component.targetMb()).toBe(1e308);
    expect(component.targetSizeBytes()).toBe(0);
    expect(component.targetValid()).toBeFalse();
    expect(component.targetLabel()).toBe('No target');
    expect(component.targetSummary()).toBe('');
    expect(component.targetError()).toBe(
      'Enter a target size of 1,000,000 MB or less.',
    );
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Enter a target size of 1,000,000 MB or less.',
    );
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('sends format, downscale and metadata toggles', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onFormatChange(inputEvent('webp'));
    component.onAllowDownscaleToggle(checkedEvent(false));
    component.onStripMetadataToggle(checkedEvent(false));
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['format']).toBe('webp');
    expect(request.options['allowDownscale']).toBeFalse();
    expect(request.options['stripMetadata']).toBeFalse();
  }));

  it('runs, polls and reports the terminal result with sizes', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_compressed.webp';
    const inputBytes = 2 * 1024 * 1024;
    const outputBytes = 400 * 1024;

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-2', inputPath, outputPath),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, path === outputPath ? outputBytes : inputBytes),
        ),
      }),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.image.compress');
    expect(request.options['targetSizeBytes']).toBe(1024 * 1024);

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(outputBytes);
    expect(component.savingsSummary()).toBe('Saved 1.6 MB (80%)');
    expect(component.targetOutcome()).toBe(
      'The output is at or under the 1 MB target.',
    );

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Saved 1.6 MB (80%)');
    expect(fixture.nativeElement.textContent).toContain(
      'The output is at or under the 1 MB target.',
    );

    component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('reports the target captured when the job started', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_compressed.webp';

    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-target', inputPath, outputPath),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, path === outputPath ? 400 * 1024 : 2 * 1024 * 1024),
        ),
      }),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(component.runTargetBytes()).toBe(1024 * 1024);
    expect(component.targetOutcome()).toBe(
      'The output is at or under the 1 MB target.',
    );

    // A later setting change must not rewrite the report of the finished job.
    component.onTargetInput(inputEvent('5'));
    tick(200);
    flushMicrotasks();

    expect(component.targetSizeBytes()).toBe(5 * 1024 * 1024);
    expect(component.targetOutcome()).toBe(
      'The output is at or under the 1 MB target.',
    );
  }));

  it('reports a best-effort miss when no output reaches the target', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_compressed.png';
    const outputBytes = 3 * 1024 * 1024;

    wailsSpy.getJobStatus.and.resolveTo(
      terminalStatus('job-miss', inputPath, outputPath),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, path === outputPath ? outputBytes : 4 * 1024 * 1024),
        ),
      }),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(component.targetOutcome()).toBe(
      'No output reached the 1 MB target; the files are the smallest the encoder could produce.',
    );
  }));

  it('rejects unsupported extensions with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/one.heic']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "1 file wasn't added. HEIC isn't supported.",
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('uses the chosen output folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(lastPlanRequest().outputDir).toBe('/tmp/out');
  }));

  it('consumes queued drop intake paths on init', fakeAsync(() => {
    TestBed.inject(DropIntakeService).queue('/image-compress', [
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);

    const queuedFixture = TestBed.createComponent(ImageCompress);
    queuedFixture.detectChanges();
    flushMicrotasks();

    expect(queuedFixture.componentInstance.selectedPaths()).toEqual([
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);
    expect(wailsSpy.planJob).toHaveBeenCalled();

    queuedFixture.destroy();
  }));
});
