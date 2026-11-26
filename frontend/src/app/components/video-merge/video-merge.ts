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
import { FileQueue, FileQueueReorder } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { VideoTargetReport } from '../tool-flow/video-target-report';
import {
  DESTINATION_PRESETS,
  DestinationPreset,
} from '../../config/destination-presets';
import { VIDEO_PRESETS, VideoPreset } from '../../config/video-presets';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
  formatDuration,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';
import {
  VideoTarget,
  normalizeTargetMb,
  parseTargetMb,
} from '../../utils/video-target';
import type { TranslationKey } from '../../i18n';

const VIDEO_MERGE_TOOL_ID = 'tool.video.merge';
const PLAN_DEBOUNCE_MS = 200;
const OPTIONS_SCHEMA_VERSION = 1;

const CROSSFADE_DURATION_DEFAULT = 0.5;
const CROSSFADE_DURATION_MIN = 0.1;
const CROSSFADE_DURATION_MAX = 3.0;

type VideoTransition = 'none' | 'crossfade';
type MergeMode = 'auto' | 'copy' | 'reencode';

const MERGE_MODES: readonly MergeMode[] = ['auto', 'copy', 'reencode'];

const MERGE_MODE_HELP_KEYS: Record<MergeMode, TranslationKey> = {
  auto: 'videoMerge.advanced.mergeModeHelpAuto',
  copy: 'videoMerge.advanced.mergeModeHelpCopy',
  reencode: 'videoMerge.advanced.mergeModeHelpReencode',
};

const SUPPORTED_VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

type VideoMergeOptions = {
  selectedPresetId: string;
  mergeMode: MergeMode;
  removeAudio: boolean;
  transition: VideoTransition;
  transitionDuration: string;
  targetMb: number | null;
};

