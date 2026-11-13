import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
  ComponentFixture,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageConverter } from './image-converter';
import { IMAGE_PRESETS } from '../../config/image-presets';
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
      outputs: request.inputPaths.map((inputPath) => {
        const extension = format === 'auto'
          ? (inputPath.split('.').pop() ?? 'png').toLowerCase()
          : format === 'jpeg'
            ? 'jpeg'
            : format;
        return {
          inputPath,
          outputPath: inputPath.replace(/\.[^./\\]+$/, `.${extension}`),
          outputKind: 'file',
          exists: false,
          adjusted: false,
        };
      }),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string, sizeBytes = 2048): FileInfoV1 {
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

  function lastPlanRequest(): JobRequestV1 {
    return wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
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
        files: paths.map((path) => fileInfo(path)),
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
    expect(request.options['width']).toBeUndefined();
    expect(request.options['longestEdge']).toBeUndefined();
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();
    expect(request.options['flipV']).toBeUndefined();
    expect(request.options['stripMetadata']).toBeUndefined();
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

  it('keeps each input format with the Original preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.selectPreset('original');
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['format']).toBe('auto');
    expect(request.options['quality']).toBe(85);
    expect(component.format()).toBe('auto');
    expect(component.outputBadgeLabel()).toBe('Original');
  }));

  it('overrides the preset with the advanced format select', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onFormatChange(inputEvent('avif'));
    flushMicrotasks();

    expect(lastPlanRequest().options['format']).toBe('avif');
    expect(component.outputBadgeLabel()).toBe('AVIF');

    component.onFormatChange(inputEvent('tiff'));
    flushMicrotasks();

    expect(lastPlanRequest().options['format']).toBe('tiff');
    expect(component.outputBadgeLabel()).toBe('TIFF');

    component.onFormatChange(inputEvent('webp'));
    flushMicrotasks();

    expect(component.isPresetActive(IMAGE_PRESETS[1])).toBeTrue();
  }));

  it('sends longestEdge only for the longest edge resize mode', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onResizeModeChange(inputEvent('longestEdge'));
    flushMicrotasks();
    component.onLongestEdgeInput(inputEvent('1600'));
    tick(200);
    flushMicrotasks();

    let request = lastPlanRequest();
    expect(request.options['longestEdge']).toBe(1600);
    expect(request.options['width']).toBeUndefined();

    component.onResizeModeChange(inputEvent('none'));
    flushMicrotasks();

    request = lastPlanRequest();
    expect(request.options['longestEdge']).toBeUndefined();
    expect(request.options['width']).toBeUndefined();
  }));

  it('sends width only for the exact width resize mode', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onResizeModeChange(inputEvent('width'));
    flushMicrotasks();
    component.onWidthInput(inputEvent('800'));
    tick(200);
    flushMicrotasks();

    let request = lastPlanRequest();
    expect(request.options['width']).toBe(800);
    expect(request.options['longestEdge']).toBeUndefined();

    component.onResizeModeChange(inputEvent('none'));
    flushMicrotasks();

    request = lastPlanRequest();
    expect(request.options['width']).toBeUndefined();
  }));

  it('sends metadata and transform options only when enabled', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onStripMetadataToggle(checkedEvent(true));
    component.onRotateChange(inputEvent('90'));
    component.onFlipToggle('h', checkedEvent(true));
    component.onFlipToggle('v', checkedEvent(true));
    flushMicrotasks();

    let request = lastPlanRequest();
    expect(request.options['stripMetadata']).toBeTrue();
    expect(request.options['rotate']).toBe(90);
    expect(request.options['flipH']).toBeTrue();
    expect(request.options['flipV']).toBeTrue();

    component.resetAdvanced();
    tick(200);
    flushMicrotasks();

    request = lastPlanRequest();
    expect(request.options['stripMetadata']).toBeUndefined();
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();
    expect(request.options['flipV']).toBeUndefined();
    expect(request.options['format']).toBe('webp');
  }));

  it('summarizes the saved bytes after a terminal job', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo.webp';
    const inputBytes = 200 * 1024;
    const outputBytes = 136 * 1024;
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, path === outputPath ? outputBytes : inputBytes),
        ),
      }),
    );
    wailsSpy.getJobStatusV1.and.resolveTo(
      terminalStatus('job-savings', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(component.inputTotalBytes()).toBe(inputBytes);
    expect(component.outputTotalBytes()).toBe(outputBytes);
    expect(component.savingsSummary()).toBe('Saved 64 KB (32%)');

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      'Saved 64 KB (32%)',
    );
  }));

  it('reports when the output is larger than the input', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo.webp';
    const inputBytes = 200 * 1024;
    const outputBytes = 240 * 1024;
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, path === outputPath ? outputBytes : inputBytes),
        ),
      }),
    );
    wailsSpy.getJobStatusV1.and.resolveTo(
      terminalStatus('job-growth', inputPath, outputPath),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(component.savingsSummary()).toBe('Output is 40 KB larger');
  }));
});
