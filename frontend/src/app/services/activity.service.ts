import {
  Injectable,
  NgZone,
  OnDestroy,
  Signal,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Observable, Subscription } from 'rxjs';

import { RECIPES } from '../config/recipes';
import { ToastService } from './toast.service';
import { ToolRegistryService } from './tool-registry.service';
import {
  JobListResponseV1,
  JobProgressEventV1,
  JobResultItemV1,
  JobResultV1,
  RecipeProgressEventV1,
  RecipeResultV1,
  Wails,
} from './wails';

export type ActivityTaskKind = 'tool' | 'recipe';
export type ActivityTaskTone = 'success' | 'warning' | 'danger' | 'info';

export interface ActivityTask {
  /** Backend job id shared by tool jobs and recipe runs. */
  id: string;
  kind: ActivityTaskKind;
  /** Tool nav name or recipe name; falls back to the raw id. */
  title: string;
  /** Route that opens the tool or recipe that owns the task. */
  route: string;
  toolId?: string;
  recipeId?: string;
  /** Raw backend status; running jobs may report a stage name here. */
  status: string;
  /** Progress stage when available, otherwise the status. */
  stage: string;
  message: string;
  /** Items (tool) or steps (recipe) completed so far. */
  current: number;
  /** Items or steps expected; 0 when unknown. */
  total: number;
  /** 0..100, or null when the job is not measurable. */
  percent: number | null;
  startedAt: number;
  endedAt: number | null;
  etaSeconds: number | null;
  inputCount: number;
  outputCount: number;
  errorMessage: string;
}

const TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

const REFRESH_LIMIT = 50;
const RECENT_LIMIT = 25;
const REFRESH_DEBOUNCE_MS = 150;
const NOTIFICATION_BODY_LIMIT = 180;

/**
 * Background task activity for the whole app.
 *
 * The service merges full snapshots (`jobs/state`, `recipes/state`) with
 * live progress events, keeps the active/recent ordering, and turns the first
 * non-terminal -> terminal transition of each job into exactly one completion
 * toast. It never throws when a stream or IPC method is missing so preview mode
 * and partial test doubles keep working.
 */
@Injectable({
  providedIn: 'root',
})
export class ActivityService implements OnDestroy {
  private readonly wails = inject(Wails);
  private readonly registry = inject(ToolRegistryService);
  private readonly toast = inject(ToastService);
  private readonly zone = inject(NgZone);

  private readonly taskState = signal<readonly ActivityTask[]>([]);
  private readonly panelOpenState = signal(false);
  private readonly dismissedState = signal<ReadonlySet<string>>(new Set());
  private readonly recipePercents = new Map<string, number>();
  private readonly notified = new Set<string>();
  private readonly subscriptions: Subscription[] = [];

  private refreshInFlight: Promise<void> | null = null;
  private refreshPending = false;
  private refreshTimer: ReturnType<typeof setTimeout> | null = null;

  /** Active tasks first by start time, then terminal tasks newest first. */
  readonly tasks: Signal<readonly ActivityTask[]> = computed(() =>
    [...this.taskState()].sort(compareTasks),
  );

  readonly activeTasks: Signal<readonly ActivityTask[]> = computed(() =>
    this.tasks().filter((task) => !isTerminalStatus(task.status)),
  );

  readonly recentTasks: Signal<readonly ActivityTask[]> = computed(() => {
    const dismissed = this.dismissedState();
    return this.tasks()
      .filter(
        (task) => isTerminalStatus(task.status) && !dismissed.has(task.id),
      )
      .slice(0, RECENT_LIMIT);
  });

  readonly runningCount: Signal<number> = computed(
    () => this.activeTasks().length,
  );

  readonly panelOpen: Signal<boolean> = this.panelOpenState.asReadonly();

  constructor() {
    this.subscribeStream<JobResultV1>('jobState$', (snapshot) =>
      this.applyJobSnapshot(snapshot),
    );
    this.subscribeStream<RecipeResultV1>('recipeState$', (snapshot) =>
      this.applyRecipeSnapshot(snapshot),
    );
    this.subscribeStream<JobProgressEventV1>('jobProgress$', (event) =>
      this.applyJobProgress(event),
    );
    this.subscribeStream<RecipeProgressEventV1>('recipeProgress$', (event) =>
      this.applyRecipeProgress(event),
    );
  }

