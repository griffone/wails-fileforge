import { WritableSignal } from '@angular/core';

import { ActivityService } from '../services/activity.service';
import {
  JobError,
  JobExecutionStatus,
  JobResult,
} from '../services/wails';

/**
 * Reattaches a tool page to a job that is still running in the background.
 *
 * Tool pages own their result signals, so navigation destroys all local state
 * while the backend job keeps running. When the user comes back, the page asks
 * the global ActivityService for the newest active job of its tool, seeds a
 * minimal snapshot and reattaches the page's event observer. Returns true when
 * a job was adopted.
 */
export interface ResumeToolJobOptions {
  activity: ActivityService;
  toolId: string;
  activeJobId: WritableSignal<string>;
  jobResult: WritableSignal<JobResult | null>;
  jobError: WritableSignal<JobError | null>;
  startObserving: () => void;
}

export function resumeToolJob(options: ResumeToolJobOptions): boolean {
  if (options.activeJobId()) {
    return false;
  }

  const task = options.activity.activeJobForTool(options.toolId);
  if (!task) {
    return false;
  }

  options.activeJobId.set(task.id);
  options.jobError.set(null);
  options.jobResult.set({
    jobId: task.id,
    success: false,
    message: task.message,
    toolId: options.toolId,
    status: task.status as JobExecutionStatus,
    progress: {
      current: task.current,
      total: task.total,
      stage: task.stage,
      message: task.message,
      etaSeconds: task.etaSeconds ?? undefined,
    },
    items: [],
    startedAt: task.startedAt,
  });
  options.startObserving();

  return true;
}

/**
 * Reattaches the recipe runner to a run that is still in progress. The
 * observer seeds the full status immediately after adoption, so no partial
 * snapshot is seeded here.
 */
export interface ResumeRecipeRunOptions {
  activity: ActivityService;
  recipeId: string;
  runJobId: WritableSignal<string>;
  startObserving: () => void;
}

export function resumeRecipeRun(options: ResumeRecipeRunOptions): boolean {
  if (options.runJobId()) {
    return false;
  }

  const task = options.activity.activeRecipeFor(options.recipeId);
  if (!task) {
    return false;
  }

  options.runJobId.set(task.id);
  options.startObserving();

  return true;
}
