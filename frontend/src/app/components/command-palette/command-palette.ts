import {
  AfterViewChecked,
  Component,
  ElementRef,
  EventEmitter,
  HostListener,
  Input,
  OnChanges,
  Output,
  SimpleChanges,
  ViewChild,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  ArrowDown,
  ArrowUp,
  CornerDownLeft,
  LucideAngularModule,
  Search,
  SearchX,
} from 'lucide-angular';

import { ToolDefinition } from '../../config/tools';
import { RecentToolsService } from '../../services/recent-tools.service';
import { ToolRegistryService } from '../../services/tool-registry.service';

/** A single selectable row, carrying its position in the flattened result list. */
interface PaletteEntry {
  tool: ToolDefinition;
  categoryLabel: string;
  meta: string;
  index: number;
}

interface PaletteGroup {
  id: string;
  label: string;
  entries: PaletteEntry[];
}

interface ScoredTool {
  tool: ToolDefinition;
  score: number;
}

const DIACRITICS = /[\u0300-\u036f]/g;

function normalize(value: string): string {
  return value.normalize('NFD').replace(DIACRITICS, '').toLowerCase();
}

/**
 * Command palette: one searchable index over every workbench destination.
 *
 * Browse mode mirrors the navigation rail with a Recent group on top. Search
 * mode folds the query into name, nav label, description, category, formats and
 * keywords, then groups the matches by category with name matches ranked first.
 */
@Component({
  selector: 'app-command-palette',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './command-palette.html',
  styleUrl: './command-palette.css',
})
export class CommandPalette implements OnChanges, AfterViewChecked {
  private readonly registry = inject(ToolRegistryService);
  private readonly recentTools = inject(RecentToolsService);

  @Input() open = false;

  @Output() closed = new EventEmitter<void>();
  @Output() navigate = new EventEmitter<string>();

  readonly searchIcon = Search;
  readonly emptyIcon = SearchX;
  readonly upIcon = ArrowUp;
  readonly downIcon = ArrowDown;
  readonly enterIcon = CornerDownLeft;

  readonly query = signal('');
  readonly activeIndex = signal(0);

  @ViewChild('searchInput') private searchInput?: ElementRef<HTMLInputElement>;
  @ViewChild('listbox') private listbox?: ElementRef<HTMLElement>;

