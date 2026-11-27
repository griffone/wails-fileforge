import {
  ComponentFixture,
  discardPeriodicTasks,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EMPTY, Subject } from 'rxjs';

import { ImageUpscale } from './image-upscale';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobRequest,
  JobResultItem,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  ToolchainStatus,
  Wails,
} from '../../services/wails';

describe('ImageUpscale', () => {
  let fixture: ComponentFixture<ImageUpscale>;
  let component: ImageUpscale;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobStateSubject: Subject<JobResult>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function readyToolchain(
    overrides: Partial<ToolchainStatus> = {},
  ): ToolchainStatus {
    return {
      success: true,
      message: 'ok',
      ready: true,
      installing: false,
      percent: 100,
      storageDir: '/home/user/.local/share/fileforge/toolchain',
      components: [
        {
          id: 'upscale',
          label: 'Upscale engine',
          ready: true,
          required: false,
          detail: 'Local upscale engine',
        },
        {
          id: 'upscale-model',
          label: 'Photo upscale model',
          ready: true,
          required: false,
          standard: true,
          detail: 'Local photo upscale model',
        },
        {
          id: 'upscale-anime-model',
          label: 'Illustration upscale model',
          ready: true,
          required: false,
          standard: true,
          detail: 'Local illustration upscale model',
        },
      ],
      model: 'small',
      ocrLanguages: [],
      ...overrides,
    };
  }

  function missingToolchain(
    overrides: Partial<ToolchainStatus> = {},
  ): ToolchainStatus {
    return readyToolchain({
      ready: false,
      components: [
        {
          id: 'upscale',
          label: 'Upscale engine',
          ready: false,
          required: false,
          detail: 'The upscale engine is not installed. Run Install / repair.',
        },
        {
          id: 'upscale-model',
          label: 'Photo upscale model',
          ready: false,
          required: false,
          standard: true,
          detail: 'The photo upscale model is not installed. Run Install / repair to download it.',
        },
        {
          id: 'upscale-anime-model',
          label: 'Illustration upscale model',
          ready: false,
          required: false,
          standard: true,
          detail:
            'The illustration upscale model is not installed. Run Install / repair to download it.',
        },
      ],
      ...overrides,
    });
  }

  function fileInfo(path: string, sizeBytes = 2048): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
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

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const scale = Number(request.options['scale'] ?? 2);
    return {
      success: true,
      message: 'Ready to upscale 1 image 2x.',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(
          /\.[^./\\]+$/,
          `_${scale}x.png`,
        ),
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
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
        toolId: 'tool.image.upscale',
        status: jobStatus,
        progress: {
          current: items.length,
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

  function create(status: ToolchainStatus = readyToolchain()): void {
    wailsSpy.getToolchainStatus.and.resolveTo(status);
    fixture = TestBed.createComponent(ImageUpscale);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'getToolchainStatus',
      'getToolchainInstallState',
      'startToolchainInstall',
      'cancelToolchainInstall',
      'inspectFiles',
      'getThumbnail',
      'planJob',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openDirectoryDialog',
      'openMultipleFilesDialog',
      'openPath',
      'revealInFolder',
      'verifyJob',
    ]);
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ = EMPTY;
    (wailsSpy as unknown as { toolchainInstall$: unknown }).toolchainInstall$ =
      EMPTY;
    jobStateSubject = new Subject<JobResult>();
    (wailsSpy as unknown as { jobState$: unknown }).jobState$ =
      jobStateSubject.asObservable();

    wailsSpy.getToolchainStatus.and.resolveTo(readyToolchain());
    wailsSpy.getToolchainInstallState.and.resolveTo(readyToolchain());
    wailsSpy.startToolchainInstall.and.resolveTo(missingToolchain());
    wailsSpy.cancelToolchainInstall.and.resolveTo(missingToolchain());
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => fileInfo(path)),
      }),
    );
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'upscale-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'upscale-job-1',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });
    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'unavailable',
      outputs: [],
    });

    await TestBed.configureTestingModule({
      imports: [ImageUpscale],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
  });

  afterEach(() => {
    fixture?.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('plans with the default general 2x options', fakeAsync(() => {
    create();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packReady()).toBeTrue();
    expect(component.packBadge()).toBe('Pack ready');

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.upscale');
    expect(request.mode).toBe('single');
    expect(request.options).toEqual({
      scale: 2,
      model: 'general',
      tileSize: 128,
      format: 'png',
    });
    expect(component.canRun()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'One download, then offline',
    );
  }));

  it('blocks plan and run when the pack is missing and links to Settings', fakeAsync(() => {
    create(missingToolchain());
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packReady()).toBeFalse();
    expect(component.packBanner()?.title).toBe(
      'The upscale pack is not installed.',
    );
    expect(component.packBanner()?.detail).toContain('about 73 MB');

    const link = query<HTMLAnchorElement>('.pack-settings');
    expect(link?.getAttribute('href')).toBe('/settings');

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();

    expect(wailsSpy.planJob).not.toHaveBeenCalled();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'The upscale engine or the selected model is not installed. Use Install upscale pack above.',
    );
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();
  }));

  it('uses the bundled illustration model without a user file', fakeAsync(() => {
    create();
    flushMicrotasks();
    fixture.detectChanges();

    component.onModelChange(inputEvent('anime'));
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.modelReady()).toBeTrue();
    expect(component.packBanner()).toBeNull();
    expect(fixture.nativeElement.textContent).toContain(
      'The illustration model is included in the pack.',
    );

    component.onPathsSelected(['/tmp/in/art.png']);
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['model']).toBe('anime');
    expect(request.options['modelPath']).toBeUndefined();
    expect(component.canRun()).toBeTrue();
  }));

  it('reports a missing illustration model only when no override is set', fakeAsync(() => {
    const status = readyToolchain({
      components: [
        {
          id: 'upscale',
          label: 'Upscale engine',
          ready: true,
          required: false,
          standard: true,
          detail: 'Local upscale engine',
        },
        {
          id: 'upscale-anime-model',
          label: 'Illustration upscale model',
          ready: false,
          required: false,
          standard: true,
          detail:
            'The illustration upscale model is not installed. Run Install / repair to download it.',
        },
      ],
    });
    create(status);
    flushMicrotasks();
    fixture.detectChanges();

    component.onModelChange(inputEvent('anime'));
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.modelReady()).toBeFalse();
    expect(component.packBanner()?.title).toBe(
      'The upscale pack needs repair.',
    );

    component.onPathsSelected(['/tmp/in/art.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
    expect(component.actionHelper()).toBe(
      'The illustration model is missing. Use Install upscale pack above.',
    );

    // A custom override is an advanced option: it satisfies the choice even
    // when the managed illustration model is missing.
    TestBed.inject(PreferencesService).setUpscaleAnimeModelPath(
      '/models/anime.onnx',
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.modelReady()).toBeTrue();
    expect(component.packBanner()).toBeNull();
  }));

  it('sends the anime model path and quality for JPEG output', fakeAsync(() => {
    create();
    flushMicrotasks();
    TestBed.inject(PreferencesService).setUpscaleAnimeModelPath(
      '/models/anime.onnx',
    );

    component.onModelChange(inputEvent('anime'));
    component.onFormatChange(inputEvent('jpeg'));
    component.onQualityInput(inputEvent('85'));
    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options).toEqual({
      scale: 2,
      model: 'anime',
      tileSize: 128,
      format: 'jpeg',
      quality: 85,
      modelPath: '/models/anime.onnx',
    });
  }));

  it('offers the inline install, shows progress and re-plans afterwards', fakeAsync(() => {
    const missing = missingToolchain();
    const installing = missingToolchain({
      installing: true,
      percent: 12,
      phase: 'onnx',
      detail: 'Installing the shared ONNX engine…',
    });

    wailsSpy.getToolchainStatus.and.resolveTo(missing);
    wailsSpy.startToolchainInstall.and.resolveTo(installing);
    fixture = TestBed.createComponent(ImageUpscale);
    component = fixture.componentInstance;
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packReady()).toBeFalse();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    const install = query<HTMLButtonElement>('.pack-install');
    expect(install?.textContent).toContain(
      'Install upscale pack (about 22.5 MB of models)',
    );

    // The install finishes shortly after it starts.
    wailsSpy.getToolchainStatus.and.resolveTo(readyToolchain());
    wailsSpy.getToolchainInstallState.and.resolveTo(readyToolchain());

    install?.click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.startToolchainInstall).toHaveBeenCalled();
    expect(component.packInstalling()).toBeTrue();
    expect(component.packPercent()).toBe(12);
    expect(fixture.nativeElement.textContent).toContain(
      'Installing the shared ONNX engine…',
    );
    expect(query<HTMLButtonElement>('.pack-cancel')).not.toBeNull();

    // The service polls the installer, then the page re-plans.
    tick(1500);
    flushMicrotasks();
    tick(500);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packInstalling()).toBeFalse();
    expect(component.packReady()).toBeTrue();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    expect(query<HTMLButtonElement>('.pack-install')).toBeNull();
    discardPeriodicTasks();
  }));

  it('cancels the inline install', fakeAsync(() => {
    const missing = missingToolchain();
    const installing = missingToolchain({
      installing: true,
      percent: 40,
      phase: 'upscale',
    });

    wailsSpy.getToolchainStatus.and.resolveTo(missing);
    wailsSpy.startToolchainInstall.and.resolveTo(installing);
    wailsSpy.cancelToolchainInstall.and.resolveTo(
      missingToolchain({ installing: true, cancelling: true }),
    );

    fixture = TestBed.createComponent(ImageUpscale);
    component = fixture.componentInstance;
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    query<HTMLButtonElement>('.pack-install')?.click();
    flushMicrotasks();
    fixture.detectChanges();

    const cancel = query<HTMLButtonElement>('.pack-cancel');
    expect(cancel).not.toBeNull();
    cancel?.click();
    flushMicrotasks();

    expect(wailsSpy.cancelToolchainInstall).toHaveBeenCalled();

    // Settle the background poll so the test leaves no pending timers.
    wailsSpy.getToolchainInstallState.and.resolveTo(missingToolchain());
    tick(1500);
    flushMicrotasks();
    tick(500);
    flushMicrotasks();
    discardPeriodicTasks();
  }));

  it('sends batch mode and rejects unsupported files', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected([
      '/tmp/in/a.png',
      '/tmp/in/b.jpg',
      '/tmp/in/c.heic',
    ]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/in/a.png', '/tmp/in/b.jpg']);
    expect(component.notice()?.text).toBe(
      "1 file wasn't added. HEIC isn't supported.",
    );
    expect(component.primaryLabel()).toBe('Upscale 2 images');
  }));

  it('runs, observes and reports the processed summary', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const completed = status('upscale-job-1', 'success', [
      {
        inputPath: '/tmp/in/photo.png',
        outputPath: '/tmp/in/photo_2x.png',
        outputs: ['/tmp/in/photo_2x.png'],
        success: true,
        message: '2x upscale, 1280×960',
      },
    ]);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    expect(component.isPolling()).toBeTrue();

    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain(
      '2x upscale, 1280×960',
    );
  }));

  it('consumes queued drop intake paths on init', fakeAsync(() => {
    TestBed.inject(DropIntakeService).queue('/image-upscale', [
      '/tmp/queued-one.png',
    ]);

    create();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/queued-one.png']);
    expect(wailsSpy.planJob).toHaveBeenCalled();
  }));

  it('renders the page copy in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');
    create();
    flushMicrotasks();
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Ampliar');
    expect(
      element.querySelector('.action-primary')?.textContent,
    ).toContain('Ampliar imagen');
    expect(element.textContent).toContain('Solo local');
  }));
});
