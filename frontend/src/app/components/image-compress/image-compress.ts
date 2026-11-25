import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Subscription } from 'rxjs';
import {
  FolderOpen,
  LucideAngularModule,
  RotateCcw,
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

const IMAGE_TOOL_ID = 'tool.image.compress';
const SETTINGS_PLAN_DEBOUNCE_MS = 200;
const THUMBNAIL_SIZE = 160;
const BYTES_PER_MB = 1024 * 1024;
const MIN_QUALITY = 40;
// The compressor's quality search ceiling is 95, so the UI must not advertise
// more than the encoder can use.
const MAX_QUALITY = 95;
// int64 can hold far more, but anything above ~1 TB per image is not a real
// target and would only produce an unusable request.
const MAX_TARGET_MB = 1_000_000;
const MAX_TARGET_MB_LABEL = '1,000,000';
const OPTIONS_SCHEMA_VERSION = 1;

const SUPPORTED_IMAGE_EXTENSIONS = new Set([
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'tiff',
  'tif',
]);

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type CompressFormat = 'auto' | 'jpeg' | 'webp' | 'png';

interface SelectOption<T extends string> {
  value: T;
  label: string;
}

const FORMAT_OPTIONS: readonly SelectOption<CompressFormat>[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' },
  { value: 'png', label: 'PNG' },
];

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

type ImageCompressOptions = {
  targetMb: number | null;
  format: CompressFormat;
  allowDownscale: boolean;
  maxDimension: number | null;
  quality: number | null;
  stripMetadata: boolean;
};

function defaultOptions(): ImageCompressOptions {
  return {
    targetMb: 1,
    format: 'auto',
    allowDownscale: true,
    maxDimension: null,
    quality: null,
    stripMetadata: true,
  };
}

function isFormats(value: unknown): value is CompressFormat {
  return FORMAT_OPTIONS.some((option) => option.value === value);
}

/** Drops values that no longer fit the option ranges, keeping the rest. */
function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageCompressOptions> {
  const result: Partial<ImageCompressOptions> = {};

  const targetMb = stored['targetMb'];
  if (
    typeof targetMb === 'number' &&
    Number.isFinite(targetMb) &&
    targetMb > 0 &&
    targetMb <= MAX_TARGET_MB
  ) {
    result.targetMb = targetMb;
  }

  if (isFormats(stored['format'])) {
    result.format = stored['format'];
  }

  if (typeof stored['allowDownscale'] === 'boolean') {
    result.allowDownscale = stored['allowDownscale'];
  }
  if (typeof stored['stripMetadata'] === 'boolean') {
    result.stripMetadata = stored['stripMetadata'];
  }

  const maxDimension = stored['maxDimension'];
  if (
    typeof maxDimension === 'number' &&
    Number.isFinite(maxDimension) &&
    maxDimension > 0
  ) {
    result.maxDimension = Math.round(maxDimension);
  }

  const quality = stored['quality'];
  if (
    typeof quality === 'number' &&
    Number.isInteger(quality) &&
    quality >= MIN_QUALITY &&
    quality <= MAX_QUALITY
  ) {
    result.quality = quality;
  }

  return result;
}

@Component({
  selector: 'app-image-compress',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-compress.html',
  styleUrl: './image-compress.css',
})
export class ImageCompress implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly lastRequests = inject(LastRequestService);
  private readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: IMAGE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly formatOptions = FORMAT_OPTIONS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly inputInfos = signal<Record<string, FileInfo>>({});

  readonly targetMb = signal<number | null>(1);
  readonly format = signal<CompressFormat>('auto');
  readonly allowDownscale = signal(true);
  readonly maxDimension = signal<number | null>(null);
  readonly quality = signal<number | null>(null);
  readonly stripMetadata = signal(true);
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly advancedOpen = signal(false);

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');
  /** Target captured when the running job started, used for its report. */
  readonly runTargetBytes = signal(0);

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly targetSizeBytes = computed(() => {
    const target = this.targetMb();
    if (
      target === null ||
      !Number.isFinite(target) ||
      target <= 0 ||
      target > MAX_TARGET_MB
    ) {
      return 0;
    }

    const bytes = target * BYTES_PER_MB;
    if (!Number.isFinite(bytes) || bytes <= 0) {
      return 0;
    }

    return Math.max(1, Math.round(bytes));
  });

  readonly targetValid = computed(() => this.targetSizeBytes() > 0);

  /**
   * The specific reason the target cannot be used. Kept separate from
   * `targetValid` so the UI never has to guess why the run is disabled or show
   * a bogus `0 B` target.
   */
  readonly targetError = computed<string | null>(() => {
    const target = this.targetMb();
    if (target === null || !Number.isFinite(target) || target <= 0) {
      return this.i18n.t('imageCompress.target.errorPositive');
    }
    if (target > MAX_TARGET_MB) {
      return this.i18n.t('imageCompress.target.errorMax', {
        max: MAX_TARGET_MB_LABEL,
      });
    }
    if (!this.targetValid()) {
      return this.i18n.t('imageCompress.target.errorTooLarge');
    }
    return null;
  });

  readonly targetLabel = computed(() =>
    this.targetValid()
      ? formatBytes(this.targetSizeBytes())
      : this.i18n.t('imageCompress.target.noTarget'),
  );

  readonly targetSummary = computed(() =>
    this.targetValid()
      ? this.i18n.t('imageCompress.target.summary', {
          size: this.targetLabel(),
        })
      : '',
  );

  readonly formatBadge = computed(() => this.formatOptionLabel(this.format()));

  readonly inputTotalBytes = computed(() => {
    const infos = this.inputInfos();
    let total = 0;
    for (const path of this.selectedPaths()) {
      const size = infos[path]?.sizeBytes;
      if (typeof size === 'number' && size > 0) {
        total += size;
      }
    }
    return total;
  });

  readonly outputTotalBytes = computed(() => {
    let total = 0;
    for (const info of Object.values(this.outputInfos())) {
      if (typeof info.sizeBytes === 'number' && info.sizeBytes > 0) {
        total += info.sizeBytes;
      }
    }
    return total;
  });

  readonly savingsSummary = computed<string | null>(() => {
    const input = this.inputTotalBytes();
    const output = this.outputTotalBytes();
    if (input <= 0 || output <= 0 || input === output) {
      return null;
    }
    if (output < input) {
      const saved = input - output;
      const percent = Math.max(1, Math.round((saved / input) * 100));
      return this.i18n.t('imageCompress.savings.saved', {
        size: formatBytes(saved),
        percent,
      });
    }
    return this.i18n.t('imageCompress.savings.larger', {
      size: formatBytes(output - input),
    });
  });

  /**
   * Honest target report built from the inspected output sizes, so a batch
   * that misses the target never looks like a clean win.
   */
  readonly targetOutcome = computed<string | null>(() => {
    const job = this.terminalJob();
    const target = this.runTargetBytes();
    if (!job || target <= 0) {
      return null;
    }

    const infos = this.outputInfos();
    const sizes: number[] = [];
    for (const path of this.collectOutputPaths()) {
      const size = infos[path]?.sizeBytes;
      if (typeof size === 'number' && size > 0) {
        sizes.push(size);
      }
    }
    if (sizes.length === 0) {
      return null;
    }

    const targetLabel = formatBytes(target);
    const atTarget = sizes.filter((size) => size <= target).length;
    if (atTarget === sizes.length) {
      return sizes.length === 1
        ? this.i18n.t('imageCompress.outcome.single', { target: targetLabel })
        : this.i18n.t('imageCompress.outcome.all', {
            count: sizes.length,
            target: targetLabel,
          });
    }
    if (atTarget === 0) {
      return this.i18n.t('imageCompress.outcome.none', {
        target: targetLabel,
      });
    }
    return this.i18n.t('imageCompress.outcome.partial', {
      atTarget,
      total: sizes.length,
      target: targetLabel,
    });
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageCompress.action.compressing');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageCompress.action.compress');
    }
    return count === 1
      ? this.i18n.t('imageCompress.action.compressOne')
      : this.i18n.t('imageCompress.action.compressMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageCompress.destination.original'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.targetValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('imageCompress.helper.addImage');
    }
    if (!this.targetValid()) {
      return (
        this.targetError() ?? this.i18n.t('imageCompress.target.errorPositive')
      );
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
            this.i18n.t('imageCompress.error.start'),
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
    const pending = this.dropIntake.take('/image-compress');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  /** Format copy resolved at render time so the locale switch applies. */
  formatOptionLabel(format: CompressFormat): string {
    return this.i18n.tOr(
      `imageCompress.format.${format}.label`,
      FORMAT_OPTIONS.find((option) => option.value === format)?.label ?? format,
    );
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const supported: string[] = [];
    const rejected: string[] = [];
    for (const path of incoming) {
      if (SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
        supported.push(path);
      } else {
        rejected.push(path);
      }
    }

    const selected = new Set(this.selectedPaths());
    const added: string[] = [];
    for (const path of supported) {
      if (selected.has(path)) {
        continue;
      }
      selected.add(path);
      added.push(path);
    }

    this.notice.set(
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    );

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
    this.inputInfos.set({});
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

  onTargetInput(event: Event): void {
    const raw = this.readValue(event).trim();
    const value = Number.parseFloat(raw);
    this.targetMb.set(raw === '' || !Number.isFinite(value) ? null : value);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  onFormatChange(event: Event): void {
    const value = this.readValue(event) as CompressFormat;
    if (
      !FORMAT_OPTIONS.some((option) => option.value === value) ||
      value === this.format()
    ) {
      return;
    }
    this.format.set(value);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onAllowDownscaleToggle(event: Event): void {
    this.allowDownscale.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onMaxDimensionInput(event: Event): void {
    this.maxDimension.set(this.readPositiveInteger(event));
    this.schedulePlanRefresh();
  }

  onQualityInput(event: Event): void {
    const value = this.readNumber(event);
    this.quality.set(
      value === null
        ? null
        : Math.min(MAX_QUALITY, Math.max(MIN_QUALITY, Math.round(value))),
    );
    this.schedulePlanRefresh();
  }

  onStripMetadataToggle(event: Event): void {
    this.stripMetadata.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  resetAdvanced(): void {
    this.maxDimension.set(null);
    this.quality.set(null);
    this.stripMetadata.set(true);
    this.schedulePlanRefresh();
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
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is refreshed right before running so the destination and the
    // collision-safe names always match the current settings.
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
        message: this.i18n.t('imageCompress.error.start'),
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: this.i18n.t('imageCompress.error.start'),
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message || this.i18n.t('imageCompress.error.start'),
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    this.settings.save(this.currentOptions());
    this.lastRequests.save(request);

    const total = this.selectedPaths().length;
    this.activeJobId.set(response.jobId);
    this.runTargetBytes.set(this.targetSizeBytes());
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: this.i18n.t('imageCompress.status.waiting'),
      toolId: IMAGE_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: this.i18n.t('imageCompress.status.waiting'),
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
        response.message || this.i18n.t('imageCompress.error.cancel'),
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('imageCompress.error.openFile'),
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('imageCompress.error.openFolder'),
    );
  }

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

    await this.refreshPlan();
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
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.inputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.runTargetBytes.set(0);
    this.notice.set(null);
    this.actionMessage.set('');
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.outputDir.set(event.request.outputDir);
    this.selectedPaths.set([...event.request.inputPaths]);
    this.syncFileItems();
    this.isSubmitting.set(false);
    this.activeJobId.set(event.jobId);
    this.runTargetBytes.set(this.targetSizeBytes());
    this.jobResult.set({
      jobId: event.jobId,
      success: false,
      message: this.i18n.t('imageCompress.status.waiting'),
      toolId: IMAGE_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total: event.request.inputPaths.length,
        stage: 'queued',
        message: this.i18n.t('imageCompress.status.waiting'),
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isPolling.set(true);
    this.startObserving();
  }

  private currentOptions(): ImageCompressOptions {
    return {
      targetMb: this.targetMb(),
      format: this.format(),
      allowDownscale: this.allowDownscale(),
      maxDimension: this.maxDimension(),
      quality: this.quality(),
      stripMetadata: this.stripMetadata(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.targetMb.set(options.targetMb);
    this.format.set(options.format);
    this.allowDownscale.set(options.allowDownscale);
    this.maxDimension.set(options.maxDimension);
    this.quality.set(options.quality);
    this.stripMetadata.set(options.stripMetadata);
  }

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {
      targetSizeBytes: this.targetSizeBytes(),
      format: this.format(),
      allowDownscale: this.allowDownscale(),
      stripMetadata: this.stripMetadata(),
    };

    const maxDimension = this.maxDimension();
    if (maxDimension && maxDimension > 0) {
      options['maxDimension'] = maxDimension;
    }

    const quality = this.quality();
    if (quality !== null) {
      options['quality'] = quality;
    }

    const paths = this.selectedPaths();
    return {
      toolId: IMAGE_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length === 0 || !this.targetValid()) {
      this.clearPlanState();
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);

    const response: PlanJobResponse = await this.wailsService
      .planJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: this.i18n.t('imageCompress.error.plan'),
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: this.i18n.t('imageCompress.error.plan'),
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
      message: response.message || this.i18n.t('imageCompress.error.plan'),
    };
    // Keep the failed plan so the shared plan card can show the problem next
    // to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    return false;
  }

  private clearPlanState(): void {
    this.planToken += 1;
    this.plan.set(null);
    this.planLoading.set(false);
    this.clearPlanRefreshTimer();
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    if (this.selectedPaths().length === 0 || !this.targetValid()) {
      this.clearPlanState();
      return;
    }
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, SETTINGS_PLAN_DEBOUNCE_MS);
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
      toolId: IMAGE_TOOL_ID,
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
        message: response.message || this.i18n.t('imageCompress.error.status'),
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
        message: this.i18n.t('imageCompress.error.inspect'),
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
    this.fileItems.set(paths.map((path) => ({ path, name: fileName(path) })));

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
        const inputInfos: Record<string, FileInfo> = {};
        for (const info of response.files) {
          infos.set(info.path, info);
          if (info.path) {
            inputInfos[info.path] = info;
          }
        }
        this.inputInfos.set(inputInfos);

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
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
    }
    return parts.join(' · ');
  }

  private rejectionNotice(rejected: string[]): string {
    const extensions = [
      ...new Set(
        rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('imageCompress.notice.filesOne')
        : this.i18n.t('imageCompress.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('imageCompress.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('imageCompress.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readNumber(event: Event): number | null {
    const raw = this.readValue(event);
    if (raw.trim() === '') {
      return null;
    }
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) ? parsed : null;
  }

  private readPositiveInteger(event: Event): number | null {
    const value = this.readNumber(event);
    return value === null || value <= 0 ? null : Math.max(1, Math.round(value));
  }

  private readValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readChecked(event: Event): boolean {
    return !!(event.target as HTMLInputElement | null)?.checked;
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
