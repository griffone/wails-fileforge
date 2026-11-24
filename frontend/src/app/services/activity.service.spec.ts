import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ActivityService, ActivityTask } from './activity.service';
import { I18nService } from './i18n.service';
import { ToastService } from './toast.service';
import { ToolRegistryService } from './tool-registry.service';
import {
  JobProgressEvent,
  JobResult,
  RecipeProgressEvent,
  RecipeResult,
  Wails,
} from './wails';

function jobSnapshot(overrides: Partial<JobResult> = {}): JobResult {
  return {
    jobId: 'job-1',
    success: false,
    message: '',
    toolId: 'tool.pdf.merge',
    status: 'running',
    progress: { current: 0, total: 10, stage: 'processing', message: '' },
    items: [],
    startedAt: 1000,
    ...overrides,
  };
}

function recipeSnapshot(
  overrides: Partial<RecipeResult> = {},
): RecipeResult {
  return {
    jobId: 'recipe-job-1',
    recipeId: 'recipe.email_pdf',
    status: 'running',
    message: '',
    currentStep: 1,
    totalSteps: 2,
    steps: [],
    outputs: [],
    startedAt: 1000,
    ...overrides,
  };
}

function activityTask(overrides: Partial<ActivityTask> = {}): ActivityTask {
  return {
    id: 'job-missing',
    kind: 'tool',
    title: 'Merge',
    route: '/pdf-merge',
    toolId: 'tool.pdf.merge',
    status: 'running',
    stage: 'processing',
    message: '',
    current: 0,
    total: 0,
    percent: null,
    startedAt: 1,
    endedAt: null,
    etaSeconds: null,
    inputCount: 0,
    outputCount: 0,
    errorMessage: '',
    ...overrides,
  };
}

