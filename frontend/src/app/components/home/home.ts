import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  ElementRef,
  HostListener,
  OnInit,
  Signal,
  ViewChild,
  WritableSignal,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import {
  ArrowUpRight,
  Copy,
  Download,
  ExternalLink,
  FolderOpen,
  LucideAngularModule,
  Pencil,
  Play,
  Search,
  SearchX,
  ShieldCheck,
  Trash2,
  Upload,
  Workflow,
  X,
} from 'lucide-angular';

import { RecipeDefinition, RECIPES } from '../../config/recipes';
import {
  findShortcutById,
  formatShortcutLabel,
} from '../../config/shortcuts';
import { ToolCategory, ToolDefinition } from '../../config/tools';
import {
  RecipeOptionFields,
  type RecipeOptionChange,
} from '../recipe-options/recipe-option-fields';
import {
  optionInitialValues,
  optionRequestValues,
  optionValidationError,
  type RecipeOptionValue,
} from '../recipe-options/recipe-option-values';
import { I18nService } from '../../services/i18n.service';
import {
  RecentConversion,
  RecentConversionsService,
} from '../../services/recent-conversions.service';
import { RecentToolsService } from '../../services/recent-tools.service';
import { ShortcutService } from '../../services/shortcut.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import {
  AutomationStatusResponse,
  ImportUserRecipesResponse,
  PathActionResponse,
  Recipe,
  RecipeOptionField,
  ToolCatalogEntry,
  UserRecipeResponse,
  Wails,
} from '../../services/wails';
import {
  fileDir,
  fileExtension,
  fileName,
  formatRelativeTime,
} from '../../utils/format';
import { jobStatusDefinition } from '../../utils/job-status';

type CategoryFilter = 'all' | ToolCategory;

const DIACRITICS = /[\u0300-\u036f]/g;
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

type RecipeDialogMode = 'duplicate' | 'edit' | 'rename';

function normalize(value: string): string {
  return value.normalize('NFD').replace(DIACRITICS, '').toLowerCase();
}

@Component({
  selector: 'app-home',
  standalone: true,
  imports: [RouterLink, LucideAngularModule, RecipeOptionFields],
  templateUrl: './home.html',
  styleUrl: './home.css',
})
export class Home implements OnInit {
  readonly registry = inject(ToolRegistryService);
  readonly i18n = inject(I18nService);

  private readonly recentTools = inject(RecentToolsService);
  private readonly recentConversionsService = inject(RecentConversionsService);
  private readonly router = inject(Router);
  private readonly wailsService = inject(Wails);
  private readonly document = inject(DOCUMENT);
  private readonly changeDetector = inject(ChangeDetectorRef);
  private readonly shortcuts = inject(ShortcutService);

  readonly searchIcon = Search;
  readonly clearIcon = X;
  readonly emptyIcon = SearchX;
  readonly arrowIcon = ArrowUpRight;
  readonly shieldIcon = ShieldCheck;
  readonly openIcon = ExternalLink;
  readonly folderIcon = FolderOpen;
  readonly automationIcon = Workflow;
  readonly copyIcon = Copy;
  readonly editIcon = Pencil;
  readonly runIcon = Play;
  readonly deleteIcon = Trash2;
  readonly exportIcon = Download;
  readonly importIcon = Upload;

  readonly query = signal('');
  readonly activeCategory = signal<CategoryFilter>('all');
  readonly actionMessage = signal('');
  readonly automationStatus = signal<AutomationStatusResponse | null>(null);

  // User recipes come from the backend catalog; built-in cards stay static.
  private readonly runtimeRecipesState = signal<Recipe[]>([]);
  readonly recipeMessage = signal('');
  readonly userRecipes = computed(() =>
    this.runtimeRecipesState().filter((recipe) => recipe.userDefined),
  );

