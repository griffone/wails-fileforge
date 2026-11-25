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
  FolderOpen,
  LucideAngularModule,
  Pencil,
  Plus,
  ShieldCheck,
  Trash2,
  TriangleAlert,
  Workflow,
} from 'lucide-angular';
import { Subject, takeUntil } from 'rxjs';

import {
  AutomationRule,
  AutomationRunEvent,
  AutomationRunRecord,
  AutomationStatusResponse,
  ListAutomationRunsResponse,
  ListAutomationRulesResponse,
  Recipe,
  Wails,
} from '../../services/wails';
import { I18nService } from '../../services/i18n.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { fileName, formatCount } from '../../utils/format';

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

const OUTPUT_PREVIEW_LIMIT = 3;
const RUN_HISTORY_LIMIT = 20;

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
  readonly formWatchDir = signal('');
  readonly formRecipeId = signal('');
  readonly formExtensionsText = signal('');
  readonly formOutputDir = signal('');
  readonly formDeleteSource = signal(false);
  readonly formEnabled = signal(true);

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

  readonly nameInvalid = computed(
    () => this.formAttempted() && this.formName().trim().length === 0,
  );

  readonly watchDirInvalid = computed(
    () => this.formAttempted() && this.formWatchDir().trim().length === 0,
  );

  readonly recipeInvalid = computed(
    () => this.formAttempted() && this.formRecipeId().trim().length === 0,
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

  extensionsLabel(rule: AutomationRule): string {
    const extensions = rule.extensions ?? [];
    return extensions.length > 0
      ? extensions.map((extension) => extension.toUpperCase()).join(' · ')
      : this.i18n.t('automations.rule.recipeDefaults');
  }

  outputFolderLabel(rule: AutomationRule): string {
    return (
      rule.outputDir?.trim() || this.i18n.t('automations.form.nextToSource')
    );
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

  runFileName(run: AutomationRunRecord): string {
    return fileName(run.filePath) || this.i18n.t('automations.runs.unknownFile');
  }

  relativeTime(timestamp: number | undefined): string {
    if (!timestamp || !Number.isFinite(timestamp) || timestamp <= 0) {
      return '';
    }

    const now = Date.now();
    const diff = Math.max(0, now - timestamp);

    if (diff < 60_000) {
      return this.i18n.t('home.time.justNow');
    }

    if (diff < 3_600_000) {
      return this.i18n.t('home.time.minutesAgo', {
        count: Math.max(1, Math.floor(diff / 60_000)),
      });
    }

    if (diff < 86_400_000) {
      return this.i18n.t('home.time.hoursAgo', {
        count: Math.max(1, Math.floor(diff / 3_600_000)),
      });
    }

    const date = new Date(timestamp);
    const yesterday = new Date(now);
    yesterday.setDate(yesterday.getDate() - 1);

    return date.toDateString() === yesterday.toDateString()
      ? this.i18n.t('home.time.yesterday')
      : date.toLocaleDateString(this.i18n.locale(), {
          month: 'short',
          day: 'numeric',
        });
  }

  openCreateForm(): void {
    this.editingRule.set(null);
    this.formName.set('');
    this.formWatchDir.set('');
    this.formRecipeId.set(this.recipes()[0]?.id ?? '');
    this.formExtensionsText.set('');
    this.formOutputDir.set('');
    this.formDeleteSource.set(false);
    this.formEnabled.set(true);
    this.formError.set('');
    this.formAttempted.set(false);
    this.confirmDeleteId.set('');
    this.formOpen.set(true);
  }

  openEditForm(rule: AutomationRule): void {
    this.editingRule.set(rule);
    this.formName.set(rule.name);
    this.formWatchDir.set(rule.watchDir);
    this.formRecipeId.set(rule.recipeId);
    this.formExtensionsText.set((rule.extensions ?? []).join(', '));
    this.formOutputDir.set(rule.outputDir ?? '');
    this.formDeleteSource.set(rule.deleteSource === true);
    this.formEnabled.set(rule.enabled);
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
  }

  onNameInput(event: Event): void {
    this.formName.set((event.target as HTMLInputElement | null)?.value ?? '');
  }

  onExtensionsInput(event: Event): void {
    this.formExtensionsText.set(
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

  clearOutputFolder(): void {
    this.formOutputDir.set('');
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
    if (this.formRecipeId().trim().length === 0) {
      this.formError.set(this.i18n.t('automations.validation.recipe'));
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
      enabled: this.formEnabled(),
      watchDir: this.formWatchDir().trim(),
      recipeId: this.formRecipeId().trim(),
      extensions: this.parseExtensions(this.formExtensionsText()),
      outputDir: this.formOutputDir().trim(),
      deleteSource: this.formDeleteSource(),
    };
  }

  private parseExtensions(value: string): string[] {
    const parsed = value
      .split(/[\s,]+/)
      .map((part) => part.trim().toLowerCase().replace(/^\.+/, ''))
      .filter((part) => part.length > 0);

    return Array.from(new Set(parsed));
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
