import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { ActivatedRoute } from '@angular/router';
import { EMPTY, Subject } from 'rxjs';

import { RecipeRun } from './recipe-run';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import {
  FileInfo,
  RecipePlanResponse,
  RecipeProgressEvent,
  RecipeRequest,
  RecipeResult,
  RecipeStatusResponse,
  Recipe,
  RecipeOptionKind,
  Wails,
} from '../../services/wails';

const EMAIL_RECIPE_ID = 'recipe.email_pdf';
const WEB_RECIPE_ID = 'recipe.web_image_set';
const SOCIAL_RECIPE_ID = 'recipe.social_clip';
const CLEAN_SCAN_RECIPE_ID = 'recipe.clean_scan';
const TARGET_BYTES = 10 * 1024 * 1024;

const emailPdfRecipe: Recipe = {
  id: EMAIL_RECIPE_ID,
  name: 'Email PDF',
  description:
    'Merge one or more PDFs and compress the result to a size that is easy to send by email.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  maxInputs: 0,
  highlights: ['Merge when needed', 'Target size', 'Verified + undo'],
  steps: [
    {
      id: 'merge',
      name: 'Merge PDFs',
      toolId: 'tool.pdf.merge',
      detail: 'Only when there are two or more PDFs',
    },
    {
      id: 'optimize',
      name: 'Optimize to target',
      toolId: 'tool.pdf.optimize',
      detail: 'Ghostscript when the target needs it',
    },
  ],
  defaultTargetSizeBytes: TARGET_BYTES,
  duplicable: false,
  optionSchema: [
    {
      key: 'targetSizeBytes',
      label: 'Target size',
      kind: 'integer' as RecipeOptionKind,
      unit: 'MB',
      min: 1 * 1024 * 1024,
      max: 100 * 1024 * 1024,
      scale: 1024 * 1024,
      default: TARGET_BYTES,
      help: 'Best effort: the output may stay above the target.',
    },
  ],
};

const webImageSetRecipe: Recipe = {
  id: WEB_RECIPE_ID,
  name: 'Web image set',
  description:
    'Export responsive WebP and JPEG variants with a ZIP bundle and a ready-to-paste srcset snippet.',
  category: 'image',
  inputKind: 'image',
  acceptExtensions: ['png', 'jpg', 'jpeg', 'webp'],
  maxInputs: 1,
  highlights: ['No upscaling', 'WebP + JPEG', 'ZIP + srcset'],
  steps: [
    {
      id: 'build',
      name: 'Build responsive variants',
      toolId: 'tool.image.convert',
      detail: 'WebP and JPEG at the widths your source can support',
    },
    {
      id: 'create-zip',
      name: 'Create ZIP bundle',
      toolId: '',
      detail: 'All variants plus the snippet in one download',
    },
    {
      id: 'snippet',
      name: 'Write srcset snippet',
      toolId: '',
      detail: 'Ready-to-paste picture markup',
    },
  ],
  defaultTargetSizeBytes: 0,
  duplicable: false,
};

const WEB_OUTPUTS = [
  '/tmp/out/hero-1600.webp',
  '/tmp/out/hero-1600.jpg',
  '/tmp/out/hero-800.webp',
  '/tmp/out/hero-800.jpg',
  '/tmp/out/hero-web-set.zip',
  '/tmp/out/hero-srcset.html',
];

const socialClipRecipe: Recipe = {
  id: SOCIAL_RECIPE_ID,
  name: 'Social clip',
  description:
    'Encode one video as MP4 and WebM and export a poster frame, ready for social uploads.',
  category: 'video',
  inputKind: 'video',
  acceptExtensions: ['mp4', 'mov', 'mkv', 'webm', 'avi'],
  maxInputs: 1,
  highlights: ['MP4 + WebM', 'Source resolution', 'Poster frame'],
  steps: [
    {
      id: 'mp4',
      name: 'Encode MP4',
      toolId: 'tool.video.convert',
      detail: 'H.264 + AAC at the source resolution',
    },
    {
      id: 'webm',
      name: 'Encode WebM',
      toolId: 'tool.video.convert',
      detail: 'VP9 + Opus at the source resolution',
    },
    {
      id: 'poster',
      name: 'Extract poster',
      toolId: '',
      detail: 'JPEG frame from the first second',
    },
  ],
  defaultTargetSizeBytes: 0,
  duplicable: false,
};

const SOCIAL_OUTPUTS = [
  '/tmp/in/clip-social.mp4',
  '/tmp/in/clip-social.webm',
  '/tmp/in/clip-poster.jpg',
];

const cleanScanRecipe: Recipe = {
  id: CLEAN_SCAN_RECIPE_ID,
  name: 'Clean scan',
  description:
    'Turn a scanned PDF into a searchable PDF with a local text sidecar.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  maxInputs: 1,
  highlights: ['Local OCR', 'Searchable PDF', 'Text sidecar'],
  steps: [
    {
      id: 'ocr',
      name: 'OCR every page',
      toolId: 'tool.pdf.ocr',
      detail: 'Rasterize at 300 DPI and recognize text locally',
    },
    {
      id: 'verify',
      name: 'Verify text layer',
      toolId: '',
      detail: 'Page count and extracted text',
    },
  ],
  defaultTargetSizeBytes: 0,
  duplicable: false,
};

const CLEAN_SCAN_OUTPUTS = [
  '/tmp/in/report_clean.pdf',
  '/tmp/in/report_clean.txt',
];

function statusResponse(result: RecipeResult): RecipeStatusResponse {
  return { success: true, found: true, message: result.status, result };
}

function createWailsSpy(): jasmine.SpyObj<Wails> {
  const spy = jasmine.createSpyObj<Wails>('Wails', [
    'listRecipes',
    'planRecipe',
    'inspectFiles',
    'runRecipe',
    'getRecipeStatus',
    'cancelRecipe',
    'openFileDialog',
    'openMultipleFilesDialog',
    'openDirectoryDialog',
    'openPath',
    'revealInFolder',
  ]);
  // The component subscribes to the live recipe stream in its constructor.
  (spy as unknown as { recipeProgress$: unknown }).recipeProgress$ = EMPTY;
  return spy;
}

