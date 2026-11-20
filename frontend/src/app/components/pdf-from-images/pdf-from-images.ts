import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
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
import { ResultCard } from '../tool-flow/result-card';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  InspectFilesResponse,
  JobError,
  JobExecutionStatus,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  RunJobResponse,
  Wails,
} from '../../services/wails';
import { fileExtension, fileName, formatCount } from '../../utils/format';
import { resumeToolJob } from '../../utils/resume-task';

const PDF_IMAGES_TOOL_ID = 'tool.pdf.images';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 250;
const THUMBNAIL_SIZE = 320;
const IMAGE_DRAG_TYPE = 'application/x-fileforge-image';
const GAP_MM = 4;

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

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
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

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
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
    FileDrop,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-from-images.html',
  styleUrl: './pdf-from-images.css',
})
export class PdfFromImages implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

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

  readonly selectedPaths = signal<string[]>([]);
  /** Grid positions keyed by input path so order changes never detach them. */
  readonly placements = signal<Record<string, Placement>>({});
  readonly pageSize = signal<PageSize>('a4');
  readonly orientation = signal<Orientation>('portrait');
  readonly perPage = signal<PerPage>(4);
  readonly fit = signal<Fit>('contain');
  readonly marginMm = signal<number>(10);
  readonly thumbnails = signal<Record<string, string>>({});

  readonly outputDir = signal(this.preferences.defaultOutputDir());

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planError = signal<JobError | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');
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
    return option?.label ?? this.pageSize().toUpperCase();
  });

  readonly layoutSummary = computed(
    () =>
      `${this.pageSizeLabel()} ${this.orientation()} · ${this.grid().columns} × ${this.grid().rows} · ${this.marginMm()} mm`,
  );

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to the first image',
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
    this.isSubmitting() || this.isPolling() ? 'Creating…' : 'Create PDF',
  );

  readonly actionHelper = computed(() => {
    if (this.unplacedPaths().length > 0) {
      return 'Place all images or remove them.';
    }
    if (this.selectedPaths().length === 0) {
      return 'Add at least one image to create a PDF.';
    }
    return '';
  });

  readonly terminalJob = computed<JobResult | null>(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
  });

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? describeToolError(error, 'The PDF could not be created.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private planDirty = false;

  constructor(private readonly wailsService: Wails) {
    this.wailsService.jobProgress$
      .pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        if (!event || event.jobId !== this.activeJobId()) {
          return;
        }

        const current = this.jobResult();
        if (!current) {
          return;
        }

        this.jobResult.set({
          ...current,
          message: event.progress.message,
          progress: event.progress,
          // Wait for the status endpoint to settle the final items. The event
          // can arrive before the job store has the complete result.
          status: this.isTerminalStatus(event.status)
            ? current.status
            : (event.status as JobExecutionStatus),
        });

        if (this.isTerminalStatus(event.status)) {
          void this.pollJobStatus();
        }
      });
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/pdf-from-images');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'image');
  }

  pagesLabel(): string {
    return formatCount(this.pages(), 'page');
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
    return `Empty cell, page ${page + 1}, position ${cell + 1}`;
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const known = new Set(this.selectedPaths());
    const added: string[] = [];
    const rejected: string[] = [];

    for (const path of incoming) {
      if (!SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase())) {
        rejected.push(path);
        continue;
      }
      if (known.has(path)) {
        continue;
      }
      known.add(path);
      added.push(path);
    }

    this.notice.set(
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    );

    if (added.length === 0) {
      return;
    }

    this.actionMessage.set('');
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.placements.update((placements) =>
      this.fillEmptyCells(placements, added),
    );
    this.loadThumbnails(added);
    this.schedulePlanRefresh();
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.onPathsSelected(paths);
    }
  }

  clear(): void {
    if (this.selectedPaths().length === 0) {
      return;
    }
    this.selectedPaths.set([]);
    this.placements.set({});
    this.notice.set(null);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  removeImage(path: string): void {
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }
    this.selectedPaths.set(next);
    this.placements.update((placements) => {
      const updated = { ...placements };
      delete updated[path];
      return updated;
    });
    this.actionMessage.set('');
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
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
    this.schedulePlanRefresh();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.refreshPlan();
  }

  useFirstImageFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.schedulePlanRefresh();
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
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    if (this.planLoading() || this.planDirty || !this.plan()?.success) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponse = await this.wailsService
      .runJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The PDF could not be created.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The PDF could not be created.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The PDF could not be created.',
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    const total = this.selectedPaths().length;
    this.activeJobId.set(response.jobId);
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: 'Waiting to start',
      toolId: PDF_IMAGES_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: 'Waiting to start',
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isSubmitting.set(false);
    this.isPolling.set(true);
    this.startPolling();
  }

  async cancel(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }
    const response = await this.wailsService.cancelJob(jobId);
    if (!response.success) {
      this.actionMessage.set(
        response.message || 'The job could not be cancelled.',
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success ? '' : response.message || 'The file could not be opened.',
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || 'The folder could not be opened.',
    );
  }

  /** The tool always renders one PDF, so retry reruns every current input. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');

    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    await this.run();
  }

  reset(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.planDirty = false;

    this.selectedPaths.set([]);
    this.placements.set({});
    this.thumbnails.set({});
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
    this.dragOverSlot.set(null);
    this.dragOverTray.set(false);
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

  private fillEmptyCells(
    placements: Record<string, Placement>,
    paths: string[],
  ): Record<string, Placement> {
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
      this.schedulePlanRefresh();
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
      text: 'The grid changed; the images were refilled.',
    });
    this.schedulePlanRefresh();
  }

  private async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (this.selectedPaths().length === 0) {
      this.planToken += 1;
      this.planDirty = false;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponse = await this.wailsService
      .planJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The output could not be planned.',
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: 'The output could not be planned.',
        },
      }));
    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);
    this.planDirty = false;

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobError = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || 'The output could not be planned.',
    };
    // Keep the failed plan so the shared plan card shows the error in context.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private schedulePlanRefresh(): void {
    this.planDirty = true;
    // Invalidate an in-flight plan: it was built from the previous layout.
    this.planToken += 1;
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  private resumeBackgroundJob(): void {
    resumeToolJob({
      activity: this.activity,
      toolId: PDF_IMAGES_TOOL_ID,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startPolling: () => this.startPolling(),
    });
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling.set(true);
    void this.pollJobStatus();
    this.pollingTimer = setInterval(() => {
      void this.pollJobStatus();
    }, POLLING_INTERVAL_MS);
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isPolling.set(false);
  }

  private async pollJobStatus(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      this.stopPolling();
      return;
    }

    const response: JobStatusResponse = await this.wailsService
      .getJobStatus(jobId)
      .catch(() => ({
        success: false,
        message: 'The job status could not be read.',
        found: false,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'JOB_STATUS_FAILED',
          message: 'The job status could not be read.',
        },
      }));
    if (this.activeJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.stopPolling();
      this.activeJobId.set('');
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'JOB_STATUS_MISSING',
          message: response.message || 'The job status could not be read.',
        },
      );
      return;
    }

    this.jobResult.set(response.result);
    if (this.isTerminalStatus(response.result.status)) {
      this.stopPolling();
      this.activeJobId.set('');
      void this.loadOutputInfos();
    }
  }

  private async loadOutputInfos(): Promise<void> {
    const paths = this.collectOutputPaths();
    if (paths.length === 0) {
      this.outputInfos.set({});
      return;
    }

    const response: InspectFilesResponse = await this.wailsService
      .inspectFiles(paths)
      .catch(() => ({
        success: false,
        message: 'File sizes could not be read.',
        files: [],
      }));
    if (!response.success) {
      this.outputInfos.set({});
      return;
    }

    const infos: Record<string, FileInfo> = {};
    for (const info of response.files) {
      if (info.path) {
        infos[info.path] = info;
      }
    }
    this.outputInfos.set(infos);
  }

  private collectOutputPaths(): string[] {
    const paths = new Set<string>();
    for (const item of this.jobResult()?.items ?? []) {
      if (!item.success) {
        continue;
      }
      const outputs =
        item.outputs && item.outputs.length > 0
          ? item.outputs
          : item.outputPath
            ? [item.outputPath]
            : [];
      for (const output of outputs) {
        if (output) {
          paths.add(output);
        }
      }
    }
    return [...paths];
  }

  private loadThumbnails(paths: string[]): void {
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

  private rejectionNotice(rejected: string[]): string {
    const extensions = [
      ...new Set(
        rejected.map(
          (path) => fileExtension(path).toUpperCase() || 'UNKNOWN',
        ),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1 ? "1 file wasn't added." : `${count} files weren't added.`;

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = `${extensions[0]} isn't supported.`;
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = `${head} and ${extensions[extensions.length - 1]} aren't supported.`;
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