  ngOnDestroy(): void {
    if (this.refreshTimer !== null) {
      clearTimeout(this.refreshTimer);
      this.refreshTimer = null;
    }
    for (const subscription of this.subscriptions) {
      try {
        subscription.unsubscribe();
      } catch {
        // ignore
      }
    }
    this.subscriptions.length = 0;
  }

  openPanel(): void {
    this.panelOpenState.set(true);
  }

  closePanel(): void {
    this.panelOpenState.set(false);
  }

  togglePanel(): void {
    this.panelOpenState.update((open) => !open);
  }

  /** Newest non-terminal tool job for one tool, or null. */
  activeJobForTool(toolId: string): ActivityTask | null {
    return this.newestActive(
      (task) => task.kind === 'tool' && task.toolId === toolId,
    );
  }

  /** Newest non-terminal recipe run for one recipe, or null. */
  activeRecipeFor(recipeId: string): ActivityTask | null {
    return this.newestActive(
      (task) => task.kind === 'recipe' && task.recipeId === recipeId,
    );
  }

  /**
   * Rebuilds the list from the backend snapshot. Terminal tasks found here are
   * treated as already notified: they finished before the UI could subscribe,
   * so surfacing a toast for them would be noise.
   */
  refresh(): Promise<void> {
    if (this.refreshInFlight) {
      return this.refreshInFlight;
    }

    this.refreshInFlight = this.performRefresh().finally(() => {
      this.refreshInFlight = null;
    });
    return this.refreshInFlight;
  }

  /** Requests cancellation; returns '' on success or the failure message. */
  async cancelTask(task: ActivityTask): Promise<string> {
    const outcome = await this.requestCancel(task);
    if (outcome.success) {
      return '';
    }

    const message = outcome.message.trim() || 'Could not cancel the task.';
    this.toast.push({
      tone: 'danger',
      title: 'Cancel failed',
      body: message,
    });
    return message;
  }

  /** Hides a finished task from the recent list; the backend is untouched. */
  dismissTask(id: string): void {
    const task = this.taskState().find((item) => item.id === id);
    if (!task || !isTerminalStatus(task.status)) {
      return;
    }

    this.dismissedState.update((dismissed) => {
      const next = new Set(dismissed);
      next.add(id);
      return next;
    });
  }

  /** Dismisses every terminal task currently listed. */
  clearFinished(): void {
    const ids = this.tasks()
      .filter((task) => isTerminalStatus(task.status))
      .map((task) => task.id);
    if (ids.length === 0) {
      return;
    }

    this.dismissedState.update((dismissed) => {
      const next = new Set(dismissed);
      for (const id of ids) {
        next.add(id);
      }
      return next;
    });
  }

  private async performRefresh(): Promise<void> {
    const [jobsOutcome, recipesOutcome] = await Promise.allSettled([
      this.fetchJobs(REFRESH_LIMIT),
      this.fetchRecipeRuns(REFRESH_LIMIT),
    ]);

    if (jobsOutcome.status === 'fulfilled') {
      for (const job of jobsOutcome.value) {
        if (job && isTerminalStatus(job.status)) {
          this.notified.add(notificationKey('tool', job.jobId));
        }
      }
      for (const job of jobsOutcome.value) {
        this.applyJobSnapshot(job);
      }
    }

    if (recipesOutcome.status === 'fulfilled') {
      for (const recipe of recipesOutcome.value) {
        if (recipe && isTerminalStatus(recipe.status)) {
          this.notified.add(notificationKey('recipe', recipe.jobId));
        }
      }
      for (const recipe of recipesOutcome.value) {
        this.applyRecipeSnapshot(recipe);
      }
    }
  }

  private async fetchJobs(limit: number): Promise<readonly JobResultV1[]> {
    const list = (this.wails as unknown as Record<string, unknown>)[
      'listJobsV1'
    ];
    if (typeof list !== 'function') {
      return [];
    }

    const response = await (
      list as (count: number) => Promise<JobListResponseV1>
    ).call(this.wails, limit);
    return Array.isArray(response?.jobs) ? response.jobs : [];
  }

  private async fetchRecipeRuns(
    limit: number,
  ): Promise<readonly RecipeResultV1[]> {
    const list = (this.wails as unknown as Record<string, unknown>)[
      'listRecipeRunsV1'
    ];
    if (typeof list !== 'function') {
      return [];
    }

    const response = await (
      list as (count: number) => Promise<RecipeResultV1[]>
    ).call(this.wails, limit);
    return Array.isArray(response) ? response : [];
  }

