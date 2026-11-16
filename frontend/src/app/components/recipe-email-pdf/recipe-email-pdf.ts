import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import {
  FileInfoV1,
  JobErrorV1,
  JobExecutionStatusV1,
  JobResultItemV1,
  JobResultV1,
  RecipePlanResponseV1,
  RecipeRequestV1,
  RecipeResultV1,
  RecipeStatusResponseV1,
  RecipeV1,
  RunRecipeResponseV1,
  VerificationReportV1,
  Wails,
} from '../../services/wails';
import { fileExtension, fileName, formatBytes, formatCount } from '../../utils/format';

const RECIPE_ID = 'recipe.email_pdf';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 250;
const DEFAULT_TARGET_MB = 10;
const BYTES_PER_MB = 1024 * 1024;

const TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  'success',
  'failed',
  'partial_success',
  'cancelled',
]);

type RecipeStepState = 'pending' | 'running' | 'success' | 'skipped' | 'failed';

export interface RecipeStepView {
  id: string;
  name: string;
  state: RecipeStepState;
  message: string;
}

/**
 * Local definition shown before (or instead of) the backend catalog. The steps
 * mirror the curated Email PDF recipe so the page never blocks on IPC.
 */
const EMAIL_PDF_FALLBACK: RecipeV1 = {
  id: RECIPE_ID,
  name: 'Email PDF',
  description:
    'Merge one or more PDFs and compress the result into one file that fits an email limit.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  steps: [
    { id: 'merge', name: 'Merge PDFs', toolId: 'tool.pdf.merge' },
    { id: 'optimize', name: 'Optimize to target', toolId: 'tool.pdf.optimize' },
  ],
  defaultTargetSizeBytes: DEFAULT_TARGET_MB * BYTES_PER_MB,
};

function normalizeStepState(value: string | undefined): RecipeStepState {
  switch (value) {
    case 'success':
    case 'failed':
    case 'skipped':
    case 'running':
      return value;
    default:
      return 'pending';
  }
}

function defaultTargetMb(recipe: RecipeV1 | null): number {
  const bytes = recipe?.defaultTargetSizeBytes ?? 0;
  return bytes > 0 ? bytes / BYTES_PER_MB : DEFAULT_TARGET_MB;
}

function targetInputText(mb: number): string {
  if (!Number.isFinite(mb) || mb <= 0) {
    return '';
  }
  return String(Number(mb.toFixed(2)));
}

@Component({
  selector: 'app-recipe-email-pdf',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
  ],
  templateUrl: './recipe-email-pdf.html',
  styleUrl: './recipe-email-pdf.css',
})
export class RecipeEmailPdf implements OnDestroy, OnInit {
  private readonly wailsService = inject(Wails);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  /** Empty means "next to the first PDF"; the backend resolves it. */
  readonly outputDir = signal('');
  readonly targetMb = signal(DEFAULT_TARGET_MB);
  readonly targetText = signal(String(DEFAULT_TARGET_MB));
  readonly recipe = signal<RecipeV1 | null>(EMAIL_PDF_FALLBACK);

  readonly plan = signal<RecipePlanResponseV1 | null>(null);
  readonly planLoading = signal(false);

  readonly result = signal<RecipeResultV1 | null>(null);
  readonly runJobId = signal('');
  readonly isSubmitting = signal(false);
  readonly isRunning = signal(false);
  readonly cancelRequested = signal(false);
  readonly actionError = signal<JobErrorV1 | null>(null);
  readonly notice = signal('');

  readonly fileName = fileName;

  readonly targetInvalid = computed(() => {
    const value = this.targetMb();
    return !Number.isFinite(value) || value <= 0;
  });

