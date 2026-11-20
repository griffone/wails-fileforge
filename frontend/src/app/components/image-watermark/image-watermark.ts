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
  Image as ImageIcon,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  Upload,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  ImagePreviewSourceResponse,
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

const IMAGE_TOOL_ID = 'tool.image.watermark';
const SETTINGS_PLAN_DEBOUNCE_MS = 200;
const PREVIEW_DEBOUNCE_MS = 150;
const THUMBNAIL_SIZE = 160;
const LOGO_THUMBNAIL_SIZE = 256;
const TEXT_LINE_HEIGHT = 1.25;
const FONT_ASCENT = 0.88;
const MIN_PREVIEW_FONT_SIZE = 12;
const MAX_PREVIEW_SHRINK_STEPS = 24;
const MAX_TEXT_RUNES = 200;
const MAX_TEXT_LINES = 10;

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

const SUPPORTED_LOGO_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'webp']);

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type WatermarkKind = 'text' | 'logo';
type WatermarkFormat = 'auto' | 'jpeg' | 'png' | 'webp';

interface SelectOption<T extends string> {
  value: T;
  label: string;
}

const FORMAT_OPTIONS: readonly SelectOption<WatermarkFormat>[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'png', label: 'PNG' },
  { value: 'webp', label: 'WebP' },
];

interface AnchorOption {
  id: string;
  label: string;
  /** 0 = start, 0.5 = center, 1 = end. Mirrors the backend anchor factors. */
  ax: number;
  ay: number;
}

const ANCHORS: readonly AnchorOption[] = [
  { id: 'top-left', label: 'Top left', ax: 0, ay: 0 },
  { id: 'top-center', label: 'Top center', ax: 0.5, ay: 0 },
  { id: 'top-right', label: 'Top right', ax: 1, ay: 0 },
  { id: 'center-left', label: 'Center left', ax: 0, ay: 0.5 },
  { id: 'center', label: 'Center', ax: 0.5, ay: 0.5 },
  { id: 'center-right', label: 'Center right', ax: 1, ay: 0.5 },
  { id: 'bottom-left', label: 'Bottom left', ax: 0, ay: 1 },
  { id: 'bottom-center', label: 'Bottom center', ax: 0.5, ay: 1 },
  { id: 'bottom-right', label: 'Bottom right', ax: 1, ay: 1 },
];

const DEFAULT_ANCHOR = 'bottom-right';
const DEFAULT_TEXT_SCALE = 5;
const DEFAULT_LOGO_SCALE = 20;
const DEFAULT_OPACITY = 0.6;
const DEFAULT_COLOR = '#FFFFFF';
const DEFAULT_MARGIN = 3;
const DEFAULT_ROTATION = -30;
const DEFAULT_QUALITY = 92;
const MIN_QUALITY = 40;
const MAX_QUALITY = 100;

interface TextLinePreview {
  text: string;
  y: number;
}

interface TextPreview {
  x: number;
  fontSize: number;
  fontWeight: number;
  anchor: 'start' | 'middle' | 'end';
  lines: TextLinePreview[];
}

interface TilePreview {
  width: number;
  height: number;
  fontSize: number;
  padding: number;
  /**
   * SVG pattern transform with the negated rotation angle and the stamp centre
   * as its origin. See the comment in `tilePreview` for the sign convention.
   */
  transform: string;
  lines: TextLinePreview[];
}

interface LogoPreview {
  href: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

@Component({
  selector: 'app-image-watermark',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-watermark.html',
  styleUrl: './image-watermark.css',
})
export class ImageWatermark implements OnDestroy, OnInit {
  private static nextInstance = 0;

  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly uploadIcon = Upload;
  readonly imageIcon = ImageIcon;

  readonly anchors = ANCHORS;
  readonly formatOptions = FORMAT_OPTIONS;
  readonly tilePatternId = `ff-wm-tile-${(ImageWatermark.nextInstance += 1)}`;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);

