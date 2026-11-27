import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
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
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { VideoTargetReport } from '../tool-flow/video-target-report';
import { OpenWindowAction } from '../open-window-action/open-window-action';
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

const VIDEO_CONVERT_TOOL_ID = 'tool.video.convert';
const ADVANCED_PLAN_DEBOUNCE_MS = 200;
const OPTIONS_SCHEMA_VERSION = 1;

const SUPPORTED_VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

const RESOLUTIONS = ['original', '1080p', '720p', '480p'] as const;
type Resolution = (typeof RESOLUTIONS)[number];

const BITRATE_OPTIONS_KBPS = [2000, 5000, 8000, 12000] as const;

const FORMAT_LABELS: Record<VideoPreset['targetFormat'], string> = {
  mp4: 'MP4',
  webm: 'WebM',
};

type TargetFormat = VideoPreset['targetFormat'];
type QualityPreset = VideoPreset['qualityPreset'];

const QUALITY_KEYS: Record<QualityPreset, TranslationKey> = {
  high: 'videoConvert.quality.high',
  medium: 'videoConvert.quality.medium',
  low: 'videoConvert.quality.low',
};

const TARGET_FORMATS: readonly TargetFormat[] = ['mp4', 'webm'];
const QUALITY_PRESETS: readonly QualityPreset[] = ['high', 'medium', 'low'];

type VideoConvertOptions = {
  selectedPresetId: string;
  targetFormatOverride: TargetFormat | null;
  qualityOverride: QualityPreset | null;
  resolution: Resolution;
  bitrateKbps: number | null;
  removeAudio: boolean;
  targetMb: number | null;
};