  readonly targetBytes = computed(() =>
    Math.round(this.targetMb() * BYTES_PER_MB),
  );

  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.targetInvalid() &&
      !this.isSubmitting() &&
      !this.isRunning() &&
      !this.planBlocked(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting()) {
      return 'Preparing…';
    }
    if (this.isRunning()) {
      return 'Creating…';
    }
    return 'Create email PDF';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to the first PDF',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly terminalResult = computed<RecipeResultV1 | null>(() => {
    const result = this.result();
    return result && this.isTerminalStatus(result.status) ? result : null;
  });

  /**
   * Adapts the recipe snapshot to the shared ResultCard contract. A recipe has
   * one logical output item even when the internal chain produced a merge
   * intermediate; only the final paths are listed.
   */
  readonly resultJob = computed<JobResultV1 | null>(() => {
    const result = this.terminalResult();
    if (!result) {
      return null;
    }

    const status = result.status as JobExecutionStatusV1;
    const outputs = result.outputs ?? [];
    const items: JobResultItemV1[] =
      outputs.length > 0
        ? [
            {
              inputPath: this.selectedPaths()[0] ?? '',
              outputPath: outputs[0],
              outputs,
              outputCount: outputs.length,
              success: status !== 'failed' && status !== 'cancelled',
              message: result.message,
            },
          ]
        : [];

    return {
      jobId: result.jobId,
      success: status === 'success' || status === 'partial_success',
      message: result.message,
      toolId: RECIPE_ID,
      status,
      progress: {
        current: result.currentStep,
        total: result.totalSteps,
        stage: result.status,
        message: result.message,
        etaSeconds: undefined,
      },
      items,
      error: result.error,
      startedAt: result.startedAt,
      endedAt: result.endedAt,
    };
  });

  readonly verification = computed<VerificationReportV1 | null>(
    () => this.result()?.verification ?? null,
  );

  /**
   * Recipe steps with the per-step state merged in. Before the run the Merge
   * step is labelled as skipped when a single PDF is queued; once the run
   * exists the backend step snapshot takes over.
   */
  readonly steps = computed<RecipeStepView[]>(() => {
    const recipeSteps = this.recipe()?.steps ?? [];
    const result = this.result();
    const hasRun = result !== null;
    const active = this.isRunning();
    const singleInput = this.selectedPaths().length === 1;

    return recipeSteps.map((step, index) => {
      const resultStep = result?.steps?.[index];

      if (singleInput && index === 0) {
        return {
          id: step.id,
          name: step.name,
          state: 'skipped',
          message: hasRun
            ? (resultStep?.message ?? '')
            : 'Not needed with a single PDF',
        };
      }

      let state = normalizeStepState(resultStep?.status);
      if (
        active &&
        result &&
        result.currentStep === index + 1 &&
        state === 'pending'
      ) {
        state = 'running';
      }

      return {
        id: step.id,
        name: step.name,
        state,
        message: resultStep?.message ?? '',
      };
    });
  });

  readonly progressPercent = computed(() => {
    const result = this.result();
    if (!result) {
      return 0;
    }
    if (this.isTerminalStatus(result.status)) {
      return 100;
    }
    if (result.totalSteps <= 0) {
      return 0;
    }
    const percent = Math.round((result.currentStep / result.totalSteps) * 100);
    return Math.min(100, Math.max(0, percent));
  });

  readonly progressLabel = computed(() => {
    const result = this.result();
    const total = result?.totalSteps || this.steps().length;
    if (!result) {
      return 'Starting…';
    }
    return `Step ${Math.max(1, Math.min(result.currentStep, total))} of ${total}`;
  });

  readonly activeStepMessage = computed(() => {
    const active = this.steps().find((step) => step.state === 'running');
    return active?.message || this.result()?.message || '';
  });

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return 'Add at least one PDF to create an email PDF.';
    }
    if (this.targetInvalid()) {
      return 'Enter a target size larger than 0 MB.';
    }
    return '';
  });

  readonly runErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.actionError() ?? this.terminalResult()?.error;
    if (error) {
      return describeToolError(error, 'The recipe could not finish.');
    }

    const result = this.terminalResult();
    if (result?.status === 'failed' && result.message.trim()) {
      return describeToolError(
        { code: 'EXEC_IO_TRANSIENT', message: result.message },
        'The recipe could not finish.',
      );
    }

    return null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private planDirty = false;
  private pollInFlight = false;
  private fileItemsGeneration = 0;
  private targetTouched = false;

  async ngOnInit(): Promise<void> {
    this.recipe.set(EMAIL_PDF_FALLBACK);

    const response = await this.wailsService.listRecipesV1().catch(() => ({
      success: false,
      message: 'The recipe catalog could not be read.',
      recipes: [] as RecipeV1[],
    }));
    if (!response.success) {
      return;
    }

    const found = response.recipes.find((candidate) => candidate.id === RECIPE_ID);
    if (!found) {
      return;
    }

    this.recipe.set(found);
    if (!this.targetTouched) {
      const defaultMb = defaultTargetMb(found);
      this.targetMb.set(defaultMb);
      this.targetText.set(targetInputText(defaultMb));
    }
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
  }

  fileCountLabel(): string {
    const count = this.selectedPaths().length;
    return count === 1 ? '1 PDF' : `${count} PDFs`;
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
    const known = new Set(
      this.selectedPaths().map((path) => this.pathKey(path)),
    );

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

    this.actionError.set(null);
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
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.fileItemsGeneration += 1;
    this.planToken += 1;
    this.planDirty = false;
    this.plan.set(null);
    this.planLoading.set(false);
    this.result.set(null);
    this.runJobId.set('');
    this.isSubmitting.set(false);
    this.cancelRequested.set(false);
    this.actionError.set(null);
    this.notice.set('');
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

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.schedulePlanRefresh();
  }

  useFirstPdfFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.schedulePlanRefresh();
  }

  onTargetInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.targetTouched = true;
    this.targetText.set(value);
    this.targetMb.set(Number(value));
    this.actionError.set(null);
    this.schedulePlanRefresh();
  }

  async run(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionError.set(null);
    this.notice.set('');
    this.result.set(null);
    this.runJobId.set('');
    this.cancelRequested.set(false);
    this.stopPolling();

    if (this.planLoading() || this.planDirty || !this.plan()?.success) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    this.isSubmitting.set(true);
    const response: RunRecipeResponseV1 = await this.wailsService
      .runRecipeV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The recipe could not start.',
        jobId: '',
        status: 'failed',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_RUN_FAILED',
          message: 'The recipe could not start.',
        },
      }));
    this.isSubmitting.set(false);

    if (!response.success || !response.jobId) {
      this.actionError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_RUN_FAILED',
          message: response.message || 'The recipe could not start.',
        },
      );
      return;
    }

    this.runJobId.set(response.jobId);
    this.startPolling();
  }

  async cancel(): Promise<void> {
    const jobId = this.runJobId();
    if (!jobId || this.cancelRequested()) {
      return;
    }

    this.cancelRequested.set(true);
    const response: RecipeStatusResponseV1 = await this.wailsService
      .cancelRecipeV1(jobId)
      .catch(() => ({
        success: false,
        found: false,
        message: 'The recipe could not be cancelled.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_CANCEL_FAILED',
          message: 'The recipe could not be cancelled.',
        },
      }));
    if (!response.success) {
      this.actionError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_CANCEL_FAILED',
          message: response.message || 'The recipe could not be cancelled.',
        },
      );
    }
  }

  async openPath(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.notice.set(
      response.success ? '' : response.message || 'The file could not be opened.',
    );
  }

  async revealInFolder(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.notice.set(
      response.success
        ? ''
        : response.message || 'The folder could not be opened.',
    );
  }

  resetRun(): void {
    this.stopPolling();
    this.result.set(null);
    this.runJobId.set('');
    this.isSubmitting.set(false);
    this.cancelRequested.set(false);
    this.actionError.set(null);
    this.notice.set('');
  }

  stepStateLabel(state: RecipeStepState): string {
    switch (state) {
      case 'success':
        return 'Done';
      case 'failed':
        return 'Failed';
      case 'skipped':
        return 'Skipped';
      case 'running':
        return 'Running';
      default:
        return 'Pending';
    }
  }

  stepDotState(state: RecipeStepState): string {
    switch (state) {
      case 'success':
        return 'success';
      case 'failed':
        return 'danger';
      case 'running':
        return 'info';
      default:
        return 'idle';
    }
  }

  private buildRequest(): RecipeRequestV1 {
    return {
      recipeId: this.recipe()?.id ?? RECIPE_ID,
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options: { targetSizeBytes: this.targetBytes() },
    };
  }

  private async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (this.selectedPaths().length === 0 || this.targetInvalid()) {
      this.planToken += 1;
      this.planDirty = false;
      this.plan.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planDirty = false;
    this.planLoading.set(true);
    this.plan.set(null);

    const response: RecipePlanResponseV1 = await this.wailsService
      .planRecipeV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The output could not be planned.',
        inputPaths: [],
        outputDir: '',
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_PLAN_FAILED',
          message: 'The output could not be planned.',
        },
      }));
    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);
    this.plan.set(response);
    return response.success;
  }

  private schedulePlanRefresh(): void {
    this.planDirty = true;
    // Invalidate an in-flight plan: it was built from older files or options.
    this.planToken += 1;
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

  private startPolling(): void {
    this.stopPolling();
    this.isRunning.set(true);
    void this.pollStatus();
    this.pollingTimer = setInterval(() => {
      void this.pollStatus();
    }, POLLING_INTERVAL_MS);
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isRunning.set(false);
  }

  private async pollStatus(): Promise<void> {
    if (this.pollInFlight) {
      return;
    }

    const jobId = this.runJobId();
    if (!jobId) {
      this.stopPolling();
      return;
    }

    this.pollInFlight = true;
    try {
      const response: RecipeStatusResponseV1 = await this.wailsService
        .getRecipeStatusV1(jobId)
        .catch(() => ({
          success: false,
          found: false,
          message: 'The recipe status could not be read.',
          error: {
            code: 'EXEC_IO_TRANSIENT',
            detail_code: 'RECIPE_STATUS_FAILED',
            message: 'The recipe status could not be read.',
          },
        }));
      if (this.runJobId() !== jobId) {
        return;
      }

      if (!response.found || !response.result) {
        this.stopPolling();
        this.actionError.set(
          response.error ?? {
            code: 'EXEC_IO_TRANSIENT',
            detail_code: 'RECIPE_STATUS_MISSING',
            message: response.message || 'The recipe status could not be read.',
          },
        );
        return;
      }

      this.result.set(response.result);
      if (this.isTerminalStatus(response.result.status)) {
        this.stopPolling();
        if (response.result.status === 'cancelled') {
          this.notice.set('Cancelled — no output was written.');
        }
      }
    } finally {
      this.pollInFlight = false;
    }
  }

  private swap(leftIndex: number, rightIndex: number): void {
    const next = [...this.selectedPaths()];
    const left = next[leftIndex];
    next[leftIndex] = next[rightIndex];
    next[rightIndex] = left;
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.schedulePlanRefresh();
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
      if (generation !== this.fileItemsGeneration || !response.success) {
        return;
      }

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
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }
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

  private intakeNotice(rejected: string[], duplicates: string[]): string {
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

    return parts.join(' ');
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
