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
import {
  FileInfoV1,
  RecipePlanResponseV1,
  RecipeProgressEventV1,
  RecipeRequestV1,
  RecipeResultV1,
  RecipeStatusResponseV1,
  RecipeV1,
  Wails,
} from '../../services/wails';

const EMAIL_RECIPE_ID = 'recipe.email_pdf';
const WEB_RECIPE_ID = 'recipe.web_image_set';
const SOCIAL_RECIPE_ID = 'recipe.social_clip';
const CLEAN_SCAN_RECIPE_ID = 'recipe.clean_scan';
const TARGET_BYTES = 10 * 1024 * 1024;

const emailPdfRecipe: RecipeV1 = {
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
};

const webImageSetRecipe: RecipeV1 = {
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
};

const WEB_OUTPUTS = [
  '/tmp/out/hero-1600.webp',
  '/tmp/out/hero-1600.jpg',
  '/tmp/out/hero-800.webp',
  '/tmp/out/hero-800.jpg',
  '/tmp/out/hero-web-set.zip',
  '/tmp/out/hero-srcset.html',
];

const socialClipRecipe: RecipeV1 = {
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
};

const SOCIAL_OUTPUTS = [
  '/tmp/in/clip-social.mp4',
  '/tmp/in/clip-social.webm',
  '/tmp/in/clip-poster.jpg',
];

const cleanScanRecipe: RecipeV1 = {
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
};

const CLEAN_SCAN_OUTPUTS = [
  '/tmp/in/report_clean.pdf',
  '/tmp/in/report_clean.txt',
];

function statusResponse(result: RecipeResultV1): RecipeStatusResponseV1 {
  return { success: true, found: true, message: result.status, result };
}

function inputEvent(value: string): Event {
  return { target: { value } } as unknown as Event;
}

