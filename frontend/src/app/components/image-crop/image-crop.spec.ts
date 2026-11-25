import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageCrop } from './image-crop';
import { I18nService } from '../../services/i18n.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResultItem,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('ImageCrop', () => {
  let fixture: ComponentFixture<ImageCrop>;
  let component: ImageCrop;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function fileName(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index >= 0 ? path.slice(index + 1) : path;
  }

  function fileStem(path: string): string {
    return fileName(path).replace(/\.[^./\\]+$/, '');
  }

  function outputExtension(inputPath: string, request: JobRequest): string {
    const requested = String(request.options['format'] ?? '').toLowerCase();
    if (requested) {
      return requested === 'jpg' ? 'jpeg' : requested;
    }
    const extension = (inputPath.split('.').pop() ?? 'png').toLowerCase();
    return extension === 'jpg' ? 'jpeg' : extension;
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const outputs = request.inputPaths.map((inputPath) => {
      const dir = request.outputDir || firstDir(inputPath);
      const name = `${fileStem(inputPath)}_cropped.${outputExtension(
        inputPath,
        request,
      )}`;
      return {
        inputPath,
        outputPath: dir ? `${dir}/${name}` : name,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      };
    });

    return {
      success: true,
      message: 'ok',
      outputs,
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string): FileInfo {
    const name = fileName(path);
    return {
      path,
      name,
      directory: firstDir(path),
      extension: (name.split('.').pop() ?? '').toLowerCase(),
      sizeBytes: 2048,
      kind: 'image',
      width: 100,
      height: 80,
      exists: true,
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResult['status'],
    items: JobResultItem[],
  ): JobStatusResponse {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.image.crop',
        status: jobStatus,
        progress: {
          current: jobStatus === 'running' ? 0 : items.length,
          total: items.length || 1,
          stage: jobStatus,
          message: jobStatus,
        },
        items,
        startedAt: 1,
        endedAt: jobStatus === 'running' ? undefined : 2,
      },
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

  function primaryButton(): HTMLButtonElement {
    return fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'getImagePreviewSource',
      'getImageCropPreview',
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
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.getImagePreviewSource.and.resolveTo({
      success: true,
      message: 'ok',
      dataBase64: 'AA==',
      mimeType: 'image/png',
      width: 100,
      height: 80,
    });
    wailsSpy.getImageCropPreview.and.resolveTo({
      success: true,
      message: 'ok',
      dataBase64: 'AA==',
      mimeType: 'image/png',
      width: 20,
      height: 20,
    });
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'crop-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'crop-job-1',
    });
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.revealInFolder.and.resolveTo({
      success: true,
      message: 'revealed',
    });
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
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
      imports: [ImageCrop],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(ImageCrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('plans one image as single with the default crop options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.crop');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/photo.png']);
    expect(request.outputDir).toBe('');
    expect(request.options['x']).toBe(0);
    expect(request.options['y']).toBe(0);
    expect(request.options['width']).toBe(100);
    expect(request.options['height']).toBe(80);
    expect(request.options['ratioPreset']).toBe('free');
    expect(request.options['format']).toBeUndefined();
    expect(request.options['outputWidth']).toBeUndefined();
    expect(request.options['outputHeight']).toBeUndefined();
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();
    expect(request.options['flipV']).toBeUndefined();

    expect(component.previewUrl()).toContain('data:image/png;base64,');
    expect(component.imageWidth()).toBe(100);
    expect(component.imageHeight()).toBe(80);
    expect(component.cropSizeLabel()).toBe('100 × 80 px');
    expect(component.imageSizeLabel()).toBe('100 × 80 px');
    expect(component.primaryLabel()).toBe('Crop image');
    expect(component.canRun()).toBeTrue();

    fixture.detectChanges();
    expect(primaryButton().disabled).toBeFalse();
  }));

  it('plans a batch automatically for two images', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    component.onPathsSelected(['/tmp/b.jpg']);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.jpg']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.isBatch()).toBeTrue();
    expect(component.primaryLabel()).toBe('Crop 2 images');

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      'The same crop applies to every image.',
    );
  }));

  it('changes the crop and plan options when a ratio preset is selected', fakeAsync(() => {
    wailsSpy.getImagePreviewSource.and.resolveTo({
      success: true,
      message: 'ok',
      dataBase64: 'AA==',
      mimeType: 'image/png',
      width: 160,
      height: 90,
    });

    component.onPathsSelected(['/tmp/wide.png']);
    flushMicrotasks();

    component.selectRatio('4:3');
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.options['ratioPreset']).toBe('4:3');
    expect(request.options['width']).toBe(120);
    expect(request.options['height']).toBe(90);
    expect(
      (request.options['width'] as number) * 3,
    ).toBe((request.options['height'] as number) * 4);
    expect(component.cropSizeLabel()).toBe('120 × 90 px');

    const active = fixture.nativeElement.querySelector(
      '[data-ratio="4:3"]',
    ) as HTMLButtonElement;
    const free = fixture.nativeElement.querySelector(
      '[data-ratio="free"]',
    ) as HTMLButtonElement;
    expect(active.getAttribute('aria-pressed')).toBe('true');
    expect(free.getAttribute('aria-pressed')).toBe('false');
  }));

  it('debounces bounds edits before planning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    component.onBoundsInput('width', inputEvent('90'));
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);

    tick(199);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls + 1);
    expect(lastPlanRequest().options['width']).toBe(90);
  }));

  it('adds the chosen output format and omits it for preserve original', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    component.onFormatChange(inputEvent('webp'));
    flushMicrotasks();
    expect(component.format()).toBe('webp');
    expect(lastPlanRequest().options['format']).toBe('webp');

    component.onFormatChange(inputEvent(''));
    flushMicrotasks();
    expect(component.format()).toBe('');
    expect(lastPlanRequest().options['format']).toBeUndefined();
  }));

  it('adds output size, rotation and flips to the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    component.onOutputSizeInput('width', inputEvent('240'));
    component.onOutputSizeInput('height', inputEvent('160'));
    tick(200);
    flushMicrotasks();

    let request = lastPlanRequest();
    expect(request.options['outputWidth']).toBe(240);
    expect(request.options['outputHeight']).toBe(160);
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();

    component.onRotateChange(inputEvent('90'));
    component.onFlipToggle('h', checkedEvent(true));
    component.onFlipToggle('v', checkedEvent(true));
    flushMicrotasks();

    request = lastPlanRequest();
    expect(request.options['rotate']).toBe(90);
    expect(request.options['flipH']).toBeTrue();
    expect(request.options['flipV']).toBeTrue();
    expect(request.options['outputWidth']).toBe(240);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      'Transforms apply after cropping. Preview shows the crop.',
    );
  }));

  it('omits empty output sizes and disabled transforms', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    component.onOutputSizeInput('width', inputEvent('0'));
    component.onOutputSizeInput('height', inputEvent(''));
    tick(200);
    flushMicrotasks();

    component.onRotateChange(inputEvent('0'));
    component.onFlipToggle('h', checkedEvent(false));
    component.onFlipToggle('v', checkedEvent(false));
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['outputWidth']).toBeUndefined();
    expect(request.options['outputHeight']).toBeUndefined();
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();
    expect(request.options['flipV']).toBeUndefined();
  }));

  it('reset transforms clears the crop transform options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    component.onOutputSizeInput('width', inputEvent('240'));
    component.onOutputSizeInput('height', inputEvent('160'));
    component.onRotateChange(inputEvent('270'));
    component.onFlipToggle('h', checkedEvent(true));
    component.onFlipToggle('v', checkedEvent(true));
    tick(200);
    flushMicrotasks();

    component.resetTransforms();
    flushMicrotasks();

    expect(component.outputWidth()).toBeNull();
    expect(component.outputHeight()).toBeNull();
    expect(component.rotate()).toBe(0);
    expect(component.flipH()).toBeFalse();
    expect(component.flipV()).toBeFalse();

    const request = lastPlanRequest();
    expect(request.options['outputWidth']).toBeUndefined();
    expect(request.options['outputHeight']).toBeUndefined();
    expect(request.options['rotate']).toBeUndefined();
    expect(request.options['flipH']).toBeUndefined();
    expect(request.options['flipV']).toBeUndefined();
  }));

  it('blocks the action for an empty or out-of-bounds crop', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    component.onBoundsInput('width', inputEvent(''));
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.crop()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(primaryButton().disabled).toBeTrue();
    expect(component.actionHelper()).toContain('Enter whole numbers');
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);

    component.onBoundsInput('width', inputEvent('500'));
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(primaryButton().disabled).toBeTrue();
    expect(component.actionHelper()).toContain('stay inside the image');
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls);
  }));

  it('shows a failed plan inline and blocks the action', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    wailsSpy.planJob.and.resolveTo({
      success: false,
      message: 'The folder is not available.',
      outputs: [],
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'IMAGE_CROP_OUTPUT_DIR_INVALID',
        message: 'The folder is not available.',
      },
    });

    component.selectRatio('1:1');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.planBlocked()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(primaryButton().disabled).toBeTrue();
    expect(
      fixture.nativeElement.querySelector('.output-plan-error'),
    ).not.toBeNull();
  }));

  it('runs from the fresh plan without a validation step', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png', '/tmp/b.jpg']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJob.calls.count()).toBe(planCalls + 1);
    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.image.crop');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.jpg']);
    expect(request.options['ratioPreset']).toBe('free');
    expect(wailsSpy.validateJob).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('crop-job-1');

    component.reset();
  }));

  it('inspects terminal results and opens the cropped file', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_cropped.png';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'crop-job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('crop-job-2', 'success', [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          outputCount: 1,
          success: true,
          message: 'image crop successful',
        },
      ]),
    );

    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(fixture.nativeElement.textContent).toContain('1 image cropped');

    const inspectedPaths = wailsSpy.inspectFiles.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) => paths.includes(outputPath)),
    ).toBeTrue();

    const openButton = fixture.nativeElement.querySelector(
      '.result-open',
    ) as HTMLButtonElement;
    openButton.click();
    flushMicrotasks();

    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('retries only failed inputs with the same crop settings', fakeAsync(() => {
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'crop-retry-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('crop-retry-1', 'partial_success', [
        {
          inputPath: '/tmp/a.png',
          outputPath: '/tmp/a_cropped.png',
          outputs: ['/tmp/a_cropped.png'],
          success: true,
          message: 'image crop successful',
        },
        {
          inputPath: '/tmp/b.jpg',
          outputPath: '',
          success: false,
          message: 'image crop failed',
          error: {
            code: 'EXEC_IO_TRANSIENT',
            detail_code: 'IMAGE_CROP_BATCH_ITEM',
            message: 'image crop failed',
          },
        },
      ]),
    );

    component.onPathsSelected(['/tmp/a.png', '/tmp/b.jpg']);
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');
    void component.chooseOutputFolder();
    flushMicrotasks();

    component.onFormatChange(inputEvent('png'));
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('partial_success');

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'crop-retry-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('crop-retry-2', 'running', []),
    );

    void component.retryFailed();
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.jpg']);

    const runCalls = wailsSpy.runJob.calls.allArgs();
    const retryRequest = runCalls[runCalls.length - 1][0] as JobRequest;
    expect(retryRequest.mode).toBe('single');
    expect(retryRequest.inputPaths).toEqual(['/tmp/b.jpg']);
    expect(retryRequest.outputDir).toBe('/tmp/out');
    expect(retryRequest.options['format']).toBe('png');
    expect(retryRequest.options['x']).toBe(0);
    expect(retryRequest.options['y']).toBe(0);
    expect(retryRequest.options['width']).toBe(100);
    expect(retryRequest.options['height']).toBe(80);
    expect(retryRequest.options['ratioPreset']).toBe('free');

    component.reset();
  }));

  it('uses the chosen destination folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls + 1);
    expect(lastPlanRequest().outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/photo_cropped.png',
    );

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each image');
    expect(lastPlanRequest().outputDir).toBe('');
  }));

  it('rejects non-images and duplicates with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toContain('notes.txt');
    expect(component.notice()?.text).toContain("isn't an image");
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    component.onPathsSelected(['/tmp/a.png', '/tmp/a.png']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.png']);
    expect(component.notice()?.text).toContain(
      'a.png is already in the queue.',
    );
    expect(component.fileItems().length).toBe(1);
  }));

  it('renders the file-first page without the legacy mode and path controls', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.eyebrow')?.textContent).toContain('IMAGE');
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Image',
    );
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Crop',
    );
    expect(element.querySelectorAll('.page-head a').length).toBe(0);

    expect(element.querySelector('#crop-mode')).toBeNull();
    expect(element.querySelector('#outputPath')).toBeNull();
    expect(element.querySelector('app-tool-execution-panel')).toBeNull();

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['validate']).toBeUndefined();
    expect(legacy['panelFields']).toBeUndefined();
  });

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Recortar');
    expect(title).toContain('imágenes');
    expect(
      element.querySelector('[data-ratio="1:1"] .preset-hint')?.textContent,
    ).toContain('Cuadrada');
    expect(
      element.querySelector('.toolbar .btn-secondary')?.textContent,
    ).toContain('Elegir archivos');
  });
});
