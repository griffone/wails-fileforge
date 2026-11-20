import { Subject } from 'rxjs';

import { observeJob, observeRecipe } from './job-stream';
import {
  JobProgressEvent,
  JobResult,
  JobStatusResponse,
  RecipeProgressEvent,
  RecipeResult,
  RecipeStatusResponse,
  Wails,
} from '../services/wails';

function jobResult(jobId: string, status: JobResult['status']): JobResult {
  return {
    jobId,
    success: status === 'success',
    message: status,
    toolId: 'tool.pdf.merge',
    status,
    progress: { current: 1, total: 2, stage: status, message: status },
    items: [],
    startedAt: 1,
    endedAt: status === 'running' ? undefined : 2,
  };
}

function jobStatus(result: JobResult): JobStatusResponse {
  return { success: true, found: true, message: result.status, result };
}

function recipeResult(
  jobId: string,
  status: RecipeResult['status'],
): RecipeResult {
  return {
    jobId,
    recipeId: 'recipe.email_pdf',
    status,
    message: status,
    currentStep: 1,
    totalSteps: 2,
    steps: [],
    outputs: [],
    startedAt: 1,
    endedAt: status === 'running' ? undefined : 2,
  };
}

function recipeStatus(result: RecipeResult): RecipeStatusResponse {
  return { success: true, found: true, message: result.status, result };
}

interface FakeWails {
  wails: Wails;
  progress: Subject<JobProgressEvent>;
  state: Subject<JobResult>;
  recipeProgress: Subject<RecipeProgressEvent>;
  recipeState: Subject<RecipeResult>;
  getJobStatus: jasmine.Spy;
  getRecipeStatus: jasmine.Spy;
}

function fakeWails(): FakeWails {
  const progress = new Subject<JobProgressEvent>();
  const state = new Subject<JobResult>();
  const recipeProgress = new Subject<RecipeProgressEvent>();
  const recipeState = new Subject<RecipeResult>();
  const getJobStatus = jasmine.createSpy('getJobStatus');
  const getRecipeStatus = jasmine.createSpy('getRecipeStatus');

  const wails = {
    jobProgress$: progress.asObservable(),
    jobState$: state.asObservable(),
    recipeProgress$: recipeProgress.asObservable(),
    recipeState$: recipeState.asObservable(),
    getJobStatus,
    getRecipeStatus,
  } as unknown as Wails;

  return {
    wails,
    progress,
    state,
    recipeProgress,
    recipeState,
    getJobStatus,
    getRecipeStatus,
  };
}