function createRoute(recipeId: string): ActivatedRoute {
  return { snapshot: { data: { recipeId } } } as unknown as ActivatedRoute;
}

/** The locale is persisted, so every spec starts from the default language. */
function clearPreferences(): void {
  localStorage.removeItem(PREFERENCES_STORAGE_KEY);
}

describe('RecipeRun (Email PDF)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let recipeStateSubject: Subject<RecipeResult>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function fileInfo(path: string): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: 'pdf',
      sizeBytes: 2 * 1024 * 1024,
      kind: 'pdf',
      pageCount: 3,
      exists: true,
    };
  }

  function planResponse(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to email 1 PDF.',
      recipe: emailPdfRecipe,
      inputPaths: request.inputPaths,
      outputDir: request.outputDir || '/tmp',
      outputs: [
        {
          inputPath: request.inputPaths.join(','),
          outputPath: '/tmp/a_email.pdf',
          outputKind: 'file',
          outputCount: 1,
          exists: false,
          adjusted: false,
        },
      ],
      warnings: [
        'Target compression is best effort: the output may stay above 10.0 MB.',
      ],
    };
  }

  function recipeResult(
    overrides: Partial<RecipeResult> = {},
  ): RecipeResult {
    return {
      jobId: 'recipe-job-1',
      recipeId: EMAIL_RECIPE_ID,
      status: 'success',
      message: 'Email PDF ready: a_email.pdf.',
      currentStep: 2,
      totalSteps: 2,
      steps: [
        {
          id: 'merge',
          name: 'Merge PDFs',
          toolId: 'tool.pdf.merge',
          status: 'skipped',
          message: 'Using the original PDF',
        },
        {
          id: 'optimize',
          name: 'Optimize to target',
          toolId: 'tool.pdf.optimize',
          status: 'success',
          message: 'Optimized to 8.0 MB.',
        },
      ],
      outputs: ['/tmp/a_email.pdf'],
      targetSizeBytes: TARGET_BYTES,
      verification: {
        success: true,
        message: 'All checks passed.',
        outputs: [
          {
            path: '/tmp/a_email.pdf',
            exists: true,
            sizeBytes: 8 * 1024 * 1024,
            kind: 'pdf',
            checks: [
              {
                key: 'target',
                label: 'Target size',
                passed: true,
                value: '8.0 MB',
              },
              { key: 'pages', label: 'Pages', passed: true, value: '3 pages' },
            ],
          },
        ],
        warnings: [],
      },
      startedAt: 1,
      endedAt: 2,
      ...overrides,
    };
  }

  function runningRecipeResult(): RecipeResult {
    return recipeResult({
      status: 'running',
      message: 'Optimizing to 10.0 MB.',
      currentStep: 2,
      outputs: [],
      verification: undefined,
      endedAt: undefined,
      steps: [
        {
          id: 'merge',
          name: 'Merge PDFs',
          toolId: 'tool.pdf.merge',
          status: 'skipped',
          message: 'Using the original PDF',
        },
        {
          id: 'optimize',
          name: 'Optimize to target',
          toolId: 'tool.pdf.optimize',
          status: 'pending',
        },
      ],
    });
  }

  function cancelledRecipeResult(): RecipeResult {
    return recipeResult({
      status: 'cancelled',
      message: 'Recipe cancelled.',
      currentStep: 1,
      outputs: [],
      verification: undefined,
      steps: [
        {
          id: 'merge',
          name: 'Merge PDFs',
          toolId: 'tool.pdf.merge',
          status: 'skipped',
          message: 'Using the original PDF',
        },
        {
          id: 'optimize',
          name: 'Optimize to target',
          toolId: 'tool.pdf.optimize',
          status: 'pending',
        },
      ],
    });
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'recipe-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(runningRecipeResult()),
    );
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
      result: runningRecipeResult(),
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });
    recipeStateSubject = new Subject<RecipeResult>();
    (wailsSpy as unknown as { recipeState$: unknown }).recipeState$ =
      recipeStateSubject.asObservable();

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(EMAIL_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the curated recipe and shows the default 10 MB target', async () => {
    await fixture.whenStable();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(EMAIL_RECIPE_ID);
    expect(component.optionValues()['targetSizeBytes']).toBe('10');
    expect(component.optionFields().map((field) => field.key)).toEqual([
      'targetSizeBytes',
    ]);
    expect(component.multipleInputs()).toBeTrue();
    expect(query<HTMLInputElement>('#recipe-option-targetSizeBytes')?.value).toBe('10');
    expect(fixture.nativeElement.textContent).toContain('Email PDF');
    expect(fixture.nativeElement.textContent).toContain('Optimize to target');
    expect(fixture.nativeElement.textContent).toContain('Target size');
  });

  it('keeps the local fallback when the recipe catalog is unavailable', async () => {
    wailsSpy.listRecipes.and.resolveTo({
      success: false,
      message: 'ipc error',
      recipes: [],
    });

    const fresh = TestBed.createComponent(RecipeRun);
    fresh.detectChanges();
    await fresh.whenStable();
    fresh.detectChanges();

    expect(fresh.componentInstance.recipe()?.id).toBe(EMAIL_RECIPE_ID);
    expect(fresh.componentInstance.steps().map((step) => step.name)).toEqual([
      'Merge PDFs',
      'Optimize to target',
    ]);
    expect(
      (fresh.nativeElement.querySelector('#recipe-option-targetSizeBytes') as HTMLInputElement)
        .value,
    ).toBe('10');
    fresh.destroy();
  });

  it('filters non-PDF drops and shows a warning', fakeAsync(() => {
    component.onPathsSelected([
      '/tmp/notes.txt',
      '/tmp/a.pdf',
      '/tmp/a.pdf',
    ]);
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual(['/tmp/a.pdf']);
    expect(component.notice()).toContain('notes.txt');
    expect(component.notice()).toContain("isn't a PDF");
    expect(component.notice()).toContain('a.pdf is already in the queue.');
    expect(fixture.nativeElement.textContent).toContain("isn't a PDF");
  }));

  it('plans with the target bytes after files are selected', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(EMAIL_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/a.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({ targetSizeBytes: TARGET_BYTES });
    expect(component.plan()?.success).toBeTrue();
    expect(component.planBlocked()).toBeFalse();
  }));

  it('runs, observes to terminal and shows the verified result', fakeAsync(() => {
    wailsSpy.getRecipeStatus.and.resolveTo(statusResponse(recipeResult()));

    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(EMAIL_RECIPE_ID);
    expect(request.options).toEqual({ targetSizeBytes: TARGET_BYTES });
    expect(wailsSpy.getRecipeStatus).toHaveBeenCalledWith('recipe-job-1');
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.toolId).toBe(EMAIL_RECIPE_ID);
    expect(component.resultJob()?.items?.length).toBe(1);
    expect(component.verification()?.success).toBeTrue();

    const text = fixture.nativeElement.textContent as string;
    expect(query('app-result-card')).not.toBeNull();
    expect(text).toContain('1 PDF prepared');
    expect(text).toContain('Verified');
    expect(text).toContain('2/2 checks passed');
  }));

  it('shows the partial result message instead of the retry copy', fakeAsync(() => {
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(
        recipeResult({
          status: 'partial_success',
          message:
            'Email PDF created, but it is 12.4 MB: above the 10 MB target.',
        }),
      ),
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalResult()?.status).toBe('partial_success');
    expect(component.resultJob()?.items?.length).toBe(1);
    expect(component.partialDetail()).toBe(
      'Email PDF created, but it is 12.4 MB: above the 10 MB target.',
    );

    const text = fixture.nativeElement.textContent as string;
    expect(query('app-result-card')).not.toBeNull();
    expect(text).toContain(
      'Email PDF created, but it is 12.4 MB: above the 10 MB target.',
    );
    expect(text).not.toContain('Retry them below');
  }));

  it('cancels a running recipe and settles on the terminal state event', fakeAsync(() => {
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(runningRecipeResult()),
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isRunning()).toBeTrue();
    expect(query<HTMLButtonElement>('.action-row .btn-ghost')?.textContent).toContain(
      'Cancel',
    );

    void component.cancel();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.cancelRecipe).toHaveBeenCalledWith('recipe-job-1');
    expect(component.cancelRequested()).toBeTrue();
    expect(
      query<HTMLButtonElement>('.action-row .btn-ghost')?.textContent,
    ).toContain('Cancelling…');

    recipeStateSubject.next(cancelledRecipeResult());
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalResult()?.status).toBe('cancelled');
    expect(component.isRunning()).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain(
      'Cancelled — no output was written.',
    );
  }));

  it('blocks the run when the target is invalid', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const target = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;
    target.value = '0';
    target.dispatchEvent(new Event('input'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.optionsInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(
      (query<HTMLButtonElement>('.action-primary') as HTMLButtonElement).disabled,
    ).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'Target size must be between 1 and 100 MB.',
    );

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runRecipe).not.toHaveBeenCalled();
  }));
});

