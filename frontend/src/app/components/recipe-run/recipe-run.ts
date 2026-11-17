import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';
import { Subject, takeUntil } from 'rxjs';

import { FileDrop } from '../file-drop/file-drop';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { findRecipeFallback } from '../../config/recipes';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import {
  FileInfoV1,
  JobErrorV1,
  JobExecutionStatusV1,
  JobResultItemV1,
  JobResultV1,
  RecipePlanResponseV1,
  RecipeProgressEventV1,
  RecipeRequestV1,
  RecipeResultV1,
  RecipeStatusResponseV1,
  RecipeV1,
  RunRecipeResponseV1,
  VerificationReportV1,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
  formatDuration,
} from '../../utils/format';

const DEFAULT_RECIPE_ID = 'recipe.email_pdf';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 250;
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
  /** Backend message for the step; empty before and between runs. */
  message: string;
  /** Static helper copy from the recipe definition. */
  detail: string;
}

interface TitleParts {
  lead: string;
  tail: string;
}

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

/** `Email PDF` → `email PDF`, so the action button reads like a sentence. */
function lowerFirst(value: string): string {
  return value ? value.charAt(0).toLowerCase() + value.slice(1) : value;
}

function defaultTargetMb(recipe: RecipeV1 | null): number {
  const bytes = recipe?.defaultTargetSizeBytes ?? 0;
  return bytes > 0 ? bytes / BYTES_PER_MB : 0;
}

function targetInputText(mb: number): string {
  if (!Number.isFinite(mb) || mb <= 0) {
    return '';
  }
  return String(Number(mb.toFixed(2)));
}

/** Splits a recipe name on its last space so the tail can render in serif. */
function splitTitle(name: string): TitleParts {
  const trimmed = name.trim();
  const index = trimmed.lastIndexOf(' ');
  if (index <= 0) {
    return { lead: trimmed, tail: '' };
  }
  return { lead: trimmed.slice(0, index), tail: trimmed.slice(index + 1) };
}

@Component({
  selector: 'app-recipe-run',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
  ],
  templateUrl: './recipe-run.html',
  styleUrl: './recipe-run.css',
})
export class RecipeRun implements OnDestroy, OnInit {
  private readonly wailsService = inject(Wails);
  private readonly route = inject(ActivatedRoute);
  private readonly recipeId = this.resolveRecipeId();

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  /** Empty means "next to the source"; the backend resolves it. */
  readonly outputDir = signal('');
  readonly targetMb = signal(defaultTargetMb(this.resolveFallback()));
  readonly targetText = signal(
    targetInputText(defaultTargetMb(this.resolveFallback())),
  );
  readonly recipe = signal<RecipeV1 | null>(this.resolveFallback());

  readonly plan = signal<RecipePlanResponseV1 | null>(null);
  readonly planLoading = signal(false);

  readonly result = signal<RecipeResultV1 | null>(null);
  readonly runJobId = signal('');
  /** Last live event from `recipes/progress/v1` for the current job. */
  readonly liveProgress = signal<RecipeProgressEventV1 | null>(null);
  readonly isSubmitting = signal(false);
  readonly isRunning = signal(false);
  readonly cancelRequested = signal(false);
  readonly actionError = signal<JobErrorV1 | null>(null);
  readonly notice = signal('');

  readonly fileName = fileName;

