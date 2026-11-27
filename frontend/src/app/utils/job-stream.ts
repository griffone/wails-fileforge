import { Observable, Subscription } from 'rxjs';
import { filter } from 'rxjs/operators';

import {
  JobProgressEvent,
  JobResult,
  JobStatusResponse,
  RecipeProgressEvent,
  RecipeResult,
  RecipeStatusResponse,
  Wails,
} from '../services/wails';
import { isTerminalJobStatus } from './job-status';

export interface JobStreamHandlers {
  onProgress?: (event: JobProgressEvent) => void;
  onState?: (job: JobResult) => void;
  /** The backend no longer tracks the job; the page surfaces the error. */
  onMissing?: (response: JobStatusResponse) => void;
}

export interface RecipeStreamHandlers {
  onProgress?: (event: RecipeProgressEvent) => void;
  onState?: (result: RecipeResult) => void;
  /** The backend no longer tracks the run; the page surfaces the error. */
  onMissing?: (response: RecipeStatusResponse) => void;
}

/**
 * Follows one tool job through the shared Wails event streams.
 *
 * `jobProgress$` and `jobState$` replay the latest event to late subscribers,
 * so every emission is filtered by job id. The replay may also belong to another
 * job: the one status fetch below seeds the real snapshot before any event is
 * trusted. It is the only status call the observer makes; progress is event
 * driven from then on.
 */
export function observeJob(
  wails: Wails,
  jobId: string,
  handlers: JobStreamHandlers,
): Subscription {
  const subscription = new Subscription();
  const trimmed = typeof jobId === 'string' ? jobId.trim() : '';
  if (!wails || !trimmed) {
    return subscription;
  }

  let terminalSeen = false;

  const progress$ = wails.jobProgress$;
  if (isStream(progress$)) {
    subscription.add(
      progress$.pipe(filter((event) => event?.jobId === trimmed)).subscribe({
        next: (event) => handlers.onProgress?.(event),
        error: () => undefined,
      }),
    );
  }

  const state$ = wails.jobState$;
  if (isStream(state$)) {
    subscription.add(
      state$.pipe(filter((job) => job?.jobId === trimmed)).subscribe({
        next: (job) => {
          terminalSeen = terminalSeen || isTerminalJobStatus(job.status);
          handlers.onState?.(job);
        },
        error: () => undefined,
      }),
    );
  }

  seedJobStatus(wails, trimmed, subscription, () => terminalSeen, handlers);

  return subscription;
}

/**
 * Follows one recipe run through `recipes/progress` and `recipes/state` with
 * the same filtering and seed contract as `observeJob`.
 */
export function observeRecipe(
  wails: Wails,
  runJobId: string,
  handlers: RecipeStreamHandlers,
): Subscription {
  const subscription = new Subscription();
  const trimmed = typeof runJobId === 'string' ? runJobId.trim() : '';
  if (!wails || !trimmed) {
    return subscription;
  }

  let terminalSeen = false;

  const progress$ = wails.recipeProgress$;
  if (isStream(progress$)) {
    subscription.add(
      progress$.pipe(filter((event) => event?.jobId === trimmed)).subscribe({
        next: (event) => handlers.onProgress?.(event),
        error: () => undefined,
      }),
    );
  }

  const state$ = wails.recipeState$;
  if (isStream(state$)) {
    subscription.add(
      state$.pipe(filter((result) => result?.jobId === trimmed)).subscribe({
        next: (result) => {
          terminalSeen = terminalSeen || isTerminalRecipeStatus(result.status);
          handlers.onState?.(result);
        },
        error: () => undefined,
      }),
    );
  }

  seedRecipeStatus(wails, trimmed, subscription, () => terminalSeen, handlers);

  return subscription;
}

/**
 * A late subscriber can receive the terminal state before the seed request
 * resolves. Skipping the seed in that case keeps a stale snapshot from
 * overwriting the settled result. A non-terminal replay is not authoritative:
 * it can be the submit snapshot, so the fresh status still wins.
 */
function seedJobStatus(
  wails: Wails,
  jobId: string,
  subscription: Subscription,
  terminalSeen: () => boolean,
  handlers: JobStreamHandlers,
): void {
  Promise.resolve()
    .then(() => wails.getJobStatus(jobId))
    .then((response) => {
      if (subscription.closed || terminalSeen()) {
        return;
      }
      if (response?.found && response.result) {
        handlers.onState?.(response.result);
        return;
      }
      handlers.onMissing?.(response);
    })
    .catch(() => undefined);
}

function seedRecipeStatus(
  wails: Wails,
  jobId: string,
  subscription: Subscription,
  terminalSeen: () => boolean,
  handlers: RecipeStreamHandlers,
): void {
  Promise.resolve()
    .then(() => wails.getRecipeStatus(jobId))
    .then((response) => {
      if (subscription.closed || terminalSeen()) {
        return;
      }
      if (response?.found && response.result) {
        handlers.onState?.(response.result);
        return;
      }
      handlers.onMissing?.(response);
    })
    .catch(() => undefined);
}

function isStream<T>(value: Observable<T> | undefined): value is Observable<T> {
  return !!value && typeof value.subscribe === 'function';
}

const TERMINAL_RECIPE_STATUSES: ReadonlySet<string> = new Set([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

function isTerminalRecipeStatus(status: string): boolean {
  return TERMINAL_RECIPE_STATUSES.has(status);
}
