import {
  Component,
  NgZone,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  ArrowRight,
  FolderOpen,
  ListChecks,
  LucideAngularModule,
  Pencil,
  Plus,
  ShieldCheck,
  Trash2,
  TriangleAlert,
  Workflow,
} from 'lucide-angular';
import { Subject, takeUntil } from 'rxjs';

import type { TranslationKey, TranslationParams } from '../../i18n';
import {
  AutomationOrganizePreviewResponse,
  AutomationRule,
  AutomationRunEvent,
  AutomationRunRecord,
  AutomationStatusResponse,
  ListAutomationRunsResponse,
  ListAutomationRulesResponse,
  OrganizePlannedMove,
  Recipe,
  Wails,
} from '../../services/wails';
import { I18nService } from '../../services/i18n.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import {
  fileName,
  formatBytes,
  formatCount,
  formatRelativeTime,
} from '../../utils/format';

type ActionTone = 'success' | 'warning' | 'danger';

interface ActionMessage {
  tone: ActionTone;
  text: string;
}

interface LastRunSummary {
  at: number;
  status: string;
  message: string;
}

/** One inline template problem, resolved to copy only when it is rendered. */
interface TemplateIssue {
  key: TranslationKey;
  params?: TranslationParams;
}

type RuleAction = 'recipe' | 'organize';
type CollisionPolicy = 'skip' | 'suffix';

const OUTPUT_PREVIEW_LIMIT = 3;
const RUN_HISTORY_LIMIT = 20;

/** Preview rows rendered before the "show all" control. */
const PREVIEW_VISIBLE_MOVES = 20;

const RECIPE_ACTION: RuleAction = 'recipe';
const ORGANIZE_ACTION: RuleAction = 'organize';
const COLLISION_SKIP: CollisionPolicy = 'skip';
const COLLISION_SUFFIX: CollisionPolicy = 'suffix';

/**
 * Synthetic id the undo history uses for organize moves. It is not a tool and
 * not a recipe, so a history row that carries it must never be shown raw.
 */
const ORGANIZE_HISTORY_TOOL_ID = 'automation.organize';

/**
 * Tokens the backend accepts, mirrored from internal/automation/template.go.
 * The list also feeds the inline error, so a user always sees what exists.
 */
const ORGANIZE_TOKENS = [
  'name',
  'ext',
  'year',
  'month',
  'date',
  'title',
] as const;

type OrganizeToken = (typeof ORGANIZE_TOKENS)[number];

const ORGANIZE_TOKEN_HELP: Readonly<Record<OrganizeToken, TranslationKey>> = {
  name: 'automations.organize.token.name',
  ext: 'automations.organize.token.ext',
  year: 'automations.organize.token.year',
  month: 'automations.organize.token.month',
  date: 'automations.organize.token.date',
  title: 'automations.organize.token.title',
};

/** Only terminal runs are kept: a `running` event is progress, not history. */
const TERMINAL_RUN_STATUSES = new Set([
  'success',
  'partial_success',
  'failed',
  'cancelled',
  'interrupted',
]);

@Component({
  selector: 'app-automations',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './automations.html',
  styleUrl: './automations.css',
})
export class Automations implements OnInit, OnDestroy {
  private readonly wailsService = inject(Wails);
  private readonly zone = inject(NgZone);
  private readonly registry = inject(ToolRegistryService);

