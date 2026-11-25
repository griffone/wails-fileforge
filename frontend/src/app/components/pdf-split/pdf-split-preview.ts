import {
  Component,
  EventEmitter,
  Input,
  OnChanges,
  OnDestroy,
  Output,
  SimpleChanges,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  Check,
  LucideAngularModule,
  TriangleAlert,
} from 'lucide-angular';

import { I18nService } from '../../services/i18n.service';
import { PdfRendererService } from '../../services/pdf-renderer.service';
import { Wails } from '../../services/wails';
import { fileName, formatCount } from '../../utils/format';
import type { TranslationKey, TranslationParams } from '../../i18n';

const STRATEGY_EVERY_PAGE = 'every_page';
const STRATEGY_RANGES = 'ranges';
const STRATEGY_SELECTED = 'selected';
const STRATEGY_PAGES_PER_FILE = 'pages_per_file';
const STRATEGY_BOOKMARKS = 'bookmarks';
const THUMBNAIL_WIDTH = 140;
const INITIAL_PAGE_LIMIT = 60;
const JPEG_QUALITY = 0.72;
const DEFAULT_PAGES_PER_FILE = 5;
const MAX_PAGES_PER_FILE = 1000;

export type SplitPreviewStrategy =
  | 'every_page'
  | 'ranges'
  | 'selected'
  | 'pages_per_file'
  | 'bookmarks';

interface PreviewPage {
  page: number;
}

interface PreviewGroup {
  key: string;
  part: number;
  start: number;
  end: number;
  outputName: string;
  /** Pages whose thumbnails are inside the current render window. */
  pages: PreviewPage[];
  /** Total pages in the resulting file, even beyond the render window. */
  pageCount: number;
}

export interface PageRange {
  start: number;
  end: number;
}

interface RangeParseResult {
  ranges: PageRange[];
  error: TranslationKey | null;
  errorParams?: TranslationParams;
}

const RANGE_TOKEN = /^(\d+)(?:\s*-\s*(\d+))?$/;

/**
 * Previews the files a PDF split will create. The PDF is loaded once through
 * the shared renderer; strategy/range edits only regroup the cached pages.
 */
@Component({
  selector: 'app-pdf-split-preview',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './pdf-split-preview.html',
  styleUrl: './pdf-split-preview.css',
})
export class PdfSplitPreview implements OnChanges, OnDestroy {
  @Input() paths: string[] = [];
  @Input() strategy: SplitPreviewStrategy = STRATEGY_EVERY_PAGE;
  @Input() ranges = '';
  @Input() selectedPages: number[] = [];
  @Input() pagesPerFile = DEFAULT_PAGES_PER_FILE;
  @Input() selectable = false;

  @Output() readonly selectedPagesChange = new EventEmitter<number[]>();

  readonly alertIcon = TriangleAlert;
  readonly checkIcon = Check;
  readonly pageLimit = INITIAL_PAGE_LIMIT;

  readonly i18n = inject(I18nService);

  readonly files = signal<string[]>([]);
  readonly selectedPath = signal('');
  readonly loading = signal(false);
  readonly unavailable = signal(false);
  readonly rendering = signal(false);
  readonly rangeError = signal<TranslationKey | null>(null);
  readonly rangeErrorParams = signal<TranslationParams | undefined>(undefined);
  readonly groups = signal<PreviewGroup[]>([]);
  readonly thumbnails = signal<Record<number, string>>({});
  readonly limited = signal(false);
  readonly totalPages = signal(0);
  readonly countValue = signal(0);
  readonly countKind = signal<'file' | 'part' | ''>('');
  readonly showAllRequested = signal(false);
  /** Page cards offered for selection in selected mode. */
  readonly selectablePages = signal<number[]>([]);
  /** Local mirror of the selection so clicks update before the parent does. */
  readonly selection = signal<number[]>([]);

