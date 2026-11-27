import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  FolderOpen,
  ImageUpscale as ImageUpscaleIcon,
  LucideAngularModule,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { ToolchainStateService } from '../../services/toolchain-state.service';
import {
  FileInfo,
  JobRequest,
  ToolchainComponent,
  ToolchainInstallOptions,
  ToolchainStatus,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const UPSCALE_TOOL_ID = 'tool.image.upscale';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;
const PACK_INSTALL_POLL_MS = 500;
const PACK_INSTALL_TIMEOUT_MS = 10 * 60 * 1000;
const DEFAULT_OCR_LANGUAGES = ['eng', 'spa'];

const SCALE_OPTIONS = [2, 4] as const;
const TILE_OPTIONS = [64, 128, 256, 512] as const;
const FORMAT_OPTIONS = ['png', 'jpeg', 'webp'] as const;

const SUPPORTED_IMAGE_EXTENSIONS = new Set([
  'jpg',
  'jpeg',
  'png',
  'webp',
  'bmp',
  'tiff',
  'tif',
  'gif',
]);

type UpscaleScale = (typeof SCALE_OPTIONS)[number];
type UpscaleTile = (typeof TILE_OPTIONS)[number];
type UpscaleFormat = (typeof FORMAT_OPTIONS)[number];
type UpscaleModel = 'general' | 'anime';
type CapabilityStatus = 'loading' | 'ok' | 'error';

export interface PackBanner {
  title: string;
  detail: string;
}

export interface PackComponentState {
  id: string;
  label: string;
  ready: boolean;
  detail: string;
}

type ImageUpscaleOptions = {
  scale: UpscaleScale;
  model: UpscaleModel;
  tileSize: UpscaleTile;
  format: UpscaleFormat;
  quality: number;
};

function defaultOptions(): ImageUpscaleOptions {
  return {
    scale: 2,
    model: 'general',
    tileSize: 128,
    format: 'png',
    quality: 90,
  };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageUpscaleOptions> {
  const result: Partial<ImageUpscaleOptions> = {};

  const scale = Number(stored['scale']);
  if (SCALE_OPTIONS.some((option) => option === scale)) {
    result.scale = scale as UpscaleScale;
  }

  const model = stored['model'];
  if (model === 'general' || model === 'anime') {
    result.model = model;
  }

  const tileSize = Number(stored['tileSize']);
  if (TILE_OPTIONS.some((option) => option === tileSize)) {
    result.tileSize = tileSize as UpscaleTile;
  }

  const format = stored['format'];
  if (FORMAT_OPTIONS.some((option) => option === format)) {
    result.format = format as UpscaleFormat;
  }

  const quality = Number(stored['quality']);
  if (Number.isFinite(quality) && quality >= 1 && quality <= 100) {
    result.quality = Math.round(quality);
  }

  return result;
}

@Component({
  selector: 'app-image-upscale',
  standalone: true,
  imports: [
    RouterLink,
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-upscale.html',
  styleUrl: './image-upscale.css',
})
export class ImageUpscale implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  /** Shared toolchain state: the page installs and repairs the pack inline. */
  private readonly pack = inject(ToolchainStateService);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: UPSCALE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: UPSCALE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () =>
      this.selectedPaths().length > 0 &&
      this.packReady() &&
      this.optionsValid(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () => this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'imageUpscale.error.start',
      planFailed: 'imageUpscale.error.plan',
      waiting: 'imageUpscale.status.waiting',
      inspectFailed: 'imageUpscale.error.inspect',
      statusMissing: 'imageUpscale.error.status',
      cancelFailed: 'imageUpscale.error.cancel',
      openFileFailed: 'imageUpscale.error.openFile',
      revealFailed: 'imageUpscale.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
    onRunAgainAdopted: (request) => this.applyRequestOptions(request),
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly recheckIcon = RefreshCw;
  readonly upscaleIcon = ImageUpscaleIcon;

  readonly scaleOptions = SCALE_OPTIONS;
  readonly tileOptions = TILE_OPTIONS;
  readonly formatOptions = FORMAT_OPTIONS;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;

  readonly scale = signal<UpscaleScale>(2);
  readonly model = signal<UpscaleModel>('general');
  readonly tileSize = signal<UpscaleTile>(128);
  readonly format = signal<UpscaleFormat>('png');
  readonly quality = signal(90);
  readonly outputDir = this.job.outputDir;

  readonly toolchain = this.pack.status;
  readonly toolchainEvent = this.pack.event;
  readonly toolchainChecking = signal(false);
  private readonly toolchainChecked = signal(false);

  /** Progress of an install started from this page. */
  private readonly packInstallRequested = signal(false);
  private readonly packInstallFailed = signal('');

  readonly toolchainState = computed<CapabilityStatus>(() => {
    if (!this.toolchainChecked()) {
      return 'loading';
    }
    return this.toolchain()?.success ? 'ok' : 'error';
  });

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

  readonly animeModelPath = this.preferences.upscaleAnimeModelPath;

  readonly packInstalling = computed(
    () =>
      this.toolchain()?.installing === true || this.packInstallRequested(),
  );

  readonly packCancelling = computed(
    () =>
      this.toolchain()?.cancelling === true ||
      (this.packInstallRequested() &&
        this.toolchainEvent()?.phase === 'cancelling'),
  );

  readonly packPercent = computed(() => {
    const status = this.toolchain();
    const event = this.toolchainEvent();
    const raw =
      status?.installing && event && !event.done
        ? event.percent
        : (status?.percent ?? 0);
    if (!Number.isFinite(raw)) {
      return 0;
    }
    return Math.min(100, Math.max(0, Math.round(raw)));
  });

  readonly packProgressMessage = computed(() => {
    const event = this.toolchainEvent();
    if (event?.message) {
      return event.message;
    }
    const status = this.toolchain();
    return status?.detail || status?.message || '';
  });

  readonly packInstallError = computed(
    () =>
      this.packInstallFailed().trim() ||
      (this.toolchain()?.error ?? '').trim() ||
      (this.toolchainEvent()?.error ?? '').trim(),
  );

  readonly packComponents = computed<PackComponentState[]>(() => {
    const components = this.toolchain()?.components ?? [];
    const resolve = (
      id: string,
      fallbackLabel: string,
    ): PackComponentState => {
      const component: ToolchainComponent | undefined = components.find(
        (candidate) => candidate.id === id,
      );
      return {
        id,
        label: component?.label?.trim() || fallbackLabel,
        ready: component?.ready === true,
        detail: component?.detail?.trim() ?? '',
      };
    };

    const list = [
      resolve('upscale', this.i18n.t('imageUpscale.pack.engineFallback')),
    ];
    if (this.model() === 'general') {
      list.push(
        resolve('upscale-model', this.i18n.t('imageUpscale.pack.modelFallback')),
      );
    } else if (!this.animeModelConfigured()) {
      // A custom override replaces the managed illustration model, so the
      // managed file is not a requirement in that case.
      list.push(
        resolve(
          'upscale-anime-model',
          this.i18n.t('imageUpscale.pack.illustrationModelFallback'),
        ),
      );
    }
    return list;
  });

  readonly engineReady = computed(
    () =>
      (this.toolchain()?.components ?? []).find(
        (component) => component.id === 'upscale',
      )?.ready === true,
  );

  readonly managedModelReady = computed(
    () =>
      (this.toolchain()?.components ?? []).find(
        (component) => component.id === 'upscale-model',
      )?.ready === true,
  );

  readonly animeModelConfigured = computed(
    () => this.animeModelPath().trim().length > 0,
  );

  readonly illustrationModelReady = computed(
    () =>
      (this.toolchain()?.components ?? []).find(
        (component) => component.id === 'upscale-anime-model',
      )?.ready === true,
  );

  readonly modelReady = computed(() =>
    this.model() === 'general'
      ? this.managedModelReady()
      : this.animeModelConfigured() || this.illustrationModelReady(),
  );

  readonly missingPackComponents = computed(() =>
    this.packComponents().filter((component) => !component.ready),
  );

  /** Ready only when the engine and the chosen model are verified. */
  readonly packReady = computed(
    () => this.engineReady() && this.modelReady(),
  );

  readonly packBanner = computed<PackBanner | null>(() => {
    if (this.packReady() || this.toolchainState() === 'loading') {
      return null;
    }

    if (this.toolchainState() === 'error') {
      return {
        title: this.i18n.t('imageUpscale.pack.checkErrorTitle'),
        detail: this.i18n.t('imageUpscale.pack.checkErrorDetail'),
      };
    }

    const anyReady = this.packComponents().some(
      (component) => component.ready,
    );
    return {
      title: anyReady
        ? this.i18n.t('imageUpscale.pack.needsRepairTitle')
        : this.i18n.t('imageUpscale.pack.missingTitle'),
      detail: this.i18n.t('imageUpscale.pack.detail', {
        size: this.i18n.t('imageUpscale.pack.size'),
      }),
    };
  });

  readonly packBadge = computed(() => {
    if (this.toolchainState() === 'loading' || this.toolchainChecking()) {
      return this.i18n.t('imageUpscale.pack.badge.checking');
    }
    return this.packReady()
      ? this.i18n.t('imageUpscale.pack.badge.ready')
      : this.i18n.t('imageUpscale.pack.badge.missing');
  });

  readonly qualityEnabled = computed(() => this.format() !== 'png');

  readonly normalizedQuality = computed(() =>
    Math.min(100, Math.max(1, Math.round(this.quality()))),
  );

  readonly optionsValid = computed(
    () =>
      !this.qualityEnabled() ||
      (Number.isFinite(this.quality()) &&
        this.quality() >= 1 &&
        this.quality() <= 100),
  );

  readonly scaleLabel = computed(() => `${this.scale()}x`);

  readonly modelLabel = computed(() =>
    this.model() === 'anime'
      ? this.i18n.t('imageUpscale.model.anime')
      : this.i18n.t('imageUpscale.model.general'),
  );

  readonly formatLabel = computed(() =>
    this.format() === 'jpeg'
      ? 'JPEG'
      : this.format() === 'webp'
        ? 'WebP'
        : 'PNG',
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageUpscale.action.upscaling');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageUpscale.action.upscale');
    }
    return count === 1
      ? this.i18n.t('imageUpscale.action.upscaleOne')
      : this.i18n.t('imageUpscale.action.upscaleMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageUpscale.destination.original'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.packReady() &&
      this.optionsValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('imageUpscale.helper.addImage');
    }
    if (this.toolchainState() === 'loading' || this.toolchainChecking()) {
      return this.i18n.t('imageUpscale.helper.checking');
    }
    if (!this.engineReady() || !this.modelReady()) {
      return this.model() === 'anime' && this.engineReady()
        ? this.i18n.t('imageUpscale.helper.anime')
        : this.i18n.t('imageUpscale.helper.notReady');
    }
    return '';
  });

  /**
   * The tool reports what it produced per image (`4x upscale, W×H`). Show the
   * first successful message above the shared result card.
   */
  readonly processedSummary = computed<string | null>(() => {
    const job = this.terminalJob();
    if (!job) {
      return null;
    }

    const item = (job.items ?? []).find(
      (candidate) => candidate.success && candidate.message?.trim(),
    );
    return item ? item.message.trim() : null;
  });

  ngOnInit(): void {
    this.restoreOptions();
    void this.refreshToolchain();

    this.job.takePendingDrop('/image-upscale');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
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

  onScaleChange(event: Event): void {
    const value = Number(
      (event.target as HTMLSelectElement | null)?.value ?? '',
    );
    if (!SCALE_OPTIONS.some((option) => option === value)) {
      return;
    }
    this.scale.set(value as UpscaleScale);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onModelChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (value !== 'general' && value !== 'anime') {
      return;
    }
    if (value === this.model()) {
      return;
    }
    this.model.set(value);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onTileChange(event: Event): void {
    const value = Number(
      (event.target as HTMLSelectElement | null)?.value ?? '',
    );
    if (!TILE_OPTIONS.some((option) => option === value)) {
      return;
    }
    this.tileSize.set(value as UpscaleTile);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onFormatChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (!FORMAT_OPTIONS.some((option) => option === value)) {
      return;
    }
    this.format.set(value as UpscaleFormat);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onQualityInput(event: Event): void {
    const raw = (event.target as HTMLInputElement | null)?.value ?? '';
    const value = Number(raw);
    this.quality.set(Number.isFinite(value) ? Math.round(value) : 0);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  /**
   * Reloads the toolchain snapshot through the shared state service. The pack
   * is part of the standard install; the page can install or repair it inline.
   */
  async refreshToolchain(): Promise<void> {
    this.toolchainChecking.set(true);
    try {
      await this.pack.refresh();
    } finally {
      this.toolchainChecking.set(false);
      this.toolchainChecked.set(true);
    }

    if (this.selectedPaths().length === 0) {
      return;
    }
    if (this.packReady()) {
      this.schedulePlan();
    } else {
      this.job.clearPlan();
    }
  }

  /**
   * Installs or repairs the pack from this page. It names the model download
   * size, shows live progress and re-plans once the engine and models are in
   * place.
   */
  async installPack(): Promise<void> {
    if (this.packInstalling()) {
      return;
    }

    this.packInstallFailed.set('');
    this.packInstallRequested.set(true);

    const status = this.toolchain();
    const response = await this.pack.install(this.packInstallOptions(status));
    if (!response) {
      this.packInstallRequested.set(false);
      this.packInstallFailed.set(
        this.i18n.t('imageUpscale.pack.installFailed'),
      );
      return;
    }

    if (!response.installing) {
      await this.finishPackInstall();
      return;
    }

    void this.awaitPackInstall();
  }

  async cancelPackInstall(): Promise<void> {
    await this.pack.cancel();
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

  private packInstallOptions(status: ToolchainStatus | null): ToolchainInstallOptions {
    return {
      model: status?.model || 'medium',
      ocrLanguages:
        status?.ocrLanguages && status.ocrLanguages.length > 0
          ? [...status.ocrLanguages]
          : [...DEFAULT_OCR_LANGUAGES],
    };
  }

  /**
   * Waits for the background install to settle. The service polls the installer
   * while it runs, so this only watches the shared status; the bound stops a
   * lost status from leaving the page stuck in a "installing" state.
   */
  private async awaitPackInstall(): Promise<void> {
    const deadline = Date.now() + PACK_INSTALL_TIMEOUT_MS;
    while (this.toolchain()?.installing && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, PACK_INSTALL_POLL_MS));
    }
    await this.finishPackInstall();
  }

  private async finishPackInstall(): Promise<void> {
    this.packInstallRequested.set(false);
    await this.refreshToolchain();
    if (this.selectedPaths().length > 0 && this.packReady()) {
      this.schedulePlan();
    }
  }

  private currentOptions(): ImageUpscaleOptions {
    return {
      scale: this.scale(),
      model: this.model(),
      tileSize: this.tileSize(),
      format: this.format(),
      quality: this.normalizedQuality(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.scale.set(options.scale);
    this.model.set(options.model);
    this.tileSize.set(options.tileSize);
    this.format.set(options.format);
    this.quality.set(options.quality);
  }

  /** Applies the options of a request the result card re-ran. */
  private applyRequestOptions(request: JobRequest): void {
    const options = request.options ?? {};
    const scale = Number(options['scale']);
    if (SCALE_OPTIONS.some((option) => option === scale)) {
      this.scale.set(scale as UpscaleScale);
    }
    const model = options['model'];
    if (model === 'general' || model === 'anime') {
      this.model.set(model);
    }
    const tileSize = Number(options['tileSize']);
    if (TILE_OPTIONS.some((option) => option === tileSize)) {
      this.tileSize.set(tileSize as UpscaleTile);
    }
    const format = options['format'];
    if (FORMAT_OPTIONS.some((option) => option === format)) {
      this.format.set(format as UpscaleFormat);
    }
    const quality = Number(options['quality']);
    if (Number.isFinite(quality) && quality > 0) {
      this.quality.set(Math.round(quality));
    }
  }

  /**
   * Drops the plan at once when the page cannot plan (no inputs, pack not ready
   * or invalid options) instead of leaving a stale plan on screen.
   */
  private schedulePlan(): void {
    if (
      this.selectedPaths().length === 0 ||
      !this.packReady() ||
      !this.optionsValid()
    ) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {
      scale: this.scale(),
      model: this.model(),
      tileSize: this.tileSize(),
      format: this.format(),
    };
    if (this.qualityEnabled()) {
      options['quality'] = this.normalizedQuality();
    }
    if (this.model() === 'anime') {
      const path = this.animeModelPath().trim();
      if (path) {
        options['modelPath'] = path;
      }
    }

    const paths = this.selectedPaths();
    return {
      toolId: UPSCALE_TOOL_ID,
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
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
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
        ? this.i18n.t('imageUpscale.notice.filesOne')
        : this.i18n.t('imageUpscale.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('imageUpscale.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('imageUpscale.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }
}
