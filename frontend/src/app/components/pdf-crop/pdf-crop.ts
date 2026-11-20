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
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
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

const PDF_CROP_TOOL_ID = 'tool.pdf.crop';
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 160;
const PREVIEW_SIZE = 220;
const MAX_MARGIN_PT = 720;

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

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

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

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
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
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly presets = CROP_PRESETS;
  readonly marginFields = MARGIN_FIELDS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly cropPreset = signal<CropPresetId>(CROP_PRESET_SMALL);
  readonly margins = signal<MarginValues>({ ...EMPTY_MARGINS });
  readonly pageSelectionMode = signal<PageSelectionMode>(PAGE_SELECTION_ALL);
  readonly ranges = signal('');
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly previewThumbnail = signal('');
  readonly previewName = signal('');
  readonly previewPageSize = signal<PageSize>({ ...A4_PAGE_POINTS });

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
      this.presets.find((preset) => preset.id === this.cropPreset())?.label ??
      'Small',
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
      return 'Enter all four margins to use Custom.';
    }

    return `Use numbers from 0 to ${MAX_MARGIN_PT} pt for every margin.`;
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Cropping…';
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return 'Crop PDFs';
    }
    return count === 1 ? 'Crop 1 PDF' : `Crop ${count} PDFs`;
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each PDF',
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
      ? `First page of ${name}. The same crop applies to every PDF.`
      : `First page of ${name}. The crop is applied when you run it.`;
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
      return `${this.formatMarginValue(margins.top)} pt`;
    }
    const top = this.formatMarginValue(margins.top);
    const right = this.formatMarginValue(margins.right);
    const bottom = this.formatMarginValue(margins.bottom);
    const left = this.formatMarginValue(margins.left);
    return `T ${top} · R ${right} · B ${bottom} · L ${left}`;
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
      return 'Add at least one PDF to crop.';
    }
    if (this.rangesRequired()) {
      return 'Enter the page ranges to crop.';
    }
    return this.marginsError();
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
      ? describeToolError(error, 'The crop could not start.')
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;
  private previewGeneration = 0;
  private previewPath = '';

  constructor(private readonly wailsService: Wails) {}

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/pdf-crop');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'PDF', 'PDFs');
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
    const known = new Set(
      this.selectedPaths().map((path) => this.pathKey(path)),
    );

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

  selectPreset(presetId: CropPresetId): void {
    if (presetId === this.cropPreset()) {
      return;
    }
    this.cropPreset.set(presetId);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  selectPageMode(mode: PageSelectionMode): void {
    if (mode === this.pageSelectionMode()) {
      return;
    }
    this.pageSelectionMode.set(mode);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onRangesInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.ranges.set(value);
    this.actionMessage.set('');

    if (value.trim().length === 0) {
      this.clearPlanRefreshTimer();
      this.clearPlanState();
      return;
    }

    this.schedulePlanRefresh();
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
      this.clearPlanRefreshTimer();
      this.clearPlanState();
      return;
    }

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
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for destinations, so it is always
    // refreshed with the current inputs before starting the job.
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
      toolId: PDF_CROP_TOOL_ID,
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

  /** Retries only the failed inputs with the same preset, ranges and folder. */
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
    this.previewGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.previewThumbnail.set('');
    this.previewName.set('');
    this.previewPath = '';
    this.previewPageSize.set({ ...A4_PAGE_POINTS });
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

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const preset = this.cropPreset();
    const pageSelection =
      this.pageSelectionMode() === PAGE_SELECTION_RANGES
        ? this.ranges().trim()
        : '';

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

  private async refreshPlan(): Promise<boolean> {
    if (
      this.selectedPaths().length === 0 ||
      this.rangesRequired() ||
      !this.marginsValid()
    ) {
      this.clearPlanState();
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
    // Keep the failed plan so the shared plan card can show the range or
    // destination problem next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private clearPlanState(): void {
    this.planToken += 1;
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
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
      toolId: PDF_CROP_TOOL_ID,
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
    const existing = new Map(this.fileItems().map((item) => [item.path, item]));
    this.fileItems.set(
      paths.map(
        (path) => existing.get(path) ?? { path, name: fileName(path) },
      ),
    );

    this.refreshPreview();

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

        const firstInfo = infos.get(paths[0]);
        if (firstInfo && paths[0] === this.previewPath) {
          this.applyPreviewPageSize(firstInfo);
        }
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
      parts.push(formatCount(info.pageCount, 'page'));
    }
    return parts.join(' · ');
  }

  private intakeNotice(rejected: string[], duplicates: string[]): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(`${fileName(rejected[0])} wasn't added because it isn't a PDF.`);
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(`${names} weren't added because they aren't PDFs.`);
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(`${duplicates.length} PDFs are already in the queue.`);
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