describe('ActivityService', () => {
  let service: ActivityService;
  let registry: ToolRegistryService;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let toastSpy: jasmine.SpyObj<ToastService>;
  let hasFocus: jasmine.Spy;

  let jobState$: Subject<JobResult>;
  let recipeState$: Subject<RecipeResult>;
  let jobProgress$: Subject<JobProgressEvent>;
  let recipeProgress$: Subject<RecipeProgressEvent>;

  beforeEach(() => {
    localStorage.setItem('ff-preferences', JSON.stringify({ language: 'en' }));

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listJobs',
      'listRecipeRuns',
      'cancelJob',
      'cancelRecipe',
      'notify',
    ]);
    wailsSpy.listJobs.and.resolveTo({
      success: true,
      message: 'ok',
      jobs: [],
    });
    wailsSpy.listRecipeRuns.and.resolveTo([]);
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'job-1',
    });
    wailsSpy.cancelRecipe.and.resolveTo({
      success: true,
      found: true,
      message: 'cancel requested',
    });
    wailsSpy.notify.and.resolveTo({ success: true, message: 'sent' });

    jobState$ = new Subject<JobResult>();
    recipeState$ = new Subject<RecipeResult>();
    jobProgress$ = new Subject<JobProgressEvent>();
    recipeProgress$ = new Subject<RecipeProgressEvent>();

    const streams = wailsSpy as unknown as {
      jobState$: Subject<JobResult>;
      recipeState$: Subject<RecipeResult>;
      jobProgress$: Subject<JobProgressEvent>;
      recipeProgress$: Subject<RecipeProgressEvent>;
    };
    streams.jobState$ = jobState$;
    streams.recipeState$ = recipeState$;
    streams.jobProgress$ = jobProgress$;
    streams.recipeProgress$ = recipeProgress$;

    toastSpy = jasmine.createSpyObj<ToastService>('ToastService', [
      'push',
      'dismiss',
      'clear',
    ]);
    toastSpy.push.and.returnValue(1);

    hasFocus = spyOn(document, 'hasFocus').and.returnValue(true);

    TestBed.configureTestingModule({
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ToastService, useValue: toastSpy },
      ],
    });

    registry = TestBed.inject(ToolRegistryService);
    service = TestBed.inject(ActivityService);
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
  });

  it('creates a task from a full snapshot with title, route and progress', () => {
    jobState$.next(
      jobSnapshot({
        progress: {
          current: 1,
          total: 4,
          stage: 'processing',
          message: 'Merging',
        },
      }),
    );

    const definition = registry.findById('tool.pdf.merge');
    const task = service.tasks()[0];

    expect(service.tasks().length).toBe(1);
    expect(task.id).toBe('job-1');
    expect(task.title).toBe(definition?.navName ?? '');
    expect(task.route).toBe('/pdf-merge');
    expect(task.status).toBe('running');
    expect(task.current).toBe(1);
    expect(task.total).toBe(4);
    expect(task.percent).toBe(25);
    expect(task.etaSeconds).toBeNull();
    expect(service.runningCount()).toBe(1);
  });

  it('pushes one toast per terminal transition and notifies natively only when unfocused', () => {
    hasFocus.and.returnValue(false);
    const finished = jobSnapshot({
      jobId: 'job-a',
      status: 'success',
      success: true,
      message: 'Merged 2 files',
      progress: { current: 10, total: 10, stage: 'done', message: 'Merged 2 files' },
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/a-out.pdf',
          outputs: ['/tmp/a-out.pdf'],
          success: true,
          message: 'done',
        },
        {
          inputPath: '/tmp/b.pdf',
          outputPath: '/tmp/b-out.pdf',
          outputs: ['/tmp/b-out.pdf'],
          success: true,
          message: 'done',
        },
      ],
      endedAt: 2000,
    });

    jobState$.next(jobSnapshot({ jobId: 'job-a' }));
    jobState$.next(finished);

    expect(toastSpy.push).toHaveBeenCalledTimes(1);
    const toast = toastSpy.push.calls.mostRecent().args[0];
    expect(toast.tone).toBe('success');
    expect(toast.title).toBe('Merge finished');
    expect(toast.body).toBe('Merged 2 files');
    expect(toast.actionLabel).toBe('View activity');
    expect(wailsSpy.notify).toHaveBeenCalledOnceWith(
      'Merge finished',
      'Merged 2 files',
    );

    jobState$.next(finished);
    expect(toastSpy.push).toHaveBeenCalledTimes(1);
    expect(wailsSpy.notify).toHaveBeenCalledTimes(1);

    hasFocus.and.returnValue(true);
    const failed = jobSnapshot({
      jobId: 'job-b',
      status: 'failed',
      message: '',
      progress: { current: 0, total: 1, stage: 'failed', message: '' },
      error: { code: 'EXEC_IO_TRANSIENT', message: 'disk full' },
      endedAt: 3000,
    });
    jobState$.next(jobSnapshot({ jobId: 'job-b' }));
    jobState$.next(failed);

    expect(toastSpy.push).toHaveBeenCalledTimes(2);
    const failureToast = toastSpy.push.calls.mostRecent().args[0];
    expect(failureToast.tone).toBe('danger');
    expect(failureToast.title).toBe('Merge failed');
    expect(failureToast.body).toBe('disk full');
    expect(wailsSpy.notify).toHaveBeenCalledTimes(1);
  });

  it('merges progress events into the existing task without duplicating it', () => {
    jobState$.next(jobSnapshot({ jobId: 'job-3' }));
    jobProgress$.next({
      jobId: 'job-3',
      toolId: 'tool.pdf.merge',
      status: 'processing',
      progress: {
        current: 5,
        total: 10,
        stage: 'processing',
        message: 'Half way',
        etaSeconds: 12,
      },
    });

    expect(service.tasks().length).toBe(1);
    const task = service.tasks()[0];
    expect(task.current).toBe(5);
    expect(task.total).toBe(10);
    expect(task.percent).toBe(50);
    expect(task.etaSeconds).toBe(12);
    expect(task.message).toBe('Half way');
  });

  it('keeps the recipe event percent when a later snapshot omits it', () => {
    recipeState$.next(recipeSnapshot());
    recipeProgress$.next({
      jobId: 'recipe-job-1',
      recipeId: 'recipe.email_pdf',
      status: 'processing',
      currentStep: 1,
      totalSteps: 2,
      stepName: 'Optimize',
      stage: 'optimize',
      message: 'Optimizing',
      percent: 70,
    });

    expect(service.tasks()[0].percent).toBe(70);

    recipeState$.next(recipeSnapshot({ message: 'Optimizing' }));

    const task = service.tasks()[0];
    expect(service.tasks().length).toBe(1);
    expect(task.title).toBe('Email PDF');
    expect(task.route).toBe('/recipe/email-pdf');
    expect(task.percent).toBe(70);
  });

  it('returns the running job for a tool and ignores terminal ones', () => {
    jobState$.next(
      jobSnapshot({
        jobId: 'job-done',
        status: 'success',
        success: true,
        progress: { current: 10, total: 10, stage: 'done', message: 'done' },
        endedAt: 1500,
      }),
    );
    jobState$.next(
      jobSnapshot({ jobId: 'job-running', status: 'running', startedAt: 2000 }),
    );

    expect(service.activeJobForTool('tool.pdf.merge')?.id).toBe('job-running');

    jobState$.next(
      jobSnapshot({
        jobId: 'job-running',
        status: 'cancelled',
        startedAt: 2000,
        endedAt: 2500,
      }),
    );

    expect(service.activeJobForTool('tool.pdf.merge')).toBeNull();
  });

  it('translates the task title and completion toast in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');

    jobState$.next(jobSnapshot({ jobId: 'job-es' }));

    expect(service.tasks()[0].title).toBe('Unir');

    jobState$.next(
      jobSnapshot({
        jobId: 'job-es',
        status: 'success',
        success: true,
        message: '',
        progress: { current: 2, total: 2, stage: 'done', message: '' },
        items: [
          {
            inputPath: '/tmp/a.pdf',
            outputPath: '/tmp/a-out.pdf',
            outputs: ['/tmp/a-out.pdf'],
            success: true,
            message: 'ok',
          },
          {
            inputPath: '/tmp/b.pdf',
            outputPath: '/tmp/b-out.pdf',
            outputs: ['/tmp/b-out.pdf'],
            success: true,
            message: 'ok',
          },
        ],
        endedAt: 2000,
      }),
    );

    const toast = toastSpy.push.calls.mostRecent().args[0];
    expect(toast.title).toBe('Unir finalizado');
    expect(toast.body).toBe('2 archivos listos');
    expect(toast.actionLabel).toBe('Ver actividad');
  });

  it('uses the translated recipe name for a task title in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');

    recipeState$.next(recipeSnapshot());

    expect(service.tasks()[0].title).toBe('PDF por email');
  });

  it('translates cancel failures in Spanish', async () => {
    TestBed.inject(I18nService).setLocale('es');
    wailsSpy.cancelJob.and.rejectWith(new Error('boom'));

    jobState$.next(jobSnapshot({ jobId: 'job-cancel-es' }));

    const message = await service.cancelTask(service.tasks()[0]);
    const toast = toastSpy.push.calls.mostRecent().args[0];

    expect(message).toBe('boom');
    expect(toast.title).toBe('No se pudo cancelar');
    expect(toast.body).toBe('boom');
  });

  it('treats terminal tasks found by refresh as already notified', async () => {
    const finished = jobSnapshot({
      jobId: 'job-old',
      status: 'success',
      success: true,
      progress: { current: 1, total: 1, stage: 'done', message: 'Done' },
      endedAt: 2000,
    });
    wailsSpy.listJobs.and.resolveTo({
      success: true,
      message: 'ok',
      jobs: [finished],
    });
    wailsSpy.listRecipeRuns.and.resolveTo([]);

    await service.refresh();

    expect(wailsSpy.listJobs).toHaveBeenCalledWith(50);
    expect(wailsSpy.listRecipeRuns).toHaveBeenCalledWith(50);
    expect(service.tasks().length).toBe(1);
    expect(toastSpy.push).not.toHaveBeenCalled();
    expect(wailsSpy.notify).not.toHaveBeenCalled();

    jobState$.next(finished);

    expect(toastSpy.push).not.toHaveBeenCalled();
    expect(wailsSpy.notify).not.toHaveBeenCalled();
  });
});

describe('ActivityService with a partial Wails double', () => {
  beforeEach(() => {
    localStorage.setItem('ff-preferences', JSON.stringify({ language: 'en' }));
  });

  afterEach(() => {
    localStorage.removeItem('ff-preferences');
  });

  it('constructs and stays idle without the new streams or methods', async () => {
    const partial = jasmine.createSpyObj<Wails>('Wails', ['listTools']);

    TestBed.configureTestingModule({
      providers: [{ provide: Wails, useValue: partial }],
    });

    const activity = TestBed.inject(ActivityService);

    expect(activity.tasks()).toEqual([]);
    expect(activity.runningCount()).toBe(0);

    await expectAsync(activity.refresh()).toBeResolved();
    expect(activity.tasks()).toEqual([]);

    const message = await activity.cancelTask(activityTask());
    expect(message).toContain('not available');
  });
});