  private async requestCancel(
    task: ActivityTask,
  ): Promise<{ success: boolean; message: string }> {
    const methodName = task.kind === 'recipe' ? 'cancelRecipeV1' : 'cancelJobV1';
    const cancel = (this.wails as unknown as Record<string, unknown>)[methodName];
    if (typeof cancel !== 'function') {
      return { success: false, message: 'Cancelling is not available right now.' };
    }

    try {
      const response = await (
        cancel as (
          id: string,
        ) => Promise<{ success: boolean; message: string }>
      ).call(this.wails, task.id);
      if (!response || response.success !== true) {
        return {
          success: false,
          message:
            response && typeof response.message === 'string'
              ? response.message
              : '',
        };
      }
      return { success: true, message: '' };
    } catch (error) {
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Cancel failed',
      };
    }
  }

  private applyJobSnapshot(result: JobResultV1): void {
    if (
      !result ||
      typeof result.jobId !== 'string' ||
      result.jobId.trim().length === 0
    ) {
      return;
    }

    const previous = this.findTask('tool', result.jobId);
    const task = this.buildJobTask(result, previous);
    // A refresh snapshot can be older than a terminal event that already
    // landed; never downgrade a finished task back to running.
    if (previous && isTerminalStatus(previous.status) && !isTerminalStatus(task.status)) {
      return;
    }
    this.upsertTask(task);
    this.maybeNotify(previous, task);
  }

  private applyRecipeSnapshot(result: RecipeResultV1): void {
    if (
      !result ||
      typeof result.jobId !== 'string' ||
      result.jobId.trim().length === 0
    ) {
      return;
    }

    const previous = this.findTask('recipe', result.jobId);
    const task = this.buildRecipeTask(result, previous);
    if (previous && isTerminalStatus(previous.status) && !isTerminalStatus(task.status)) {
      return;
    }
    this.upsertTask(task);
    this.maybeNotify(previous, task);
  }

  private applyJobProgress(event: JobProgressEventV1): void {
    if (
      !event ||
      typeof event.jobId !== 'string' ||
      event.jobId.trim().length === 0
    ) {
      return;
    }

    const existing = this.findTask('tool', event.jobId);
    if (!existing) {
      this.scheduleRefresh();
      return;
    }

    const progress = event.progress;
    if (!progress) {
      return;
    }

    const current = toCount(progress.current);
    const total = toCount(progress.total);
    const status = textOf(event.status) || existing.status;
    if (isTerminalStatus(existing.status) && !isTerminalStatus(status)) {
      return;
    }
    const updated: ActivityTask = {
      ...existing,
      status,
      stage: textOf(progress.stage) || existing.stage,
      message: textOf(progress.message) || existing.message,
      current,
      total,
      percent: percentFromCounts(current, total) ?? existing.percent,
      etaSeconds: optionalSeconds(progress.etaSeconds) ?? existing.etaSeconds,
      endedAt:
        existing.endedAt ??
        (isTerminalStatus(status) ? Date.now() : existing.endedAt),
    };

    this.upsertTask(updated);
    this.maybeNotify(existing, updated);
  }

  private applyRecipeProgress(event: RecipeProgressEventV1): void {
    if (
      !event ||
      typeof event.jobId !== 'string' ||
      event.jobId.trim().length === 0
    ) {
      return;
    }

    const percent = clampPercent(event.percent);
    if (percent !== null) {
      this.recipePercents.set(event.jobId, percent);
    }

    const existing = this.findTask('recipe', event.jobId);
    if (!existing) {
      this.scheduleRefresh();
      return;
    }

    const current = toCount(event.currentStep);
    const total = toCount(event.totalSteps);
    const status = textOf(event.status) || existing.status;
    if (isTerminalStatus(existing.status) && !isTerminalStatus(status)) {
      return;
    }
    const updated: ActivityTask = {
      ...existing,
      status,
      stage: textOf(event.stage) || existing.stage,
      message:
        textOf(event.message) || textOf(event.stepName) || existing.message,
      current,
      total,
      percent:
        percent ?? percentFromCounts(current, total) ?? existing.percent,
      endedAt:
        existing.endedAt ??
        (isTerminalStatus(status) ? Date.now() : existing.endedAt),
    };

    this.upsertTask(updated);
    this.maybeNotify(existing, updated);
  }

  private buildJobTask(
    result: JobResultV1,
    previous: ActivityTask | null,
  ): ActivityTask {
    const toolId = textOf(result.toolId);
    const definition = toolId ? this.registry.findById(toolId) : null;
    const title = definition?.navName || definition?.name || toolId || result.jobId;
    const route = definition?.route || '/';
    const status =
      textOf(result.status) || previous?.status || 'queued';
    const progress = result.progress ?? {
      current: 0,
      total: 0,
      stage: '',
      message: '',
    };
    const current = toCount(progress.current);
    const total = toCount(progress.total);
    const items = Array.isArray(result.items) ? result.items : [];
    const stage = textOf(progress.stage) || status;

    return {
      id: result.jobId,
      kind: 'tool',
      title,
      route,
      toolId,
      status,
      stage,
      message: textOf(progress.message) || textOf(result.message),
      current,
      total,
      percent: percentFromCounts(current, total) ?? previous?.percent ?? null,
      startedAt: timestampOf(result.startedAt, previous?.startedAt),
      endedAt:
        optionalTimestamp(result.endedAt) ??
        (isTerminalStatus(status) ? Date.now() : null),
      etaSeconds: optionalSeconds(progress.etaSeconds),
      inputCount: items.length > 0 ? items.length : total,
      outputCount: this.jobOutputCount(items),
      errorMessage: textOf(result.error?.message),
    };
  }

  private buildRecipeTask(
    result: RecipeResultV1,
    previous: ActivityTask | null,
  ): ActivityTask {
    const recipeId = textOf(result.recipeId);
    const recipe = RECIPES.find((candidate) => candidate.id === recipeId) ?? null;
    const title = recipe?.fallback.name?.trim() || recipeId || result.jobId;
    const status = textOf(result.status) || previous?.status || 'queued';
    const current = toCount(result.currentStep);
    const total = toCount(result.totalSteps);
    const eventPercent = this.recipePercents.get(result.jobId);
    const percent =
      eventPercent ??
      percentFromCounts(current, total) ??
      previous?.percent ??
      null;

    return {
      id: result.jobId,
      kind: 'recipe',
      title,
      route: recipe?.route || '/',
      recipeId,
      status,
      stage: status,
      message: textOf(result.message),
      current,
      total,
      percent,
      startedAt: timestampOf(result.startedAt, previous?.startedAt),
      endedAt:
        optionalTimestamp(result.endedAt) ??
        (isTerminalStatus(status) ? Date.now() : null),
      etaSeconds: previous?.etaSeconds ?? null,
      inputCount: 0,
      outputCount: Array.isArray(result.outputs) ? result.outputs.length : 0,
      errorMessage: textOf(result.error?.message),
    };
  }

  private jobOutputCount(items: readonly JobResultItemV1[]): number {
    let count = 0;
    for (const item of items) {
      if (!item || item.success !== true) {
        continue;
      }
      if (Array.isArray(item.outputs) && item.outputs.length > 0) {
        count += item.outputs.length;
      } else if (textOf(item.outputPath)) {
        count += 1;
      }
    }
    return count;
  }

  private maybeNotify(
    previous: ActivityTask | null,
    task: ActivityTask,
  ): void {
    if (!isTerminalStatus(task.status)) {
      return;
    }
    if (previous && isTerminalStatus(previous.status)) {
      return;
    }

    const key = notificationKey(task.kind, task.id);
    if (this.notified.has(key)) {
      return;
    }

    this.notified.add(key);
    this.notifyCompletion(task);
  }

  private notifyCompletion(task: ActivityTask): void {
    const succeeded =
      task.status === 'success' || task.status === 'partial_success';
    const cancelled = task.status === 'cancelled';
    const title = succeeded
      ? `${task.title} finished`
      : cancelled
        ? `${task.title} cancelled`
        : `${task.title} failed`;
    const body = this.notificationBody(task, succeeded);
    const tone: ActivityTaskTone = succeeded
      ? 'success'
      : cancelled
        ? 'warning'
        : 'danger';

    this.toast.push({
      tone,
      title,
      body,
      actionLabel: 'View activity',
      action: () => this.openPanel(),
    });

    if (typeof document !== 'undefined' && !document.hasFocus()) {
      this.safeNotify(title, body);
    }
  }

  private notificationBody(task: ActivityTask, succeeded: boolean): string {
    const message = textOf(task.message);
    let body: string;
    if (message) {
      body = message;
    } else if (succeeded) {
      body = `${task.outputCount} files ready`;
    } else if (textOf(task.errorMessage)) {
      body = textOf(task.errorMessage);
    } else {
      body = statusLabel(task.status);
    }

    return body.length > NOTIFICATION_BODY_LIMIT
      ? body.slice(0, NOTIFICATION_BODY_LIMIT).trimEnd()
      : body;
  }

  private safeNotify(title: string, body: string): void {
    const notify = (this.wails as unknown as Record<string, unknown>)[
      'notifyV1'
    ];
    if (typeof notify !== 'function') {
      return;
    }

    try {
      const result = (
        notify as (notificationTitle: string, notificationBody: string) => unknown
      ).call(this.wails, title, body);
      if (result && typeof (result as PromiseLike<unknown>).then === 'function') {
        void Promise.resolve(result).catch(() => undefined);
      }
    } catch {
      // Native notifications are best-effort; the toast already covered it.
    }
  }

  private newestActive(
    predicate: (task: ActivityTask) => boolean,
  ): ActivityTask | null {
    let newest: ActivityTask | null = null;
    for (const task of this.taskState()) {
      if (isTerminalStatus(task.status) || !predicate(task)) {
        continue;
      }
      if (!newest || task.startedAt > newest.startedAt) {
        newest = task;
      }
    }
    return newest;
  }

  private findTask(kind: ActivityTaskKind, id: string): ActivityTask | null {
    return (
      this.taskState().find((task) => task.kind === kind && task.id === id) ??
      null
    );
  }

  private upsertTask(task: ActivityTask): void {
    this.taskState.update((tasks) => {
      const index = tasks.findIndex(
        (candidate) => candidate.kind === task.kind && candidate.id === task.id,
      );
      if (index < 0) {
        return [...tasks, task];
      }

      const next = tasks.slice();
      next[index] = task;
      return next;
    });
  }

  private scheduleRefresh(): void {
    if (this.refreshPending) {
      return;
    }

    this.refreshPending = true;
    this.refreshTimer = setTimeout(() => {
      this.refreshPending = false;
      this.refreshTimer = null;
      void this.refresh().catch(() => undefined);
    }, REFRESH_DEBOUNCE_MS);
  }

  private subscribeStream<T>(
    name: string,
    next: (value: T) => void,
  ): void {
    const source = this.resolveStream<T>(name);
    if (!source) {
      return;
    }

    try {
      this.subscriptions.push(
        source.subscribe({
          next: (value) => {
            try {
              this.zone.run(() => next(value));
            } catch {
              // A malformed payload must never break the activity stream.
            }
          },
          error: () => undefined,
        }),
      );
    } catch {
      // Partial test doubles may expose a non-subscribable placeholder.
    }
  }

  private resolveStream<T>(name: string): Observable<T> | null {
    const source = (this.wails as unknown as Record<string, unknown>)[name];
    if (
      !source ||
      typeof (source as { subscribe?: unknown }).subscribe !== 'function'
    ) {
      return null;
    }
    return source as Observable<T>;
  }
}

