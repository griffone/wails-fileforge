import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
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
import { ResultCard } from '../tool-flow/result-card';
import {
  IMAGE_FORMAT_OPTIONS,
  IMAGE_PRESETS,
  ImageOutputFormat,
  ImagePreset,
} from '../../config/image-presets';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
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

const IMAGE_TOOL_ID = 'tool.image.convert';
const ADVANCED_PLAN_DEBOUNCE_MS = 200;
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

type ResizeMode = 'none' | 'longestEdge' | 'width';

interface SelectOption<T extends string | number> {
  value: T;
  label: string;
}

const RESIZE_MODES: readonly SelectOption<ResizeMode>[] = [
  { value: 'none', label: 'None' },
  { value: 'longestEdge', label: 'Longest edge' },
  { value: 'width', label: 'Exact width' },
];

const ROTATE_OPTIONS: readonly SelectOption<number>[] = [
  { value: 0, label: '0°' },
  { value: 90, label: '90°' },
  { value: 180, label: '180°' },
  { value: 270, label: '270°' },
];

type ImageConverterOptions = {
  selectedPresetId: string;
  format: ImageOutputFormat;
  quality: number | null;
  resizeMode: ResizeMode;
  longestEdge: number | null;
  width: number | null;
  stripMetadata: boolean;
  rotate: number;
  flipH: boolean;
  flipV: boolean;
};

function defaultOptions(): ImageConverterOptions {
  return {
    selectedPresetId: 'webp',
    format: 'webp',
    quality: null,
    resizeMode: 'none',
    longestEdge: null,
    width: null,
    stripMetadata: false,
    rotate: 0,
    flipH: false,
    flipV: false,
  };
}

function isOutputFormat(value: unknown): value is ImageOutputFormat {
  return IMAGE_FORMAT_OPTIONS.some((option) => option.value === value);
}

function isResizeMode(value: unknown): value is ResizeMode {
  return RESIZE_MODES.some((option) => option.value === value);
}

function isPresetId(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    IMAGE_PRESETS.some((preset) => preset.id === value)
  );
}

function isRotate(value: unknown): value is number {
  return ROTATE_OPTIONS.some((option) => option.value === value);
}

function positiveIntegerOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? Math.round(value)
    : null;
}

