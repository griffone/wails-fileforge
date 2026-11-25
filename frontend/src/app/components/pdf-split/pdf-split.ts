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
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import {
  PdfSplitPreview,
  groupConsecutivePages,
  normalizePageSelection,
  pagesToRangeSpec,
} from './pdf-split-preview';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_SPLIT_TOOL_ID = 'tool.pdf.split';
const STRATEGY_EVERY_PAGE = 'every_page';
const STRATEGY_RANGES = 'ranges';
const STRATEGY_SELECTED = 'selected';
const STRATEGY_PAGES_PER_FILE = 'pages_per_file';
const STRATEGY_BOOKMARKS = 'bookmarks';
const PLAN_DEBOUNCE_MS = 250;
const DEFAULT_PAGES_PER_FILE = 5;
const MIN_PAGES_PER_FILE = 1;
const MAX_PAGES_PER_FILE = 1000;
const OPTIONS_SCHEMA_VERSION = 1;
// Matches ResultCard: larger sets collapse into one folder summary row.
const GROUP_OUTPUT_LIMIT = 4;

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

type PdfSplitOptions = {
  strategy: SplitStrategy;
  ranges: string;
  pagesPerFileInput: string;
};

function defaultOptions(): PdfSplitOptions {
  return {
    strategy: STRATEGY_EVERY_PAGE,
    ranges: '',
    pagesPerFileInput: String(DEFAULT_PAGES_PER_FILE),
  };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfSplitOptions> {
  const result: Partial<PdfSplitOptions> = {};

  const strategy = stored['strategy'];
  if (
    typeof strategy === 'string' &&
    SPLIT_STRATEGIES.some((preset) => preset.id === strategy)
  ) {
    result.strategy = strategy as SplitStrategy;
  }

  if (typeof stored['ranges'] === 'string') {
    result.ranges = stored['ranges'];
  }

  if (typeof stored['pagesPerFileInput'] === 'string') {
    result.pagesPerFileInput = stored['pagesPerFileInput'];
  }

  return result;
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
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_SPLIT_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_SPLIT_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () =>
      this.selectedPaths().length > 0 &&
      !this.rangesRequired() &&
      !this.selectionRequired() &&
      !this.pagesPerFileInvalid(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    collectOutputPaths: () => this.groupedSafeOutputPaths(),
    afterPathsChanged: (previous, paths) => {
      const previousFirst = previous[0] ?? '';
      const nextFirst = paths[0] ?? '';
      if (paths.length === 0 || nextFirst !== previousFirst) {
        this.selectedPages.set([]);
      }
      void this.job.refreshPlan();
    },
    messages: {
      start: 'pdfSplit.error.start',
      planFailed: 'pdfSplit.error.plan',
      waiting: 'pdfSplit.status.waiting',
      inspectFailed: 'pdfSplit.error.inspect',
      statusMissing: 'pdfSplit.error.status',
      cancelFailed: 'pdfSplit.error.cancel',
      openFileFailed: 'pdfSplit.error.openFile',
      revealFailed: 'pdfSplit.error.openFolder',
    },
    notice: {
      rejectedOne: 'pdfSplit.notice.rejectedOne',
      rejectedMany: 'pdfSplit.notice.rejectedMany',
      duplicateOne: 'pdfSplit.notice.duplicateOne',
      duplicateMany: 'pdfSplit.notice.duplicateMany',
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

  readonly strategies = SPLIT_STRATEGIES;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly strategy = signal<SplitStrategy>(STRATEGY_EVERY_PAGE);
  readonly ranges = signal('');
  readonly selectedPages = signal<number[]>([]);
  readonly pagesPerFileInput = signal(String(DEFAULT_PAGES_PER_FILE));
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
      this.strategyLabel(
        this.strategies.find((preset) => preset.id === this.strategy()) ??
          SPLIT_STRATEGIES[0],
      ),
  );

  readonly rangesRequired = computed(
    () => this.isRangesStrategy() && this.ranges().trim().length === 0,
  );

  readonly selectionRequired = computed(
    () => this.isSelectedStrategy() && this.selectedPages().length === 0,
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('pdfSplit.action.splitting');
    }
    const count = this.selectedPaths().length;
    const base =
      count === 0
        ? this.i18n.t('pdfSplit.action.split')
        : count === 1
          ? this.i18n.t('pdfSplit.action.splitOne')
          : this.i18n.t('pdfSplit.action.splitMany', { count });
    if (this.isSelectedStrategy() && this.selectedPages().length > 0) {
      const parts = groupConsecutivePages(this.selectedPages()).length;
      return this.i18n.t('pdfSplit.action.splitInto', {
        base,
        count: this.partCountLabel(parts),
      });
    }
    if (this.isPagesPerFileStrategy() && this.plannedPartCount() > 0) {
      return this.i18n.t('pdfSplit.action.splitInto', {
        base,
        count: this.partCountLabel(this.plannedPartCount()),
      });
    }
    if (this.isBookmarksStrategy() && this.plannedPartCount() > 0) {
      return this.i18n.t('pdfSplit.action.splitInto', {
        base,
        count: formatCount(
          this.plannedPartCount(),
          this.i18n.t('common.fileOne'),
          this.i18n.t('common.fileMany'),
        ),
      });
    }
    return base;
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfSplit.destination.each'),
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
      return this.i18n.t('pdfSplit.helper.addOne');
    }
    if (this.rangesRequired()) {
      return this.i18n.t('pdfSplit.helper.ranges');
    }
    if (this.selectionRequired()) {
      return this.i18n.t('pdfSplit.helper.selection');
    }
    if (this.pagesPerFileInvalid()) {
      return this.i18n.t('pdfSplit.helper.pagesPerFile');
    }
    return '';
  });

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/pdf-split');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  /** Strategy copy resolved at render time so the locale switch applies. */
  strategyLabel(preset: StrategyPreset): string {
    return this.i18n.tOr(
      `pdfSplit.strategy.${preset.id}.label`,
      preset.label,
    );
  }

  strategyHint(preset: StrategyPreset): string {
    return this.i18n.tOr(
      `pdfSplit.strategy.${preset.id}.hint`,
      preset.hint,
    );
  }

  private partCountLabel(count: number): string {
    return formatCount(
      count,
      this.i18n.t('pdfSplit.unit.partOne'),
      this.i18n.t('pdfSplit.unit.partMany'),
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

  selectStrategy(strategyId: SplitStrategy): void {
    if (strategyId === this.strategy()) {
      return;
    }
    this.strategy.set(strategyId);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onRangesInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.ranges.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onPagesPerFileInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.pagesPerFileInput.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
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
      this.job.clearPlan();
      return;
    }

    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
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
    await this.job.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
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

  /** Retries only the failed inputs with the same strategy, ranges and folder. */
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
    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.selectedPages.set([]);
  }

  private currentOptions(): PdfSplitOptions {
    return {
      strategy: this.strategy(),
      ranges: this.ranges(),
      pagesPerFileInput: this.pagesPerFileInput(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.strategy.set(options.strategy);
    this.ranges.set(options.ranges);
    this.pagesPerFileInput.set(options.pagesPerFileInput);
  }

  private buildRequest(): JobRequest {
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

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(
        formatCount(
          info.pageCount,
          this.i18n.t('common.pageOne'),
          this.i18n.t('common.pageMany'),
        ),
      );
    }
    return parts.join(' · ');
  }

  /**
   * A split can produce dozens of parts; results above the card's grouping
   * threshold render as one folder row, so their individual sizes are not read.
   */
  private groupedSafeOutputPaths(): string[] {
    const paths = new Set<string>();
    for (const item of this.jobResult()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs = item.outputs ?? [];
      const count = Math.max(item.outputCount ?? 0, outputs.length);
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
}

function samePageList(left: number[], right: number[]): boolean {
  return (
    left.length === right.length &&
    left.every((page, index) => page === right[index])
  );
}
