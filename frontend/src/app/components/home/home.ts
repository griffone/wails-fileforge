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
  X,
} from 'lucide-angular';

import { RecipeDefinition, RECIPES } from '../../config/recipes';
import { ToolCategory, ToolDefinition } from '../../config/tools';
import {
  RecentConversion,
  RecentConversionsService,
} from '../../services/recent-conversions.service';
import { RecentToolsService } from '../../services/recent-tools.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { ToolCatalogEntryV1, PathActionResponseV1, Wails } from '../../services/wails';
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

  readonly query = signal('');
  readonly activeCategory = signal<CategoryFilter>('all');
  readonly actionMessage = signal('');

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
        [tool.name, tool.description, ...tool.formats, ...tool.keywords].join(' '),
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
  catalogTools: ToolCatalogEntryV1[] = [];
  catalogToolsCount = 0;
  /** True once the backend catalog responds; false when only the static registry is available. */
  runtimeReachable = true;

  @ViewChild('searchInput') private searchInput?: ElementRef<HTMLInputElement>;

  ngOnInit(): void {
    void this.loadData();
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
    return this.toolFor(entry)?.name ?? entry.toolName;
  }

  statusLabel(status: string): string {
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
        ? 'No files created'
        : 'No outputs';
    }

    const first = fileName(entry.outputs[0]);
    return entry.outputs.length === 1
      ? first
      : `${entry.outputs.length} files · ${first} + more`;
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
      : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }

  async openRecent(entry: RecentConversion): Promise<void> {
    const path = this.firstOutput(entry);
    if (!path) {
      return;
    }

    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      this.actionFailureMessage(response, 'The file could not be opened.'),
    );
  }

  async revealRecent(entry: RecentConversion): Promise<void> {
    const directory = this.outputDirectory(entry);
    if (!directory) {
      return;
    }

    const response = await this.wailsService.revealInFolder(directory);
    this.actionMessage.set(
      this.actionFailureMessage(response, 'The folder could not be opened.'),
    );
  }

  private actionFailureMessage(
    response: PathActionResponseV1,
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

  private async loadData(): Promise<void> {
    try {
      const catalogResponse = await this.wailsService.listToolsV1();
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
