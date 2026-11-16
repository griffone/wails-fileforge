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
  AutomationRuleV1,
  AutomationRunEventV1,
  AutomationStatusResponseV1,
  ListAutomationRulesResponseV1,
  RecipeV1,
  Wails,
} from '../../services/wails';
import { fileName } from '../../utils/format';

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

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly plusIcon = Plus;
  readonly editIcon = Pencil;
  readonly trashIcon = Trash2;
  readonly alertIcon = TriangleAlert;
  readonly workflowIcon = Workflow;

  readonly rules = signal<AutomationRuleV1[]>([]);
  readonly recipes = signal<RecipeV1[]>([]);
  readonly status = signal<AutomationStatusResponseV1 | null>(null);

  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly errorMessage = signal('');
  readonly actionMessage = signal<ActionMessage | null>(null);

  readonly formOpen = signal(false);
  readonly editingRule = signal<AutomationRuleV1 | null>(null);
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
    try {
      const [recipesResponse, rulesResponse, statusResponse] =
        await Promise.all([
          this.wailsService.listRecipesV1().catch(() => ({
            success: false,
            message: 'The recipe catalog could not be read.',
            recipes: [] as RecipeV1[],
          })),
          this.wailsService.listAutomationRulesV1().catch(() => ({
            success: false,
            message: 'The automation rules could not be loaded.',
            rules: [] as AutomationRuleV1[],
          })),
          this.wailsService.getAutomationStatusV1().catch(() => ({
            success: false,
            message: 'The automation status could not be read.',
            totalRules: 0,
            activeRules: 0,
          })),
        ]);

      if (recipesResponse.success) {
        this.recipes.set(recipesResponse.recipes ?? []);
      } else {
        this.recipes.set([]);
        this.setError(recipesResponse.message);
      }

      this.applyRulesResponse(rulesResponse);
      this.applyStatusResponse(statusResponse);
    } finally {
      this.loading.set(false);
    }
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
  }

  recipeName(rule: AutomationRuleV1): string {
    return (
      this.recipes().find((recipe) => recipe.id === rule.recipeId)?.name ??
      rule.recipeId
    );
  }

  extensionsLabel(rule: AutomationRuleV1): string {
    const extensions = rule.extensions ?? [];
    return extensions.length > 0
      ? extensions.map((extension) => extension.toUpperCase()).join(' · ')
      : 'Recipe defaults';
  }

  outputFolderLabel(rule: AutomationRuleV1): string {
    return rule.outputDir?.trim() || 'Next to the source';
  }

  runtimeLabel(rule: AutomationRuleV1): string {
    switch (rule.runtimeStatus) {
      case 'watching':
        return 'Watching';
      case 'degraded':
        return 'Degraded';
      default:
        return 'Stopped';
    }
  }

  runtimeBadgeClass(rule: AutomationRuleV1): string {
    switch (rule.runtimeStatus) {
      case 'watching':
        return 'badge badge-success';
      case 'degraded':
        return 'badge badge-danger';
      default:
        return 'badge badge-neutral';
    }
  }

  runtimeDotState(rule: AutomationRuleV1): string {
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
        return 'Success';
      case 'partial_success':
        return 'Partial';
      case 'failed':
        return 'Failed';
      case 'cancelled':
        return 'Cancelled';
      case 'interrupted':
        return 'Interrupted';
      case 'running':
        return 'Running';
      case 'queued':
        return 'Queued';
      default:
        return status?.trim() || 'Not run';
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

  outputNames(rule: AutomationRuleV1): string[] {
    return (rule.lastOutputs ?? [])
      .slice(0, OUTPUT_PREVIEW_LIMIT)
      .map((path) => fileName(path));
  }

  relativeTime(timestamp: number | undefined): string {
    if (!timestamp || !Number.isFinite(timestamp) || timestamp <= 0) {
      return '';
    }

    const now = Date.now();
    const diff = Math.max(0, now - timestamp);

    if (diff < 60_000) {
      return 'Just now';
    }

    if (diff < 3_600_000) {
      return `${Math.max(1, Math.floor(diff / 60_000))} min ago`;
    }

    if (diff < 86_400_000) {
      return `${Math.max(1, Math.floor(diff / 3_600_000))} h ago`;
    }

    const date = new Date(timestamp);
    const yesterday = new Date(now);
    yesterday.setDate(yesterday.getDate() - 1);

    return date.toDateString() === yesterday.toDateString()
      ? 'Yesterday'
      : date.toLocaleDateString(undefined, {
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

  openEditForm(rule: AutomationRuleV1): void {
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
      this.formError.set('Give the rule a name.');
      return;
    }
    if (this.formWatchDir().trim().length === 0) {
      this.formError.set('Choose a folder to watch.');
      return;
    }
    if (this.formRecipeId().trim().length === 0) {
      this.formError.set('Choose a recipe to run.');
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.saveAutomationRuleV1(
      this.buildPayload(),
    );
    this.saving.set(false);

    if (!response.success) {
      this.formError.set(
        this.friendlyMessage(response.message, 'The rule could not be saved.'),
      );
      return;
    }

    this.closeForm();
    await this.refreshRules();
    this.setAction('success', response.message || 'Automation rule saved.');
  }

  async toggleRule(rule: AutomationRuleV1): Promise<void> {
    if (this.saving()) {
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.setAutomationRuleEnabledV1(
      rule.id,
      !rule.enabled,
    );
    this.saving.set(false);

    if (!response.success) {
      this.setAction(
        'danger',
        this.friendlyMessage(
          response.message,
          'The rule could not be updated.',
        ),
      );
      return;
    }

    await this.refreshRules();
    this.setAction(
      'success',
      this.friendlyMessage(
        response.message,
        rule.enabled ? 'Rule disabled.' : 'Rule enabled.',
      ),
    );
  }

  requestDelete(rule: AutomationRuleV1): void {
    this.confirmDeleteId.set(rule.id);
  }

  cancelDelete(): void {
    this.confirmDeleteId.set('');
  }

  async deleteRule(rule: AutomationRuleV1): Promise<void> {
    if (this.saving()) {
      return;
    }

    this.saving.set(true);
    const response = await this.wailsService.deleteAutomationRuleV1(rule.id);
    this.saving.set(false);
    this.confirmDeleteId.set('');

    if (!response.success) {
      this.setAction(
        'danger',
        this.friendlyMessage(
          response.message,
          'The rule could not be deleted.',
        ),
      );
      return;
    }

    await this.refreshRules();
    this.setAction(
      'success',
      this.friendlyMessage(response.message, 'Automation rule deleted.'),
    );
  }

  private buildPayload(): AutomationRuleV1 {
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
      this.wailsService.listAutomationRulesV1(),
      this.wailsService.getAutomationStatusV1(),
    ]);

    if (rulesResponse.success) {
      this.rules.set(rulesResponse.rules ?? []);
      this.errorMessage.set('');
    } else {
      this.setError(rulesResponse.message);
    }

    this.applyStatusResponse(statusResponse);
  }

  private applyRulesResponse(response: ListAutomationRulesResponseV1): void {
    if (response.success) {
      this.rules.set(response.rules ?? []);
      return;
    }
    this.setError(response.message);
  }

  private applyStatusResponse(response: AutomationStatusResponseV1): void {
    if (response.success) {
      this.status.set(response);
    }
  }

  private setError(message: string): void {
    this.errorMessage.set(
      this.friendlyMessage(
        message,
        'The automation rules could not be loaded. Open the desktop app to manage folder rules.',
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
  private applyRunEvent(event: AutomationRunEventV1): void {
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
      const base: AutomationStatusResponseV1 = current ?? {
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
  }
}