  readonly i18n = inject(I18nService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly plusIcon = Plus;
  readonly editIcon = Pencil;
  readonly trashIcon = Trash2;
  readonly alertIcon = TriangleAlert;
  readonly workflowIcon = Workflow;
  readonly previewIcon = ListChecks;
  readonly arrowIcon = ArrowRight;

  readonly rules = signal<AutomationRule[]>([]);
  readonly recipes = signal<Recipe[]>([]);
  readonly status = signal<AutomationStatusResponse | null>(null);
  readonly runs = signal<AutomationRunRecord[]>([]);

  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly errorMessage = signal('');
  readonly actionMessage = signal<ActionMessage | null>(null);

  readonly runsLoading = signal(false);
  readonly confirmClearRuns = signal(false);

  readonly formOpen = signal(false);
  readonly editingRule = signal<AutomationRule | null>(null);
  readonly editingId = computed(() => this.editingRule()?.id ?? '');

  readonly formName = signal('');
  readonly formAction = signal<RuleAction>(RECIPE_ACTION);
  readonly formWatchDir = signal('');
  readonly formRecipeId = signal('');
  readonly formExtensionsText = signal('');
  readonly formRecursive = signal(false);
  readonly formPatternsText = signal('');
  readonly formOutputDir = signal('');
  readonly formDeleteSource = signal(false);
  readonly formEnabled = signal(true);

  readonly formTargetDir = signal('');
  readonly formFolderTemplate = signal('');
  readonly formNameTemplate = signal('');
  readonly formCollisionPolicy = signal<CollisionPolicy>(COLLISION_SKIP);

  readonly previewLoading = signal(false);
  readonly previewMoves = signal<OrganizePlannedMove[]>([]);
  readonly previewStored = signal(false);
  readonly previewError = signal('');
  readonly previewExpanded = signal(false);

  /**
   * Configuration key of the last successful preview, and of the preview the
   * server stored on the saved rule. Both mirror organizeConfigHash: the same
   * fields, trimmed and with sorted lists, so a relevant edit always breaks the
   * comparison and no unrelated one does.
   */
  private readonly lastPreviewKey = signal('');
  private readonly storedPreviewKey = signal('');

  readonly formError = signal('');
  readonly formAttempted = signal(false);

  readonly confirmDeleteId = signal('');

  readonly selectedRecipe = computed(
    () =>
      this.recipes().find((recipe) => recipe.id === this.formRecipeId()) ?? null,
  );

  readonly acceptedExtensionsHelp = computed(() => {
    const extensions = this.selectedRecipe()?.acceptExtensions ?? [];
    return extensions.map((extension) => `.${extension}`).join(', ');
  });

  readonly extensionsPlaceholder = computed(() =>
    (this.selectedRecipe()?.acceptExtensions ?? []).join(', '),
  );

  /**
   * Extensions are shared by both actions, but an organize rule has no recipe to
   * inherit them from: empty means every accepted file name.
   */
  readonly extensionsHelpKey = computed<TranslationKey>(() =>
    this.isOrganize()
      ? 'automations.form.extensionsHelpOrganize'
      : 'automations.form.extensionsHelp',
  );

  readonly extensionsPlaceholderText = computed(() => {
    if (this.isOrganize()) {
      return this.i18n.t('automations.form.extensionsPlaceholderOrganize');
    }
    return this.extensionsPlaceholder();
  });

  readonly nameInvalid = computed(
    () => this.formAttempted() && this.formName().trim().length === 0,
  );

  readonly watchDirInvalid = computed(
    () => this.formAttempted() && this.formWatchDir().trim().length === 0,
  );

  readonly recipeInvalid = computed(
    () =>
      !this.isOrganize() &&
      this.formAttempted() &&
      this.formRecipeId().trim().length === 0,
  );

  readonly isOrganize = computed(() => this.formAction() === ORGANIZE_ACTION);

  readonly targetDirInvalid = computed(
    () => this.isOrganize() && this.formAttempted() && !this.formTargetDir().trim(),
  );

  /**
   * Patterns are matched against the file name only, so a folder separator or
   * a "**" can never work. The backend is the source of truth; this catches the
   * obvious cases before a round trip and keeps the message next to the field.
   */
  readonly patternsError = computed(() => {
    for (const pattern of this.parsePatterns(this.formPatternsText())) {
      if (/[\\/]/.test(pattern) || pattern.includes('**')) {
        return this.i18n.t('automations.validation.patternsScope');
      }
      if (this.unclosedCharacterClass(pattern)) {
        return this.i18n.t('automations.validation.patternInvalid', {
          pattern,
        });
      }
    }

    return '';
  });

  readonly patternsInvalid = computed(
    () =>
      this.patternsError().length > 0 &&
      (this.formAttempted() || this.formPatternsText().trim().length > 0),
  );

  /** One-line meaning for every token the templates accept. */
  readonly tokenHelp = computed(() =>
    ORGANIZE_TOKENS.map((token) => ({
      token: `{${token}}`,
      help: this.i18n.t(ORGANIZE_TOKEN_HELP[token]),
    })),
  );

  readonly folderTemplateIssue = computed<TemplateIssue | null>(() =>
    this.inspectFolderTemplate(this.formFolderTemplate()),
  );

  readonly nameTemplateIssue = computed<TemplateIssue | null>(() =>
    this.inspectNameTemplate(this.formNameTemplate()),
  );

  readonly folderTemplateInvalid = computed(
    () =>
      this.isOrganize() &&
      this.folderTemplateIssue() !== null &&
      (this.formAttempted() ||
        this.formFolderTemplate().trim().length > 0),
  );

  readonly nameTemplateInvalid = computed(
    () =>
      this.isOrganize() &&
      this.nameTemplateIssue() !== null &&
      (this.formAttempted() || this.formNameTemplate().trim().length > 0),
  );

  readonly folderTemplateError = computed(() =>
    this.issueMessage(this.folderTemplateIssue()),
  );

  readonly nameTemplateError = computed(() =>
    this.issueMessage(this.nameTemplateIssue()),
  );

  /** Configuration key of the fields the server hashes for the enable gate. */
  readonly formConfigKey = computed(() =>
    this.configKey({
      action: this.formAction(),
      watchDir: this.formWatchDir(),
      extensions: this.parseExtensions(this.formExtensionsText()),
      recursive: this.formRecursive(),
      patterns: this.parsePatterns(this.formPatternsText()),
      targetDir: this.formTargetDir(),
      folderTemplate: this.formFolderTemplate(),
      nameTemplate: this.formNameTemplate(),
      collisionPolicy: this.formCollisionPolicy(),
    }),
  );

  /**
   * True when the saved rule carries a preview of exactly these settings.
   * A preview of unsaved edits never opens the gate, because the server still
   * refuses to enable a rule whose stored hash does not match.
   */
  readonly previewCurrent = computed(
    () =>
      !this.isOrganize() || this.storedPreviewKey() === this.formConfigKey(),
  );

  /** A preview ran, but the settings changed afterwards. */
  readonly previewStale = computed(() => {
    const last = this.lastPreviewKey();
    return (
      this.isOrganize() && last.length > 0 && last !== this.formConfigKey()
    );
  });

  /** The rendered plan belongs to the current settings. */
  readonly previewVisible = computed(() => {
    const last = this.lastPreviewKey();
    return (
      this.isOrganize() && last.length > 0 && last === this.formConfigKey()
    );
  });

  readonly enableGateActive = computed(
    () => this.isOrganize() && !this.previewCurrent(),
  );

  readonly enableGateHint = computed(() =>
    this.i18n.t(this.enableGateHintKey()),
  );

  readonly previewTotals = computed(() => {
    let planned = 0;
    let skipped = 0;
    let bytes = 0;
    for (const move of this.previewMoves()) {
      if (move.action === 'move') {
        planned += 1;
        bytes += move.sizeBytes ?? 0;
      } else {
        skipped += 1;
      }
    }

    return { planned, skipped, bytes };
  });

  readonly previewSizeLabel = computed(() =>
    formatBytes(this.previewTotals().bytes),
  );

  readonly visiblePreviewMoves = computed(() =>
    this.previewExpanded()
      ? this.previewMoves()
      : this.previewMoves().slice(0, PREVIEW_VISIBLE_MOVES),
  );

  readonly hasMorePreviewMoves = computed(
    () => this.previewMoves().length > PREVIEW_VISIBLE_MOVES,
  );

  readonly totalRuleCount = computed(
    () => this.status()?.totalRules ?? this.rules().length,
  );

  readonly activeRuleCount = computed(() => {
    const status = this.status();
    if (status) {
      return status.activeRules;
    }
    return this.rules().filter((rule) => rule.enabled).length;
  });

  /** Most recent run: the summary first, then the rules as a local fallback. */
  readonly lastRun = computed<LastRunSummary | null>(() => {
    const status = this.status();
    if (status && (status.lastRunAt ?? 0) > 0) {
      return {
        at: status.lastRunAt ?? 0,
        status: status.lastStatus ?? '',
        message: status.lastMessage ?? '',
      };
    }

    const latest = this.rules()
      .filter((rule) => (rule.lastRunAt ?? 0) > 0)
      .sort((left, right) => (right.lastRunAt ?? 0) - (left.lastRunAt ?? 0))[0];

    return latest
      ? {
          at: latest.lastRunAt ?? 0,
          status: latest.lastStatus ?? '',
          message: latest.lastMessage ?? '',
        }
      : null;
  });

  private readonly destroy$ = new Subject<void>();

  constructor() {
    this.wailsService.automationRun$
      ?.pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        this.zone.run(() => this.applyRunEvent(event));
      });
  }

  async ngOnInit(): Promise<void> {
    this.loading.set(true);
    this.runsLoading.set(true);
    try {
      const [recipesResponse, rulesResponse, statusResponse, runsResponse] =
        await Promise.all([
          this.wailsService.listRecipes().catch(() => ({
            success: false,
            message: this.i18n.t('automations.error.recipes'),
            recipes: [] as Recipe[],
          })),
          this.wailsService.listAutomationRules().catch(() => ({
            success: false,
            message: this.i18n.t('automations.error.rules'),
            rules: [] as AutomationRule[],
          })),
          this.wailsService.getAutomationStatus().catch(() => ({
            success: false,
            message: this.i18n.t('automations.error.status'),
            totalRules: 0,
            activeRules: 0,
          })),
          this.wailsService.listAutomationRuns(RUN_HISTORY_LIMIT).catch(
            () => ({
              success: false,
              message: this.i18n.t('automations.error.runs'),
              runs: [] as AutomationRunRecord[],
            }),
          ),
        ]);

      if (recipesResponse.success) {
        this.recipes.set(recipesResponse.recipes ?? []);
      } else {
        this.recipes.set([]);
        this.setError(recipesResponse.message);
      }

      this.applyRulesResponse(rulesResponse);
      this.applyStatusResponse(statusResponse);
      this.applyRunsResponse(runsResponse);
    } finally {
      this.loading.set(false);
      this.runsLoading.set(false);
    }
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
  }

  recipeLabel(recipe: Recipe): string {
    const definition = this.registry.recipes.find(
      (candidate) => candidate.id === recipe.id,
    );
    return definition
      ? this.registry.recipeName(definition)
      : this.i18n.tOr(`tools.${recipe.id}.name`, recipe.name);
  }

  recipeName(rule: AutomationRule): string {
    const recipe = this.recipes().find(
      (candidate) => candidate.id === rule.recipeId,
    );
    return recipe ? this.recipeLabel(recipe) : rule.recipeId;
  }

  isOrganizeRule(rule: AutomationRule): boolean {
    return rule.action === ORGANIZE_ACTION;
  }

  actionLabel(action: string | undefined): string {
    return action === ORGANIZE_ACTION
      ? this.i18n.t('automations.rule.actionOrganize')
      : this.i18n.t('automations.rule.actionProcess');
  }

  extensionsLabel(rule: AutomationRule): string {
    const extensions = rule.extensions ?? [];
    return extensions.length > 0
      ? extensions.map((extension) => extension.toUpperCase()).join(' · ')
      : this.i18n.t('automations.rule.recipeDefaults');
  }

  extensionsLabelFor(rule: AutomationRule): string {
    if (this.isOrganizeRule(rule) && (rule.extensions ?? []).length === 0) {
      return this.i18n.t('automations.organize.allFiles');
    }

    return this.extensionsLabel(rule);
  }

  patternsLabel(rule: AutomationRule): string {
    return (rule.patterns ?? []).join(' · ');
  }

  outputFolderLabel(rule: AutomationRule): string {
    return (
      rule.outputDir?.trim() || this.i18n.t('automations.form.nextToSource')
    );
  }

  targetFolderLabel(rule: AutomationRule): string {
    return rule.targetDir?.trim() || this.i18n.t('automations.form.noFolder');
  }

  folderTemplateLabel(rule: AutomationRule): string {
    return (
      rule.folderTemplate?.trim() ||
      this.i18n.t('automations.organize.noFolderTemplate')
    );
  }

  nameTemplateLabel(rule: AutomationRule): string {
    return (
      rule.nameTemplate?.trim() || this.i18n.t('automations.organize.keepName')
    );
  }

  collisionLabel(rule: AutomationRule): string {
    return rule.collisionPolicy === COLLISION_SUFFIX
      ? this.i18n.t('automations.organize.collisionSuffix')
      : this.i18n.t('automations.organize.collisionSkip');
  }

  /** The server refuses to enable an organize rule without a stored preview. */
  ruleNeedsPreview(rule: AutomationRule): boolean {
    return (
      this.isOrganizeRule(rule) && (rule.previewHash ?? '').trim().length === 0
    );
  }

  /** An enabled rule is always stoppable; only enabling is gated. */
  toggleBlocked(rule: AutomationRule): boolean {
    return !rule.enabled && this.ruleNeedsPreview(rule);
  }

  runtimeLabel(rule: AutomationRule): string {
    switch (rule.runtimeStatus) {
      case 'watching':
        return this.i18n.t('automations.runtime.watching');
      case 'degraded':
        return this.i18n.t('automations.runtime.degraded');
      default:
        return this.i18n.t('automations.runtime.stopped');
    }
  }

  runtimeBadgeClass(rule: AutomationRule): string {
    switch (rule.runtimeStatus) {
      case 'watching':
        return 'badge badge-success';
      case 'degraded':
        return 'badge badge-danger';
      default:
        return 'badge badge-neutral';
    }
  }

  runtimeDotState(rule: AutomationRule): string {
    switch (rule.runtimeStatus) {
      case 'watching':
        return 'success';
      case 'degraded':
        return 'danger';
      default:
        return 'idle';
    }
  }

  statusLabel(status: string | undefined): string {
    switch (status) {
      case 'success':
        return this.i18n.t('home.status.success');
      case 'partial_success':
        return this.i18n.t('home.status.partial');
      case 'failed':
        return this.i18n.t('home.status.failed');
      case 'cancelled':
        return this.i18n.t('home.status.cancelled');
      case 'interrupted':
        return this.i18n.t('home.status.interrupted');
      case 'running':
        return this.i18n.t('activity.status.running');
      case 'queued':
        return this.i18n.t('activity.status.queued');
      default:
        return status?.trim() || this.i18n.t('automations.status.notRun');
    }
  }

  statusBadgeClass(status: string | undefined): string {
    switch (status) {
      case 'success':
        return 'badge badge-success';
      case 'partial_success':
      case 'interrupted':
        return 'badge badge-warning';
      case 'failed':
        return 'badge badge-danger';
      case 'running':
      case 'queued':
        return 'badge badge-info';
      default:
        return 'badge badge-neutral';
    }
  }

  outputNames(rule: AutomationRule): string[] {
    return (rule.lastOutputs ?? [])
      .slice(0, OUTPUT_PREVIEW_LIMIT)
      .map((path) => fileName(path));
  }

  runOutputsLabel(run: AutomationRunRecord): string {
    const count = run.outputs?.length ?? 0;
    if (count === 0) {
      return this.i18n.t('home.output.none');
    }
    return formatCount(
      count,
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
    );
  }

  /**
   * Run rows can carry the synthetic organize tool id, a missing rule name or
   * neither. The label never falls back to a raw synthetic id.
   */
  runRuleLabel(run: AutomationRunRecord): string {
    const name = (run.ruleName ?? '').trim();
    const id = (run.ruleId ?? '').trim();
    if (name.length > 0 && name !== ORGANIZE_HISTORY_TOOL_ID) {
      return name;
    }
    if (id.length > 0 && id !== ORGANIZE_HISTORY_TOOL_ID) {
      return id;
    }

    return this.i18n.t('automations.runs.organizeRule');
  }

  runFileName(run: AutomationRunRecord): string {
    return fileName(run.filePath) || this.i18n.t('automations.runs.unknownFile');
  }

  relativeTime(timestamp: number | undefined): string {
    return formatRelativeTime(
      timestamp ?? 0,
      this.i18n.locale(),
      (key, params) => this.i18n.t(key, params),
    );
  }

  /**
   * A suffix is a move the planner renamed to avoid overwriting. The wire model
   * has no separate action for it, so it is derived from the rule's collision
   * policy and the numeric suffix the planner appends (-2, -3, ...).
   */
  previewActionLabel(move: OrganizePlannedMove): string {
    if (move.action !== 'move') {
      return this.i18n.t('automations.organize.actionSkip');
    }
    if (this.previewUsesSuffix(move)) {
      return this.i18n.t('automations.organize.actionSuffix');
    }

    return this.i18n.t('automations.organize.actionMove');
  }

  previewActionClass(move: OrganizePlannedMove): string {
    if (move.action !== 'move') {
      return 'badge badge-neutral';
    }
    if (this.previewUsesSuffix(move)) {
      return 'badge badge-warning';
    }

    return 'badge badge-success';
  }

  openCreateForm(): void {
    this.editingRule.set(null);
    this.formName.set('');
    this.formAction.set(RECIPE_ACTION);
    this.formWatchDir.set('');
    this.formRecipeId.set(this.recipes()[0]?.id ?? '');
    this.formExtensionsText.set('');
    this.formRecursive.set(false);
    this.formPatternsText.set('');
    this.formOutputDir.set('');
    this.formDeleteSource.set(false);
    this.formEnabled.set(true);
    this.formTargetDir.set('');
    this.formFolderTemplate.set('');
    this.formNameTemplate.set('');
    this.formCollisionPolicy.set(COLLISION_SKIP);
    this.resetPreview();
    this.formError.set('');
    this.formAttempted.set(false);
    this.confirmDeleteId.set('');
    this.formOpen.set(true);
  }

  openEditForm(rule: AutomationRule): void {
    this.editingRule.set(rule);
    this.formName.set(rule.name);
    this.formAction.set(
      rule.action === ORGANIZE_ACTION ? ORGANIZE_ACTION : RECIPE_ACTION,
    );
    this.formWatchDir.set(rule.watchDir);
    this.formRecipeId.set(rule.recipeId);
    this.formExtensionsText.set((rule.extensions ?? []).join(', '));
    this.formRecursive.set(rule.recursive === true);
    this.formPatternsText.set((rule.patterns ?? []).join(', '));
    this.formOutputDir.set(rule.outputDir ?? '');
    this.formDeleteSource.set(rule.deleteSource === true);
    this.formEnabled.set(rule.enabled);
    this.formTargetDir.set(rule.targetDir ?? '');
    this.formFolderTemplate.set(rule.folderTemplate ?? '');
    this.formNameTemplate.set(rule.nameTemplate ?? '');
    this.formCollisionPolicy.set(
      rule.collisionPolicy === COLLISION_SUFFIX
        ? COLLISION_SUFFIX
        : COLLISION_SKIP,
    );
    this.resetPreview();
    // The server only keeps a hash that matched the stored configuration, so a
    // non-empty hash means these exact settings were previewed.
    if (
      rule.action === ORGANIZE_ACTION &&
      (rule.previewHash ?? '').trim().length > 0
    ) {
      this.storedPreviewKey.set(this.ruleConfigKey(rule));
    }
    this.formError.set('');
    this.formAttempted.set(false);
    this.confirmDeleteId.set('');
    this.formOpen.set(true);
  }

  closeForm(): void {
    this.formOpen.set(false);
    this.editingRule.set(null);
    this.formError.set('');
    this.formAttempted.set(false);
    this.resetPreview();
  }

  onNameInput(event: Event): void {
    this.formName.set((event.target as HTMLInputElement | null)?.value ?? '');
  }

  onActionChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    this.formAction.set(value === ORGANIZE_ACTION ? ORGANIZE_ACTION : RECIPE_ACTION);
    this.formError.set('');
  }

  onExtensionsInput(event: Event): void {
    this.formExtensionsText.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onRecursiveToggle(event: Event): void {
    this.formRecursive.set(
      (event.target as HTMLInputElement | null)?.checked ?? false,
    );
  }

  onPatternsInput(event: Event): void {
    this.formPatternsText.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onRecipeChange(event: Event): void {
    this.formRecipeId.set((event.target as HTMLSelectElement | null)?.value ?? '');
    // The backend applies the recipe defaults when extensions are empty.
    this.formExtensionsText.set('');
  }

  onEnabledToggle(event: Event): void {
    this.formEnabled.set(
      (event.target as HTMLInputElement | null)?.checked ?? false,
    );
  }

  onDeleteSourceToggle(event: Event): void {
    this.formDeleteSource.set(
      (event.target as HTMLInputElement | null)?.checked ?? false,
    );
  }

  onFolderTemplateInput(event: Event): void {
    this.formFolderTemplate.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onNameTemplateInput(event: Event): void {
    this.formNameTemplate.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onCollisionChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    this.formCollisionPolicy.set(
      value === COLLISION_SUFFIX ? COLLISION_SUFFIX : COLLISION_SKIP,
    );
  }

  async chooseWatchFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.formWatchDir.set(directory);
    this.formError.set('');
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.formOutputDir.set(directory);
  }

  async chooseTargetFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.formTargetDir.set(directory);
    this.formError.set('');
  }

  clearOutputFolder(): void {
    this.formOutputDir.set('');
  }

  async preview(): Promise<void> {
    if (this.previewLoading() || this.saving()) {
      return;
    }

    this.formAttempted.set(true);
    this.previewError.set('');

    const validationError = this.previewValidationError();
    if (validationError.length > 0) {
      this.previewError.set(validationError);
      return;
    }

    this.previewLoading.set(true);
    const response: AutomationOrganizePreviewResponse =
      await this.wailsService.previewAutomationRule(this.buildPayload());
    this.previewLoading.set(false);

    if (!response.success) {
      this.previewMoves.set([]);
      this.previewStored.set(false);
      this.lastPreviewKey.set('');
      this.previewError.set(
        this.friendlyMessage(
          response.message,
          this.i18n.t('automations.error.preview'),
        ),
      );
      return;
    }

    const key = this.formConfigKey();
    this.previewMoves.set(response.moves ?? []);
    this.previewStored.set(response.previewStored === true);
    this.previewExpanded.set(false);
    this.previewError.set('');
    this.lastPreviewKey.set(key);

    if (response.previewStored === true) {
      // The server stored the hash on the saved rule; this exact configuration
      // is now what the enable gate compares against.
      this.storedPreviewKey.set(key);
      await this.refreshRules();
      this.setAction('success', this.i18n.t('automations.success.previewStored'));
    }
  }

  togglePreviewList(): void {
    this.previewExpanded.update((expanded) => !expanded);
  }

  async save(): Promise<void> {
    if (this.saving()) {
      return;
    }

    this.formAttempted.set(true);
    this.formError.set('');

    if (this.formName().trim().length === 0) {
      this.formError.set(this.i18n.t('automations.validation.name'));
      return;
    }
    if (this.formWatchDir().trim().length === 0) {
      this.formError.set(this.i18n.t('automations.validation.watchFolder'));
      return;
    }
    if (this.isOrganize()) {
      const organizeError = this.organizeValidationError();
      if (organizeError.length > 0) {
        this.formError.set(organizeError);
        return;
      }
    } else if (this.formRecipeId().trim().length === 0) {
      this.formError.set(this.i18n.t('automations.validation.recipe'));
      return;
    }
    const patternsError = this.patternsError();
    if (patternsError.length > 0) {
      this.formError.set(patternsError);
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.saveAutomationRule(
      this.buildPayload(),
    );
    this.saving.set(false);

    if (!response.success) {
      this.formError.set(
        this.friendlyMessage(
          response.message,
          this.i18n.t('automations.error.save'),
        ),
      );
      return;
    }

    this.closeForm();
    await this.refreshRules();
    this.setAction('success', this.i18n.t('automations.success.saved'));
  }

  async toggleRule(rule: AutomationRule): Promise<void> {
    if (this.saving()) {
      return;
    }

    // Mirrors the server gate: never ask to enable an organize rule whose
    // current settings have no stored preview.
    if (this.toggleBlocked(rule)) {
      this.setAction('danger', this.i18n.t('automations.organize.gateCard'));
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.setAutomationRuleEnabled(
      rule.id,
      !rule.enabled,
    );
    this.saving.set(false);

    if (!response.success) {
      this.setAction(
        'danger',
        this.friendlyMessage(
          response.message,
          this.i18n.t('automations.error.update'),
        ),
      );
      return;
    }

    await this.refreshRules();
    this.setAction(
      'success',
      this.i18n.t(
        rule.enabled
          ? 'automations.success.disabled'
          : 'automations.success.enabled',
      ),
    );
  }

  requestDelete(rule: AutomationRule): void {
    this.confirmDeleteId.set(rule.id);
  }

  cancelDelete(): void {
    this.confirmDeleteId.set('');
  }

  requestClearRuns(): void {
    if (this.runsLoading() || this.runs().length === 0) {
      return;
    }
    this.confirmClearRuns.set(true);
  }

  cancelClearRuns(): void {
    this.confirmClearRuns.set(false);
  }

  async clearRuns(): Promise<void> {
    if (this.runsLoading()) {
      return;
    }

    this.runsLoading.set(true);
    const response = await this.wailsService.clearAutomationRuns();
    this.runsLoading.set(false);
    this.confirmClearRuns.set(false);

    if (!response.success) {
      this.setAction(
        'danger',
        this.friendlyMessage(
          response.message,
          this.i18n.t('automations.error.clearRuns'),
        ),
      );
      return;
    }

    this.runs.set([]);
    this.setAction('success', this.i18n.t('automations.success.runsCleared'));
  }

  async deleteRule(rule: AutomationRule): Promise<void> {
    if (this.saving()) {
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.deleteAutomationRule(rule.id);
    this.saving.set(false);
    this.confirmDeleteId.set('');

    if (!response.success) {
      this.setAction(
        'danger',
        this.friendlyMessage(
          response.message,
          this.i18n.t('automations.error.delete'),
        ),
      );
      return;
    }

    await this.refreshRules();
    this.setAction('success', this.i18n.t('automations.success.deleted'));
  }

  private buildPayload(): AutomationRule {
    const base = this.editingRule();
    const organize = this.isOrganize();

    return {
      ...(base ?? {
        id: '',
        name: '',
        enabled: true,
        watchDir: '',
        recipeId: '',
        createdAt: 0,
      }),
      name: this.formName().trim(),
      action: organize ? ORGANIZE_ACTION : RECIPE_ACTION,
      // The gate mirrors the server: a configuration without a stored preview
      // is saved disabled instead of being sent as enabled and silently reverted.
      enabled: this.formEnabled() && !this.enableGateActive(),
      watchDir: this.formWatchDir().trim(),
      recipeId: this.formRecipeId().trim(),
      extensions: this.parseExtensions(this.formExtensionsText()),
      recursive: this.formRecursive(),
      patterns: this.parsePatterns(this.formPatternsText()),
      outputDir: organize ? '' : this.formOutputDir().trim(),
      deleteSource: organize ? false : this.formDeleteSource(),
      targetDir: organize ? this.formTargetDir().trim() : '',
      folderTemplate: organize ? this.formFolderTemplate().trim() : '',
      nameTemplate: organize ? this.formNameTemplate().trim() : '',
      collisionPolicy: organize ? this.formCollisionPolicy() : '',
    };
  }

  private parseExtensions(value: string): string[] {
    const parsed = value
      .split(/[\s,]+/)
      .map((part) => part.trim().toLowerCase().replace(/^\.+/, ''))
      .filter((part) => part.length > 0);

    return Array.from(new Set(parsed));
  }

  /**
   * Patterns are comma-separated because a file name may contain spaces, and
   * the values are matched literally against the base name.
   */
  private parsePatterns(value: string): string[] {
    const parsed = value
      .split(',')
      .map((part) => part.trim())
      .filter((part) => part.length > 0);

    return Array.from(new Set(parsed));
  }

  /** A '[' with no closing ']' can never match, so flag it inline. */
  private unclosedCharacterClass(pattern: string): boolean {
    let open = false;
    for (const character of pattern) {
      if (character === '[' && !open) {
        open = true;
      } else if (character === ']' && open) {
        open = false;
      }
    }

    return open;
  }

  private previewValidationError(): string {
    if (this.formName().trim().length === 0) {
      return this.i18n.t('automations.validation.name');
    }
    if (this.formWatchDir().trim().length === 0) {
      return this.i18n.t('automations.validation.watchFolder');
    }

    return this.organizeValidationError();
  }

  /** Mirrors validateOrganizeRule: target, templates, in that order. */
  private organizeValidationError(): string {
    if (this.formTargetDir().trim().length === 0) {
      return this.i18n.t('automations.validation.targetFolder');
    }
    const folderIssue = this.folderTemplateIssue();
    if (folderIssue) {
      return this.issueMessage(folderIssue);
    }
    const nameIssue = this.nameTemplateIssue();
    if (nameIssue) {
      return this.issueMessage(nameIssue);
    }

    return '';
  }

  /**
   * The backend parses a template into literals and tokens, rejecting a stray
   * brace and an unknown token before any structural rule. The same order is
   * used here so the inline message is the one the save would return.
   */
  private tokenIssue(template: string): TemplateIssue | null {
    const available = ORGANIZE_TOKENS.map((token) => `{${token}}`).join(', ');
    for (let index = 0; index < template.length; index += 1) {
      const character = template[index];
      if (character === '{') {
        const end = template.indexOf('}', index + 1);
        if (end < 0) {
          return { key: 'automations.validation.templateUnclosedBrace' };
        }
        const token = template.slice(index + 1, end);
        if (!ORGANIZE_TOKENS.includes(token as OrganizeToken)) {
          return {
            key: 'automations.validation.templateUnknownToken',
            params: { token, tokens: available },
          };
        }
        index = end;
        continue;
      }
      if (character === '}') {
        return { key: 'automations.validation.templateStrayBrace' };
      }
    }

    return null;
  }

  private inspectFolderTemplate(template: string): TemplateIssue | null {
    const tokenIssue = this.tokenIssue(template);
    if (tokenIssue) {
      return tokenIssue;
    }

    const trimmed = template.trim();
    if (trimmed.length === 0) {
      return null;
    }
    if (trimmed.startsWith('/') || trimmed.startsWith('\\')) {
      return { key: 'automations.validation.folderTemplateAbsolute' };
    }
    for (const segment of trimmed.replace(/\\/g, '/').split('/')) {
      const value = segment.trim();
      if (value.length === 0) {
        return { key: 'automations.validation.folderTemplateEmptySegment' };
      }
      if (value === '.' || value === '..') {
        return { key: 'automations.validation.folderTemplateDotSegment' };
      }
      if (value.includes(':')) {
        return { key: 'automations.validation.folderTemplateColon' };
      }
    }

    return null;
  }

  private inspectNameTemplate(template: string): TemplateIssue | null {
    const tokenIssue = this.tokenIssue(template);
    if (tokenIssue) {
      return tokenIssue;
    }
    if (/[\\/]/.test(template)) {
      return { key: 'automations.validation.nameTemplateSeparator' };
    }
    const trimmed = template.trim();
    if (trimmed === '.' || trimmed === '..') {
      return { key: 'automations.validation.nameTemplateDot' };
    }

    return null;
  }

  private issueMessage(issue: TemplateIssue | null): string {
    return issue ? this.i18n.t(issue.key, issue.params) : '';
  }

  private resetPreview(): void {
    this.previewMoves.set([]);
    this.previewStored.set(false);
    this.previewError.set('');
    this.previewExpanded.set(false);
    this.lastPreviewKey.set('');
    this.storedPreviewKey.set('');
  }

  private enableGateHintKey(): TranslationKey {
    if (this.previewStale()) {
      return 'automations.organize.gateChanged';
    }
    if (this.lastPreviewKey().length > 0 && !this.previewStored()) {
      return 'automations.organize.gateNotStored';
    }
    if (!this.editingId()) {
      return 'automations.organize.gateSaveFirst';
    }

    return 'automations.organize.gatePreview';
  }

  /** Serializes the exact fields organizeConfigHash fingerprints. */
  private configKey(config: {
    action: string;
    watchDir: string;
    extensions: string[];
    recursive: boolean;
    patterns: string[];
    targetDir: string;
    folderTemplate: string;
    nameTemplate: string;
    collisionPolicy: string;
  }): string {
    return JSON.stringify({
      action: config.action === ORGANIZE_ACTION ? ORGANIZE_ACTION : RECIPE_ACTION,
      watchDir: this.normalizeDirectory(config.watchDir),
      extensions: [...config.extensions].sort(),
      recursive: config.recursive === true,
      patterns: [...config.patterns].sort(),
      targetDir: this.normalizeDirectory(config.targetDir),
      folderTemplate: config.folderTemplate.trim(),
      nameTemplate: config.nameTemplate.trim(),
      collisionPolicy:
        config.collisionPolicy === COLLISION_SUFFIX
          ? COLLISION_SUFFIX
          : COLLISION_SKIP,
    });
  }

  private ruleConfigKey(rule: AutomationRule): string {
    return this.configKey({
      action: rule.action ?? RECIPE_ACTION,
      watchDir: rule.watchDir ?? '',
      extensions: this.parseExtensions((rule.extensions ?? []).join(', ')),
      recursive: rule.recursive === true,
      patterns: this.parsePatterns((rule.patterns ?? []).join(', ')),
      targetDir: rule.targetDir ?? '',
      folderTemplate: rule.folderTemplate ?? '',
      nameTemplate: rule.nameTemplate ?? '',
      collisionPolicy: rule.collisionPolicy ?? COLLISION_SKIP,
    });
  }

  private normalizeDirectory(path: string): string {
    return path.trim().replace(/[\\/]+$/, '');
  }

  /** True when the final segment stem ends with the planner's -N suffix. */
  private hasCollisionSuffix(destination: string): boolean {
    const segment = destination.split(/[\\/]/).pop() ?? '';
    const dot = segment.lastIndexOf('.');
    const stem = dot > 0 ? segment.slice(0, dot) : segment;

    return /-\d+$/.test(stem);
  }

  /**
   * The planner only appends a suffix when the rule policy asks for it, so a
   * template-produced "-2" never claims a collision rename under skip.
   */
  private previewUsesSuffix(move: OrganizePlannedMove): boolean {
    return (
      move.action === 'move' &&
      this.formCollisionPolicy() === COLLISION_SUFFIX &&
      this.hasCollisionSuffix(move.destination ?? '')
    );
  }

  private async refreshRules(): Promise<void> {
    const [rulesResponse, statusResponse] = await Promise.all([
      this.wailsService.listAutomationRules(),
      this.wailsService.getAutomationStatus(),
    ]);

    if (rulesResponse.success) {
      this.rules.set(rulesResponse.rules ?? []);
      this.errorMessage.set('');
    } else {
      this.setError(rulesResponse.message);
    }

    this.applyStatusResponse(statusResponse);
  }

  private applyRulesResponse(response: ListAutomationRulesResponse): void {
    if (response.success) {
      this.rules.set(response.rules ?? []);
      return;
    }
    this.setError(response.message);
  }

  private applyStatusResponse(response: AutomationStatusResponse): void {
    if (response.success) {
      this.status.set(response);
    }
  }

  private applyRunsResponse(response: ListAutomationRunsResponse): void {
    if (response.success) {
      this.runs.set(response.runs ?? []);
      return;
    }

    this.runs.set([]);
    this.errorMessage.set(
      this.friendlyMessage(
        response.message,
        this.i18n.t('automations.error.runsUnavailable'),
      ),
    );
  }

  private setError(message: string): void {
    this.errorMessage.set(
      this.friendlyMessage(
        message,
        this.i18n.t('automations.error.rulesUnavailable'),
      ),
    );
  }

  private friendlyMessage(message: string, fallback: string): string {
    const trimmed = message?.trim() ?? '';
    const technical = /wails\/runtime|Cannot POST|<!DOCTYPE/i.test(trimmed);

    return trimmed.length > 0 &&
      trimmed.length <= 200 &&
      !technical &&
      !/[<>]/.test(trimmed)
      ? trimmed
      : fallback;
  }

  private setAction(tone: ActionTone, text: string): void {
    this.actionMessage.set({ tone, text });
  }

  /**
   * Live events can be replayed for a rule this page no longer lists, so unknown
   * ids are ignored instead of creating phantom last-run state.
   */
  private applyRunEvent(event: AutomationRunEvent): void {
    if (!event || typeof event.ruleId !== 'string' || event.ruleId.length === 0) {
      return;
    }

    const target = this.rules().find((rule) => rule.id === event.ruleId);
    if (!target) {
      return;
    }

    const at = event.at > 0 ? event.at : Date.now();
    // ReplaySubject can replay an older event when the page opens; never let it
    // overwrite a newer persisted/summary last run.
    if ((target.lastRunAt ?? 0) > at) {
      return;
    }

    const outputs = Array.isArray(event.outputs) ? [...event.outputs] : [];

    this.rules.update((rules) =>
      rules.map((rule) =>
        rule.id === event.ruleId
          ? {
              ...rule,
              lastRunAt: at,
              lastStatus: event.status,
              lastMessage: event.message ?? '',
              lastFile: event.filePath ?? '',
              lastOutputs: outputs,
            }
          : rule,
      ),
    );

    this.status.update((current) => {
      if (current && (current.lastRunAt ?? 0) > at) {
        return current;
      }

      const rules = this.rules();
      const base: AutomationStatusResponse = current ?? {
        success: true,
        message: '',
        totalRules: rules.length,
        activeRules: rules.filter((rule) => rule.enabled).length,
      };

      return {
        ...base,
        totalRules: rules.length,
        activeRules: rules.filter(
          (rule) => rule.runtimeStatus === 'watching',
        ).length,
        lastRunAt: at,
        lastStatus: event.status,
        lastMessage: event.message ?? '',
        lastFile: event.filePath ?? '',
      };
    });

    if (!TERMINAL_RUN_STATUSES.has(event.status)) {
      return;
    }

    const record: AutomationRunRecord = {
      id: `live-${at}-${event.ruleId}`,
      ruleId: event.ruleId,
      ruleName: event.ruleName || target.name,
      filePath: event.filePath ?? '',
      status: event.status,
      message: event.message ?? '',
      outputs,
      at,
    };

    this.runs.update((runs) => {
      if (runs.some((run) => run.id === record.id)) {
        return runs;
      }
      return [record, ...runs].slice(0, RUN_HISTORY_LIMIT);
    });
  }
}
