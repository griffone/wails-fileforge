import { signal } from '@angular/core';

import { ActivityService, ActivityTask } from '../services/activity.service';
import { JobError, JobResult } from '../services/wails';
import { resumeRecipeRun, resumeToolJob } from './resume-task';

function task(overrides: Partial<ActivityTask> = {}): ActivityTask {
  return {
    id: 'job-1',
    kind: 'tool',
    title: 'Image Compress',
    route: '/image-compress',
    toolId: 'tool.image.compress',
    status: 'running',
    stage: 'processing',
    message: 'Compressing 1 of 2',
    current: 1,
    total: 2,
    percent: 50,
    startedAt: 1000,
    endedAt: null,
    etaSeconds: 12,
    inputCount: 2,
    outputCount: 0,
    errorMessage: '',
    ...overrides,
  };
}

function activityStub(job: ActivityTask | null, recipe: ActivityTask | null) {
  return {
    activeJobForTool: jasmine
      .createSpy('activeJobForTool')
      .and.callFake(() => job),
    activeRecipeFor: jasmine
      .createSpy('activeRecipeFor')
      .and.callFake(() => recipe),
  } as unknown as ActivityService;
}

describe('resumeToolJob', () => {
  it('seeds the page snapshot and starts observing for the active job', () => {
    const activeJobId = signal('');
    const jobResult = signal<JobResult | null>(null);
    const jobError = signal<JobError | null>({
      code: 'EXEC_IO_TRANSIENT',
      message: 'stale',
    });
    const startObserving = jasmine.createSpy('startObserving');

    const resumed = resumeToolJob({
      activity: activityStub(task(), null),
      toolId: 'tool.image.compress',
      activeJobId,
      jobResult,
      jobError,
      startObserving,
    });

    expect(resumed).toBeTrue();
    expect(activeJobId()).toBe('job-1');
    expect(jobError()).toBeNull();
    expect(jobResult()?.status).toBe('running');
    expect(jobResult()?.progress.current).toBe(1);
    expect(jobResult()?.progress.total).toBe(2);
    expect(jobResult()?.startedAt).toBe(1000);
    expect(startObserving).toHaveBeenCalled();
  });

  it('does nothing when the page already owns a job', () => {
    const startObserving = jasmine.createSpy('startObserving');
    const resumed = resumeToolJob({
      activity: activityStub(task(), null),
      toolId: 'tool.image.compress',
      activeJobId: signal('job-existing'),
      jobResult: signal<JobResult | null>(null),
      jobError: signal<JobError | null>(null),
      startObserving,
    });

    expect(resumed).toBeFalse();
    expect(startObserving).not.toHaveBeenCalled();
  });

  it('does nothing when no job is active for the tool', () => {
    const startObserving = jasmine.createSpy('startObserving');
    const resumed = resumeToolJob({
      activity: activityStub(null, null),
      toolId: 'tool.image.compress',
      activeJobId: signal(''),
      jobResult: signal<JobResult | null>(null),
      jobError: signal<JobError | null>(null),
      startObserving,
    });

    expect(resumed).toBeFalse();
    expect(startObserving).not.toHaveBeenCalled();
  });
});

describe('resumeRecipeRun', () => {
  it('adopts the active recipe run and starts polling', () => {
    const runJobId = signal('');
    const startPolling = jasmine.createSpy('startPolling');

    const resumed = resumeRecipeRun({
      activity: activityStub(null, task({ id: 'recipe-job-1', kind: 'recipe' })),
      recipeId: 'recipe.email_pdf',
      runJobId,
      startPolling,
    });

    expect(resumed).toBeTrue();
    expect(runJobId()).toBe('recipe-job-1');
    expect(startPolling).toHaveBeenCalled();
  });

  it('does nothing when the page already owns a run', () => {
    const startPolling = jasmine.createSpy('startPolling');
    const resumed = resumeRecipeRun({
      activity: activityStub(null, task({ id: 'recipe-job-1', kind: 'recipe' })),
      recipeId: 'recipe.email_pdf',
      runJobId: signal('recipe-existing'),
      startPolling,
    });

    expect(resumed).toBeFalse();
    expect(startPolling).not.toHaveBeenCalled();
  });
});
