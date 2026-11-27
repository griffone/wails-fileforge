import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  FolderOpen,
  Image as ImageIcon,
  LayoutGrid,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { JobRequest, Wails } from '../../services/wails';
import { fileExtension, fileName, formatCount } from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_IMAGES_TOOL_ID = 'tool.pdf.images';
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 320;
const IMAGE_DRAG_TYPE = 'application/x-fileforge-image';
const GAP_MM = 4;
const OPTIONS_SCHEMA_VERSION = 1;

const SUPPORTED_IMAGE_EXTENSIONS = new Set([
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'tiff',
  'tif',
]);

type PageSize = 'a4' | 'letter' | 'a5';
type Orientation = 'portrait' | 'landscape';
type PerPage = 1 | 2 | 4 | 6 | 9;
type Fit = 'contain' | 'cover';
type MoveDirection = 'left' | 'right' | 'up' | 'down';

interface Placement {
  page: number;
  cell: number;
}

interface ImagesGrid {
  columns: number;
  rows: number;
  cells: number;
}

interface PageDimensions {
  width: number;
  height: number;
}

interface SelectOption<T extends string | number> {
  value: T;
  label: string;
}

const PAGE_SIZES_MM: Record<PageSize, PageDimensions> = {
  a4: { width: 210, height: 297 },
  letter: { width: 215.9, height: 279.4 },
  a5: { width: 148, height: 210 },
};

const PAGE_SIZE_OPTIONS: readonly SelectOption<PageSize>[] = [
  { value: 'a4', label: 'A4' },
  { value: 'letter', label: 'Letter' },
  { value: 'a5', label: 'A5' },
];

const ORIENTATION_OPTIONS: readonly SelectOption<Orientation>[] = [
  { value: 'portrait', label: 'Portrait' },
  { value: 'landscape', label: 'Landscape' },
];

const FIT_OPTIONS: readonly SelectOption<Fit>[] = [
  { value: 'contain', label: 'Fit inside' },
  { value: 'cover', label: 'Fill cell' },
];

const MARGIN_OPTIONS: readonly SelectOption<number>[] = [
  { value: 0, label: 'None (0 mm)' },
  { value: 6, label: 'Small (6 mm)' },
  { value: 10, label: 'Normal (10 mm)' },
  { value: 16, label: 'Wide (16 mm)' },
];

const PER_PAGE_OPTIONS: readonly PerPage[] = [1, 2, 4, 6, 9];

type PdfFromImagesOptions = {
  pageSize: PageSize;
  orientation: Orientation;
  perPage: PerPage;
  fit: Fit;
  marginMm: number;
};