  readonly kind = signal<WatermarkKind>('text');
  readonly text = signal('');
  readonly logoPath = signal('');
  /** Small thumbnail for the logo picker. */
  readonly logoThumbnailUrl = signal('');
  /** Full logo preview source, used for the overlay and its intrinsic size. */
  readonly logoPreviewUrl = signal('');
  /** EXIF-normalized intrinsic width of the logo, in pixels. */
  readonly logoNaturalWidth = signal(0);
  readonly logoAspect = signal(1);
  readonly logoLoading = signal(false);
  readonly logoError = signal('');
  readonly anchor = signal(DEFAULT_ANCHOR);
  readonly marginPercent = signal(DEFAULT_MARGIN);
  readonly textScalePercent = signal(DEFAULT_TEXT_SCALE);
  readonly logoScalePercent = signal(DEFAULT_LOGO_SCALE);
  readonly opacity = signal(DEFAULT_OPACITY);
  readonly color = signal(DEFAULT_COLOR);
  readonly bold = signal(true);
  readonly tile = signal(false);
  readonly rotationDegrees = signal(DEFAULT_ROTATION);
  readonly format = signal<WatermarkFormat>('auto');
  readonly quality = signal(DEFAULT_QUALITY);
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly advancedOpen = signal(false);

  readonly previewUrl = signal('');
  readonly imageWidth = signal(0);
  readonly imageHeight = signal(0);
  readonly previewLoading = signal(false);
  readonly previewError = signal('');

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly hasSelection = computed(() => this.selectedPaths().length > 0);
  readonly isBatch = computed(() => this.selectedPaths().length > 1);
  readonly hasPreview = computed(
    () =>
      this.previewUrl().length > 0 &&
      this.imageWidth() > 0 &&
      this.imageHeight() > 0,
  );

  readonly activeAnchor = computed<AnchorOption>(
    () => ANCHORS.find((option) => option.id === this.anchor()) ?? ANCHORS[8],
  );

  readonly kindLabel = computed(() =>
    this.kind() === 'logo' ? 'Logo' : 'Text',
  );

  readonly scalePercent = computed(() =>
    this.kind() === 'logo' ? this.logoScalePercent() : this.textScalePercent(),
  );

  readonly scaleRange = computed(() =>
    this.kind() === 'logo'
      ? { min: 5, max: 60, step: 1 }
      : { min: 3, max: 8, step: 0.5 },
  );

  readonly normalizedText = computed(() => this.text().replace(/\r\n?/g, '\n'));

  readonly textLines = computed(() => this.normalizedText().split('\n'));

  readonly textRuneCount = computed(
    () => Array.from(this.normalizedText()).length,
  );

  readonly contentValid = computed(() => {
    if (this.kind() === 'logo') {
      const path = this.logoPath().trim();
      return (
        path.length > 0 &&
        SUPPORTED_LOGO_EXTENSIONS.has(fileExtension(path).toLowerCase())
      );
    }

    const trimmed = this.text().trim();
    return (
      trimmed.length > 0 &&
      this.textRuneCount() <= MAX_TEXT_RUNES &&
      this.textLines().length <= MAX_TEXT_LINES
    );
  });

  readonly marginPixels = computed(() => {
    const width = this.imageWidth();
    const height = this.imageHeight();
    if (width <= 0 || height <= 0) {
      return 0;
    }
    const shortEdge = Math.min(width, height);
    return Math.max(1, Math.round((shortEdge * this.marginPercent()) / 100));
  });

