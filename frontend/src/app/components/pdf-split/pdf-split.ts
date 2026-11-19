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
import {
  PdfSplitPreview,
  groupConsecutivePages,
  normalizePageSelection,
  pagesToRangeSpec,
} from './pdf-split-preview';
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
} from '../../utils/format';
import { resumeToolJob } from '../../utils/resume-task';

const PDF_SPLIT_TOOL_ID = 'tool.pdf.split';
const STRATEGY_EVERY_PAGE = 'every_page';
const STRATEGY_RANGES = 'ranges';
const STRATEGY_SELECTED = 'selected';
const STRATEGY_PAGES_PER_FILE = 'pages_per_file';
const STRATEGY_BOOKMARKS = 'bookmarks';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 160;
const DEFAULT_PAGES_PER_FILE = 5;
const MIN_PAGES_PER_FILE = 1;
const MAX_PAGES_PER_FILE = 1000;
// Matches ResultCard: larger sets collapse into one folder summary row.
const GROUP_OUTPUT_LIMIT = 4;

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type SplitStrategy =
  | typeof STRATEGY_EVERY_PAGE
  | typeof STRATEGY_RANGES
  | typeof STRATEGY_SELECTED
  | typeof STRATEGY_PAGES_PER_FILE
  | typeof STRATEGY_BOOKMARKS;

interface StrategyPreset {
  id: SplitStrategy;
  label: string;
  hint: string;
}

const SPLIT_STRATEGIES: StrategyPreset[] = [
  {
    id: STRATEGY_EVERY_PAGE,
    label: 'Every page',
    hint: 'One PDF for each page.',
  },
  {
    id: STRATEGY_RANGES,
    label: 'Page ranges',
    hint: 'Split into the ranges you type.',
  },
  {
    id: STRATEGY_SELECTED,
    label: 'Selected pages',
    hint: 'Pick pages; consecutive ones group together',
  },
  {
    id: STRATEGY_PAGES_PER_FILE,
    label: 'Pages per file',
    hint: 'Split every N pages.',
  },
  {
    id: STRATEGY_BOOKMARKS,
    label: 'Bookmarks',
    hint: 'One file per top-level bookmark.',
  },
];

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

