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
import { ResultCard } from '../tool-flow/result-card';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  JobRequest,
  ToolchainComponent,
  ToolchainStatus,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const BACKGROUND_TOOL_ID = 'tool.image.background';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;
const DEFAULT_BACKGROUND_COLOR = '#ffffff';

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

type BackgroundMode = 'transparent' | 'color';
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

type ImageBackgroundOptions = {
  backgroundMode: BackgroundMode;
  color: string;
};

function defaultOptions(): ImageBackgroundOptions {
  return { backgroundMode: 'transparent', color: DEFAULT_BACKGROUND_COLOR };
}

/** `#A1B2C3` and `a1b2c3` both normalise to `#a1b2c3`; invalid stays empty. */
function normalizeHexColor(raw: string): string {
  const trimmed = (raw ?? '').trim();
  const withHash = trimmed.startsWith('#') ? trimmed : `#${trimmed}`;
  return /^#[0-9a-fA-F]{6}$/.test(withHash) ? withHash.toLowerCase() : '';
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageBackgroundOptions> {
  const result: Partial<ImageBackgroundOptions> = {};
  const mode = stored['backgroundMode'];
  if (mode === 'transparent' || mode === 'color') {
    result.backgroundMode = mode;
  }
  const color = stored['color'];
  if (typeof color === 'string') {
    result.color = color;
  }
  return result;
}

@Component({
  selector: 'app-image-background',
  standalone: true,
  imports: [
    RouterLink,
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-background.html',
  styleUrl: './image-background.css',
})
export class ImageBackground implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: BACKGROUND_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: BACKGROUND_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () =>
      this.selectedPaths().length > 0 &&
      this.packReady() &&
      this.colorValid(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () =>
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'imageBackground.error.start',
      planFailed: 'imageBackground.error.plan',
      waiting: 'imageBackground.status.waiting',
      inspectFailed: 'imageBackground.error.inspect',
      statusMissing: 'imageBackground.error.status',
      cancelFailed: 'imageBackground.error.cancel',
      openFileFailed: 'imageBackground.error.openFile',
      revealFailed: 'imageBackground.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly recheckIcon = RefreshCw;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;

  readonly backgroundMode = signal<BackgroundMode>('transparent');
  readonly color = signal(DEFAULT_BACKGROUND_COLOR);
  readonly outputDir = this.job.outputDir;

  readonly toolchain = signal<ToolchainStatus | null>(null);
  readonly toolchainState = signal<CapabilityStatus>('loading');
  readonly toolchainChecking = signal(false);

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

    return [
      resolve('rembg', this.i18n.t('imageBackground.pack.engineFallback')),
      resolve('rembg-model', this.i18n.t('imageBackground.pack.modelFallback')),
    ];
  });

  readonly missingPackComponents = computed(() =>
    this.packComponents().filter((component) => !component.ready),
  );

  /** Ready only when both the engine and the model are verified on disk. */
  readonly packReady = computed(() =>
    this.packComponents().every((component) => component.ready),
  );

  readonly packBanner = computed<PackBanner | null>(() => {
    if (this.packReady() || this.toolchainState() === 'loading') {
      return null;
    }

    if (this.toolchainState() === 'error') {
      return {
        title: this.i18n.t('imageBackground.pack.checkErrorTitle'),
        detail: this.i18n.t('imageBackground.pack.checkErrorDetail'),
      };
    }

    const anyReady = this.packComponents().some((component) => component.ready);
    return {
      title: anyReady
        ? this.i18n.t('imageBackground.pack.needsRepairTitle')
        : this.i18n.t('imageBackground.pack.missingTitle'),
      detail: this.i18n.t('imageBackground.pack.detail', {
        size: this.i18n.t('imageBackground.pack.size'),
      }),
    };
  });

  readonly packBadge = computed(() => {
    if (this.toolchainState() === 'loading' || this.toolchainChecking()) {
      return this.i18n.t('imageBackground.pack.badge.checking');
    }
    return this.packReady()
      ? this.i18n.t('imageBackground.pack.badge.ready')
      : this.i18n.t('imageBackground.pack.badge.missing');
  });

  readonly normalizedColor = computed(() =>
    normalizeHexColor(this.color()),
  );

  readonly colorValid = computed(
    () => this.backgroundMode() !== 'color' || this.normalizedColor() !== '',
  );

  readonly backgroundLabel = computed(() =>
    this.backgroundMode() === 'color'
      ? this.normalizedColor() || this.i18n.t('imageBackground.field.color')
      : this.i18n.t('imageBackground.mode.transparent'),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageBackground.action.removing');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageBackground.action.remove');
    }
    return count === 1
      ? this.i18n.t('imageBackground.action.removeOne')
      : this.i18n.t('imageBackground.action.removeMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageBackground.destination.original'),
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
      this.colorValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('imageBackground.helper.addImage');
    }
    if (this.toolchainState() === 'loading' || this.toolchainChecking()) {
      return this.i18n.t('imageBackground.helper.checking');
    }
    if (!this.packReady()) {
      return this.i18n.t('imageBackground.helper.notReady');
    }
    if (!this.colorValid()) {
      return this.i18n.t('imageBackground.helper.color');
    }
    return '';
  });

  /**
   * The tool reports what it produced per image (`transparent PNG WxH` or
   * `background #rrggbb, WxH`). Show the first successful message above the
   * shared result card, which lists output names and sizes.
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

    this.job.takePendingDrop('/image-background');
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

  onModeChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (value !== 'transparent' && value !== 'color') {
      return;
    }
    if (value === this.backgroundMode()) {
      return;
    }
    this.backgroundMode.set(value);
    this.actionMessage.set('');
    this.schedulePlan();
  }

  onColorInput(event: Event): void {
    this.color.set(
      (event.target as HTMLInputElement | null)?.value ??
        DEFAULT_BACKGROUND_COLOR,
    );
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
   * Reloads the toolchain snapshot. The pack is optional and never installed
   * from this page: the banner links to Settings instead.
   */
  async refreshToolchain(): Promise<void> {
    this.toolchainChecking.set(true);
    try {
      const status = await this.wailsService.getToolchainStatus();
      this.toolchain.set(status);
      this.toolchainState.set(status.success ? 'ok' : 'error');
    } catch {
      this.toolchain.set(null);
      this.toolchainState.set('error');
    } finally {
      this.toolchainChecking.set(false);
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
  }

  private currentOptions(): ImageBackgroundOptions {
    return { backgroundMode: this.backgroundMode(), color: this.color() };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.backgroundMode.set(options.backgroundMode);
    this.color.set(options.color);
  }

  /**
   * Drops the plan at once when the page cannot plan (no inputs, pack not
   * ready or an invalid colour) instead of leaving a stale plan on screen.
   */
  private schedulePlan(): void {
    if (
      this.selectedPaths().length === 0 ||
      !this.packReady() ||
      !this.colorValid()
    ) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {};
    if (this.backgroundMode() === 'color') {
      const color = this.normalizedColor();
      if (color) {
        options['backgroundColor'] = color;
      }
    }

    const paths = this.selectedPaths();
    return {
      toolId: BACKGROUND_TOOL_ID,
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
        ? this.i18n.t('imageBackground.notice.filesOne')
        : this.i18n.t('imageBackground.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('imageBackground.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t(
        'imageBackground.notice.extensionsMany',
        {
          head,
          tail: extensions[extensions.length - 1],
        },
      );
    }

    return `${filesCopy} ${extensionsCopy}`;
  }
}
