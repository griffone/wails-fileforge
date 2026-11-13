import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
  ComponentFixture,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageConverter } from './image-converter';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('ImageConverter', () => {
  let fixture: ComponentFixture<ImageConverter>;
  let component: ImageConverter;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const format = String(request.options['format'] ?? 'webp');
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, `.${format}`),
        outputKind: 'file',
        exists: false,
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
      kind: 'image',
      width: 640,
      height: 480,
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponseV1 {
    const result: JobResultV1 = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.image.convert',
      status: 'running',
      progress: {
        current: 0,
        total: 1,
        stage: 'running',
        message: 'Working on your image',
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
      toolId: 'tool.image.convert',
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
          message: 'conversion successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJobV1',
      'inspectFilesV1',
      'getThumbnail',
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
    wailsSpy.getThumbnail.and.resolveTo(null);
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
      imports: [ImageConverter],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(ImageConverter);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans a single image with the default WebP preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.jpg']);
    flushMicrotasks();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.image.convert');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/photo.jpg']);
    expect(request.outputDir).toBe('');
    expect(request.options['format']).toBe('webp');
    expect(request.options['quality']).toBe(80);
    expect(component.fileItems()[0].name).toBe('photo.jpg');
    expect(component.fileItems()[0].meta).toBe('2 KB · 640 × 480');
  }));

  it('plans a batch and labels the primary action with the count', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png', '/tmp/b.jpg']);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.jpg']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.primaryLabel()).toBe('Convert 2 images');
  }));

  it('sends the JPEG preset format and quality', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.selectPreset('jpeg');
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['format']).toBe('jpeg');
    expect(request.options['quality']).toBe(85);
    expect(component.primaryLabel()).toBe('Convert 1 image');
  }));

  it('runs directly from the plan without a validate step', fakeAsync(() => {
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-1',
      status: 'queued',
    });

    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.image.convert');
    expect(request.options['format']).toBe('webp');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');
    fixture.destroy();
  }));

  it('shows the terminal result, loads output sizes and opens files', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo.webp';

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

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) => paths.includes(outputPath)),
    ).toBeTrue();
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    component.onOpen(outputPath);
    flushMicrotasks();

    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('rejects unsupported extensions with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/one.heic', '/tmp/two.heic']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.fileItems()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "2 files weren't added. HEIC isn't supported.",
    );
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));

  it('uses the chosen output folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    const callsBefore = wailsSpy.planJobV1.calls.count();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(component.destinationLabel()).toBe('/tmp/out');

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(wailsSpy.planJobV1.calls.count()).toBe(callsBefore + 1);
  }));

  it('adds the advanced max width to the plan options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onMaxWidthInput({
      target: { value: '1200' },
    } as unknown as Event);
    tick(200);
    flushMicrotasks();

    expect(component.maxWidth()).toBe(1200);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['width']).toBe(1200);
  }));
});
