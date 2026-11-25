import { Signal, WritableSignal, computed, signal } from '@angular/core';
import { Subscription } from 'rxjs';

import { ActivityService } from '../services/activity.service';
import { DropIntakeService } from '../services/drop-intake.service';
import { I18nService } from '../services/i18n.service';
import { LastRequestService } from '../services/last-request.service';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../services/tool-error';
import {
  FileInfo,
  InspectFilesResponse,
  JobError,
  JobExecutionStatus,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  RunJobResponse,
  UndoResult,
  UndoSkipped,
  VerificationReport,
  Wails,
} from '../services/wails';
import type { TranslationKey } from '../i18n';
import type { RunAgainStartedEvent } from '../components/tool-flow/result-card';
import { FileQueueItem } from '../components/tool-flow/tool-flow.types';
import { fileName } from './format';
import { isTerminalJobStatus } from './job-status';
import { observeJob } from './job-stream';
import { resumeToolJob } from './resume-task';

/**
 * Shared lifecycle for a tool page.
 *
 * A page keeps its feature model (options, templates, previews), while this
 * controller owns the run pipeline: plan, run, observe, reconcile, verify,
 * undo, resume and the queue helpers the 17 tool pages used to copy.
 *
 * It is a plain class created per page instead of a root service: the state
 * belongs to one page instance, navigation must destroy it, and a service
 * would either leak state across tools or need a provider per component. A
 * plain class is also testable without TestBed.
 */
export interface ToolRunNoticeKeys {
  rejectedOne: TranslationKey;
  rejectedMany: TranslationKey;
  duplicateOne: TranslationKey;
  duplicateMany: TranslationKey;
  /** Whether the many-rejected copy takes `names` or a `count`. */
  rejectedManyFormat: 'names' | 'count';
}

export interface ToolRunMessageKeys {
  /** Fallback for a failed run and for the job error copy. */
  start: TranslationKey;
  planFailed: TranslationKey;
  waiting: TranslationKey;
  inspectFailed: TranslationKey;
  statusMissing: TranslationKey;
  cancelFailed: TranslationKey;
  openFileFailed: TranslationKey;
  revealFailed: TranslationKey;
}

export interface ToolRunControllerConfig {
  toolId: string;
  wails: Wails;
  i18n: I18nService;
  activity: ActivityService;
  dropIntake: DropIntakeService;
  /** Builds the current request from the page model. */
  buildRequest: () => JobRequest;
  /**
   * Stores the accepted request, so the result card can offer "run again".
   * Every page that runs through this controller gets the behaviour for free.
   */
  lastRequests: LastRequestService;
  /** Whether an intake path belongs to this tool. */
  acceptsPath: (path: string) => boolean;
  /** False clears the plan instead of calling `planJob`. */
  canPlan: () => boolean;
  /** Page-owned gating, evaluated again when a run starts. */
  canRun: () => boolean;
  /** Extra page-owned blocking (for example an invalid custom name). */
  extraBlocked?: () => boolean;
  /** Page-specific metadata line for a queue item. */
  describeFileInfo: (info: FileInfo) => string;
  /**
   * Output paths to inspect for sizes after a terminal job. Defaults to every
   * successful output; a page may skip results it renders as a folder summary.
   */
  collectOutputPaths?: () => string[];
  initialOutputDir?: string;
  /** Defaults to true; transcription emits no thumbnails. */
  loadThumbnails?: boolean;
  /**
   * When true (default) every run re-plans first. `pdf-merge` keeps the plan
   * it already validated and only re-plans when it is missing or stale.
   */
  alwaysRefreshPlanBeforeRun?: boolean;
  messages: ToolRunMessageKeys;
  /**
   * Copy for the shared intake notice. A page whose intake never goes through
   * `onPathsSelected` (single-file tools) omits it.
   */
  notice?: ToolRunNoticeKeys;
  /**
   * Page-specific intake copy (for example "2 files skipped: .txt"), used
   * instead of the built-in rejected/duplicate sentences. It returns null when
   * the page wants no notice at all.
   */
  noticeText?: (
    rejected: readonly string[],
    duplicates: readonly string[],
  ) => { tone: 'warning' | 'danger'; text: string } | null;
  /**
   * Receives every successful inspection of the queue, for pages that derive
   * extra UI from it (input totals, preview page size).
   */
  onFilesInspected?: (infos: FileInfo[]) => void;
  /**
   * Runs after the queue changed. Defaults to an immediate plan refresh; the
   * page may debounce it or refresh a preview as well.
   */
  afterPathsChanged?: (
    previousPaths: readonly string[],
    paths: readonly string[],
  ) => void;
  /** Persists options and the last request once a job is accepted. */
  onRunAccepted?: (request: JobRequest, plan: PlanJobResponse | null) => void;
  /** Applies a request's options before a run-again job is adopted. */
  onRunAgainAdopted?: (request: JobRequest) => void;
}