  readonly selectedFileName = computed(() => fileName(this.selectedPath()));
  readonly selectionLabel = computed(() => {
    const count = this.selection().length;
    const pages = formatCount(
      count,
      this.i18n.t('common.pageOne'),
      this.i18n.t('common.pageMany'),
    );
    return count === 1
      ? this.i18n.t('pdfSplit.preview.selectionOne', { count: pages })
      : this.i18n.t('pdfSplit.preview.selectionMany', { count: pages });
  });

  readonly countLabel = computed(() => {
    const kind = this.countKind();
    const value = this.countValue();
    if (!kind || value <= 0) {
      return '';
    }
    return kind === 'part'
      ? formatCount(
          value,
          this.i18n.t('pdfSplit.unit.partOne'),
          this.i18n.t('pdfSplit.unit.partMany'),
        )
      : formatCount(
          value,
          this.i18n.t('common.fileOne'),
          this.i18n.t('common.fileMany'),
        );
  });

  readonly rangeErrorText = computed(() => {
    const key = this.rangeError();
    return key ? this.i18n.t(key, this.rangeErrorParams()) : '';
  });

  private readonly pdf = signal<unknown>(null);
  private readonly numPages = signal(0);
  private loadedPath = '';

  private loadToken = 0;
  private generation = 0;
  private renderQueue: number[] = [];
  private readonly queuedPages = new Set<number>();
  private readonly failedPages = new Set<number>();
  private drainActive = false;

