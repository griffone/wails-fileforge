import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
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
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import {
  IMAGE_FORMAT_OPTIONS,
  IMAGE_PRESETS,
  ImageOutputFormat,
  ImagePreset,
} from '../../config/image-presets';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import {
  FileInfoV1,
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

const IMAGE_TOOL_ID = 'tool.image.convert';
const POLLING_INTERVAL_MS = 1000;
const ADVANCED_PLAN_DEBOUNCE_MS = 200;
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

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

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
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly presets = IMAGE_PRESETS;
  readonly formatOptions = IMAGE_FORMAT_OPTIONS;
  readonly resizeModes = RESIZE_MODES;
  readonly rotateOptions = ROTATE_OPTIONS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly selectedPresetId = signal('webp');
  readonly format = signal<ImageOutputFormat>('webp');
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly advancedOpen = signal(false);
  readonly quality = signal<number | null>(null);
  readonly resizeMode = signal<ResizeMode>('none');
  readonly longestEdge = signal<number | null>(null);
  readonly width = signal<number | null>(null);
  readonly stripMetadata = signal(false);
  readonly rotate = signal(0);
  readonly flipH = signal(false);
  readonly flipV = signal(false);
  readonly inputInfos = signal<Record<string, FileInfoV1>>({});

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
    return preset?.label ?? this.formatLabel(format);
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
      return `Saved ${formatBytes(saved)} (${percent}%)`;
    }
    return `Output is ${formatBytes(output - input)} larger`;
  });

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Converting…';
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return 'Convert';
    }
    return count === 1 ? 'Convert 1 image' : `Convert ${count} images`;
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each original',
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
      ? describeToolError(error, 'The conversion could not start.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

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
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/image-converter');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
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
    void this.refreshPlan();
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
    await this.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.refreshPlan();
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
    this.schedulePlanRefresh();
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
    void this.refreshPlan();
  }

  onResizeModeChange(event: Event): void {
    const value = this.readValue(event) as ResizeMode;
    if (!RESIZE_MODES.some((mode) => mode.value === value)) {
      return;
    }
    this.resizeMode.set(value);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onLongestEdgeInput(event: Event): void {
    this.longestEdge.set(this.readPositiveInteger(event));
    this.schedulePlanRefresh();
  }

  onWidthInput(event: Event): void {
    this.width.set(this.readPositiveInteger(event));
    this.schedulePlanRefresh();
  }

  onStripMetadataToggle(event: Event): void {
    this.stripMetadata.set(this.readChecked(event));
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onRotateChange(event: Event): void {
    const value = Number.parseInt(this.readValue(event), 10);
    if (!ROTATE_OPTIONS.some((option) => option.value === value)) {
      return;
    }
    this.rotate.set(value);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onFlipToggle(axis: 'h' | 'v', event: Event): void {
    if (axis === 'h') {
      this.flipH.set(this.readChecked(event));
    } else {
      this.flipV.set(this.readChecked(event));
    }
    this.actionMessage.set('');
    void this.refreshPlan();
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
    this.schedulePlanRefresh();
  }

  async run(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.actionMessage.set('');

    if (this.planLoading() || !this.plan()?.success) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponseV1 = await this.wailsService
      .runJobV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The conversion could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The conversion could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The conversion could not start.',
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
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.inputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  private buildRequest(): JobRequestV1 {
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

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length === 0) {
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
    // Keep the failed plan so the shared plan card can show the error in
    // context, next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, ADVANCED_PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
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
          code: 'EXEC_IO_TRANSIENT',
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
    this.fileItems.set(
      paths.map((path) => ({ path, name: fileName(path) })),
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
        const inputInfos: Record<string, FileInfoV1> = {};
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

  private rejectionNotice(rejected: string[]): string {
    const extensions = [
      ...new Set(
        rejected.map(
          (path) => fileExtension(path).toUpperCase() || 'UNKNOWN',
        ),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1 ? "1 file wasn't added." : `${count} files weren't added.`;

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = `${extensions[0]} isn't supported.`;
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = `${head} and ${extensions[extensions.length - 1]} aren't supported.`;
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
    return (
      IMAGE_FORMAT_OPTIONS.find((option) => option.value === format)?.label ??
      format
    );
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