describe('RecipeRun (Web image set)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let recipeProgressSubject: Subject<RecipeProgressEvent>;
  let recipeStateSubject: Subject<RecipeResult>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function fileInfo(path: string): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
      extension: 'png',
      sizeBytes: 640 * 1024,
      kind: 'image',
      width: 720,
      height: 480,
      exists: true,
    };
  }

  function planResponse(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to build 6 files.',
      recipe: webImageSetRecipe,
      inputPaths: request.inputPaths,
      outputDir: request.outputDir || '/tmp/in',
      outputs: WEB_OUTPUTS.map((path) => ({
        inputPath: request.inputPaths[0] ?? '',
        outputPath: path,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  function webRecipeResult(): RecipeResult {
    return {
      jobId: 'web-job-1',
      recipeId: WEB_RECIPE_ID,
      status: 'success',
      message: 'Web image set ready: 6 files.',
      currentStep: 3,
      totalSteps: 3,
      steps: [
        {
          id: 'build',
          name: 'Build responsive variants',
          toolId: 'tool.image.convert',
          status: 'success',
          message: 'Built 4 variants.',
        },
        {
          id: 'create-zip',
          name: 'Create ZIP bundle',
          toolId: '',
          status: 'success',
        },
        {
          id: 'snippet',
          name: 'Write srcset snippet',
          toolId: '',
          status: 'success',
        },
      ],
      outputs: [...WEB_OUTPUTS],
      verification: {
        success: true,
        message: 'All checks passed.',
        outputs: WEB_OUTPUTS.map((path) => ({
          path,
          exists: true,
          sizeBytes: 512 * 1024,
          kind: path.endsWith('.zip') ? 'archive' : 'file',
          checks: [
            {
              key: 'exists',
              label: 'Exists',
              passed: true,
              value: 'yes',
            },
          ],
        })),
        warnings: [],
      },
      startedAt: 1,
      endedAt: 2,
    };
  }

  function runningWebRecipeResult(): RecipeResult {
    return {
      jobId: 'web-job-1',
      recipeId: WEB_RECIPE_ID,
      status: 'running',
      message: 'Building variants…',
      currentStep: 2,
      totalSteps: 3,
      steps: [
        {
          id: 'build',
          name: 'Build responsive variants',
          toolId: 'tool.image.convert',
          status: 'pending',
        },
        {
          id: 'create-zip',
          name: 'Create ZIP bundle',
          toolId: '',
          status: 'pending',
        },
        {
          id: 'snippet',
          name: 'Write srcset snippet',
          toolId: '',
          status: 'pending',
        },
      ],
      outputs: [],
      startedAt: 1,
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    recipeProgressSubject = new Subject<RecipeProgressEvent>();
    (wailsSpy as unknown as { recipeProgress$: unknown }).recipeProgress$ =
      recipeProgressSubject.asObservable();
    recipeStateSubject = new Subject<RecipeResult>();
    (wailsSpy as unknown as { recipeState$: unknown }).recipeState$ =
      recipeStateSubject.asObservable();

    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'web-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(webRecipeResult()),
    );
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
      result: webRecipeResult(),
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(WEB_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the web recipe, hides the target field and shows the details', async () => {
    await fixture.whenStable();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(WEB_RECIPE_ID);
    expect(component.optionFields()).toEqual([]);
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-option-targetSizeBytes')).toBeNull();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Web image set');
    expect(text).toContain('No upscaling');
    expect(text).toContain('WebP + JPEG');
    expect(text).toContain('ZIP + srcset');
    expect(text).toContain('WebP and JPEG at the widths your source can support');
    expect(text).toContain('The recipe chooses the output formats.');
  });

  it('rejects a GIF or other non-image drop with a warning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/animation.gif']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()).toContain('animation.gif');
    expect(component.notice()).toContain("isn't an image");

    component.onPathsSelected(['/tmp/in/notes.txt']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()).toContain('notes.txt');
  }));

  it('keeps only the first image and plans with one input', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/hero.png', '/tmp/in/thumb.jpg']);
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual(['/tmp/in/hero.png']);
    expect(component.notice()).toContain('Only the first image is used.');
    expect(component.fileItems()[0]?.meta).toContain('720 × 480 px');

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(WEB_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/hero.png']);
  }));

  it('plans without a target size option', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.options).toEqual({});
    expect('targetSizeBytes' in request.options).toBeFalse();
    expect(component.optionFields()).toEqual([]);
  }));

  it('shows live recipe progress from recipes/progress while running', fakeAsync(() => {
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(runningWebRecipeResult()),
    );

    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isRunning()).toBeTrue();
    expect(component.runJobId()).toBe('web-job-1');
    // Before the event the seeded snapshot marks step 2 as active.
    expect(component.steps()[1].state).toBe('running');

    recipeProgressSubject.next({
      jobId: 'web-job-1',
      recipeId: WEB_RECIPE_ID,
      status: 'running',
      currentStep: 1,
      totalSteps: 3,
      stepName: 'Build responsive variants',
      stage: 'running',
      message: '1600 px WebP',
      percent: 37,
    });
    fixture.detectChanges();

    expect(component.progressPercent()).toBe(37);
    expect(component.steps()[0].state).toBe('running');
    expect(component.steps()[1].state).toBe('pending');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('37%');
    expect(text).toContain('1600 px WebP');

    // An event from another recipe job must not touch this page.
    recipeProgressSubject.next({
      jobId: 'another-job',
      recipeId: WEB_RECIPE_ID,
      status: 'running',
      currentStep: 3,
      totalSteps: 3,
      stepName: 'Write srcset snippet',
      stage: 'running',
      message: 'Someone else',
      percent: 99,
    });
    fixture.detectChanges();

    expect(component.progressPercent()).toBe(37);
    expect(fixture.nativeElement.textContent).toContain('1600 px WebP');
    expect(fixture.nativeElement.textContent).not.toContain('Someone else');

    // Reset the run so the observer is released before fakeAsync exits.
    component.resetRun();
    fixture.detectChanges();
  }));

  it('runs, observes to terminal and lists all six verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(WEB_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items?.length).toBe(6);
    expect(component.resultJob()?.toolId).toBe(WEB_RECIPE_ID);

    const text = fixture.nativeElement.textContent as string;
    for (const output of WEB_OUTPUTS) {
      expect(text).toContain(output.split('/').pop() as string);
    }
    expect(text).toContain('6 files prepared');
    expect(text).toContain('Verified');
    expect(text).toContain('6/6 checks passed');
  }));
});