  constructor(
    private readonly wailsService: Wails,
    private readonly pdfRenderer: PdfRendererService,
  ) {}

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['paths']) {
      const files = this.normalizePaths(this.paths);
      this.files.set(files);
      const current = this.selectedPath();
      const next = files.includes(current) ? current : (files[0] ?? '');
      if (next !== current) {
        this.showAllRequested.set(false);
      }
      this.selectedPath.set(next);
      void this.loadSelectedPdf();
    }

    if (changes['selectedPages']) {
      this.selection.set(normalizePageSelection(this.selectedPages));
    }

    if (changes['strategy'] || changes['ranges'] || changes['pagesPerFile']) {
      this.showAllRequested.set(false);
      this.rebuildView();
    } else if (changes['selectedPages'] || changes['selectable']) {
      this.rebuildView();
    }
  }

  ngOnDestroy(): void {
    this.generation += 1;
    this.renderQueue = [];
    this.queuedPages.clear();
    const pdf = this.pdf();
    this.pdf.set(null);
    if (pdf) {
      void this.pdfRenderer.destroy(pdf);
    }
  }

  fileLabel(path: string): string {
    return fileName(path);
  }

  fileCountLabel(count: number): string {
    return formatCount(
      count,
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
    );
  }

  /** Group headers resolved at render time so the locale switch applies. */
  groupTitle(group: PreviewGroup): string {
    return group.start === group.end
      ? this.i18n.t('pdfSplit.preview.partPage', {
          part: group.part,
          page: group.start,
        })
      : this.i18n.t('pdfSplit.preview.partPages', {
          part: group.part,
          start: group.start,
          end: group.end,
        });
  }

  thumbnail(page: number): string {
    return this.thumbnails()[page] ?? '';
  }

  onFileChange(event: Event): void {
    const path = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (!path || path === this.selectedPath()) {
      return;
    }

    this.showAllRequested.set(false);
    this.selectedPath.set(path);
    void this.loadSelectedPdf();
  }

  showAll(): void {
    if (this.showAllRequested()) {
      return;
    }
    this.showAllRequested.set(true);
    this.rebuildView();
  }

  /** Selected strategy, or a caller that explicitly asks for page picking. */
  isSelectable(): boolean {
    return this.selectable || this.strategy === STRATEGY_SELECTED;
  }

  isPageSelected(page: number): boolean {
    return this.selection().includes(page);
  }

  togglePage(page: number): void {
    if (!this.isSelectable()) {
      return;
    }
    const next = new Set(this.selection());
    if (next.has(page)) {
      next.delete(page);
    } else {
      next.add(page);
    }
    this.applySelection([...next]);
  }

  selectAll(): void {
    const pages = Array.from(
      { length: this.numPages() },
      (_, index) => index + 1,
    );
    this.applySelection(pages);
  }

  clearSelection(): void {
    this.applySelection([]);
  }

  private applySelection(pages: number[]): void {
    const next = normalizePageSelection(pages);
    if (samePageList(next, this.selection())) {
      return;
    }
    this.selection.set(next);
    this.rebuildView();
    this.selectedPagesChange.emit([...next]);
  }

  private normalizePaths(paths: string[]): string[] {
    const seen = new Set<string>();
    const normalized: string[] = [];
    for (const raw of Array.isArray(paths) ? paths : []) {
      const path = (raw ?? '').trim();
      if (path && !seen.has(path)) {
        seen.add(path);
        normalized.push(path);
      }
    }
    return normalized;
  }

  private async loadSelectedPdf(): Promise<void> {
    const path = this.selectedPath();
    if (!path) {
      this.loadToken += 1;
      this.resetPreview();
      return;
    }

    if (path === this.loadedPath && this.pdf()) {
      this.rebuildView();
      return;
    }

    const token = (this.loadToken += 1);
    this.resetPreview();
    this.loading.set(true);

    let source: Awaited<ReturnType<Wails['getPdfPreviewSource']>> | null = null;
    try {
      source = await this.wailsService.getPdfPreviewSource(path);
    } catch {
      source = null;
    }
    if (token !== this.loadToken) {
      return;
    }

    if (!source?.success || !source.dataBase64) {
      this.failPreview();
      return;
    }

    let pdf: unknown;
    try {
      pdf = await this.pdfRenderer.loadFromBytes(
        decodeBase64(source.dataBase64),
      );
    } catch {
      if (token === this.loadToken) {
        this.failPreview();
      }
      return;
    }

    if (token !== this.loadToken) {
      return;
    }

    this.loadedPath = path;
    this.pdf.set(pdf);
    this.numPages.set(readPageCount(pdf));
    this.loading.set(false);
    this.rebuildView();
  }

  private resetPreview(): void {
    this.generation += 1;
    this.renderQueue = [];
    this.queuedPages.clear();
    this.failedPages.clear();
    this.loadedPath = '';
    this.pdf.set(null);
    this.numPages.set(0);
    this.thumbnails.set({});
    this.groups.set([]);
    this.rangeError.set(null);
    this.rangeErrorParams.set(undefined);
    this.countValue.set(0);
    this.countKind.set('');
    this.limited.set(false);
    this.totalPages.set(0);
    this.selectablePages.set([]);
    this.unavailable.set(false);
    this.rendering.set(false);
  }

  private failPreview(): void {
    this.loading.set(false);
    this.unavailable.set(true);
    this.numPages.set(0);
    this.groups.set([]);
    this.countValue.set(0);
    this.countKind.set('');
    this.limited.set(false);
    this.totalPages.set(0);
    this.selectablePages.set([]);
    this.rangeError.set(null);
    this.rangeErrorParams.set(undefined);
  }

  private rebuildView(): void {
    if (!this.pdf() || this.numPages() <= 0) {
      return;
    }

    this.rangeError.set(null);
    this.rangeErrorParams.set(undefined);
    this.renderQueue = [];
    this.queuedPages.clear();

    if (this.isSelectable()) {
      this.buildSelectedView(this.numPages());
      return;
    }

    this.selectablePages.set([]);

    if (this.strategy === STRATEGY_RANGES) {
      this.buildRangeGroups(this.numPages());
      return;
    }

    if (this.strategy === STRATEGY_BOOKMARKS) {
      this.buildBookmarkPlaceholder();
      return;
    }

    if (this.strategy === STRATEGY_PAGES_PER_FILE) {
      this.buildPagesPerFileGroups(this.numPages());
      return;
    }

    this.buildEveryPageGroups(this.numPages());
  }

  private buildEveryPageGroups(numPages: number): void {
    const visible = this.showAllRequested()
      ? numPages
      : Math.min(numPages, INITIAL_PAGE_LIMIT);
    const pages = Array.from({ length: visible }, (_, index) => index + 1);
    const groups: PreviewGroup[] = pages.map((page) => ({
      key: `page-${page}`,
      part: page,
      start: page,
      end: page,
      outputName: everyPageOutputName(page),
      pages: [{ page }],
      pageCount: 1,
    }));

    this.groups.set(groups);
    this.countValue.set(numPages);
    this.countKind.set('file');
    this.totalPages.set(numPages);
    this.limited.set(visible < numPages);
    this.scheduleRender(pages);
  }

  private buildPagesPerFileGroups(numPages: number): void {
    const chunkSize = normalizePagesPerFile(this.pagesPerFile);
    const allPages = Array.from({ length: numPages }, (_, index) => index + 1);
    const visiblePages = this.showAllRequested()
      ? allPages
      : allPages.slice(0, INITIAL_PAGE_LIMIT);

    const groups: PreviewGroup[] = [];
    let part = 1;
    for (let start = 1; start <= numPages; start += chunkSize) {
      const end = Math.min(start + chunkSize - 1, numPages);
      groups.push({
        key: `pages-${part}-${start}-${end}`,
        part,
        start,
        end,
        outputName: pagesPerFileOutputName(part, { start, end }),
        pages: visiblePages
          .filter((page) => page >= start && page <= end)
          .map((page) => ({ page })),
        pageCount: end - start + 1,
      });
      part += 1;
    }

    this.groups.set(groups);
    this.countValue.set(groups.length);
    this.countKind.set('file');
    this.totalPages.set(numPages);
    this.limited.set(visiblePages.length < numPages);
    this.scheduleRender(visiblePages);
  }

  /**
   * Bookmark spans are resolved by the backend plan, so the preview only
   * explains where the results will appear instead of faking thumbnails.
   */
  private buildBookmarkPlaceholder(): void {
    this.groups.set([]);
    this.countValue.set(0);
    this.countKind.set('');
    this.totalPages.set(0);
    this.limited.set(false);
    this.selectablePages.set([]);
  }

  private buildRangeGroups(numPages: number): void {
    const parsed = parseRanges(this.ranges, numPages);
    if (parsed.error) {
      this.groups.set([]);
      this.rangeError.set(parsed.error);
      this.rangeErrorParams.set(parsed.errorParams);
      this.countValue.set(0);
      this.countKind.set('');
      this.totalPages.set(0);
      this.limited.set(false);
      return;
    }

    const allPages = flattenRanges(parsed.ranges);
    const visiblePages = this.showAllRequested()
      ? allPages
      : allPages.slice(0, INITIAL_PAGE_LIMIT);
    const groups: PreviewGroup[] = parsed.ranges.map((range, index) => {
      const part = index + 1;
      return {
        key: `range-${part}-${range.start}-${range.end}`,
        part,
        start: range.start,
        end: range.end,
        outputName: rangeOutputName(part, range),
        pages: visiblePages
          .filter((page) => page >= range.start && page <= range.end)
          .map((page) => ({ page })),
        pageCount: range.end - range.start + 1,
      };
    });

    this.groups.set(groups);
    this.countValue.set(parsed.ranges.length);
    this.countKind.set('part');
    this.totalPages.set(allPages.length);
    this.limited.set(visiblePages.length < allPages.length);
    this.scheduleRender(visiblePages);
  }

  private buildSelectedView(numPages: number): void {
    const allPages = Array.from({ length: numPages }, (_, index) => index + 1);
    const visiblePages = this.showAllRequested()
      ? allPages
      : allPages.slice(0, INITIAL_PAGE_LIMIT);
    const runs = groupConsecutivePages(this.selection());

    const groups: PreviewGroup[] = runs.map((range, index) => {
      const part = index + 1;
      return {
        key: `selected-${part}-${range.start}-${range.end}`,
        part,
        start: range.start,
        end: range.end,
        outputName: rangeOutputName(part, range),
        pages: allPages
          .filter((page) => page >= range.start && page <= range.end)
          .map((page) => ({ page })),
        pageCount: range.end - range.start + 1,
      };
    });

    this.selectablePages.set(visiblePages);
    this.groups.set(groups);
    this.countValue.set(groups.length);
    this.countKind.set(groups.length > 0 ? 'part' : '');
    this.totalPages.set(numPages);
    this.limited.set(visiblePages.length < numPages);
    // Selected pages outside the window still need their part thumbnails.
    this.scheduleRender([...new Set([...visiblePages, ...this.selection()])]);
  }

  private scheduleRender(pages: number[]): void {
    const rendered = this.thumbnails();
    for (const page of pages) {
      if (
        rendered[page] ||
        this.failedPages.has(page) ||
        this.queuedPages.has(page)
      ) {
        continue;
      }
      this.queuedPages.add(page);
      this.renderQueue.push(page);
    }
    void this.drainRenderQueue();
  }

  private async drainRenderQueue(): Promise<void> {
    if (this.drainActive) {
      return;
    }

    this.drainActive = true;
    this.rendering.set(true);
    try {
      while (this.renderQueue.length > 0) {
        const page = this.renderQueue.shift() as number;
        const generation = this.generation;
        // Yield so thumbnails appear progressively and scrolling stays smooth.
        await this.yieldToBrowser();
        if (generation !== this.generation) {
          break;
        }

        const pdf = this.pdf();
        if (!pdf) {
          break;
        }

        const thumbnail = await this.renderThumbnail(pdf, page, generation);
        if (generation === this.generation) {
          this.queuedPages.delete(page);
        }
        if (thumbnail && generation === this.generation) {
          this.thumbnails.update((thumbs) => ({ ...thumbs, [page]: thumbnail }));
        }
      }
    } finally {
      this.drainActive = false;
      this.rendering.set(false);
    }

    if (this.renderQueue.length > 0 && this.pdf()) {
      void this.drainRenderQueue();
    }
  }

  private async renderThumbnail(
    pdf: unknown,
    pageNumber: number,
    generation: number,
  ): Promise<string> {
    try {
      const page = await (pdf as PdfDocumentLike).getPage(pageNumber);
      if (generation !== this.generation) {
        return '';
      }

      const viewport = page?.getViewport?.({ scale: 1 });
      const width = Number(viewport?.width) || THUMBNAIL_WIDTH;
      const scale = THUMBNAIL_WIDTH / width;
      const canvas = document.createElement('canvas');
      await this.pdfRenderer.renderPageToCanvasWithFallback(
        pdf,
        pageNumber,
        canvas,
        scale,
        this.selectedPath(),
      );
      if (generation !== this.generation) {
        return '';
      }
      return canvas.toDataURL('image/jpeg', JPEG_QUALITY);
    } catch {
      if (generation === this.generation) {
        this.failedPages.add(pageNumber);
      }
      return '';
    }
  }

  private yieldToBrowser(): Promise<void> {
    return new Promise<void>((resolve) => {
      setTimeout(resolve, 0);
    });
  }
}

