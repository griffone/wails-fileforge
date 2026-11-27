import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageWatermark } from './image-watermark';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  ImagePreviewSourceResponse,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

const PIXEL_PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

describe('ImageWatermark', () => {
  let fixture: ComponentFixture<ImageWatermark>;
  let component: ImageWatermark;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_watermarked.png'),
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
      width: 800,
      height: 600,
      exists: true,
    };
  }

  function previewResponse(): ImagePreviewSourceResponse {
    return {
      success: true,
      message: 'ok',
      dataBase64: PIXEL_PNG,
      mimeType: 'image/png',
      width: 800,
      height: 600,
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
      toolId: 'tool.image.watermark',
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
          message: 'text watermark at bottom-right (font 30 px, 800×600)',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'getImagePreviewSource',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openFileDialog',
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
    wailsSpy.getThumbnail.and.resolveTo(`data:image/png;base64,${PIXEL_PNG}`);
    wailsSpy.getImagePreviewSource.and.callFake((path: string) =>
      Promise.resolve(
        path === '/tmp/logo-small.png'
          ? {
              success: true,
              message: 'ok',
              dataBase64: PIXEL_PNG,
              mimeType: 'image/png',
              width: 100,
              height: 50,
            }
          : previewResponse(),
      ),
    );
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
        toolId: 'tool.image.watermark',
        status: 'running',
        progress: {
          current: 0,
          total: 1,
          stage: 'running',
          message: 'watermarking 1/1',
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
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'unavailable',
      outputs: [],
    });

    await TestBed.configureTestingModule({
      imports: [ImageWatermark],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(ImageWatermark);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('starts with the documented defaults', () => {
    expect(component.kind()).toBe('text');
    expect(component.text()).toBe('');
    expect(component.logoPath()).toBe('');
    expect(component.anchor()).toBe('bottom-right');
    expect(component.marginPercent()).toBe(3);
    expect(component.scalePercent()).toBe(5);
    expect(component.opacity()).toBe(0.6);
    expect(component.color()).toBe('#FFFFFF');
    expect(component.bold()).toBeTrue();
    expect(component.tile()).toBeFalse();
    expect(component.rotationDegrees()).toBe(-30);
    expect(component.format()).toBe('auto');
    expect(component.quality()).toBe(92);
    expect(component.activeAnchor().label).toBe('Bottom right');
    expect(component.canRun()).toBeFalse();
  });

  it('sends the text watermark request shape', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    // Empty text is invalid, so no plan request is sent yet.
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    component.onTextInput(inputEvent('Confidential'));
    tick(200);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.watermark');
    expect(request.mode).toBe('single');
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({
      kind: 'text',
      anchor: 'bottom-right',
      marginPercent: 3,
      scalePercent: 5,
      opacity: 0.6,
      tile: false,
      format: 'auto',
      quality: 92,
      text: 'Confidential',
      color: '#FFFFFF',
      bold: true,
    });
    expect(request.options['logoPath']).toBeUndefined();
    expect(request.options['rotationDegrees']).toBeUndefined();
  }));

  it('maps the logo request without text-only options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onLogoSelected(['/tmp/logo.png']);
    flushMicrotasks();

    expect(component.kind()).toBe('logo');
    expect(component.actionHelper()).toBe('');
    const request = lastPlanRequest();
    expect(request.options['kind']).toBe('logo');
    expect(request.options['logoPath']).toBe('/tmp/logo.png');
    expect(request.options['scalePercent']).toBe(20);
    expect(request.options['text']).toBeUndefined();
    expect(request.options['color']).toBeUndefined();
    expect(request.options['bold']).toBeUndefined();
    expect(request.options['rotationDegrees']).toBeUndefined();
  }));

  it('updates the anchor in the request and the overlay', fakeAsync(() => {
    component.onTextInput(inputEvent('Draft'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    expect(component.imageWidth()).toBe(800);
    expect(component.imageHeight()).toBe(600);

    component.setAnchor('top-left');
    flushMicrotasks();

    expect(lastPlanRequest().options['anchor']).toBe('top-left');
    // 3% of the 600 px short edge = 18 px; the block starts at the anchor.
    expect(component.textPreview()?.x).toBe(18);
    expect(component.textPreview()?.anchor).toBe('start');
    expect(component.activeAnchor().label).toBe('Top left');

    component.setAnchor('center');
    flushMicrotasks();

    expect(lastPlanRequest().options['anchor']).toBe('center');
    expect(component.textPreview()?.x).toBe(400);
    expect(component.textPreview()?.anchor).toBe('middle');

    component.setAnchor('bottom-right');
    flushMicrotasks();

    expect(component.textPreview()?.x).toBe(782);
    expect(component.textPreview()?.anchor).toBe('end');
  }));

  it('sends rotation only for tiled text and disables logo mode', fakeAsync(() => {
    component.onTextInput(inputEvent('Sample'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    expect(lastPlanRequest().options['rotationDegrees']).toBeUndefined();

    component.onTileToggle(checkedEvent(true));
    flushMicrotasks();

    expect(component.tile()).toBeTrue();
    expect(lastPlanRequest().options['tile']).toBeTrue();
    expect(lastPlanRequest().options['rotationDegrees']).toBe(-30);
    expect(component.tilePreview()).not.toBeNull();
    // The backend rotates counter-clockwise for positive angles while SVG
    // rotates clockwise, so the preview negates the angle.
    expect(component.tilePreview()?.transform).toContain('rotate(30 ');

    component.onRotationInput(inputEvent('45'));
    tick(200);
    flushMicrotasks();
    expect(lastPlanRequest().options['rotationDegrees']).toBe(45);
    expect(component.tilePreview()?.transform).toContain('rotate(-45 ');

    fixture.detectChanges();
    const logoButton = Array.from(
      fixture.nativeElement.querySelectorAll(
        '.segmented .chip',
      ) as NodeListOf<HTMLButtonElement>,
    ).find((button) => button.textContent?.trim() === 'Logo');
    expect(logoButton?.disabled).toBeTrue();

    component.setKind('logo');
    flushMicrotasks();
    expect(component.tile()).toBeFalse();
    expect(component.tilePreview()).toBeNull();
  }));

  it('loads the first image and the logo thumbnail for the preview', fakeAsync(() => {
    component.onPathsSelected(['/tmp/first.png', '/tmp/second.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    expect(wailsSpy.getImagePreviewSource).toHaveBeenCalledOnceWith(
      '/tmp/first.png',
    );
    expect(component.previewUrl()).toContain('data:image/png;base64,');
    expect(component.imageWidth()).toBe(800);
    expect(component.imageHeight()).toBe(600);

    component.onLogoSelected(['/tmp/logo.png']);
    flushMicrotasks();

    expect(wailsSpy.getThumbnail).toHaveBeenCalledWith('/tmp/logo.png', 256);
    expect(wailsSpy.getImagePreviewSource).toHaveBeenCalledWith(
      '/tmp/logo.png',
    );
    expect(component.logoThumbnailUrl()).toContain('data:image/png;base64,');
    expect(component.logoNaturalWidth()).toBe(800);
    expect(component.logoPreview()?.width).toBe(160);
  }));

  it('caps the preview logo at its natural width instead of upscaling it', fakeAsync(() => {
    component.onPathsSelected(['/tmp/first.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    component.onLogoSelected(['/tmp/logo-small.png']);
    flushMicrotasks();

    expect(component.logoNaturalWidth()).toBe(100);
    expect(component.logoAspect()).toBe(2);
    expect(component.logoPreview()?.width).toBe(100);
    expect(component.logoPreview()?.height).toBe(50);

    // 60% of the 800 px image is 480 px, still above the natural 100 px.
    component.onScaleInput(inputEvent('60'));
    tick(200);
    flushMicrotasks();
    expect(component.logoPreview()?.width).toBe(100);

    // 5% of the image is 40 px, below the natural width, so it scales down.
    component.onScaleInput(inputEvent('5'));
    tick(200);
    flushMicrotasks();
    expect(component.logoPreview()?.width).toBe(40);
    expect(component.logoPreview()?.height).toBe(20);
  }));

  it('left-aligns multi-line text at one block edge', fakeAsync(() => {
    component.onTextInput(inputEvent('WIDE LINE\nX'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    const preview = component.textPreview();
    expect(preview?.fontSize).toBe(30);
    expect(preview?.anchor).toBe('start');
    // 9 chars * 30 px * 0.58 = 156.6 px estimated block width, left of the
    // bottom-right anchor at 782 px.
    expect(preview?.x).toBeCloseTo(625.4, 1);
    expect(preview?.lines.map((line) => line.text)).toEqual(['WIDE LINE', 'X']);
  }));

  it('applies the backend 12 px font floor to the preview', fakeAsync(() => {
    component.onTextInput(inputEvent('Hi'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    component.imageWidth.set(100);
    component.imageHeight.set(100);
    flushMicrotasks();

    expect(component.textPreview()?.fontSize).toBe(12);

    component.onTileToggle(checkedEvent(true));
    flushMicrotasks();
    expect(component.tilePreview()?.fontSize).toBe(12);
  }));

  it('blocks the run while the text is empty or too long', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe('Enter the watermark text.');
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();

    component.onTextInput(inputEvent('x'.repeat(201)));
    tick(200);
    flushMicrotasks();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Text must be at most 200 characters.',
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('blocks the run without a valid logo', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.setKind('logo');
    flushMicrotasks();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe('Choose a logo image.');

    component.onLogoSelected(['/tmp/logo.txt']);
    flushMicrotasks();

    expect(component.logoPath()).toBe('');
    expect(component.logoError()).toBe('Use a PNG, JPEG or WebP logo.');
    expect(component.canRun()).toBeFalse();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('blocks the run when the backend rejects the plan', fakeAsync(() => {
    wailsSpy.planJob.and.resolveTo({
      success: false,
      message: 'The watermark text contains an unsupported character.',
      outputs: [],
      effectiveOptions: {},
      warnings: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'IMAGE_WATERMARK_TEXT_UNSUPPORTED',
        message: 'The watermark text contains an unsupported character.',
      },
    });

    component.onPathsSelected(['/tmp/a.png']);
    component.onTextInput(inputEvent('Draft'));
    tick(200);
    flushMicrotasks();

    expect(component.planBlocked()).toBeTrue();
    expect(component.plan()?.error?.message).toBe(
      'The watermark text contains an unsupported character.',
    );
    expect(component.canRun()).toBeFalse();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('runs, observes and reports the watermarked result', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_watermarked.png';

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
    component.onTextInput(inputEvent('Draft'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['text']).toBe('Draft');
    expect(request.options['kind']).toBe('text');

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 image watermarked');

    component.onReveal(outputPath);
    flushMicrotasks();
    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith(outputPath);
  }));

  it('updates the margin, scale, opacity and color options', fakeAsync(() => {
    component.onTextInput(inputEvent('Draft'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onMarginInput(inputEvent('5'));
    component.onScaleInput(inputEvent('8'));
    component.onOpacityInput(inputEvent('0.25'));
    component.onColorInput(inputEvent('#ff8800'));
    component.onBoldToggle(checkedEvent(false));
    tick(200);
    flushMicrotasks();

    const options = lastPlanRequest().options;
    expect(options['marginPercent']).toBe(5);
    expect(options['scalePercent']).toBe(8);
    expect(options['opacity']).toBe(0.25);
    expect(options['color']).toBe('#ff8800');
    expect(options['bold']).toBeFalse();
  }));

  it('clamps out-of-range numeric settings', fakeAsync(() => {
    component.onTextInput(inputEvent('Draft'));
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();

    component.onMarginInput(inputEvent('1'));
    component.onScaleInput(inputEvent('40'));
    component.onOpacityInput(inputEvent('0'));
    component.onRotationInput(inputEvent('-200'));
    component.onQualityInput(inputEvent('5'));
    tick(200);
    flushMicrotasks();

    const options = lastPlanRequest().options;
    expect(options['marginPercent']).toBe(2);
    expect(options['scalePercent']).toBe(8);
    expect(options['opacity']).toBe(0.05);
    expect(component.rotationDegrees()).toBe(-90);
    expect(options['quality']).toBe(40);
  }));

  it('rejects unsupported extensions with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/one.heic']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "one.heic wasn't added because it isn't an image.",
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('consumes queued drop intake paths on init', fakeAsync(() => {
    TestBed.inject(DropIntakeService).queue('/image-watermark', [
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);

    const queuedFixture = TestBed.createComponent(ImageWatermark);
    queuedFixture.componentInstance.onTextInput(inputEvent('Queued'));
    queuedFixture.detectChanges();
    flushMicrotasks();
    tick(150);
    flushMicrotasks();

    expect(queuedFixture.componentInstance.selectedPaths()).toEqual([
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);
    expect(wailsSpy.getImagePreviewSource).toHaveBeenCalledWith(
      '/tmp/queued-one.png',
    );

    queuedFixture.destroy();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Añadir');
    expect(title).toContain('marca de agua');
    expect(
      element.querySelector('.segmented .chip')?.textContent,
    ).toContain('Texto');
    expect(
      element.querySelector('.action-primary')?.textContent,
    ).toContain('Aplicar marca de agua');
  });
});