describe('RecipeRun (Social clip)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function videoFileInfo(path: string): FileInfo {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'mp4',
      sizeBytes: 1024 * 1024,
      kind: 'video',
      durationSeconds: 1,
      width: 160,
      height: 120,
      videoCodec: 'h264',
      audioCodec: 'aac',
      exists: true,
    };
  }

  function planResponse(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to publish 3 files.',
      recipe: socialClipRecipe,
      inputPaths: request.inputPaths,
      outputDir: request.outputDir || '/tmp/in',
      outputs: SOCIAL_OUTPUTS.map((path) => ({
        inputPath: request.inputPaths[0] ?? '',
        outputPath: path,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  function socialRecipeResult(): RecipeResult {
    return {
      jobId: 'social-job-1',
      recipeId: SOCIAL_RECIPE_ID,
      status: 'success',
      message: 'Social clip ready: 3 files.',
      currentStep: 3,
      totalSteps: 3,
      steps: [
        {
          id: 'mp4',
          name: 'Encode MP4',
          toolId: 'tool.video.convert',
          status: 'success',
          message: 'Encoded H.264 + AAC.',
        },
        {
          id: 'webm',
          name: 'Encode WebM',
          toolId: 'tool.video.convert',
          status: 'success',
          message: 'Encoded VP9 + Opus.',
        },
        {
          id: 'poster',
          name: 'Extract poster',
          toolId: '',
          status: 'success',
          message: 'Extracted the poster frame.',
        },
      ],
      outputs: [...SOCIAL_OUTPUTS],
      verification: {
        success: true,
        message: 'All checks passed.',
        outputs: SOCIAL_OUTPUTS.map((path) => ({
          path,
          exists: true,
          sizeBytes: 900 * 1024,
          kind: path.endsWith('.jpg') ? 'image' : 'video',
          checks: [
            {
              key: 'exists',
              label: 'Exists',
              passed: true,
              value: 'yes',
            },
          ],
        })),
        warnings: [],
      },
      startedAt: 1,
      endedAt: 2,
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe, socialClipRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(videoFileInfo),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'social-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(socialRecipeResult()),
    );
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
      result: socialRecipeResult(),
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(SOCIAL_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the Social clip recipe and shows the video details', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(SOCIAL_RECIPE_ID);
    expect(component.optionFields()).toEqual([]);
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-option-targetSizeBytes')).toBeNull();

    expect(component.inputTitle()).toBe('Video');
    expect(component.dropTitle()).toBe('Drop a video here');
    expect(component.chooseLabel()).toBe('Choose video');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Social clip');
    expect(text).toContain('MP4 + WebM');
    expect(text).toContain('Source resolution');
    expect(text).toContain('Poster frame');
    expect(text).toContain('H.264 + AAC at the source resolution');
    expect(text).toContain('VP9 + Opus at the source resolution');
    expect(text).toContain('JPEG frame from the first second');

    component.onPathsSelected(['/tmp/in/clip.mp4']);
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const meta = component.fileItems()[0]?.meta ?? '';
    expect(meta).toContain('160 × 120 px');
    expect(meta).toContain('0:01');
  }));

  it('rejects a non-video drop with a warning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/notes.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()).toContain('notes.pdf');
    expect(component.notice()).toContain("isn't a video");
    expect(fixture.nativeElement.textContent).toContain("isn't a video");
  }));

  it('plans with one input and no options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/clip.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(SOCIAL_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/clip.mp4']);
    expect(request.options).toEqual({});
    expect(component.optionFields()).toEqual([]);
  }));

  it('runs, observes to terminal and lists the three verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/clip.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(SOCIAL_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items?.length).toBe(3);
    expect(component.resultJob()?.toolId).toBe(SOCIAL_RECIPE_ID);

    const text = fixture.nativeElement.textContent as string;
    for (const output of SOCIAL_OUTPUTS) {
      expect(text).toContain(output.split('/').pop() as string);
    }
    expect(text).toContain('3 files prepared');
    expect(text).toContain('Verified');
    expect(text).toContain('3/3 checks passed');
  }));
});