  private focusPending = false;

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['open'] && this.open) {
      this.query.set('');
      this.activeIndex.set(0);
      this.focusPending = true;
      this.scrollListToTop();
    }
  }

  ngAfterViewChecked(): void {
    if (!this.focusPending) {
      return;
    }

    this.focusPending = false;
    this.searchInput?.nativeElement.focus();
  }

  readonly groups = computed(() =>
    this.withIndexes(this.query().trim() ? this.searchGroups(this.query()) : this.browseGroups()),
  );

  readonly flatEntries = computed(() => this.groups().flatMap((group) => group.entries));

  optionId(index: number): string {
    return `palette-option-${index}`;
  }

  isActive(index: number): boolean {
    return this.activeIndex() === index;
  }

  setActive(index: number): void {
    this.activeIndex.set(index);
  }

  onQueryInput(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    this.query.set(target?.value ?? '');
    this.activeIndex.set(0);
    this.scrollListToTop();
  }

  onInputKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        this.moveActive(1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        this.moveActive(-1);
        break;
      case 'Home':
        event.preventDefault();
        this.jumpTo(0);
        break;
      case 'End':
        event.preventDefault();
        this.jumpTo(this.flatEntries().length - 1);
        break;
      case 'Enter':
        event.preventDefault();
        this.selectActive();
        break;
      default:
        break;
    }
  }

  onBackdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) {
      this.close();
    }
  }

  select(entry: PaletteEntry): void {
    this.navigate.emit(entry.tool.route);
    this.closed.emit();
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.open) {
      return;
    }

    if (event.key === 'Tab') {
      // The palette has a single focus target; keep Tab inside the dialog.
      event.preventDefault();
      this.searchInput?.nativeElement.focus();
      return;
    }

    if (event.key !== 'Escape') {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    this.close();
  }

  private close(): void {
    this.closed.emit();
  }

  private selectActive(): void {
    const entry = this.flatEntries()[this.activeIndex()];
    if (entry) {
      this.select(entry);
    }
  }

  private moveActive(delta: number): void {
    const total = this.flatEntries().length;
    if (total === 0) {
      return;
    }

    const next = (this.activeIndex() + delta + total) % total;
    this.jumpTo(next);
  }

  private jumpTo(index: number): void {
    const total = this.flatEntries().length;
    if (total === 0) {
      return;
    }

    const clamped = Math.min(Math.max(index, 0), total - 1);
    this.activeIndex.set(clamped);
    this.scrollActiveIntoView(clamped);
  }

  private scrollActiveIntoView(index: number): void {
    const listbox = this.listbox?.nativeElement;
    if (!listbox) {
      return;
    }

    const option = listbox.querySelector<HTMLElement>(`[data-index="${index}"]`);
    option?.scrollIntoView?.({ block: 'nearest' });
  }

  private scrollListToTop(): void {
    const listbox = this.listbox?.nativeElement;
    listbox?.scrollTo?.({ top: 0 });
  }

  private browseGroups(): PaletteGroup[] {
    const groups: PaletteGroup[] = [];
    const recent = this.recentTools.recent();

    if (recent.length > 0) {
      groups.push({
        id: 'recent',
        label: 'Recent',
        entries: recent.map((tool) => this.toEntry(tool, 'recent')),
      });
    }

    for (const group of this.registry.navGroups) {
      if (group.items.length === 0) {
        continue;
      }

      groups.push({
        id: group.id,
        label: group.label,
        entries: group.items.map((tool) => this.toEntry(tool, group.id)),
      });
    }

    return groups;
  }

  private searchGroups(rawQuery: string): PaletteGroup[] {
    const tokens = normalize(rawQuery).split(/\s+/).filter(Boolean);
    if (tokens.length === 0) {
      return [];
    }

    const scored: ScoredTool[] = [];
    for (const tool of this.registry.navigation) {
      const score = this.scoreTool(tool, tokens);
      if (score !== null) {
        scored.push({ tool, score });
      }
    }

    const ranked: Array<{ order: number; best: number; group: PaletteGroup }> = [];

    this.registry.categories.forEach((category, order) => {
      const matches = scored
        .filter((entry) => entry.tool.category === category.id)
        .sort((a, b) => b.score - a.score || a.tool.name.localeCompare(b.tool.name));

      if (matches.length === 0) {
        return;
      }

      ranked.push({
        order,
        best: matches[0].score,
        group: {
          id: category.id,
          label: category.label,
          entries: matches.map((entry) => this.toEntry(entry.tool, category.id)),
        },
      });
    });

    // Strongest group first so name matches always outrank description hits.
    ranked.sort((a, b) => b.best - a.best || a.order - b.order);
    return ranked.map((entry) => entry.group);
  }

  /**
   * Returns a relevance score, or null when any query token is missing.
   * Name and nav-label matches outrank keyword, format and description hits.
   */
  private scoreTool(tool: ToolDefinition, tokens: string[]): number | null {
    const name = normalize(tool.name);
    const navName = normalize(tool.navName);
    const description = normalize(tool.description);
    const category = normalize(this.registry.categoryLabel(tool.category));
    const formats = tool.formats.map((format) => normalize(format));
    const keywords = tool.keywords.map((keyword) => normalize(keyword));

    let total = 0;
    for (const token of tokens) {
      let best = 0;

      if (name === token) {
        best = 1200;
      } else if (navName === token) {
        best = 1100;
      } else if (name.startsWith(token)) {
        best = 900;
      } else if (navName.startsWith(token)) {
        best = 820;
      } else if (keywords.some((keyword) => keyword === token)) {
        best = 760;
      } else if (name.includes(token)) {
        best = 640;
      } else if (navName.includes(token)) {
        best = 560;
      } else if (keywords.some((keyword) => keyword.includes(token))) {
        best = 480;
      } else if (formats.some((format) => format.includes(token))) {
        best = 400;
      } else if (category.includes(token)) {
        best = 340;
      } else if (description.includes(token)) {
        best = 220;
      } else {
        return null;
      }

      total += best;
    }

    return total;
  }

  private toEntry(tool: ToolDefinition, groupId: string): PaletteEntry {
    const categoryLabel = this.registry.categoryLabel(tool.category);
    const formats = tool.formats.slice(0, 3).join(' · ');

    return {
      tool,
      categoryLabel,
      meta: groupId === 'recent' ? categoryLabel : formats || categoryLabel,
      index: 0,
    };
  }

  private withIndexes(groups: PaletteGroup[]): PaletteGroup[] {
    let index = 0;
    for (const group of groups) {
      for (const entry of group.entries) {
        entry.index = index;
        index += 1;
      }
    }

    return groups;
  }
}