  /**
   * Live progress is advisory: the polled status stays the source of truth for
   * step results and outputs, but the event stream updates the stepper and the
   * bar as soon as the backend reaches a stage instead of one poll later.
   */
  constructor() {
    this.wailsService.recipeProgress$
      .pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        if (!event || event.jobId !== this.runJobId()) {
          return;
        }

        this.liveProgress.set(event);
      });
  }

  /** Recipes with a fixed outcome (no target size) hide the field entirely. */
  readonly showTargetSize = computed(
    () => (this.recipe()?.defaultTargetSizeBytes ?? 0) > 0,
  );

  /** Only `maxInputs === 1` restricts the queue; 0 means unlimited. */
  readonly multipleInputs = computed(
    () => (this.recipe()?.maxInputs ?? 0) !== 1,
  );

  readonly recipeHighlights = computed(() => this.recipe()?.highlights ?? []);

  readonly acceptAttribute = computed(() => {
    const extensions = this.recipe()?.acceptExtensions ?? [];
    return extensions.map((extension) => `.${extension}`).join(',');
  });

  readonly inputKindLabel = computed(() =>
    this.kindLabel(this.recipe()?.inputKind ?? ''),
  );

  readonly extensionsLabel = computed(() =>
    (this.recipe()?.acceptExtensions ?? [])
      .map((extension) => extension.toUpperCase())
      .join(' · '),
  );

  readonly inputHint = computed(() => {
    if (!this.multipleInputs()) {
      const kind = this.inputKindLabel();
      const extensions = this.extensionsLabel();
      if (extensions.toLowerCase() === kind.toLowerCase()) {
        return `One ${kind}`;
      }
      return `One ${kind} · ${extensions}`;
    }

    return `${this.extensionsLabel()} files · order matters`;
  });

  readonly dropTitle = computed(() => {
    if (this.multipleInputs()) {
      return this.recipe()?.inputKind === 'pdf' ? 'Drop PDFs here' : 'Drop files here';
    }
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'Drop a PDF here';
      case 'image':
        return 'Drop an image here';
      case 'video':
        return 'Drop a video here';
      default:
        return 'Drop a file here';
    }
  });

  private readonly titleParts = computed(() =>
    splitTitle(this.recipe()?.name ?? ''),
  );

  readonly titleLead = computed(() => this.titleParts().lead);
  readonly titleTail = computed(() => this.titleParts().tail);

  readonly targetInvalid = computed(() => {
    if (!this.showTargetSize()) {
      return false;
    }
    const value = this.targetMb();
    return !Number.isFinite(value) || value <= 0;
  });

  readonly targetBytes = computed(() =>
    this.showTargetSize() ? Math.round(this.targetMb() * BYTES_PER_MB) : 0,
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
    return `Create ${lowerFirst(this.recipe()?.name ?? 'recipe')}`;
  });

  /**
   * Count noun for the result summary. Mixed output sets (for example the
   * Clean scan PDF plus TXT sidecar) are described as files, not PDFs.
   */
  readonly resultToolLabel = computed(() => {
    const outputs = this.terminalResult()?.outputs ?? [];
    if (outputs.length > 0) {
      const allPdf = outputs.every(
        (path) => fileExtension(path).toLowerCase() === 'pdf',
      );
      return allPdf ? 'PDF' : 'file';
    }
    return this.recipe()?.inputKind === 'pdf' ? 'PDF' : 'file';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || this.defaultDestinationLabel(),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly defaultDestinationHelp = computed(() => {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'The result is saved next to the first PDF.';
      case 'image':
        return 'The result is saved next to the source image.';
      case 'video':
        return 'The results are saved next to the source video.';
      default:
        return 'The result is saved next to the original file.';
    }
  });

  readonly terminalResult = computed<RecipeResultV1 | null>(() => {
    const result = this.result();
    return result && this.isTerminalStatus(result.status) ? result : null;
  });

  /**
   * Recipes report `partial_success` when the output exists but a target or
   * verification check needs attention, so the backend message replaces the
   * "retry them below" copy meant for failed inputs.
   */
  readonly partialDetail = computed(() => {
    const result = this.terminalResult();
    return result?.status === 'partial_success' ? (result.message ?? '') : '';
  });

  /**
   * Adapts the recipe snapshot to the shared ResultCard contract. Every final
   * output is its own row, so a six-file web set lists six files while Email
   * PDF still lists the one file it produced.
   */
  readonly resultJob = computed<JobResultV1 | null>(() => {
    const result = this.terminalResult();
    if (!result) {
      return null;
    }

    const status = result.status as JobExecutionStatusV1;
    const outputs = result.outputs ?? [];
    const items: JobResultItemV1[] = outputs.map((path) => ({
      inputPath: this.selectedPaths()[0] ?? '',
      outputPath: path,
      outputs: [path],
      outputCount: 1,
      success: status !== 'failed' && status !== 'cancelled',
      message: result.message,
    }));

    return {
      jobId: result.jobId,
      success: status === 'success' || status === 'partial_success',
      message: result.message,
      toolId: this.recipe()?.id ?? this.recipeId,
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
   * The live event for the job currently on screen, or null. Using it only
   * while running keeps stale events from other runs out of the page.
   */
  private readonly activeLiveProgress = computed<RecipeProgressEventV1 | null>(
    () => {
      if (!this.isRunning()) {
        return null;
      }

      const event = this.liveProgress();
      if (!event || event.jobId !== this.runJobId()) {
        return null;
      }

      return event;
    },
  );

  /**
   * Recipe steps with the per-step state merged in. Before a run every step is
   * pending; after the run starts the backend step snapshot takes over, and the
   * live event marks the active step between polls.
   */
  readonly steps = computed<RecipeStepView[]>(() => {
    const recipeSteps = this.recipe()?.steps ?? [];
    const result = this.result();
    const active = this.isRunning();
    const liveStep = this.activeLiveProgress()?.currentStep ?? 0;
    const currentStep = liveStep > 0 ? liveStep : (result?.currentStep ?? 0);

    return recipeSteps.map((step, index) => {
      const resultStep = result?.steps?.[index];
      let state = normalizeStepState(resultStep?.status);
      if (active && currentStep === index + 1 && state === 'pending') {
        state = 'running';
      }

      return {
        id: step.id,
        name: step.name,
        state,
        message: resultStep?.message ?? '',
        detail: step.detail ?? '',
      };
    });
  });

  readonly progressPercent = computed(() => {
    const result = this.result();
    if (result && this.isTerminalStatus(result.status)) {
      return 100;
    }

    const live = this.activeLiveProgress();
    if (live && live.percent >= 0) {
      return Math.min(100, Math.max(0, Math.round(live.percent)));
    }

    if (!result) {
      return 0;
    }
    if (result.totalSteps <= 0) {
      return 0;
    }
    const percent = Math.round((result.currentStep / result.totalSteps) * 100);
    return Math.min(100, Math.max(0, percent));
  });

  readonly progressLabel = computed(() => {
    const live = this.activeLiveProgress();
    const result = this.result();
    if (!live && !result) {
      return 'Starting…';
    }

    const total = live?.totalSteps || result?.totalSteps || this.steps().length;
    const currentStep = live?.currentStep || result?.currentStep || 1;
    return `Step ${Math.max(1, Math.min(currentStep, total))} of ${total}`;
  });

  readonly activeStepMessage = computed(() => {
    const live = this.activeLiveProgress();
    if (live?.message) {
      return live.message;
    }

    const active = this.steps().find((step) => step.state === 'running');
    return active?.message || this.result()?.message || '';
  });

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      const name = lowerFirst(this.recipe()?.name ?? 'the result');
      const article = /^[aeiou]/.test(name) ? 'an' : 'a';
      return `Add ${this.kindArticle()} to create ${article} ${name}.`;
    }
    if (this.targetInvalid()) {
      return 'Enter a target size larger than 0 MB.';
    }
    return '';
  });

  readonly settingsSub = computed(() =>
    this.showTargetSize()
      ? 'Set the target size and where the results are saved.'
      : 'Choose where the results are saved.',
  );

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

  private readonly destroy$ = new Subject<void>();
  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private planDirty = false;
  private pollInFlight = false;
  private fileItemsGeneration = 0;
  private targetTouched = false;

  async ngOnInit(): Promise<void> {
    const fallback = this.resolveFallback();
    this.recipe.set(fallback);
    this.applyDefaultTarget(fallback);

    const response = await this.wailsService.listRecipesV1().catch(() => ({
      success: false,
      message: 'The recipe catalog could not be read.',
      recipes: [] as RecipeV1[],
    }));
    if (!response.success) {
      return;
    }

    const found = response.recipes.find(
      (candidate) => candidate.id === this.recipeId,
    );
    if (!found) {
      return;
    }

    this.recipe.set(found);
    this.applyDefaultTarget(found);
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
    this.stopPolling();
    this.clearPlanRefreshTimer();
  }

  fileCountLabel(): string {
    const count = this.selectedPaths().length;
    return `${count} ${this.kindNoun(count)}`;
  }

  /** Panel titles stay concrete per input kind instead of saying "files". */
  inputTitle(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'PDFs';
      case 'image':
        return 'Image';
      case 'video':
        return 'Video';
      default:
        return 'Files';
    }
  }

  chooseLabel(): string {
    if (this.multipleInputs()) {
      return this.recipe()?.inputKind === 'pdf' ? 'Choose PDFs' : 'Choose files';
    }
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'Choose PDF';
      case 'image':
        return 'Choose image';
      case 'video':
        return 'Choose video';
      default:
        return 'Choose file';
    }
  }

  orderHint(): string {
    return this.recipe()?.inputKind === 'pdf'
      ? 'The order below is the merge order.'
      : 'Files run in the order shown.';
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const acceptedExtensions = new Set(
      (this.recipe()?.acceptExtensions ?? []).map((extension) =>
        extension.toLowerCase(),
      ),
    );

    const accepted: string[] = [];
    const rejected: string[] = [];
    for (const path of incoming) {
      if (!acceptedExtensions.has(fileExtension(path).toLowerCase())) {
        rejected.push(path);
        continue;
      }
      accepted.push(path);
    }

    if (!this.multipleInputs()) {
      if (accepted.length === 0) {
        this.notice.set(this.intakeNotice(rejected, [], 0));
        return;
      }

      const unique: string[] = [];
      const seen = new Set<string>();
      for (const path of accepted) {
        const key = this.pathKey(path);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        unique.push(path);
      }

      const extras = unique.length - 1;
      this.notice.set(this.intakeNotice(rejected, [], extras));
      this.actionError.set(null);
      this.selectedPaths.set([unique[0]]);
      this.syncFileItems();
      this.schedulePlanRefresh();
      return;
    }

    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(
      this.selectedPaths().map((path) => this.pathKey(path)),
    );

    for (const path of accepted) {
      const key = this.pathKey(path);
      if (known.has(key)) {
        duplicates.push(path);
        continue;
      }

      known.add(key);
      added.push(path);
    }

    this.notice.set(this.intakeNotice(rejected, duplicates, 0));
    if (added.length === 0) {
      return;
    }

    this.actionError.set(null);
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  async chooseFiles(): Promise<void> {
    if (this.multipleInputs()) {
      const paths = await this.wailsService.openMultipleFilesDialog();
      if (paths.length > 0) {
        this.onPathsSelected(paths);
      }
      return;
    }

    const path = (await this.wailsService.openFileDialog()).trim();
    if (path) {
      this.onPathsSelected([path]);
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
    this.liveProgress.set(null);
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

  useDefaultFolder(): void {
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
    this.liveProgress.set(null);
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
    this.liveProgress.set(null);
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

  private resolveRecipeId(): string {
    const data = this.route.snapshot?.data ?? {};
    const candidate =
      typeof data['recipeId'] === 'string' ? data['recipeId'].trim() : '';
    return candidate || DEFAULT_RECIPE_ID;
  }

  private resolveFallback(): RecipeV1 | null {
    return (
      findRecipeFallback(this.recipeId) ?? findRecipeFallback(DEFAULT_RECIPE_ID)
    );
  }

  private buildRequest(): RecipeRequestV1 {
    const options: Record<string, unknown> = {};
    if (this.showTargetSize()) {
      options['targetSizeBytes'] = this.targetBytes();
    }

    return {
      recipeId: this.recipe()?.id ?? this.recipeId,
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options,
    };
  }

  private applyDefaultTarget(recipe: RecipeV1 | null): void {
    if (this.targetTouched) {
      return;
    }
    const defaultMb = defaultTargetMb(recipe);
    this.targetMb.set(defaultMb);
    this.targetText.set(targetInputText(defaultMb));
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
        this.liveProgress.set(null);
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
    if (
      info.kind === 'video' &&
      typeof info.durationSeconds === 'number' &&
      info.durationSeconds > 0
    ) {
      parts.push(formatDuration(info.durationSeconds));
    }
    if (
      typeof info.width === 'number' &&
      info.width > 0 &&
      typeof info.height === 'number' &&
      info.height > 0
    ) {
      parts.push(`${info.width} × ${info.height} px`);
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(formatCount(info.pageCount, 'page'));
    }
    return parts.join(' · ');
  }

  private intakeNotice(
    rejected: string[],
    duplicates: string[],
    extraAccepted: number,
  ): string {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        `${fileName(rejected[0])} wasn't added because it isn't ${this.kindArticle()}.`,
      );
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(
        `${names} weren't added because they aren't ${this.kindNoun(2)}.`,
      );
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(
        `${duplicates.length} ${this.kindNoun(duplicates.length)} are already in the queue.`,
      );
    }

    if (extraAccepted > 0) {
      parts.push(`Only the first ${this.inputKindLabel()} is used.`);
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

  private kindLabel(kind: string): string {
    switch (kind) {
      case 'pdf':
        return 'PDF';
      case 'image':
        return 'image';
      case 'video':
        return 'video';
      default:
        return 'file';
    }
  }

  private kindArticle(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'a PDF';
      case 'image':
        return 'an image';
      case 'video':
        return 'a video';
      default:
        return 'a file';
    }
  }

  private kindNoun(count: number): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return count === 1 ? 'PDF' : 'PDFs';
      case 'image':
        return count === 1 ? 'image' : 'images';
      case 'video':
        return count === 1 ? 'video' : 'videos';
      default:
        return count === 1 ? 'file' : 'files';
    }
  }

  private defaultDestinationLabel(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return 'Next to the first PDF';
      case 'image':
        return 'Next to the image';
      case 'video':
        return 'Next to the video';
      default:
        return 'Next to the original';
    }
  }
}