describe('RecipeRun (Clean scan)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function cleanScanFileInfo(path: string): FileInfo {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 3 * 1024 * 1024,
      kind: 'pdf',
      pageCount: 4,
      exists: true,
    };
  }

  function planResponse(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to make 1 PDF searchable.',
      recipe: cleanScanRecipe,
      inputPaths: request.inputPaths,
      outputDir: request.outputDir || '/tmp/in',
      outputs: CLEAN_SCAN_OUTPUTS.map((path) => ({
        inputPath: request.inputPaths[0] ?? '',
        outputPath: path,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  function cleanScanResult(): RecipeResult {
    return {
      jobId: 'clean-job-1',
      recipeId: CLEAN_SCAN_RECIPE_ID,
      status: 'success',
      message: 'Clean scan ready: report_clean.pdf and report_clean.txt.',
      currentStep: 2,
      totalSteps: 2,
      steps: [
        {
          id: 'ocr',
          name: 'OCR every page',
          toolId: 'tool.pdf.ocr',
          status: 'success',
          message: 'OCR finished: 4 pages, 980 characters recognized.',
        },
        {
          id: 'verify',
          name: 'Verify text layer',
          toolId: '',
          status: 'success',
          message: 'Page count and text layer verified.',
        },
      ],
      outputs: [...CLEAN_SCAN_OUTPUTS],
      verification: {
        success: true,
        message: 'All checks passed.',
        outputs: CLEAN_SCAN_OUTPUTS.map((path) => ({
          path,
          exists: true,
          sizeBytes: 2 * 1024 * 1024,
          kind: path.endsWith('.txt') ? 'document' : 'pdf',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'yes' },
            {
              key: path.endsWith('.txt') ? 'text_sidecar' : 'text_layer',
              label: 'Text',
              passed: true,
              value: 'non-empty',
            },
          ],
        })),
        warnings: [],
      },
      startedAt: 1,
      endedAt: 2,
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [
        emailPdfRecipe,
        webImageSetRecipe,
        socialClipRecipe,
        cleanScanRecipe,
      ],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(cleanScanFileInfo),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'clean-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo(
      statusResponse(cleanScanResult()),
    );
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
      result: cleanScanResult(),
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(CLEAN_SCAN_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the Clean scan recipe, hides the target and shows both step details', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(component.optionFields()).toEqual([]);
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-option-targetSizeBytes')).toBeNull();
    expect(component.inputTitle()).toBe('PDFs');
    expect(component.chooseLabel()).toBe('Choose PDF');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Clean scan');
    expect(text).toContain('Local OCR');
    expect(text).toContain('Searchable PDF');
    expect(text).toContain('Text sidecar');
    expect(text).toContain('Rasterize at 300 DPI and recognize text locally');
    expect(text).toContain('Page count and extracted text');
    expect(text).toContain('The recipe chooses the output formats.');
  }));

  it('plans one PDF with no options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/report.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({});
    expect(component.optionFields()).toEqual([]);
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual(
      CLEAN_SCAN_OUTPUTS,
    );
  }));

  it('runs, observes to terminal and lists both verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(request.recipeId).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items?.length).toBe(2);
    expect(component.resultJob()?.toolId).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(component.verification()?.success).toBeTrue();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('report_clean.pdf');
    expect(text).toContain('report_clean.txt');
    expect(text).toContain('2 files prepared');
    expect(text).toContain('Verified');
    expect(text).toContain('4/4 checks passed');
  }));
});

