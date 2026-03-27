import { CommonModule } from '@angular/common';
import {
  Component,
  ElementRef,
  OnDestroy,
  ViewChild,
} from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';

import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
import {
  ImageCropPreviewRequestV1,
  ImageCropPreviewResponseV1,
  ImagePreviewSourceResponseV1,
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const IMAGE_CROP_TOOL_ID = 'tool.image.crop';
const POLLING_INTERVAL_MS = 1000;

type JobMode = 'single' | 'batch';
type RatioPreset = 'free' | '1:1' | '4:3' | '16:9';
type DragMode = 'draw' | 'move' | 'resize-nw' | 'resize-ne' | 'resize-sw' | 'resize-se';

interface CropRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

@Component({
  selector: 'app-image-crop',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './image-crop.html',
  styleUrl: './image-crop.css',
})
export class ImageCrop implements OnDestroy {
  @ViewChild('singleFileInput') singleFileInput?: ElementRef<HTMLInputElement>;
  @ViewChild('batchFileInput') batchFileInput?: ElementRef<HTMLInputElement>;
  @ViewChild('previewImage') previewImage?: ElementRef<HTMLImageElement>;

  readonly form;
  readonly panelFields: ExecutionPanelField[] = [
    {
      controlName: 'jobMode',
      label: 'Mode',
      type: 'select',
      options: [
        { value: 'single', label: 'single' },
        { value: 'batch', label: 'batch' },
      ],
      helpText: 'Batch applies the same crop area to all selected files.',
    },
    {
      controlName: 'outputPath',
      label: 'Output path (optional)',
      type: 'text',
      placeholder: '/path/to/image_cropped.png',
      visibleModes: ['single'],
      helpText: 'If empty, output is auto-generated as *_cropped with collision suffixes.',
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
      controlName: 'ratioPreset',
      label: 'Ratio preset',
      type: 'select',
      options: [
        { value: '1:1', label: '1:1' },
        { value: '4:3', label: '4:3' },
        { value: '16:9', label: '16:9' },
        { value: 'free', label: 'free' },
      ],
      helpText: 'Preset locks ratio. Free unlocks ratio.',
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
      helpText: 'Default preserves source format.',
    },
    { controlName: 'x', label: 'x', type: 'text', placeholder: '0' },
    { controlName: 'y', label: 'y', type: 'text', placeholder: '0' },
    { controlName: 'width', label: 'width', type: 'text', placeholder: '1' },
    { controlName: 'height', label: 'height', type: 'text', placeholder: '1' },
  ];

  selectedInputPath = '';
  selectedInputPaths: string[] = [];
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  previewMessage = 'Select an image to start crop preview.';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  isPreviewLoading = false;

  sourcePreviewDataUrl = '';
  sourcePreviewMimeType = '';
  sourceImageWidth = 0;
  sourceImageHeight = 0;
  cropPreviewDataUrl = '';
  cropPreviewMimeType = '';

  selectionRectPx: CropRect = { x: 0, y: 0, width: 0, height: 0 };

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private previewDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  private activePreviewRequestID = 0;
  private dragState:
    | {
        mode: DragMode;
        startX: number;
        startY: number;
        startRect: CropRect;
      }
    | undefined;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: ['single' as JobMode, Validators.required],
      outputPath: [''],
      outputDir: [''],
      ratioPreset: ['1:1' as RatioPreset, Validators.required],
      format: [''],
      x: ['0', Validators.required],
      y: ['0', Validators.required],
      width: ['1', Validators.required],
      height: ['1', Validators.required],
    });

    this.form.controls.x.valueChanges.subscribe(() => this.onManualCoordinatesChanged());
    this.form.controls.y.valueChanges.subscribe(() => this.onManualCoordinatesChanged());
    this.form.controls.width.valueChanges.subscribe(() => this.onManualCoordinatesChanged());
    this.form.controls.height.valueChanges.subscribe(() => this.onManualCoordinatesChanged());
    this.form.controls.ratioPreset.valueChanges.subscribe((preset) => this.onRatioPresetChanged(preset as RatioPreset));
    this.form.controls.format.valueChanges.subscribe(() => this.scheduleCropPreviewRefresh());
  }

  ngOnDestroy(): void {
    this.stopPolling();
    if (this.previewDebounceTimer) {
      clearTimeout(this.previewDebounceTimer);
      this.previewDebounceTimer = null;
    }
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
      void this.refreshPreviewSource();
      return;
    }

    this.selectedInputPath = selected;
    this.selectedInputPaths = [];
    void this.refreshPreviewSource();
  }

  async selectMultipleImagesFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPath = selected[0].trim();
      void this.refreshPreviewSource();
      return;
    }

    for (const path of selected) {
      this.addBatchInput(path);
    }

    this.previewMessage = 'Batch mode: preview uses first selected image.';
    void this.refreshPreviewSource();
  }

  async selectOutputDirectory(): Promise<void> {
    const selected = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selected) {
      return;
    }
    this.form.patchValue({ outputDir: selected });
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
    this.sourcePreviewMimeType = '';
    this.sourceImageWidth = 0;
    this.sourceImageHeight = 0;
    this.cropPreviewDataUrl = '';
    this.cropPreviewMimeType = '';
    this.previewMessage = 'Select an image to start crop preview.';
  }

  onModeChanged(): void {
    this.clearMessages();
    this.clearPreviewMessagesOnly();

    if (this.currentMode() === 'batch') {
      this.selectedInputPath = '';
    } else {
      this.selectedInputPaths = [];
    }

    void this.refreshPreviewSource();
  }

  onPreviewImageLoaded(): void {
    this.syncSelectionRectFromForm();
  }

  onPreviewPointerDown(event: PointerEvent): void {
    if (!this.sourcePreviewDataUrl || this.currentMode() !== 'single') {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode: 'draw',
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onSelectionHandlePointerDown(event: PointerEvent, mode: DragMode): void {
    if (!this.sourcePreviewDataUrl || this.currentMode() !== 'single') {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    event.stopPropagation();
    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode,
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onSelectionBodyPointerDown(event: PointerEvent): void {
    if (!this.sourcePreviewDataUrl || this.currentMode() !== 'single') {
      return;
    }
    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    event.stopPropagation();
    const local = this.pointerToLocal(event, imageRect);
    this.dragState = {
      mode: 'move',
      startX: local.x,
      startY: local.y,
      startRect: { ...this.selectionRectPx },
    };

    const target = event.currentTarget as HTMLElement | null;
    target?.setPointerCapture(event.pointerId);
  }

  onPreviewPointerMove(event: PointerEvent): void {
    if (!this.dragState) {
      return;
    }

    const imageRect = this.previewImageElementRect();
    if (!imageRect) {
      return;
    }

    const pointer = this.pointerToLocal(event, imageRect);
    const nextRect = this.nextRectForDrag(pointer.x, pointer.y, imageRect.width, imageRect.height);
    this.selectionRectPx = nextRect;
    this.syncFormFromSelectionRect();
  }

  onPreviewPointerUp(): void {
    if (!this.dragState) {
      return;
    }

    this.dragState = undefined;
    this.scheduleCropPreviewRefresh();
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
      ? 'Validation OK. Ready to run crop job.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();
    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
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

  previewCropStyle(): Record<string, string> {
    if (!this.sourcePreviewDataUrl || this.selectionRectPx.width <= 0 || this.selectionRectPx.height <= 0) {
      return { display: 'none' };
    }

    return {
      left: `${this.selectionRectPx.x}px`,
      top: `${this.selectionRectPx.y}px`,
      width: `${this.selectionRectPx.width}px`,
      height: `${this.selectionRectPx.height}px`,
    };
  }

  private async refreshPreviewSource(): Promise<void> {
    const sourcePath = this.previewSourcePath();
    if (!sourcePath) {
      this.sourcePreviewDataUrl = '';
      this.cropPreviewDataUrl = '';
      this.previewMessage = this.currentMode() === 'batch'
        ? 'Select at least one image in batch mode to preview.'
        : 'Select an image to start crop preview.';
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
      this.cropPreviewDataUrl = '';
      return;
    }

    const mimeType = response.mimeType || 'image/png';
    this.sourcePreviewMimeType = mimeType;
    this.sourceImageWidth = response.width || 0;
    this.sourceImageHeight = response.height || 0;
    this.sourcePreviewDataUrl = `data:${mimeType};base64,${response.dataBase64}`;
    this.previewMessage = 'Source preview loaded (EXIF normalized in backend).';
    this.isPreviewLoading = false;

    this.syncSelectionRectFromForm();
    this.scheduleCropPreviewRefresh();
  }

  private onManualCoordinatesChanged(): void {
    this.syncSelectionRectFromForm();
    this.scheduleCropPreviewRefresh();
  }

  private onRatioPresetChanged(preset: RatioPreset): void {
    if (preset !== 'free') {
      const ratio = this.ratioToValue(preset);
      if (ratio > 0) {
        const width = this.parseIntOrFallback(this.form.controls.width.value, 1);
        const adjustedHeight = Math.max(1, Math.round(width / ratio));
        this.form.patchValue({ height: `${adjustedHeight}` }, { emitEvent: false });
      }
    }

    this.syncSelectionRectFromForm();
    this.scheduleCropPreviewRefresh();
  }

  private scheduleCropPreviewRefresh(): void {
    if (this.previewDebounceTimer) {
      clearTimeout(this.previewDebounceTimer);
      this.previewDebounceTimer = null;
    }

    if (!this.sourcePreviewDataUrl) {
      return;
    }

    this.previewDebounceTimer = setTimeout(() => {
      void this.refreshCroppedPreview();
    }, 120);
  }

  private async refreshCroppedPreview(): Promise<void> {
    const sourcePath = this.previewSourcePath();
    if (!sourcePath) {
      this.cropPreviewDataUrl = '';
      return;
    }

    const parsed = this.formCoordinates();
    if (!parsed) {
      this.cropPreviewDataUrl = '';
      return;
    }

    const requestID = ++this.activePreviewRequestID;
    this.isPreviewLoading = true;
    this.previewMessage = 'Generating pixel-perfect crop preview in backend...';

    const request: ImageCropPreviewRequestV1 = {
      inputPath: sourcePath,
      x: parsed.x,
      y: parsed.y,
      width: parsed.width,
      height: parsed.height,
      ratioPreset: this.form.controls.ratioPreset.value as RatioPreset,
      format: this.form.controls.format.value,
    };

    const response: ImageCropPreviewResponseV1 =
      await this.wailsService.getImageCropPreviewV1(request);

    if (requestID !== this.activePreviewRequestID) {
      return;
    }

    this.isPreviewLoading = false;

    if (!response.success || !response.dataBase64) {
      this.cropPreviewDataUrl = '';
      this.previewMessage = this.mapJobError(response.error, response.message);
      return;
    }

    const mimeType = response.mimeType || this.sourcePreviewMimeType || 'image/png';
    this.cropPreviewMimeType = mimeType;
    this.cropPreviewDataUrl = `data:${mimeType};base64,${response.dataBase64}`;
    this.previewMessage = 'Preview is pixel-perfect with backend execution.';
  }

  private localValidationError(): string {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const ratioPreset = this.form.controls.ratioPreset.value as RatioPreset;
    const coords = this.formCoordinates();

    if (mode === 'single') {
      if (!this.selectedInputPath.trim()) {
        return 'Select one input image in single mode.';
      }
      if (!this.isImagePath(this.selectedInputPath.trim())) {
        return `Input extension is not supported: ${this.selectedInputPath}`;
      }
      if (outputPath && outputPath.endsWith('/')) {
        return 'Output path must include a filename.';
      }
    }

    if (mode === 'batch') {
      if (this.selectedInputPaths.length < 1) {
        return 'Select at least one input image in batch mode.';
      }
      for (const inputPath of this.selectedInputPaths) {
        if (!this.isImagePath(inputPath)) {
          return `Input extension is not supported: ${inputPath}`;
        }
      }
    }

    if (!coords) {
      return 'Coordinates must be integers (x, y, width, height).';
    }
    if (coords.x < 0 || coords.y < 0) {
      return 'Coordinates use top-left origin, so x and y must be >= 0.';
    }
    if (coords.width < 1 || coords.height < 1) {
      return 'Minimum crop size is 1x1.';
    }

    if (ratioPreset !== 'free') {
      const expected = this.ratioToValue(ratioPreset);
      if (expected > 0) {
        const actual = coords.width / coords.height;
        if (Math.abs(actual - expected) > 0.0001) {
          return `Ratio preset ${ratioPreset} requires locked width/height proportion.`;
        }
      }
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const ratioPreset = this.form.controls.ratioPreset.value as RatioPreset;
    const format = this.form.controls.format.value.trim();
    const coords = this.formCoordinates() ?? { x: 0, y: 0, width: 1, height: 1 };

    const options: Record<string, unknown> = {
      x: coords.x,
      y: coords.y,
      width: coords.width,
      height: coords.height,
      ratioPreset,
      ...(format ? { format } : {}),
      ...(mode === 'single' && outputPath ? { outputPath } : {}),
      ...(mode === 'batch' && outputDir ? { outputDir } : {}),
    };

    return {
      toolId: IMAGE_CROP_TOOL_ID,
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

    if (this.isTerminalStatus(response.result.status)) {
      this.stopPolling();
      this.activeJobId = '';
    }
  }

  private clearMessages(): void {
    this.validationMessage = '';
    this.submitMessage = '';
    this.statusMessage = '';
  }

  private clearPreviewMessagesOnly(): void {
    this.previewMessage = this.currentMode() === 'batch'
      ? 'Batch mode: preview uses first selected image.'
      : 'Select an image to start crop preview.';
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    switch (error.detail_code) {
      case 'IMAGE_CROP_OUT_OF_BOUNDS':
        return `Crop area is out of bounds: ${error.message}`;
      case 'IMAGE_CROP_RATIO_MISMATCH':
        return `Ratio mismatch: ${error.message}`;
      default:
        break;
    }

    const technical = error.detail_code ? ` [${error.detail_code}]` : '';
    return `${error.code}${technical}: ${error.message}`;
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

  private formCoordinates(): CropRect | null {
    const x = Number.parseInt(this.form.controls.x.value.trim(), 10);
    const y = Number.parseInt(this.form.controls.y.value.trim(), 10);
    const width = Number.parseInt(this.form.controls.width.value.trim(), 10);
    const height = Number.parseInt(this.form.controls.height.value.trim(), 10);

    if (![x, y, width, height].every((v) => Number.isFinite(v))) {
      return null;
    }

    return { x, y, width, height };
  }

  private syncSelectionRectFromForm(): void {
    const coords = this.formCoordinates();
    const imgRect = this.previewImageElementRect();
    if (!coords || !imgRect || this.sourceImageWidth <= 0 || this.sourceImageHeight <= 0) {
      return;
    }

    const scaleX = imgRect.width / this.sourceImageWidth;
    const scaleY = imgRect.height / this.sourceImageHeight;
    this.selectionRectPx = {
      x: Math.max(0, Math.round(coords.x * scaleX)),
      y: Math.max(0, Math.round(coords.y * scaleY)),
      width: Math.max(1, Math.round(coords.width * scaleX)),
      height: Math.max(1, Math.round(coords.height * scaleY)),
    };
  }

  private syncFormFromSelectionRect(): void {
    const imgRect = this.previewImageElementRect();
    if (!imgRect || this.sourceImageWidth <= 0 || this.sourceImageHeight <= 0) {
      return;
    }

    const scaleX = this.sourceImageWidth / imgRect.width;
    const scaleY = this.sourceImageHeight / imgRect.height;

    let width = Math.max(1, Math.round(this.selectionRectPx.width * scaleX));
    let height = Math.max(1, Math.round(this.selectionRectPx.height * scaleY));

    const ratioPreset = this.form.controls.ratioPreset.value as RatioPreset;
    if (ratioPreset !== 'free') {
      const ratio = this.ratioToValue(ratioPreset);
      if (ratio > 0) {
        height = Math.max(1, Math.round(width / ratio));
      }
    }

    const x = Math.max(0, Math.round(this.selectionRectPx.x * scaleX));
    const y = Math.max(0, Math.round(this.selectionRectPx.y * scaleY));

    this.form.patchValue(
      {
        x: `${x}`,
        y: `${y}`,
        width: `${width}`,
        height: `${height}`,
      },
      { emitEvent: false }
    );
  }

  private ratioToValue(preset: RatioPreset): number {
    switch (preset) {
      case '1:1':
        return 1;
      case '4:3':
        return 4 / 3;
      case '16:9':
        return 16 / 9;
      default:
        return 0;
    }
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

  private pointerToLocal(event: PointerEvent, rect: DOMRect): { x: number; y: number } {
    const x = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
    const y = Math.min(Math.max(event.clientY - rect.top, 0), rect.height);
    return { x, y };
  }

  private nextRectForDrag(
    pointerX: number,
    pointerY: number,
    maxWidth: number,
    maxHeight: number
  ): CropRect {
    if (!this.dragState) {
      return this.selectionRectPx;
    }

    const dx = pointerX - this.dragState.startX;
    const dy = pointerY - this.dragState.startY;
    const start = this.dragState.startRect;

    const clamp = (value: number, min: number, max: number): number =>
      Math.min(Math.max(value, min), max);

    let next: CropRect = { ...start };

    switch (this.dragState.mode) {
      case 'draw': {
        const x1 = clamp(this.dragState.startX, 0, maxWidth);
        const y1 = clamp(this.dragState.startY, 0, maxHeight);
        const x2 = clamp(pointerX, 0, maxWidth);
        const y2 = clamp(pointerY, 0, maxHeight);
        next = {
          x: Math.min(x1, x2),
          y: Math.min(y1, y2),
          width: Math.max(1, Math.abs(x2 - x1)),
          height: Math.max(1, Math.abs(y2 - y1)),
        };
        break;
      }
      case 'move': {
        next = {
          x: clamp(start.x + dx, 0, Math.max(0, maxWidth - start.width)),
          y: clamp(start.y + dy, 0, Math.max(0, maxHeight - start.height)),
          width: start.width,
          height: start.height,
        };
        break;
      }
      case 'resize-nw': {
        const x = clamp(start.x + dx, 0, start.x + start.width - 1);
        const y = clamp(start.y + dy, 0, start.y + start.height - 1);
        next = {
          x,
          y,
          width: Math.max(1, start.width + (start.x - x)),
          height: Math.max(1, start.height + (start.y - y)),
        };
        break;
      }
      case 'resize-ne': {
        const right = clamp(start.x + start.width + dx, start.x + 1, maxWidth);
        const y = clamp(start.y + dy, 0, start.y + start.height - 1);
        next = {
          x: start.x,
          y,
          width: Math.max(1, right - start.x),
          height: Math.max(1, start.height + (start.y - y)),
        };
        break;
      }
      case 'resize-sw': {
        const x = clamp(start.x + dx, 0, start.x + start.width - 1);
        const bottom = clamp(start.y + start.height + dy, start.y + 1, maxHeight);
        next = {
          x,
          y: start.y,
          width: Math.max(1, start.width + (start.x - x)),
          height: Math.max(1, bottom - start.y),
        };
        break;
      }
      case 'resize-se': {
        const right = clamp(start.x + start.width + dx, start.x + 1, maxWidth);
        const bottom = clamp(start.y + start.height + dy, start.y + 1, maxHeight);
        next = {
          x: start.x,
          y: start.y,
          width: Math.max(1, right - start.x),
          height: Math.max(1, bottom - start.y),
        };
        break;
      }
      default:
        break;
    }

    const ratioPreset = this.form.controls.ratioPreset.value as RatioPreset;
    if (ratioPreset !== 'free') {
      const ratio = this.ratioToValue(ratioPreset);
      if (ratio > 0) {
        const adjustedHeight = Math.max(1, Math.round(next.width / ratio));
        next.height = Math.min(adjustedHeight, Math.max(1, maxHeight - next.y));
      }
    }

    return {
      x: Math.round(next.x),
      y: Math.round(next.y),
      width: Math.max(1, Math.round(next.width)),
      height: Math.max(1, Math.round(next.height)),
    };
  }

  private parseIntOrFallback(raw: string, fallback: number): number {
    const value = Number.parseInt(raw.trim(), 10);
    if (!Number.isFinite(value)) {
      return fallback;
    }
    return value;
  }

  private isImagePath(path: string): boolean {
    const lower = path.toLowerCase();
    return (
      lower.endsWith('.jpg') ||
      lower.endsWith('.jpeg') ||
      lower.endsWith('.png') ||
      lower.endsWith('.gif') ||
      lower.endsWith('.webp') ||
      lower.endsWith('.bmp') ||
      lower.endsWith('.tiff') ||
      lower.endsWith('.tif')
    );
  }

  private outputDirFromPath(outputPath: string): string {
    const lastSlash = Math.max(outputPath.lastIndexOf('/'), outputPath.lastIndexOf('\\'));
    if (lastSlash <= 0) {
      return '';
    }
    return outputPath.slice(0, lastSlash);
  }

  private isTerminalStatus(status: string): boolean {
    return (
      status === 'success' ||
      status === 'failed' ||
      status === 'partial_success' ||
      status === 'cancelled' ||
      status === 'interrupted'
    );
  }
}
