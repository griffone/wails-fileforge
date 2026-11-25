import {
  Component,
  ElementRef,
  HostListener,
  OnDestroy,
  OnInit,
  ViewChild,
  computed,
  inject,
  signal,
} from '@angular/core';
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
import {
  describeToolError,
  localizeToolError,
} from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  ImageAnnotateOperationType,
  ImageAnnotateOperation,
  ImageAnnotatePreviewRequest,
  ImageAnnotatePreviewResponse,
  ImagePreviewSourceResponse,
  JobRequest,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { ToolRunController } from '../../utils/tool-run-controller';

const IMAGE_ANNOTATE_TOOL_ID = 'tool.image.annotate';
const OPERATION_PLAN_DEBOUNCE_MS = 250;
const ANNOTATE_PREVIEW_DEBOUNCE_MS = 200;
const HISTORY_LIMIT = 50;
const DUPLICATE_OFFSET_PX = 10;

const OPERATION_TYPES: readonly ImageAnnotateOperationType[] = [
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
  type: ImageAnnotateOperationType;
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
export class ImageAnnotate implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  private readonly job: ToolRunController = new ToolRunController({
    toolId: IMAGE_ANNOTATE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () =>
      this.selectedPaths().length > 0 && this.hasOperations(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.plan()?.success === false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (previousPaths, paths) =>
      this.onQueueChanged(previousPaths, paths),
    messages: {
      start: 'imageAnnotate.error.start',
      planFailed: 'imageAnnotate.error.plan',
      waiting: 'imageAnnotate.status.waiting',
      inspectFailed: 'imageAnnotate.error.inspect',
      statusMissing: 'imageAnnotate.error.status',
      cancelFailed: 'imageAnnotate.error.cancel',
      openFileFailed: 'imageAnnotate.error.openFile',
      revealFailed: 'imageAnnotate.error.openFolder',
    },
    notice: {
      rejectedOne: 'imageAnnotate.notice.rejectedOne',
      rejectedMany: 'imageAnnotate.notice.rejectedMany',
      duplicateOne: 'imageAnnotate.notice.duplicateOne',
      duplicateMany: 'imageAnnotate.notice.duplicateMany',
      rejectedManyFormat: 'count',
    },
  });

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

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
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

  readonly previewUrl = signal('');
  readonly imageWidth = signal(0);
  readonly imageHeight = signal(0);

  readonly operations = signal<ImageAnnotateOperation[]>([]);
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
  readonly advancedOpen = signal(false);

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
      return this.i18n.t('imageAnnotate.action.annotating');
    }
    const count = this.selectedPaths().length;
    return count > 1
      ? this.i18n.t('imageAnnotate.action.annotateMany', { count })
      : this.i18n.t('imageAnnotate.action.annotate');
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageAnnotate.destination.original'),
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
      return this.i18n.t('imageAnnotate.helper.addImage');
    }
    if (!this.hasOperations()) {
      return this.i18n.t('imageAnnotate.helper.addOperation');
    }
    return '';
  });

  readonly operationFormTitle = computed(() => {
    const index = this.selectedOperationIndex();
    return index >= 0
      ? this.i18n.t('imageAnnotate.operations.editTitle', {
          index: index + 1,
        })
      : this.i18n.t('imageAnnotate.operations.newTitle');
  });

  private annotatePreviewTimer: ReturnType<typeof setTimeout> | null = null;
  private annotatePreviewToken = 0;
  private previewGeneration = 0;
  /**
   * Snapshots are deep copies so later mutations of the live operations
   * cannot rewrite history.
   */
  private readonly undoStack: ImageAnnotateOperation[][] = [];
  private readonly redoStack: ImageAnnotateOperation[][] = [];

  ngOnInit(): void {
    this.job.takePendingDrop('/image-annotate');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
    this.clearAnnotatePreviewTimer();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  /** Tool palette copy resolved at render time so locale switches apply. */
  toolLabel(tool: { id: AnnotateTool; label: string }): string {
    return this.i18n.tOr(`imageAnnotate.tool.${tool.id}`, tool.label);
  }

  /** Type select keeps the raw id in English; Spanish gets a readable noun. */
  operationTypeOptionLabel(type: ImageAnnotateOperationType): string {
    return this.i18n.tOr(`imageAnnotate.type.${type}`, type);
  }

  formatOptionLabel(option: FormatOption): string {
    const key = option.value || 'preserve';
    return this.i18n.tOr(`imageAnnotate.format.${key}.label`, option.label);
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

  removeFile(path: string): void {
    this.job.removeFile(path);
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
    const value = this.readEventValue(event) as ImageAnnotateOperationType;
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
      this.actionMessage.set(this.i18n.t('imageAnnotate.error.textRequired'));
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
    next: ImageAnnotateOperation[],
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
    void this.job.refreshPlan();
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
    await this.job.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }

    this.outputDir.set('');
    void this.job.refreshPlan();
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
    await this.job.run();
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
    this.job.replacePaths([...new Set(failed)], { notify: false });
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');

    if ((this.selectedPaths()[0] ?? '') !== previousFirst) {
      await this.refreshPreviewSource();
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();

    this.previewGeneration += 1;
    this.annotatePreviewToken += 1;
    this.clearAnnotatePreviewTimer();

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
  }

  operationTypeLabel(operation: ImageAnnotateOperation): string {
    switch (operation.type) {
      case 'text':
        return this.i18n.t('imageAnnotate.operationType.text');
      case 'arrow':
        return this.i18n.t('imageAnnotate.operationType.arrow');
      case 'rect':
        return this.i18n.t('imageAnnotate.operationType.rect');
      case 'blur':
        return this.i18n.t('imageAnnotate.operationType.blur');
      case 'redact':
        return this.i18n.t('imageAnnotate.operationType.redact');
      default:
        return operation.type;
    }
  }

  operationDetail(operation: ImageAnnotateOperation): string {
    switch (operation.type) {
      case 'text': {
        const text = (operation.text ?? '').split('\n')[0].trim();
        const preview = text.length > 32 ? `${text.slice(0, 32)}…` : text;
        return this.i18n.t('imageAnnotate.detail.text', {
          preview:
            preview || this.i18n.t('imageAnnotate.detail.textFallback'),
          x: operation.x ?? 0,
          y: operation.y ?? 0,
        });
      }
      case 'arrow':
        return `${operation.x ?? 0}, ${operation.y ?? 0} → ${operation.x2 ?? 0}, ${operation.y2 ?? 0}`;
      case 'rect':
        return this.i18n.t('imageAnnotate.detail.rect', {
          width: operation.width ?? 0,
          height: operation.height ?? 0,
          x: operation.x ?? 0,
          y: operation.y ?? 0,
        });
      case 'blur':
        return this.i18n.t('imageAnnotate.detail.blur', {
          width: operation.width ?? 0,
          height: operation.height ?? 0,
          intensity: operation.blurIntensity ?? 0,
        });
      case 'redact':
        return this.i18n.t('imageAnnotate.detail.rect', {
          width: operation.width ?? 0,
          height: operation.height ?? 0,
          x: operation.x ?? 0,
          y: operation.y ?? 0,
        });
      default:
        return '';
    }
  }

  displayRectForOperation(
    operation: ImageAnnotateOperation,
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
    operation: ImageAnnotateOperation,
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
    operation: ImageAnnotateOperation,
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

  private buildRequest(): JobRequest {
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

  private onQueueChanged(
    previousPaths: readonly string[],
    paths: readonly string[],
  ): void {
    if ((previousPaths[0] ?? '') !== (paths[0] ?? '')) {
      void this.reloadPreviewAndPlan();
    } else {
      void this.job.refreshPlan();
    }
  }

  private scheduleOperationRefresh(): void {
    if (!this.hasOperations()) {
      this.job.clearPlan();
    } else {
      this.job.schedulePlanRefresh(OPERATION_PLAN_DEBOUNCE_MS);
    }
    this.scheduleAnnotatePreviewRefresh();
  }

  private pushHistory(
    stack: ImageAnnotateOperation[][],
    operations: readonly ImageAnnotateOperation[],
  ): void {
    stack.push(this.cloneOperations(operations));
    if (stack.length > HISTORY_LIMIT) {
      stack.splice(0, stack.length - HISTORY_LIMIT);
    }
  }

  private cloneOperations(
    operations: readonly ImageAnnotateOperation[],
  ): ImageAnnotateOperation[] {
    // Operations are plain data; a JSON round-trip keeps history independent
    // from the live objects and their optional fields.
    return JSON.parse(
      JSON.stringify(operations),
    ) as ImageAnnotateOperation[];
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

  private async reloadPreviewAndPlan(): Promise<void> {
    await this.refreshPreviewSource();
    await this.job.refreshPlan();
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

    const response: ImagePreviewSourceResponse = await this.wailsService
      .getImagePreviewSource(path)
      .catch(() => ({
        success: false,
        message: this.i18n.t('imageAnnotate.error.preview'),
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_PREVIEW_READ_FAILED',
          message: this.i18n.t('imageAnnotate.error.preview'),
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
        localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            response.error,
            this.i18n.t('imageAnnotate.error.preview'),
          ),
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

    const request: ImageAnnotatePreviewRequest = {
      inputPath: path,
      operations: this.operations(),
      format: this.outputFormat(),
    };

    const response: ImageAnnotatePreviewResponse =
      await this.wailsService.getImageAnnotatePreview(request).catch(() => ({
        success: false,
        message: this.i18n.t('imageAnnotate.error.annotatePreview'),
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'IMAGE_ANNOTATE_PREVIEW_FAILED',
          message: this.i18n.t('imageAnnotate.error.annotatePreview'),
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

  private readOperationFromForm(): ImageAnnotateOperation {
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

  private patchOperationForm(operation: ImageAnnotateOperation): void {
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
  ): ImageAnnotateOperation | null {
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
      text =
        window
          .prompt(this.i18n.t('imageAnnotate.error.promptText'), '')
          ?.trim() ?? '';
    }

    if (!text) {
      return;
    }

    const operation: ImageAnnotateOperation = {
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

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
    }
    return parts.join(' · ');
  }

  private readEventValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }
}