function createWailsSpy(): jasmine.SpyObj<Wails> {
  const spy = jasmine.createSpyObj<Wails>('Wails', [
    'listRecipesV1',
    'planRecipeV1',
    'inspectFilesV1',
    'runRecipeV1',
    'getRecipeStatusV1',
    'cancelRecipeV1',
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

describe('RecipeRun (Email PDF)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function fileInfo(path: string): FileInfoV1 {
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

  function planResponse(request: RecipeRequestV1): RecipePlanResponseV1 {
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
    overrides: Partial<RecipeResultV1> = {},
  ): RecipeResultV1 {
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

  function runningRecipeResult(): RecipeResultV1 {
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

  function cancelledRecipeResult(): RecipeResultV1 {
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
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe],
    });
    wailsSpy.planRecipeV1.and.callFake((request: RecipeRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.runRecipeV1.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'recipe-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatusV1.and.resolveTo(
      statusResponse(runningRecipeResult()),
    );
    wailsSpy.cancelRecipeV1.and.resolveTo({
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
    expect(component.targetMb()).toBe(10);
    expect(component.showTargetSize()).toBeTrue();
    expect(component.multipleInputs()).toBeTrue();
    expect(query<HTMLInputElement>('#recipe-target-mb')?.value).toBe('10');
    expect(fixture.nativeElement.textContent).toContain('Email PDF');
    expect(fixture.nativeElement.textContent).toContain('Optimize to target');
    expect(fixture.nativeElement.textContent).toContain('Target size');
  });

  it('keeps the local fallback when the recipe catalog is unavailable', async () => {
    wailsSpy.listRecipesV1.and.resolveTo({
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
      (fresh.nativeElement.querySelector('#recipe-target-mb') as HTMLInputElement)
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

    expect(wailsSpy.planRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(EMAIL_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/a.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({ targetSizeBytes: TARGET_BYTES });
    expect(component.plan()?.success).toBeTrue();
    expect(component.planBlocked()).toBeFalse();
  }));

  it('runs, polls to terminal and shows the verified result', fakeAsync(() => {
    wailsSpy.getRecipeStatusV1.and.resolveTo(statusResponse(recipeResult()));

    component.onPathsSelected(['/tmp/a.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(EMAIL_RECIPE_ID);
    expect(request.options).toEqual({ targetSizeBytes: TARGET_BYTES });
    expect(wailsSpy.getRecipeStatusV1).toHaveBeenCalledWith('recipe-job-1');
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.toolId).toBe(EMAIL_RECIPE_ID);
    expect(component.resultJob()?.items.length).toBe(1);
    expect(component.verification()?.success).toBeTrue();

    const text = fixture.nativeElement.textContent as string;
    expect(query('app-result-card')).not.toBeNull();
    expect(text).toContain('1 PDF prepared');
    expect(text).toContain('Verified');
    expect(text).toContain('2/2 checks passed');
  }));

  it('shows the partial result message instead of the retry copy', fakeAsync(() => {
    wailsSpy.getRecipeStatusV1.and.resolveTo(
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
    expect(component.resultJob()?.items.length).toBe(1);
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

  it('cancels a running recipe and keeps polling to terminal', fakeAsync(() => {
    let pollCount = 0;
    wailsSpy.getRecipeStatusV1.and.callFake(() => {
      pollCount += 1;
      return Promise.resolve(
        statusResponse(
          pollCount === 1 ? runningRecipeResult() : cancelledRecipeResult(),
        ),
      );
    });

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

    expect(wailsSpy.cancelRecipeV1).toHaveBeenCalledWith('recipe-job-1');
    expect(component.cancelRequested()).toBeTrue();
    expect(
      query<HTMLButtonElement>('.action-row .btn-ghost')?.textContent,
    ).toContain('Cancelling…');

    tick(1000);
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

    component.onTargetInput(inputEvent('0'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.targetInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(
      (query<HTMLButtonElement>('.action-primary') as HTMLButtonElement).disabled,
    ).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'Enter a target size larger than 0 MB.',
    );

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runRecipeV1).not.toHaveBeenCalled();
  }));
});

describe('RecipeRun (Web image set)', () => {
  let fixture: ComponentFixture<RecipeRun>;
  let component: RecipeRun;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let recipeProgressSubject: Subject<RecipeProgressEventV1>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function fileInfo(path: string): FileInfoV1 {
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

  function planResponse(request: RecipeRequestV1): RecipePlanResponseV1 {
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

  function webRecipeResult(): RecipeResultV1 {
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

  function runningWebRecipeResult(): RecipeResultV1 {
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
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe],
    });
    wailsSpy.planRecipeV1.and.callFake((request: RecipeRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    recipeProgressSubject = new Subject<RecipeProgressEventV1>();
    (wailsSpy as unknown as { recipeProgress$: unknown }).recipeProgress$ =
      recipeProgressSubject.asObservable();

    wailsSpy.runRecipeV1.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'web-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatusV1.and.resolveTo(
      statusResponse(webRecipeResult()),
    );
    wailsSpy.cancelRecipeV1.and.resolveTo({
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
    expect(component.showTargetSize()).toBeFalse();
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-target-mb')).toBeNull();

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

    expect(wailsSpy.planRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(WEB_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/hero.png']);
  }));

  it('plans without a target size option', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.options).toEqual({});
    expect('targetSizeBytes' in request.options).toBeFalse();
    expect(component.targetBytes()).toBe(0);
  }));

  it('shows live recipe progress from recipes/progress while running', fakeAsync(() => {
    wailsSpy.getRecipeStatusV1.and.resolveTo(
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
    // Before the event the polled snapshot marks step 2 as active.
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

    // Stop the polling interval before fakeAsync verifies pending timers.
    component.resetRun();
    fixture.detectChanges();
  }));

  it('runs, polls to terminal and lists all six verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/hero.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(WEB_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items.length).toBe(6);
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

  function videoFileInfo(path: string): FileInfoV1 {
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

  function planResponse(request: RecipeRequestV1): RecipePlanResponseV1 {
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

  function socialRecipeResult(): RecipeResultV1 {
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
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe, webImageSetRecipe, socialClipRecipe],
    });
    wailsSpy.planRecipeV1.and.callFake((request: RecipeRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(videoFileInfo),
      }),
    );
    wailsSpy.runRecipeV1.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'social-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatusV1.and.resolveTo(
      statusResponse(socialRecipeResult()),
    );
    wailsSpy.cancelRecipeV1.and.resolveTo({
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
    expect(component.showTargetSize()).toBeFalse();
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-target-mb')).toBeNull();

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

    expect(wailsSpy.planRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(SOCIAL_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/clip.mp4']);
    expect(request.options).toEqual({});
    expect(component.targetBytes()).toBe(0);
  }));

  it('runs, polls to terminal and lists the three verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/clip.mp4']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(SOCIAL_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items.length).toBe(3);
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

  function cleanScanFileInfo(path: string): FileInfoV1 {
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

  function planResponse(request: RecipeRequestV1): RecipePlanResponseV1 {
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

  function cleanScanResult(): RecipeResultV1 {
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
    wailsSpy = createWailsSpy();

    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [
        emailPdfRecipe,
        webImageSetRecipe,
        socialClipRecipe,
        cleanScanRecipe,
      ],
    });
    wailsSpy.planRecipeV1.and.callFake((request: RecipeRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(cleanScanFileInfo),
      }),
    );
    wailsSpy.runRecipeV1.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'clean-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatusV1.and.resolveTo(
      statusResponse(cleanScanResult()),
    );
    wailsSpy.cancelRecipeV1.and.resolveTo({
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
    expect(component.showTargetSize()).toBeFalse();
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-target-mb')).toBeNull();
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

    expect(wailsSpy.planRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(request.inputPaths).toEqual(['/tmp/in/report.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({});
    expect(component.targetBytes()).toBe(0);
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual(
      CLEAN_SCAN_OUTPUTS,
    );
  }));

  it('runs, polls to terminal and lists both verified outputs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipeV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(request.recipeId).toBe(CLEAN_SCAN_RECIPE_ID);
    expect(request.options).toEqual({});
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.items.length).toBe(2);
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

  function videoFileInfo(path: string): FileInfoV1 {
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

  function planResponse(request: RecipeRequestV1): RecipePlanResponseV1 {
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

  function burnedResult(): RecipeResultV1 {
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
    wailsSpy = createWailsSpy();

    // The catalog predates the new recipe: the local fallback must cover it.
    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [
        emailPdfRecipe,
        webImageSetRecipe,
        socialClipRecipe,
        cleanScanRecipe,
      ],
    });
    wailsSpy.planRecipeV1.and.callFake((request: RecipeRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(videoFileInfo),
      }),
    );
    wailsSpy.runRecipeV1.and.resolveTo({
      success: true,
      message: 'recipe submitted',
      jobId: 'burned-job-1',
      status: 'queued',
    });
    wailsSpy.getRecipeStatusV1.and.resolveTo(statusResponse(burnedResult()));
    wailsSpy.cancelRecipeV1.and.resolveTo({
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
    expect(component.showTargetSize()).toBeFalse();
    expect(component.multipleInputs()).toBeFalse();
    expect(query('#recipe-target-mb')).toBeNull();

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

    expect(wailsSpy.planRecipeV1).toHaveBeenCalledTimes(1);
    const planRequest = wailsSpy.planRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(planRequest.recipeId).toBe(BURNED_RECIPE_ID);
    expect(planRequest.inputPaths).toEqual(['/tmp/in/lecture.mp4']);
    expect(planRequest.options).toEqual({});
    expect(component.targetBytes()).toBe(0);

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runRecipeV1).toHaveBeenCalledTimes(1);
    const runRequest = wailsSpy.runRecipeV1.calls.mostRecent()
      .args[0] as RecipeRequestV1;
    expect(runRequest.recipeId).toBe(BURNED_RECIPE_ID);
    expect(runRequest.options).toEqual({});

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('lecture_subtitled.mp4');
    expect(text).toContain('lecture_subtitled.srt');
    expect(text).toContain('Verified');
  }));
});

