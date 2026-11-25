import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
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
import { ResultCard } from '../tool-flow/result-card';
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

const PDF_CROP_TOOL_ID = 'tool.pdf.crop';
const PLAN_DEBOUNCE_MS = 250;
const PREVIEW_SIZE = 220;
const MAX_MARGIN_PT = 720;
const OPTIONS_SCHEMA_VERSION = 1;

/** A4 without a known page size. The overlay is approximate by design. */
const A4_PAGE_POINTS = { width: 595, height: 842 } as const;

const CROP_PRESET_SMALL = 'small';
const CROP_PRESET_CUSTOM = 'custom';

type CropPresetId = 'none' | 'small' | 'medium' | 'large' | 'custom';

/** Margins the backend applies per preset, in points. */
const PRESET_MARGIN_POINTS: Record<Exclude<CropPresetId, 'custom'>, number> = {
  none: 0,
  small: 10,
  medium: 20,
  large: 40,
};

interface CropPreset {
  id: CropPresetId;
  label: string;
  hint: string;
}

const CROP_PRESETS: CropPreset[] = [
  {
    id: CROP_PRESET_SMALL,
    label: 'Small',
    hint: '10 pt on each side',
  },
  {
    id: 'medium',
    label: 'Medium',
    hint: '20 pt on each side',
  },
  {
    id: 'large',
    label: 'Large',
    hint: '40 pt on each side',
  },
  {
    id: 'none',
    label: 'None',
    hint: 'Keep page size',
  },
  {
    id: CROP_PRESET_CUSTOM,
    label: 'Custom',
    hint: 'Exact margins',
  },
];

type PageSelectionMode = 'all' | 'ranges';
const PAGE_SELECTION_ALL: PageSelectionMode = 'all';
const PAGE_SELECTION_RANGES: PageSelectionMode = 'ranges';

type MarginSide = 'top' | 'right' | 'bottom' | 'left';

const MARGIN_SIDES: readonly MarginSide[] = ['top', 'right', 'bottom', 'left'];

interface MarginField {
  key: MarginSide;
  label: string;
}

const MARGIN_FIELDS: MarginField[] = [
  { key: 'top', label: 'Top' },
  { key: 'right', label: 'Right' },
  { key: 'bottom', label: 'Bottom' },
  { key: 'left', label: 'Left' },
];

type MarginValues = Record<MarginSide, string>;