  /**
   * Approximate the drawn text in image pixels. The SVG uses the image's own
   * viewBox, so these numbers need no DOM measurement.
   */
  readonly textPreview = computed<TextPreview | null>(() => {
    if (this.kind() !== 'text' || this.tile()) {
      return null;
    }
    const lines = this.textLines();
    if (!this.text().trim() || lines.length === 0) {
      return null;
    }

    const width = this.imageWidth();
    const height = this.imageHeight();
    if (width <= 0 || height <= 0) {
      return null;
    }

    const margin = this.marginPixels();
    const maxWidth = Math.max(1, width - 2 * margin);
    const maxHeight = Math.max(1, height - 2 * margin);
    const shortEdge = Math.min(width, height);
    // The backend clamps the drawn font size to a 12 px legibility floor.
    const requested = Math.max(
      MIN_PREVIEW_FONT_SIZE,
      Math.round((shortEdge * this.scalePercent()) / 100),
    );

    let fontSize = requested;
    let blockWidth = this.estimateBlockWidth(lines, fontSize);
    let blockHeight = this.estimateBlockHeight(lines.length, fontSize);
    for (
      let step = 0;
      step < MAX_PREVIEW_SHRINK_STEPS &&
      (blockWidth > maxWidth || blockHeight > maxHeight) &&
      fontSize > MIN_PREVIEW_FONT_SIZE;
      step += 1
    ) {
      const widthRatio = blockWidth > 0 ? maxWidth / blockWidth : 1;
      const heightRatio = blockHeight > 0 ? maxHeight / blockHeight : 1;
      const next = Math.max(
        MIN_PREVIEW_FONT_SIZE,
        Math.floor(fontSize * Math.min(1, widthRatio, heightRatio)),
      );
      if (next >= fontSize) {
        break;
      }
      fontSize = next;
      blockWidth = this.estimateBlockWidth(lines, fontSize);
      blockHeight = this.estimateBlockHeight(lines.length, fontSize);
    }

    const anchor = this.activeAnchor();
    const anchorX = margin + (width - 2 * margin) * anchor.ax;
    const anchorY = margin + (height - 2 * margin) * anchor.ay;
    const top = anchorY - anchor.ay * blockHeight;
    const lineHeight = fontSize * TEXT_LINE_HEIGHT;
    // The backend left-aligns every line of a multi-line block at one edge
    // computed from the anchor and the measured block width. Mirror that with
    // the estimated width so lines do not fan out around the anchor.
    const multiLine = lines.length > 1;
    const blockLeft = multiLine ? anchorX - anchor.ax * blockWidth : anchorX;

    return {
      x: blockLeft,
      fontSize,
      fontWeight: this.bold() ? 700 : 400,
      anchor: multiLine
        ? 'start'
        : anchor.ax === 0
          ? 'start'
          : anchor.ax === 1
            ? 'end'
            : 'middle',
      lines: lines.map((line, index) => ({
        text: line,
        y: top + fontSize * FONT_ASCENT + index * lineHeight,
      })),
    };
  });

  readonly tilePreview = computed<TilePreview | null>(() => {
    if (this.kind() !== 'text' || !this.tile()) {
      return null;
    }
    const lines = this.textLines();
    if (!this.text().trim() || lines.length === 0) {
      return null;
    }

    const width = this.imageWidth();
    const height = this.imageHeight();
    if (width <= 0 || height <= 0) {
      return null;
    }

    const shortEdge = Math.min(width, height);
    // The backend clamps the drawn font size to a 12 px legibility floor.
    const fontSize = Math.max(
      MIN_PREVIEW_FONT_SIZE,
      Math.round((shortEdge * this.scalePercent()) / 100),
    );
    const lineHeight = fontSize * TEXT_LINE_HEIGHT;
    const padding = Math.max(2, fontSize / 2);
    const blockWidth = this.estimateBlockWidth(lines, fontSize);
    const tileWidth = blockWidth + 2 * padding;
    const tileHeight = lines.length * lineHeight + 2 * padding;
    const rotation = this.rotationDegrees();
    // Backend `imaging.Rotate` treats positive angles as counter-clockwise,
    // while SVG `rotate()` turns clockwise. Negate the angle so the preview
    // matches the output, and rotate around the stamp centre instead of the
    // pattern origin.
    const transform = `rotate(${-rotation} ${Number(
      (tileWidth / 2).toFixed(2),
    )} ${Number((tileHeight / 2).toFixed(2))})`;

    return {
      width: tileWidth,
      height: tileHeight,
      fontSize,
      padding,
      transform,
      lines: lines.map((line, index) => ({
        text: line,
        y: padding + fontSize * FONT_ASCENT + index * lineHeight,
      })),
    };
  });

