import { DOCUMENT } from '@angular/common';
import {
  Component,
  ElementRef,
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
  ExternalLink,
  FolderOpen,
  LucideAngularModule,
  Search,
  SearchX,
  ShieldCheck,
  Workflow,
  X,
} from 'lucide-angular';

import { RecipeDefinition, RECIPES } from '../../config/recipes';
import { ToolCategory, ToolDefinition } from '../../config/tools';
import { I18nService } from '../../services/i18n.service';
import {
  RecentConversion,
  RecentConversionsService,
} from '../../services/recent-conversions.service';
import { RecentToolsService } from '../../services/recent-tools.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { ToolCatalogEntry, PathActionResponse, AutomationStatusResponse, Wails } from '../../services/wails';
import { fileDir, fileExtension, fileName } from '../../utils/format';

type CategoryFilter = 'all' | ToolCategory;

const DIACRITICS = /[\u0300-\u036f]/g;
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

function normalize(value: string): string {
  return value.normalize('NFD').replace(DIACRITICS, '').toLowerCase();
}

@Component({
  selector: 'app-home',
  standalone: true,
  imports: [RouterLink, LucideAngularModule],
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

  readonly searchIcon = Search;
  readonly clearIcon = X;
  readonly emptyIcon = SearchX;
  readonly arrowIcon = ArrowUpRight;
  readonly shieldIcon = ShieldCheck;
  readonly openIcon = ExternalLink;
  readonly folderIcon = FolderOpen;
  readonly automationIcon = Workflow;

  readonly query = signal('');
  readonly activeCategory = signal<CategoryFilter>('all');
  readonly actionMessage = signal('');
  readonly automationStatus = signal<AutomationStatusResponse | null>(null);

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

  readonly recent = this.recentTools.recent;
  readonly showRecent = computed(() => !this.hasFilters() && this.recent().length > 0);

  readonly recentConversions = this.recentConversionsService.recent;

  readonly shortcutLabel = this.resolveShortcutLabel();

  /** Kept for callers and specs; the runtime catalog is not the tool source. */
  catalogTools: ToolCatalogEntry[] = [];
  catalogToolsCount = 0;
  /** True once the backend catalog responds; false when only the static registry is available. */
  runtimeReachable = true;

  @ViewChild('searchInput') private searchInput?: ElementRef<HTMLInputElement>;

  ngOnInit(): void {
    void this.loadData();
    void this.loadAutomationStatus();
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
      default:
        return status;
    }
  }

  statusClass(status: string): string {
    switch (status) {
      case 'success':
        return 'badge-success';
      case 'partial_success':
      case 'interrupted':
        return 'badge-warning';
      case 'failed':
        return 'badge-danger';
      default:
        return 'badge-neutral';
    }
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
    if (!Number.isFinite(timestamp) || timestamp <= 0) {
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

  private resolveShortcutLabel(): string {
    if (typeof navigator === 'undefined') {
      return 'Ctrl K';
    }

    const platform = navigator.platform ?? '';
    const isMac = /mac/i.test(platform) || /mac os x/i.test(navigator.userAgent);
    return isMac ? '⌘K' : 'Ctrl K';
  }
}