const DEFAULT_PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 160;

export class ToolRunController {
  readonly outputDir: WritableSignal<string>;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planError = signal<JobError | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<{ tone: 'warning' | 'danger'; text: string } | null>(
    null,
  );
  readonly actionMessage = signal('');

  readonly verification = signal<VerificationReport | null>(null);
  readonly verificationLoading = signal(false);
  readonly verificationUnavailable = signal(false);

  readonly undoBusy = signal(false);
  readonly undoMode = signal<'undo' | 'restore'>('undo');
  readonly undoMessage = signal('');
  readonly undoTone = signal<'success' | 'warning'>('success');
  readonly undoSkipped = signal<UndoSkipped[]>([]);

  readonly terminalJob: Signal<JobResult | null> = computed(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress: Signal<boolean> = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
  });

  readonly jobErrorCopy: Signal<ToolErrorCopy | null> = computed(() => {
    const error = this.jobError();
    return error
      ? localizeToolError(
          (key) => this.config.i18n.t(key),
          describeToolError(error, this.config.i18n.t(this.config.messages.start)),
        )
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private planDirty = false;
  private fileItemsGeneration = 0;
  private readonly autoVerifiedJobIds = new Set<string>();

  constructor(private readonly config: ToolRunControllerConfig) {
    this.outputDir = signal(config.initialOutputDir ?? '');
  }

  /** Pulls paths queued by the global drop overlay for this page. */
  takePendingDrop(route: string): void {
    const pending = this.config.dropIntake.take(route);
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
  }

  /** Reattaches to a background job after navigation, if one is active. */
  resume(): boolean {
    return resumeToolJob({
      activity: this.config.activity,
      toolId: this.config.toolId,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startObserving: () => this.startObserving(),
    });
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const previous = [...this.selectedPaths()];
    const rejected: string[] = [];
    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(previous.map((path) => toolPathKey(path)));

    for (const path of incoming) {
      if (!this.config.acceptsPath(path)) {
        rejected.push(path);
        continue;
      }

      const key = toolPathKey(path);
      if (known.has(key)) {
        duplicates.push(path);
        continue;
      }

      known.add(key);
      added.push(path);
    }

    this.notice.set(this.intakeNotice(rejected, duplicates));

    if (added.length === 0) {
      return;
    }

    this.actionMessage.set('');
    this.selectedPaths.set([...previous, ...added]);
    this.syncFileItems();
    this.notifyPathsChanged(previous);
  }

  /** Clears the queue; returns false when it was already empty. */
  clear(): boolean {
    if (this.selectedPaths().length === 0) {
      return false;
    }

    const previous = [...this.selectedPaths()];
    this.selectedPaths.set([]);
    this.notice.set(null);
    this.actionMessage.set('');
    this.syncFileItems();
    this.notifyPathsChanged(previous);
    return true;
  }

  /** Removes one path; returns false when it was not queued. */
  removeFile(path: string): boolean {
    const previous = [...this.selectedPaths()];
    const next = previous.filter((candidate) => candidate !== path);
    if (next.length === previous.length) {
      return false;
    }

    this.selectedPaths.set(next);
    this.syncFileItems();
    this.notifyPathsChanged(previous);
    return true;
  }

  /** Replaces the queue in one step (retry-failed flows). */
  replacePaths(
    paths: readonly string[],
    options: { notify?: boolean } = {},
  ): void {
    const previous = [...this.selectedPaths()];
    const next = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);

    this.selectedPaths.set(next);
    this.syncFileItems();
    if (options.notify !== false) {
      this.notifyPathsChanged(previous);
    }
  }

  /** Moves one path by one slot; returns false at the edges. */
  movePath(path: string, offset: -1 | 1): boolean {
    const previous = [...this.selectedPaths()];
    const index = previous.indexOf(path);
    const target = index + offset;
    if (index < 0 || target < 0 || target >= previous.length) {
      return false;
    }

    const next = [...previous];
    [next[index], next[target]] = [next[target], next[index]];
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.notifyPathsChanged(previous);
    return true;
  }

  /** Moves one path to a final index (drag reorder); returns false as a no-op. */
  reorderPath(path: string, to: number): boolean {
    const previous = [...this.selectedPaths()];
    const from = previous.indexOf(path);
    if (from < 0 || to < 0 || to >= previous.length || to === from) {
      return false;
    }

    const next = [...previous];
    next.splice(from, 1);
    next.splice(to, 0, path);
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.notifyPathsChanged(previous);
    return true;
  }

  /** Rebuilds the queue, keeping enriched items for paths still present. */
  syncFileItems(): void {
    const paths = this.selectedPaths();
    const generation = (this.fileItemsGeneration += 1);
    const existing = new Map(
      this.fileItems().map((item) => [item.path, item]),
    );
    this.fileItems.set(
      paths.map(
        (path) => existing.get(path) ?? { path, name: fileName(path) },
      ),
    );

    if (paths.length > 0) {
      void this.enrichFileItems(paths, generation);
    }
  }

  async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (!this.config.canPlan()) {
      this.planToken += 1;
      this.planDirty = false;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponse = await this.config.wails
      .planJob(this.config.buildRequest())
      .catch(() => this.planFailure());

    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);
    this.planDirty = false;

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobError = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message:
        response.message ||
        this.config.i18n.t(this.config.messages.planFailed),
    };
    // Keep the failed plan so the shared plan card can show the conflict in
    // context, next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  schedulePlanRefresh(delayMs: number = DEFAULT_PLAN_DEBOUNCE_MS): void {
    this.planDirty = true;
    // Invalidate an in-flight plan: it was built from older options.
    this.planToken += 1;
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, delayMs);
  }

  clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  /** Drops the plan and any pending refresh (no valid request yet). */
  clearPlan(): void {
    this.planToken += 1;
    this.planDirty = false;
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.clearPlanRefreshTimer();
  }

  async run(): Promise<void> {
    if (!this.config.canRun() || this.config.extraBlocked?.()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    const shouldPlan =
      this.config.alwaysRefreshPlanBeforeRun !== false ||
      this.planLoading() ||
      this.planDirty ||
      this.plan()?.success !== true;
    if (shouldPlan) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    await this.submit();
  }

  async cancel(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    const response = await this.config.wails.cancelJob(jobId);
    if (!response.success) {
      this.actionMessage.set(
        response.message ||
          this.config.i18n.t(this.config.messages.cancelFailed),
      );
    }
  }

  async openPath(path: string): Promise<void> {
    const response = await this.config.wails.openPath(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message ||
            this.config.i18n.t(this.config.messages.openFileFailed),
    );
  }

  async revealPath(path: string): Promise<void> {
    const response = await this.config.wails.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message ||
            this.config.i18n.t(this.config.messages.revealFailed),
    );
  }

  /** Clears the previous outcome before a retry re-plans the same inputs. */
  clearRunOutcome(): void {
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.clearVerification();
    this.resetUndoState();
  }

  /** Adopts a job the result card planned and started with the same request. */
  adoptRunAgain(event: RunAgainStartedEvent): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.jobError.set(null);
    this.planError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.outputDir.set(event.request.outputDir);
    this.selectedPaths.set([...event.request.inputPaths]);
    this.config.onRunAgainAdopted?.(event.request);
    this.syncFileItems();
    this.isSubmitting.set(false);
    this.activeJobId.set(event.jobId);
    this.jobResult.set({
      jobId: event.jobId,
      success: false,
      message: this.config.i18n.t(this.config.messages.waiting),
      toolId: this.config.toolId,
      status: 'queued',
      progress: {
        current: 0,
        total: event.request.inputPaths.length,
        stage: 'queued',
        message: this.config.i18n.t(this.config.messages.waiting),
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isPolling.set(true);
    this.startObserving();
  }

  /** Stops local observation only; the backend job keeps running. */
  dispose(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  reset(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.planDirty = false;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.isPolling.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
    this.clearVerification();
    this.resetUndoState();
    this.autoVerifiedJobIds.clear();
  }

  /** Full read-back for the current terminal job, if any. */
  async verifyOutputs(jobId?: string): Promise<VerificationReport | null> {
    const id = (jobId ?? this.jobResult()?.jobId ?? '').trim();
    if (!id || typeof this.config.wails.verifyJob !== 'function') {
      this.setVerificationUnavailable();
      return null;
    }

    this.verificationLoading.set(true);
    this.verificationUnavailable.set(false);
    this.verification.set(null);

    let report: VerificationReport | null = null;
    try {
      report = await this.config.wails.verifyJob(id);
    } catch {
      report = null;
    }

    this.verificationLoading.set(false);
    if (
      !report ||
      !Array.isArray(report.outputs) ||
      report.outputs.length === 0
    ) {
      this.setVerificationUnavailable();
      return null;
    }

    this.verification.set(report);
    return report;
  }

  /** Moves the job outputs to the trash; a second call can restore them. */
  async undo(jobId?: string): Promise<UndoResult | null> {
    return this.applyJobUndo('undo', jobId);
  }

  async restore(jobId?: string): Promise<UndoResult | null> {
    return this.applyJobUndo('restore', jobId);
  }

  private notifyPathsChanged(previousPaths: readonly string[]): void {
    const paths = this.selectedPaths();
    if (this.config.afterPathsChanged) {
      this.config.afterPathsChanged(previousPaths, paths);
      return;
    }
    void this.refreshPlan();
  }

  private async submit(): Promise<void> {
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.clearVerification();
    this.resetUndoState();
    this.isSubmitting.set(true);

    const request = this.config.buildRequest();
    const response: RunJobResponse = await this.config.wails
      .runJob(request)
      .catch(() => this.runFailure());

    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message ||
            this.config.i18n.t(this.config.messages.start),
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    this.config.onRunAccepted?.(request, this.plan());
    // Stored at the single point where a submission is accepted, so a page
    // cannot forget to remember the request its result card can repeat.
    this.config.lastRequests.save(request);
    this.startJob(response.jobId, request.inputPaths.length);
  }

  private startJob(jobId: string, total: number): void {
    this.activeJobId.set(jobId);
    this.jobResult.set({
      jobId,
      success: false,
      message: this.config.i18n.t(this.config.messages.waiting),
      toolId: this.config.toolId,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: this.config.i18n.t(this.config.messages.waiting),
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isSubmitting.set(false);
    this.isPolling.set(true);
    this.startObserving();
  }

  private startObserving(): void {
    this.stopObserving();
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    this.isPolling.set(true);
    this.jobSubscription = observeJob(this.config.wails, jobId, {
      onProgress: (event) => this.applyJobProgress(event),
      onState: (result) => this.applyJobStatus(result),
      onMissing: (response) => this.handleMissingJob(response),
    });
  }

  private stopObserving(): void {
    if (this.jobSubscription) {
      this.jobSubscription.unsubscribe();
      this.jobSubscription = null;
    }
    this.isPolling.set(false);
  }

  private applyJobProgress(event: JobProgressEvent): void {
    if (!event) {
      return;
    }

    const current = this.jobResult();
    if (!current) {
      return;
    }

    this.jobResult.set({
      ...current,
      message: event.progress.message,
      progress: event.progress,
      // The terminal snapshot arrives on jobs/state; a progress event can
      // overtake the complete result, so the current status is kept.
      status: this.isTerminalStatus(event.status)
        ? current.status
        : (event.status as JobExecutionStatus),
    });

    if (this.isTerminalStatus(event.status)) {
      void this.reconcileJobStatus();
    }
  }

  private applyJobStatus(result: JobResult): void {
    if (!result) {
      return;
    }

    this.jobResult.set(result);
    if (this.isTerminalStatus(result.status)) {
      this.stopObserving();
      this.activeJobId.set('');
      void this.loadOutputInfos();
      this.autoVerify(result.jobId);
    }
  }

  /**
   * Read-back for a job that just settled. The result card renders whatever
   * this produces, so the page and the card never verify the same job twice.
   */
  private autoVerify(jobId: string): void {
    if (!jobId || this.autoVerifiedJobIds.has(jobId)) {
      return;
    }
    this.autoVerifiedJobIds.add(jobId);
    void this.verifyOutputs(jobId);
  }

  /** The backend no longer tracks the adopted job (restart or retention). */
  private handleMissingJob(response: JobStatusResponse): void {
    if (!this.activeJobId()) {
      return;
    }

    this.stopObserving();
    this.activeJobId.set('');
    this.jobError.set(
      response.error ?? {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'JOB_STATUS_MISSING',
        message:
          response.message ||
          this.config.i18n.t(this.config.messages.statusMissing),
      },
    );
  }

  /**
   * One-shot recovery when a terminal progress event arrives before its
   * jobs/state snapshot. This is never a timer, and it is a no-op once the
   * page has already settled the job.
   */
  private async reconcileJobStatus(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    const response = await this.config.wails
      .getJobStatus(jobId)
      .catch(() => null);
    if (!response || this.activeJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.handleMissingJob(response);
      return;
    }

    this.applyJobStatus(response.result);
  }

  private async loadOutputInfos(): Promise<void> {
    const paths =
      this.config.collectOutputPaths?.() ?? this.collectOutputPaths();
    if (paths.length === 0) {
      this.outputInfos.set({});
      return;
    }

    const response: InspectFilesResponse = await this.config.wails
      .inspectFiles(paths)
      .catch(() => ({
        success: false,
        message: this.config.i18n.t(this.config.messages.inspectFailed),
        files: [],
      }));
    if (!response.success) {
      this.outputInfos.set({});
      return;
    }

    const infos: Record<string, FileInfo> = {};
    for (const info of response.files) {
      if (info.path) {
        infos[info.path] = info;
      }
    }
    this.outputInfos.set(infos);
  }

  private collectOutputPaths(): string[] {
    const paths = new Set<string>();
    for (const item of this.jobResult()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs = (item.outputs ?? []).filter((path) => !!path);
      if (outputs.length > 0) {
        for (const output of outputs) {
          paths.add(output);
        }
      } else if (item.outputPath) {
        paths.add(item.outputPath);
      }
    }
    return [...paths];
  }

  private async enrichFileItems(
    paths: string[],
    generation: number,
  ): Promise<void> {
    try {
      const response = await this.config.wails.inspectFiles(paths);
      if (generation === this.fileItemsGeneration && response.success) {
        const infos = new Map<string, FileInfo>();
        for (const info of response.files) {
          infos.set(info.path, info);
        }

        this.fileItems.update((items) =>
          items.map((item) => {
            const info = infos.get(item.path);
            const meta = info ? this.config.describeFileInfo(info) : '';
            return meta ? { ...item, meta } : item;
          }),
        );
        this.config.onFilesInspected?.(response.files);
      }
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }

    if (generation !== this.fileItemsGeneration) {
      return;
    }
    if (this.config.loadThumbnails === false) {
      return;
    }

    for (const path of paths) {
      this.loadThumbnail(path, generation);
    }
  }

  private loadThumbnail(path: string, generation: number): void {
    if (typeof this.config.wails.getThumbnail !== 'function') {
      return;
    }

    void this.config.wails
      .getThumbnail(path, THUMBNAIL_SIZE)
      .then((thumbnailUrl) => {
        if (!thumbnailUrl || generation !== this.fileItemsGeneration) {
          return;
        }
        this.fileItems.update((items) =>
          items.map((item) =>
            item.path === path ? { ...item, thumbnailUrl } : item,
          ),
        );
      })
      .catch(() => {
        // Thumbnails are optional; keep the placeholder icon.
      });
  }

  private intakeNotice(
    rejected: string[],
    duplicates: string[],
  ): { tone: 'warning' | 'danger'; text: string } | null {
    if (this.config.noticeText) {
      return this.config.noticeText(rejected, duplicates);
    }

    const parts: string[] = [];
    const keys = this.config.notice;
    if (!keys) {
      return null;
    }

    if (rejected.length === 1) {
      parts.push(
        this.config.i18n.t(keys.rejectedOne, {
          name: fileName(rejected[0]),
        }),
      );
    } else if (rejected.length > 1) {
      const params: Record<string, string | number> =
        keys.rejectedManyFormat === 'names'
          ? { names: rejected.map((path) => fileName(path)).join(', ') }
          : { count: rejected.length };
      parts.push(this.config.i18n.t(keys.rejectedMany, params));
    }

    if (duplicates.length === 1) {
      parts.push(
        this.config.i18n.t(keys.duplicateOne, {
          name: fileName(duplicates[0]),
        }),
      );
    } else if (duplicates.length > 1) {
      parts.push(
        this.config.i18n.t(keys.duplicateMany, {
          count: duplicates.length,
        }),
      );
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  private async applyJobUndo(
    action: 'undo' | 'restore',
    jobId?: string,
  ): Promise<UndoResult | null> {
    const id = (jobId ?? this.jobResult()?.jobId ?? '').trim();
    if (!id || this.undoBusy()) {
      return null;
    }

    const methodName = action === 'undo' ? 'undoJob' : 'restoreJob';
    const method = (
      this.config.wails as unknown as Record<string, unknown>
    )[methodName];
    if (typeof method !== 'function') {
      return null;
    }

    this.undoBusy.set(true);
    this.undoMessage.set('');
    this.undoSkipped.set([]);

    try {
      const result = await (
        method as (jobId: string) => Promise<UndoResult>
      ).call(this.config.wails, id);
      this.applyUndoResult(result, action);
      return result;
    } catch {
      this.undoMessage.set(
        this.config.i18n.t(
          action === 'undo'
            ? 'resultCard.undoFailed'
            : 'resultCard.restoreFailed',
        ),
      );
      this.undoTone.set('warning');
      this.undoSkipped.set([]);
      return null;
    } finally {
      this.undoBusy.set(false);
    }
  }

  private applyUndoResult(
    result: UndoResult | null | undefined,
    action: 'undo' | 'restore',
  ): void {
    const skipped = Array.isArray(result?.skipped) ? result.skipped : [];
    this.undoSkipped.set(skipped);
    this.undoMessage.set(result?.message ?? '');
    this.undoTone.set(
      result?.success && skipped.length === 0 ? 'success' : 'warning',
    );

    if (result?.success) {
      this.undoMode.set(action === 'undo' ? 'restore' : 'undo');
    }
  }

  private resetUndoState(): void {
    this.undoBusy.set(false);
    this.undoMode.set('undo');
    this.undoMessage.set('');
    this.undoSkipped.set([]);
    this.undoTone.set('success');
  }

  private clearVerification(): void {
    this.verification.set(null);
    this.verificationLoading.set(false);
    this.verificationUnavailable.set(false);
  }

  private setVerificationUnavailable(): void {
    this.verificationLoading.set(false);
    this.verification.set(null);
    this.verificationUnavailable.set(true);
  }

  private isTerminalStatus(status: string): boolean {
    return isTerminalJobStatus(status);
  }

  private planFailure(): PlanJobResponse {
    const message = this.config.i18n.t(this.config.messages.planFailed);
    return {
      success: false,
      message,
      outputs: [],
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'PLAN_JOB_FAILED',
        message,
      },
    };
  }

  private runFailure(): RunJobResponse {
    const message = this.config.i18n.t(this.config.messages.start);
    return {
      success: false,
      message,
      jobId: '',
      status: 'failed',
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'RUN_JOB_FAILED',
        message,
      },
    };
  }
}

/**
 * Windows and UNC paths are case-insensitive, so the same file dropped with
 * different casing is still a duplicate there.
 */
export function toolPathKey(path: string): string {
  const normalized = path.trim().replace(/\\/g, '/');
  return /^[a-zA-Z]:\//.test(normalized) || normalized.startsWith('//')
    ? normalized.toLowerCase()
    : normalized;
}
