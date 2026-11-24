import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Subscription } from 'rxjs';
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
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
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
import { observeJob } from '../../utils/job-stream';
import { resumeToolJob } from '../../utils/resume-task';

const BACKGROUND_TOOL_ID = 'tool.image.background';
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 160;
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

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type BackgroundMode = 'transparent' | 'color';
type CapabilityStatus = 'loading' | 'ok' | 'error';

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

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

/** `#A1B2C3` and `a1b2c3` both normalise to `#a1b2c3`; invalid stays empty. */
function normalizeHexColor(raw: string): string {
  const trimmed = (raw ?? '').trim();
  const withHash = trimmed.startsWith('#') ? trimmed : `#${trimmed}`;
  return /^#[0-9a-fA-F]{6}$/.test(withHash) ? withHash.toLowerCase() : '';
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
  private readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly recheckIcon = RefreshCw;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);

  readonly backgroundMode = signal<BackgroundMode>('transparent');
  readonly color = signal(DEFAULT_BACKGROUND_COLOR);
  readonly outputDir = signal(this.preferences.defaultOutputDir());

  readonly toolchain = signal<ToolchainStatus | null>(null);
  readonly toolchainState = signal<CapabilityStatus>('loading');
  readonly toolchainChecking = signal(false);

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

  readonly terminalJob = computed<JobResult | null>(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
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

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            error,
            this.i18n.t('imageBackground.error.start'),
          ),
        )
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {}

  ngOnInit(): void {
    void this.refreshToolchain();

    const pending = this.dropIntake.take('/image-background');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
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
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
  }

  removeFile(path: string): void {
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
  }

  onColorInput(event: Event): void {
    this.color.set(
      (event.target as HTMLInputElement | null)?.value ??
        DEFAULT_BACKGROUND_COLOR,
    );
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
    this.schedulePlanRefresh();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.schedulePlanRefresh();
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
      this.schedulePlanRefresh();
    } else {
      this.clearPlanState();
    }
  }

  async run(): Promise<void> {
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for output names and destinations, so it
    // is always refreshed with the current inputs before starting the job.
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
        message: this.i18n.t('imageBackground.error.start'),
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: this.i18n.t('imageBackground.error.start'),
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message || this.i18n.t('imageBackground.error.start'),
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
      message: this.i18n.t('imageBackground.status.waiting'),
      toolId: BACKGROUND_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: this.i18n.t('imageBackground.status.waiting'),
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
        response.message || this.i18n.t('imageBackground.error.cancel'),
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success ? '' : response.message || this.i18n.t('imageBackground.error.openFile'),
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('imageBackground.error.openFolder'),
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
    this.stopObserving();
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

  private async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (
      this.selectedPaths().length === 0 ||
      !this.packReady() ||
      !this.colorValid()
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
        message: this.i18n.t('imageBackground.error.plan'),
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: this.i18n.t('imageBackground.error.plan'),
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
      message: response.message || this.i18n.t('imageBackground.error.plan'),
    };
    // Keep the failed plan so the shared plan card shows the reason next to
    // the destination it belongs to.
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
    if (
      this.selectedPaths().length === 0 ||
      !this.packReady() ||
      !this.colorValid()
    ) {
      this.clearPlanState();
      return;
    }
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
      toolId: BACKGROUND_TOOL_ID,
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
        message: response.message || this.i18n.t('imageBackground.error.status'),
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
        message: this.i18n.t('imageBackground.error.inspect'),
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
    this.fileItems.set(paths.map((path) => ({ path, name: fileName(path) })));

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

  private rejectionNotice(rejected: string[]): string {
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

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