function defaultOptions(): PdfFromImagesOptions {
  return {
    pageSize: 'a4',
    orientation: 'portrait',
    perPage: 4,
    fit: 'contain',
    marginMm: 10,
  };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfFromImagesOptions> {
  const result: Partial<PdfFromImagesOptions> = {};

  const pageSize = stored['pageSize'];
  if (
    typeof pageSize === 'string' &&
    PAGE_SIZE_OPTIONS.some((option) => option.value === pageSize)
  ) {
    result.pageSize = pageSize as PageSize;
  }

  const orientation = stored['orientation'];
  if (
    typeof orientation === 'string' &&
    ORIENTATION_OPTIONS.some((option) => option.value === orientation)
  ) {
    result.orientation = orientation as Orientation;
  }

  const perPage = stored['perPage'];
  if (
    typeof perPage === 'number' &&
    PER_PAGE_OPTIONS.some((option) => option === perPage)
  ) {
    result.perPage = perPage as PerPage;
  }

  const fit = stored['fit'];
  if (typeof fit === 'string' && FIT_OPTIONS.some((option) => option.value === fit)) {
    result.fit = fit as Fit;
  }

  const margin = stored['marginMm'];
  if (
    typeof margin === 'number' &&
    MARGIN_OPTIONS.some((option) => option.value === margin)
  ) {
    result.marginMm = margin;
  }

  return result;
}

/** One grid shape per images-per-page count, rotated for landscape pages. */
function gridShape(perPage: PerPage, orientation: Orientation): ImagesGrid {
  switch (perPage) {
    case 1:
      return { columns: 1, rows: 1, cells: 1 };
    case 2:
      return orientation === 'landscape'
        ? { columns: 2, rows: 1, cells: 2 }
        : { columns: 1, rows: 2, cells: 2 };
    case 4:
      return { columns: 2, rows: 2, cells: 4 };
    case 6:
      return orientation === 'landscape'
        ? { columns: 3, rows: 2, cells: 6 }
        : { columns: 2, rows: 3, cells: 6 };
    case 9:
      return { columns: 3, rows: 3, cells: 9 };
  }
}

@Component({
  selector: 'app-pdf-from-images',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-from-images.html',
  styleUrl: './pdf-from-images.css',
})
export class PdfFromImages implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_IMAGES_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_IMAGES_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0,
    canRun: () => this.canRun(),
    loadThumbnails: false,
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: () => '',
    afterPathsChanged: (previous, paths) => {
      this.applyPlacements(previous, paths);
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
    },
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'pdfFromImages.error.start',
      planFailed: 'pdfFromImages.error.plan',
      waiting: 'pdfFromImages.status.waiting',
      inspectFailed: 'pdfFromImages.error.inspect',
      statusMissing: 'pdfFromImages.error.status',
      cancelFailed: 'pdfFromImages.error.cancel',
      openFileFailed: 'pdfFromImages.error.openFile',
      revealFailed: 'pdfFromImages.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly gridIcon = LayoutGrid;
  readonly imageIcon = ImageIcon;
  readonly leftIcon = ArrowLeft;
  readonly rightIcon = ArrowRight;
  readonly upIcon = ArrowUp;
  readonly downIcon = ArrowDown;

  readonly pageSizeOptions = PAGE_SIZE_OPTIONS;
  readonly orientationOptions = ORIENTATION_OPTIONS;
  readonly fitOptions = FIT_OPTIONS;
  readonly marginOptions = MARGIN_OPTIONS;
  readonly perPageOptions = PER_PAGE_OPTIONS;
  readonly gapMm = GAP_MM;
  readonly fileName = fileName;

  readonly selectedPaths = this.job.selectedPaths;
  /** Grid positions keyed by input path so order changes never detach them. */
  readonly placements = signal<Record<string, Placement>>({});
  readonly pageSize = signal<PageSize>('a4');
  readonly orientation = signal<Orientation>('portrait');
  readonly perPage = signal<PerPage>(4);
  readonly fit = signal<Fit>('contain');
  readonly marginMm = signal<number>(10);
  readonly thumbnails = signal<Record<string, string>>({});

  readonly outputDir = this.job.outputDir;

  readonly plan = this.job.plan;
  readonly planError = this.job.planError;
  readonly planLoading = this.job.planLoading;

  readonly jobResult = this.job.jobResult;
  readonly jobError = this.job.jobError;
  readonly outputInfos = this.job.outputInfos;

  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly activeJobId = this.job.activeJobId;

  readonly notice = this.job.notice;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;
  readonly dragOverSlot = signal<string | null>(null);
  readonly dragOverTray = signal(false);

  readonly grid = computed<ImagesGrid>(() =>
    gridShape(this.perPage(), this.orientation()),
  );

  readonly unplacedPaths = computed(() => {
    const placements = this.placements();
    return this.selectedPaths().filter((path) => !placements[path]);
  });

  readonly pages = computed(() => {
    const placements = Object.values(this.placements());
    let highest = -1;
    for (const placement of placements) {
      highest = Math.max(highest, placement.page);
    }

    let count = Math.max(1, highest + 1);
    // Unplaced images need somewhere to go: open a page when the grid is full.
    if (
      this.unplacedPaths().length > 0 &&
      placements.length >= count * this.grid().cells
    ) {
      count += 1;
    }
    return count;
  });

  readonly pageSlots = computed<(string | null)[][]>(() => {
    const pages = this.pages();
    const cells = this.grid().cells;
    const slots: (string | null)[][] = Array.from({ length: pages }, () =>
      Array.from({ length: cells }, () => null),
    );

    for (const path of this.selectedPaths()) {
      const placement = this.placements()[path];
      if (!placement) {
        continue;
      }
      const page = slots[placement.page];
      if (page && placement.cell >= 0 && placement.cell < cells) {
        page[placement.cell] = path;
      }
    }

    return slots;
  });

  readonly pageDimensions = computed<PageDimensions>(() => {
    const size = PAGE_SIZES_MM[this.pageSize()];
    return this.orientation() === 'landscape'
      ? { width: size.height, height: size.width }
      : size;
  });

  readonly pageAspect = computed(
    () => this.pageDimensions().width / this.pageDimensions().height,
  );

  readonly pagePaddingPercent = computed(
    () => (this.marginMm() / this.pageDimensions().width) * 100,
  );

  readonly pageGapPercent = computed(
    () => (this.gapMm / this.pageDimensions().width) * 100,
  );

  readonly pageRowGapPercent = computed(
    () => (this.gapMm / this.pageDimensions().height) * 100,
  );

  readonly pageSizeLabel = computed(() => {
    const option = PAGE_SIZE_OPTIONS.find(
      (candidate) => candidate.value === this.pageSize(),
    );
    return option
      ? this.pageSizeOptionLabel(option)
      : this.pageSize().toUpperCase();
  });

  readonly layoutSummary = computed(() =>
    this.i18n.t('pdfFromImages.layout.summary', {
      size: this.pageSizeLabel(),
      orientation: this.i18n.tOr(
        `pdfFromImages.orientation.${this.orientation()}Inline`,
        this.orientation(),
      ),
      columns: this.grid().columns,
      rows: this.grid().rows,
      margin: this.marginMm(),
    }),
  );

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfFromImages.destination.firstImage'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.unplacedPaths().length === 0 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() =>
    this.isSubmitting() || this.isPolling()
      ? this.i18n.t('pdfFromImages.action.creating')
      : this.i18n.t('pdfFromImages.action.create'),
  );

  readonly actionHelper = computed(() => {
    if (this.unplacedPaths().length > 0) {
      return this.i18n.t('pdfFromImages.helper.unplaced');
    }
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfFromImages.helper.addOne');
    }
    return '';
  });

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/pdf-from-images');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  pagesLabel(): string {
    return formatCount(
      this.pages(),
      this.i18n.t('common.pageOne'),
      this.i18n.t('common.pageMany'),
    );
  }

  filledCount(page: number): number {
    return (this.pageSlots()[page] ?? []).filter(Boolean).length;
  }

  slotKey(page: number, cell: number): string {
    return `${page}:${cell}`;
  }

  slotAriaLabel(path: string | null, page: number, cell: number): string {
    if (path) {
      return fileName(path);
    }
    return this.i18n.t('pdfFromImages.slot.empty', {
      page: page + 1,
      position: cell + 1,
    });
  }

  /** Option labels resolved at render time so the locale switch applies. */
  pageSizeOptionLabel(option: SelectOption<PageSize>): string {
    return this.i18n.tOr(
      `pdfFromImages.pageSize.${option.value}`,
      option.label,
    );
  }

  orientationOptionLabel(option: SelectOption<Orientation>): string {
    return this.i18n.tOr(
      `pdfFromImages.orientation.${option.value}`,
      option.label,
    );
  }

  fitOptionLabel(option: SelectOption<Fit>): string {
    return this.i18n.tOr(`pdfFromImages.fit.${option.value}`, option.label);
  }

  marginOptionLabel(option: SelectOption<number>): string {
    return this.i18n.tOr(
      `pdfFromImages.margin.${option.value}`,
      option.label,
    );
  }

  onPathsSelected(paths: string[]): void {
    this.job.onPathsSelected(paths);
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.job.onPathsSelected(paths);
    }
  }

  clear(): void {
    this.job.clear();
  }

  removeImage(path: string): void {
    this.job.removeFile(path);
  }

  unplace(path: string): void {
    if (!this.placements()[path]) {
      return;
    }
    this.placements.update((placements) => {
      const updated = { ...placements };
      delete updated[path];
      return updated;
    });
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  placeFromTray(path: string): void {
    if (!this.selectedPaths().includes(path) || this.placements()[path]) {
      return;
    }
    const target = this.firstEmptySlot();
    if (!target) {
      return;
    }
    this.moveImageToSlot(path, target.page, target.cell);
  }

  /** Moves, swaps or drops an image on a slot; a tray item fills the target. */
  moveImageToSlot(path: string, page: number, cell: number): void {
    if (!this.selectedPaths().includes(path)) {
      return;
    }
    if (page < 0 || page >= this.pages()) {
      return;
    }
    if (cell < 0 || cell >= this.grid().cells) {
      return;
    }

    const placements = this.placements();
    const source = placements[path];
    if (source && source.page === page && source.cell === cell) {
      return;
    }

    let occupant: string | null = null;
    for (const candidate of this.selectedPaths()) {
      const placement = placements[candidate];
      if (placement && placement.page === page && placement.cell === cell) {
        occupant = candidate;
        break;
      }
    }
    if (occupant === path) {
      return;
    }

    const next = { ...placements };
    if (!occupant) {
      next[path] = { page, cell };
    } else if (source) {
      next[path] = { page, cell };
      next[occupant] = { page: source.page, cell: source.cell };
    } else {
      next[path] = { page, cell };
      delete next[occupant];
    }

    this.placements.set(next);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  moveSlot(path: string, direction: MoveDirection): void {
    const target = this.moveTarget(path, direction);
    if (!target) {
      return;
    }
    this.moveImageToSlot(path, target.page, target.cell);
  }

  canMoveSlot(path: string, direction: MoveDirection): boolean {
    return this.moveTarget(path, direction) !== null;
  }

  selectPerPage(count: PerPage): void {
    if (count === this.perPage()) {
      return;
    }
    this.perPage.set(count);
    this.reflowForGridChange();
  }

  selectOrientation(orientation: Orientation): void {
    if (orientation === this.orientation()) {
      return;
    }
    this.orientation.set(orientation);
    this.reflowForGridChange();
  }

  selectFit(fit: Fit): void {
    if (fit === this.fit()) {
      return;
    }
    this.fit.set(fit);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onPageSizeChange(event: Event): void {
    const value = this.readValue(event) as PageSize;
    if (
      !PAGE_SIZE_OPTIONS.some((option) => option.value === value) ||
      value === this.pageSize()
    ) {
      return;
    }
    this.pageSize.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onMarginChange(event: Event): void {
    const value = Number.parseInt(this.readValue(event), 10);
    if (
      !MARGIN_OPTIONS.some((option) => option.value === value) ||
      value === this.marginMm()
    ) {
      return;
    }
    this.marginMm.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.job.refreshPlan();
  }

  useFirstImageFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onDragStart(event: DragEvent, path: string | null): void {
    if (!path) {
      return;
    }
    const transfer = event.dataTransfer;
    if (!transfer) {
      return;
    }
    const payload = JSON.stringify({ path });
    transfer.effectAllowed = 'move';
    transfer.setData(IMAGE_DRAG_TYPE, payload);
    transfer.setData('text/plain', payload);
    this.dragOverSlot.set(null);
    this.dragOverTray.set(false);
  }

  onDragEnd(): void {
    this.dragOverSlot.set(null);
    this.dragOverTray.set(false);
  }

  onSlotDragOver(event: DragEvent, page: number, cell: number): void {
    if (!this.hasImagePayload(event)) {
      return;
    }
    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move';
    }
    this.dragOverTray.set(false);
    this.dragOverSlot.set(this.slotKey(page, cell));
  }

  onSlotDragLeave(event: DragEvent, page: number, cell: number): void {
    const host = event.currentTarget as HTMLElement | null;
    const related = event.relatedTarget as Node | null;
    if (host && related && host.contains(related)) {
      return;
    }
    if (this.dragOverSlot() === this.slotKey(page, cell)) {
      this.dragOverSlot.set(null);
    }
  }

  onSlotDrop(event: DragEvent, page: number, cell: number): void {
    if (!this.hasImagePayload(event)) {
      return;
    }
    event.preventDefault();
    const path = this.readDragPath(event);
    this.onDragEnd();
    if (path) {
      this.moveImageToSlot(path, page, cell);
    }
  }

  onTrayDragOver(event: DragEvent): void {
    if (!this.hasImagePayload(event)) {
      return;
    }
    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move';
    }
    this.dragOverSlot.set(null);
    this.dragOverTray.set(true);
  }

  onTrayDragLeave(event: DragEvent): void {
    const host = event.currentTarget as HTMLElement | null;
    const related = event.relatedTarget as Node | null;
    if (host && related && host.contains(related)) {
      return;
    }
    this.dragOverTray.set(false);
  }

  onTrayDrop(event: DragEvent): void {
    if (!this.hasImagePayload(event)) {
      return;
    }
    event.preventDefault();
    const path = this.readDragPath(event);
    this.onDragEnd();
    if (path) {
      this.unplace(path);
    }
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
  }

  async cancel(): Promise<void> {
    await this.job.cancel();
  }

  async onOpen(path: string): Promise<void> {
    await this.job.openPath(path);
  }

  async onReveal(path: string): Promise<void> {
    await this.job.revealPath(path);
  }

  /** The tool always renders one PDF, so retry reruns every current input. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.job.clearRunOutcome();

    const planned = await this.job.refreshPlan();
    if (!planned) {
      return;
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.placements.set({});
    this.thumbnails.set({});
    this.dragOverSlot.set(null);
    this.dragOverTray.set(false);
  }

  private currentOptions(): PdfFromImagesOptions {
    return {
      pageSize: this.pageSize(),
      orientation: this.orientation(),
      perPage: this.perPage(),
      fit: this.fit(),
      marginMm: this.marginMm(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.pageSize.set(options.pageSize);
    this.orientation.set(options.orientation);
    this.perPage.set(options.perPage);
    this.fit.set(options.fit);
    this.marginMm.set(options.marginMm);
  }

  private buildRequest(): JobRequest {
    return {
      toolId: PDF_IMAGES_TOOL_ID,
      mode: 'single',
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options: {
        pageSize: this.pageSize(),
        orientation: this.orientation(),
        columns: this.grid().columns,
        rows: this.grid().rows,
        marginMm: this.marginMm(),
        gapMm: this.gapMm,
        fit: this.fit(),
        slots: this.buildSlots(),
      },
    };
  }

  /** Slots carry the input index so the backend never guesses an order. */
  private buildSlots(): Array<{ page: number; cell: number; inputIndex: number }> {
    const paths = this.selectedPaths();
    const placements = this.placements();
    const slots: Array<{ page: number; cell: number; inputIndex: number }> = [];

    for (let inputIndex = 0; inputIndex < paths.length; inputIndex += 1) {
      const placement = placements[paths[inputIndex]];
      if (!placement) {
        continue;
      }
      slots.push({
        page: placement.page,
        cell: placement.cell,
        inputIndex,
      });
    }

    return slots;
  }

  /**
   * Keeps grid positions in sync with the queue: removed images lose their
   * placement and newly added ones fill the empty cells in order.
   */
  private applyPlacements(
    previousPaths: readonly string[],
    paths: readonly string[],
  ): void {
    const previous = new Set(previousPaths);
    const current = new Set(paths);
    const added = paths.filter((path) => !previous.has(path));

    const existing = this.placements();
    const next: Record<string, Placement> = {};
    for (const [path, placement] of Object.entries(existing)) {
      if (current.has(path)) {
        next[path] = placement;
      }
    }

    this.placements.set(this.fillEmptyCells(next, [...added]));
    this.loadThumbnails(added);
  }

  private fillEmptyCells(
    placements: Record<string, Placement>,
    paths: readonly string[],
  ): Record<string, Placement> {
    if (paths.length === 0) {
      return placements;
    }

    const next = { ...placements };
    const cells = this.grid().cells;
    const occupied = new Set(
      Object.values(next).map((placement) =>
        this.slotKey(placement.page, placement.cell),
      ),
    );

    let page = 0;
    let cell = 0;
    for (const path of paths) {
      while (occupied.has(this.slotKey(page, cell))) {
        cell += 1;
        if (cell >= cells) {
          cell = 0;
          page += 1;
        }
      }
      next[path] = { page, cell };
      occupied.add(this.slotKey(page, cell));
      cell += 1;
      if (cell >= cells) {
        cell = 0;
        page += 1;
      }
    }

    return next;
  }

  private firstEmptySlot(): Placement | null {
    const occupied = new Set(
      Object.values(this.placements()).map((placement) =>
        this.slotKey(placement.page, placement.cell),
      ),
    );
    const cells = this.grid().cells;

    for (let page = 0; page < this.pages(); page += 1) {
      for (let cell = 0; cell < cells; cell += 1) {
        if (!occupied.has(this.slotKey(page, cell))) {
          return { page, cell };
        }
      }
    }

    return null;
  }

  private moveTarget(
    path: string,
    direction: MoveDirection,
  ): Placement | null {
    const placement = this.placements()[path];
    if (!placement) {
      return null;
    }

    const { columns, rows, cells } = this.grid();
    const { page, cell } = placement;
    const column = cell % columns;
    const row = Math.floor(cell / columns);

    switch (direction) {
      case 'left':
        if (column === 0) {
          return page > 0 ? { page: page - 1, cell: cells - 1 } : null;
        }
        return { page, cell: cell - 1 };
      case 'right':
        if (column === columns - 1) {
          return page < this.pages() - 1 ? { page: page + 1, cell: 0 } : null;
        }
        return { page, cell: cell + 1 };
      case 'up':
        return row > 0 ? { page, cell: cell - columns } : null;
      case 'down':
        return row < rows - 1 ? { page, cell: cell + columns } : null;
    }
  }

  /**
   * A grid change invalidates the cell numbers, so images are refilled in their
   * current page/cell order to keep the same visual sequence.
   */
  private reflowForGridChange(): void {
    this.actionMessage.set('');

    if (this.selectedPaths().length === 0) {
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
      return;
    }

    const placements = this.placements();
    const ordered = [...this.selectedPaths()].sort((left, right) => {
      const a = placements[left];
      const b = placements[right];
      if (!a && !b) {
        return 0;
      }
      if (!a) {
        return 1;
      }
      if (!b) {
        return -1;
      }
      return a.page - b.page || a.cell - b.cell;
    });

    const next: Record<string, Placement> = {};
    const cells = this.grid().cells;
    let page = 0;
    let cell = 0;
    for (const path of ordered) {
      next[path] = { page, cell };
      cell += 1;
      if (cell >= cells) {
        cell = 0;
        page += 1;
      }
    }

    this.placements.set(next);
    this.notice.set({
      tone: 'warning',
      text: this.i18n.t('pdfFromImages.notice.gridChanged'),
    });
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  private loadThumbnails(paths: readonly string[]): void {
    for (const path of paths) {
      if (this.thumbnails()[path]) {
        continue;
      }
      void this.wailsService
        .getThumbnail(path, THUMBNAIL_SIZE)
        .then((thumbnailUrl) => {
          if (!thumbnailUrl) {
            return;
          }
          this.thumbnails.update((cache) => ({
            ...cache,
            [path]: thumbnailUrl,
          }));
        })
        .catch(() => {
          // Thumbnails are optional; slots and the tray show the file name.
        });
    }
  }

  private hasImagePayload(event: DragEvent): boolean {
    const transfer = event.dataTransfer;
    return !!transfer && Array.from(transfer.types).includes(IMAGE_DRAG_TYPE);
  }

  private readDragPath(event: DragEvent): string | null {
    const transfer = event.dataTransfer;
    if (!transfer) {
      return null;
    }
    const raw =
      transfer.getData(IMAGE_DRAG_TYPE) || transfer.getData('text/plain');
    if (!raw) {
      return null;
    }
    try {
      const payload = JSON.parse(raw) as { path?: unknown };
      return typeof payload?.path === 'string' && payload.path.length > 0
        ? payload.path
        : null;
    } catch {
      return null;
    }
  }

  private rejectionNotice(rejected: readonly string[]): string {
    const extensions = [
      ...new Set(
        rejected.map(
          (path) => fileExtension(path).toUpperCase() || 'UNKNOWN',
        ),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('pdfFromImages.notice.filesOne')
        : this.i18n.t('pdfFromImages.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('pdfFromImages.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('pdfFromImages.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }
}