describe('RecipeRun (Burned-in subtitles)', () => {
  const BURNED_RECIPE_ID = 'recipe.burned_subtitles';
  const BURNED_OUTPUTS = ['/tmp/in/lecture_subtitled.mp4', '/tmp/in/lecture_subtitled.srt'];

  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function videoFileInfo(path: string): FileInfo {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'mp4',
      sizeBytes: 2 * 1024 * 1024,
      kind: 'video',
      durationSeconds: 12,
      width: 320,
      height: 180,
      videoCodec: 'h264',
      audioCodec: 'aac',
      exists: true,
    };
  }

  function planResponse(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to burn 2 files.',
      recipe: component.recipe() ?? undefined,
      inputPaths: request.inputPaths,
      outputDir: request.outputDir || '/tmp/in',
      outputs: BURNED_OUTPUTS.map((path) => ({
        inputPath: request.inputPaths[0] ?? '',
        outputPath: path,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  function burnedResult(): RecipeResult {
    return {
      jobId: 'burned-job-1',
      recipeId: BURNED_RECIPE_ID,
      status: 'success',
      message: 'Burned-in subtitles ready: lecture_subtitled.mp4 and lecture_subtitled.srt.',
      currentStep: 3,
      totalSteps: 3,
      steps: [
        {
          id: 'transcribe',
          name: 'Transcribe audio',
          toolId: 'tool.video.transcribe',
          status: 'success',
          message: 'Transcribed 12 seconds of audio.',
        },
        {
          id: 'burn',
          name: 'Burn subtitles',
          toolId: '',
          status: 'success',
          message: 'Rendered the cues and copied the audio.',
        },
        {
          id: 'verify',
          name: 'Verify sync',
          toolId: '',
          status: 'success',
          message: 'Duration, cues and frames verified.',
        },
      ],
      outputs: [...BURNED_OUTPUTS],
      verification: {
        success: true,
        message: 'All checks passed.',
        outputs: BURNED_OUTPUTS.map((path) => ({
          path,
          exists: true,
          sizeBytes: 1024 * 1024,
          kind: path.endsWith('.srt') ? 'document' : 'video',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'yes' },
            {
              key: 'cues',
              label: 'Cues',
              passed: true,
              value: 'in sync',
            },
          ],
        })),
        warnings: [],
      },
      startedAt: 1,
      endedAt: 2,
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    // The catalog predates the new recipe: the local fallback must cover it.
    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [
        emailPdfRecipe,
        webImageSetRecipe,
        socialClipRecipe,
        cleanScanRecipe,
      ],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(videoFileInfo),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'burned-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo(statusResponse(burnedResult()));
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
      result: burnedResult(),
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(BURNED_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the local fallback as a one-video recipe with three steps', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(BURNED_RECIPE_ID);
    expect(component.recipe()?.name).toBe('Burned-in subtitles');
    expect(component.optionFields()).toEqual([]);
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-option-targetSizeBytes')).toBeNull();

    expect(component.inputTitle()).toBe('Video');
    expect(component.dropTitle()).toBe('Drop a video here');
    expect(component.chooseLabel()).toBe('Choose video');
    expect(component.acceptAttribute()).toBe('.mp4,.mov,.mkv,.webm,.avi');

    const accept = query<HTMLInputElement>('.file-drop-input')?.getAttribute(
      'accept',
    );
    expect(accept).toBe('.mp4,.mov,.mkv,.webm,.avi');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Burned-in subtitles');
    expect(text).toContain('Local Whisper');
    expect(text).toContain('Synchronized burn-in');
    expect(text).toContain('SRT sidecar');
    expect(text).toContain('Transcribe audio');
    expect(text).toContain('Whisper writes timed subtitles locally');
    expect(text).toContain('Burn subtitles');
    expect(text).toContain('ffmpeg renders the cues into the picture; audio is copied');
    expect(text).toContain('Verify sync');
    expect(text).toContain('Duration, cues and rendered frames are checked');
  }));

  it('plans and runs the burned-in subtitles recipe with no options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/lecture.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    const planRequest = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(planRequest.recipeId).toBe(BURNED_RECIPE_ID);
    expect(planRequest.inputPaths).toEqual(['/tmp/in/lecture.mp4']);
    expect(planRequest.options).toEqual({});
    expect(component.optionFields()).toEqual([]);

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipe).toHaveBeenCalledTimes(1);
    const runRequest = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(runRequest.recipeId).toBe(BURNED_RECIPE_ID);
    expect(runRequest.options).toEqual({});

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('lecture_subtitled.mp4');
    expect(text).toContain('lecture_subtitled.srt');
    expect(text).toContain('Verified');
  }));
});

describe('RecipeRun (Spanish)', () => {
  let fixture: ComponentFixture<RecipeRun>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  beforeEach(async () => {
    clearPreferences();
    const wailsSpy = createWailsSpy();
    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe],
    });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(EMAIL_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    clearPreferences();
  });

  it('renders the recipe overlay, steps and action copy in Spanish', async () => {
    const i18n = TestBed.inject(I18nService);
    i18n.setLocale('es');

    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('RECETA');
    expect(text).toContain('Local · Verificado · Reversible');
    expect(text).toContain('PDF por email');
    expect(text).toContain(
      'Une uno o varios PDF y comprime el resultado a un tamaño fácil de enviar por correo electrónico.',
    );

    // Highlights come from the recipes dictionary, not the English fallback.
    expect(text).toContain('Unir si hace falta');
    expect(text).toContain('Tamaño objetivo');
    expect(text).toContain('Verificado + deshacer');
    expect(text).not.toContain('Merge when needed');

    expect(text).toContain('Unir PDF');
    expect(text).toContain('Solo cuando hay dos o más PDF');
    expect(text).toContain('Optimizar al objetivo');
    expect(text).toContain('Ghostscript cuando el objetivo lo requiere');
    expect(text).toContain('Pendiente');

    expect(text).toContain('Ajustes');
    expect(text).toContain('Carpeta de salida');
    expect(text).toContain('Pasos');
    expect(text).toContain('2 pasos');

    // The generic option renderer localizes the label, help and inline error.
    const target = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;
    expect(target.value).toBe('10');
    expect(text).toContain(
      'Mejor esfuerzo: la salida puede superar el objetivo.',
    );
    expect(text).toContain(
      'Define las opciones de la receta y dónde se guardan los resultados.',
    );

    target.value = '0';
    target.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain(
      'Tamaño objetivo debe estar entre 1 y 100 MB.',
    );

    expect(
      query<HTMLButtonElement>('.action-primary')?.textContent?.trim(),
    ).toBe('Crear PDF por email');
    expect(text).toContain('Agrega un PDF para crear PDF por email.');

    i18n.setLocale('en');
  });
});