  readonly logoPreview = computed<LogoPreview | null>(() => {
    if (this.kind() !== 'logo') {
      return null;
    }
    const href = this.logoPreviewUrl() || this.logoThumbnailUrl();
    if (!href || !this.logoPath().trim()) {
      return null;
    }

    const width = this.imageWidth();
    const height = this.imageHeight();
    if (width <= 0 || height <= 0) {
      return null;
    }

    const margin = this.marginPixels();
    const anchor = this.activeAnchor();
    const targetWidth = Math.round((width * this.scalePercent()) / 100);
    // Mirrors the backend: never upscale past the logo's natural width, and
    // never draw wider than the image itself.
    const naturalWidth = this.logoNaturalWidth();
    const cappedWidth =
      naturalWidth > 0 ? Math.min(targetWidth, naturalWidth) : targetWidth;
    const logoWidth = Math.max(1, Math.min(width, cappedWidth));
    const aspect = this.logoAspect() > 0 ? this.logoAspect() : 1;
    const logoHeight = logoWidth / aspect;
    const anchorX = margin + (width - 2 * margin) * anchor.ax;
    const anchorY = margin + (height - 2 * margin) * anchor.ay;

    return {
      href,
      x: anchorX - anchor.ax * logoWidth,
      y: anchorY - anchor.ay * logoHeight,
      width: logoWidth,
      height: logoHeight,
    };
  });

