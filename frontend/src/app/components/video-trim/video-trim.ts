import {
  Component,
  ElementRef,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import {
  FolderOpen,
  LucideAngularModule,
  Pause,
  Play,
  ShieldCheck,
  TriangleAlert,
  VideoOff,
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
  FileInfo,
  InspectFilesResponse,
  JobError,
  JobExecutionStatus,
  JobRequest,
  JobResult,
  JobStatusResponse,
  MediaSourceResponse,
  PlanJobResponse,
  RunJobResponse,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
  formatDuration,
  parseTimeToSeconds,
} from '../../utils/format';
import { resumeToolJob } from '../../utils/resume-task';

const VIDEO_TRIM_TOOL_ID = 'tool.video.trim';
const POLLING_INTERVAL_MS = 1000;
const TIME_PLAN_DEBOUNCE_MS = 200;
const MIN_TRIM_RANGE_SECONDS = 0.1;
const SMALL_NUDGE_SECONDS = 0.1;
const LARGE_NUDGE_SECONDS = 1;
const PREVIEW_UNAVAILABLE_MESSAGE =
  'Video preview unavailable. You can still trim by time.';

const SUPPORTED_VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

const FORMAT_LABELS: Record<VideoPreset['targetFormat'], string> = {
  mp4: 'MP4',
  webm: 'WebM',
};

const QUALITY_LABELS: Record<VideoPreset['qualityPreset'], string> = {
  high: 'High',
  medium: 'Balanced',
  low: 'Small',
};

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type TargetFormat = VideoPreset['targetFormat'];
type QualityPreset = VideoPreset['qualityPreset'];
type TrimMode = 'auto' | 'copy' | 'reencode';
type TrimHandle = 'start' | 'end';

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

@Component({
  selector: 'app-video-trim',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './video-trim.html',
  styleUrl: './video-trim.css',
})
export class VideoTrim implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly playIcon = Play;
  readonly pauseIcon = Pause;
  readonly videoOffIcon = VideoOff;

  readonly presets = VIDEO_PRESETS;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly selectedPresetId = signal(VIDEO_PRESETS[0].id);
  readonly trimMode = signal<TrimMode>('auto');
  readonly outputDir = signal(this.preferences.defaultOutputDir());
  readonly advancedOpen = signal(false);

  readonly startInput = signal('0');
  readonly endInput = signal('');
  readonly durationSeconds = signal<number | null>(null);

  readonly mediaUrl = signal<string | null>(null);
  readonly videoError = signal('');
  readonly videoReady = signal(false);
  readonly mediaDuration = signal<number | null>(null);
  readonly currentTime = signal(0);
  readonly isPlaying = signal(false);
  readonly fileDurations = signal<Record<string, number>>({});

  private readonly videoPreview =
    viewChild<ElementRef<HTMLVideoElement>>('videoPreview');
  private readonly timelineTrack =
    viewChild<ElementRef<HTMLElement>>('timelineTrack');

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

  readonly selectedPreset = computed<VideoPreset>(
    () =>
      VIDEO_PRESETS.find(
        (preset) => preset.id === this.selectedPresetId(),
      ) ?? VIDEO_PRESETS[0],
  );

  readonly targetFormat = computed<TargetFormat>(
    () => this.selectedPreset().targetFormat,
  );

  readonly qualityPreset = computed<QualityPreset>(
    () => this.selectedPreset().qualityPreset,
  );

  readonly outputSummary = computed(
    () =>
      `${FORMAT_LABELS[this.targetFormat()]} · ${QUALITY_LABELS[this.qualityPreset()]}`,
  );

  readonly startSeconds = computed(() =>
    parseTimeToSeconds(this.startInput()),
  );
  readonly endSeconds = computed(() => parseTimeToSeconds(this.endInput()));

  readonly timeError = computed(() => {
    const start = this.startSeconds();
    const end = this.endSeconds();
    const endText = this.endInput().trim();

    if (!endText) {
      return start === null ? 'Use a time like 00:30.' : '';
    }
    if (start === null || end === null) {
      return 'Use a time like 00:30.';
    }
    if (end <= start) {
      return 'End must be after start.';
    }

    const duration = this.durationSeconds();
    if (duration !== null && start >= duration) {
      return 'Start must be before the video ends.';
    }

    return '';
  });

  readonly timeRangeValid = computed(
    () =>
      this.endInput().trim().length > 0 &&
      this.timeError() === '' &&
      this.startSeconds() !== null &&
      this.endSeconds() !== null,
  );

  readonly firstVideoDuration = computed<number | null>(() => {
    const path = this.selectedPaths()[0];
    if (!path) {
      return null;
    }
    const duration = this.fileDurations()[path];
    return typeof duration === 'number' && duration > 0 ? duration : null;
  });

  readonly previewDuration = computed<number | null>(() => {
    const actual = this.mediaDuration();
    if (actual !== null) {
      return actual;
    }
    return this.firstVideoDuration() ?? this.durationSeconds();
  });

  readonly sourceDurationLabel = computed(() => {
    const duration = this.previewDuration();
    return duration !== null ? formatDuration(duration) : '';
  });

  readonly selectionStartPercent = computed(() =>
    this.timePercent(this.startSeconds()),
  );

  readonly selectionEndPercent = computed(() => {
    const end = this.endSeconds();
    return this.timePercent(end ?? this.previewDuration());
  });

  readonly selectionWidthPercent = computed(() =>
    Math.max(0, this.selectionEndPercent() - this.selectionStartPercent()),
  );

  readonly playheadPercent = computed(() =>
    this.timePercent(this.currentTime()),
  );

  readonly batchDurationWarning = computed(() => {
    if (!this.isBatch()) {
      return '';
    }

    const end = this.endSeconds();
    if (end === null) {
      return '';
    }

    const durations = this.fileDurations();
    for (const path of this.selectedPaths()) {
      const duration = durations[path];
      if (
        typeof duration === 'number' &&
        duration > 0 &&
        duration < end
      ) {
        return (
          `The end is longer than "${fileName(path)}" (${formatDuration(duration)}). ` +
          "The cut will stop at that clip's end."
        );
      }
    }

    return '';
  });

  readonly isBatch = computed(() => this.selectedPaths().length > 1);

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Trimming…';
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return 'Trim';
    }
    return count === 1 ? 'Trim 1 video' : `Trim ${count} videos`;
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
      this.timeRangeValid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return 'Add at least one video to trim.';
    }
    if (!this.endInput().trim()) {
      return 'Set an end time to trim.';
    }
    return '';
  });

  readonly trimModeHelp = computed(() => {
    switch (this.trimMode()) {
      case 'copy':
        return 'Fastest. Only works when the cut points line up with keyframes.';
      case 'reencode':
        return 'Most precise cut. Slower and produces a larger file.';
      default:
        return 'Tries a fast copy first and re-encodes only when needed.';
    }
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
      ? describeToolError(error, 'The trim could not start.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;
  private mediaRequestToken = 0;
  private mediaPath = '';
  private mediaPrimaryUrl = '';
  private mediaSecondaryUrl = '';
  private mediaTryingSecondary = false;
  private dragHandle: TrimHandle | null = null;
  private dragCleanup: (() => void) | null = null;
  private selectionEndSeconds: number | null = null;

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
            : (event.status as JobExecutionStatus),
        });

        if (this.isTerminalStatus(event.status)) {
          void this.pollJobStatus();
        }
      });
  }

  ngOnDestroy(): void {
    this.endHandleDrag();
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/video-trim');
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

    const supported: string[] = [];
    const rejected: string[] = [];
    for (const path of incoming) {
      if (SUPPORTED_VIDEO_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
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
    if (presetId === this.selectedPresetId()) {
      return;
    }
    this.selectedPresetId.set(presetId);
    void this.refreshPlan();
  }

  onTrimModeChange(event: Event): void {
    const value = this.readSelect(event);
    if (value !== 'auto' && value !== 'copy' && value !== 'reencode') {
      return;
    }
    this.trimMode.set(value);
    this.schedulePlanRefresh();
  }

  onStartInput(event: Event): void {
    this.startInput.set(this.readInput(event));
    this.onTimeChanged();
  }

  onEndInput(event: Event): void {
    this.endInput.set(this.readInput(event));
    this.onTimeChanged();
  }

  onStartBlur(event: Event): void {
    this.startInput.set(this.readInput(event));
    this.onTimeChanged();
  }

  onEndBlur(event: Event): void {
    this.endInput.set(this.readInput(event));
    this.onTimeChanged();
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

  formatClock(seconds: number | null): string {
    if (seconds === null || !Number.isFinite(seconds) || seconds <= 0) {
      return '0:00';
    }
    return formatDuration(seconds);
  }

  timePercent(seconds: number | null): number {
    const duration = this.previewDuration();
    if (
      duration === null ||
      duration <= 0 ||
      seconds === null ||
      !Number.isFinite(seconds)
    ) {
      return 0;
    }
    return Math.min(100, Math.max(0, (seconds / duration) * 100));
  }

  onVideoMetadata(event: Event): void {
    const target = event.target as HTMLVideoElement | null;
    const raw = target?.duration ?? Number.NaN;
    const duration =
      Number.isFinite(raw) && raw > 0
        ? raw
        : (this.firstVideoDuration() ?? this.durationSeconds());

    this.videoReady.set(true);
    if (duration === null || !Number.isFinite(duration) || duration <= 0) {
      return;
    }

    this.mediaDuration.set(duration);
    if (this.selectedPaths().length === 1) {
      this.durationSeconds.set(duration);
    }
    if (!this.endInput().trim()) {
      this.endInput.set(this.toTimeInput(duration));
      this.onTimeChanged();
    }
  }

  onTimeUpdate(event: Event): void {
    const video = event.target as HTMLVideoElement | null;
    const time = video?.currentTime ?? 0;
    if (!Number.isFinite(time)) {
      return;
    }

    const selectionEnd = this.selectionEndSeconds;
    if (selectionEnd !== null && time >= selectionEnd) {
      this.selectionEndSeconds = null;
      this.videoElement()?.pause();
      this.seekTo(selectionEnd);
      this.isPlaying.set(false);
      return;
    }

    this.currentTime.set(Math.max(0, time));
  }

  onVideoEnded(): void {
    this.selectionEndSeconds = null;
    this.isPlaying.set(false);
  }

  onVideoPlay(): void {
    this.isPlaying.set(true);
  }

  onVideoPause(): void {
    this.isPlaying.set(false);
  }

  onVideoError(): void {
    if (this.mediaSecondaryUrl && !this.mediaTryingSecondary) {
      this.mediaTryingSecondary = true;
      this.videoError.set('');
      this.videoReady.set(false);
      this.mediaUrl.set(this.mediaSecondaryUrl);
      queueMicrotask(() => this.videoElement()?.load());
      return;
    }

    if (!this.mediaUrl()) {
      return;
    }
    this.mediaUrl.set(null);
    this.videoError.set(PREVIEW_UNAVAILABLE_MESSAGE);
    this.videoReady.set(false);
    this.isPlaying.set(false);
    this.selectionEndSeconds = null;
  }

  togglePlay(): void {
    const video = this.videoElement();
    if (!video || !this.mediaUrl() || this.videoError()) {
      return;
    }

    if (video.paused) {
      this.selectionEndSeconds = null;
      this.playVideo(video);
      return;
    }

    video.pause();
  }

  playSelection(): void {
    const start = this.startSeconds();
    const end = this.endSeconds();
    if (start === null || end === null || end <= start) {
      return;
    }

    this.selectionEndSeconds = end;
    this.seekTo(start);

    const video = this.videoElement();
    if (video) {
      this.playVideo(video);
    }
  }

  setStartHere(): void {
    this.setStartAt(this.currentTime());
  }

  setEndHere(): void {
    this.setEndAt(this.currentTime());
  }

  onTimelinePointerDown(event: PointerEvent): void {
    if (this.dragHandle) {
      return;
    }

    const time = this.pointerTime(event);
    if (time === null) {
      return;
    }
    event.preventDefault();
    this.seekTo(time);
  }

  onHandlePointerDown(event: PointerEvent, handle: TrimHandle): void {
    event.preventDefault();
    event.stopPropagation();

    if (this.previewDuration() === null) {
      return;
    }

    this.endHandleDrag();
    this.dragHandle = handle;

    const onMove = (moveEvent: PointerEvent) => {
      if (this.dragHandle !== handle) {
        return;
      }
      moveEvent.preventDefault();
      const time = this.pointerTime(moveEvent);
      if (time !== null) {
        this.applyHandlePosition(handle, time);
      }
    };
    const onUp = () => this.endHandleDrag();

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    this.dragCleanup = () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };

    const time = this.pointerTime(event);
    if (time !== null) {
      this.applyHandlePosition(handle, time);
    }
  }

  onEditorKeydown(event: KeyboardEvent): void {
    if (this.shouldIgnoreKey(event)) {
      return;
    }

    const step = event.shiftKey ? LARGE_NUDGE_SECONDS : SMALL_NUDGE_SECONDS;
    switch (event.key) {
      case ' ':
      case 'Spacebar':
        event.preventDefault();
        this.togglePlay();
        break;
      case 'i':
      case 'I':
        event.preventDefault();
        this.setStartHere();
        break;
      case 'o':
      case 'O':
        event.preventDefault();
        this.setEndHere();
        break;
      case 'ArrowLeft':
        event.preventDefault();
        this.seekTo(this.currentTime() - step);
        break;
      case 'ArrowRight':
        event.preventDefault();
        this.seekTo(this.currentTime() + step);
        break;
    }
  }

  onHandleKeydown(event: KeyboardEvent, handle: TrimHandle): void {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') {
      return;
    }

    event.preventDefault();
    event.stopPropagation();

    const step = event.shiftKey ? LARGE_NUDGE_SECONDS : SMALL_NUDGE_SECONDS;
    const delta = event.key === 'ArrowLeft' ? -step : step;
    if (handle === 'start') {
      this.setStartAt((this.startSeconds() ?? 0) + delta);
      return;
    }

    const end = this.endSeconds() ?? this.previewDuration() ?? 0;
    this.setEndAt(end + delta);
  }

  endHandleDrag(): void {
    this.dragCleanup?.();
    this.dragCleanup = null;
    this.dragHandle = null;
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

    const response: RunJobResponse = await this.wailsService
      .runJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The trim could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The trim could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The trim could not start.',
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
      toolId: VIDEO_TRIM_TOOL_ID,
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
    this.endHandleDrag();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;
    this.mediaRequestToken += 1;
    this.mediaPath = '';
    this.mediaPrimaryUrl = '';
    this.mediaSecondaryUrl = '';
    this.mediaTryingSecondary = false;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.startInput.set('0');
    this.endInput.set('');
    this.durationSeconds.set(null);
    this.mediaUrl.set(null);
    this.videoError.set('');
    this.videoReady.set(false);
    this.mediaDuration.set(null);
    this.currentTime.set(0);
    this.isPlaying.set(false);
    this.fileDurations.set({});
    this.selectionEndSeconds = null;
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

  private videoElement(): HTMLVideoElement | null {
    return this.videoPreview()?.nativeElement ?? null;
  }

  private async refreshPreviewSource(): Promise<void> {
    const path = this.selectedPaths()[0] ?? '';
    if (path === this.mediaPath) {
      return;
    }

    this.mediaPath = path;
    const token = (this.mediaRequestToken += 1);
    this.mediaPrimaryUrl = '';
    this.mediaSecondaryUrl = '';
    this.mediaTryingSecondary = false;
    this.mediaUrl.set(null);
    this.videoError.set('');
    this.videoReady.set(false);
    this.mediaDuration.set(null);
    this.currentTime.set(0);
    this.isPlaying.set(false);
    this.selectionEndSeconds = null;

    if (!path) {
      return;
    }

    const response: MediaSourceResponse = await this.wailsService
      .registerMediaPath(path)
      .catch(() => ({
        success: false,
        url: '',
        message: PREVIEW_UNAVAILABLE_MESSAGE,
      }));
    if (token !== this.mediaRequestToken) {
      return;
    }

    if (response.success && response.url) {
      const preferLoopback = this.shouldPreferLoopback(
        response.url,
        response.fallbackUrl,
      );
      this.mediaPrimaryUrl = preferLoopback
        ? response.fallbackUrl ?? response.url
        : response.url;
      this.mediaSecondaryUrl = preferLoopback
        ? response.url
        : response.fallbackUrl ?? '';
      this.mediaUrl.set(this.mediaPrimaryUrl);
      return;
    }

    this.mediaUrl.set(null);
    this.videoError.set(PREVIEW_UNAVAILABLE_MESSAGE);
  }

  /**
   * Wails dev loads the webview from the Angular dev server, so the relative
   * asset-server path never reaches the Go middleware. Prefer the loopback URL
   * there; production keeps the relative URL and only retries on error.
   */
  private shouldPreferLoopback(
    primaryUrl: string,
    fallbackUrl: string | undefined,
  ): boolean {
    if (!fallbackUrl || /^https?:/i.test(primaryUrl)) {
      return false;
    }

    const host = window.location.hostname;
    const port = window.location.port;
    return (host === 'localhost' || host === '127.0.0.1') && port !== '';
  }

  private applyHandlePosition(handle: TrimHandle, seconds: number): void {
    if (handle === 'start') {
      this.setStartAt(seconds);
      const start = this.startSeconds();
      if (start !== null) {
        this.seekTo(start);
      }
      return;
    }

    this.setEndAt(seconds);
    const end = this.endSeconds();
    if (end !== null) {
      this.seekTo(end);
    }
  }

  private setStartAt(seconds: number): void {
    const duration = this.previewDuration();
    let upper =
      duration !== null
        ? Math.max(0, duration - MIN_TRIM_RANGE_SECONDS)
        : Number.POSITIVE_INFINITY;

    const end = this.endSeconds();
    if (end !== null) {
      upper = Math.min(upper, Math.max(0, end - MIN_TRIM_RANGE_SECONDS));
    }

    const next = this.clamp(seconds, 0, upper);
    this.startInput.set(this.toTimeInput(next));
    this.onTimeChanged();
  }

  private setEndAt(seconds: number): void {
    const duration = this.previewDuration();
    const start = this.startSeconds() ?? 0;
    const upper = duration ?? Number.POSITIVE_INFINITY;
    const lower = Math.min(start + MIN_TRIM_RANGE_SECONDS, upper);
    const next = this.clamp(seconds, lower, upper);
    this.endInput.set(this.toTimeInput(next));
    this.onTimeChanged();
  }

  private seekTo(seconds: number): void {
    const duration = this.previewDuration();
    const target = this.clamp(
      seconds,
      0,
      duration !== null && duration > 0 ? duration : Number.POSITIVE_INFINITY,
    );

    this.currentTime.set(target);
    const video = this.videoElement();
    if (!video) {
      return;
    }

    try {
      video.currentTime = target;
    } catch {
      // Seeking before metadata is safe to ignore; the next update retries.
    }
  }

  private playVideo(video: HTMLVideoElement): void {
    try {
      const result = video.play();
      if (result && typeof result.catch === 'function') {
        result.catch(() => this.isPlaying.set(false));
      }
    } catch {
      this.isPlaying.set(false);
    }
  }

  private pointerTime(event: PointerEvent): number | null {
    const duration = this.previewDuration();
    if (duration === null || duration <= 0) {
      return null;
    }

    const rect = this.timelineTrack()?.nativeElement.getBoundingClientRect();
    if (!rect || rect.width <= 0) {
      return null;
    }

    const ratio = (event.clientX - rect.left) / rect.width;
    return this.clamp(ratio, 0, 1) * duration;
  }

  private shouldIgnoreKey(event: KeyboardEvent): boolean {
    const target = event.target as HTMLElement | null;
    if (!target) {
      return false;
    }

    const tag = target.tagName?.toLowerCase() ?? '';
    if (tag === 'input' || tag === 'select' || tag === 'textarea') {
      return true;
    }
    if (target.isContentEditable) {
      return true;
    }

    const isActivationKey =
      event.key === ' ' || event.key === 'Spacebar' || event.key === 'Enter';
    return isActivationKey && (tag === 'button' || tag === 'a');
  }

  private toTimeInput(seconds: number): string {
    const rounded = Math.round(Math.max(0, seconds) * 10) / 10;
    const tenths = Math.round(rounded * 10);
    const whole = Math.floor(tenths / 10);
    const fraction = tenths % 10;

    const hours = Math.floor(whole / 3600);
    const minutes = Math.floor((whole % 3600) / 60);
    const rest = whole % 60;
    const suffix = fraction > 0 ? `.${fraction}` : '';

    if (hours > 0) {
      return `${hours}:${this.padTwo(minutes)}:${this.padTwo(rest)}${suffix}`;
    }
    return `${minutes}:${this.padTwo(rest)}${suffix}`;
  }

  private padTwo(value: number): string {
    return String(value).padStart(2, '0');
  }

  private clamp(value: number, min: number, max: number): number {
    if (!Number.isFinite(value)) {
      return min;
    }
    return Math.min(Math.max(value, min), max);
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    return {
      toolId: VIDEO_TRIM_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: {
        startTime: this.startSeconds() ?? 0,
        endTime: this.endSeconds() ?? 0,
        targetFormat: this.targetFormat(),
        qualityPreset: this.qualityPreset(),
        trimMode: this.trimMode(),
      },
    };
  }

  private async refreshPlan(): Promise<boolean> {
    if (
      this.selectedPaths().length === 0 ||
      !this.timeRangeValid()
    ) {
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
    // Keep the failed plan so the shared plan card can show the error in
    // context, next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private onTimeChanged(): void {
    if (!this.timeRangeValid()) {
      this.clearPlanRefreshTimer();
      this.planToken += 1;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return;
    }
    this.schedulePlanRefresh();
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, TIME_PLAN_DEBOUNCE_MS);
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
      toolId: VIDEO_TRIM_TOOL_ID,
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

    const response: JobStatusResponse = await this.wailsService
      .getJobStatus(jobId)
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
    const existing = new Map(this.fileItems().map((item) => [item.path, item]));
    this.fileItems.set(
      paths.map((path) => existing.get(path) ?? { path, name: fileName(path) }),
    );

    const knownDurations = this.fileDurations();
    const durations: Record<string, number> = {};
    for (const path of paths) {
      const duration = knownDurations[path];
      if (typeof duration === 'number' && duration > 0) {
        durations[path] = duration;
      }
    }
    this.fileDurations.set(durations);

    if (paths.length !== 1) {
      this.durationSeconds.set(null);
    }

    void this.refreshPreviewSource();

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
      if (generation !== this.fileItemsGeneration) {
        return;
      }
      if (response.success) {
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

        const durations: Record<string, number> = {};
        for (const path of paths) {
          const duration = infos.get(path)?.durationSeconds;
          if (typeof duration === 'number' && duration > 0) {
            durations[path] = duration;
          }
        }
        if (Object.keys(durations).length > 0) {
          this.fileDurations.update((current) => ({
            ...current,
            ...durations,
          }));
        }

        if (paths.length === 1) {
          const duration = infos.get(paths[0])?.durationSeconds;
          this.durationSeconds.set(
            typeof duration === 'number' && duration > 0 ? duration : null,
          );
        }

        // Metadata can arrive before inspection. When that happens, the
        // inspected length is the only duration available for the default end.
        const firstDuration = infos.get(paths[0])?.durationSeconds;
        if (
          this.videoReady() &&
          !this.endInput().trim() &&
          typeof firstDuration === 'number' &&
          firstDuration > 0
        ) {
          this.endInput.set(this.toTimeInput(firstDuration));
          this.onTimeChanged();
        }
      }
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }
  }

  private describeFileInfo(info: FileInfo): string {
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

  private rejectionNotice(rejected: string[]): string {
    const extensions = [
      ...new Set(
        rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
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

  private readInput(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readSelect(event: Event): string {
    return (event.target as HTMLSelectElement | null)?.value ?? '';
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
