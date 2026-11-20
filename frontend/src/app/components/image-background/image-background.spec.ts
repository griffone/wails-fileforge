import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EMPTY, Subject } from 'rxjs';

import { ImageBackground } from './image-background';
import { DropIntakeService } from '../../services/drop-intake.service';
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

describe('ImageBackground', () => {
  let fixture: ComponentFixture<ImageBackground>;
  let component: ImageBackground;
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
          id: 'rembg',
          label: 'Background removal engine',
          ready: true,
          required: false,
          detail: 'Dedicated Python environment installed.',
        },
        {
          id: 'rembg-model',
          label: 'Background removal model',
          ready: true,
          required: false,
          detail: 'isnet-general-use model installed.',
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
          id: 'rembg',
          label: 'Background removal engine',
          ready: false,
          required: false,
          detail: 'Not installed. Run Install / repair in Settings.',
        },
        {
          id: 'rembg-model',
          label: 'Background removal model',
          ready: false,
          required: false,
          detail: 'The model file is missing.',
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
    return {
      success: true,
      message: 'Ready to remove the background from 1 image.',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_nobg.png'),
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
        toolId: 'tool.image.background',
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

  /** Creates the page with the given toolchain and renders the first frame. */
  function create(status: ToolchainStatus = readyToolchain()): void {
    wailsSpy.getToolchainStatus.and.resolveTo(status);
    fixture = TestBed.createComponent(ImageBackground);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'getToolchainStatus',
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
    jobStateSubject = new Subject<JobResult>();
    (wailsSpy as unknown as { jobState$: unknown }).jobState$ =
      jobStateSubject.asObservable();

    wailsSpy.getToolchainStatus.and.resolveTo(readyToolchain());
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
      jobId: 'background-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'background-job-1',
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
      imports: [ImageBackground],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem('ff-preferences');
  });

  it('enables the page when the engine and the model are ready', fakeAsync(() => {
    create();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getToolchainStatus).toHaveBeenCalled();
    expect(component.packReady()).toBeTrue();
    expect(component.packBanner()).toBeNull();
    expect(component.packBadge()).toBe('Pack ready');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Remove Background');
    expect(text).toContain('Drop images here');
    expect(text).not.toContain('The background removal pack is not installed.');

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.background');
    expect(request.mode).toBe('single');
    expect(request.options).toEqual({});
    expect(component.canRun()).toBeTrue();
  }));

  it('blocks plan and run when the pack is missing and links to Settings', fakeAsync(() => {
    create(missingToolchain());
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packReady()).toBeFalse();
    expect(component.packBadge()).toBe('Pack missing');
    expect(component.packBanner()?.title).toBe(
      'The background removal pack is not installed.',
    );
    expect(component.packBanner()?.detail).toContain('about 630 MB');
    expect(component.packBanner()?.detail).toContain('Install / repair');

    const link = query<HTMLAnchorElement>('.pack-banner a');
    expect(link).not.toBeNull();
    expect(link?.getAttribute('href')).toBe('/settings');
    expect(link?.textContent).toContain('Install / repair');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Not installed. Run Install / repair in Settings.');
    expect(text).toContain('The model file is missing.');

    expect(query<HTMLButtonElement>('.pack-actions button')?.textContent).toContain(
      'Recheck',
    );

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJob).not.toHaveBeenCalled();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'The background removal pack is not ready. Install it in Settings first.',
    );
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('asks for repair when only one pack component is ready', fakeAsync(() => {
    create(
      readyToolchain({
        components: [
          {
            id: 'rembg',
            label: 'Background removal engine',
            ready: true,
            required: false,
            detail: 'Dedicated Python environment installed.',
          },
          {
            id: 'rembg-model',
            label: 'Background removal model',
            ready: false,
            required: false,
            detail: 'The model file is missing.',
          },
        ],
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.packReady()).toBeFalse();
    expect(component.missingPackComponents().map((item) => item.id)).toEqual([
      'rembg-model',
    ]);
    expect(component.packBanner()?.title).toBe(
      'The background removal pack needs repair.',
    );
    expect(fixture.nativeElement.textContent).toContain(
      'The model file is missing.',
    );
  }));

  it('sends no backgroundColor for transparent output', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.inputPaths).toEqual(['/tmp/in/photo.png']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({});
    expect(component.backgroundMode()).toBe('transparent');
    expect(query<HTMLSelectElement>('#background-mode')?.value).toBe(
      'transparent',
    );
  }));

  it('sends a normalized #rrggbb color when a color is chosen', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    component.onModeChange(inputEvent('color'));
    component.onColorInput(inputEvent('#A1B2C3'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.options).toEqual({ backgroundColor: '#a1b2c3' });
    expect(component.normalizedColor()).toBe('#a1b2c3');
    expect(component.backgroundLabel()).toBe('#a1b2c3');
    expect(query<HTMLInputElement>('#background-color')?.value).toBe(
      '#a1b2c3',
    );
  }));

  it('recheck reloads the status and unblocks the plan', fakeAsync(() => {
    create(missingToolchain());
    flushMicrotasks();
    fixture.detectChanges();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    wailsSpy.getToolchainStatus.and.resolveTo(readyToolchain());
    (query<HTMLButtonElement>('.pack-actions button') as HTMLButtonElement).click();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getToolchainStatus).toHaveBeenCalledTimes(2);
    expect(component.packReady()).toBeTrue();
    expect(component.packBanner()).toBeNull();
    expect(wailsSpy.planJob).toHaveBeenCalled();
    expect(component.canRun()).toBeTrue();
  }));

  it('runs, observes and reports the processed summary', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/photo.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.getJobStatus.and.resolveTo(
      status('background-job-1', 'running', []),
    );
    const completed = status('background-job-1', 'success', [
      {
        inputPath: '/tmp/in/photo.png',
        outputPath: '/tmp/in/photo_nobg.png',
        outputs: ['/tmp/in/photo_nobg.png'],
        success: true,
        message: 'transparent PNG 640×480',
      },
    ]);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.image.background');
    expect(request.options).toEqual({});
    expect(component.isPolling()).toBeTrue();

    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('1 image processed');
    expect(text).toContain('transparent PNG 640×480');

    component.onOpen('/tmp/in/photo_nobg.png');
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/in/photo_nobg.png');
  }));

  it('uses batch mode and rejects unsupported files', fakeAsync(() => {
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
    expect(component.primaryLabel()).toBe(
      'Remove background from 2 images',
    );
  }));

  it('consumes queued drop intake paths on init', fakeAsync(() => {
    TestBed.inject(DropIntakeService).queue('/image-background', [
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);

    create();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);
    expect(wailsSpy.planJob).toHaveBeenCalled();
  }));
});
