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
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import {
  Crop,
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
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import {
  FileInfoV1,
  ImageCropPreviewRequestV1,
  ImagePreviewSourceResponseV1,
  InspectFilesResponseV1,
  JobErrorV1,
  JobExecutionStatusV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  RunJobResponseV1,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';

const IMAGE_CROP_TOOL_ID = 'tool.image.crop';
const POLLING_INTERVAL_MS = 1000;
const BOUNDS_PLAN_DEBOUNCE_MS = 200;
const CROP_PREVIEW_DEBOUNCE_MS = 200;
const THUMBNAIL_SIZE = 160;

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

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

const EMPTY_BOUNDS: BoundsInput = { x: '0', y: '0', width: '0', height: '0' };
const EMPTY_RECT: CropRect = { x: 0, y: 0, width: 0, height: 0 };

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
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly cropIcon = Crop;
  readonly resetIcon = RotateCcw;

  readonly ratioPresets = RATIO_PRESETS;
  readonly formatOptions = FORMAT_OPTIONS;
  readonly rotateOptions = ROTATE_OPTIONS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
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
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly advancedOpen = signal(false);

  readonly plan = signal<PlanJobResponseV1 | null>(null);
  readonly planError = signal<JobErrorV1 | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResultV1 | null>(null);
  readonly jobError = signal<JobErrorV1 | null>(null);
  readonly outputInfos = signal<Record<string, FileInfoV1>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly previewLoading = signal(false);
  readonly previewError = signal('');
  readonly cropPreviewUrl = signal('');
  readonly isCropPreviewLoading = signal(false);

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
      return 'Enter whole numbers for X, Y, width and height.';
    }
    if (crop.width < 1 || crop.height < 1) {
      return 'Crop width and height must be at least 1 px.';
    }
    if (crop.x < 0 || crop.y < 0) {
      return 'Crop position must be zero or greater.';
    }
    if (
      crop.x + crop.width > this.imageWidth() ||
      crop.y + crop.height > this.imageHeight()
    ) {
      return 'The crop must stay inside the image.';
    }

    const preset = this.ratioDefinition();
    if (
      this.ratioPreset() !== 'free' &&
      crop.width * preset.heightStep !== crop.height * preset.widthStep
    ) {
      if (!this.ratioFitsImage()) {
        return 'This image is too small for the selected ratio.';
      }
      return `A ${this.ratioPreset()} crop needs matching width and height.`;
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
      return 'Cropping…';
    }
    const count = this.selectedPaths().length;
    return count > 1 ? `Crop ${count} images` : 'Crop image';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each image',
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

  readonly actionHelper = computed(() => {
    if (!this.hasSelection()) {
      return 'Add at least one image to crop.';
    }
    if (!this.hasPreview()) {
      return this.previewError() || 'Loading the image preview…';
    }
    return this.cropError();
  });

  readonly terminalJob = computed<JobResultV1 | null>(() => {
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
      ? describeToolError(error, 'The crop could not start.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private cropPreviewTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private cropPreviewToken = 0;
  private fileItemsGeneration = 0;
  private previewGeneration = 0;

  private dragState:
    | {
        mode: DragMode;
        startX: number;
        startY: number;
        startRect: CropRect;
      }
    | undefined;

  constructor(private readonly wailsService: Wails) {
    this.wailsService.jobProgress$
      .pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        if (!event || event.jobId !== this.activeJobId()) {
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
          // Wait for the status endpoint to settle the final items. The event
          // can arrive before the job store has the complete result.
          status: this.isTerminalStatus(event.status)
            ? current.status
            : (event.status as JobExecutionStatusV1),
        });

        if (this.isTerminalStatus(event.status)) {
          void this.pollJobStatus();
        }
      });
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.clearCropPreviewTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/image-crop');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
  }

  @HostListener('window:resize')
  onWindowResize(): void {
    this.syncSelectionRectFromCrop();
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
      if (
        !SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase())
      ) {
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

    const firstChanged = previousFirst !== this.selectedPaths()[0];
    if (firstChanged) {
      void this.reloadPreviewAndPlan();
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
    void this.reloadPreviewAndPlan();
  }

  removeFile(path: string): void {
    const previousFirst = this.selectedPaths()[0] ?? '';
    const next = this.selectedPaths().filter(
      (candidate) => candidate !== path,
    );
    if (next.length === this.selectedPaths().length) {
      return;
    }

    this.selectedPaths.set(next);
    this.syncFileItems();

    if ((next[0] ?? '') === previousFirst) {
      void this.refreshPlan();
    } else {
      void this.reloadPreviewAndPlan();
    }
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
    void this.refreshPlan();
  }

  onBoundsInput(axis: BoundsAxis, event: Event): void {
    const value = this.readEventValue(event);
    this.bounds.update((bounds) => ({ ...bounds, [axis]: value }));
    this.actionMessage.set('');
    this.syncSelectionRectFromCrop();
    this.schedulePlanRefresh();
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
    void this.refreshPlan();
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
    this.schedulePlanRefresh();
  }

  onRotateChange(event: Event): void {
    const value = Number.parseInt(this.readEventValue(event), 10);
    if (!ROTATE_OPTIONS.some((option) => option.value === value)) {
      return;
    }

    this.rotate.set(value as RotateAngle);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onFlipToggle(axis: 'h' | 'v', event: Event): void {
    const checked = !!(event.target as HTMLInputElement | null)?.checked;
    if (axis === 'h') {
      this.flipH.set(checked);
    } else {
      this.flipV.set(checked);
    }
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  resetTransforms(): void {
    this.outputWidth.set(null);
    this.outputHeight.set(null);
    this.rotate.set(0);
    this.flipH.set(false);
    this.flipV.set(false);
    void this.refreshPlan();
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
    await this.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }

    this.outputDir.set('');
    void this.refreshPlan();
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
    this.schedulePlanRefresh();
    this.scheduleCropPreviewRefresh();
  }

  async run(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for destinations and collision-safe
    // names, so it is refreshed with the current crop before starting.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponseV1 = await this.wailsService
      .runJobV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The crop could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The crop could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The crop could not start.',
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
      toolId: IMAGE_CROP_TOOL_ID,
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
    this.startPolling();
  }

  async cancel(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    const response = await this.wailsService.cancelJobV1(jobId);
    if (!response.success) {
      this.actionMessage.set(
        response.message || 'The job could not be cancelled.',
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success ? '' : response.message || 'The file could not be opened.',
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
    this.selectedPaths.set([...new Set(failed)]);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.syncFileItems();

    if ((this.selectedPaths()[0] ?? '') !== previousFirst) {
      await this.refreshPreviewSource();
    }

    await this.run();
  }

  reset(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.clearCropPreviewTimer();
    this.planToken += 1;
    this.cropPreviewToken += 1;
    this.fileItemsGeneration += 1;
    this.previewGeneration += 1;
    this.dragState = undefined;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
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

  private buildRequest(): JobRequestV1 {
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

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length === 0 || !this.cropValid()) {
      this.clearPlanState();
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponseV1 = await this.wailsService
      .planJobV1(this.buildRequest())
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

    const error: JobErrorV1 = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || 'The output could not be planned.',
    };
    // Keep the failed plan so the shared plan card can show the destination
    // or format problem next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private clearPlanState(): void {
    this.planToken += 1;
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.clearPlanRefreshTimer();
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    if (this.selectedPaths().length === 0) {
      this.clearPlanState();
      return;
    }

    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, BOUNDS_PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  private async reloadPreviewAndPlan(): Promise<void> {
    await this.refreshPreviewSource();
    await this.refreshPlan();
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

    if (!path) {
      this.previewLoading.set(false);
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      this.selectionRectPx.set({ ...EMPTY_RECT });
      return;
    }

    this.previewLoading.set(true);

    const response: ImagePreviewSourceResponseV1 =
      await this.wailsService.getImagePreviewSourceV1(path).catch(() => ({
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
      this.selectionRectPx.set({ ...EMPTY_RECT });
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

    const request: ImageCropPreviewRequestV1 = {
      inputPath: path,
      x: crop.x,
      y: crop.y,
      width: crop.width,
      height: crop.height,
      ratioPreset: this.ratioPreset(),
      format: this.format(),
    };

    const response = await this.wailsService
      .getImageCropPreviewV1(request)
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

  private startPolling(): void {
    this.stopPolling();
    this.isPolling.set(true);
    void this.pollJobStatus();
    this.pollingTimer = setInterval(() => {
      void this.pollJobStatus();
    }, POLLING_INTERVAL_MS);
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isPolling.set(false);
  }

  private async pollJobStatus(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      this.stopPolling();
      return;
    }

    const response: JobStatusResponseV1 = await this.wailsService
      .getJobStatusV1(jobId)
      .catch(() => ({
        success: false,
        message: 'The job status could not be read.',
        found: false,
        error: {
          code: 'EXEC_IO_TRANSIENT' as const,
          detail_code: 'JOB_STATUS_FAILED',
          message: 'The job status could not be read.',
        },
      }));
    if (this.activeJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.stopPolling();
      this.activeJobId.set('');
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'JOB_STATUS_MISSING',
          message: response.message || 'The job status could not be read.',
        },
      );
      return;
    }

    this.jobResult.set(response.result);
    if (this.isTerminalStatus(response.result.status)) {
      this.stopPolling();
      this.activeJobId.set('');
      void this.loadOutputInfos();
    }
  }

  private async loadOutputInfos(): Promise<void> {
    const paths = this.collectOutputPaths();
    if (paths.length === 0) {
      this.outputInfos.set({});
      return;
    }

    const response: InspectFilesResponseV1 = await this.wailsService
      .inspectFilesV1(paths)
      .catch(() => ({
        success: false,
        message: 'File sizes could not be read.',
        files: [],
      }));
    if (!response.success) {
      this.outputInfos.set({});
      return;
    }

    const infos: Record<string, FileInfoV1> = {};
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

  private syncFileItems(): void {
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

  private async enrichFileItems(
    paths: string[],
    generation: number,
  ): Promise<void> {
    try {
      const response = await this.wailsService.inspectFilesV1(paths);
      if (generation === this.fileItemsGeneration && response.success) {
        const infos = new Map<string, FileInfoV1>();
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

  private describeFileInfo(info: FileInfoV1): string {
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

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
