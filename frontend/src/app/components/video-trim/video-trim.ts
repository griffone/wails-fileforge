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
import { VIDEO_PRESETS, VideoPreset } from '../../config/video-presets';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  JobRequest,
  MediaSourceResponse,
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
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';
import type { TranslationKey } from '../../i18n';

const VIDEO_TRIM_TOOL_ID = 'tool.video.trim';
const TIME_PLAN_DEBOUNCE_MS = 200;
const OPTIONS_SCHEMA_VERSION = 1;
const MIN_TRIM_RANGE_SECONDS = 0.1;
const SMALL_NUDGE_SECONDS = 0.1;
const LARGE_NUDGE_SECONDS = 1;

const SUPPORTED_VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

const FORMAT_LABELS: Record<VideoPreset['targetFormat'], string> = {
  mp4: 'MP4',
  webm: 'WebM',
};

type TargetFormat = VideoPreset['targetFormat'];
type QualityPreset = VideoPreset['qualityPreset'];
type TrimMode = 'auto' | 'copy' | 'reencode';
type TrimHandle = 'start' | 'end';

const QUALITY_KEYS: Record<QualityPreset, TranslationKey> = {
  high: 'videoTrim.quality.high',
  medium: 'videoTrim.quality.medium',
  low: 'videoTrim.quality.low',
};

const TRIM_MODE_HELP_KEYS: Record<TrimMode, TranslationKey> = {
  auto: 'videoTrim.advanced.trimModeHelpAuto',
  copy: 'videoTrim.advanced.trimModeHelpCopy',
  reencode: 'videoTrim.advanced.trimModeHelpReencode',
};

type VideoTrimOptions = {
  selectedPresetId: string;
  trimMode: TrimMode;
};