  readonly previewDescription = computed(() => {
    if (!this.hasPreview()) {
      return '';
    }
    const position = this.activeAnchor().label.toLowerCase();
    const opacity = Math.round(this.opacity() * 100);
    if (this.kind() === 'logo') {
      return `Logo watermark at ${position}, ${this.scalePercent()}% of the image width, ${opacity}% opacity.`;
    }
    if (this.tile()) {
      return `Tiled text watermark, ${this.scalePercent()}% font size, ${opacity}% opacity. The tiled preview is approximate.`;
    }
    return `Text watermark at ${position}, ${this.scalePercent()}% font size, ${opacity}% opacity.`;
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Watermarking…';
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return 'Watermark';
    }
    return count === 1 ? 'Watermark 1 image' : `Watermark ${count} images`;
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each original',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.hasSelection() &&
      this.contentValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (!this.hasSelection()) {
      return 'Add at least one image to watermark.';
    }
    if (this.kind() === 'logo') {
      if (!this.logoPath().trim()) {
        return 'Choose a logo image.';
      }
      if (!this.contentValid()) {
        return 'Use a PNG, JPEG or WebP logo.';
      }
      return '';
    }
    if (!this.text().trim()) {
      return 'Enter the watermark text.';
    }
    if (this.textRuneCount() > MAX_TEXT_RUNES) {
      return `Text must be at most ${MAX_TEXT_RUNES} characters.`;
    }
    if (this.textLines().length > MAX_TEXT_LINES) {
      return `Text must be at most ${MAX_TEXT_LINES} lines.`;
    }
    return '';
  });

  readonly textLimitLabel = computed(
    () => `${this.textRuneCount()}/${MAX_TEXT_RUNES} characters`,
  );

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
      ? describeToolError(error, 'The watermark could not start.')
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private previewRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private previewGeneration = 0;
  private logoToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {}

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.clearPreviewRefreshTimer();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/image-watermark');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileName(path: string): string {
    return fileName(path);
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'image');
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const previousFirst = this.selectedPaths()[0] ?? '';
    const rejected: string[] = [];
    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(
      this.selectedPaths().map((path) => this.pathKey(path)),
    );

    for (const path of incoming) {
      if (!SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
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

    if (previousFirst !== this.selectedPaths()[0]) {
      this.schedulePreviewRefresh();
      void this.refreshPlan();
    } else {
      void this.refreshPlan();
    }
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
    this.schedulePreviewRefresh();
    void this.refreshPlan();
  }

  removeFile(path: string): void {
    const previousFirst = this.selectedPaths()[0] ?? '';
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }

    this.selectedPaths.set(next);
    this.syncFileItems();

    if ((next[0] ?? '') !== previousFirst) {
      this.schedulePreviewRefresh();
    }
    void this.refreshPlan();
  }

  setKind(kind: WatermarkKind): void {
    if (kind === this.kind()) {
      return;
    }
    this.kind.set(kind);
    if (kind === 'logo') {
      this.tile.set(false);
      if (this.logoPath().trim()) {
        void this.refreshLogoPreview();
      }
    }
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  setAnchor(anchorId: string): void {
    if (!ANCHORS.some((option) => option.id === anchorId)) {
      return;
    }
    if (anchorId === this.anchor()) {
      return;
    }
    this.anchor.set(anchorId);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  isActiveAnchor(anchorId: string): boolean {
    return this.anchor() === anchorId;
  }

  justifyFor(ax: number): string {
    return ax === 0 ? 'flex-start' : ax === 1 ? 'flex-end' : 'center';
  }

  alignFor(ay: number): string {
    return ay === 0 ? 'flex-start' : ay === 1 ? 'flex-end' : 'center';
  }

  onTextInput(event: Event): void {
    this.text.set(this.readValue(event));
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  onColorInput(event: Event): void {
    this.color.set(this.readValue(event) || DEFAULT_COLOR);
    this.schedulePlanRefresh();
  }

  onBoldToggle(event: Event): void {
    this.bold.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onMarginInput(event: Event): void {
    this.marginPercent.set(this.readClampedFloat(event, 2, 5, DEFAULT_MARGIN));
    this.schedulePlanRefresh();
  }

  onScaleInput(event: Event): void {
    const range = this.scaleRange();
    const value = this.readClampedFloat(
      event,
      range.min,
      range.max,
      this.scalePercent(),
    );
    if (this.kind() === 'logo') {
      this.logoScalePercent.set(value);
    } else {
      this.textScalePercent.set(value);
    }
    this.schedulePlanRefresh();
  }

  onOpacityInput(event: Event): void {
    this.opacity.set(this.readClampedFloat(event, 0.05, 1, DEFAULT_OPACITY));
    this.schedulePlanRefresh();
  }

  onTileToggle(event: Event): void {
    this.tile.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onRotationInput(event: Event): void {
    this.rotationDegrees.set(
      this.readClampedFloat(event, -90, 90, DEFAULT_ROTATION),
    );
    this.schedulePlanRefresh();
  }

  onFormatChange(event: Event): void {
    const value = this.readValue(event) as WatermarkFormat;
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

  onQualityInput(event: Event): void {
    const value = this.readNumber(event);
    this.quality.set(
      value === null
        ? DEFAULT_QUALITY
        : Math.min(MAX_QUALITY, Math.max(MIN_QUALITY, Math.round(value))),
    );
    this.schedulePlanRefresh();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  async chooseLogo(): Promise<void> {
    const path = (await this.wailsService.openFileDialog()).trim();
    if (path) {
      this.onLogoSelected([path]);
    }
  }

  onLogoSelected(paths: string[]): void {
    const path = (paths[0] ?? '').trim();
    if (!path) {
      return;
    }
    if (!SUPPORTED_LOGO_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
      this.logoError.set('Use a PNG, JPEG or WebP logo.');
      this.actionMessage.set('');
      return;
    }

    this.kind.set('logo');
    this.tile.set(false);
    this.logoPath.set(path);
    this.logoError.set('');
    this.actionMessage.set('');
    void this.refreshLogoPreview();
    void this.refreshPlan();
  }

  removeLogo(): void {
    if (!this.logoPath() && !this.logoError()) {
      return;
    }
    this.logoToken += 1;
    this.logoPath.set('');
    this.logoThumbnailUrl.set('');
    this.logoPreviewUrl.set('');
    this.logoNaturalWidth.set(0);
    this.logoAspect.set(1);
    this.logoLoading.set(false);
    this.logoError.set('');
    void this.refreshPlan();
  }

  onPreviewImageError(): void {
    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.previewLoading.set(false);
    this.previewError.set('The preview image could not be displayed.');
  }

  onLogoPreviewError(): void {
    this.logoPreviewUrl.set('');
    this.logoError.set('The logo preview could not be displayed.');
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

    // The plan is the source of truth for destinations and collision-safe
    // names, so it is refreshed with the current watermark first.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponse = await this.wailsService
      .runJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The watermark could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The watermark could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The watermark could not start.',
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    const total = this.selectedPaths().length;
    this.activeJobId.set(response.jobId);
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: 'Waiting to start',
      toolId: IMAGE_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: 'Waiting to start',
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
        response.message || 'The job could not be cancelled.',
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || 'The file could not be opened.',
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || 'The folder could not be opened.',
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

    const previousFirst = this.selectedPaths()[0] ?? '';
    this.selectedPaths.set([...new Set(failed)]);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.syncFileItems();

    if ((this.selectedPaths()[0] ?? '') !== previousFirst) {
      this.schedulePreviewRefresh();
    }

    await this.run();
  }

  reset(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.clearPreviewRefreshTimer();
    this.planToken += 1;
    this.previewGeneration += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.previewLoading.set(false);
    this.previewError.set('');
    this.plan.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {
      kind: this.kind(),
      anchor: this.anchor(),
      marginPercent: this.marginPercent(),
      scalePercent: this.scalePercent(),
      opacity: this.opacity(),
      tile: this.tile(),
      format: this.format(),
      quality: this.quality(),
    };

    if (this.kind() === 'logo') {
      options['logoPath'] = this.logoPath().trim();
    } else {
      options['text'] = this.normalizedText();
      options['color'] = this.color();
      options['bold'] = this.bold();
      if (this.tile()) {
        options['rotationDegrees'] = this.rotationDegrees();
      }
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
    if (this.selectedPaths().length === 0 || !this.contentValid()) {
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
        message: 'The output could not be planned.',
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: 'The output could not be planned.',
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
      message: response.message || 'The output could not be planned.',
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
    if (this.selectedPaths().length === 0 || !this.contentValid()) {
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

  private schedulePreviewRefresh(): void {
    this.clearPreviewRefreshTimer();
    // A pending load belongs to the previous selection; discard it so it can
    // never replace the preview of the current one.
    this.previewGeneration += 1;
    this.previewLoading.set(false);
    this.previewError.set('');
    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);

    const path = this.previewSourcePath();
    if (!path) {
      return;
    }

    this.previewRefreshTimer = setTimeout(() => {
      this.previewRefreshTimer = null;
      void this.refreshPreviewSource();
    }, PREVIEW_DEBOUNCE_MS);
  }

  private clearPreviewRefreshTimer(): void {
    if (this.previewRefreshTimer) {
      clearTimeout(this.previewRefreshTimer);
      this.previewRefreshTimer = null;
    }
  }

  private async refreshPreviewSource(): Promise<void> {
    const path = this.previewSourcePath();
    const generation = (this.previewGeneration += 1);
    this.previewError.set('');

    if (!path) {
      this.previewLoading.set(false);
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      return;
    }

    this.previewLoading.set(true);

    const response: ImagePreviewSourceResponse = await this.wailsService
      .getImagePreviewSource(path)
      .catch(() => ({
        success: false,
        message: 'The image preview could not be loaded.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_PREVIEW_READ_FAILED',
          message: 'The image preview could not be loaded.',
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
      this.previewError.set(
        describeToolError(
          response.error,
          'The image preview could not be loaded.',
        ).description,
      );
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.imageWidth.set(response.width ?? 0);
    this.imageHeight.set(response.height ?? 0);
    this.previewUrl.set(`data:${mimeType};base64,${response.dataBase64}`);
  }

  private async refreshLogoPreview(): Promise<void> {
    const path = this.logoPath().trim();
    const token = (this.logoToken += 1);
    this.logoThumbnailUrl.set('');
    this.logoPreviewUrl.set('');
    this.logoNaturalWidth.set(0);
    this.logoAspect.set(1);
    this.logoError.set('');

    if (!path) {
      this.logoLoading.set(false);
      return;
    }

    this.logoLoading.set(true);
    const [thumbnailUrl, source] = await Promise.all([
      this.wailsService
        .getThumbnail(path, LOGO_THUMBNAIL_SIZE)
        .catch(() => null),
      this.wailsService.getImagePreviewSource(path).catch(() => null),
    ]);
    if (token !== this.logoToken) {
      return;
    }

    this.logoLoading.set(false);
    if (thumbnailUrl) {
      this.logoThumbnailUrl.set(thumbnailUrl);
    }

    if (source?.success && source.dataBase64) {
      const mimeType = source.mimeType || 'image/png';
      this.logoPreviewUrl.set(`data:${mimeType};base64,${source.dataBase64}`);
      const width = source.width ?? 0;
      const height = source.height ?? 0;
      if (width > 0 && height > 0) {
        this.logoNaturalWidth.set(width);
        this.logoAspect.set(width / height);
      }
    }

    if (!this.logoThumbnailUrl() && this.logoPreviewUrl()) {
      this.logoThumbnailUrl.set(this.logoPreviewUrl());
    }
    if (!this.logoThumbnailUrl() && !this.logoPreviewUrl()) {
      this.logoError.set('The logo preview could not be loaded.');
      return;
    }

    // The full source supplies the intrinsic size; the thumbnail is only a
    // fallback for the visible preview and its aspect ratio.
    if (!this.logoPreviewUrl() && this.logoThumbnailUrl()) {
      this.measureLogo(this.logoThumbnailUrl(), token);
    }
  }

  private measureLogo(url: string, token: number): void {
    if (typeof Image === 'undefined') {
      return;
    }
    const image = new Image();
    image.onload = () => {
      if (
        token === this.logoToken &&
        image.naturalWidth > 0 &&
        image.naturalHeight > 0
      ) {
        this.logoAspect.set(image.naturalWidth / image.naturalHeight);
        if (this.logoNaturalWidth() <= 0) {
          this.logoNaturalWidth.set(image.naturalWidth);
        }
      }
    };
    image.src = url;
  }

  private previewSourcePath(): string {
    return this.selectedPaths()[0]?.trim() ?? '';
  }

  private estimateBlockWidth(
    lines: readonly string[],
    fontSize: number,
  ): number {
    let widest = 0;
    for (const line of lines) {
      const width = Array.from(line).length * fontSize * 0.58;
      if (width > widest) {
        widest = width;
      }
    }
    return widest;
  }

  private estimateBlockHeight(lineCount: number, fontSize: number): number {
    if (lineCount <= 0) {
      return 0;
    }
    return (lineCount - 1) * fontSize * TEXT_LINE_HEIGHT + fontSize * 1.16;
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
        message: response.message || 'The job status could not be read.',
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
        message: 'File sizes could not be read.',
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
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
    }
    return parts.join(' · ');
  }

  private intakeNotice(
    rejected: string[],
    duplicates: string[],
  ): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        `${fileName(rejected[0])} wasn't added because it isn't an image.`,
      );
    } else if (rejected.length > 1) {
      parts.push(
        `${rejected.length} files weren't added because they aren't images.`,
      );
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(`${duplicates.length} images are already in the queue.`);
    }

    return parts.length > 0 ? { tone: 'warning', text: parts.join(' ') } : null;
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

  private readNumber(event: Event): number | null {
    const raw = this.readValue(event);
    if (raw.trim() === '') {
      return null;
    }
    const parsed = Number.parseFloat(raw);
    return Number.isFinite(parsed) ? parsed : null;
  }

  private readClampedFloat(
    event: Event,
    min: number,
    max: number,
    fallback: number,
  ): number {
    const value = this.readNumber(event);
    if (value === null) {
      return fallback;
    }
    return Math.min(max, Math.max(min, value));
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