interface PdfPageLike {
  getViewport?(options: { scale: number }): { width: number; height: number };
}

interface PdfDocumentLike {
  numPages?: number;
  getPage(pageNumber: number): Promise<PdfPageLike>;
}

function readPageCount(pdf: unknown): number {
  const count = Number((pdf as PdfDocumentLike | null)?.numPages);
  return Number.isFinite(count) && count > 0 ? Math.floor(count) : 0;
}

function decodeBase64(value: string): Uint8Array {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function padThree(value: number): string {
  return String(value).padStart(3, '0');
}

function everyPageOutputName(page: number): string {
  return `page_${padThree(page)}.pdf`;
}

function rangeOutputName(part: number, range: PageRange): string {
  const suffix =
    range.start === range.end
      ? `p${range.start}`
      : `p${range.start}-${range.end}`;
  return `range_${padThree(part)}_${suffix}.pdf`;
}

function pagesPerFileOutputName(part: number, range: PageRange): string {
  const suffix =
    range.start === range.end
      ? `p${range.start}`
      : `p${range.start}-${range.end}`;
  return `part_${padThree(part)}_${suffix}.pdf`;
}

/** Falls back to the UI default when the caller sends an unusable chunk size. */
export function normalizePagesPerFile(value: number): number {
  if (
    Number.isInteger(value) &&
    value >= 1 &&
    value <= MAX_PAGES_PER_FILE
  ) {
    return value;
  }
  return DEFAULT_PAGES_PER_FILE;
}

function flattenRanges(ranges: PageRange[]): number[] {
  const pages: number[] = [];
  for (const range of ranges) {
    for (let page = range.start; page <= range.end; page += 1) {
      pages.push(page);
    }
  }
  return pages;
}

/** Sorted, de-duplicated page numbers; invalid entries are dropped. */
export function normalizePageSelection(pages: number[]): number[] {
  const unique = new Set<number>();
  for (const page of Array.isArray(pages) ? pages : []) {
    if (Number.isInteger(page) && page > 0) {
      unique.add(page);
    }
  }
  return [...unique].sort((left, right) => left - right);
}

/** Consecutive pages collapse into one run: `1,2,3,5,7,8` → `1-3`, `5`, `7-8`. */
export function groupConsecutivePages(pages: number[]): PageRange[] {
  const runs: PageRange[] = [];
  for (const page of normalizePageSelection(pages)) {
    const last = runs[runs.length - 1];
    if (last && page === last.end + 1) {
      last.end = page;
    } else {
      runs.push({ start: page, end: page });
    }
  }
  return runs;
}

/** Range syntax understood by the backend: `1,2,3,5,7,8` → `1-3,5,7-8`. */
export function pagesToRangeSpec(pages: number[]): string {
  return groupConsecutivePages(pages)
    .map((range) =>
      range.start === range.end
        ? `${range.start}`
        : `${range.start}-${range.end}`,
    )
    .join(',');
}

function samePageList(left: number[], right: number[]): boolean {
  return (
    left.length === right.length &&
    left.every((page, index) => page === right[index])
  );
}

function parseRanges(value: string, numPages: number): RangeParseResult {
  const trimmed = (value ?? '').trim();
  if (!trimmed) {
    return {
      ranges: [],
      error: 'pdfSplit.preview.error.emptyRanges',
    };
  }

  const tokens = trimmed.split(',');
  const ranges: PageRange[] = [];
  const seen = new Set<number>();

  for (const rawToken of tokens) {
    const token = rawToken.trim();
    const match = RANGE_TOKEN.exec(token);
    if (!match) {
      return {
        ranges: [],
        error: 'pdfSplit.preview.error.invalidSyntax',
      };
    }

    const start = Number(match[1]);
    const end = Number(match[2] ?? match[1]);
    if (!start || !end || start > end) {
      return {
        ranges: [],
        error: 'pdfSplit.preview.error.invalidRange',
      };
    }

    if (end > numPages) {
      return {
        ranges: [],
        error: 'pdfSplit.preview.error.outside',
        errorParams: { page: end, total: numPages },
      };
    }

    for (let page = start; page <= end; page += 1) {
      if (seen.has(page)) {
        return {
          ranges: [],
          error: 'pdfSplit.preview.error.duplicate',
          errorParams: { page },
        };
      }
      seen.add(page);
    }

    ranges.push({ start, end });
  }

  return { ranges, error: null };
}