  // Duplicate/edit/rename dialog state.
  readonly recipeDialogOpen = signal(false);
  readonly recipeDialogMode = signal<RecipeDialogMode>('duplicate');
  readonly recipeDialogName = signal('');
  readonly recipeDialogRecipe = signal<Recipe | null>(null);
  readonly recipeDialogValues = signal<Record<string, string | boolean>>({});
  readonly recipeDialogErrors = signal<Record<string, string>>({});
  readonly recipeDialogMessage = signal('');
  readonly recipeDialogSaving = signal(false);

  // Preset sharing: export the whole catalog or import a shared file.
  readonly recipeImportOverwrite = signal(false);
  readonly recipeTransferBusy = signal(false);
  readonly recipeImportSummary = signal<ImportUserRecipesResponse | null>(
    null,
  );

  // Deletion is permanent, so it goes through a blocking confirmation.
  readonly recipeDeleteConfirmOpen = signal(false);
  readonly recipeDeleteTarget = signal<Recipe | null>(null);
  readonly recipeDeleteSaving = signal(false);

  readonly recipeDeleteConfirmBody = computed(() =>
    this.i18n.t('home.myRecipes.deleteConfirmBody', {
      name: this.recipeDeleteTarget()?.name ?? '',
    }),
  );

  @ViewChild('recipeDeleteDialog')
  private recipeDeleteDialog?: ElementRef<HTMLElement>;
  @ViewChild('recipeDeleteCancel')
  private recipeDeleteCancel?: ElementRef<HTMLButtonElement>;

  private recipeDeleteFocusOrigin: HTMLElement | null = null;

  readonly recipeDialogFields = computed<RecipeOptionField[]>(() => {
    if (this.recipeDialogMode() === 'rename') {
      return [];
    }
    return this.recipeDialogRecipe()?.optionSchema ?? [];
  });

  readonly recipeDialogTitle = computed(() => {
    switch (this.recipeDialogMode()) {
      case 'edit':
        return this.i18n.t('home.recipeDialog.titleEdit');
      case 'rename':
        return this.i18n.t('home.recipeDialog.titleRename');
      default:
        return this.i18n.t('home.recipeDialog.titleDuplicate');
    }
  });

  readonly recipeDialogSaveLabel = computed(() => {
    switch (this.recipeDialogMode()) {
      case 'edit':
        return this.i18n.t('home.recipeDialog.saveEdit');
      case 'rename':
        return this.i18n.t('home.recipeDialog.saveRename');
      default:
        return this.i18n.t('home.recipeDialog.saveDuplicate');
    }
  });

  readonly recipeDialogValid = computed(
    () =>
      this.recipeDialogName().trim().length > 0 &&
      Object.keys(this.recipeDialogErrors()).length === 0,
  );

  readonly showAutomationStatus = computed(() => {
    const status = this.automationStatus();
    return !!status && status.success && status.totalRules > 0;
  });

  private readonly onboardingDismissedState: WritableSignal<boolean>;
  readonly onboardingDismissed: Signal<boolean>;

  constructor() {
    this.onboardingDismissedState = signal(this.readOnboardingDismissed());
    this.onboardingDismissed = this.onboardingDismissedState.asReadonly();
  }

  /** The four tool-bearing categories; Home and System stay out of the bench. */
  readonly filterCategories = this.registry.categories.filter(
    (category) => category.id !== 'workspace' && category.id !== 'system',
  );

  readonly filteredTools = computed(() => {
    const category = this.activeCategory();
    const tokens = normalize(this.query().trim()).split(/\s+/).filter(Boolean);

    return this.registry.tools.filter((tool) => {
      if (category !== 'all' && tool.category !== category) {
        return false;
      }

      if (tokens.length === 0) {
        return true;
      }

      const haystack = normalize(
        [
          this.registry.toolName(tool),
          this.registry.navName(tool),
          this.registry.toolDescription(tool),
          this.registry.categoryLabel(tool.category),
          tool.name,
          tool.navName,
          ...tool.formats,
          ...tool.keywords,
        ].join(' '),
      );
      return tokens.every((token) => haystack.includes(token));
    });
  });

