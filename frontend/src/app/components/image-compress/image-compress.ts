import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
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
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const IMAGE_TOOL_ID = 'tool.image.compress';
const SETTINGS_PLAN_DEBOUNCE_MS = 200;
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
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: IMAGE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: IMAGE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: this.lastRequests,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && this.targetValid(),
    canRun: () => this.canRun(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (_previous, paths) => {
      if (paths.length === 0) {
        this.inputInfos.set({});
      }
      void this.job.refreshPlan();
    },
    onFilesInspected: (infos) => this.applyInputInfos(infos),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'imageCompress.error.start',
      planFailed: 'imageCompress.error.plan',
      waiting: 'imageCompress.status.waiting',
      inspectFailed: 'imageCompress.error.inspect',
      statusMissing: 'imageCompress.error.status',
      cancelFailed: 'imageCompress.error.cancel',
      openFileFailed: 'imageCompress.error.openFile',
      revealFailed: 'imageCompress.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
      this.runTargetBytes.set(this.targetSizeBytes());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly formatOptions = FORMAT_OPTIONS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly inputInfos = signal<Record<string, FileInfo>>({});

  readonly targetMb = signal<number | null>(1);
  readonly format = signal<CompressFormat>('auto');
  readonly allowDownscale = signal(true);
  readonly maxDimension = signal<number | null>(null);
  readonly quality = signal<number | null>(null);
  readonly stripMetadata = signal(true);
  readonly outputDir = this.job.outputDir;
  readonly advancedOpen = signal(false);

  readonly plan = this.job.plan;
  readonly planLoading = this.job.planLoading;

  readonly jobResult = this.job.jobResult;
  readonly jobError = this.job.jobError;
  readonly outputInfos = this.job.outputInfos;

  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly activeJobId = this.job.activeJobId;
  /** Target captured when the running job started, used for its report. */
  readonly runTargetBytes = signal(0);

  readonly notice = this.job.notice;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;

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

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/image-compress');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
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
    this.job.onPathsSelected(paths);
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.job.onPathsSelected(paths);
    }
  }

  clear(): void {
    this.job.clear();
  }

  removeFile(path: string): void {
    this.job.removeFile(path);
  }

  onTargetInput(event: Event): void {
    const raw = this.readValue(event).trim();
    const value = Number.parseFloat(raw);
    this.targetMb.set(raw === '' || !Number.isFinite(value) ? null : value);
    this.actionMessage.set('');
    this.schedulePlan();
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
    void this.job.refreshPlan();
  }

  onAllowDownscaleToggle(event: Event): void {
    this.allowDownscale.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onMaxDimensionInput(event: Event): void {
    this.maxDimension.set(this.readPositiveInteger(event));
    this.schedulePlan();
  }

  onQualityInput(event: Event): void {
    const value = this.readNumber(event);
    this.quality.set(
      value === null
        ? null
        : Math.min(MAX_QUALITY, Math.max(MIN_QUALITY, Math.round(value))),
    );
    this.schedulePlan();
  }

  onStripMetadataToggle(event: Event): void {
    this.stripMetadata.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  resetAdvanced(): void {
    this.maxDimension.set(null);
    this.quality.set(null);
    this.stripMetadata.set(true);
    this.schedulePlan();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.job.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  async cancel(): Promise<void> {
    await this.job.cancel();
  }

  async onOpen(path: string): Promise<void> {
    await this.job.openPath(path);
  }

  async onReveal(path: string): Promise<void> {
    await this.job.revealPath(path);
  }

  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    this.job.replacePaths([...new Set(failed)], { notify: false });
    this.job.clearRunOutcome();

    const planned = await this.job.refreshPlan();
    if (!planned) {
      return;
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.inputInfos.set({});
    this.runTargetBytes.set(0);
    this.restoreOptions();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
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

  private applyInputInfos(infos: FileInfo[]): void {
    const inputInfos: Record<string, FileInfo> = {};
    for (const info of infos) {
      if (info.path) {
        inputInfos[info.path] = info;
      }
    }
    this.inputInfos.set(inputInfos);
  }

  /**
   * Mirrors the old scheduler: an invalid target cannot be planned, so the
   * stale plan is dropped at once instead of showing for one debounce tick.
   */
  private schedulePlan(): void {
    if (this.selectedPaths().length === 0 || !this.targetValid()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(SETTINGS_PLAN_DEBOUNCE_MS);
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

  private collectOutputPaths(): string[] {    const paths = new Set<string>();
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

  private rejectionNotice(rejected: readonly string[]): string {
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
}