describe('RecipeRun (user recipe)', () => {
  const USER_RECIPE_ID = 'recipe.user.42';
  const USER_TARGET_BYTES = 5 * 1024 * 1024;

  const userRecipe: Recipe = {
    ...emailPdfRecipe,
    id: USER_RECIPE_ID,
    name: 'Email under 5 MB',
    userDefined: true,
    baseRecipeId: EMAIL_RECIPE_ID,
    duplicable: true,
    optionSchema: [
      {
        key: 'targetSizeBytes',
        label: 'Target size',
        kind: 'integer' as RecipeOptionKind,
        unit: 'MB',
        min: 1 * 1024 * 1024,
        max: 100 * 1024 * 1024,
        scale: 1024 * 1024,
        default: USER_TARGET_BYTES,
      },
    ],
    optionOverrides: { targetSizeBytes: USER_TARGET_BYTES },
  };

  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function createUserRoute(recipeId: string): ActivatedRoute {
    return {
      snapshot: { data: {}, params: { recipeId } },
    } as unknown as ActivatedRoute;
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, userRecipe],
    });
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => ({
          path,
          name: path.split(/[\\/]/).pop() ?? path,
          directory: '/tmp',
          extension: 'pdf',
          sizeBytes: 1024,
          kind: 'pdf',
          exists: true,
        })),
      }),
    );
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve({
        success: true,
        message: 'Ready to email 1 PDF.',
        recipe: userRecipe,
        inputPaths: request.inputPaths,
        outputDir: '/tmp',
        outputs: [
          {
            inputPath: request.inputPaths[0] ?? '',
            outputPath: '/tmp/a_email.pdf',
            outputKind: 'file',
            outputCount: 1,
            exists: false,
            adjusted: false,
          },
        ],
        warnings: [],
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'user-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatus.and.resolveTo({
      success: true,
      found: true,
      message: 'success',
      result: {
        jobId: 'user-job-1',
        recipeId: USER_RECIPE_ID,
        status: 'success',
        message: 'Email PDF ready: a_email.pdf.',
        currentStep: 2,
        totalSteps: 2,
        steps: [],
        outputs: ['/tmp/a_email.pdf'],
        targetSizeBytes: USER_TARGET_BYTES,
        startedAt: 1,
        endedAt: 2,
      },
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createUserRoute(USER_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the user recipe, hides the built-in target and runs with saved overrides', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(USER_RECIPE_ID);
    expect(component.recipe()?.userDefined).toBeTrue();
    expect(component.optionFields()).toEqual([]);
    expect(component.userOptionSummaries()).toEqual([
      { key: 'targetSizeBytes', label: 'Target size', value: '5 MB' },
    ]);

    // The page must not offer to edit what the recipe already stores.
    expect(query('#recipe-option-targetSizeBytes')).toBeNull();
    expect(query('.settings-grid input')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain(
      'This recipe saves these values. Edit the recipe to change them.',
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const planRequest = wailsSpy.planRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(planRequest.recipeId).toBe(USER_RECIPE_ID);
    expect(planRequest.options).toEqual({});

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    const runRequest = wailsSpy.runRecipe.calls.mostRecent()
      .args[0] as RecipeRequest;
    expect(runRequest.recipeId).toBe(USER_RECIPE_ID);
    expect(runRequest.options).toEqual({});
    expect(component.terminalResult()?.recipeId).toBe(USER_RECIPE_ID);

    component.resetRun();
    fixture.detectChanges();
  }));
});

describe('RecipeRun (generic option schema)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function emailPlan(request: RecipeRequest): RecipePlanResponse {
    return {
      success: true,
      message: 'Ready to email 1 PDF.',
      recipe: emailPdfRecipe,
      inputPaths: request.inputPaths,
      outputDir: '/tmp',
      outputs: [
        {
          inputPath: request.inputPaths[0] ?? '',
          outputPath: '/tmp/a_email.pdf',
          outputKind: 'file',
          outputCount: 1,
          exists: false,
          adjusted: false,
        },
      ],
      warnings: [],
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(emailPlan(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => ({
          path,
          name: path.split(/[\\/]/).pop() ?? path,
          directory: '/tmp',
          extension: 'pdf',
          sizeBytes: 1024,
          kind: 'pdf',
          exists: true,
        })),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'email-job-1',
      status: 'queued',
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(EMAIL_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('renders the byte-based target default in MB with declared bounds', () => {
    const input = query<HTMLInputElement>('#recipe-option-targetSizeBytes');

    expect(input).not.toBeNull();
    expect(input?.value).toBe('10');
    expect(input?.getAttribute('min')).toBe('1');
    expect(input?.getAttribute('max')).toBe('100');
    expect(input?.getAttribute('step')).toBe('1');

    const suffix = fixture.nativeElement.querySelector(
      '#recipe-option-targetSizeBytes + .input-suffix-label',
    ) as HTMLElement | null;
    expect(suffix?.textContent?.trim()).toBe('MB');
    expect(fixture.nativeElement.textContent).toContain(
      'Best effort: the output may stay above the target.',
    );
  });

  it('re-plans after a short debounce and sends canonical bytes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ targetSizeBytes: TARGET_BYTES });

    const input = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;
    input.value = '5';
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    // Nothing is planned before the debounce elapses.
    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);
    tick(249);
    flushMicrotasks();
    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(1);

    tick(1);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(2);
    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ targetSizeBytes: 5 * 1024 * 1024 });
  }));

  it('blocks a fractional or out-of-range integer with an inline message', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const input = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;

    input.value = '2.5';
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    expect(component.optionErrors()['targetSizeBytes']).toContain(
      'whole number',
    );
    expect(component.canRun()).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain(
      'Target size must be a whole number (MB).',
    );

    input.value = '500';
    input.dispatchEvent(new Event('input'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.optionErrors()['targetSizeBytes']).toBe(
      'Target size must be between 1 and 100 MB.',
    );
    expect(component.canRun()).toBeFalse();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runRecipe).not.toHaveBeenCalled();
  }));
});

