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
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import {
  describeToolError,
  localizeToolError,
} from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
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

const IMAGE_TOOL_ID = 'tool.image.watermark';
const SETTINGS_PLAN_DEBOUNCE_MS = 200;
const PREVIEW_DEBOUNCE_MS = 150;
const LOGO_THUMBNAIL_SIZE = 256;
const TEXT_LINE_HEIGHT = 1.25;
const FONT_ASCENT = 0.88;
const MIN_PREVIEW_FONT_SIZE = 12;
const MAX_PREVIEW_SHRINK_STEPS = 24;
const MAX_TEXT_RUNES = 200;
const MAX_TEXT_LINES = 10;
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

const SUPPORTED_LOGO_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'webp']);

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

type ImageWatermarkOptions = {
  kind: WatermarkKind;
  text: string;
  logoPath: string;
  anchor: string;
  marginPercent: number;
  textScalePercent: number;
  logoScalePercent: number;
  opacity: number;
  color: string;
  bold: boolean;
  tile: boolean;
  rotationDegrees: number;
  format: WatermarkFormat;
  quality: number;
};

function defaultOptions(): ImageWatermarkOptions {
  return {
    kind: 'text',
    text: '',
    logoPath: '',
    anchor: DEFAULT_ANCHOR,
    marginPercent: DEFAULT_MARGIN,
    textScalePercent: DEFAULT_TEXT_SCALE,
    logoScalePercent: DEFAULT_LOGO_SCALE,
    opacity: DEFAULT_OPACITY,
    color: DEFAULT_COLOR,
    bold: true,
    tile: false,
    rotationDegrees: DEFAULT_ROTATION,
    format: 'auto',
    quality: DEFAULT_QUALITY,
  };
}