function qualityOrNull(value: unknown): number | null {
  return typeof value === 'number' &&
    Number.isInteger(value) &&
    value >= 1 &&
    value <= 100
    ? value
    : null;
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageConverterOptions> {
  const result: Partial<ImageConverterOptions> = {};

  if (isPresetId(stored['selectedPresetId'])) {
    result.selectedPresetId = stored['selectedPresetId'];
  }
  if (isOutputFormat(stored['format'])) {
    result.format = stored['format'];
  }
  if ('quality' in stored) {
    result.quality = qualityOrNull(stored['quality']);
  }
  if (isResizeMode(stored['resizeMode'])) {
    result.resizeMode = stored['resizeMode'];
  }
  if ('longestEdge' in stored) {
    result.longestEdge = positiveIntegerOrNull(stored['longestEdge']);
  }
  if ('width' in stored) {
    result.width = positiveIntegerOrNull(stored['width']);
  }
  if (typeof stored['stripMetadata'] === 'boolean') {
    result.stripMetadata = stored['stripMetadata'];
  }
  if (isRotate(stored['rotate'])) {
    result.rotate = stored['rotate'];
  }
  if (typeof stored['flipH'] === 'boolean') {
    result.flipH = stored['flipH'];
  }
  if (typeof stored['flipV'] === 'boolean') {
    result.flipV = stored['flipV'];
  }

  return result;
}

@Component({
  selector: 'app-image-converter',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-converter.html',
  styleUrl: './image-converter.css',
})
export class ImageConverter implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
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
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0,
    canRun: () => this.canRun(),
    alwaysRefreshPlanBeforeRun: false,
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
      start: 'imageConverter.error.start',
      planFailed: 'imageConverter.error.plan',
      waiting: 'imageConverter.status.waiting',
      inspectFailed: 'imageConverter.error.inspect',
      statusMissing: 'imageConverter.error.status',
      cancelFailed: 'imageConverter.error.cancel',
      openFileFailed: 'imageConverter.error.openFile',
      revealFailed: 'imageConverter.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly presets = IMAGE_PRESETS;
  readonly formatOptions = IMAGE_FORMAT_OPTIONS;
  readonly resizeModes = RESIZE_MODES;
  readonly rotateOptions = ROTATE_OPTIONS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly outputDir = this.job.outputDir;
  readonly selectedPresetId = signal('webp');
  readonly format = signal<ImageOutputFormat>('webp');
  readonly advancedOpen = signal(false);
  readonly quality = signal<number | null>(null);
  readonly resizeMode = signal<ResizeMode>('none');
  readonly longestEdge = signal<number | null>(null);
  readonly width = signal<number | null>(null);
  readonly stripMetadata = signal(false);
  readonly rotate = signal(0);
  readonly flipH = signal(false);
  readonly flipV = signal(false);
  readonly inputInfos = signal<Record<string, FileInfo>>({});

  readonly plan = this.job.plan;
  readonly planError = this.job.planError;
  readonly planLoading = this.job.planLoading;

  readonly jobResult = this.job.jobResult;
  readonly jobError = this.job.jobError;
  readonly outputInfos = this.job.outputInfos;

  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly activeJobId = this.job.activeJobId;

  readonly notice = this.job.notice;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;

  readonly selectedPreset = computed<ImagePreset>(
    () =>
      IMAGE_PRESETS.find((preset) => preset.id === this.selectedPresetId()) ??
      IMAGE_PRESETS[0],
  );
  readonly isPng = computed(() => this.format() === 'png');
  readonly isAutoFormat = computed(() => this.format() === 'auto');
  readonly presetQuality = computed(
    () =>
      IMAGE_PRESETS.find((preset) => preset.format === this.format())
        ?.quality ?? 85,
  );
  readonly qualityValue = computed(
    () => this.quality() ?? this.presetQuality(),
  );
  readonly outputBadgeLabel = computed(() => {
    const format = this.format();
    const preset = IMAGE_PRESETS.find(
      (candidate) => candidate.format === format,
    );
    return preset ? this.presetLabel(preset) : this.formatLabel(format);
  });

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
      return this.i18n.t('imageConverter.savings.saved', {
        size: formatBytes(saved),
        percent,
      });
    }
    return this.i18n.t('imageConverter.savings.larger', {
      size: formatBytes(output - input),
    });
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageConverter.action.converting');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageConverter.action.convert');
    }
    return count === 1
      ? this.i18n.t('imageConverter.action.convertOne')
      : this.i18n.t('imageConverter.action.convertMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageConverter.destination.original'),
  );
  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );
  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/image-converter');
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

  /** Preset copy resolved at render time so the locale switch applies. */
  presetLabel(preset: ImagePreset): string {
    return this.i18n.tOr(
      `imageConverter.preset.${preset.id}.label`,
      preset.label,
    );
  }

  presetHint(preset: ImagePreset): string {
    return this.i18n.tOr(
      `imageConverter.preset.${preset.id}.hint`,
      preset.hint,
    );
  }

  resizeModeLabel(mode: ResizeMode): string {
    return this.i18n.tOr(
      `imageConverter.resize.${mode}`,
      this.resizeModes.find((option) => option.value === mode)?.label ?? mode,
    );
  }

  formatOptionLabel(format: ImageOutputFormat): string {
    return this.i18n.tOr(
      `imageConverter.format.${format}.label`,
      this.formatOptions.find((option) => option.value === format)?.label ??
        format,
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

  selectPreset(presetId: string): void {
    const preset = IMAGE_PRESETS.find((candidate) => candidate.id === presetId);
    if (!preset) {
      return;
    }
    if (presetId === this.selectedPresetId() && preset.format === this.format()) {
      return;
    }
    this.selectedPresetId.set(presetId);
    this.format.set(preset.format);
    this.quality.set(null);
    void this.job.refreshPlan();
  }

  isPresetActive(preset: ImagePreset): boolean {
    return preset.format === this.format();
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

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  onQualityInput(event: Event): void {
    if (this.isPng()) {
      return;
    }
    const value = this.readNumber(event);
    this.quality.set(
      value === null ? null : Math.min(100, Math.max(1, Math.round(value))),
    );
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onFormatChange(event: Event): void {
    const value = this.readValue(event) as ImageOutputFormat;
    if (
      !IMAGE_FORMAT_OPTIONS.some((option) => option.value === value) ||
      value === this.format()
    ) {
      return;
    }
    this.format.set(value);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onResizeModeChange(event: Event): void {
    const value = this.readValue(event) as ResizeMode;
    if (!RESIZE_MODES.some((mode) => mode.value === value)) {
      return;
    }
    this.resizeMode.set(value);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onLongestEdgeInput(event: Event): void {
    this.longestEdge.set(this.readPositiveInteger(event));
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onWidthInput(event: Event): void {
    this.width.set(this.readPositiveInteger(event));
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onStripMetadataToggle(event: Event): void {
    this.stripMetadata.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onRotateChange(event: Event): void {
    const value = Number.parseInt(this.readValue(event), 10);
    if (!ROTATE_OPTIONS.some((option) => option.value === value)) {
      return;
    }
    this.rotate.set(value);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onFlipToggle(axis: 'h' | 'v', event: Event): void {
    if (axis === 'h') {
      this.flipH.set(this.readChecked(event));
    } else {
      this.flipV.set(this.readChecked(event));
    }
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  resetAdvanced(): void {
    this.quality.set(null);
    this.format.set(this.selectedPreset().format);
    this.resizeMode.set('none');
    this.longestEdge.set(null);
    this.width.set(null);
    this.stripMetadata.set(false);
    this.rotate.set(0);
    this.flipH.set(false);
    this.flipV.set(false);
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
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
  }

  private currentOptions(): ImageConverterOptions {
    return {
      selectedPresetId: this.selectedPresetId(),
      format: this.format(),
      quality: this.quality(),
      resizeMode: this.resizeMode(),
      longestEdge: this.longestEdge(),
      width: this.width(),
      stripMetadata: this.stripMetadata(),
      rotate: this.rotate(),
      flipH: this.flipH(),
      flipV: this.flipV(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.selectedPresetId.set(options.selectedPresetId);
    this.format.set(options.format);
    this.quality.set(options.quality);
    this.resizeMode.set(options.resizeMode);
    this.longestEdge.set(options.longestEdge);
    this.width.set(options.width);
    this.stripMetadata.set(options.stripMetadata);
    this.rotate.set(options.rotate);
    this.flipH.set(options.flipH);
    this.flipV.set(options.flipV);
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

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {
      format: this.format(),
      quality: this.qualityValue(),
    };

    if (this.resizeMode() === 'longestEdge') {
      const longestEdge = this.longestEdge();
      if (longestEdge && longestEdge > 0) {
        options['longestEdge'] = longestEdge;
      }
    } else if (this.resizeMode() === 'width') {
      const width = this.width();
      if (width && width > 0) {
        options['width'] = width;
      }
    }

    if (this.stripMetadata()) {
      options['stripMetadata'] = true;
    }
    if (this.rotate() !== 0) {
      options['rotate'] = this.rotate();
    }
    if (this.flipH()) {
      options['flipH'] = true;
    }
    if (this.flipV()) {
      options['flipV'] = true;
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
        rejected.map(
          (path) => fileExtension(path).toUpperCase() || 'UNKNOWN',
        ),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('imageConverter.notice.filesOne')
        : this.i18n.t('imageConverter.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('imageConverter.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('imageConverter.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readNumber(event: Event): number | null {
    const raw = (event.target as HTMLInputElement | null)?.value ?? '';
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

  private formatLabel(format: ImageOutputFormat): string {
    return this.formatOptionLabel(format);
  }
}