describe('RecipeRun (built-in enum option)', () => {
  const ENUM_OUTPUTS = ['/tmp/in/hero-1600.webp'];

  const enumRecipe: Recipe = {
    ...webImageSetRecipe,
    defaultTargetSizeBytes: 0,
    optionSchema: [
      {
        key: 'variantPreset',
        label: 'Variant preset',
        kind: 'enum' as RecipeOptionKind,
        default: 'standard',
        values: [
          { value: 'standard', label: '1600 / 800 px (default)' },
          { value: 'compact', label: '1200 / 600 px' },
          { value: 'large', label: '2400 / 1200 px' },
        ],
        help: 'Widths are capped at the source width; images are never upscaled.',
      },
    ],
  };

  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, enumRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve({
        success: true,
        message: 'Ready to build 1 file.',
        recipe: enumRecipe,
        inputPaths: request.inputPaths,
        outputDir: '/tmp/in',
        outputs: ENUM_OUTPUTS.map((path) => ({
          inputPath: request.inputPaths[0] ?? '',
          outputPath: path,
          outputKind: 'file',
          exists: false,
          adjusted: false,
        })),
        warnings: [],
      }),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => ({
          path,
          name: path.split(/[\\/]/).pop() ?? path,
          directory: '/tmp/in',
          extension: 'png',
          sizeBytes: 1024,
          kind: 'image',
          exists: true,
        })),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'enum-job-1',
      status: 'queued',
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(WEB_RECIPE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('renders a select with the declared values and sends the chosen value', fakeAsync(() => {
    const select = query<HTMLSelectElement>(
      '#recipe-option-variantPreset',
    ) as HTMLSelectElement;

    expect(select).not.toBeNull();
    expect(select.value).toBe('standard');
    expect(select.querySelectorAll('option').length).toBe(3);
    expect(fixture.nativeElement.textContent).toContain('1200 / 600 px');

    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ variantPreset: 'standard' });

    select.value = 'compact';
    select.dispatchEvent(new Event('change'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(2);
    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ variantPreset: 'compact' });
  }));
});

describe('RecipeRun (built-in boolean option)', () => {
  const ARCHIVE_ID = 'recipe.archive_document';
  const ARCHIVE_OUTPUTS = ['/tmp/in/contract_archived.pdf'];

  const archiveRecipe: Recipe = {
    id: ARCHIVE_ID,
    name: 'Archive document',
    description:
      'Turn a DOCX into a PDF/A-2 file and report its SHA-256 for long-term storage.',
    category: 'documents',
    inputKind: 'document',
    acceptExtensions: ['docx'],
    maxInputs: 1,
    highlights: ['PDF/A-2', 'SHA-256 checksum', 'Optional sidecar'],
    steps: [
      {
        id: 'render',
        name: 'Render PDF/A-2',
        toolId: 'tool.doc.docx_to_pdf',
        detail: 'Bundled DOCX renderer, then Ghostscript distils the PDF',
      },
      {
        id: 'verify',
        name: 'Hash and verify',
        toolId: '',
        detail: 'PDF signature, PDF/A marker, page count and SHA-256',
      },
    ],
    defaultTargetSizeBytes: 0,
    duplicable: true,
    optionSchema: [
      {
        key: 'writeSidecar',
        label: 'SHA-256 sidecar',
        kind: 'boolean' as RecipeOptionKind,
        default: false,
        help: 'Write <pdf>.sha256 next to the PDF with a `hash  filename` line.',
      },
    ],
  };

  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function archivePlan(request: RecipeRequest): RecipePlanResponse {
    const options = request.options ?? {};
    const outputs = options['writeSidecar']
      ? [...ARCHIVE_OUTPUTS, '/tmp/in/contract_archived.pdf.sha256']
      : [...ARCHIVE_OUTPUTS];

    return {
      success: true,
      message: 'Ready to archive contract.docx as PDF/A-2.',
      recipe: archiveRecipe,
      inputPaths: request.inputPaths,
      outputDir: '/tmp/in',
      outputs: outputs.map((path) => ({
        inputPath: request.inputPaths[0] ?? '',
        outputPath: path,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  beforeEach(async () => {
    clearPreferences();
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, archiveRecipe],
    });
    wailsSpy.planRecipe.and.callFake((request: RecipeRequest) =>
      Promise.resolve(archivePlan(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => ({
          path,
          name: path.split(/[\\/]/).pop() ?? path,
          directory: '/tmp/in',
          extension: 'docx',
          sizeBytes: 4096,
          kind: 'document',
          exists: true,
        })),
      }),
    );
    wailsSpy.runRecipe.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'archive-job-1',
      status: 'queued',
    });
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeRun],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ActivatedRoute, useValue: createRoute(ARCHIVE_ID) },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeRun);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    clearPreferences();
  });

  it('renders the checkbox off by default and omits the key until ticked', fakeAsync(() => {
    const checkbox = query<HTMLInputElement>(
      '#recipe-option-writeSidecar',
    ) as HTMLInputElement;
    expect(checkbox).not.toBeNull();
    expect(checkbox.checked).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain('SHA-256 sidecar');

    component.onPathsSelected(['/tmp/in/contract.docx']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({});

    checkbox.checked = true;
    checkbox.dispatchEvent(new Event('change'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planRecipe).toHaveBeenCalledTimes(2);
    expect(
      (wailsSpy.planRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ writeSidecar: true });

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      (wailsSpy.runRecipe.calls.mostRecent().args[0] as RecipeRequest).options,
    ).toEqual({ writeSidecar: true });
  }));

  it('sends the sidecar in Spanish and translates the new copy', fakeAsync(() => {
    const i18n = TestBed.inject(I18nService);
    i18n.setLocale('es');
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Archivo SHA-256');
    expect(text).toContain(
      'Escribe <pdf>.sha256 junto al PDF con una línea `hash  nombre`.',
    );
    expect(text).toContain('Ajustes');
    expect(text).toContain(
      'Define las opciones de la receta y dónde se guardan los resultados.',
    );

    i18n.setLocale('en');
  }));
});