@Component({
  selector: 'app-pdf-split',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
    PdfSplitPreview,
  ],
  templateUrl: './pdf-split.html',
  styleUrl: './pdf-split.css',
})
export class PdfSplit implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly strategies = SPLIT_STRATEGIES;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly strategy = signal<SplitStrategy>(STRATEGY_EVERY_PAGE);
  readonly ranges = signal('');
  readonly selectedPages = signal<number[]>([]);
  readonly pagesPerFileInput = signal(String(DEFAULT_PAGES_PER_FILE));
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

  readonly isRangesStrategy = computed(
    () => this.strategy() === STRATEGY_RANGES,
  );

  readonly isSelectedStrategy = computed(
    () => this.strategy() === STRATEGY_SELECTED,
  );

  readonly isPagesPerFileStrategy = computed(
    () => this.strategy() === STRATEGY_PAGES_PER_FILE,
  );

  readonly isBookmarksStrategy = computed(
    () => this.strategy() === STRATEGY_BOOKMARKS,
  );

  /** Validated chunk size; null while the input is empty or out of range. */
  readonly pagesPerFile = computed<number | null>(() => {
    const value = Number(this.pagesPerFileInput().trim());
    return Number.isInteger(value) &&
      value >= MIN_PAGES_PER_FILE &&
      value <= MAX_PAGES_PER_FILE
      ? value
      : null;
  });

  readonly pagesPerFileInvalid = computed(
    () => this.isPagesPerFileStrategy() && this.pagesPerFile() === null,
  );

  /** Total files the current plan expects, across every input PDF. */
  readonly plannedPartCount = computed(() => {
    const plan = this.plan();
    if (!plan?.success) {
      return 0;
    }
    return plan.outputs.reduce(
      (total, output) => total + (output.outputCount ?? 0),
      0,
    );
  });

  readonly activeStrategyLabel = computed(
    () =>
      this.strategies.find((preset) => preset.id === this.strategy())?.label ??
      'Every page',
  );

  readonly rangesRequired = computed(
    () => this.isRangesStrategy() && this.ranges().trim().length === 0,
  );

  readonly selectionRequired = computed(
    () => this.isSelectedStrategy() && this.selectedPages().length === 0,
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Splitting…';
    }
    const count = this.selectedPaths().length;
    const base =
      count === 0
        ? 'Split PDFs'
        : count === 1
          ? 'Split 1 PDF'
          : `Split ${count} PDFs`;
    if (this.isSelectedStrategy() && this.selectedPages().length > 0) {
      const parts = groupConsecutivePages(this.selectedPages()).length;
      return `${base} into ${formatCount(parts, 'part')}`;
    }
    if (this.isPagesPerFileStrategy() && this.plannedPartCount() > 0) {
      return `${base} into ${formatCount(this.plannedPartCount(), 'part')}`;
    }
    if (this.isBookmarksStrategy() && this.plannedPartCount() > 0) {
      return `${base} into ${formatCount(this.plannedPartCount(), 'file')}`;
    }
    return base;
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each PDF',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.rangesRequired() &&
      !this.selectionRequired() &&
      !this.pagesPerFileInvalid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return 'Add at least one PDF to split.';
    }
    if (this.rangesRequired()) {
      return 'Enter the page ranges to split.';
    }
    if (this.selectionRequired()) {
      return 'Select at least one page to build the split.';
    }
    if (this.pagesPerFileInvalid()) {
      return 'Enter a whole number of pages from 1 to 1000.';
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
    return error
      ? describeToolError(error, 'The split could not start.')
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
    const pending = this.dropIntake.take('/pdf-split');
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
    const known = new Set(this.selectedPaths().map((path) => this.pathKey(path)));

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

    const previousFirst = this.selectedPaths()[0] ?? '';
    this.actionMessage.set('');
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.syncSelectionForPaths(previousFirst);
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
    this.selectedPages.set([]);
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
    const previousFirst = this.selectedPaths()[0] ?? '';
    this.selectedPaths.set(next);
    this.syncSelectionForPaths(previousFirst);
    this.syncFileItems();
    void this.refreshPlan();
  }

  selectStrategy(strategyId: SplitStrategy): void {
    if (strategyId === this.strategy()) {
      return;
    }
    this.strategy.set(strategyId);
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  onRangesInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.ranges.set(value);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  onPagesPerFileInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.pagesPerFileInput.set(value);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  onSelectedPagesChange(pages: number[]): void {
    const next = normalizePageSelection(pages);
    if (samePageList(next, this.selectedPages())) {
      return;
    }

    this.selectedPages.set(next);
    this.actionMessage.set('');

    if (next.length === 0) {
      // No selection means no output: drop the stale plan immediately.
      this.clearPlanRefreshTimer();
      this.planToken += 1;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return;
    }

    this.schedulePlanRefresh();
  }

  /** `1,2,3,5,7,8` → `1-3,5,7-8`, the range syntax the backend understands. */
  pagesToRanges(pages: number[]): string {
    return pagesToRangeSpec(pages);
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

    // The plan is the source of truth for ranges and destinations, so it is
    // always refreshed with the current inputs before starting the job.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponseV1 = await this.wailsService
      .runJobV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The split could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The split could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The split could not start.',
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
      toolId: PDF_SPLIT_TOOL_ID,
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

  /** Retries only the failed inputs with the same strategy, ranges and folder. */
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
    this.selectedPages.set([]);
  }

  private buildRequest(): JobRequestV1 {
    const paths = this.selectedPaths();
    const strategy = this.strategyForPlan();
    const options: Record<string, unknown> = { strategy };
    const ranges = this.rangesForPlan();
    if (strategy === STRATEGY_RANGES && ranges) {
      options['ranges'] = ranges;
    }
    if (strategy === STRATEGY_PAGES_PER_FILE) {
      const pagesPerFile = this.pagesPerFile();
      if (pagesPerFile !== null) {
        options['pagesPerFile'] = pagesPerFile;
      }
    }

    return {
      toolId: PDF_SPLIT_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  /** The backend only knows every_page/ranges; picks become explicit ranges. */
  private strategyForPlan(): SplitStrategy {
    return this.isSelectedStrategy() ? STRATEGY_RANGES : this.strategy();
  }

  private rangesForPlan(): string {
    return this.isSelectedStrategy()
      ? pagesToRangeSpec(this.selectedPages())
      : this.ranges().trim();
  }

  private async refreshPlan(): Promise<boolean> {
    if (
      this.selectedPaths().length === 0 ||
      this.rangesRequired() ||
      this.selectionRequired() ||
      this.pagesPerFileInvalid()
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
    // Keep the failed plan so the shared plan card can show the range or
    // destination problem next to the destination it belongs to.
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
      toolId: PDF_SPLIT_TOOL_ID,
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

      const outputs = item.outputs ?? [];
      const count = Math.max(item.outputCount ?? 0, outputs.length);
      // Grouped results show a folder summary; individual sizes are not used.
      if (count > GROUP_OUTPUT_LIMIT) {
        continue;
      }

      const candidates =
        outputs.length > 0
          ? outputs
          : item.outputPath
            ? [item.outputPath]
            : [];
      for (const output of candidates) {
        if (output) {
          paths.add(output);
        }
      }
    }
    return [...paths];
  }

  /** Page picks belong to the previewed PDF; drop them when it changes. */
  private syncSelectionForPaths(previousFirst: string): void {
    const nextFirst = this.selectedPaths()[0] ?? '';
    if (nextFirst !== previousFirst && this.selectedPages().length > 0) {
      this.selectedPages.set([]);
    }
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
        // Thumbnails are optional; keep the placeholder icon.
      });
  }

  private describeFileInfo(info: FileInfoV1): string {
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

function samePageList(left: number[], right: number[]): boolean {
  return (
    left.length === right.length &&
    left.every((page, index) => page === right[index])
  );
}