  readonly categoryCounts = computed<Record<ToolCategory, number>>(() => {
    const counts: Record<ToolCategory, number> = {
      workspace: 0,
      image: 0,
      pdf: 0,
      docs: 0,
      video: 0,
      audio: 0,
      system: 0,
    };

    for (const tool of this.registry.tools) {
      counts[tool.category] += 1;
    }

    return counts;
  });

  readonly hasFilters = computed(
    () => this.activeCategory() !== 'all' || this.query().trim().length > 0,
  );

  readonly recipes = RECIPES;
  readonly showRecipes = computed(() => !this.hasFilters());

  /** Recipe cards borrow the category tone of the inputs they consume. */
  recipeTone(recipe: RecipeDefinition): string {
    if (
      recipe.fallback.category === 'video' ||
      recipe.fallback.inputKind === 'video'
    ) {
      return 'video';
    }
    if (recipe.fallback.category === 'image') {
      return 'image';
    }
    return recipe.fallback.inputKind === 'pdf' ? 'pdf' : 'docs';
  }

  /** Runtime recipe for a static card; null until the catalog answers. */
  runtimeRecipe(id: string): Recipe | null {
    return this.runtimeRecipesState().find((recipe) => recipe.id === id) ?? null;
  }

  /** Duplicate is only offered when the runtime recipe declares a schema. */
  duplicableRecipe(definition: RecipeDefinition): Recipe | null {
    const recipe = this.runtimeRecipe(definition.id);
    return recipe && !recipe.userDefined && recipe.duplicable ? recipe : null;
  }

  /** Human label for the base recipe a user recipe was copied from. */
  recipeBaseName(recipe: Recipe): string {
    if (!recipe.baseRecipeId) {
      return '';
    }

    const runtime = this.runtimeRecipe(recipe.baseRecipeId);
    if (runtime) {
      return runtime.name;
    }

    const definition = RECIPES.find(
      (candidate) => candidate.id === recipe.baseRecipeId,
    );
    return definition
      ? this.registry.recipeName(definition)
      : recipe.baseRecipeId;
  }

  /** Input badge for a user recipe card. */
  userRecipeInputLabel(recipe: Recipe): string {
    return (recipe.acceptExtensions ?? [])
      .map((extension) => extension.toUpperCase())
      .join(' · ');
  }

  /** Tone class for a user recipe card, borrowed from its input kind. */
  userRecipeTone(recipe: Recipe): string {
    if (recipe.category === 'video' || recipe.inputKind === 'video') {
      return 'video';
    }
    if (recipe.category === 'image') {
      return 'image';
    }
    return recipe.inputKind === 'pdf' ? 'pdf' : 'docs';
  }

  openDuplicate(recipe: Recipe): void {
    if (!recipe.duplicable) {
      return;
    }

    this.resetRecipeDialog();
    this.recipeDialogMode.set('duplicate');
    this.recipeDialogRecipe.set(recipe);
    this.recipeDialogName.set(
      `${recipe.name} ${this.i18n.t('home.recipeDialog.copySuffix')}`,
    );
    this.recipeDialogValues.set(
      this.initialDialogValues(recipe, recipe.userDefined === true),
    );
    this.recipeDialogOpen.set(true);
  }

  openEdit(recipe: Recipe): void {
    this.resetRecipeDialog();
    this.recipeDialogMode.set('edit');
    this.recipeDialogRecipe.set(recipe);
    this.recipeDialogName.set(recipe.name);
    this.recipeDialogValues.set(this.initialDialogValues(recipe, true));
    this.recipeDialogOpen.set(true);
  }

  openRename(recipe: Recipe): void {
    this.resetRecipeDialog();
    this.recipeDialogMode.set('rename');
    this.recipeDialogRecipe.set(recipe);
    this.recipeDialogName.set(recipe.name);
    this.recipeDialogOpen.set(true);
  }

  closeRecipeDialog(): void {
    this.recipeDialogOpen.set(false);
    this.recipeDialogSaving.set(false);
  }

  onRecipeDialogNameInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.recipeDialogName.set(value);
    this.updateDialogNameError();
  }

  onRecipeOptionChange(change: RecipeOptionChange): void {
    this.setDialogFieldValue(change.field, change.value);
  }

  async saveRecipeDialog(): Promise<void> {
    if (this.recipeDialogSaving()) {
      return;
    }

    this.recipeDialogMessage.set('');
    this.updateDialogNameError();
    this.validateAllDialogFields();
    if (!this.recipeDialogValid()) {
      return;
    }

    const source = this.recipeDialogRecipe();
    const mode = this.recipeDialogMode();
    const name = this.recipeDialogName().trim();
    if (!source && mode !== 'duplicate') {
      this.recipeDialogMessage.set(
        this.i18n.t('home.recipeDialog.error.missingSource'),
      );
      return;
    }

    this.recipeDialogSaving.set(true);
    let response: UserRecipeResponse;
    if (mode === 'rename' && source) {
      response = await this.wailsService.renameUserRecipe(source.id, name);
    } else {
      const baseRecipeId = source?.userDefined
        ? (source.baseRecipeId ?? '')
        : (source?.id ?? '');
      response = await this.wailsService.saveUserRecipe({
        id: mode === 'edit' ? (source?.id ?? '') : '',
        name,
        baseRecipeId,
        options: this.buildDialogOptions(),
      });
    }
    this.recipeDialogSaving.set(false);

    if (!response.success) {
      this.recipeDialogMessage.set(
        response.message || this.i18n.t('home.recipeDialog.error.save'),
      );
      return;
    }

    this.recipeMessage.set(this.i18n.t('home.myRecipes.saved'));
    this.closeRecipeDialog();
    await this.loadRecipes();
  }

  /** Opens the blocking delete confirmation and focuses the safe choice. */
  requestDeleteUserRecipe(recipe: Recipe): void {
    if (this.recipeDeleteConfirmOpen()) {
      return;
    }

    const active = this.document.activeElement;
    this.recipeDeleteFocusOrigin =
      active instanceof HTMLElement ? active : null;
    this.recipeDeleteTarget.set(recipe);
    this.recipeDeleteConfirmOpen.set(true);
    // Render first so the cancel button exists before focusing it.
    this.changeDetector.detectChanges();
    this.recipeDeleteCancel?.nativeElement.focus();
  }

  /** Dismisses the confirmation without touching the store or the list. */
  cancelDeleteUserRecipe(): void {
    if (this.recipeDeleteSaving()) {
      return;
    }
    this.closeDeleteConfirm();
  }

  /** Deletes exactly once after explicit consent, then refreshes the list. */
  async confirmDeleteUserRecipe(): Promise<void> {
    if (!this.recipeDeleteConfirmOpen() || this.recipeDeleteSaving()) {
      return;
    }

    const recipe = this.recipeDeleteTarget();
    if (!recipe) {
      this.closeDeleteConfirm();
      return;
    }

    this.recipeDeleteSaving.set(true);
    const response = await this.wailsService.deleteUserRecipe(recipe.id);
    this.recipeDeleteSaving.set(false);
    this.closeDeleteConfirm();

    if (!response.success) {
      this.recipeMessage.set(
        response.message || this.i18n.t('home.myRecipes.deleteError'),
      );
      return;
    }

    this.recipeMessage.set(this.i18n.t('home.myRecipes.deleted'));
    await this.loadRecipes();
  }

  /**
   * Exports every user recipe. A cancelled save dialog is a no-op: it never
   * reaches the backend and never shows an error.
   */
  async exportUserRecipes(): Promise<void> {
    if (this.recipeTransferBusy()) {
      return;
    }

    const recipes = this.userRecipes();
    if (recipes.length === 0) {
      this.recipeMessage.set(this.i18n.t('home.myRecipes.exportEmpty'));
      return;
    }

    const path = await this.wailsService.saveFileDialog(
      'fileforge-recipes.json',
    );
    if (!path) {
      return;
    }

    this.recipeTransferBusy.set(true);
    const response = await this.wailsService.exportUserRecipes(
      path,
      recipes.map((recipe) => recipe.id),
    );
    this.recipeTransferBusy.set(false);

    if (!response.success) {
      this.recipeMessage.set(
        response.message || this.i18n.t('home.myRecipes.exportError'),
      );
      return;
    }

    this.recipeMessage.set(
      this.i18n.t('home.myRecipes.exported', { count: response.exported }),
    );
  }

  /**
   * Imports a shared recipe file and keeps the typed summary on screen. A
   * cancelled file dialog is a no-op with no message.
   */
  async importUserRecipes(): Promise<void> {
    if (this.recipeTransferBusy()) {
      return;
    }

    const path = await this.wailsService.openImportFileDialog();
    if (!path) {
      return;
    }

    this.recipeTransferBusy.set(true);
    const response = await this.wailsService.importUserRecipes(
      path,
      this.recipeImportOverwrite(),
    );
    this.recipeTransferBusy.set(false);
    this.recipeImportSummary.set(response);

    if (!response.success) {
      this.recipeMessage.set(
        response.message || this.i18n.t('home.myRecipes.importError'),
      );
      return;
    }

    this.recipeMessage.set('');
    await this.loadRecipes();
  }

  setRecipeImportOverwrite(event: Event): void {
    const target = event.target;
    if (target instanceof HTMLInputElement) {
      this.recipeImportOverwrite.set(target.checked);
    }
  }

  onRecipeDeleteDialogKeydown(event: KeyboardEvent): void {
    if (event.isComposing) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.cancelDeleteUserRecipe();
      return;
    }

    if (event.key === 'Tab') {
      this.trapDialogFocus(event, this.recipeDeleteDialog?.nativeElement);
      event.stopPropagation();
      return;
    }

    // The dialog is modal: shortcuts must not fire behind it.
    event.stopPropagation();
  }

  /**
   * A blocking dialog owns the keyboard: Escape cancels, Tab stays inside, and
   * every other shortcut is suspended until it closes.
   */
  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.recipeDeleteConfirmOpen()) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.cancelDeleteUserRecipe();
      return;
    }

    if (event.key === 'Tab') {
      this.trapDialogFocus(event, this.recipeDeleteDialog?.nativeElement);
      return;
    }

    event.stopPropagation();
  }

  /** Keeps Tab inside the confirmation dialog, matching the app's dialogs. */
  private trapDialogFocus(
    event: KeyboardEvent,
    dialog: HTMLElement | undefined,
  ): void {
    if (!dialog) {
      return;
    }

    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => element.offsetParent !== null);
    if (focusable.length === 0) {
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
      return;
    }
    if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  private closeDeleteConfirm(): void {
    if (!this.recipeDeleteConfirmOpen()) {
      return;
    }

    this.recipeDeleteConfirmOpen.set(false);
    this.recipeDeleteTarget.set(null);
    const origin = this.recipeDeleteFocusOrigin;
    this.recipeDeleteFocusOrigin = null;
    // Remove the dialog before handing focus back to the element that opened it.
    this.changeDetector.detectChanges();
    origin?.focus();
  }

  private resetRecipeDialog(): void {
    this.recipeDialogRecipe.set(null);
    this.recipeDialogName.set('');
    this.recipeDialogValues.set({});
    this.recipeDialogErrors.set({});
    this.recipeDialogMessage.set('');
    this.recipeDialogSaving.set(false);
  }

  private async loadRecipes(): Promise<void> {
    try {
      const response = await this.wailsService.listRecipes();
      this.runtimeRecipesState.set(response.success ? response.recipes : []);
    } catch {
      this.runtimeRecipesState.set([]);
    }
  }

  private initialDialogValues(
    recipe: Recipe,
    useOverrides: boolean,
  ): Record<string, RecipeOptionValue> {
    return optionInitialValues(
      recipe.optionSchema ?? [],
      useOverrides ? recipe.optionOverrides : undefined,
    );
  }

  private setDialogFieldValue(
    field: RecipeOptionField,
    value: RecipeOptionValue,
  ): void {
    this.recipeDialogValues.update((values) => ({
      ...values,
      [field.key]: value,
    }));

    const error = this.validateOptionField(field, value);
    this.recipeDialogErrors.update((errors) => {
      const next = { ...errors };
      if (error) {
        next[field.key] = error;
      } else {
        delete next[field.key];
      }
      return next;
    });
  }

  private updateDialogNameError(): void {
    const name = this.recipeDialogName().trim();
    const error = name
      ? ''
      : this.i18n.t('home.recipeDialog.error.nameRequired');
    this.recipeDialogErrors.update((errors) => {
      const next = { ...errors };
      if (error) {
        next['name'] = error;
      } else {
        delete next['name'];
      }
      return next;
    });
  }

  private validateAllDialogFields(): void {
    this.updateDialogNameError();

    const fields = this.recipeDialogFields();
    const values = this.recipeDialogValues();
    const errors: Record<string, string> = {};
    for (const field of fields) {
      const error = this.validateOptionField(field, values[field.key] ?? '');
      if (error) {
        errors[field.key] = error;
      }
    }

    this.recipeDialogErrors.update((current) => ({
      ...current,
      ...errors,
    }));
  }

  private validateOptionField(
    field: RecipeOptionField,
    raw: string | boolean,
  ): string {
    const error = optionValidationError(
      field,
      raw,
      this.optionLabel(field),
      (choice) =>
        this.i18n.tOr(`recipeRun.choice.${choice.value}`, choice.label),
    );

    return error ? this.i18n.t(error.key, error.params) : '';
  }

  private optionLabel(field: RecipeOptionField): string {
    return this.i18n.tOr(`recipeRun.option.${field.key}`, field.label);
  }

  private buildDialogOptions(): Record<string, unknown> {
    return optionRequestValues(
      this.recipeDialogFields(),
      this.recipeDialogValues(),
    );
  }

  readonly recent = this.recentTools.recent;
  readonly showRecent = computed(() => !this.hasFilters() && this.recent().length > 0);

  readonly recentConversions = this.recentConversionsService.recent;

  readonly shortcutLabel = formatShortcutLabel(
    findShortcutById('palette.open'),
    this.shortcuts.platform,
  );

  /** Kept for callers and specs; the runtime catalog is not the tool source. */
  catalogTools: ToolCatalogEntry[] = [];
  catalogToolsCount = 0;
  /** True once the backend catalog responds; false when only the static registry is available. */
  runtimeReachable = true;

  @ViewChild('searchInput') private searchInput?: ElementRef<HTMLInputElement>;

  ngOnInit(): void {
    void this.loadData();
    void this.loadAutomationStatus();
    void this.loadRecipes();
  }

  isRuntimeAvailable(): boolean {
    return this.wailsService.isRuntimeAvailable();
  }

  onSearchInput(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    this.query.set(target?.value ?? '');
  }

  onSearchKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter') {
      return;
    }

    event.preventDefault();
    const [first] = this.filteredTools();
    if (first) {
      void this.router.navigateByUrl(first.route);
    }
  }

  clearSearch(): void {
    this.query.set('');
    this.searchInput?.nativeElement.focus();
  }

  selectCategory(category: CategoryFilter): void {
    this.activeCategory.set(category);
  }

  clearFilters(): void {
    this.query.set('');
    this.activeCategory.set('all');
  }

  dismissOnboarding(): void {
    this.onboardingDismissedState.set(true);
    try {
      this.document?.defaultView?.localStorage?.setItem(ONBOARDING_STORAGE_KEY, '1');
    } catch {
      // Storage can be unavailable; dismissal still holds for this session.
    }
  }

  clearRecent(): void {
    this.recentConversionsService.clear();
    this.actionMessage.set('');
  }

  toolFor(entry: RecentConversion): ToolDefinition | null {
    return (
      this.registry.findById(entry.toolId) ?? this.registry.findByRoute(entry.route)
    );
  }

  toolName(entry: RecentConversion): string {
    const tool = this.toolFor(entry);
    return tool ? this.registry.toolName(tool) : entry.toolName;
  }

  statusLabel(status: string): string {
    const key = jobStatusDefinition(status)?.labels.home;
    return key ? this.i18n.t(key) : status;
  }

  statusClass(status: string): string {
    return jobStatusDefinition(status)?.tones.home ?? 'badge-neutral';
  }

  firstOutput(entry: RecentConversion): string {
    return entry.outputs[0] ?? '';
  }

  outputFileName(entry: RecentConversion): string {
    const first = this.firstOutput(entry);
    return first ? fileName(first) : '';
  }

  outputDirectory(entry: RecentConversion): string {
    const first = this.firstOutput(entry);
    return first ? fileDir(first) : '';
  }

  outputSummary(entry: RecentConversion): string {
    if (entry.outputs.length === 0) {
      return entry.status === 'failed' || entry.status === 'cancelled'
        ? this.i18n.t('home.output.noneFailed')
        : this.i18n.t('home.output.none');
    }

    const first = fileName(entry.outputs[0]);
    return entry.outputs.length === 1
      ? first
      : this.i18n.t('home.output.multiple', {
          count: entry.outputs.length,
          first,
        });
  }

  canOpen(entry: RecentConversion): boolean {
    const first = this.firstOutput(entry);
    return !!first && fileExtension(first).length > 0;
  }

  relativeTime(timestamp: number): string {
    return formatRelativeTime(
      timestamp,
      this.i18n.locale(),
      (key, params) => this.i18n.t(key, params),
    );
  }

  async openRecent(entry: RecentConversion): Promise<void> {
    const path = this.firstOutput(entry);
    if (!path) {
      return;
    }

    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      this.actionFailureMessage(response, this.i18n.t('home.error.openFile')),
    );
  }

  async revealRecent(entry: RecentConversion): Promise<void> {
    const directory = this.outputDirectory(entry);
    if (!directory) {
      return;
    }

    const response = await this.wailsService.revealInFolder(directory);
    this.actionMessage.set(
      this.actionFailureMessage(response, this.i18n.t('home.error.openFolder')),
    );
  }

  private actionFailureMessage(
    response: PathActionResponse,
    fallback: string,
  ): string {
    if (response.success) {
      return '';
    }

    const message = response.message?.trim() ?? '';
    return message.length > 0 && message.length <= 160 && !/[<>]/.test(message)
      ? message
      : fallback;
  }

  private readOnboardingDismissed(): boolean {
    try {
      return (
        this.document?.defaultView?.localStorage?.getItem(ONBOARDING_STORAGE_KEY) ===
        '1'
      );
    } catch {
      return false;
    }
  }

  /**
   * Automation status is independent of the tool catalog: a catalog failure
   * must not hide the folder-rule card. Zero rules hides the card entirely.
   */
  private async loadAutomationStatus(): Promise<void> {
    try {
      const response = await this.wailsService.getAutomationStatus();
      if (response.success && response.totalRules > 0) {
        this.automationStatus.set(response);
        return;
      }
      this.automationStatus.set(null);
    } catch {
      this.automationStatus.set(null);
    }
  }

  private async loadData(): Promise<void> {
    try {
      const catalogResponse = await this.wailsService.listTools();
      if (!catalogResponse.success) {
        this.catalogTools = [];
        this.catalogToolsCount = 0;
        this.runtimeReachable = false;
        return;
      }

      this.catalogTools = catalogResponse.tools;
      this.catalogToolsCount = catalogResponse.tools.length;
      this.runtimeReachable = true;
    } catch {
      this.catalogTools = [];
      this.catalogToolsCount = 0;
      this.runtimeReachable = false;
    }
  }
}
