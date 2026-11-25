import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Subscription } from 'rxjs';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
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
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { observeJob } from '../../utils/job-stream';
import { resumeToolJob } from '../../utils/resume-task';
import { createToolOptions } from '../../utils/tool-options';

const PDF_OPTIMIZE_TOOL_ID = 'tool.pdf.optimize';
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 160;
const METADATA_MAX_LENGTH = 512;
const OPTIONS_SCHEMA_VERSION = 1;

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type MetadataKey = 'title' | 'author' | 'subject' | 'keywords' | 'creator';

interface MetadataFieldDefinition {
  key: MetadataKey;
  label: string;
  id: string;
  placeholder: string;
}

const METADATA_FIELDS: readonly MetadataFieldDefinition[] = [
  {
    key: 'title',
    label: 'Title',
    id: 'pdf-optimize-title',
    placeholder: 'Document title',
  },
  {
    key: 'author',
    label: 'Author',
    id: 'pdf-optimize-author',
    placeholder: 'Document author',
  },
  {
    key: 'subject',
    label: 'Subject',
    id: 'pdf-optimize-subject',
    placeholder: 'Document subject',
  },
  {
    key: 'keywords',
    label: 'Keywords',
    id: 'pdf-optimize-keywords',
    placeholder: 'Comma-separated keywords',
  },
  {
    key: 'creator',
    label: 'Creator',
    id: 'pdf-optimize-creator',
    placeholder: 'Application or source',
  },
];

type MetadataValues = Record<MetadataKey, string>;

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

function emptyMetadata(): MetadataValues {
  return {
    title: '',
    author: '',
    subject: '',
    keywords: '',
    creator: '',
  };
}

type PdfOptimizeOptions = {
  compress: boolean;
  metadata: MetadataValues;
};

function defaultOptions(): PdfOptimizeOptions {
  return { compress: true, metadata: emptyMetadata() };
}

/** Only known fields with valid shapes survive a stale or corrupt payload. */
function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfOptimizeOptions> {
  const result: Partial<PdfOptimizeOptions> = {};

  if (typeof stored['compress'] === 'boolean') {
    result.compress = stored['compress'];
  }

  const rawMetadata = stored['metadata'];
  if (typeof rawMetadata === 'object' && rawMetadata !== null) {
    const metadata = emptyMetadata();
    const record = rawMetadata as Record<string, unknown>;
    for (const field of METADATA_FIELDS) {
      const value = record[field.key];
      if (typeof value === 'string' && characterCount(value) <= METADATA_MAX_LENGTH) {
        metadata[field.key] = value;
      }
    }
    result.metadata = metadata;
  }

  return result;
}

/** Counts code points so the limit matches the backend's rune count. */
function characterCount(value: string): number {
  return [...value].length;
}

