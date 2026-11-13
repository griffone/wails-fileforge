import {
  Component,
  Input,
  OnChanges,
  OnDestroy,
  SimpleChanges,
  computed,
  signal,
} from '@angular/core';
import {
  LucideAngularModule,
  TriangleAlert,
} from 'lucide-angular';

import { PdfRendererService } from '../../services/pdf-renderer.service';
import { Wails } from '../../services/wails';
import { fileName, formatCount } from '../../utils/format';

const STRATEGY_EVERY_PAGE = 'every_page';
const STRATEGY_RANGES = 'ranges';
const THUMBNAIL_WIDTH = 140;
const INITIAL_PAGE_LIMIT = 60;
const JPEG_QUALITY = 0.72;

type SplitPreviewStrategy = 'every_page' | 'ranges';

interface PreviewPage {
  page: number;
}

interface PreviewGroup {
  key: string;
  title: string;
  outputName: string;
  /** Pages whose thumbnails are inside the current render window. */
  pages: PreviewPage[];
  /** Total pages in the resulting file, even beyond the render window. */
  pageCount: number;
}

interface ParsedRange {
  start: number;
  end: number;
}

interface RangeParseResult {
  ranges: ParsedRange[];
  error: string;
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

  readonly alertIcon = TriangleAlert;
  readonly pageLimit = INITIAL_PAGE_LIMIT;

  readonly files = signal<string[]>([]);
  readonly selectedPath = signal('');
  readonly loading = signal(false);
  readonly unavailable = signal(false);
  readonly rendering = signal(false);
  readonly rangeError = signal('');
  readonly groups = signal<PreviewGroup[]>([]);
  readonly thumbnails = signal<Record<number, string>>({});
  readonly limited = signal(false);
  readonly totalPages = signal(0);
  readonly countLabel = signal('');
  readonly showAllRequested = signal(false);

  readonly selectedFileName = computed(() => fileName(this.selectedPath()));

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

    if (changes['strategy'] || changes['ranges']) {
      this.showAllRequested.set(false);
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
    return formatCount(count, 'file');
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
    this.rangeError.set('');
    this.countLabel.set('');
    this.limited.set(false);
    this.totalPages.set(0);
    this.unavailable.set(false);
    this.rendering.set(false);
  }

  private failPreview(): void {
    this.loading.set(false);
    this.unavailable.set(true);
    this.numPages.set(0);
    this.groups.set([]);
    this.countLabel.set('');
    this.limited.set(false);
    this.totalPages.set(0);
    this.rangeError.set('');
  }

  private rebuildView(): void {
    if (!this.pdf() || this.numPages() <= 0) {
      return;
    }

    this.rangeError.set('');
    this.renderQueue = [];
    this.queuedPages.clear();

    if (this.strategy === STRATEGY_RANGES) {
      this.buildRangeGroups(this.numPages());
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
      title: `Page ${page}`,
      outputName: everyPageOutputName(page),
      pages: [{ page }],
      pageCount: 1,
    }));

    this.groups.set(groups);
    this.countLabel.set(formatCount(numPages, 'file'));
    this.totalPages.set(numPages);
    this.limited.set(visible < numPages);
    this.scheduleRender(pages);
  }

  private buildRangeGroups(numPages: number): void {
    const parsed = parseRanges(this.ranges, numPages);
    if (parsed.error) {
      this.groups.set([]);
      this.rangeError.set(parsed.error);
      this.countLabel.set('');
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
      const singlePage = range.start === range.end;
      return {
        key: `range-${part}-${range.start}-${range.end}`,
        title: `Part ${part} · ${
          singlePage ? `Page ${range.start}` : `Pages ${range.start}-${range.end}`
        }`,
        outputName: rangeOutputName(part, range),
        pages: visiblePages
          .filter((page) => page >= range.start && page <= range.end)
          .map((page) => ({ page })),
        pageCount: range.end - range.start + 1,
      };
    });

    this.groups.set(groups);
    this.countLabel.set(formatCount(parsed.ranges.length, 'part'));
    this.totalPages.set(allPages.length);
    this.limited.set(visiblePages.length < allPages.length);
    this.scheduleRender(visiblePages);
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

function rangeOutputName(part: number, range: ParsedRange): string {
  const suffix =
    range.start === range.end
      ? `p${range.start}`
      : `p${range.start}-${range.end}`;
  return `range_${padThree(part)}_${suffix}.pdf`;
}

function flattenRanges(ranges: ParsedRange[]): number[] {
  const pages: number[] = [];
  for (const range of ranges) {
    for (let page = range.start; page <= range.end; page += 1) {
      pages.push(page);
    }
  }
  return pages;
}

function parseRanges(value: string, numPages: number): RangeParseResult {
  const trimmed = (value ?? '').trim();
  if (!trimmed) {
    return {
      ranges: [],
      error: 'Enter page ranges to preview the resulting parts.',
    };
  }

  const tokens = trimmed.split(',');
  const ranges: ParsedRange[] = [];
  const seen = new Set<number>();

  for (const rawToken of tokens) {
    const token = rawToken.trim();
    const match = RANGE_TOKEN.exec(token);
    if (!match) {
      return {
        ranges: [],
        error: 'Use page numbers and ranges separated by commas, like 1-3,5,8-10.',
      };
    }

    const start = Number(match[1]);
    const end = Number(match[2] ?? match[1]);
    if (!start || !end || start > end) {
      return {
        ranges: [],
        error: 'Each range must start at a page lower than or equal to its end.',
      };
    }

    if (end > numPages) {
      return {
        ranges: [],
        error: `Page ${end} is outside this PDF (${numPages} pages).`,
      };
    }

    for (let page = start; page <= end; page += 1) {
      if (seen.has(page)) {
        return {
          ranges: [],
          error: `Page ${page} appears more than once. Remove duplicate or overlapping ranges.`,
        };
      }
      seen.add(page);
    }

    ranges.push({ start, end });
  }

  return { ranges, error: '' };
}