function defaultOptions(): VideoTrimOptions {
  return { selectedPresetId: VIDEO_PRESETS[0].id, trimMode: 'auto' };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<VideoTrimOptions> {
  const result: Partial<VideoTrimOptions> = {};

  const presetId = stored['selectedPresetId'];
  if (
    typeof presetId === 'string' &&
    VIDEO_PRESETS.some((preset) => preset.id === presetId)
  ) {
    result.selectedPresetId = presetId;
  }

  const trimMode = stored['trimMode'];
  if (trimMode === 'auto' || trimMode === 'copy' || trimMode === 'reencode') {
    result.trimMode = trimMode;
  }

  return result;
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
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: VIDEO_TRIM_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: VIDEO_TRIM_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_VIDEO_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && this.timeRangeValid(),
    canRun: () => this.canRun(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (_previous, paths) => {
      this.syncDurations(paths);
      void this.refreshPreviewSource();
      void this.job.refreshPlan();
    },
    onFilesInspected: (infos) => this.applyInspectedDurations(infos),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'videoTrim.error.start',
      planFailed: 'videoTrim.error.plan',
      waiting: 'videoTrim.status.waiting',
      inspectFailed: 'videoTrim.error.inspect',
      statusMissing: 'videoTrim.error.status',
      cancelFailed: 'videoTrim.error.cancel',
      openFileFailed: 'videoTrim.error.openFile',
      revealFailed: 'videoTrim.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly playIcon = Play;
  readonly pauseIcon = Pause;
  readonly videoOffIcon = VideoOff;

  readonly presets = VIDEO_PRESETS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly selectedPresetId = signal(VIDEO_PRESETS[0].id);
  readonly trimMode = signal<TrimMode>('auto');
  readonly outputDir = this.job.outputDir;
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
      this.i18n.t('videoTrim.output.summary', {
        format: FORMAT_LABELS[this.targetFormat()],
        quality: this.i18n.t(QUALITY_KEYS[this.qualityPreset()]),
      }),
  );

  /** Preset labels and hints are id-keyed overlays on the shared config. */
  presetLabel(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoTrim.preset.${preset.id}.label`,
      preset.label,
    );
  }

  presetHint(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoTrim.preset.${preset.id}.hint`,
      preset.hint,
    );
  }

  readonly startSeconds = computed(() =>
    parseTimeToSeconds(this.startInput()),
  );
  readonly endSeconds = computed(() => parseTimeToSeconds(this.endInput()));

  readonly timeError = computed(() => {
    const start = this.startSeconds();
    const end = this.endSeconds();
    const endText = this.endInput().trim();

    if (!endText) {
      return start === null
        ? this.i18n.t('videoTrim.time.errorInvalid')
        : '';
    }
    if (start === null || end === null) {
      return this.i18n.t('videoTrim.time.errorInvalid');
    }
    if (end <= start) {
      return this.i18n.t('videoTrim.time.errorEndAfterStart');
    }

    const duration = this.durationSeconds();
    if (duration !== null && start >= duration) {
      return this.i18n.t('videoTrim.time.errorStartBeforeEnd');
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
        return this.i18n.t('videoTrim.time.batchWarning', {
          name: fileName(path),
          duration: formatDuration(duration),
        });
      }
    }

    return '';
  });

  readonly isBatch = computed(() => this.selectedPaths().length > 1);

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('videoTrim.action.trimming');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('videoTrim.action.trim');
    }
    return count === 1
      ? this.i18n.t('videoTrim.action.trimOne')
      : this.i18n.t('videoTrim.action.trimMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('videoTrim.output.destinationEach'),
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
      return this.i18n.t('videoTrim.helper.addOne');
    }
    if (!this.endInput().trim()) {
      return this.i18n.t('videoTrim.helper.setEnd');
    }
    return '';
  });

  readonly trimModeHelp = computed(() =>
    this.i18n.t(TRIM_MODE_HELP_KEYS[this.trimMode()]),
  );

  private mediaRequestToken = 0;
  private mediaPath = '';
  private mediaPrimaryUrl = '';
  private mediaSecondaryUrl = '';
  private mediaTryingSecondary = false;
  private dragHandle: TrimHandle | null = null;
  private dragCleanup: (() => void) | null = null;
  private selectionEndSeconds: number | null = null;

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/video-trim');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.endHandleDrag();
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.videoOne'),
      this.i18n.t('common.videoMany'),
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
    if (presetId === this.selectedPresetId()) {
      return;
    }
    this.selectedPresetId.set(presetId);
    void this.job.refreshPlan();
  }

  onTrimModeChange(event: Event): void {
    const value = this.readSelect(event);
    if (value !== 'auto' && value !== 'copy' && value !== 'reencode') {
      return;
    }
    this.trimMode.set(value);
    this.job.schedulePlanRefresh(TIME_PLAN_DEBOUNCE_MS);
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
    this.videoError.set(
      this.i18n.t('videoTrim.preview.videoUnavailable'),
    );
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
    this.endHandleDrag();
    this.mediaRequestToken += 1;
    this.mediaPath = '';
    this.mediaPrimaryUrl = '';
    this.mediaSecondaryUrl = '';
    this.mediaTryingSecondary = false;

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
  }

  private currentOptions(): VideoTrimOptions {
    return {
      selectedPresetId: this.selectedPresetId(),
      trimMode: this.trimMode(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.selectedPresetId.set(options.selectedPresetId);
    this.trimMode.set(options.trimMode);
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
        message: this.i18n.t('videoTrim.preview.videoUnavailable'),
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
    this.videoError.set(
      this.i18n.t('videoTrim.preview.videoUnavailable'),
    );
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

  private onTimeChanged(): void {
    if (!this.timeRangeValid()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(TIME_PLAN_DEBOUNCE_MS);
  }

  /** Drops durations for paths no longer queued, as the queue does. */
  private syncDurations(paths: readonly string[]): void {
    const known = this.fileDurations();
    const durations: Record<string, number> = {};
    for (const path of paths) {
      const duration = known[path];
      if (typeof duration === 'number' && duration > 0) {
        durations[path] = duration;
      }
    }
    this.fileDurations.set(durations);

    if (paths.length !== 1) {
      this.durationSeconds.set(null);
    }
  }

  private applyInspectedDurations(infos: FileInfo[]): void {
    const byPath = new Map(infos.map((info) => [info.path, info]));
    const paths = this.selectedPaths();

    const durations: Record<string, number> = {};
    for (const path of paths) {
      const duration = byPath.get(path)?.durationSeconds;
      if (typeof duration === 'number' && duration > 0) {
        durations[path] = duration;
      }
    }
    if (Object.keys(durations).length > 0) {
      this.fileDurations.update((current) => ({ ...current, ...durations }));
    }

    if (paths.length === 1) {
      const duration = byPath.get(paths[0])?.durationSeconds;
      this.durationSeconds.set(
        typeof duration === 'number' && duration > 0 ? duration : null,
      );
    }

    // Metadata can arrive before inspection. When that happens, the inspected
    // length is the only duration available for the default end.
    const firstDuration = byPath.get(paths[0])?.durationSeconds;
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

  private rejectionNotice(rejected: readonly string[]): string {
    const extensions = [
      ...new Set(
        rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('videoTrim.notice.filesOne')
        : this.i18n.t('videoTrim.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('videoTrim.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('videoTrim.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readInput(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readSelect(event: Event): string {
    return (event.target as HTMLSelectElement | null)?.value ?? '';
  }
}
