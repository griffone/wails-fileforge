import {
  Component,
  ElementRef,
  OnInit,
  ViewChild,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import {
  ArrowUpRight,
  LucideAngularModule,
  Search,
  SearchX,
  ShieldCheck,
  X,
} from 'lucide-angular';

import { ToolCategory } from '../../config/tools';
import { RecentToolsService } from '../../services/recent-tools.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { ToolCatalogEntryV1, Wails } from '../../services/wails';

type CategoryFilter = 'all' | ToolCategory;

const DIACRITICS = /[\u0300-\u036f]/g;

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
  private readonly router = inject(Router);
  private readonly wailsService = inject(Wails);

  readonly searchIcon = Search;
  readonly clearIcon = X;
  readonly emptyIcon = SearchX;
  readonly arrowIcon = ArrowUpRight;
  readonly shieldIcon = ShieldCheck;

  readonly query = signal('');
  readonly activeCategory = signal<CategoryFilter>('all');

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

  readonly recent = this.recentTools.recent;
  readonly showRecent = computed(() => !this.hasFilters() && this.recent().length > 0);

  readonly shortcutLabel = this.resolveShortcutLabel();

  /** Kept for callers and specs; the runtime catalog is not the tool source. */
  catalogTools: ToolCatalogEntryV1[] = [];
  catalogToolsCount = 0;

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

  private async loadData(): Promise<void> {
    try {
      const catalogResponse = await this.wailsService.listToolsV1();
      if (!catalogResponse.success) {
        this.catalogTools = [];
        this.catalogToolsCount = 0;
        return;
      }

      this.catalogTools = catalogResponse.tools;
      this.catalogToolsCount = catalogResponse.tools.length;
    } catch {
      this.catalogTools = [];
      this.catalogToolsCount = 0;
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
