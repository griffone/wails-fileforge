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
    expect(text).toContain('Variants and formats are fixed by the recipe.');
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

  it('shows live recipe progress from recipes/progress/v1 while running', fakeAsync(() => {
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
