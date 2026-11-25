import {
  Component,
  ElementRef,
  HostListener,
  OnDestroy,
  OnInit,
  ViewChild,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  Crop,
  FolderOpen,
  LucideAngularModule,
  RotateCcw,
  ScanLine,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
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
  ImageCropPreviewRequest,
  ImagePreviewSourceResponse,
  JobRequest,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const IMAGE_CROP_TOOL_ID = 'tool.image.crop';
const BOUNDS_PLAN_DEBOUNCE_MS = 200;
const CROP_PREVIEW_DEBOUNCE_MS = 200;
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

type RatioPresetId = 'free' | '1:1' | '4:3' | '16:9';

interface RatioPreset {
  id: RatioPresetId;
  label: string;
  hint: string;
  widthStep: number;
  heightStep: number;
}

const RATIO_PRESETS: readonly RatioPreset[] = [
  { id: 'free', label: 'Free', hint: 'Any shape', widthStep: 1, heightStep: 1 },
  { id: '1:1', label: '1:1', hint: 'Square', widthStep: 1, heightStep: 1 },
  { id: '4:3', label: '4:3', hint: 'Classic', widthStep: 4, heightStep: 3 },
  { id: '16:9', label: '16:9', hint: 'Wide', widthStep: 16, heightStep: 9 },
];

interface FormatOption {
  value: string;
  label: string;
}

const FORMAT_OPTIONS: readonly FormatOption[] = [
  { value: '', label: 'Preserve original' },
  { value: 'png', label: 'PNG' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' },
  { value: 'gif', label: 'GIF' },
  { value: 'tiff', label: 'TIFF' },
];

type BoundsAxis = 'x' | 'y' | 'width' | 'height';

type BoundsInput = Record<BoundsAxis, string>;

type RotateAngle = 0 | 90 | 180 | 270;

interface RotateOption {
  value: RotateAngle;
  label: string;
}

const ROTATE_OPTIONS: readonly RotateOption[] = [
  { value: 0, label: '0°' },
  { value: 90, label: '90°' },
  { value: 180, label: '180°' },
  { value: 270, label: '270°' },
];

type DragMode =
  | 'draw'
  | 'move'
  | 'resize-nw'
  | 'resize-ne'
  | 'resize-sw'
  | 'resize-se';

interface CropRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

const EMPTY_BOUNDS: BoundsInput = { x: '0', y: '0', width: '0', height: '0' };
const EMPTY_RECT: CropRect = { x: 0, y: 0, width: 0, height: 0 };

type ImageCropOptions = {
  ratioPreset: RatioPresetId;
  format: string;
  outputWidth: number | null;
  outputHeight: number | null;
  rotate: RotateAngle;
  flipH: boolean;
  flipV: boolean;
};

function defaultOptions(): ImageCropOptions {
  return {
    ratioPreset: 'free',
    format: '',
    outputWidth: null,
    outputHeight: null,
    rotate: 0,
    flipH: false,
    flipV: false,
  };
}

function positiveIntegerOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? Math.max(1, Math.round(value))
    : null;
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageCropOptions> {
  const result: Partial<ImageCropOptions> = {};

  const preset = stored['ratioPreset'];
  if (
    typeof preset === 'string' &&
    RATIO_PRESETS.some((candidate) => candidate.id === preset)
  ) {
    result.ratioPreset = preset as RatioPresetId;
  }

  if (typeof stored['format'] === 'string') {
    result.format = stored['format'];
  }

  if ('outputWidth' in stored) {
    result.outputWidth = positiveIntegerOrNull(stored['outputWidth']);
  }
  if ('outputHeight' in stored) {
    result.outputHeight = positiveIntegerOrNull(stored['outputHeight']);
  }

  const rotate = stored['rotate'];
  if (ROTATE_OPTIONS.some((option) => option.value === rotate)) {
    result.rotate = rotate as RotateAngle;
  }

  if (typeof stored['flipH'] === 'boolean') {
    result.flipH = stored['flipH'];
  }
  if (typeof stored['flipV'] === 'boolean') {
    result.flipV = stored['flipV'];
  }

  return result;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function readInteger(raw: string): number | null {
  const trimmed = raw.trim();
  if (!/^-?\d+$/.test(trimmed)) {
    return null;
  }
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : null;
}

@Component({
  selector: 'app-image-crop',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-crop.html',
  styleUrl: './image-crop.css',
})
export class ImageCrop implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  private readonly settings = createToolOptions(this.preferences, {
    toolId: IMAGE_CROP_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: IMAGE_CROP_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && this.cropValid(),
    canRun: () => this.canRun(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (previous) => {
      const firstChanged =
        (previous[0] ?? '') !== (this.selectedPaths()[0] ?? '');
      if (firstChanged) {
        void this.reloadPreviewAndPlan();
      } else {
        void this.job.refreshPlan();
      }
    },
    messages: {
      start: 'imageCrop.error.start',
      planFailed: 'imageCrop.error.plan',
      waiting: 'imageCrop.status.waiting',
      inspectFailed: 'imageCrop.error.inspect',
      statusMissing: 'imageCrop.error.status',
      cancelFailed: 'imageCrop.error.cancel',
      openFileFailed: 'imageCrop.error.openFile',
      revealFailed: 'imageCrop.error.openFolder',
    },
    notice: {
      rejectedOne: 'imageCrop.notice.rejectedOne',
      rejectedMany: 'imageCrop.notice.rejectedMany',
      duplicateOne: 'imageCrop.notice.duplicateOne',
      duplicateMany: 'imageCrop.notice.duplicateMany',
      rejectedManyFormat: 'count',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly cropIcon = Crop;
  readonly resetIcon = RotateCcw;
  readonly scanIcon = ScanLine;

  readonly ratioPresets = RATIO_PRESETS;
  readonly formatOptions = FORMAT_OPTIONS;
  readonly rotateOptions = ROTATE_OPTIONS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly previewUrl = signal('');
  readonly imageWidth = signal(0);
  readonly imageHeight = signal(0);
  readonly ratioPreset = signal<RatioPresetId>('free');
  readonly bounds = signal<BoundsInput>({ ...EMPTY_BOUNDS });
  readonly format = signal('');
  readonly outputWidth = signal<number | null>(null);
  readonly outputHeight = signal<number | null>(null);
  readonly rotate = signal<RotateAngle>(0);
  readonly flipH = signal(false);
  readonly flipV = signal(false);
  readonly outputDir = this.job.outputDir;
  readonly advancedOpen = signal(false);

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

  readonly previewLoading = signal(false);
  readonly previewError = signal('');
  readonly cropPreviewUrl = signal('');
  readonly isCropPreviewLoading = signal(false);
  readonly suggestionLoading = signal(false);
  readonly suggestionNotice = signal('');

  readonly selectionRectPx = signal<CropRect>({ ...EMPTY_RECT });

  readonly crop = computed<CropRect | null>(() => {
    const bounds = this.bounds();
    const x = readInteger(bounds.x);
    const y = readInteger(bounds.y);
    const width = readInteger(bounds.width);
    const height = readInteger(bounds.height);

    if (x === null || y === null || width === null || height === null) {
      return null;
    }

    return { x, y, width, height };
  });

  readonly hasSelection = computed(() => this.selectedPaths().length > 0);

  readonly hasPreview = computed(
    () =>
      this.previewUrl().length > 0 &&
      this.imageWidth() > 0 &&
      this.imageHeight() > 0,
  );

  readonly isBatch = computed(() => this.selectedPaths().length > 1);

  readonly cropValid = computed(() => {
    const crop = this.crop();
    const imageWidth = this.imageWidth();
    const imageHeight = this.imageHeight();

    if (!crop || imageWidth <= 0 || imageHeight <= 0) {
      return false;
    }
    if (crop.x < 0 || crop.y < 0 || crop.width < 1 || crop.height < 1) {
      return false;
    }
    if (
      crop.x + crop.width > imageWidth ||
      crop.y + crop.height > imageHeight
    ) {
      return false;
    }

    const preset = this.ratioDefinition();
    if (
      this.ratioPreset() !== 'free' &&
      crop.width * preset.heightStep !== crop.height * preset.widthStep
    ) {
      return false;
    }

    return true;
  });

  readonly ratioFitsImage = computed(() => {
    const preset = this.ratioDefinition();
    return (
      Math.floor(this.imageWidth() / preset.widthStep) >= 1 &&
      Math.floor(this.imageHeight() / preset.heightStep) >= 1
    );
  });

  readonly cropError = computed(() => {
    if (!this.hasSelection() || !this.hasPreview()) {
      return '';
    }

    const crop = this.crop();
    if (!crop) {
      return this.i18n.t('imageCrop.error.invalidNumbers');
    }
    if (crop.width < 1 || crop.height < 1) {
      return this.i18n.t('imageCrop.error.minSize');
    }
    if (crop.x < 0 || crop.y < 0) {
      return this.i18n.t('imageCrop.error.position');
    }
    if (
      crop.x + crop.width > this.imageWidth() ||
      crop.y + crop.height > this.imageHeight()
    ) {
      return this.i18n.t('imageCrop.error.outside');
    }

    const preset = this.ratioDefinition();
    if (
      this.ratioPreset() !== 'free' &&
      crop.width * preset.heightStep !== crop.height * preset.widthStep
    ) {
      if (!this.ratioFitsImage()) {
        return this.i18n.t('imageCrop.error.ratioTooSmall');
      }
      return this.i18n.t('imageCrop.error.ratioMismatch', {
        ratio: this.ratioPreset(),
      });
    }

    return '';
  });

  readonly cropSizeLabel = computed(() => {
    const crop = this.crop();
    return crop && crop.width > 0 && crop.height > 0
      ? `${crop.width} × ${crop.height} px`
      : '—';
  });

  readonly imageSizeLabel = computed(() =>
    this.imageWidth() > 0 && this.imageHeight() > 0
      ? `${this.imageWidth()} × ${this.imageHeight()} px`
      : '—',
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageCrop.action.cropping');
    }
    const count = this.selectedPaths().length;
    return count > 1
      ? this.i18n.t('imageCrop.action.cropMany', { count })
      : this.i18n.t('imageCrop.action.crop');
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageCrop.destination.original'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.cropValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly canDetectBorders = computed(
    () => this.hasPreview() && !this.suggestionLoading(),
  );

  readonly actionHelper = computed(() => {
    if (!this.hasSelection()) {
      return this.i18n.t('imageCrop.helper.addImage');
    }
    if (!this.hasPreview()) {
      return (
        this.previewError() || this.i18n.t('imageCrop.helper.loadingPreview')
      );
    }
    return this.cropError();
  });

  private cropPreviewTimer: ReturnType<typeof setTimeout> | null = null;
  private cropPreviewToken = 0;
  private previewGeneration = 0;

  private dragState:
    | {
        mode: DragMode;
        startX: number;
        startY: number;
        startRect: CropRect;
      }
    | undefined;

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/image-crop');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
    this.clearCropPreviewTimer();
  }

  @HostListener('window:resize')
  onWindowResize(): void {
    this.syncSelectionRectFromCrop();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  /** Ratio preset copy resolved at render time so locale switches apply. */
  ratioLabel(preset: RatioPreset): string {
    return this.i18n.tOr(`imageCrop.ratio.${preset.id}.label`, preset.label);
  }

  ratioHint(preset: RatioPreset): string {
    return this.i18n.tOr(`imageCrop.ratio.${preset.id}.hint`, preset.hint);
  }

  formatOptionLabel(option: FormatOption): string {
    const key = option.value || 'preserve';
    return this.i18n.tOr(`imageCrop.format.${key}.label`, option.label);
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

  selectRatio(presetId: RatioPresetId): void {
    if (presetId === this.ratioPreset()) {
      return;
    }

    this.ratioPreset.set(presetId);
    this.actionMessage.set('');

    if (this.hasPreview() && !this.cropValid()) {
      this.applyRatioCrop();
    }

    this.syncSelectionRectFromCrop();
    this.scheduleCropPreviewRefresh();
    void this.job.refreshPlan();
  }

  onBoundsInput(axis: BoundsAxis, event: Event): void {
    const value = this.readEventValue(event);
    this.bounds.update((bounds) => ({ ...bounds, [axis]: value }));
    this.actionMessage.set('');
    this.suggestionNotice.set('');
    this.syncSelectionRectFromCrop();
    this.job.schedulePlanRefresh(BOUNDS_PLAN_DEBOUNCE_MS);
    this.scheduleCropPreviewRefresh();
  }

  onFormatChange(event: Event): void {
    const value = this.readEventValue(event);
    if (value === this.format()) {
      return;
    }

    this.format.set(value);
    this.actionMessage.set('');
    this.scheduleCropPreviewRefresh();
    void this.job.refreshPlan();
  }

  onOutputSizeInput(axis: 'width' | 'height', event: Event): void {
    const value = this.readNumber(event);
    const normalized =
      value === null || value <= 0 ? null : Math.max(1, Math.round(value));

    if (axis === 'width') {
      this.outputWidth.set(normalized);
    } else {
      this.outputHeight.set(normalized);
    }
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(BOUNDS_PLAN_DEBOUNCE_MS);
  }

  onRotateChange(event: Event): void {
    const value = Number.parseInt(this.readEventValue(event), 10);
    if (!ROTATE_OPTIONS.some((option) => option.value === value)) {
      return;
    }

    this.rotate.set(value as RotateAngle);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onFlipToggle(axis: 'h' | 'v', event: Event): void {
    const checked = !!(event.target as HTMLInputElement | null)?.checked;
    if (axis === 'h') {
      this.flipH.set(checked);
    } else {
      this.flipV.set(checked);
    }
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  resetTransforms(): void {
    this.outputWidth.set(null);
    this.outputHeight.set(null);
    this.rotate.set(0);
    this.flipH.set(false);
    this.flipV.set(false);
    void this.job.refreshPlan();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
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

  onPreviewImageLoaded(): void {
    this.syncSelectionRectFromCrop();
  }

  onPreviewPointerDown(event: PointerEvent): void {
    if (!this.hasPreview()) {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode: 'draw',
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx() },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onSelectionHandlePointerDown(
    event: PointerEvent,
    mode: DragMode,
  ): void {
    if (!this.hasPreview()) {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    event.stopPropagation();
    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode,
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx() },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onSelectionBodyPointerDown(event: PointerEvent): void {
    if (!this.hasPreview()) {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    event.stopPropagation();
    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode: 'move',
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx() },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onPreviewPointerMove(event: PointerEvent): void {
    if (!this.dragState) {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    const pointer = this.pointerToLocal(event, imageRect);
    const nextRect = this.nextRectForDrag(
      pointer.x,
      pointer.y,
      imageRect.width,
      imageRect.height,
    );
    this.selectionRectPx.set(nextRect);
    this.syncBoundsFromSelectionRect();
  }

  onPreviewPointerUp(): void {
    if (!this.dragState) {
      return;
    }

    this.dragState = undefined;
    this.job.schedulePlanRefresh(BOUNDS_PLAN_DEBOUNCE_MS);
    this.scheduleCropPreviewRefresh();
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  /**
   * Asks the backend for a crop rectangle that removes a uniform border, fills
   * the four bounds fields with it and refreshes the preview. Nothing is
   * cropped: the user still accepts or adjusts the suggestion.
   */
  async detectBorders(): Promise<void> {
    const path = this.previewSourcePath();
    if (!path || this.suggestionLoading()) {
      return;
    }

    this.suggestionLoading.set(true);
    this.suggestionNotice.set('');
    this.actionMessage.set('');

    const response = await this.wailsService
      .getImageCropSuggestion(path)
      .catch(() => null);

    this.suggestionLoading.set(false);

    if (!response || !response.success) {
      this.suggestionNotice.set(this.i18n.t('imageCrop.suggestion.error'));
      return;
    }

    if (!response.found) {
      this.suggestionNotice.set(this.i18n.t('imageCrop.suggestion.none'));
      return;
    }

    // A detected border is free-form. If the active ratio preset cannot
    // represent it, drop to free so the suggestion is usable as-is.
    const preset = this.ratioDefinition();
    const ratioMatches =
      this.ratioPreset() === 'free' ||
      response.width * preset.heightStep ===
        response.height * preset.widthStep;
    if (!ratioMatches) {
      this.ratioPreset.set('free');
    }

    this.bounds.set({
      x: `${response.x}`,
      y: `${response.y}`,
      width: `${response.width}`,
      height: `${response.height}`,
    });
    this.syncSelectionRectFromCrop();
    await this.refreshCropPreview();
    await this.job.refreshPlan();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
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

  /** Retries only the failed inputs with the same crop settings and folder. */
  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    const previousFirst = this.selectedPaths()[0] ?? '';
    this.job.replacePaths([...new Set(failed)], { notify: false });
    this.job.clearRunOutcome();

    if ((this.selectedPaths()[0] ?? '') !== previousFirst) {
      await this.refreshPreviewSource();
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.clearCropPreviewTimer();
    this.cropPreviewToken += 1;
    this.previewGeneration += 1;
    this.dragState = undefined;

    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.bounds.set({ ...EMPTY_BOUNDS });
    this.selectionRectPx.set({ ...EMPTY_RECT });
    this.outputWidth.set(null);
    this.outputHeight.set(null);
    this.rotate.set(0);
    this.flipH.set(false);
    this.flipV.set(false);
    this.previewLoading.set(false);
    this.previewError.set('');
    this.cropPreviewUrl.set('');
    this.isCropPreviewLoading.set(false);
    this.suggestionLoading.set(false);
    this.suggestionNotice.set('');
  }

  private currentOptions(): ImageCropOptions {
    return {
      ratioPreset: this.ratioPreset(),
      format: this.format(),
      outputWidth: this.outputWidth(),
      outputHeight: this.outputHeight(),
      rotate: this.rotate(),
      flipH: this.flipH(),
      flipV: this.flipV(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.ratioPreset.set(options.ratioPreset);
    this.format.set(options.format);
    this.outputWidth.set(options.outputWidth);
    this.outputHeight.set(options.outputHeight);
    this.rotate.set(options.rotate);
    this.flipH.set(options.flipH);
    this.flipV.set(options.flipV);
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const crop = this.crop() ?? { x: 0, y: 0, width: 1, height: 1 };
    const format = this.format().trim();

    const options: Record<string, unknown> = {
      x: crop.x,
      y: crop.y,
      width: crop.width,
      height: crop.height,
      ratioPreset: this.ratioPreset(),
    };
    if (format) {
      options['format'] = format;
    }

    const outputWidth = this.outputWidth();
    if (outputWidth && outputWidth > 0) {
      options['outputWidth'] = outputWidth;
    }
    const outputHeight = this.outputHeight();
    if (outputHeight && outputHeight > 0) {
      options['outputHeight'] = outputHeight;
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

    return {
      toolId: IMAGE_CROP_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  private async reloadPreviewAndPlan(): Promise<void> {
    await this.refreshPreviewSource();
    await this.job.refreshPlan();
  }

  private async refreshPreviewSource(): Promise<void> {
    const path = this.previewSourcePath();
    const generation = (this.previewGeneration += 1);

    this.clearCropPreviewTimer();
    this.cropPreviewToken += 1;
    this.cropPreviewUrl.set('');
    this.isCropPreviewLoading.set(false);
    this.previewError.set('');
    this.actionMessage.set('');
    this.suggestionNotice.set('');

    if (!path) {
      this.previewLoading.set(false);
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      this.selectionRectPx.set({ ...EMPTY_RECT });
      return;
    }

    this.previewLoading.set(true);

    const response: ImagePreviewSourceResponse =
      await this.wailsService.getImagePreviewSource(path).catch(() => ({
        success: false,
        message: this.i18n.t('imageCrop.error.preview'),
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_PREVIEW_READ_FAILED',
          message: this.i18n.t('imageCrop.error.preview'),
        },
      }));
    if (generation !== this.previewGeneration) {
      return;
    }

    this.previewLoading.set(false);

    if (!response.success || !response.dataBase64) {
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      this.selectionRectPx.set({ ...EMPTY_RECT });
      this.previewError.set(
        localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            response.error,
            this.i18n.t('imageCrop.error.preview'),
          ),
        ).description,
      );
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.imageWidth.set(response.width ?? 0);
    this.imageHeight.set(response.height ?? 0);
    this.previewUrl.set(
      `data:${mimeType};base64,${response.dataBase64}`,
    );

    this.prepareCropForImage();
  }

  private prepareCropForImage(): void {
    if (!this.hasPreview()) {
      this.selectionRectPx.set({ ...EMPTY_RECT });
      return;
    }

    if (!this.cropValid()) {
      this.applyRatioCrop();
    }

    this.syncSelectionRectFromCrop();
    this.scheduleCropPreviewRefresh();
  }

  private applyRatioCrop(): void {
    const imageWidth = this.imageWidth();
    const imageHeight = this.imageHeight();
    if (imageWidth <= 0 || imageHeight <= 0) {
      return;
    }

    const preset = this.ratioDefinition();
    if (this.ratioPreset() === 'free') {
      this.setCrop({
        x: 0,
        y: 0,
        width: imageWidth,
        height: imageHeight,
      });
      return;
    }

    const units = Math.min(
      Math.floor(imageWidth / preset.widthStep),
      Math.floor(imageHeight / preset.heightStep),
    );
    if (units < 1) {
      this.setCrop({
        x: 0,
        y: 0,
        width: imageWidth,
        height: imageHeight,
      });
      return;
    }

    const width = units * preset.widthStep;
    const height = units * preset.heightStep;
    this.setCrop({
      x: Math.floor((imageWidth - width) / 2),
      y: Math.floor((imageHeight - height) / 2),
      width,
      height,
    });
  }

  private setCrop(crop: CropRect): void {
    this.bounds.set({
      x: `${crop.x}`,
      y: `${crop.y}`,
      width: `${crop.width}`,
      height: `${crop.height}`,
    });
  }

  private scheduleCropPreviewRefresh(): void {
    this.clearCropPreviewTimer();
    // A pending preview belongs to the previous crop; discard it so it can
    // never replace the preview of the new selection.
    this.cropPreviewToken += 1;
    this.isCropPreviewLoading.set(false);
    if (!this.previewUrl()) {
      return;
    }

    this.cropPreviewTimer = setTimeout(() => {
      this.cropPreviewTimer = null;
      void this.refreshCropPreview();
    }, CROP_PREVIEW_DEBOUNCE_MS);
  }

  private clearCropPreviewTimer(): void {
    if (this.cropPreviewTimer) {
      clearTimeout(this.cropPreviewTimer);
      this.cropPreviewTimer = null;
    }
  }

  private async refreshCropPreview(): Promise<void> {
    const path = this.previewSourcePath();
    const crop = this.crop();

    if (!path || !crop || !this.cropValid()) {
      this.cropPreviewUrl.set('');
      return;
    }

    const token = (this.cropPreviewToken += 1);
    this.isCropPreviewLoading.set(true);

    const request: ImageCropPreviewRequest = {
      inputPath: path,
      x: crop.x,
      y: crop.y,
      width: crop.width,
      height: crop.height,
      ratioPreset: this.ratioPreset(),
      format: this.format(),
    };

    const response = await this.wailsService
      .getImageCropPreview(request)
      .catch(() => null);
    if (token !== this.cropPreviewToken) {
      return;
    }

    this.isCropPreviewLoading.set(false);

    if (!response || !response.success || !response.dataBase64) {
      this.cropPreviewUrl.set('');
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.cropPreviewUrl.set(
      `data:${mimeType};base64,${response.dataBase64}`,
    );
  }

  private previewSourcePath(): string {
    return this.selectedPaths()[0]?.trim() ?? '';
  }

  private ratioDefinition(): RatioPreset {
    return (
      RATIO_PRESETS.find((preset) => preset.id === this.ratioPreset()) ??
      RATIO_PRESETS[0]
    );
  }

  private syncSelectionRectFromCrop(): void {
    const element = this.previewImage?.nativeElement;
    const crop = this.crop();
    const imageWidth = this.imageWidth();
    const imageHeight = this.imageHeight();

    if (!element || !crop || imageWidth <= 0 || imageHeight <= 0) {
      this.selectionRectPx.set({ ...EMPTY_RECT });
      return;
    }

    const rect = element.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) {
      return;
    }

    const scaleX = rect.width / imageWidth;
    const scaleY = rect.height / imageHeight;
    this.selectionRectPx.set({
      x: Math.max(0, Math.round(crop.x * scaleX)),
      y: Math.max(0, Math.round(crop.y * scaleY)),
      width: Math.max(1, Math.round(crop.width * scaleX)),
      height: Math.max(1, Math.round(crop.height * scaleY)),
    });
  }

  private syncBoundsFromSelectionRect(): void {
    const rect = this.previewImageElementRect();
    const imageWidth = this.imageWidth();
    const imageHeight = this.imageHeight();
    const selection = this.selectionRectPx();

    if (!rect || imageWidth <= 0 || imageHeight <= 0) {
      return;
    }

    const scaleX = imageWidth / rect.width;
    const scaleY = imageHeight / rect.height;

    const x = clamp(Math.round(selection.x * scaleX), 0, imageWidth - 1);
    const y = clamp(Math.round(selection.y * scaleY), 0, imageHeight - 1);
    let width = clamp(
      Math.round(selection.width * scaleX),
      1,
      imageWidth - x,
    );
    let height = clamp(
      Math.round(selection.height * scaleY),
      1,
      imageHeight - y,
    );

    const preset = this.ratioDefinition();
    if (this.ratioPreset() !== 'free') {
      const unitsX = Math.floor(width / preset.widthStep);
      const unitsY = Math.floor(height / preset.heightStep);
      const maxUnitsX = Math.floor(
        (imageWidth - x) / preset.widthStep,
      );
      const maxUnitsY = Math.floor(
        (imageHeight - y) / preset.heightStep,
      );
      const units = Math.min(unitsX, unitsY, maxUnitsX, maxUnitsY);
      if (units < 1) {
        return;
      }

      width = units * preset.widthStep;
      height = units * preset.heightStep;
    }

    this.setCrop({ x, y, width, height });
    this.syncSelectionRectFromCrop();
  }

  private ratioToValue(preset: RatioPresetId): number {
    switch (preset) {
      case '1:1':
        return 1;
      case '4:3':
        return 4 / 3;
      case '16:9':
        return 16 / 9;
      default:
        return 0;
    }
  }

  private previewImageElementRect(): DOMRect | null {
    const element = this.previewImage?.nativeElement;
    if (!element) {
      return null;
    }

    const rect = element.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) {
      return null;
    }
    return rect;
  }

  private pointerToLocal(
    event: PointerEvent,
    rect: DOMRect,
  ): { x: number; y: number } {
    const x = clamp(event.clientX - rect.left, 0, rect.width);
    const y = clamp(event.clientY - rect.top, 0, rect.height);
    return { x, y };
  }

  private nextRectForDrag(
    pointerX: number,
    pointerY: number,
    maxWidth: number,
    maxHeight: number,
  ): CropRect {
    if (!this.dragState) {
      return this.selectionRectPx();
    }

    const dx = pointerX - this.dragState.startX;
    const dy = pointerY - this.dragState.startY;
    const start = this.dragState.startRect;

    let next: CropRect = { ...start };

    switch (this.dragState.mode) {
      case 'draw': {
        const x1 = clamp(this.dragState.startX, 0, maxWidth);
        const y1 = clamp(this.dragState.startY, 0, maxHeight);
        const x2 = clamp(pointerX, 0, maxWidth);
        const y2 = clamp(pointerY, 0, maxHeight);
        next = {
          x: Math.min(x1, x2),
          y: Math.min(y1, y2),
          width: Math.max(1, Math.abs(x2 - x1)),
          height: Math.max(1, Math.abs(y2 - y1)),
        };
        break;
      }
      case 'move': {
        next = {
          x: clamp(start.x + dx, 0, Math.max(0, maxWidth - start.width)),
          y: clamp(start.y + dy, 0, Math.max(0, maxHeight - start.height)),
          width: start.width,
          height: start.height,
        };
        break;
      }
      case 'resize-nw': {
        const x = clamp(start.x + dx, 0, start.x + start.width - 1);
        const y = clamp(start.y + dy, 0, start.y + start.height - 1);
        next = {
          x,
          y,
          width: Math.max(1, start.width + (start.x - x)),
          height: Math.max(1, start.height + (start.y - y)),
        };
        break;
      }
      case 'resize-ne': {
        const right = clamp(
          start.x + start.width + dx,
          start.x + 1,
          maxWidth,
        );
        const y = clamp(start.y + dy, 0, start.y + start.height - 1);
        next = {
          x: start.x,
          y,
          width: Math.max(1, right - start.x),
          height: Math.max(1, start.height + (start.y - y)),
        };
        break;
      }
      case 'resize-sw': {
        const x = clamp(start.x + dx, 0, start.x + start.width - 1);
        const bottom = clamp(
          start.y + start.height + dy,
          start.y + 1,
          maxHeight,
        );
        next = {
          x,
          y: start.y,
          width: Math.max(1, start.width + (start.x - x)),
          height: Math.max(1, bottom - start.y),
        };
        break;
      }
      case 'resize-se': {
        const right = clamp(
          start.x + start.width + dx,
          start.x + 1,
          maxWidth,
        );
        const bottom = clamp(
          start.y + start.height + dy,
          start.y + 1,
          maxHeight,
        );
        next = {
          x: start.x,
          y: start.y,
          width: Math.max(1, right - start.x),
          height: Math.max(1, bottom - start.y),
        };
        break;
      }
      default:
        break;
    }

    const ratioPreset = this.ratioPreset();
    if (ratioPreset !== 'free') {
      const ratio = this.ratioToValue(ratioPreset);
      if (ratio > 0) {
        const adjustedHeight = Math.max(1, Math.round(next.width / ratio));
        next.height = Math.min(
          adjustedHeight,
          Math.max(1, maxHeight - next.y),
        );
      }
    }

    return {
      x: Math.round(next.x),
      y: Math.round(next.y),
      width: Math.max(1, Math.round(next.width)),
      height: Math.max(1, Math.round(next.height)),
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

  private readEventValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readNumber(event: Event): number | null {
    const raw = this.readEventValue(event);
    if (raw.trim() === '') {
      return null;
    }
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) ? parsed : null;
  }
}