function isTerminalStatus(status: string | null | undefined): boolean {
  return typeof status === 'string' && TERMINAL_STATUSES.has(status);
}

function compareTasks(a: ActivityTask, b: ActivityTask): number {
  const aTerminal = isTerminalStatus(a.status);
  const bTerminal = isTerminalStatus(b.status);
  if (aTerminal !== bTerminal) {
    return aTerminal ? 1 : -1;
  }

  if (!aTerminal) {
    return a.startedAt - b.startedAt || a.id.localeCompare(b.id);
  }

  const aEnd = a.endedAt ?? a.startedAt;
  const bEnd = b.endedAt ?? b.startedAt;
  return bEnd - aEnd || b.startedAt - a.startedAt || a.id.localeCompare(b.id);
}

function textOf(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function toCount(value: unknown): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return 0;
  }
  return Math.trunc(value);
}

function percentFromCounts(current: number, total: number): number | null {
  if (total <= 0) {
    return null;
  }
  return clampPercent((current / total) * 100);
}

function clampPercent(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null;
  }
  return Math.min(100, Math.max(0, Math.round(value)));
}

function optionalSeconds(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    return null;
  }
  return value;
}

function optionalTimestamp(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return null;
  }
  return value;
}

function timestampOf(value: unknown, fallback: number | undefined): number {
  return optionalTimestamp(value) ?? fallback ?? Date.now();
}

function notificationKey(kind: ActivityTaskKind, id: string): string {
  return `${kind}:${id}`;
}

function statusLabel(status: string): string {
  switch (status) {
    case 'success':
      return 'Completed successfully';
    case 'partial_success':
      return 'Completed with warnings';
    case 'cancelled':
      return 'Cancelled';
    case 'failed':
      return 'Failed';
    case 'interrupted':
      return 'Interrupted';
    default:
      return status || 'Finished';
  }
}
