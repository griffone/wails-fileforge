import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { RecipeEmailPdf } from './recipe-email-pdf';
import {
  FileInfoV1,
  RecipePlanResponseV1,
  RecipeRequestV1,
  RecipeResultV1,
  RecipeStatusResponseV1,
  RecipeV1,
  Wails,
} from '../../services/wails';

const RECIPE_ID = 'recipe.email_pdf';
const TARGET_BYTES = 10 * 1024 * 1024;

const emailPdfRecipe: RecipeV1 = {
  id: RECIPE_ID,
  name: 'Email PDF',
  description:
    'Merge one or more PDFs and compress the result into one file that fits an email limit.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  steps: [
    { id: 'merge', name: 'Merge PDFs', toolId: 'tool.pdf.merge' },
    { id: 'optimize', name: 'Optimize to target', toolId: 'tool.pdf.optimize' },
  ],
  defaultTargetSizeBytes: TARGET_BYTES,
};

describe('RecipeEmailPdf', () => {
  let fixture: ComponentFixture<RecipeEmailPdf>;
  let component: RecipeEmailPdf;
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
      recipeId: RECIPE_ID,
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

  function statusResponse(result: RecipeResultV1): RecipeStatusResponseV1 {
    return { success: true, found: true, message: result.status, result };
  }

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listRecipesV1',
      'planRecipeV1',
      'inspectFilesV1',
      'runRecipeV1',
      'getRecipeStatusV1',
      'cancelRecipeV1',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'openPath',
      'revealInFolder',
    ]);

    wailsSpy.listRecipesV1.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [emailPdfRecipe],
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
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [RecipeEmailPdf],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(RecipeEmailPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('loads the curated recipe and shows the default 10 MB target', async () => {
    await fixture.whenStable();
    fixture.detectChanges();

    expect(component.recipe()?.id).toBe(RECIPE_ID);
    expect(component.targetMb()).toBe(10);
    expect(query<HTMLInputElement>('#recipe-target-mb')?.value).toBe('10');
    expect(fixture.nativeElement.textContent).toContain('Email PDF');
    expect(fixture.nativeElement.textContent).toContain('Optimize to target');
  });

  it('keeps the local fallback when the recipe catalog is unavailable', async () => {
    wailsSpy.listRecipesV1.and.resolveTo({
      success: false,
      message: 'ipc error',
      recipes: [],
    });

    const fresh = TestBed.createComponent(RecipeEmailPdf);
    fresh.detectChanges();
    await fresh.whenStable();
    fresh.detectChanges();

    expect(fresh.componentInstance.recipe()?.id).toBe(RECIPE_ID);
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
    expect(request.recipeId).toBe(RECIPE_ID);
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
    expect(request.recipeId).toBe(RECIPE_ID);
    expect(request.options).toEqual({ targetSizeBytes: TARGET_BYTES });
    expect(wailsSpy.getRecipeStatusV1).toHaveBeenCalledWith('recipe-job-1');
    expect(component.terminalResult()?.status).toBe('success');
    expect(component.resultJob()?.toolId).toBe(RECIPE_ID);
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
