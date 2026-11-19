import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
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
import { VIDEO_PRESETS, VideoPreset } from '../../config/video-presets';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
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
  formatDuration,
} from '../../utils/format';
import { resumeToolJob } from '../../utils/resume-task';

const VIDEO_MERGE_TOOL_ID = 'tool.video.merge';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 200;
const THUMBNAIL_SIZE = 160;

const CROSSFADE_DURATION_DEFAULT = 0.5;
const CROSSFADE_DURATION_MIN = 0.1;
const CROSSFADE_DURATION_MAX = 3.0;

type VideoTransition = 'none' | 'crossfade';

const SUPPORTED_VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

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

@Component({
  selector: 'app-video-merge',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './video-merge.html',
  styleUrl: './video-merge.css',
})
export class VideoMerge implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly presets = VIDEO_PRESETS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly selectedPresetId = signal(VIDEO_PRESETS[0].id);
  readonly removeAudio = signal(false);
  readonly transition = signal<VideoTransition>('none');
  readonly transitionDuration = signal(
    CROSSFADE_DURATION_DEFAULT.toFixed(1),
  );
  readonly outputDir = signal(this.preferences.defaultOutputDir());

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

  readonly selectedPreset = computed<VideoPreset>(
    () =>
      VIDEO_PRESETS.find(
        (preset) => preset.id === this.selectedPresetId(),
      ) ?? VIDEO_PRESETS[0],
  );

  readonly isCrossfade = computed(() => this.transition() === 'crossfade');

  readonly transitionDurationValue = computed<number | null>(() => {
    const raw = this.transitionDuration().trim();
    if (raw === '') {
      return null;
    }
    const value = Number(raw);
    return Number.isFinite(value) ? value : null;
  });

  /** A crossfade needs a finite duration inside the backend's allowed range. */
  readonly transitionDurationInvalid = computed(() => {
    if (!this.isCrossfade()) {
      return false;
    }
    const value = this.transitionDurationValue();
    return (
      value === null ||
      value < CROSSFADE_DURATION_MIN ||
      value > CROSSFADE_DURATION_MAX
    );
  });

  readonly canRun = computed(
    () =>
      this.selectedPaths().length >= 2 &&
      !this.transitionDurationInvalid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Merging…';
    }
    const count = this.selectedPaths().length;
    return count >= 2 ? `Merge ${count} videos` : 'Merge videos';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to the first video',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly actionHelper = computed(() => {
    const count = this.selectedPaths().length;
    if (count === 0) {
      return 'Add at least two videos to merge.';
    }
    if (count === 1) {
      return 'Add one more video to merge.';
    }
    return '';
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
    return error ? describeToolError(error, 'The merge could not start.') : null;
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
    const pending = this.dropIntake.take('/video-merge');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'video');
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
    const known = new Set(this.selectedPaths().map((path) => this.pathKey(path)));

    for (const path of incoming) {
      if (!SUPPORTED_VIDEO_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
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

  moveUp(path: string): void {
    const index = this.selectedPaths().indexOf(path);
    if (index <= 0) {
      return;
    }
    this.swap(index, index - 1);
  }

  moveDown(path: string): void {
    const index = this.selectedPaths().indexOf(path);
    if (index < 0 || index >= this.selectedPaths().length - 1) {
      return;
    }
    this.swap(index, index + 1);
  }

  selectPreset(presetId: string): void {
    if (presetId === this.selectedPresetId()) {
      return;
    }
    this.selectedPresetId.set(presetId);
    void this.refreshPlan();
  }

  onRemoveAudioToggle(event: Event): void {
    this.removeAudio.set(!!(event.target as HTMLInputElement | null)?.checked);
    void this.refreshPlan();
  }

  onTransitionChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value;
    this.transition.set(value === 'crossfade' ? 'crossfade' : 'none');
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onTransitionDurationInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.transitionDuration.set(value);
    this.actionMessage.set('');
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

  useFirstVideoFolder(): void {
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
        message: 'The merge could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The merge could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The merge could not start.',
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
      toolId: VIDEO_MERGE_TOOL_ID,
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

  /** A failed merge has no partial queue to retry: rerun the same inputs. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');

    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

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
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  private buildRequest(): JobRequestV1 {
    const preset = this.selectedPreset();
    const options: Record<string, unknown> = {
      targetFormat: preset.targetFormat,
      qualityPreset: preset.qualityPreset,
    };
    if (this.removeAudio()) {
      options['removeAudio'] = true;
    }
    if (this.isCrossfade()) {
      options['transition'] = 'crossfade';
      const duration = this.transitionDurationValue();
      if (duration !== null) {
        options['transitionDuration'] = duration;
      }
    }

    return {
      toolId: VIDEO_MERGE_TOOL_ID,
      mode: 'single',
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options,
    };
  }

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length < 2) {
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
      toolId: VIDEO_MERGE_TOOL_ID,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startPolling: () => this.startPolling(),
    });
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

  private swap(leftIndex: number, rightIndex: number): void {
    const next = [...this.selectedPaths()];
    const left = next[leftIndex];
    next[leftIndex] = next[rightIndex];
    next[rightIndex] = left;
    this.selectedPaths.set(next);
    this.syncFileItems();
    void this.refreshPlan();
  }

  private syncFileItems(): void {
    const paths = this.selectedPaths();
    const generation = (this.fileItemsGeneration += 1);
    const existing = new Map(this.fileItems().map((item) => [item.path, item]));
    this.fileItems.set(
      paths.map((path) => existing.get(path) ?? { path, name: fileName(path) }),
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
        // Thumbnails are optional; keep the video icon.
      });
  }

  private describeFileInfo(info: FileInfoV1): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (
      typeof info.durationSeconds === 'number' &&
      info.durationSeconds > 0
    ) {
      parts.push(formatDuration(info.durationSeconds));
    }
    return parts.join(' · ');
  }

  private intakeNotice(rejected: string[], duplicates: string[]): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(`${fileName(rejected[0])} wasn't added because it isn't a video.`);
    } else if (rejected.length > 0) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(`${names} weren't added because they aren't videos.`);
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(`${duplicates.length} videos are already in the queue.`);
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
