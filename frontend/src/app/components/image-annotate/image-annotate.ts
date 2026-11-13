import {
  Component,
  ElementRef,
  HostListener,
  OnDestroy,
  ViewChild,
  computed,
  signal,
} from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import {
  ArrowDown,
  ArrowUp,
  CopyPlus,
  FolderOpen,
  Layers,
  LucideAngularModule,
  PenTool,
  Redo2,
  ShieldCheck,
  Trash2,
  TriangleAlert,
  Undo2,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import {
  FileInfoV1,
  ImageAnnotateOperationTypeV1,
  ImageAnnotateOperationV1,
  ImageAnnotatePreviewRequestV1,
  ImageAnnotatePreviewResponseV1,
  ImagePreviewSourceResponseV1,
  InspectFilesResponseV1,
  JobErrorV1,
  JobExecutionStatusV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  RunJobResponseV1,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';

const IMAGE_ANNOTATE_TOOL_ID = 'tool.image.annotate';
const POLLING_INTERVAL_MS = 1000;
const OPERATION_PLAN_DEBOUNCE_MS = 250;
const ANNOTATE_PREVIEW_DEBOUNCE_MS = 200;
const THUMBNAIL_SIZE = 160;
const HISTORY_LIMIT = 50;
const DUPLICATE_OFFSET_PX = 10;

const OPERATION_TYPES: readonly ImageAnnotateOperationTypeV1[] = [
  'text',
  'arrow',
  'rect',
  'blur',
  'redact',
];

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

type AnnotateTool = 'select' | 'rect' | 'blur' | 'redact' | 'arrow' | 'text';

type OperationFieldKey =
  | 'x'
  | 'y'
  | 'width'
  | 'height'
  | 'x2'
  | 'y2'
  | 'text'
  | 'color'
  | 'opacity'
  | 'strokeWidth'
  | 'fontSize'
  | 'blurIntensity';

interface OperationFormValue {
  type: ImageAnnotateOperationTypeV1;
  x: string;
  y: string;
  width: string;
  height: string;
  x2: string;
  y2: string;
  text: string;
  color: string;
  opacity: string;
  strokeWidth: string;
  fontSize: string;
  blurIntensity: string;
}

const DEFAULT_OPERATION_FORM: OperationFormValue = {
  type: 'rect',
  x: '0',
  y: '0',
  width: '40',
  height: '24',
  x2: '60',
  y2: '32',
  text: '',
  color: '#ff0000',
  opacity: '1',
  strokeWidth: '2',
  fontSize: '18',
  blurIntensity: '40',
};

interface FormatOption {
  value: string;
  label: string;
}

const FORMAT_OPTIONS: readonly FormatOption[] = [
  { value: '', label: 'Preserve original' },
  { value: 'png', label: 'PNG' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' },
  { value: 'gif', label: 'GIF' },
  { value: 'tiff', label: 'TIFF' },
];

interface OverlayPoint {
  x: number;
  y: number;
}

interface OverlayRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface DrawDraft {
  type: 'rect' | 'blur' | 'redact' | 'arrow';
  startX: number;
  startY: number;
  endX: number;
  endY: number;
}

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

interface CommitOptions {
  /** Operation to select after the change; -1 clears the selection. */
  selection?: number;
}

@Component({
  selector: 'app-image-annotate',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-annotate.html',
  styleUrl: './image-annotate.css',
})
export class ImageAnnotate implements OnDestroy {
  private readonly destroy$ = new Subject<void>();
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly arrowUpIcon = ArrowUp;
  readonly arrowDownIcon = ArrowDown;
  readonly trashIcon = Trash2;
  readonly penIcon = PenTool;
  readonly layersIcon = Layers;
  readonly undoIcon = Undo2;
  readonly redoIcon = Redo2;
  readonly duplicateIcon = CopyPlus;

  readonly operationTypes = OPERATION_TYPES;
  readonly formatOptions = FORMAT_OPTIONS;
  readonly toolOptions: readonly { id: AnnotateTool; label: string }[] = [
    { id: 'select', label: 'Select' },
    { id: 'text', label: 'Text' },
    { id: 'arrow', label: 'Arrow' },
    { id: 'rect', label: 'Rect' },
    { id: 'blur', label: 'Blur' },
    { id: 'redact', label: 'Redact' },
  ];

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly previewUrl = signal('');
  readonly imageWidth = signal(0);
  readonly imageHeight = signal(0);

  readonly operations = signal<ImageAnnotateOperationV1[]>([]);
  readonly selectedOperationIndex = signal(-1);
  readonly activeTool = signal<AnnotateTool>('rect');
  readonly operationForm = signal<OperationFormValue>({
    ...DEFAULT_OPERATION_FORM,
  });
  readonly drawDraft = signal<DrawDraft | null>(null);
  /** Form fields are a draft until the user applies them to the operation. */
  readonly formDirty = signal(false);
  readonly canUndo = signal(false);
  readonly canRedo = signal(false);

  readonly outputFormat = signal('');
  readonly outputDir = signal('');
  readonly advancedOpen = signal(false);

  readonly plan = signal<PlanJobResponseV1 | null>(null);
  readonly planError = signal<JobErrorV1 | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResultV1 | null>(null);
  readonly jobError = signal<JobErrorV1 | null>(null);
  readonly outputInfos = signal<Record<string, FileInfoV1>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly previewLoading = signal(false);
  readonly previewError = signal('');
  readonly annotatePreviewUrl = signal('');
  readonly isAnnotatePreviewLoading = signal(false);

  readonly hasSelection = computed(() => this.selectedPaths().length > 0);
  readonly hasPreview = computed(
    () =>
      this.previewUrl().length > 0 &&
      this.imageWidth() > 0 &&
      this.imageHeight() > 0,
  );
  readonly hasOperations = computed(() => this.operations().length > 0);
  readonly isBatch = computed(() => this.selectedPaths().length > 1);

  readonly drawingCursor = computed(
    () => this.hasPreview() && this.activeTool() !== 'select',
  );

  readonly canDraw = computed(
    () =>
      this.previewUrl().length > 0 &&
      this.imageWidth() > 0 &&
      this.imageHeight() > 0,
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Annotating…';
    }
    const count = this.selectedPaths().length;
    return count > 1 ? `Annotate ${count} images` : 'Annotate image';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each image',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.operations().length > 0 &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (!this.hasSelection()) {
      return 'Add at least one image to annotate.';
    }
    if (!this.hasOperations()) {
      return 'Draw on the image or add an annotation to run.';
    }
    return '';
  });

  readonly operationFormTitle = computed(() => {
    const index = this.selectedOperationIndex();
    return index >= 0 ? `Edit annotation ${index + 1}` : 'New annotation';
  });

  readonly terminalJob = computed<JobResultV1 | null>(() => {
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
      ? describeToolError(error, 'The annotation could not start.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private annotatePreviewTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private annotatePreviewToken = 0;
  private fileItemsGeneration = 0;
  private previewGeneration = 0;
  /**
   * Snapshots are deep copies so later mutations of the live operations
   * cannot rewrite history.
   */
  private readonly undoStack: ImageAnnotateOperationV1[][] = [];
  private readonly redoStack: ImageAnnotateOperationV1[][] = [];

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
            : (event.status as JobExecutionStatusV1),
        });

        if (this.isTerminalStatus(event.status)) {
          void this.pollJobStatus();
        }
      });
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.clearAnnotatePreviewTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'image');
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const previousFirst = this.selectedPaths()[0] ?? '';
    const rejected: string[] = [];
    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(
      this.selectedPaths().map((path) => this.pathKey(path)),
    );

    for (const path of incoming) {
      if (
        !SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase())
      ) {
        rejected.push(path);
        continue;
      }

      const key = this.pathKey(path);
      if (known.has(key)) {
        duplicates.push(path);
        continue;
      }

      known.add(key);
      added.push(path);
    }

    this.notice.set(this.intakeNotice(rejected, duplicates));

    if (added.length === 0) {
      return;
    }

    this.actionMessage.set('');
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.syncFileItems();

    const firstChanged = previousFirst !== this.selectedPaths()[0];
    if (firstChanged) {
      void this.reloadPreviewAndPlan();
    } else {
      void this.refreshPlan();
    }
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
    this.notice.set(null);
    this.actionMessage.set('');
    this.syncFileItems();
    void this.reloadPreviewAndPlan();
  }

  removeFile(path: string): void {
    const previousFirst = this.selectedPaths()[0] ?? '';
    const next = this.selectedPaths().filter(
      (candidate) => candidate !== path,
    );
    if (next.length === this.selectedPaths().length) {
      return;
    }

    this.selectedPaths.set(next);
    this.syncFileItems();

    if ((next[0] ?? '') === previousFirst) {
      void this.refreshPlan();
    } else {
      void this.reloadPreviewAndPlan();
    }
  }

  setActiveTool(tool: AnnotateTool): void {
    this.activeTool.set(tool);
  }

  isActiveTool(tool: AnnotateTool): boolean {
    return this.activeTool() === tool;
  }

  isOperationSelected(index: number): boolean {
    return this.selectedOperationIndex() === index;
  }

  selectOperation(index: number): void {
    const operations = this.operations();
    if (index < 0 || index >= operations.length) {
      this.selectedOperationIndex.set(-1);
      this.formDirty.set(false);
      return;
    }

    this.formDirty.set(false);
    this.selectedOperationIndex.set(index);
    this.patchOperationForm(operations[index]);
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (event.defaultPrevented || event.isComposing) {
      return;
    }
    // Never hijack editing keys while the user is typing in a field.
    if (this.isEditableTarget(event)) {
      return;
    }

    const modifier = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();

    if (modifier && key === 'z') {
      event.preventDefault();
      if (event.shiftKey) {
        this.redo();
      } else {
        this.undo();
      }
      return;
    }

    if (modifier && key === 'y') {
      event.preventDefault();
      this.redo();
      return;
    }

    if (modifier && key === 'd') {
      if (this.selectedOperationIndex() < 0) {
        return;
      }
      event.preventDefault();
      this.duplicateOperation();
      return;
    }

    if (!modifier && (event.key === 'Delete' || event.key === 'Backspace')) {
      if (this.selectedOperationIndex() < 0) {
        return;
      }
      event.preventDefault();
      this.removeSelectedOperation();
    }
  }

  onOperationShapeClick(index: number, event: MouseEvent): void {
    event.stopPropagation();
    this.selectOperation(index);
    this.setActiveTool('select');
  }

  onOperationField(key: OperationFieldKey, event: Event): void {
    const value = this.readEventValue(event);
    this.operationForm.update((form) => ({ ...form, [key]: value }));
    if (this.selectedOperationIndex() >= 0) {
      this.formDirty.set(true);
    }
  }

  onOperationTypeChange(event: Event): void {
    const value = this.readEventValue(event) as ImageAnnotateOperationTypeV1;
    if (!OPERATION_TYPES.includes(value)) {
      return;
    }

    this.operationForm.update((form) => ({ ...form, type: value }));
    if (this.selectedOperationIndex() >= 0) {
      this.formDirty.set(true);
      this.applyOperationEdit();
    }
  }

  /** Applies the form draft to the selected operation as one history step. */
  applyOperationEdit(): void {
    const index = this.selectedOperationIndex();
    const operations = this.operations();
    if (index < 0 || index >= operations.length || !this.formDirty()) {
      return;
    }

    const next = [...operations];
    next[index] = this.readOperationFromForm();
    this.commitOperations(next, { selection: index });
  }

  addOperation(): void {
    const operation = this.readOperationFromForm();
    if (operation.type === 'text' && !operation.text?.trim()) {
      this.actionMessage.set('Enter the text for the text annotation.');
      return;
    }

    const operations = this.operations();
    this.commitOperations([...operations, operation], {
      selection: operations.length,
    });
  }

  removeOperation(index: number): void {
    const operations = this.operations();
    if (index < 0 || index >= operations.length) {
      return;
    }

    const selected = this.selectedOperationIndex();
    let selection = selected;
    if (selected === index) {
      selection = -1;
    } else if (selected > index) {
      selection = selected - 1;
    }

    this.commitOperations(
      operations.filter((_, i) => i !== index),
      { selection },
    );
  }

  removeSelectedOperation(): void {
    const index = this.selectedOperationIndex();
    if (index < 0) {
      return;
    }
    this.removeOperation(index);
  }

  /** Copies the selected operation with a small offset and selects the copy. */
  duplicateOperation(index: number = this.selectedOperationIndex()): void {
    const operations = this.operations();
    if (index < 0 || index >= operations.length) {
      return;
    }

    const copy = this.cloneOperations([operations[index]])[0];
    copy.x = (copy.x ?? 0) + DUPLICATE_OFFSET_PX;
    copy.y = (copy.y ?? 0) + DUPLICATE_OFFSET_PX;
    if (typeof copy.x2 === 'number') {
      copy.x2 += DUPLICATE_OFFSET_PX;
    }
    if (typeof copy.y2 === 'number') {
      copy.y2 += DUPLICATE_OFFSET_PX;
    }

    const next = [
      ...operations.slice(0, index + 1),
      copy,
      ...operations.slice(index + 1),
    ];
    this.commitOperations(next, { selection: index + 1 });
  }

  moveOperationUp(index: number): void {
    const operations = this.operations();
    if (index <= 0 || index >= operations.length) {
      return;
    }

    const next = [...operations];
    [next[index - 1], next[index]] = [next[index], next[index - 1]];

    const selected = this.selectedOperationIndex();
    let selection = selected;
    if (selected === index) {
      selection = index - 1;
    } else if (selected === index - 1) {
      selection = index;
    }

    this.commitOperations(next, { selection });
  }

  moveOperationDown(index: number): void {
    const operations = this.operations();
    if (index < 0 || index >= operations.length - 1) {
      return;
    }

    const next = [...operations];
    [next[index], next[index + 1]] = [next[index + 1], next[index]];

    const selected = this.selectedOperationIndex();
    let selection = selected;
    if (selected === index) {
      selection = index + 1;
    } else if (selected === index + 1) {
      selection = index;
    }

    this.commitOperations(next, { selection });
  }

  clearOperations(): void {
    if (!this.hasOperations()) {
      return;
    }

    this.drawDraft.set(null);
    this.commitOperations([], { selection: -1 });
  }

  /**
   * Single path for every annotation edit: snapshots the previous operations,
   * discards redo, keeps the selection valid and refreshes the plan preview.
   */
  commitOperations(
    next: ImageAnnotateOperationV1[],
    options: CommitOptions = {},
  ): void {
    this.pushHistory(this.undoStack, this.operations());
    this.redoStack.length = 0;
    this.operations.set(next);

    const requested = options.selection ?? this.selectedOperationIndex();
    let selection = -1;
    if (next.length > 0 && requested >= 0) {
      selection = Math.min(requested, next.length - 1);
    }
    this.selectedOperationIndex.set(selection);
    if (selection >= 0) {
      this.patchOperationForm(next[selection]);
    }
    this.formDirty.set(false);
    this.actionMessage.set('');
    this.refreshHistoryFlags();
    this.scheduleOperationRefresh();
  }

  undo(): void {
    const previous = this.undoStack.pop();
    if (!previous) {
      return;
    }

    this.pushHistory(this.redoStack, this.operations());
    this.operations.set(this.cloneOperations(previous));
    this.restoreHistorySelection();
    this.formDirty.set(false);
    this.actionMessage.set('');
    this.refreshHistoryFlags();
    this.scheduleOperationRefresh();
  }

  redo(): void {
    const next = this.redoStack.pop();
    if (!next) {
      return;
    }

    this.pushHistory(this.undoStack, this.operations());
    this.operations.set(this.cloneOperations(next));
    this.restoreHistorySelection();
    this.formDirty.set(false);
    this.actionMessage.set('');
    this.refreshHistoryFlags();
    this.scheduleOperationRefresh();
  }

  onFormatChange(event: Event): void {
    const value = this.readEventValue(event);
    if (value === this.outputFormat()) {
      return;
    }

    this.outputFormat.set(value);
    this.actionMessage.set('');
    this.scheduleAnnotatePreviewRefresh();
    void this.refreshPlan();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
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

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }

    this.outputDir.set('');
    void this.refreshPlan();
  }

  onPreviewImageLoaded(): void {
    // The overlay reads the rendered image size on demand; no cached geometry
    // needs to be refreshed here.
  }

  onOverlayPointerDown(event: PointerEvent): void {
    if (!this.canDraw()) {
      return;
    }

    const point = this.pointerToImage(event);
    if (!point) {
      return;
    }

    if (this.activeTool() === 'select') {
      return;
    }

    if (this.activeTool() === 'text') {
      this.createTextOperation(point.x, point.y);
      return;
    }

    this.drawDraft.set({
      type: this.activeTool() as DrawDraft['type'],
      startX: point.x,
      startY: point.y,
      endX: point.x,
      endY: point.y,
    });

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onOverlayPointerMove(event: PointerEvent): void {
    const draft = this.drawDraft();
    if (!draft) {
      return;
    }

    const point = this.pointerToImage(event);
    if (!point) {
      return;
    }

    this.drawDraft.set({
      ...draft,
      endX: point.x,
      endY: point.y,
    });
  }

  onOverlayPointerUp(): void {
    const draft = this.drawDraft();
    if (!draft) {
      return;
    }

    const nextOperation = this.operationFromDraft(draft);
    this.drawDraft.set(null);
    if (!nextOperation) {
      return;
    }

    const operations = this.operations();
    this.commitOperations([...operations, nextOperation], {
      selection: operations.length,
    });
  }

  async run(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for destinations and collision-safe
    // names, so it is refreshed with the current annotations before starting.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponseV1 = await this.wailsService
      .runJobV1(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The annotation could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The annotation could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message || 'The annotation could not start.',
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
      toolId: IMAGE_ANNOTATE_TOOL_ID,
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

    const response = await this.wailsService.cancelJobV1(jobId);
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

  /** Retries only the failed inputs with the same annotations and folder. */
  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    const previousFirst = this.selectedPaths()[0] ?? '';
    this.selectedPaths.set([...new Set(failed)]);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.syncFileItems();

    if ((this.selectedPaths()[0] ?? '') !== previousFirst) {
      await this.refreshPreviewSource();
    }

    await this.run();
  }

  reset(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.clearAnnotatePreviewTimer();
    this.planToken += 1;
    this.annotatePreviewToken += 1;
    this.fileItemsGeneration += 1;
    this.previewGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.previewUrl.set('');
    this.imageWidth.set(0);
    this.imageHeight.set(0);
    this.previewLoading.set(false);
    this.previewError.set('');
    // Reset clears the page, so history restarts with it.
    this.operations.set([]);
    this.clearHistory();
    this.selectedOperationIndex.set(-1);
    this.operationForm.set({ ...DEFAULT_OPERATION_FORM });
    this.formDirty.set(false);
    this.drawDraft.set(null);
    this.annotatePreviewUrl.set('');
    this.isAnnotatePreviewLoading.set(false);
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
  }

  operationTypeLabel(operation: ImageAnnotateOperationV1): string {
    switch (operation.type) {
      case 'text':
        return 'Text';
      case 'arrow':
        return 'Arrow';
      case 'rect':
        return 'Rectangle';
      case 'blur':
        return 'Blur';
      case 'redact':
        return 'Redact';
      default:
        return operation.type;
    }
  }

  operationDetail(operation: ImageAnnotateOperationV1): string {
    switch (operation.type) {
      case 'text': {
        const text = (operation.text ?? '').split('\n')[0].trim();
        const preview = text.length > 32 ? `${text.slice(0, 32)}…` : text;
        return `“${preview || 'Text'}” at ${operation.x ?? 0}, ${operation.y ?? 0}`;
      }
      case 'arrow':
        return `${operation.x ?? 0}, ${operation.y ?? 0} → ${operation.x2 ?? 0}, ${operation.y2 ?? 0}`;
      case 'rect':
        return `${operation.width ?? 0} × ${operation.height ?? 0} at ${operation.x ?? 0}, ${operation.y ?? 0}`;
      case 'blur':
        return `${operation.width ?? 0} × ${operation.height ?? 0} · intensity ${operation.blurIntensity ?? 0}`;
      case 'redact':
        return `${operation.width ?? 0} × ${operation.height ?? 0} at ${operation.x ?? 0}, ${operation.y ?? 0}`;
      default:
        return '';
    }
  }

  displayRectForOperation(
    operation: ImageAnnotateOperationV1,
  ): OverlayRect | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      x: (operation.x ?? 0) * scale.x,
      y: (operation.y ?? 0) * scale.y,
      width: Math.max(1, (operation.width ?? 1) * scale.x),
      height: Math.max(1, (operation.height ?? 1) * scale.y),
    };
  }

  displayArrowForOperation(
    operation: ImageAnnotateOperationV1,
  ): { from: OverlayPoint; to: OverlayPoint } | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      from: {
        x: (operation.x ?? 0) * scale.x,
        y: (operation.y ?? 0) * scale.y,
      },
      to: {
        x: (operation.x2 ?? 0) * scale.x,
        y: (operation.y2 ?? 0) * scale.y,
      },
    };
  }

  displayTextPointForOperation(
    operation: ImageAnnotateOperationV1,
  ): OverlayPoint | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      x: (operation.x ?? 0) * scale.x,
      y: (operation.y ?? 0) * scale.y,
    };
  }

  displayDraftRect(): OverlayRect | null {
    const draft = this.drawDraft();
    if (!draft || draft.type === 'arrow') {
      return null;
    }

    const normalized = this.normalizeRect(
      draft.startX,
      draft.startY,
      draft.endX,
      draft.endY,
    );
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      x: normalized.x * scale.x,
      y: normalized.y * scale.y,
      width: Math.max(1, normalized.width * scale.x),
      height: Math.max(1, normalized.height * scale.y),
    };
  }

  displayDraftArrow(): { from: OverlayPoint; to: OverlayPoint } | null {
    const draft = this.drawDraft();
    if (!draft || draft.type !== 'arrow') {
      return null;
    }

    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      from: {
        x: draft.startX * scale.x,
        y: draft.startY * scale.y,
      },
      to: {
        x: draft.endX * scale.x,
        y: draft.endY * scale.y,
      },
    };
  }

  private buildRequest(): JobRequestV1 {
    const paths = this.selectedPaths();
    const format = this.outputFormat().trim();

    const options: Record<string, unknown> = {
      operations: this.operations(),
    };
    if (format) {
      options['format'] = format;
    }

    return {
      toolId: IMAGE_ANNOTATE_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length === 0 || !this.hasOperations()) {
      this.clearPlanState();
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponseV1 = await this.wailsService
      .planJobV1(this.buildRequest())
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

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobErrorV1 = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || 'The output could not be planned.',
    };
    // Keep the failed plan so the shared plan card can show the destination
    // or annotation problem next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private clearPlanState(): void {
    this.planToken += 1;
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.clearPlanRefreshTimer();
  }

  private scheduleOperationRefresh(): void {
    if (!this.hasOperations()) {
      this.clearPlanState();
    } else {
      this.schedulePlanRefresh();
    }
    this.scheduleAnnotatePreviewRefresh();
  }

  private pushHistory(
    stack: ImageAnnotateOperationV1[][],
    operations: readonly ImageAnnotateOperationV1[],
  ): void {
    stack.push(this.cloneOperations(operations));
    if (stack.length > HISTORY_LIMIT) {
      stack.splice(0, stack.length - HISTORY_LIMIT);
    }
  }

  private cloneOperations(
    operations: readonly ImageAnnotateOperationV1[],
  ): ImageAnnotateOperationV1[] {
    // Operations are plain data; a JSON round-trip keeps history independent
    // from the live objects and their optional fields.
    return JSON.parse(
      JSON.stringify(operations),
    ) as ImageAnnotateOperationV1[];
  }

  private restoreHistorySelection(): void {
    const total = this.operations().length;
    const selection =
      total === 0
        ? -1
        : Math.min(
            Math.max(this.selectedOperationIndex(), 0),
            total - 1,
          );
    this.selectedOperationIndex.set(selection);
    if (selection >= 0) {
      this.patchOperationForm(this.operations()[selection]);
    }
  }

  private refreshHistoryFlags(): void {
    this.canUndo.set(this.undoStack.length > 0);
    this.canRedo.set(this.redoStack.length > 0);
  }

  private clearHistory(): void {
    this.undoStack.length = 0;
    this.redoStack.length = 0;
    this.refreshHistoryFlags();
  }

  private isEditableTarget(event: KeyboardEvent): boolean {
    const target = event.target as HTMLElement | null;
    if (!target || typeof target.closest !== 'function') {
      return false;
    }
    if (target.isContentEditable) {
      return true;
    }
    return !!target.closest(
      'input, select, textarea, [contenteditable="true"], [contenteditable=""]',
    );
  }

  private schedulePlanRefresh(): void {
    this.clearPlanRefreshTimer();
    if (this.selectedPaths().length === 0) {
      this.clearPlanState();
      return;
    }

    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, OPERATION_PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  private async reloadPreviewAndPlan(): Promise<void> {
    await this.refreshPreviewSource();
    await this.refreshPlan();
  }

  private async refreshPreviewSource(): Promise<void> {
    const path = this.previewSourcePath();
    const generation = (this.previewGeneration += 1);

    this.clearAnnotatePreviewTimer();
    this.annotatePreviewToken += 1;
    this.annotatePreviewUrl.set('');
    this.isAnnotatePreviewLoading.set(false);
    this.previewError.set('');
    this.actionMessage.set('');

    if (!path) {
      this.previewLoading.set(false);
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      return;
    }

    this.previewLoading.set(true);

    const response: ImagePreviewSourceResponseV1 = await this.wailsService
      .getImagePreviewSourceV1(path)
      .catch(() => ({
        success: false,
        message: 'The image preview could not be loaded.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_PREVIEW_READ_FAILED',
          message: 'The image preview could not be loaded.',
        },
      }));
    if (generation !== this.previewGeneration) {
      return;
    }

    this.previewLoading.set(false);

    if (!response.success || !response.dataBase64) {
      this.previewUrl.set('');
      this.imageWidth.set(0);
      this.imageHeight.set(0);
      this.previewError.set(
        describeToolError(
          response.error,
          'The image preview could not be loaded.',
        ).description,
      );
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.imageWidth.set(response.width ?? 0);
    this.imageHeight.set(response.height ?? 0);
    this.previewUrl.set(`data:${mimeType};base64,${response.dataBase64}`);

    this.scheduleAnnotatePreviewRefresh();
  }

  private scheduleAnnotatePreviewRefresh(): void {
    this.clearAnnotatePreviewTimer();
    // A pending preview belongs to the previous annotations; discard it so it
    // can never replace the preview of the current ones.
    this.annotatePreviewToken += 1;
    this.isAnnotatePreviewLoading.set(false);
    if (!this.previewUrl() || !this.hasOperations()) {
      this.annotatePreviewUrl.set('');
      return;
    }

    this.annotatePreviewTimer = setTimeout(() => {
      this.annotatePreviewTimer = null;
      void this.refreshAnnotatePreview();
    }, ANNOTATE_PREVIEW_DEBOUNCE_MS);
  }

  private clearAnnotatePreviewTimer(): void {
    if (this.annotatePreviewTimer) {
      clearTimeout(this.annotatePreviewTimer);
      this.annotatePreviewTimer = null;
    }
  }

  private async refreshAnnotatePreview(): Promise<void> {
    const path = this.previewSourcePath();
    if (!path || !this.hasOperations()) {
      this.annotatePreviewUrl.set('');
      return;
    }

    const token = (this.annotatePreviewToken += 1);
    this.isAnnotatePreviewLoading.set(true);

    const request: ImageAnnotatePreviewRequestV1 = {
      inputPath: path,
      operations: this.operations(),
      format: this.outputFormat(),
    };

    const response: ImageAnnotatePreviewResponseV1 =
      await this.wailsService.getImageAnnotatePreviewV1(request).catch(() => ({
        success: false,
        message: 'The annotated preview could not be generated.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_ANNOTATE_PREVIEW_FAILED',
          message: 'The annotated preview could not be generated.',
        },
      }));
    if (token !== this.annotatePreviewToken) {
      return;
    }

    this.isAnnotatePreviewLoading.set(false);

    if (!response.success || !response.dataBase64) {
      this.annotatePreviewUrl.set('');
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.annotatePreviewUrl.set(
      `data:${mimeType};base64,${response.dataBase64}`,
    );
  }

  private previewSourcePath(): string {
    return this.selectedPaths()[0]?.trim() ?? '';
  }

  private readInt(raw: string, fallback: number): number {
    const value = Number.parseInt(raw.trim(), 10);
    return Number.isFinite(value) ? value : fallback;
  }

  private readFloat(raw: string, fallback: number): number {
    const value = Number.parseFloat(raw.trim());
    return Number.isFinite(value) ? value : fallback;
  }

  private readOperationFromForm(): ImageAnnotateOperationV1 {
    const form = this.operationForm();
    return {
      type: form.type,
      x: this.readInt(form.x, 0),
      y: this.readInt(form.y, 0),
      width: this.readInt(form.width, 1),
      height: this.readInt(form.height, 1),
      x2: this.readInt(form.x2, 0),
      y2: this.readInt(form.y2, 0),
      text: form.text,
      color: form.color,
      opacity: this.readFloat(form.opacity, 1),
      strokeWidth: this.readInt(form.strokeWidth, 2),
      fontSize: this.readInt(form.fontSize, 18),
      blurIntensity: this.readInt(form.blurIntensity, 40),
    };
  }

  private patchOperationForm(operation: ImageAnnotateOperationV1): void {
    this.operationForm.set({
      type: operation.type,
      x: `${operation.x ?? 0}`,
      y: `${operation.y ?? 0}`,
      width: `${operation.width ?? 1}`,
      height: `${operation.height ?? 1}`,
      x2: `${operation.x2 ?? 0}`,
      y2: `${operation.y2 ?? 0}`,
      text: operation.text ?? '',
      color: operation.color ?? '#ff0000',
      opacity: `${operation.opacity ?? 1}`,
      strokeWidth: `${operation.strokeWidth ?? 2}`,
      fontSize: `${operation.fontSize ?? 18}`,
      blurIntensity: `${operation.blurIntensity ?? 40}`,
    });
  }

  private operationFromDraft(
    draft: DrawDraft,
  ): ImageAnnotateOperationV1 | null {
    const form = this.operationForm();

    if (draft.type === 'arrow') {
      return {
        type: 'arrow',
        x: draft.startX,
        y: draft.startY,
        x2: draft.endX,
        y2: draft.endY,
        width: this.readInt(form.width, 1),
        height: this.readInt(form.height, 1),
        color: form.color,
        opacity: this.readFloat(form.opacity, 1),
        strokeWidth: this.readInt(form.strokeWidth, 2),
        fontSize: this.readInt(form.fontSize, 18),
        blurIntensity: this.readInt(form.blurIntensity, 40),
        text: form.text,
      };
    }

    const rect = this.normalizeRect(
      draft.startX,
      draft.startY,
      draft.endX,
      draft.endY,
    );
    if (rect.width < 1 || rect.height < 1) {
      return null;
    }

    return {
      type: draft.type,
      x: rect.x,
      y: rect.y,
      width: rect.width,
      height: rect.height,
      x2: rect.x + rect.width,
      y2: rect.y + rect.height,
      text: form.text,
      color: form.color,
      opacity: this.readFloat(form.opacity, 1),
      strokeWidth: this.readInt(form.strokeWidth, 2),
      fontSize: this.readInt(form.fontSize, 18),
      blurIntensity: this.readInt(form.blurIntensity, 40),
    };
  }

  private createTextOperation(x: number, y: number): void {
    const form = this.operationForm();
    let text = form.text.trim();

    if (!text && typeof window !== 'undefined') {
      text = window.prompt('Enter annotation text', '')?.trim() ?? '';
    }

    if (!text) {
      return;
    }

    const operation: ImageAnnotateOperationV1 = {
      type: 'text',
      x,
      y,
      text,
      color: form.color,
      opacity: this.readFloat(form.opacity, 1),
      fontSize: this.readInt(form.fontSize, 18),
      strokeWidth: this.readInt(form.strokeWidth, 2),
      width: this.readInt(form.width, 1),
      height: this.readInt(form.height, 1),
      x2: this.readInt(form.x2, x),
      y2: this.readInt(form.y2, y),
      blurIntensity: this.readInt(form.blurIntensity, 40),
    };

    const operations = this.operations();
    this.commitOperations([...operations, operation], {
      selection: operations.length,
    });
  }

  private normalizeRect(
    x1: number,
    y1: number,
    x2: number,
    y2: number,
  ): OverlayRect {
    const minX = Math.max(0, Math.min(x1, x2));
    const minY = Math.max(0, Math.min(y1, y2));
    const maxX = Math.min(this.imageWidth(), Math.max(x1, x2));
    const maxY = Math.min(this.imageHeight(), Math.max(y1, y2));

    return {
      x: Math.round(minX),
      y: Math.round(minY),
      width: Math.max(1, Math.round(maxX - minX)),
      height: Math.max(1, Math.round(maxY - minY)),
    };
  }

  private pointerToImage(event: PointerEvent): OverlayPoint | null {
    const rect = this.previewImageElementRect();
    if (!rect || this.imageWidth() <= 0 || this.imageHeight() <= 0) {
      return null;
    }

    const localX = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
    const localY = Math.min(Math.max(event.clientY - rect.top, 0), rect.height);
    const scaleX = this.imageWidth() / rect.width;
    const scaleY = this.imageHeight() / rect.height;

    return {
      x: Math.round(localX * scaleX),
      y: Math.round(localY * scaleY),
    };
  }

  private imageToDisplayScale(): { x: number; y: number } | null {
    const rect = this.previewImageElementRect();
    if (!rect || this.imageWidth() <= 0 || this.imageHeight() <= 0) {
      return null;
    }

    return {
      x: rect.width / this.imageWidth(),
      y: rect.height / this.imageHeight(),
    };
  }

  private previewImageElementRect(): DOMRect | null {
    const element = this.previewImage?.nativeElement;
    if (!element) {
      return null;
    }

    const rect = element.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) {
      return null;
    }

    return rect;
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

    const response: JobStatusResponseV1 = await this.wailsService
      .getJobStatusV1(jobId)
      .catch(() => ({
        success: false,
        message: 'The job status could not be read.',
        found: false,
        error: {
          code: 'EXEC_IO_TRANSIENT' as const,
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

    const response: InspectFilesResponseV1 = await this.wailsService
      .inspectFilesV1(paths)
      .catch(() => ({
        success: false,
        message: 'File sizes could not be read.',
        files: [],
      }));
    if (!response.success) {
      this.outputInfos.set({});
      return;
    }

    const infos: Record<string, FileInfoV1> = {};
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

      const outputs = (item.outputs ?? []).filter((path) => !!path);
      if (outputs.length > 0) {
        for (const output of outputs) {
          paths.add(output);
        }
      } else if (item.outputPath) {
        paths.add(item.outputPath);
      }
    }
    return [...paths];
  }

  private syncFileItems(): void {
    const paths = this.selectedPaths();
    const generation = (this.fileItemsGeneration += 1);
    const existing = new Map(
      this.fileItems().map((item) => [item.path, item]),
    );
    this.fileItems.set(
      paths.map(
        (path) => existing.get(path) ?? { path, name: fileName(path) },
      ),
    );

    if (paths.length > 0) {
      void this.enrichFileItems(paths, generation);
    }
  }

  private async enrichFileItems(
    paths: string[],
    generation: number,
  ): Promise<void> {
    try {
      const response = await this.wailsService.inspectFilesV1(paths);
      if (generation === this.fileItemsGeneration && response.success) {
        const infos = new Map<string, FileInfoV1>();
        for (const info of response.files) {
          infos.set(info.path, info);
        }

        this.fileItems.update((items) =>
          items.map((item) => {
            const info = infos.get(item.path);
            const meta = info ? this.describeFileInfo(info) : '';
            return meta ? { ...item, meta } : item;
          }),
        );
      }
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }

    if (generation !== this.fileItemsGeneration) {
      return;
    }

    for (const path of paths) {
      this.loadThumbnail(path, generation);
    }
  }

  private loadThumbnail(path: string, generation: number): void {
    void this.wailsService
      .getThumbnail(path, THUMBNAIL_SIZE)
      .then((thumbnailUrl) => {
        if (!thumbnailUrl || generation !== this.fileItemsGeneration) {
          return;
        }
        this.fileItems.update((items) =>
          items.map((item) =>
            item.path === path ? { ...item, thumbnailUrl } : item,
          ),
        );
      })
      .catch(() => {
        // Thumbnails are optional; keep the placeholder icon.
      });
  }

  private describeFileInfo(info: FileInfoV1): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
    }
    return parts.join(' · ');
  }

  private intakeNotice(
    rejected: string[],
    duplicates: string[],
  ): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        `${fileName(rejected[0])} wasn't added because it isn't an image.`,
      );
    } else if (rejected.length > 1) {
      parts.push(
        `${rejected.length} files weren't added because they aren't images.`,
      );
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(`${duplicates.length} images are already in the queue.`);
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  /**
   * Windows and UNC paths are case-insensitive, so the same file dropped with
   * different casing is still a duplicate there.
   */
  private pathKey(path: string): string {
    const normalized = path.trim().replace(/\\/g, '/');
    return /^[a-zA-Z]:\//.test(normalized) || normalized.startsWith('//')
      ? normalized.toLowerCase()
      : normalized;
  }

  private readEventValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