describe('observeJob', () => {
  it('forwards progress and state events for the observed job only', () => {
    const fake = fakeWails();
    fake.getJobStatus.and.resolveTo(jobStatus(jobResult('job-1', 'running')));
    const progress = jasmine.createSpy('onProgress');
    const state = jasmine.createSpy('onState');

    observeJob(fake.wails, 'job-1', { onProgress: progress, onState: state });

    fake.progress.next({
      jobId: 'job-2',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: { current: 1, total: 2, stage: 'other', message: 'other' },
    });
    fake.state.next(jobResult('job-2', 'success'));

    expect(progress).not.toHaveBeenCalled();
    expect(state).not.toHaveBeenCalled();

    fake.progress.next({
      jobId: 'job-1',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: { current: 1, total: 2, stage: 'merge', message: 'merging' },
    });
    fake.state.next(jobResult('job-1', 'success'));

    expect(progress).toHaveBeenCalledTimes(1);
    expect(progress.calls.mostRecent().args[0].progress.stage).toBe('merge');
    expect(state).toHaveBeenCalledWith(jobResult('job-1', 'success'));
  });

  it('seeds the current snapshot with one status fetch', async () => {
    const fake = fakeWails();
    const seeded = jobResult('job-1', 'running');
    fake.getJobStatus.and.resolveTo(jobStatus(seeded));
    const state = jasmine.createSpy('onState');

    observeJob(fake.wails, 'job-1', { onState: state });
    await Promise.resolve();
    await Promise.resolve();

    expect(fake.getJobStatus).toHaveBeenCalledOnceWith('job-1');
    expect(state).toHaveBeenCalledOnceWith(seeded);
  });

  it('reports a missing job through onMissing', async () => {
    const fake = fakeWails();
    const response: JobStatusResponse = {
      success: false,
      found: false,
      message: 'job not found',
    };
    fake.getJobStatus.and.resolveTo(response);
    const missing = jasmine.createSpy('onMissing');

    observeJob(fake.wails, 'job-missing', { onMissing: missing });
    await Promise.resolve();
    await Promise.resolve();

    expect(missing).toHaveBeenCalledOnceWith(response);
  });

  it('keeps a newer state event when the seed resolves late', async () => {
    const fake = fakeWails();
    let resolveSeed: (value: JobStatusResponse) => void = () => undefined;
    fake.getJobStatus.and.returnValue(
      new Promise<JobStatusResponse>((resolve) => {
        resolveSeed = resolve;
      }),
    );
    const state = jasmine.createSpy('onState');

    observeJob(fake.wails, 'job-1', { onState: state });

    const terminal = jobResult('job-1', 'success');
    fake.state.next(terminal);
    expect(state).toHaveBeenCalledOnceWith(terminal);

    resolveSeed(jobStatus(jobResult('job-1', 'running')));
    await Promise.resolve();
    await Promise.resolve();

    expect(state).toHaveBeenCalledTimes(1);
  });

  it('still seeds when the replayed state event is not terminal', async () => {
    const fake = fakeWails();
    const seeded = jobResult('job-1', 'running');
    fake.getJobStatus.and.resolveTo(jobStatus(seeded));
    const state = jasmine.createSpy('onState');

    observeJob(fake.wails, 'job-1', { onState: state });
    fake.state.next(jobResult('job-1', 'queued'));
    await Promise.resolve();
    await Promise.resolve();

    expect(state).toHaveBeenCalledTimes(2);
    expect(state.calls.mostRecent().args[0]).toBe(seeded);
  });

  it('stops forwarding once the subscription is closed', async () => {
    const fake = fakeWails();
    fake.getJobStatus.and.resolveTo(jobStatus(jobResult('job-1', 'running')));
    const progress = jasmine.createSpy('onProgress');

    const subscription = observeJob(fake.wails, 'job-1', {
      onProgress: progress,
    });
    subscription.unsubscribe();

    fake.progress.next({
      jobId: 'job-1',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: { current: 1, total: 2, stage: 'merge', message: 'merging' },
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(progress).not.toHaveBeenCalled();
  });
});

describe('observeRecipe', () => {
  it('filters recipe events and seeds the run snapshot', async () => {
    const fake = fakeWails();
    const seeded = recipeResult('recipe-job-1', 'running');
    fake.getRecipeStatus.and.resolveTo(recipeStatus(seeded));
    const progress = jasmine.createSpy('onProgress');
    const state = jasmine.createSpy('onState');

    observeRecipe(fake.wails, 'recipe-job-1', {
      onProgress: progress,
      onState: state,
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(fake.getRecipeStatus).toHaveBeenCalledOnceWith('recipe-job-1');
    expect(state).toHaveBeenCalledOnceWith(seeded);

    fake.recipeProgress.next({
      jobId: 'other-job',
      recipeId: 'recipe.email_pdf',
      status: 'running',
      currentStep: 1,
      totalSteps: 2,
      stepName: 'other',
      stage: 'running',
      message: 'other',
      percent: 10,
    });
    expect(progress).not.toHaveBeenCalled();

    const event: RecipeProgressEvent = {
      jobId: 'recipe-job-1',
      recipeId: 'recipe.email_pdf',
      status: 'running',
      currentStep: 1,
      totalSteps: 2,
      stepName: 'Optimize',
      stage: 'running',
      message: 'Optimizing',
      percent: 50,
    };
    fake.recipeProgress.next(event);
    expect(progress).toHaveBeenCalledOnceWith(event);

    const terminal = recipeResult('recipe-job-1', 'success');
    fake.recipeState.next(terminal);
    expect(state).toHaveBeenCalledWith(terminal);
  });

  it('reports a missing run through onMissing', async () => {
    const fake = fakeWails();
    const response: RecipeStatusResponse = {
      success: false,
      found: false,
      message: 'recipe run not found',
    };
    fake.getRecipeStatus.and.resolveTo(response);
    const missing = jasmine.createSpy('onMissing');

    observeRecipe(fake.wails, 'recipe-job-1', { onMissing: missing });
    await Promise.resolve();
    await Promise.resolve();

    expect(missing).toHaveBeenCalledOnceWith(response);
  });
});