function finiteInRange(
  value: unknown,
  min: number,
  max: number,
): number | undefined {
  return typeof value === 'number' &&
    Number.isFinite(value) &&
    value >= min &&
    value <= max
    ? value
    : undefined;
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageWatermarkOptions> {
  const result: Partial<ImageWatermarkOptions> = {};

  const kind = stored['kind'];
  if (kind === 'text' || kind === 'logo') {
    result.kind = kind;
  }

  if (typeof stored['text'] === 'string') {
    result.text = stored['text'];
  }
  if (typeof stored['logoPath'] === 'string') {
    result.logoPath = stored['logoPath'];
  }

  const anchor = stored['anchor'];
  if (
    typeof anchor === 'string' &&
    ANCHORS.some((option) => option.id === anchor)
  ) {
    result.anchor = anchor;
  }

  const margin = finiteInRange(stored['marginPercent'], 2, 5);
  if (margin !== undefined) {
    result.marginPercent = margin;
  }
  const textScale = finiteInRange(stored['textScalePercent'], 3, 8);
  if (textScale !== undefined) {
    result.textScalePercent = textScale;
  }
  const logoScale = finiteInRange(stored['logoScalePercent'], 5, 60);
  if (logoScale !== undefined) {
    result.logoScalePercent = logoScale;
  }
  const opacity = finiteInRange(stored['opacity'], 0.05, 1);
  if (opacity !== undefined) {
    result.opacity = opacity;
  }

  if (typeof stored['color'] === 'string') {
    result.color = stored['color'];
  }
  if (typeof stored['bold'] === 'boolean') {
    result.bold = stored['bold'];
  }
  if (typeof stored['tile'] === 'boolean') {
    result.tile = stored['tile'];
  }

  const rotation = finiteInRange(stored['rotationDegrees'], -90, 90);
  if (rotation !== undefined) {
    result.rotationDegrees = rotation;
  }

  const format = stored['format'];
  if (
    typeof format === 'string' &&
    FORMAT_OPTIONS.some((option) => option.value === format)
  ) {
    result.format = format as WatermarkFormat;
  }

  const quality = stored['quality'];
  if (
    typeof quality === 'number' &&
    Number.isFinite(quality) &&
    quality >= MIN_QUALITY &&
    quality <= MAX_QUALITY
  ) {
    result.quality = Math.round(quality);
  }

  return result;
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
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && this.contentValid(),
    canRun: () => this.canRun(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (previous) => {
      if ((previous[0] ?? '') !== (this.selectedPaths()[0] ?? '')) {
        this.schedulePreviewRefresh();
      }
      void this.job.refreshPlan();
    },
    notice: {
      rejectedOne: 'imageWatermark.notice.rejectedOne',
      rejectedMany: 'imageWatermark.notice.rejectedMany',
      duplicateOne: 'imageWatermark.notice.duplicateOne',
      duplicateMany: 'imageWatermark.notice.duplicateMany',
      rejectedManyFormat: 'count',
    },
    messages: {
      start: 'imageWatermark.error.start',
      planFailed: 'imageWatermark.error.plan',
      waiting: 'imageWatermark.status.waiting',
      inspectFailed: 'imageWatermark.error.inspect',
      statusMissing: 'imageWatermark.error.status',
      cancelFailed: 'imageWatermark.error.cancel',
      openFileFailed: 'imageWatermark.error.openFile',
      revealFailed: 'imageWatermark.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly uploadIcon = Upload;
  readonly imageIcon = ImageIcon;

  readonly anchors = ANCHORS;
  readonly formatOptions = FORMAT_OPTIONS;
  readonly tilePatternId = `ff-wm-tile-${(ImageWatermark.nextInstance += 1)}`;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;

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
  readonly outputDir = this.job.outputDir;
  readonly advancedOpen = signal(false);

  readonly previewUrl = signal('');
  readonly imageWidth = signal(0);
  readonly imageHeight = signal(0);
  readonly previewLoading = signal(false);
  readonly previewError = signal('');

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
    this.kind() === 'logo'
      ? this.i18n.t('imageWatermark.kind.logo')
      : this.i18n.t('imageWatermark.kind.text'),
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
    const position = this.anchorLabel(this.activeAnchor()).toLowerCase();
    const opacity = Math.round(this.opacity() * 100);
    if (this.kind() === 'logo') {
      return this.i18n.t('imageWatermark.preview.description.logo', {
        position,
        scale: this.scalePercent(),
        opacity,
      });
    }
    if (this.tile()) {
      return this.i18n.t('imageWatermark.preview.description.tiled', {
        scale: this.scalePercent(),
        opacity,
      });
    }
    return this.i18n.t('imageWatermark.preview.description.text', {
      position,
      scale: this.scalePercent(),
      opacity,
    });
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageWatermark.action.watermarking');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageWatermark.action.watermark');
    }
    return count === 1
      ? this.i18n.t('imageWatermark.action.watermarkOne')
      : this.i18n.t('imageWatermark.action.watermarkMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageWatermark.destination.original'),
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
      return this.i18n.t('imageWatermark.helper.addImage');
    }
    if (this.kind() === 'logo') {
      if (!this.logoPath().trim()) {
        return this.i18n.t('imageWatermark.helper.chooseLogo');
      }
      if (!this.contentValid()) {
        return this.i18n.t('imageWatermark.helper.logoFormat');
      }
      return '';
    }
    if (!this.text().trim()) {
      return this.i18n.t('imageWatermark.helper.textRequired');
    }
    if (this.textRuneCount() > MAX_TEXT_RUNES) {
      return this.i18n.t('imageWatermark.helper.textTooLong', {
        max: MAX_TEXT_RUNES,
      });
    }
    if (this.textLines().length > MAX_TEXT_LINES) {
      return this.i18n.t('imageWatermark.helper.tooManyLines', {
        max: MAX_TEXT_LINES,
      });
    }
    return '';
  });

  readonly textLimitLabel = computed(() =>
    this.i18n.t('imageWatermark.textLimit', {
      count: this.textRuneCount(),
      max: MAX_TEXT_RUNES,
    }),
  );

  private previewRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private previewGeneration = 0;
  private logoToken = 0;

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/image-watermark');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
    this.clearPreviewRefreshTimer();
  }

  fileName(path: string): string {
    return fileName(path);
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  /** Anchor copy resolved at render time so the locale switch applies. */
  anchorLabel(anchor: AnchorOption): string {
    return this.i18n.tOr(
      `imageWatermark.anchor.${anchor.id}`,
      anchor.label,
    );
  }

  formatOptionLabel(format: WatermarkFormat): string {
    return this.i18n.tOr(
      `imageWatermark.format.${format}.label`,
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
    void this.job.refreshPlan();
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
    void this.job.refreshPlan();
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
    this.schedulePlan();
  }

  onColorInput(event: Event): void {
    this.color.set(this.readValue(event) || DEFAULT_COLOR);
    this.schedulePlan();
  }

  onBoldToggle(event: Event): void {
    this.bold.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onMarginInput(event: Event): void {
    this.marginPercent.set(this.readClampedFloat(event, 2, 5, DEFAULT_MARGIN));
    this.schedulePlan();
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
    this.schedulePlan();
  }

  onOpacityInput(event: Event): void {
    this.opacity.set(this.readClampedFloat(event, 0.05, 1, DEFAULT_OPACITY));
    this.schedulePlan();
  }

  onTileToggle(event: Event): void {
    this.tile.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onRotationInput(event: Event): void {
    this.rotationDegrees.set(
      this.readClampedFloat(event, -90, 90, DEFAULT_ROTATION),
    );
    this.schedulePlan();
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
    void this.job.refreshPlan();
  }

  onQualityInput(event: Event): void {
    const value = this.readNumber(event);
    this.quality.set(
      value === null
        ? DEFAULT_QUALITY
        : Math.min(MAX_QUALITY, Math.max(MIN_QUALITY, Math.round(value))),
    );
    this.schedulePlan();
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
      this.logoError.set(this.i18n.t('imageWatermark.error.logoFormat'));
      this.actionMessage.set('');
      return;
    }

    this.kind.set('logo');
    this.tile.set(false);
    this.logoPath.set(path);
    this.logoError.set('');
    this.actionMessage.set('');
    void this.refreshLogoPreview();
    void this.job.refreshPlan();
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
    void this.job.refreshPlan();
  }

  onPreviewImageError(): void {
    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.previewLoading.set(false);
    this.previewError.set(
      this.i18n.t('imageWatermark.error.previewImage'),
    );
  }

  onLogoPreviewError(): void {
    this.logoPreviewUrl.set('');
    this.logoError.set(
      this.i18n.t('imageWatermark.error.logoPreview'),
    );
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
      this.schedulePreviewRefresh();
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.clearPreviewRefreshTimer();
    this.previewGeneration += 1;

    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.previewLoading.set(false);
    this.previewError.set('');
  }

  private currentOptions(): ImageWatermarkOptions {
    return {
      kind: this.kind(),
      text: this.text(),
      logoPath: this.logoPath(),
      anchor: this.anchor(),
      marginPercent: this.marginPercent(),
      textScalePercent: this.textScalePercent(),
      logoScalePercent: this.logoScalePercent(),
      opacity: this.opacity(),
      color: this.color(),
      bold: this.bold(),
      tile: this.tile(),
      rotationDegrees: this.rotationDegrees(),
      format: this.format(),
      quality: this.quality(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.kind.set(options.kind);
    this.text.set(options.text);
    this.logoPath.set(options.logoPath);
    this.anchor.set(options.anchor);
    this.marginPercent.set(options.marginPercent);
    this.textScalePercent.set(options.textScalePercent);
    this.logoScalePercent.set(options.logoScalePercent);
    this.opacity.set(options.opacity);
    this.color.set(options.color);
    this.bold.set(options.bold);
    this.tile.set(options.tile);
    this.rotationDegrees.set(options.rotationDegrees);
    this.format.set(options.format);
    this.quality.set(options.quality);

    // The remembered logo lives on disk; reload its preview so the overlay is
    // complete before the user picks an image.
    if (this.kind() === 'logo' && this.logoPath().trim()) {
      void this.refreshLogoPreview();
    }
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

  /**
   * Mirrors the old scheduler: with no inputs or no valid watermark content the
   * stale plan is dropped at once instead of surviving one debounce tick.
   */
  private schedulePlan(): void {
    if (this.selectedPaths().length === 0 || !this.contentValid()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(SETTINGS_PLAN_DEBOUNCE_MS);
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
        message: this.i18n.t('imageWatermark.error.preview'),
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_PREVIEW_READ_FAILED',
          message: this.i18n.t('imageWatermark.error.preview'),
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
        localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            response.error,
            this.i18n.t('imageWatermark.error.preview'),
          ),
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
      this.logoError.set(
        this.i18n.t('imageWatermark.error.logoPreviewLoad'),
      );
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
}
