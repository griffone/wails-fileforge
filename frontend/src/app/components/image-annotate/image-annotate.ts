import { CommonModule } from '@angular/common';
import { Component, ElementRef, OnDestroy, ViewChild } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';

import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
import {
  ImageAnnotateOperationV1,
  ImageAnnotateOperationTypeV1,
  ImageAnnotatePreviewRequestV1,
  ImageAnnotatePreviewResponseV1,
  ImagePreviewSourceResponseV1,
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const IMAGE_ANNOTATE_TOOL_ID = 'tool.image.annotate';
const POLLING_INTERVAL_MS = 1000;

type JobMode = 'single' | 'batch';
type AnnotateTool = 'select' | 'rect' | 'blur' | 'redact' | 'arrow' | 'text';

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

@Component({
  selector: 'app-image-annotate',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './image-annotate.html',
  styleUrl: './image-annotate.css',
})
export class ImageAnnotate implements OnDestroy {
  private readonly destroy$ = new Subject<void>();
  @ViewChild('singleFileInput') singleFileInput?: ElementRef<HTMLInputElement>;
  @ViewChild('batchFileInput') batchFileInput?: ElementRef<HTMLInputElement>;
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  readonly form;
  readonly operationForm;

  readonly panelFields: ExecutionPanelField[] = [
    {
      controlName: 'jobMode',
      label: 'Mode',
      type: 'select',
      options: [
        { value: 'single', label: 'single' },
        { value: 'batch', label: 'batch' },
      ],
      helpText: 'Batch v1 applies the same operations to all selected files.',
    },
    {
      controlName: 'outputPath',
      label: 'Output path (optional)',
      type: 'text',
      placeholder: '/path/to/image_annotated.png',
      visibleModes: ['single'],
      helpText: 'If empty, output is auto-generated as *_annotated.',
    },
    {
      controlName: 'outputDir',
      label: 'Output directory (optional)',
      type: 'text',
      placeholder: '/path/to/output',
      visibleModes: ['batch'],
      helpText: 'If empty, each item is written next to its input file.',
    },
    {
      controlName: 'format',
      label: 'Output format',
      type: 'select',
      options: [
        { value: '', label: 'preserve original' },
        { value: 'jpeg', label: 'jpeg' },
        { value: 'png', label: 'png' },
        { value: 'webp', label: 'webp' },
        { value: 'gif', label: 'gif' },
        { value: 'tiff', label: 'tiff' },
      ],
    },
  ];

  selectedInputPath = '';
  selectedInputPaths: string[] = [];
  operations: ImageAnnotateOperationV1[] = [];
  selectedOperationIndex = -1;

  sourcePreviewDataUrl = '';
  annotatePreviewDataUrl = '';
  sourceImageWidth = 0;
  sourceImageHeight = 0;
  previewMessage = 'Select an image and add operations to preview.';
  isPreviewLoading = false;

  activeTool: AnnotateTool = 'rect';
  drawDraft: DrawDraft | null = null;

  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private previewDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  private suppressAdvancedSync = false;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: ['single' as JobMode, Validators.required],
      outputPath: [''],
      outputDir: [''],
      format: [''],
      redactConfirmed: [false],
    });

    this.operationForm = this.fb.nonNullable.group({
      type: ['rect' as ImageAnnotateOperationTypeV1, Validators.required],
      x: ['0', Validators.required],
      y: ['0', Validators.required],
      width: ['40'],
      height: ['24'],
      x2: ['60'],
      y2: ['32'],
      text: [''],
      color: ['#ff0000'],
      opacity: ['1'],
      strokeWidth: ['2'],
      fontSize: ['18'],
      blurIntensity: ['40'],
    });

    this.form.controls.format.valueChanges.pipe(takeUntil(this.destroy$)).subscribe(() => this.scheduleAnnotatePreviewRefresh());
    this.form.controls.jobMode.valueChanges.pipe(takeUntil(this.destroy$)).subscribe(() => {
      this.onModeChanged();
    });
    this.operationForm.valueChanges.pipe(takeUntil(this.destroy$)).subscribe(() => this.onAdvancedFormChanged());
  }

  ngOnDestroy(): void {
    this.stopPolling();
    if (this.previewDebounceTimer) {
      clearTimeout(this.previewDebounceTimer);
      this.previewDebounceTimer = null;
    }
    this.destroy$.next();
    this.destroy$.complete();
  }

  setActiveTool(tool: AnnotateTool): void {
    this.activeTool = tool;
  }

  isActiveTool(tool: AnnotateTool): boolean {
    return this.activeTool === tool;
  }

  sourceInteractionCursor(): string {
    if (!this.sourcePreviewDataUrl || this.currentMode() !== 'single') {
      return 'default';
    }

    if (this.activeTool === 'select') {
      return 'default';
    }

    return 'crosshair';
  }

  async selectImageFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = (await this.wailsService.openFileDialog()).trim();
    if (!selected) {
      return;
    }

    if (this.currentMode() === 'batch') {
      this.addBatchInput(selected);
      this.previewMessage = 'Batch mode: preview uses first selected image.';
      await this.refreshPreviewSource();
      return;
    }

    this.selectedInputPath = selected;
    this.selectedInputPaths = [];
    await this.refreshPreviewSource();
  }

  async selectMultipleImagesFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPath = selected[0].trim();
      await this.refreshPreviewSource();
      return;
    }

    for (const path of selected) {
      this.addBatchInput(path);
    }

    this.previewMessage = 'Batch mode: preview uses first selected image.';
    await this.refreshPreviewSource();
  }

  triggerSingleFileInput(): void {
    this.singleFileInput?.nativeElement.click();
  }

  triggerBatchFileInput(): void {
    this.batchFileInput?.nativeElement.click();
  }

  onSingleFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const file = target?.files?.item(0);
    if (!file) {
      return;
    }

    const path = ((file as File & { path?: string }).path ?? file.name).trim();
    this.selectedInputPath = path;
    if (target) {
      target.value = '';
    }

    void this.refreshPreviewSource();
  }

  onBatchFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const files = target?.files;
    if (!files || files.length === 0) {
      return;
    }

    for (let i = 0; i < files.length; i++) {
      const file = files.item(i);
      if (!file) {
        continue;
      }
      const path = ((file as File & { path?: string }).path ?? file.name).trim();
      this.addBatchInput(path);
    }

    if (target) {
      target.value = '';
    }

    this.previewMessage = 'Batch mode: preview uses first selected image.';
    void this.refreshPreviewSource();
  }

  removeBatchInput(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.selectedInputPaths = this.selectedInputPaths.filter((_, i) => i !== index);
    void this.refreshPreviewSource();
  }

  clearInputs(): void {
    this.selectedInputPath = '';
    this.selectedInputPaths = [];
    this.sourcePreviewDataUrl = '';
    this.annotatePreviewDataUrl = '';
    this.sourceImageWidth = 0;
    this.sourceImageHeight = 0;
    this.drawDraft = null;
    this.previewMessage = 'Select an image and add operations to preview.';
  }

  onPreviewImageLoaded(): void {
    // Hook used by overlay sizing lifecycle. Intentionally no-op.
  }

  onOverlayPointerDown(event: PointerEvent): void {
    if (!this.canInteractOnOverlay()) {
      return;
    }

    const point = this.pointerToImage(event);
    if (!point) {
      return;
    }

    if (this.activeTool === 'select') {
      return;
    }

    if (this.activeTool === 'text') {
      this.createTextOperation(point.x, point.y);
      return;
    }

    this.drawDraft = {
      type: this.activeTool,
      startX: point.x,
      startY: point.y,
      endX: point.x,
      endY: point.y,
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onOverlayPointerMove(event: PointerEvent): void {
    if (!this.drawDraft) {
      return;
    }

    const point = this.pointerToImage(event);
    if (!point) {
      return;
    }

    this.drawDraft = {
      ...this.drawDraft,
      endX: point.x,
      endY: point.y,
    };
  }

  onOverlayPointerUp(): void {
    if (!this.drawDraft) {
      return;
    }

    const nextOperation = this.operationFromDraft(this.drawDraft);
    this.drawDraft = null;
    if (!nextOperation) {
      return;
    }

    this.operations = [...this.operations, nextOperation];
    this.selectOperation(this.operations.length - 1);

    if (nextOperation.type === 'redact') {
      this.form.patchValue({ redactConfirmed: false });
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  selectOperation(index: number): void {
    if (index < 0 || index >= this.operations.length) {
      this.selectedOperationIndex = -1;
      return;
    }

    this.selectedOperationIndex = index;
    this.patchAdvancedForm(this.operations[index]);
  }

  onOperationShapeClick(index: number, event: MouseEvent): void {
    event.stopPropagation();
    this.selectOperation(index);
    this.setActiveTool('select');
  }

  isOperationSelected(index: number): boolean {
    return this.selectedOperationIndex === index;
  }

  addOperation(): void {
    const op = this.readOperationFromForm();
    this.operations = [...this.operations, op];
    this.selectOperation(this.operations.length - 1);

    if (op.type === 'redact') {
      this.form.patchValue({ redactConfirmed: false });
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  removeOperation(index: number): void {
    if (index < 0 || index >= this.operations.length) {
      return;
    }
    this.operations = this.operations.filter((_, i) => i !== index);

    if (this.selectedOperationIndex === index) {
      this.selectedOperationIndex = -1;
    } else if (this.selectedOperationIndex > index) {
      this.selectedOperationIndex -= 1;
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  moveOperationUp(index: number): void {
    if (index <= 0 || index >= this.operations.length) {
      return;
    }
    const next = [...this.operations];
    [next[index - 1], next[index]] = [next[index], next[index - 1]];
    this.operations = next;

    if (this.selectedOperationIndex === index) {
      this.selectedOperationIndex = index - 1;
    } else if (this.selectedOperationIndex === index - 1) {
      this.selectedOperationIndex = index;
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  moveOperationDown(index: number): void {
    if (index < 0 || index >= this.operations.length - 1) {
      return;
    }
    const next = [...this.operations];
    [next[index], next[index + 1]] = [next[index + 1], next[index]];
    this.operations = next;

    if (this.selectedOperationIndex === index) {
      this.selectedOperationIndex = index + 1;
    } else if (this.selectedOperationIndex === index + 1) {
      this.selectedOperationIndex = index;
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  clearOperations(): void {
    this.operations = [];
    this.drawDraft = null;
    this.selectedOperationIndex = -1;
    this.form.patchValue({ redactConfirmed: false });
    this.scheduleAnnotatePreviewRefresh();
  }

  onModeChanged(): void {
    this.clearMessages();
    if (this.currentMode() === 'batch') {
      this.selectedInputPath = '';
    } else {
      this.selectedInputPaths = [];
    }
    void this.refreshPreviewSource();
  }

  hasRedactOperations(): boolean {
    return this.operations.some((op) => op.type === 'redact');
  }

  async validate(): Promise<void> {
    this.clearMessages();
    const localError = this.localValidationError();
    if (localError) {
      this.validationMessage = localError;
      return;
    }

    const response = await this.wailsService.validateJobV1(this.buildRequest());
    this.validationMessage = response.valid
      ? 'Validation OK. Ready to run annotate job.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();
    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
    }

    if (this.hasRedactOperations()) {
      if (!this.form.controls.redactConfirmed.value) {
        this.submitMessage = 'Before running, confirm irreversible redact operations.';
        return;
      }

      const approved =
        typeof window === 'undefined'
          ? true
          : window.confirm(
              'Redact is irreversible in output files. Confirm execution?'
            );
      if (!approved) {
        this.submitMessage = 'Execution canceled. Redact confirmation was not accepted.';
        return;
      }
    }

    const request = this.buildRequest();
    this.isSubmitting = true;
    this.jobResult = null;

    const validation = await this.wailsService.validateJobV1(request);
    if (!validation.valid) {
      this.submitMessage = this.mapJobError(validation.error, validation.message);
      this.isSubmitting = false;
      return;
    }

    const runResponse = await this.wailsService.runJobV1(request);
    if (!runResponse.success || !runResponse.jobId) {
      this.submitMessage = this.mapJobError(runResponse.error, runResponse.message);
      this.isSubmitting = false;
      return;
    }

    this.activeJobId = runResponse.jobId;
    this.submitMessage = `Job submitted: ${runResponse.jobId}`;
    this.startPolling();
    this.isSubmitting = false;
  }

  async cancel(): Promise<void> {
    if (!this.activeJobId) {
      return;
    }

    const response = await this.wailsService.cancelJobV1(this.activeJobId);
    this.statusMessage = response.success
      ? 'Cancel requested.'
      : this.mapJobError(response.error, response.message);
  }

  operationSummary(op: ImageAnnotateOperationV1): string {
    switch (op.type) {
      case 'text':
        return `text @(${op.x},${op.y}) size=${op.fontSize} color=${op.color}`;
      case 'arrow':
        return `arrow (${op.x},${op.y}) -> (${op.x2},${op.y2}) stroke=${op.strokeWidth} opacity=${op.opacity}`;
      case 'rect':
        return `rect (${op.x},${op.y},${op.width},${op.height}) stroke=${op.strokeWidth} opacity=${op.opacity}`;
      case 'blur':
        return `blur (${op.x},${op.y},${op.width},${op.height}) intensity=${op.blurIntensity}`;
      case 'redact':
        return `redact (${op.x},${op.y},${op.width},${op.height}) color=${op.color}`;
      default:
        return op.type;
    }
  }

  displayRectForOperation(op: ImageAnnotateOperationV1): OverlayRect | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      x: (op.x ?? 0) * scale.x,
      y: (op.y ?? 0) * scale.y,
      width: Math.max(1, (op.width ?? 1) * scale.x),
      height: Math.max(1, (op.height ?? 1) * scale.y),
    };
  }

  displayArrowForOperation(op: ImageAnnotateOperationV1): { from: OverlayPoint; to: OverlayPoint } | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      from: {
        x: (op.x ?? 0) * scale.x,
        y: (op.y ?? 0) * scale.y,
      },
      to: {
        x: (op.x2 ?? 0) * scale.x,
        y: (op.y2 ?? 0) * scale.y,
      },
    };
  }

  displayTextPointForOperation(op: ImageAnnotateOperationV1): OverlayPoint | null {
    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      x: (op.x ?? 0) * scale.x,
      y: (op.y ?? 0) * scale.y,
    };
  }

  displayDraftRect(): OverlayRect | null {
    if (!this.drawDraft || this.drawDraft.type === 'arrow') {
      return null;
    }

    const normalized = this.normalizeRect(this.drawDraft.startX, this.drawDraft.startY, this.drawDraft.endX, this.drawDraft.endY);
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
    if (!this.drawDraft || this.drawDraft.type !== 'arrow') {
      return null;
    }

    const scale = this.imageToDisplayScale();
    if (!scale) {
      return null;
    }

    return {
      from: {
        x: this.drawDraft.startX * scale.x,
        y: this.drawDraft.startY * scale.y,
      },
      to: {
        x: this.drawDraft.endX * scale.x,
        y: this.drawDraft.endY * scale.y,
      },
    };
  }

  private async refreshPreviewSource(): Promise<void> {
    const sourcePath = this.previewSourcePath();
    if (!sourcePath) {
      this.sourcePreviewDataUrl = '';
      this.annotatePreviewDataUrl = '';
      this.sourceImageWidth = 0;
      this.sourceImageHeight = 0;
      this.previewMessage =
        this.currentMode() === 'batch'
          ? 'Select at least one image in batch mode to preview.'
          : 'Select an image and add operations to preview.';
      return;
    }

    this.isPreviewLoading = true;
    this.previewMessage = 'Loading source preview from backend...';

    const response: ImagePreviewSourceResponseV1 =
      await this.wailsService.getImagePreviewSourceV1(sourcePath);

    if (!response.success || !response.dataBase64) {
      this.isPreviewLoading = false;
      this.previewMessage = this.mapJobError(response.error, response.message);
      this.sourcePreviewDataUrl = '';
      this.annotatePreviewDataUrl = '';
      this.sourceImageWidth = 0;
      this.sourceImageHeight = 0;
      return;
    }

    this.sourcePreviewDataUrl = `data:${response.mimeType || 'image/png'};base64,${response.dataBase64}`;
    this.sourceImageWidth = response.width || 0;
    this.sourceImageHeight = response.height || 0;
    this.previewMessage = 'Source preview loaded.';
    this.isPreviewLoading = false;

    this.scheduleAnnotatePreviewRefresh();
  }

  private scheduleAnnotatePreviewRefresh(): void {
    if (this.previewDebounceTimer) {
      clearTimeout(this.previewDebounceTimer);
      this.previewDebounceTimer = null;
    }

    if (!this.sourcePreviewDataUrl || this.operations.length === 0) {
      if (this.operations.length === 0) {
        this.annotatePreviewDataUrl = '';
      }
      return;
    }

    this.previewDebounceTimer = setTimeout(() => {
      void this.refreshAnnotatePreview();
    }, 120);
  }

  private async refreshAnnotatePreview(): Promise<void> {
    const sourcePath = this.previewSourcePath();
    if (!sourcePath || this.operations.length === 0) {
      this.annotatePreviewDataUrl = '';
      return;
    }

    this.isPreviewLoading = true;
    this.previewMessage = 'Generating pixel-perfect annotate preview in backend...';

    const req: ImageAnnotatePreviewRequestV1 = {
      inputPath: sourcePath,
      operations: this.operations,
      format: this.form.controls.format.value,
    };

    const response: ImageAnnotatePreviewResponseV1 =
      await this.wailsService.getImageAnnotatePreviewV1(req);

    this.isPreviewLoading = false;

    if (!response.success || !response.dataBase64) {
      this.annotatePreviewDataUrl = '';
      this.previewMessage = this.mapJobError(response.error, response.message);
      return;
    }

    this.annotatePreviewDataUrl = `data:${response.mimeType || 'image/png'};base64,${response.dataBase64}`;
    this.previewMessage = 'Preview is pixel-perfect with backend execution.';
  }

  private localValidationError(): string {
    if (this.currentMode() === 'single') {
      if (!this.selectedInputPath.trim()) {
        return 'Select one input image in single mode.';
      }
    }

    if (this.currentMode() === 'batch' && this.selectedInputPaths.length < 1) {
      return 'Select at least one input image in batch mode.';
    }

    if (this.operations.length === 0) {
      return 'Add at least one annotation operation.';
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const format = this.form.controls.format.value.trim();

    const options: Record<string, unknown> = {
      operations: this.operations,
      ...(format ? { format } : {}),
      ...(mode === 'single' && outputPath ? { outputPath } : {}),
      ...(mode === 'batch' && outputDir ? { outputDir } : {}),
    };

    return {
      toolId: IMAGE_ANNOTATE_TOOL_ID,
      mode,
      inputPaths: mode === 'single' ? [this.selectedInputPath.trim()] : [...this.selectedInputPaths],
      outputDir: mode === 'batch' ? outputDir : this.outputDirFromPath(outputPath),
      options,
    };
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling = true;
    this.pollingTimer = setInterval(() => {
      void this.pollJobStatus();
    }, POLLING_INTERVAL_MS);
    void this.pollJobStatus();
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isPolling = false;
  }

  private async pollJobStatus(): Promise<void> {
    if (!this.activeJobId) {
      this.stopPolling();
      return;
    }

    const response: JobStatusResponseV1 = await this.wailsService.getJobStatusV1(this.activeJobId);
    if (!response.found || !response.result) {
      this.statusMessage = this.mapJobError(response.error, response.message);
      this.stopPolling();
      return;
    }

    this.jobResult = response.result;
    this.statusMessage = `${response.result.status}: ${
      response.result.error
        ? this.mapJobError(response.result.error, response.result.message)
        : response.result.message
    }`;

    if (
      response.result.status === 'success' ||
      response.result.status === 'failed' ||
      response.result.status === 'partial_success' ||
      response.result.status === 'cancelled' ||
      response.result.status === 'interrupted'
    ) {
      this.stopPolling();
      this.activeJobId = '';
    }
  }

  private clearMessages(): void {
    this.validationMessage = '';
    this.submitMessage = '';
    this.statusMessage = '';
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }
    return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
  }

  private currentMode(): JobMode {
    return this.form.controls.jobMode.value === 'batch' ? 'batch' : 'single';
  }

  private previewSourcePath(): string {
    if (this.currentMode() === 'single') {
      return this.selectedInputPath.trim();
    }
    return this.selectedInputPaths[0]?.trim() ?? '';
  }

  private addBatchInput(rawPath: string): void {
    const trimmed = rawPath.trim();
    if (!trimmed || this.selectedInputPaths.includes(trimmed)) {
      return;
    }
    this.selectedInputPaths = [...this.selectedInputPaths, trimmed];
  }

  private outputDirFromPath(outputPath: string): string {
    const lastSlash = Math.max(outputPath.lastIndexOf('/'), outputPath.lastIndexOf('\\'));
    if (lastSlash <= 0) {
      return '';
    }
    return outputPath.slice(0, lastSlash);
  }

  private readInt(raw: string, fallback: number): number {
    const n = Number.parseInt(raw.trim(), 10);
    return Number.isFinite(n) ? n : fallback;
  }

  private readFloat(raw: string, fallback: number): number {
    const n = Number.parseFloat(raw.trim());
    return Number.isFinite(n) ? n : fallback;
  }

  private readOperationFromForm(): ImageAnnotateOperationV1 {
    return {
      type: this.operationForm.controls.type.value,
      x: this.readInt(this.operationForm.controls.x.value, 0),
      y: this.readInt(this.operationForm.controls.y.value, 0),
      width: this.readInt(this.operationForm.controls.width.value, 1),
      height: this.readInt(this.operationForm.controls.height.value, 1),
      x2: this.readInt(this.operationForm.controls.x2.value, 0),
      y2: this.readInt(this.operationForm.controls.y2.value, 0),
      text: this.operationForm.controls.text.value,
      color: this.operationForm.controls.color.value,
      opacity: this.readFloat(this.operationForm.controls.opacity.value, 1),
      strokeWidth: this.readInt(this.operationForm.controls.strokeWidth.value, 2),
      fontSize: this.readInt(this.operationForm.controls.fontSize.value, 18),
      blurIntensity: this.readInt(this.operationForm.controls.blurIntensity.value, 40),
    };
  }

  private onAdvancedFormChanged(): void {
    if (this.suppressAdvancedSync || this.selectedOperationIndex < 0 || this.activeTool !== 'select') {
      return;
    }

    if (this.selectedOperationIndex >= this.operations.length) {
      this.selectedOperationIndex = -1;
      return;
    }

    const updated = [...this.operations];
    const op = this.readOperationFromForm();
    updated[this.selectedOperationIndex] = op;
    this.operations = updated;

    if (op.type === 'redact') {
      this.form.patchValue({ redactConfirmed: false });
    }

    this.scheduleAnnotatePreviewRefresh();
  }

  private patchAdvancedForm(op: ImageAnnotateOperationV1): void {
    this.suppressAdvancedSync = true;
    this.operationForm.patchValue(
      {
        type: op.type,
        x: `${op.x ?? 0}`,
        y: `${op.y ?? 0}`,
        width: `${op.width ?? 1}`,
        height: `${op.height ?? 1}`,
        x2: `${op.x2 ?? 0}`,
        y2: `${op.y2 ?? 0}`,
        text: op.text ?? '',
        color: op.color ?? '#ff0000',
        opacity: `${op.opacity ?? 1}`,
        strokeWidth: `${op.strokeWidth ?? 2}`,
        fontSize: `${op.fontSize ?? 18}`,
        blurIntensity: `${op.blurIntensity ?? 40}`,
      },
      { emitEvent: false }
    );
    this.suppressAdvancedSync = false;
  }

  private canInteractOnOverlay(): boolean {
    return (
      !!this.sourcePreviewDataUrl &&
      this.currentMode() === 'single' &&
      this.sourceImageWidth > 0 &&
      this.sourceImageHeight > 0
    );
  }

  private operationFromDraft(draft: DrawDraft): ImageAnnotateOperationV1 | null {
    if (draft.type === 'arrow') {
      return {
        type: 'arrow',
        x: draft.startX,
        y: draft.startY,
        x2: draft.endX,
        y2: draft.endY,
        width: this.readInt(this.operationForm.controls.width.value, 1),
        height: this.readInt(this.operationForm.controls.height.value, 1),
        color: this.operationForm.controls.color.value,
        opacity: this.readFloat(this.operationForm.controls.opacity.value, 1),
        strokeWidth: this.readInt(this.operationForm.controls.strokeWidth.value, 2),
        fontSize: this.readInt(this.operationForm.controls.fontSize.value, 18),
        blurIntensity: this.readInt(this.operationForm.controls.blurIntensity.value, 40),
        text: this.operationForm.controls.text.value,
      };
    }

    const rect = this.normalizeRect(draft.startX, draft.startY, draft.endX, draft.endY);

    return {
      type: draft.type,
      x: rect.x,
      y: rect.y,
      width: rect.width,
      height: rect.height,
      x2: this.readInt(this.operationForm.controls.x2.value, rect.x + rect.width),
      y2: this.readInt(this.operationForm.controls.y2.value, rect.y + rect.height),
      text: this.operationForm.controls.text.value,
      color: this.operationForm.controls.color.value,
      opacity: this.readFloat(this.operationForm.controls.opacity.value, 1),
      strokeWidth: this.readInt(this.operationForm.controls.strokeWidth.value, 2),
      fontSize: this.readInt(this.operationForm.controls.fontSize.value, 18),
      blurIntensity: this.readInt(this.operationForm.controls.blurIntensity.value, 40),
    };
  }

  private createTextOperation(x: number, y: number): void {
    const currentText = this.operationForm.controls.text.value.trim();
    let text = currentText;

    if (!text && typeof window !== 'undefined') {
      text = window.prompt('Enter annotation text', '')?.trim() ?? '';
    }

    if (!text) {
      return;
    }

    const op: ImageAnnotateOperationV1 = {
      type: 'text',
      x,
      y,
      text,
      color: this.operationForm.controls.color.value,
      opacity: this.readFloat(this.operationForm.controls.opacity.value, 1),
      fontSize: this.readInt(this.operationForm.controls.fontSize.value, 18),
      strokeWidth: this.readInt(this.operationForm.controls.strokeWidth.value, 2),
      width: this.readInt(this.operationForm.controls.width.value, 1),
      height: this.readInt(this.operationForm.controls.height.value, 1),
      x2: this.readInt(this.operationForm.controls.x2.value, x),
      y2: this.readInt(this.operationForm.controls.y2.value, y),
      blurIntensity: this.readInt(this.operationForm.controls.blurIntensity.value, 40),
    };

    this.operations = [...this.operations, op];
    this.operationForm.patchValue({ type: 'text', text });
    this.selectOperation(this.operations.length - 1);
    this.scheduleAnnotatePreviewRefresh();
  }

  private normalizeRect(x1: number, y1: number, x2: number, y2: number): OverlayRect {
    const minX = Math.max(0, Math.min(x1, x2));
    const minY = Math.max(0, Math.min(y1, y2));
    const maxX = Math.min(this.sourceImageWidth, Math.max(x1, x2));
    const maxY = Math.min(this.sourceImageHeight, Math.max(y1, y2));

    return {
      x: Math.round(minX),
      y: Math.round(minY),
      width: Math.max(1, Math.round(maxX - minX)),
      height: Math.max(1, Math.round(maxY - minY)),
    };
  }

  private pointerToImage(event: PointerEvent): OverlayPoint | null {
    const rect = this.previewImageElementRect();
    if (!rect || this.sourceImageWidth <= 0 || this.sourceImageHeight <= 0) {
      return null;
    }

    const localX = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
    const localY = Math.min(Math.max(event.clientY - rect.top, 0), rect.height);
    const scaleX = this.sourceImageWidth / rect.width;
    const scaleY = this.sourceImageHeight / rect.height;

    return {
      x: Math.round(localX * scaleX),
      y: Math.round(localY * scaleY),
    };
  }

  private imageToDisplayScale(): { x: number; y: number } | null {
    const rect = this.previewImageElementRect();
    if (!rect || this.sourceImageWidth <= 0 || this.sourceImageHeight <= 0) {
      return null;
    }

    return {
      x: rect.width / this.sourceImageWidth,
      y: rect.height / this.sourceImageHeight,
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
}