@Component({
  selector: 'app-pdf-optimize',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-optimize.html',
  styleUrl: './pdf-optimize.css',
})
export class PdfOptimize implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly lastRequests = inject(LastRequestService);
  private readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_OPTIMIZE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly metadataFields = METADATA_FIELDS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly compress = signal(true);
  readonly metadata = signal<MetadataValues>(emptyMetadata());

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planError = signal<JobError | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  /** True when a metadata value exceeds the backend's 512-character limit. */
  readonly metadataInvalid = computed(() =>
    METADATA_FIELDS.some(
      (field) =>
        characterCount(this.metadata()[field.key].trim()) > METADATA_MAX_LENGTH,
    ),
  );

  readonly hasMetadata = computed(() => {
    const values = this.metadata();
    return METADATA_FIELDS.some(
      (field) => values[field.key].trim().length > 0,
    );
  });

  readonly compressionOnly = computed(
    () => !this.compress() && !this.hasMetadata(),
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.metadataInvalid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('pdfOptimize.action.optimizing');
    }
    const count = this.selectedPaths().length;
    return count === 0
      ? this.i18n.t('pdfOptimize.action.optimize')
      : count === 1
        ? this.i18n.t('pdfOptimize.action.optimizeOne')
        : this.i18n.t('pdfOptimize.action.optimizeMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfOptimize.destination.each'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfOptimize.helper.addOne');
    }
    if (this.metadataInvalid()) {
      return this.i18n.t('pdfOptimize.helper.metadataTooLong', {
        max: METADATA_MAX_LENGTH,
      });
    }
    return '';
  });

  readonly terminalJob = computed<JobResult | null>(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
  });

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            error,
            this.i18n.t('pdfOptimize.error.start'),
          ),
        )
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {}

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  ngOnInit(): void {
    this.restoreOptions();
    const pending = this.dropIntake.take('/pdf-optimize');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  /** Metadata copy resolved at render time so the locale switch applies. */
  metadataLabel(field: MetadataFieldDefinition): string {
    return this.i18n.tOr(
      `pdfOptimize.metadata.${field.key}.label`,
      field.label,
    );
  }

  metadataPlaceholder(field: MetadataFieldDefinition): string {
    return this.i18n.tOr(
      `pdfOptimize.metadata.${field.key}.placeholder`,
      field.placeholder,
    );
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const rejected: string[] = [];
    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(this.selectedPaths().map((path) => this.pathKey(path)));

    for (const path of incoming) {
      if (fileExtension(path).toLowerCase() !== 'pdf') {
        rejected.push(path);
        continue;
      }

      const key = this.pathKey(path);
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
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.syncFileItems();
    void this.refreshPlan();
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.onPathsSelected(paths);
    }
  }

  clear(): void {
    if (this.selectedPaths().length === 0) {
      return;
    }
    this.selectedPaths.set([]);
    this.notice.set(null);
    this.actionMessage.set('');
    this.syncFileItems();
    void this.refreshPlan();
  }

  removeFile(path: string): void {
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }
    this.selectedPaths.set(next);
    this.syncFileItems();
    void this.refreshPlan();
  }

  onCompressToggle(event: Event): void {
    this.compress.set(!!(event.target as HTMLInputElement | null)?.checked);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onMetadataInput(key: MetadataKey, event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.metadata.update((current) => ({ ...current, [key]: value }));
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  resetAdvanced(): void {
    const empty = emptyMetadata();
    const values = this.metadata();
    const alreadyEmpty = METADATA_FIELDS.every(
      (field) => values[field.key] === empty[field.key],
    );
    if (alreadyEmpty && this.compress()) {
      return;
    }

    this.metadata.set(empty);
    this.compress.set(true);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.refreshPlan();
  }

  async run(): Promise<void> {
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for output names and destinations, so it
    // is always refreshed with the current inputs before starting the job.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const request = this.buildRequest();
    const response: RunJobResponse = await this.wailsService
      .runJob(request)
      .catch(() => ({
        success: false,
        message: this.i18n.t('pdfOptimize.error.start'),
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: this.i18n.t('pdfOptimize.error.start'),
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message || this.i18n.t('pdfOptimize.error.start'),
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    this.settings.save(this.currentOptions());
    this.lastRequests.save(request);

    const total = this.selectedPaths().length;
    this.activeJobId.set(response.jobId);
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: this.i18n.t('pdfOptimize.status.waiting'),
      toolId: PDF_OPTIMIZE_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: this.i18n.t('pdfOptimize.status.waiting'),
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isSubmitting.set(false);
    this.isPolling.set(true);
    this.startObserving();
  }

  async cancel(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }
    const response = await this.wailsService.cancelJob(jobId);
    if (!response.success) {
      this.actionMessage.set(
        response.message || this.i18n.t('pdfOptimize.error.cancel'),
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('pdfOptimize.error.openFile'),
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('pdfOptimize.error.openFolder'),
    );
  }

  /** Retries only the failed inputs with the same compression and metadata. */
  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    this.selectedPaths.set([...new Set(failed)]);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.syncFileItems();

    await this.run();
  }

  reset(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.restoreOptions();
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.jobError.set(null);
    this.planError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.plan.set(event.plan);
    this.outputDir.set(event.request.outputDir);
    this.selectedPaths.set([...event.request.inputPaths]);
    this.syncFileItems();
    this.isSubmitting.set(false);
    this.activeJobId.set(event.jobId);
    this.jobResult.set({
      jobId: event.jobId,
      success: false,
      message: this.i18n.t('pdfOptimize.status.waiting'),
      toolId: PDF_OPTIMIZE_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total: event.request.inputPaths.length,
        stage: 'queued',
        message: this.i18n.t('pdfOptimize.status.waiting'),
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isPolling.set(true);
    this.startObserving();
  }

  private currentOptions(): PdfOptimizeOptions {
    return { compress: this.compress(), metadata: { ...this.metadata() } };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.compress.set(options.compress);
    this.metadata.set({ ...options.metadata });
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const options: Record<string, unknown> = {};

    // Compression defaults to on in the backend, so only the opt-out is sent.
    if (!this.compress()) {
      options['compress'] = false;
    }

    const metadata = this.metadataPayload();
    if (Object.keys(metadata).length > 0) {
      options['metadata'] = metadata;
    }

    return {
      toolId: PDF_OPTIMIZE_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  /** Trims and omits blank fields; the backend keeps the existing values. */
  private metadataPayload(): Record<string, string> {
    const values = this.metadata();
    const payload: Record<string, string> = {};
    for (const field of METADATA_FIELDS) {
      const value = values[field.key].trim();
      if (value.length > 0) {
        payload[field.key] = value;
      }
    }
    return payload;
  }

  private async refreshPlan(): Promise<boolean> {
    if (
      this.selectedPaths().length === 0 ||
      this.metadataInvalid()
    ) {
      this.planToken += 1;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponse = await this.wailsService
      .planJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: this.i18n.t('pdfOptimize.error.plan'),
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: this.i18n.t('pdfOptimize.error.plan'),
        },
      }));
    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobError = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || this.i18n.t('pdfOptimize.error.plan'),
    };
    // Keep the failed plan so the shared plan card can show the metadata or
    // destination problem next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  private resumeBackgroundJob(): void {
    resumeToolJob({
      activity: this.activity,
      toolId: PDF_OPTIMIZE_TOOL_ID,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startObserving: () => this.startObserving(),
    });
  }

  private startObserving(): void {
    this.stopObserving();
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    this.isPolling.set(true);
    this.jobSubscription = observeJob(this.wailsService, jobId, {
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
    }
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
        message: response.message || this.i18n.t('pdfOptimize.error.status'),
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

    const response = await this.wailsService
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
    const paths = this.collectOutputPaths();
    if (paths.length === 0) {
      this.outputInfos.set({});
      return;
    }

    const response: InspectFilesResponse = await this.wailsService
      .inspectFiles(paths)
      .catch(() => ({
        success: false,
        message: this.i18n.t('pdfOptimize.error.inspect'),
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

      const outputs =
        item.outputs && item.outputs.length > 0
          ? item.outputs
          : item.outputPath
            ? [item.outputPath]
            : [];
      for (const output of outputs) {
        if (output) {
          paths.add(output);
        }
      }
    }
    return [...paths];
  }

  private syncFileItems(): void {
    const paths = this.selectedPaths();
    const generation = (this.fileItemsGeneration += 1);
    const existing = new Map(this.fileItems().map((item) => [item.path, item]));
    this.fileItems.set(
      paths.map(
        (path) => existing.get(path) ?? { path, name: fileName(path) },
      ),
    );

    if (paths.length > 0) {
      void this.enrichFileItems(paths, generation);
    }
  }

  private async enrichFileItems(
    paths: string[],
    generation: number,
  ): Promise<void> {
    try {
      const response = await this.wailsService.inspectFiles(paths);
      if (generation === this.fileItemsGeneration && response.success) {
        const infos = new Map<string, FileInfo>();
        for (const info of response.files) {
          infos.set(info.path, info);
        }

        this.fileItems.update((items) =>
          items.map((item) => {
            const info = infos.get(item.path);
            const meta = info ? this.describeFileInfo(info) : '';
            return meta ? { ...item, meta } : item;
          }),
        );
      }
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }

    if (generation !== this.fileItemsGeneration) {
      return;
    }

    for (const path of paths) {
      this.loadThumbnail(path, generation);
    }
  }

  private loadThumbnail(path: string, generation: number): void {
    void this.wailsService
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

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(
        formatCount(
          info.pageCount,
          this.i18n.t('common.pageOne'),
          this.i18n.t('common.pageMany'),
        ),
      );
    }
    return parts.join(' · ');
  }

  private intakeNotice(rejected: string[], duplicates: string[]): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        this.i18n.t('pdfOptimize.notice.rejectedOne', {
          name: fileName(rejected[0]),
        }),
      );
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(this.i18n.t('pdfOptimize.notice.rejectedMany', { names }));
    }

    if (duplicates.length === 1) {
      parts.push(
        this.i18n.t('pdfOptimize.notice.duplicateOne', {
          name: fileName(duplicates[0]),
        }),
      );
    } else if (duplicates.length > 1) {
      parts.push(
        this.i18n.t('pdfOptimize.notice.duplicateMany', {
          count: duplicates.length,
        }),
      );
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  /**
   * Windows and UNC paths are case-insensitive, so the same file dropped with
   * different casing is still a duplicate there.
   */
  private pathKey(path: string): string {
    const normalized = path.trim().replace(/\\/g, '/');
    return /^[a-zA-Z]:\//.test(normalized) || normalized.startsWith('//')
      ? normalized.toLowerCase()
      : normalized;
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