interface ResolvedMargins {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

interface PreviewInsets {
  top: number;
  right: number;
  bottom: number;
  left: number;
  dimmed: boolean;
  fullPage: boolean;
}

interface PageSize {
  width: number;
  height: number;
}

const EMPTY_MARGINS: MarginValues = {
  top: '',
  right: '',
  bottom: '',
  left: '',
};

type PdfCropOptions = {
  cropPreset: CropPresetId;
  margins: MarginValues;
  pageSelectionMode: PageSelectionMode;
  ranges: string;
};

function defaultOptions(): PdfCropOptions {
  return {
    cropPreset: CROP_PRESET_SMALL,
    margins: { ...EMPTY_MARGINS },
    pageSelectionMode: PAGE_SELECTION_ALL,
    ranges: '',
  };
}

function isValidMarginValue(raw: unknown): raw is string {
  if (typeof raw !== 'string') {
    return false;
  }
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return false;
  }
  const value = Number(trimmed);
  return Number.isFinite(value) && value >= 0 && value <= MAX_MARGIN_PT;
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfCropOptions> {
  const result: Partial<PdfCropOptions> = {};

  const preset = stored['cropPreset'];
  if (
    typeof preset === 'string' &&
    CROP_PRESETS.some((candidate) => candidate.id === preset)
  ) {
    result.cropPreset = preset as CropPresetId;
  }

  const rawMargins = stored['margins'];
  if (typeof rawMargins === 'object' && rawMargins !== null) {
    const record = rawMargins as Record<string, unknown>;
    const margins: MarginValues = { ...EMPTY_MARGINS };
    for (const side of MARGIN_SIDES) {
      const value = record[side];
      if (isValidMarginValue(value)) {
        margins[side] = value;
      }
    }
    result.margins = margins;
  }

  const mode = stored['pageSelectionMode'];
  if (
    typeof mode === 'string' &&
    (mode === PAGE_SELECTION_ALL || mode === PAGE_SELECTION_RANGES)
  ) {
    result.pageSelectionMode = mode;
  }

  if (typeof stored['ranges'] === 'string') {
    result.ranges = stored['ranges'];
  }

  return result;
}

@Component({
  selector: 'app-pdf-crop',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-crop.html',
  styleUrl: './pdf-crop.css',
})
export class PdfCrop implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_CROP_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_CROP_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () =>
      this.selectedPaths().length > 0 &&
      !this.rangesRequired() &&
      this.marginsValid(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () => {
      this.refreshPreview();
      void this.job.refreshPlan();
    },
    onFilesInspected: (infos) => this.applyInspectedPageSize(infos),
    messages: {
      start: 'pdfCrop.error.start',
      planFailed: 'pdfCrop.error.plan',
      waiting: 'pdfCrop.status.waiting',
      inspectFailed: 'pdfCrop.error.inspect',
      statusMissing: 'pdfCrop.error.status',
      cancelFailed: 'pdfCrop.error.cancel',
      openFileFailed: 'pdfCrop.error.openFile',
      revealFailed: 'pdfCrop.error.openFolder',
    },
    notice: {
      rejectedOne: 'pdfCrop.notice.rejectedOne',
      rejectedMany: 'pdfCrop.notice.rejectedMany',
      duplicateOne: 'pdfCrop.notice.duplicateOne',
      duplicateMany: 'pdfCrop.notice.duplicateMany',
      rejectedManyFormat: 'names',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly presets = CROP_PRESETS;
  readonly marginFields = MARGIN_FIELDS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly cropPreset = signal<CropPresetId>(CROP_PRESET_SMALL);
  readonly margins = signal<MarginValues>({ ...EMPTY_MARGINS });
  readonly pageSelectionMode = signal<PageSelectionMode>(PAGE_SELECTION_ALL);
  readonly ranges = signal('');
  readonly outputDir = this.job.outputDir;
  readonly previewThumbnail = signal('');
  readonly previewName = signal('');
  readonly previewPageSize = signal<PageSize>({ ...A4_PAGE_POINTS });

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

  readonly isCustom = computed(
    () => this.cropPreset() === CROP_PRESET_CUSTOM,
  );

  readonly isRangesMode = computed(
    () => this.pageSelectionMode() === PAGE_SELECTION_RANGES,
  );

  readonly rangesRequired = computed(
    () => this.isRangesMode() && this.ranges().trim().length === 0,
  );

  readonly activePresetLabel = computed(
    () =>
      this.presetLabel(
        this.presets.find((preset) => preset.id === this.cropPreset()) ??
          CROP_PRESETS[0],
      ),
  );

  readonly marginsValidInput = computed(() =>
    MARGIN_SIDES.every((side) => this.isValidMargin(this.margins()[side])),
  );

  /** Presets have no local margin to validate, so they are always valid. */
  readonly marginsValid = computed(
    () => !this.isCustom() || this.marginsValidInput(),
  );

  readonly marginsError = computed(() => {
    if (!this.isCustom() || this.marginsValidInput()) {
      return '';
    }

    const empty = MARGIN_SIDES.every(
      (side) => this.margins()[side].trim().length === 0,
    );
    if (empty) {
      return this.i18n.t('pdfCrop.error.marginsEmpty');
    }

    return this.i18n.t('pdfCrop.error.marginsRange', {
      max: MAX_MARGIN_PT,
    });
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('pdfCrop.action.cropping');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('pdfCrop.action.crop');
    }
    return count === 1
      ? this.i18n.t('pdfCrop.action.cropOne')
      : this.i18n.t('pdfCrop.action.cropMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfCrop.destination.each'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly previewNote = computed(() => {
    const name = this.previewName();
    if (!name) {
      return '';
    }
    return this.selectedPaths().length > 1
      ? this.i18n.t('pdfCrop.preview.noteBatch', { name })
      : this.i18n.t('pdfCrop.preview.noteFirst', { name });
  });

  readonly previewAspectRatio = computed(() => {
    const size = this.previewPageSize();
    return `${size.width} / ${size.height}`;
  });

  /**
   * Margins shown in the overlay. Custom margins only count when every side is
   * valid; otherwise the overlay falls back to the full page instead of
   * drawing a crop the backend would reject.
   */
  readonly previewMargins = computed<ResolvedMargins>(() => {
    const preset = this.cropPreset();
    if (preset === CROP_PRESET_CUSTOM) {
      return this.marginsValidInput()
        ? this.resolvedMargins()
        : { top: 0, right: 0, bottom: 0, left: 0 };
    }

    const value = PRESET_MARGIN_POINTS[preset];
    return { top: value, right: value, bottom: value, left: value };
  });

  readonly previewHasMargins = computed(() =>
    MARGIN_SIDES.some((side) => this.previewMargins()[side] > 0),
  );

  /** Kept-region insets as percentages of the page, with a dimmed outside. */
  readonly previewInsets = computed<PreviewInsets>(() => {
    const size = this.previewPageSize();
    const margins = this.previewMargins();
    const horizontal = this.insetPair(margins.left, margins.right, size.width);
    const vertical = this.insetPair(margins.top, margins.bottom, size.height);
    const hasMargins = this.previewHasMargins();

    return {
      top: vertical.start,
      right: horizontal.end,
      bottom: vertical.end,
      left: horizontal.start,
      dimmed: hasMargins,
      fullPage: !hasMargins,
    };
  });

  readonly previewMarginsLabel = computed(() => {
    const margins = this.previewMargins();
    const values = MARGIN_SIDES.map((side) => margins[side]);
    const uniform = values.every((value) => value === values[0]);
    if (uniform) {
      return this.i18n.t('pdfCrop.preview.marginsUniform', {
        value: this.formatMarginValue(margins.top),
      });
    }
    const top = this.formatMarginValue(margins.top);
    const right = this.formatMarginValue(margins.right);
    const bottom = this.formatMarginValue(margins.bottom);
    const left = this.formatMarginValue(margins.left);
    return this.i18n.t('pdfCrop.preview.marginsSides', {
      top,
      right,
      bottom,
      left,
    });
  });

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.rangesRequired() &&
      this.marginsValid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfCrop.helper.addOne');
    }
    if (this.rangesRequired()) {
      return this.i18n.t('pdfCrop.helper.ranges');
    }
    return this.marginsError();
  });

  private previewGeneration = 0;
  private previewPath = '';

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/pdf-crop');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  /** Preset copy resolved at render time so the locale switch applies. */
  presetLabel(preset: CropPreset): string {
    return this.i18n.tOr(
      `pdfCrop.preset.${preset.id}.label`,
      preset.label,
    );
  }

  presetHint(preset: CropPreset): string {
    return this.i18n.tOr(
      `pdfCrop.preset.${preset.id}.hint`,
      preset.hint,
    );
  }

  marginLabel(side: MarginSide): string {
    return this.i18n.tOr(`pdfCrop.margin.${side}`, side);
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

  selectPreset(presetId: CropPresetId): void {
    if (presetId === this.cropPreset()) {
      return;
    }
    this.cropPreset.set(presetId);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  selectPageMode(mode: PageSelectionMode): void {
    if (mode === this.pageSelectionMode()) {
      return;
    }
    this.pageSelectionMode.set(mode);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onRangesInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.ranges.set(value);
    this.actionMessage.set('');

    if (value.trim().length === 0) {
      this.job.clearPlan();
      return;
    }

    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onMarginInput(side: MarginSide, event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    const current = this.margins();
    this.margins.set({
      top: side === 'top' ? value : current.top,
      right: side === 'right' ? value : current.right,
      bottom: side === 'bottom' ? value : current.bottom,
      left: side === 'left' ? value : current.left,
    });
    this.actionMessage.set('');

    if (!this.marginsValidInput()) {
      this.job.clearPlan();
      return;
    }

    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
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

  /** Retries only the failed inputs with the same preset, ranges and folder. */
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
    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.previewGeneration += 1;
    this.previewThumbnail.set('');
    this.previewName.set('');
    this.previewPath = '';
    this.previewPageSize.set({ ...A4_PAGE_POINTS });
  }

  private currentOptions(): PdfCropOptions {
    return {
      cropPreset: this.cropPreset(),
      margins: { ...this.margins() },
      pageSelectionMode: this.pageSelectionMode(),
      ranges: this.ranges(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.cropPreset.set(options.cropPreset);
    this.margins.set({ ...options.margins });
    this.pageSelectionMode.set(options.pageSelectionMode);
    this.ranges.set(options.ranges);
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const preset = this.cropPreset();
    const pageSelection = this.isRangesMode() ? this.ranges().trim() : '';

    const options: Record<string, unknown> = {
      cropPreset: preset,
      pageSelection,
    };

    if (preset === CROP_PRESET_CUSTOM && this.marginsValidInput()) {
      options['margins'] = this.resolvedMargins();
    }

    return {
      toolId: PDF_CROP_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  private resolvedMargins(): ResolvedMargins {
    const values = this.margins();
    return {
      top: Number(values.top.trim()),
      right: Number(values.right.trim()),
      bottom: Number(values.bottom.trim()),
      left: Number(values.left.trim()),
    };
  }

  private isValidMargin(raw: string): boolean {
    const trimmed = raw.trim();
    if (trimmed.length === 0) {
      return false;
    }
    const value = Number(trimmed);
    return Number.isFinite(value) && value >= 0 && value <= MAX_MARGIN_PT;
  }

  private refreshPreview(): void {
    const generation = (this.previewGeneration += 1);
    const first = this.selectedPaths()[0] ?? '';
    const firstChanged = first !== this.previewPath;
    this.previewPath = first;
    this.previewThumbnail.set('');
    this.previewName.set(first ? fileName(first) : '');

    if (firstChanged) {
      this.previewPageSize.set({ ...A4_PAGE_POINTS });
    }

    if (!first) {
      return;
    }

    void this.wailsService
      .getThumbnail(first, PREVIEW_SIZE)
      .then((thumbnailUrl) => {
        if (thumbnailUrl && generation === this.previewGeneration) {
          this.previewThumbnail.set(thumbnailUrl);
        }
      })
      .catch(() => {
        // The preview is optional; the flow never waits for it.
      });
  }

  private applyInspectedPageSize(infos: FileInfo[]): void {
    const first = this.selectedPaths()[0];
    if (!first || first !== this.previewPath) {
      return;
    }

    const info = infos.find((candidate) => candidate.path === first);
    if (info) {
      this.applyPreviewPageSize(info);
    }
  }

  private applyPreviewPageSize(info: FileInfo): void {
    const width = info.pageWidthPoints ?? 0;
    const height = info.pageHeightPoints ?? 0;
    if (width > 0 && height > 0) {
      this.previewPageSize.set({ width, height });
      return;
    }
    this.previewPageSize.set({ ...A4_PAGE_POINTS });
  }

  /**
   * Converts one margin pair to percentages, shrinking both sides together if
   * they would overlap so the overlay always stays inside the frame.
   */
  private insetPair(
    startPt: number,
    endPt: number,
    totalPt: number,
  ): { start: number; end: number } {
    let start = this.toPercent(startPt, totalPt);
    let end = this.toPercent(endPt, totalPt);
    const sum = start + end;
    if (sum > 100) {
      start = (start / sum) * 100;
      end = (end / sum) * 100;
    }
    return { start, end };
  }

  private toPercent(value: number, total: number): number {
    if (!Number.isFinite(value) || value <= 0 || total <= 0) {
      return 0;
    }
    return Math.min(100, (value / total) * 100);
  }

  private formatMarginValue(value: number): string {
    return String(Math.round(value * 10) / 10);
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
}