function defaultOptions(): VideoMergeOptions {
  return {
    selectedPresetId: VIDEO_PRESETS[0].id,
    mergeMode: 'auto',
    removeAudio: false,
    transition: 'none',
    transitionDuration: CROSSFADE_DURATION_DEFAULT.toFixed(1),
    targetMb: null,
  };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<VideoMergeOptions> {
  const result: Partial<VideoMergeOptions> = {};

  const presetId = stored['selectedPresetId'];
  if (
    typeof presetId === 'string' &&
    VIDEO_PRESETS.some((preset) => preset.id === presetId)
  ) {
    result.selectedPresetId = presetId;
  }

  const mergeMode = stored['mergeMode'];
  if (
    typeof mergeMode === 'string' &&
    MERGE_MODES.some((mode) => mode === mergeMode)
  ) {
    result.mergeMode = mergeMode as MergeMode;
  }

  if (typeof stored['removeAudio'] === 'boolean') {
    result.removeAudio = stored['removeAudio'];
  }

  const transition = stored['transition'];
  if (transition === 'none' || transition === 'crossfade') {
    result.transition = transition;
  }

  const duration = stored['transitionDuration'];
  if (typeof duration === 'string') {
    result.transitionDuration = duration;
  }

  const targetMb = normalizeTargetMb(stored['targetMb']);
  if (targetMb !== undefined) {
    result.targetMb = targetMb;
  }

  return result;
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
    VideoTargetReport,
  ],
  templateUrl: './video-merge.html',
  styleUrl: './video-merge.css',
})
export class VideoMerge implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  /** Shared target-size state; a target forces a re-encode. */
  readonly target = new VideoTarget(this.i18n);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: VIDEO_MERGE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: VIDEO_MERGE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_VIDEO_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () =>
      this.selectedPaths().length >= 2 && this.target.valid(),
    canRun: () => this.canRun(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    messages: {
      start: 'videoMerge.error.start',
      planFailed: 'videoMerge.error.plan',
      waiting: 'videoMerge.status.waiting',
      inspectFailed: 'videoMerge.error.inspect',
      statusMissing: 'videoMerge.error.status',
      cancelFailed: 'videoMerge.error.cancel',
      openFileFailed: 'videoMerge.error.openFile',
      revealFailed: 'videoMerge.error.openFolder',
    },
    notice: {
      rejectedOne: 'videoMerge.notice.rejectedOne',
      rejectedMany: 'videoMerge.notice.rejectedMany',
      duplicateOne: 'videoMerge.notice.duplicateOne',
      duplicateMany: 'videoMerge.notice.duplicateMany',
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

  readonly presets = VIDEO_PRESETS;
  readonly destinationPresets = DESTINATION_PRESETS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly selectedPresetId = signal(VIDEO_PRESETS[0].id);
  readonly mergeMode = signal<MergeMode>('auto');
  readonly removeAudio = signal(false);
  readonly transition = signal<VideoTransition>('none');
  readonly transitionDuration = signal(
    CROSSFADE_DURATION_DEFAULT.toFixed(1),
  );
  readonly outputDir = this.job.outputDir;

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

  readonly isCrossfade = computed(() => this.transition() === 'crossfade');

  /** Preset labels and hints are id-keyed overlays on the shared config. */
  presetLabel(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoMerge.preset.${preset.id}.label`,
      preset.label,
    );
  }

  presetHint(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoMerge.preset.${preset.id}.hint`,
      preset.hint,
    );
  }

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
      this.target.valid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('videoMerge.action.merging');
    }
    const count = this.selectedPaths().length;
    return count >= 2
      ? this.i18n.t('videoMerge.action.mergeMany', { count })
      : this.i18n.t('videoMerge.action.merge');
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('videoMerge.output.destinationFirst'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly actionHelper = computed(() => {
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('videoMerge.helper.addTwo');
    }
    if (count === 1) {
      return this.i18n.t('videoMerge.helper.addOneMore');
    }
    if (!this.target.valid()) {
      return this.target.error() ?? '';
    }
    return '';
  });

  /**
   * Copy and auto copy the source streams, so a byte target always plans and
   * runs as a two-pass re-encode. The select keeps the user's own choice for
   * when the target is cleared.
   */
  readonly effectiveMergeMode = computed<MergeMode>(() =>
    this.target.hasTarget() ? 'reencode' : this.mergeMode(),
  );

  readonly mergeModeHelp = computed(() =>
    this.target.hasTarget()
      ? this.i18n.t('videoTarget.mode.forced')
      : this.i18n.t(MERGE_MODE_HELP_KEYS[this.mergeMode()]),
  );

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/video-merge');
    this.job.resume();
  }

  ngOnDestroy(): void {
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

  moveUp(path: string): void {
    this.job.movePath(path, -1);
  }

  moveDown(path: string): void {
    this.job.movePath(path, 1);
  }

  onReorder(event: FileQueueReorder): void {
    this.job.reorderPath(event.path, event.to);
  }

  selectPreset(presetId: string): void {
    if (presetId === this.selectedPresetId()) {
      return;
    }
    this.selectedPresetId.set(presetId);
    void this.job.refreshPlan();
  }

  onMergeModeChange(event: Event): void {
    // A target forces the re-encode, and the select is disabled while it is set.
    if (this.target.hasTarget()) {
      return;
    }

    const value = (event.target as HTMLSelectElement | null)?.value;
    if (!MERGE_MODES.includes(value as MergeMode)) {
      return;
    }
    this.mergeMode.set(value as MergeMode);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onTargetInput(event: Event): void {
    const raw = (event.target as HTMLInputElement | null)?.value ?? '';
    this.target.targetMb.set(parseTargetMb(raw));
    this.actionMessage.set('');
    this.schedulePlan();
  }

  /** A preset writes its exact MB value into the field and re-plans. */
  selectTargetPreset(preset: DestinationPreset): void {
    this.target.selectPreset(preset);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onRemoveAudioToggle(event: Event): void {
    this.removeAudio.set(!!(event.target as HTMLInputElement | null)?.checked);
    void this.job.refreshPlan();
  }

  onTransitionChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value;
    this.transition.set(value === 'crossfade' ? 'crossfade' : 'none');
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onTransitionDurationInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.transitionDuration.set(value);
    this.actionMessage.set('');
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

  useFirstVideoFolder(): void {
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

  /** A failed merge has no partial queue to retry: rerun the same inputs. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.job.clearRunOutcome();

    const planned = await this.job.refreshPlan();
    if (!planned) {
      return;
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
  }

  private currentOptions(): VideoMergeOptions {
    return {
      selectedPresetId: this.selectedPresetId(),
      mergeMode: this.mergeMode(),
      removeAudio: this.removeAudio(),
      transition: this.transition(),
      transitionDuration: this.transitionDuration(),
      targetMb: this.target.targetMb(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.selectedPresetId.set(options.selectedPresetId);
    this.mergeMode.set(options.mergeMode);
    this.removeAudio.set(options.removeAudio);
    this.transition.set(options.transition);
    this.transitionDuration.set(options.transitionDuration);
    this.target.restore(options.targetMb);
  }

  /** An unusable target cannot be planned, so the old plan is dropped now. */
  private schedulePlan(): void {
    if (this.selectedPaths().length < 2 || !this.target.valid()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  private buildRequest(): JobRequest {
    const preset = this.selectedPreset();
    const options: Record<string, unknown> = {
      targetFormat: preset.targetFormat,
      qualityPreset: preset.qualityPreset,
      mergeMode: this.effectiveMergeMode(),
    };
    const targetSizeBytes = this.target.sizeBytes();
    if (targetSizeBytes > 0) {
      options['targetSizeBytes'] = targetSizeBytes;
    }
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
}