function defaultOptions(): VideoConvertOptions {
  return {
    selectedPresetId: VIDEO_PRESETS[0].id,
    targetFormatOverride: null,
    qualityOverride: null,
    resolution: 'original',
    bitrateKbps: null,
    removeAudio: false,
    targetMb: null,
  };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<VideoConvertOptions> {
  const result: Partial<VideoConvertOptions> = {};

  const presetId = stored['selectedPresetId'];
  if (
    typeof presetId === 'string' &&
    VIDEO_PRESETS.some((preset) => preset.id === presetId)
  ) {
    result.selectedPresetId = presetId;
  }

  const targetFormat = stored['targetFormatOverride'];
  if (
    targetFormat === null ||
    (typeof targetFormat === 'string' &&
      TARGET_FORMATS.some((value) => value === targetFormat))
  ) {
    result.targetFormatOverride = targetFormat as TargetFormat | null;
  }

  const quality = stored['qualityOverride'];
  if (
    quality === null ||
    (typeof quality === 'string' &&
      QUALITY_PRESETS.some((value) => value === quality))
  ) {
    result.qualityOverride = quality as QualityPreset | null;
  }

  const resolution = stored['resolution'];
  if (
    typeof resolution === 'string' &&
    RESOLUTIONS.some((value) => value === resolution)
  ) {
    result.resolution = resolution as Resolution;
  }

  const bitrate = stored['bitrateKbps'];
  if (bitrate === null) {
    result.bitrateKbps = null;
  } else if (
    typeof bitrate === 'number' &&
    (BITRATE_OPTIONS_KBPS as readonly number[]).includes(bitrate)
  ) {
    result.bitrateKbps = bitrate;
  }

  if (typeof stored['removeAudio'] === 'boolean') {
    result.removeAudio = stored['removeAudio'];
  }

  const targetMb = normalizeTargetMb(stored['targetMb']);
  if (targetMb !== undefined) {
    result.targetMb = targetMb;
  }

  return result;
}

@Component({
  selector: 'app-video-convert',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
    VideoTargetReport,
  ],
  templateUrl: './video-convert.html',
  styleUrl: './video-convert.css',
})
export class VideoConvert implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  /**
   * Shared target-size state. A target overrides `bitrateKbps`, so the field
   * is optional and only lowers the size when the user asks for it.
   */
  readonly target = new VideoTarget(this.i18n);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: VIDEO_CONVERT_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: VIDEO_CONVERT_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_VIDEO_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && this.target.valid(),
    canRun: () => this.canRun(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'videoConvert.error.start',
      planFailed: 'videoConvert.error.plan',
      waiting: 'videoConvert.status.waiting',
      inspectFailed: 'videoConvert.error.inspect',
      statusMissing: 'videoConvert.error.status',
      cancelFailed: 'videoConvert.error.cancel',
      openFileFailed: 'videoConvert.error.openFile',
      revealFailed: 'videoConvert.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly presets = VIDEO_PRESETS;
  readonly destinationPresets = DESTINATION_PRESETS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly selectedPresetId = signal(VIDEO_PRESETS[0].id);
  readonly targetFormatOverride = signal<TargetFormat | null>(null);
  readonly qualityOverride = signal<QualityPreset | null>(null);
  readonly resolution = signal<Resolution>('original');
  readonly bitrateKbps = signal<number | null>(null);
  readonly removeAudio = signal(false);
  readonly outputDir = this.job.outputDir;
  readonly advancedOpen = signal(false);

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

  readonly hasAdvancedOverrides = computed(
    () =>
      this.targetFormatOverride() !== null ||
      this.qualityOverride() !== null ||
      this.resolution() !== 'original' ||
      this.bitrateKbps() !== null ||
      this.removeAudio(),
  );

  readonly targetFormat = computed<TargetFormat>(
    () => this.targetFormatOverride() ?? this.selectedPreset().targetFormat,
  );

  readonly qualityPreset = computed<QualityPreset>(
    () => this.qualityOverride() ?? this.selectedPreset().qualityPreset,
  );

  readonly outputSummary = computed(
    () =>
      this.i18n.t('videoConvert.output.summary', {
        format: FORMAT_LABELS[this.targetFormat()],
        quality: this.i18n.t(QUALITY_KEYS[this.qualityPreset()]),
      }),
  );

  /** Preset labels and hints are id-keyed overlays on the shared config. */
  presetLabel(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoConvert.preset.${preset.id}.label`,
      preset.label,
    );
  }

  presetHint(preset: VideoPreset): string {
    return this.i18n.tOr(
      `videoConvert.preset.${preset.id}.hint`,
      preset.hint,
    );
  }

  /** Preset cards reflect the effective format/quality, even after an override. */
  isPresetActive(preset: VideoPreset): boolean {
    return (
      preset.targetFormat === this.targetFormat() &&
      preset.qualityPreset === this.qualityPreset()
    );
  }

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('videoConvert.action.converting');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('videoConvert.action.convert');
    }
    return count === 1
      ? this.i18n.t('videoConvert.action.convertOne')
      : this.i18n.t('videoConvert.action.convertMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('videoConvert.output.destinationEach'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.target.valid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('videoConvert.helper.addOne');
    }
    if (!this.target.valid()) {
      return this.target.error() ?? '';
    }
    return '';
  });

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/video-convert');
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

  selectPreset(presetId: string): void {
    if (
      presetId === this.selectedPresetId() &&
      !this.hasAdvancedOverrides()
    ) {
      return;
    }
    this.selectedPresetId.set(presetId);
    this.targetFormatOverride.set(null);
    this.qualityOverride.set(null);
    void this.job.refreshPlan();
  }

  onTargetInput(event: Event): void {
    this.target.targetMb.set(parseTargetMb(this.readInput(event)));
    this.actionMessage.set('');
    this.dropBitrateForTarget();
    this.schedulePlan();
  }

  /** A preset writes its exact MB value into the field and re-plans. */
  selectTargetPreset(preset: DestinationPreset): void {
    this.target.selectPreset(preset);
    this.actionMessage.set('');
    this.dropBitrateForTarget();
    this.schedulePlan();
  }

  onTargetFormatChange(event: Event): void {
    const value = this.readSelect(event);
    if (value !== 'mp4' && value !== 'webm') {
      return;
    }
    this.targetFormatOverride.set(
      value === this.selectedPreset().targetFormat ? null : value,
    );
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onQualityChange(event: Event): void {
    const value = this.readSelect(event);
    if (value !== 'high' && value !== 'medium' && value !== 'low') {
      return;
    }
    this.qualityOverride.set(
      value === this.selectedPreset().qualityPreset ? null : value,
    );
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onResolutionChange(event: Event): void {
    const value = this.readSelect(event);
    if (!RESOLUTIONS.includes(value as Resolution)) {
      return;
    }
    this.resolution.set(value as Resolution);
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onBitrateChange(event: Event): void {
    // A target owns the bitrate; the control is disabled while one is set.
    if (this.target.hasTarget()) {
      return;
    }

    const value = this.readSelect(event);
    if (value === '') {
      this.bitrateKbps.set(null);
      this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
      return;
    }

    const kbps = Number(value);
    if (!(BITRATE_OPTIONS_KBPS as readonly number[]).includes(kbps)) {
      return;
    }
    this.bitrateKbps.set(kbps);
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  onRemoveAudioToggle(event: Event): void {
    this.removeAudio.set(this.readChecked(event));
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  resetAdvanced(): void {
    this.targetFormatOverride.set(null);
    this.qualityOverride.set(null);
    this.resolution.set('original');
    this.bitrateKbps.set(null);
    this.removeAudio.set(false);
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
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
  }

  private currentOptions(): VideoConvertOptions {
    return {
      selectedPresetId: this.selectedPresetId(),
      targetFormatOverride: this.targetFormatOverride(),
      qualityOverride: this.qualityOverride(),
      resolution: this.resolution(),
      bitrateKbps: this.bitrateKbps(),
      removeAudio: this.removeAudio(),
      targetMb: this.target.targetMb(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.selectedPresetId.set(options.selectedPresetId);
    this.targetFormatOverride.set(options.targetFormatOverride);
    this.qualityOverride.set(options.qualityOverride);
    this.resolution.set(options.resolution);
    this.bitrateKbps.set(
      options.targetMb !== null && options.targetMb > 0
        ? null
        : options.bitrateKbps,
    );
    this.removeAudio.set(options.removeAudio);
    this.target.restore(options.targetMb);
  }

  /**
   * A target computes the video bitrate, so a remembered or chosen explicit
   * bitrate cannot coexist with it. Clearing the target leaves Auto in place.
   */
  private dropBitrateForTarget(): void {
    if (this.target.hasTarget()) {
      this.bitrateKbps.set(null);
    }
  }

  /** An unusable target cannot be planned, so the old plan is dropped now. */
  private schedulePlan(): void {
    if (this.selectedPaths().length === 0 || !this.target.valid()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(ADVANCED_PLAN_DEBOUNCE_MS);
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const options: Record<string, unknown> = {
      targetFormat: this.targetFormat(),
      qualityPreset: this.qualityPreset(),
      resolution: this.resolution(),
    };
    const targetSizeBytes = this.target.sizeBytes();
    if (targetSizeBytes > 0) {
      options['targetSizeBytes'] = targetSizeBytes;
    }
    const bitrateKbps = this.bitrateKbps();
    if (bitrateKbps !== null && targetSizeBytes === 0) {
      options['bitrateKbps'] = bitrateKbps;
    }
    if (this.removeAudio()) {
      options['removeAudio'] = true;
    }

    return {
      toolId: VIDEO_CONVERT_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
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

  private rejectionNotice(rejected: readonly string[]): string {
    const extensions = [
      ...new Set(
        rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('videoConvert.notice.filesOne')
        : this.i18n.t('videoConvert.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('videoConvert.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('videoConvert.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readSelect(event: Event): string {
    return (event.target as HTMLSelectElement | null)?.value ?? '';
  }

  private readInput(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readChecked(event: Event): boolean {
    return !!(event.target as HTMLInputElement | null)?.checked;
  }
}
