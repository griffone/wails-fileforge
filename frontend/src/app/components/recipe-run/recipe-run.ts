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
import { Subscription } from 'rxjs';

import { FileDrop } from '../file-drop/file-drop';
import { FileQueue, FileQueueReorder } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { findRecipeFallback } from '../../config/recipes';
import type { TranslationKey } from '../../i18n';
import { ActivityService } from '../../services/activity.service';
import { I18nService } from '../../services/i18n.service';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../../services/tool-error';
import {
  FileInfo,
  JobError,
  JobExecutionStatus,
  JobResultItem,
  JobResult,
  RecipePlanResponse,
  RecipeProgressEvent,
  RecipeRequest,
  RecipeResult,
  RecipeStatusResponse,
  Recipe,
  RecipeOptionField,
  RunRecipeResponse,
  VerificationReport,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
  formatDuration,
} from '../../utils/format';
import { observeRecipe } from '../../utils/job-stream';
import { resumeRecipeRun } from '../../utils/resume-task';

const DEFAULT_RECIPE_ID = 'recipe.email_pdf';
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

function defaultTargetMb(recipe: Recipe | null): number {
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
  private readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);
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
  readonly recipe = signal<Recipe | null>(this.resolveFallback());

  readonly plan = signal<RecipePlanResponse | null>(null);
  readonly planLoading = signal(false);

  readonly result = signal<RecipeResult | null>(null);
  readonly runJobId = signal('');
  /** Last live event from `recipes/progress` for the current job. */
  readonly liveProgress = signal<RecipeProgressEvent | null>(null);
  readonly isSubmitting = signal(false);
  readonly isRunning = signal(false);
  readonly cancelRequested = signal(false);
  readonly actionError = signal<JobError | null>(null);
  readonly notice = signal('');

  readonly fileName = fileName;

  /**
   * Recipes with a fixed outcome (no target size) hide the field entirely.
   * User recipes always run with their saved overrides, so the built-in target
   * input would either duplicate or contradict them.
   */
  readonly showTargetSize = computed(
    () =>
      !this.recipe()?.userDefined &&
      (this.recipe()?.defaultTargetSizeBytes ?? 0) > 0,
  );

  /** Read-only summary of the declared options a user recipe runs with. */
  readonly userOptionSummaries = computed(() => {
    const recipe = this.recipe();
    if (!recipe?.userDefined) {
      return [] as { key: string; label: string; value: string }[];
    }

    return (recipe.optionSchema ?? []).map((field) => {
      const raw =
        recipe.optionOverrides?.[field.key] ?? field.default ?? '';
      return {
        key: field.key,
        label: this.i18n.tOr(
          `recipeRun.option.${field.key}`,
          field.label,
        ),
        value: this.formatOptionValue(field, raw),
      };
    });
  });

  /** Only `maxInputs === 1` restricts the queue; 0 means unlimited. */
  readonly multipleInputs = computed(
    () => (this.recipe()?.maxInputs ?? 0) !== 1,
  );

  readonly recipeHighlights = computed(() =>
    this.i18n.tList(
      `recipes.${this.recipeSlug()}.highlights` as TranslationKey,
      this.recipe()?.highlights ?? [],
    ),
  );

  /** Recipe name overlaid by id; config keeps the fallback. */
  readonly recipeName = computed(() => {
    const recipe = this.recipe();
    return this.i18n.tOr(
      `tools.${recipe?.id ?? this.recipeId}.name`,
      recipe?.name ?? '',
    );
  });

  /** Recipe description overlaid by id; config keeps the fallback. */
  readonly recipeDescription = computed(() => {
    const recipe = this.recipe();
    return this.i18n.tOr(
      `tools.${recipe?.id ?? this.recipeId}.description`,
      recipe?.description ?? '',
    );
  });

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
        return this.i18n.t('recipeRun.input.one', { kind });
      }
      return this.i18n.t('recipeRun.input.oneWithExtensions', {
        kind,
        extensions,
      });
    }

    return this.i18n.t('recipeRun.input.many', {
      extensions: this.extensionsLabel(),
    });
  });

  readonly dropTitle = computed(() => {
    if (this.multipleInputs()) {
      return this.recipe()?.inputKind === 'pdf'
        ? this.i18n.t('recipeRun.drop.pdfMany')
        : this.i18n.t('recipeRun.drop.filesMany');
    }
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t('recipeRun.drop.pdf');
      case 'image':
        return this.i18n.t('recipeRun.drop.image');
      case 'video':
        return this.i18n.t('recipeRun.drop.video');
      default:
        return this.i18n.t('recipeRun.drop.file');
    }
  });

  private readonly titleParts = computed(() => splitTitle(this.recipeName()));

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
      return this.i18n.t('recipeRun.action.preparing');
    }
    if (this.isRunning()) {
      return this.i18n.t('recipeRun.action.creating');
    }
    return this.i18n.t('recipeRun.action.create', { name: this.recipeName() });
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
      return allPdf
        ? this.i18n.t('recipeRun.result.pdf')
        : this.i18n.t('recipeRun.result.file');
    }
    return this.recipe()?.inputKind === 'pdf'
      ? this.i18n.t('recipeRun.result.pdf')
      : this.i18n.t('recipeRun.result.file');
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
        return this.i18n.t('recipeRun.output.helpPdf');
      case 'image':
        return this.i18n.t('recipeRun.output.helpImage');
      case 'video':
        return this.i18n.t('recipeRun.output.helpVideo');
      default:
        return this.i18n.t('recipeRun.output.helpFile');
    }
  });

  readonly terminalResult = computed<RecipeResult | null>(() => {
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
  readonly resultJob = computed<JobResult | null>(() => {
    const result = this.terminalResult();
    if (!result) {
      return null;
    }

    const status = result.status as JobExecutionStatus;
    const outputs = result.outputs ?? [];
    const items: JobResultItem[] = outputs.map((path) => ({
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

  readonly verification = computed<VerificationReport | null>(
    () => this.result()?.verification ?? null,
  );

  /**
   * The live event for the job currently on screen, or null. Using it only
   * while running keeps stale events from other runs out of the page.
   */
  private readonly activeLiveProgress = computed<RecipeProgressEvent | null>(
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
   * live event marks the active step until the next snapshot arrives.
   */
  readonly steps = computed<RecipeStepView[]>(() => {
    const recipeSteps = this.recipe()?.steps ?? [];
    const result = this.result();
    const active = this.isRunning();
    const liveStep = this.activeLiveProgress()?.currentStep ?? 0;
    const currentStep = liveStep > 0 ? liveStep : (result?.currentStep ?? 0);
    const slug = this.recipeSlug();

    return recipeSteps.map((step, index) => {
      const resultStep = result?.steps?.[index];
      let state = normalizeStepState(resultStep?.status);
      if (active && currentStep === index + 1 && state === 'pending') {
        state = 'running';
      }

      return {
        id: step.id,
        name: this.i18n.tOr(
          `recipes.${slug}.step.${step.id}.name`,
          step.name,
        ),
        state,
        message: resultStep?.message ?? '',
        detail: this.i18n.tOr(
          `recipes.${slug}.step.${step.id}.detail`,
          step.detail ?? '',
        ),
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
      return this.i18n.t('recipeRun.progress.starting');
    }

    const total = live?.totalSteps || result?.totalSteps || this.steps().length;
    const currentStep = live?.currentStep || result?.currentStep || 1;
    return this.i18n.t('recipeRun.progress.step', {
      current: Math.max(1, Math.min(currentStep, total)),
      total,
    });
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
      return this.i18n.t('recipeRun.action.add', {
        article: this.kindArticle(),
        name: this.recipeName(),
      });
    }
    if (this.targetInvalid()) {
      return this.i18n.t('recipeRun.target.invalid');
    }
    return '';
  });

  readonly settingsSub = computed(() =>
    this.showTargetSize()
      ? this.i18n.t('recipeRun.settings.subWithTarget')
      : this.i18n.t('recipeRun.settings.sub'),
  );

  readonly runErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.actionError() ?? this.terminalResult()?.error;
    if (error) {
      return localizeToolError(
        (key) => this.i18n.t(key),
        describeToolError(error, this.i18n.t('recipeRun.error.finish')),
      );
    }

    const result = this.terminalResult();
    if (result?.status === 'failed' && result.message.trim()) {
      return localizeToolError(
        (key) => this.i18n.t(key),
        describeToolError(
          { code: 'EXEC_IO_TRANSIENT', message: result.message },
          this.i18n.t('recipeRun.error.finish'),
        ),
      );
    }

    return null;
  });

  private runSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private planDirty = false;
  private fileItemsGeneration = 0;
  private targetTouched = false;

  async ngOnInit(): Promise<void> {
    this.resumeBackgroundRun();

    const fallback = this.resolveFallback();
    this.recipe.set(fallback);
    this.applyDefaultTarget(fallback);

    const response = await this.wailsService.listRecipes().catch(() => ({
      success: false,
      message: this.i18n.t('recipeRun.error.catalog'),
      recipes: [] as Recipe[],
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
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  fileCountLabel(): string {
    const count = this.selectedPaths().length;
    return `${count} ${this.kindNoun(count)}`;
  }

  /** Recipe id without the `recipe.` prefix; keys the recipe dictionaries. */
  recipeSlug(): string {
    return (this.recipe()?.id ?? this.recipeId).replace(/^recipe\./, '');
  }

  /** Panel titles stay concrete per input kind instead of saying "files". */
  inputTitle(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t('recipeRun.inputTitle.pdf');
      case 'image':
        return this.i18n.t('recipeRun.inputTitle.image');
      case 'video':
        return this.i18n.t('recipeRun.inputTitle.video');
      default:
        return this.i18n.t('recipeRun.inputTitle.file');
    }
  }

  chooseLabel(): string {
    if (this.multipleInputs()) {
      return this.recipe()?.inputKind === 'pdf'
        ? this.i18n.t('recipeRun.choose.pdfMany')
        : this.i18n.t('recipeRun.choose.filesMany');
    }
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t('recipeRun.choose.pdf');
      case 'image':
        return this.i18n.t('recipeRun.choose.image');
      case 'video':
        return this.i18n.t('recipeRun.choose.video');
      default:
        return this.i18n.t('recipeRun.choose.file');
    }
  }

  orderHint(): string {
    return this.recipe()?.inputKind === 'pdf'
      ? this.i18n.t('recipeRun.order.pdf')
      : this.i18n.t('recipeRun.order.files');
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
    this.stopObserving();
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

  onReorder(event: FileQueueReorder): void {
    if (!this.multipleInputs()) {
      return;
    }
    const paths = this.selectedPaths();
    const from = paths.indexOf(event.path);
    if (from < 0 || event.to < 0 || event.to >= paths.length || event.to === from) {
      return;
    }
    const next = [...paths];
    next.splice(from, 1);
    next.splice(event.to, 0, event.path);
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.schedulePlanRefresh();
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
    this.stopObserving();

    if (this.planLoading() || this.planDirty || !this.plan()?.success) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    this.isSubmitting.set(true);
    const response: RunRecipeResponse = await this.wailsService
      .runRecipe(this.buildRequest())
      .catch(() => ({
        success: false,
        message: this.i18n.t('recipeRun.error.start'),
        jobId: '',
        status: 'failed',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_RUN_FAILED',
          message: this.i18n.t('recipeRun.error.start'),
        },
      }));
    this.isSubmitting.set(false);

    if (!response.success || !response.jobId) {
      this.actionError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_RUN_FAILED',
          message: response.message || this.i18n.t('recipeRun.error.start'),
        },
      );
      return;
    }

    this.runJobId.set(response.jobId);
    this.startObserving();
  }

  async cancel(): Promise<void> {
    const jobId = this.runJobId();
    if (!jobId || this.cancelRequested()) {
      return;
    }

    this.cancelRequested.set(true);
    const response: RecipeStatusResponse = await this.wailsService
      .cancelRecipe(jobId)
      .catch(() => ({
        success: false,
        found: false,
        message: this.i18n.t('recipeRun.error.cancel'),
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_CANCEL_FAILED',
          message: this.i18n.t('recipeRun.error.cancel'),
        },
      }));
    if (!response.success) {
      this.actionError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_CANCEL_FAILED',
          message: response.message || this.i18n.t('recipeRun.error.cancel'),
        },
      );
    }
  }

  async openPath(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.notice.set(
      response.success
        ? ''
        : response.message || this.i18n.t('recipeRun.error.openFile'),
    );
  }

  async revealInFolder(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.notice.set(
      response.success
        ? ''
        : response.message || this.i18n.t('recipeRun.error.openFolder'),
    );
  }

  resetRun(): void {
    this.stopObserving();
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
        return this.i18n.t('recipeRun.step.success');
      case 'failed':
        return this.i18n.t('recipeRun.step.failed');
      case 'skipped':
        return this.i18n.t('recipeRun.step.skipped');
      case 'running':
        return this.i18n.t('recipeRun.step.running');
      default:
        return this.i18n.t('recipeRun.step.pending');
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
    const params = this.route.snapshot?.params ?? {};
    const fromParams =
      typeof params['recipeId'] === 'string' ? params['recipeId'].trim() : '';
    if (fromParams) {
      return fromParams;
    }

    const data = this.route.snapshot?.data ?? {};
    const candidate =
      typeof data['recipeId'] === 'string' ? data['recipeId'].trim() : '';
    return candidate || DEFAULT_RECIPE_ID;
  }

  private resolveFallback(): Recipe | null {
    const fallback = findRecipeFallback(this.recipeId);
    if (fallback) {
      return fallback;
    }

    // A user recipe has no static config: the catalog must provide it.
    if (this.recipeId.startsWith('recipe.user.')) {
      return null;
    }

    return findRecipeFallback(DEFAULT_RECIPE_ID);
  }

  /** Renders a stored option value in the declared display unit. */
  private formatOptionValue(field: RecipeOptionField, raw: unknown): string {
    if (field.kind === 'integer') {
      const numeric = typeof raw === 'number' ? raw : Number(raw);
      if (!Number.isFinite(numeric)) {
        return String(raw ?? '');
      }
      const scale = field.scale && field.scale > 0 ? field.scale : 1;
      const display = String(Number((numeric / scale).toFixed(2)));
      return field.unit ? `${display} ${field.unit}` : display;
    }

    if (field.kind === 'boolean') {
      return raw === true ? this.i18n.t('common.yes') : this.i18n.t('common.no');
    }

    if (field.kind === 'enum') {
      const choice = (field.values ?? []).find(
        (candidate) => candidate.value === raw,
      );
      return choice
        ? this.i18n.tOr(`recipeRun.choice.${choice.value}`, choice.label)
        : String(raw ?? '');
    }

    return String(raw ?? '');
  }

  private buildRequest(): RecipeRequest {
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

  private applyDefaultTarget(recipe: Recipe | null): void {
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

    const response: RecipePlanResponse = await this.wailsService
      .planRecipe(this.buildRequest())
      .catch(() => ({
        success: false,
        message: this.i18n.t('recipeRun.error.plan'),
        inputPaths: [],
        outputDir: '',
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RECIPE_PLAN_FAILED',
          message: this.i18n.t('recipeRun.error.plan'),
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

  private resumeBackgroundRun(): void {
    resumeRecipeRun({
      activity: this.activity,
      recipeId: this.recipeId,
      runJobId: this.runJobId,
      startObserving: () => this.startObserving(),
    });
  }

  private startObserving(): void {
    this.stopObserving();
    const jobId = this.runJobId();
    if (!jobId) {
      return;
    }

    this.isRunning.set(true);
    this.runSubscription = observeRecipe(this.wailsService, jobId, {
      onProgress: (event) => this.applyRecipeProgress(event),
      onState: (result) => this.applyRecipeStatus(result),
      onMissing: (response) => this.handleMissingRun(response),
    });
  }

  private stopObserving(): void {
    if (this.runSubscription) {
      this.runSubscription.unsubscribe();
      this.runSubscription = null;
    }
    this.isRunning.set(false);
  }

  private applyRecipeProgress(event: RecipeProgressEvent): void {
    if (!event) {
      return;
    }

    this.liveProgress.set(event);
    if (this.isTerminalStatus(event.status)) {
      void this.reconcileRecipeStatus();
    }
  }

  /**
   * One-shot recovery when a terminal progress event arrives before its
   * recipes/state snapshot. This is never a timer.
   */
  private async reconcileRecipeStatus(): Promise<void> {
    const jobId = this.runJobId();
    if (!jobId) {
      return;
    }

    const response = await this.wailsService
      .getRecipeStatus(jobId)
      .catch(() => null);
    if (!response || this.runJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.handleMissingRun(response);
      return;
    }

    this.applyRecipeStatus(response.result);
  }

  private applyRecipeStatus(result: RecipeResult): void {
    if (!result) {
      return;
    }

    this.result.set(result);
    if (this.isTerminalStatus(result.status)) {
      this.liveProgress.set(null);
      this.stopObserving();
      if (result.status === 'cancelled') {
        this.notice.set(this.i18n.t('recipeRun.notice.cancelled'));
      }
    }
  }

  /** The backend no longer tracks the adopted run (restart or retention). */
  private handleMissingRun(response: RecipeStatusResponse): void {
    if (!this.runJobId()) {
      return;
    }

    this.stopObserving();
    this.actionError.set(
      response.error ?? {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'RECIPE_STATUS_MISSING',
        message: response.message || this.i18n.t('recipeRun.error.status'),
      },
    );
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
      const response = await this.wailsService.inspectFiles(paths);
      if (generation !== this.fileItemsGeneration || !response.success) {
        return;
      }

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
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }
  }

  private describeFileInfo(info: FileInfo): string {
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

  private intakeNotice(
    rejected: string[],
    duplicates: string[],
    extraAccepted: number,
  ): string {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        this.i18n.t('recipeRun.notice.rejectedOne', {
          file: fileName(rejected[0]),
          article: this.kindArticle(),
        }),
      );
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(
        this.i18n.t('recipeRun.notice.rejectedMany', {
          files: names,
          noun: this.kindNoun(2),
        }),
      );
    }

    if (duplicates.length === 1) {
      parts.push(
        this.i18n.t('recipeRun.notice.duplicateOne', {
          file: fileName(duplicates[0]),
        }),
      );
    } else if (duplicates.length > 1) {
      parts.push(
        this.i18n.t('recipeRun.notice.duplicateMany', {
          count: duplicates.length,
          noun: this.kindNoun(duplicates.length),
        }),
      );
    }

    if (extraAccepted > 0) {
      parts.push(
        this.i18n.t('recipeRun.notice.onlyFirst', {
          kind: this.inputKindLabel(),
        }),
      );
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
        return this.i18n.t('recipeRun.kind.pdf');
      case 'image':
        return this.i18n.t('recipeRun.kind.image');
      case 'video':
        return this.i18n.t('recipeRun.kind.video');
      default:
        return this.i18n.t('recipeRun.kind.file');
    }
  }

  private kindArticle(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t('recipeRun.kind.pdfArticle');
      case 'image':
        return this.i18n.t('recipeRun.kind.imageArticle');
      case 'video':
        return this.i18n.t('recipeRun.kind.videoArticle');
      default:
        return this.i18n.t('recipeRun.kind.fileArticle');
    }
  }

  private kindNoun(count: number): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t(
          count === 1 ? 'recipeRun.kind.pdf' : 'recipeRun.kind.pdfMany',
        );
      case 'image':
        return this.i18n.t(
          count === 1 ? 'recipeRun.kind.image' : 'recipeRun.kind.imageMany',
        );
      case 'video':
        return this.i18n.t(
          count === 1 ? 'recipeRun.kind.video' : 'recipeRun.kind.videoMany',
        );
      default:
        return this.i18n.t(
          count === 1 ? 'recipeRun.kind.file' : 'recipeRun.kind.fileMany',
        );
    }
  }

  private defaultDestinationLabel(): string {
    switch (this.recipe()?.inputKind) {
      case 'pdf':
        return this.i18n.t('recipeRun.output.nextToPdf');
      case 'image':
        return this.i18n.t('recipeRun.output.nextToImage');
      case 'video':
        return this.i18n.t('recipeRun.output.nextToVideo');
      default:
        return this.i18n.t('recipeRun.output.nextToFile');
    }
  }
}
