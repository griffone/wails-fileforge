import { CommonModule } from '@angular/common';
import {
  Component,
  ElementRef,
  OnDestroy,
  OnInit,
  ViewChild,
} from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import * as pdfjsLib from 'pdfjs-dist';
import { Subscription, Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';

import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PDFPreviewSourceResponseV1,
  Wails,
} from '../../services/wails';
import { PdfRendererService } from '../../services/pdf-renderer.service';

const PDF_CROP_TOOL_ID = 'tool.pdf.crop';
const POLLING_INTERVAL_MS = 1000;
const JOB_MODE_SINGLE = 'single';
const JOB_MODE_BATCH = 'batch';

const CROP_PRESETS = ['none', 'small', 'medium', 'large', 'custom'] as const;
type CropPreset = (typeof CROP_PRESETS)[number];

const PDF_WORKER_PUBLIC_URL = 'assets/pdfjs/pdf.worker.min.mjs';
const PDF_CMAP_PUBLIC_URL = 'assets/pdfjs/cmaps/';
const PDF_STANDARD_FONT_PUBLIC_URL = 'assets/pdfjs/standard_fonts/';
const PDF_ICC_PUBLIC_URL = 'assets/pdfjs/iccs/';
const PDF_WASM_PUBLIC_URL = 'assets/pdfjs/wasm/';
type PDFPreviewStatus = 'empty' | 'loading' | 'ready' | 'error';
type PDFPreviewStage =
  | 'input'
  | 'backend'
  | 'worker-init'
  | 'pdf-load'
  | 'render'
  | 'canvas';

export type PDFPreviewErrorCode =
  | 'PDF_PREVIEW_INVALID_PATH'
  | 'PDF_PREVIEW_NOT_PDF'
  | 'PDF_PREVIEW_TOO_LARGE'
  | 'PDF_PREVIEW_READ_FAILED'
  | 'PDF_PREVIEW_WORKER_MISCONFIG'
  | 'PDF_PREVIEW_RENDER_FAILED';

class PDFPreviewRuntimeError extends Error {
  constructor(
    readonly code: PDFPreviewErrorCode,
    message: string,
    readonly stage: PDFPreviewStage,
    readonly recoverable: boolean = true,
  ) {
    super(message);
  }
}

interface WorkerInitResult {
  ok: true;
}

interface WorkerInitFailure {
  ok: false;
  error: PDFPreviewRuntimeError;
}

type EnsurePdfJsWorkerConfigured = () => WorkerInitResult | WorkerInitFailure;

function defaultEnsurePdfJsWorkerConfigured():
  | WorkerInitResult
  | WorkerInitFailure {
  try {
    if (typeof window === 'undefined') {
      return {
        ok: false,
        error: new PDFPreviewRuntimeError(
          'PDF_PREVIEW_WORKER_MISCONFIG',
          'Preview worker is unavailable in this runtime.',
          'worker-init',
        ),
      };
    }

    if (pdfjsLib.GlobalWorkerOptions.workerSrc !== PDF_WORKER_PUBLIC_URL) {
      pdfjsLib.GlobalWorkerOptions.workerSrc = PDF_WORKER_PUBLIC_URL;
    }

    if (pdfjsLib.GlobalWorkerOptions.workerSrc !== PDF_WORKER_PUBLIC_URL) {
      return {
        ok: false,
        error: new PDFPreviewRuntimeError(
          'PDF_PREVIEW_WORKER_MISCONFIG',
          'Unable to configure PDF preview worker source.',
          'worker-init',
        ),
      };
    }

    return { ok: true };
  } catch (error) {
    return {
      ok: false,
      error: new PDFPreviewRuntimeError(
        'PDF_PREVIEW_WORKER_MISCONFIG',
        error instanceof Error
          ? error.message
          : 'Unexpected worker initialization failure.',
        'worker-init',
      ),
    };
  }
}

let ensurePdfJsWorkerConfigured: EnsurePdfJsWorkerConfigured =
  defaultEnsurePdfJsWorkerConfigured;

export function setPreviewWorkerInitializerForTests(
  initializer: EnsurePdfJsWorkerConfigured | null,
): void {
  ensurePdfJsWorkerConfigured =
    initializer ?? defaultEnsurePdfJsWorkerConfigured;
}

@Component({
  selector: 'app-pdf-crop',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './pdf-crop.html',
  styleUrl: './pdf-crop.css',
})
export class PdfCrop implements OnInit, OnDestroy {
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;
  @ViewChild('previewCanvas') previewCanvas?: ElementRef<HTMLCanvasElement>;

  readonly form;
  readonly panelFields: ExecutionPanelField[] = [
    {
      controlName: 'pageSelection',
      label: 'Page selection (optional)',
      type: 'text',
      placeholder: '1-3,5,8-10',
      helpText:
        'Optional. Format: N or A-B, comma-separated. If empty, all pages are cropped.',
    },
    {
      controlName: 'cropPreset',
      label: 'Crop preset',
      type: 'select',
      options: CROP_PRESETS.map((preset) => ({ value: preset, label: preset })),
      helpText: 'Required. none | small | medium | large | custom.',
    },
    {
      controlName: 'marginTop',
      label: 'Margin top (pt)',
      type: 'text',
      placeholder: '10',
      visibleModes: ['custom'],
    },
    {
      controlName: 'marginRight',
      label: 'Margin right (pt)',
      type: 'text',
      placeholder: '10',
      visibleModes: ['custom'],
    },
    {
      controlName: 'marginBottom',
      label: 'Margin bottom (pt)',
      type: 'text',
      placeholder: '10',
      visibleModes: ['custom'],
    },
    {
      controlName: 'marginLeft',
      label: 'Margin left (pt)',
      type: 'text',
      placeholder: '10',
      visibleModes: ['custom'],
    },
  ];

  readonly jobModes = [
    { value: JOB_MODE_SINGLE, label: 'Single input' },
    { value: JOB_MODE_BATCH, label: 'Batch multi-input' },
  ];

  selectedInputPath = '';
  selectedInputFile: File | null = null;
  selectedInputPaths: string[] = [];
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  pageSelectionLiveMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  showRunSummaryConfirmation = false;
  runSummarySnapshot: {
    mode: string;
    range: string;
    cropPreset: string;
    marginsSummary: string;
    fileCount: number;
    outputTarget: string;
  } | null = null;

  previewStatus: PDFPreviewStatus = 'empty';
  previewMessage = 'Select a PDF to render crop preview.';
  previewOverlayStyle: Record<string, string> = {
    left: '0px',
    top: '0px',
    width: '0px',
    height: '0px',
  };

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private formSubscriptions = new Subscription();
  private readonly destroy$ = new Subject<void>();
  private previewMeta: {
    pointsToPxScale: number;
    canvasWidthPx: number;
    canvasHeightPx: number;
  } | null = null;
  private previewRequestID = 0;
  private activePreviewCancel: (() => void) | null = null;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails,
    private readonly pdfRenderer: PdfRendererService,
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: [JOB_MODE_SINGLE, Validators.required],
      outputPath: ['', Validators.required],
      outputDir: ['', Validators.required],
      pageSelection: [''],
      cropPreset: ['small' as CropPreset, Validators.required],
      marginTop: [''],
      marginRight: [''],
      marginBottom: [''],
      marginLeft: [''],
    });
  }

  ngOnInit(): void {
    this.formSubscriptions.add(
      this.form.valueChanges.pipe(takeUntil(this.destroy$)).subscribe(() => {
        this.updateOverlayFromCurrentForm();
      }),
    );

    this.formSubscriptions.add(
      this.form.controls.pageSelection.valueChanges
        .pipe(takeUntil(this.destroy$))
        .subscribe((value) => {
          this.updatePageSelectionLiveMessage(value);
        }),
    );

    this.updatePageSelectionLiveMessage(this.form.controls.pageSelection.value);
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.cancelActivePreviewTask();
    this.formSubscriptions.unsubscribe();
    this.destroy$.next();
    this.destroy$.complete();
  }

  async selectPdfFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = (await this.wailsService.openFileDialog()).trim();
    if (!selected) {
      return;
    }

    if (this.currentJobMode() === JOB_MODE_BATCH) {
      this.addBatchInputPath(selected);
      this.validationMessage = 'PDF input selected.';
      return;
    }

    this.selectedInputPaths = [];
    this.selectedInputFile = null;
    this.selectedInputPath = selected;
    this.validationMessage = 'PDF input selected.';
    void this.refreshPreview();
  }

  async selectMultiplePdfsFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    for (const rawPath of selected) {
      this.addBatchInputPath(rawPath);
    }
    this.validationMessage = `${selected.length} PDF input(s) selected.`;
  }

  async selectOutputDirectory(): Promise<void> {
    const selected = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selected) {
      return;
    }

    this.form.patchValue({ outputDir: selected });
    this.validationMessage = 'Output directory selected.';
  }

  triggerFileInput(): void {
    this.fileInput?.nativeElement.click();
  }

  onFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const files = target?.files;
    if (!files || files.length === 0) {
      return;
    }

    if (this.currentJobMode() === JOB_MODE_BATCH) {
      for (let i = 0; i < files.length; i++) {
        const file = files.item(i);
        if (!file) {
          continue;
        }
        const filePath = (file as File & { path?: string }).path ?? file.name;
        this.addBatchInputPath(filePath);
      }
      this.validationMessage = `${files.length} PDF input(s) selected.`;
    } else {
      this.selectedInputPaths = [];
      const file = files.item(0);
      if (!file) {
        return;
      }

      const filePath = (file as File & { path?: string }).path ?? file.name;
      this.selectedInputFile = file;
      this.selectedInputPath = filePath.trim();
      this.validationMessage = 'PDF input selected.';
      void this.refreshPreview();
    }

    if (target) {
      target.value = '';
    }
  }

  clearSelectedInput(): void {
    this.previewRequestID++;
    this.cancelActivePreviewTask();
    this.selectedInputFile = null;
    this.selectedInputPath = '';
    this.previewMeta = null;
    this.previewStatus = 'empty';
    this.previewMessage = 'Select a PDF to render crop preview.';
    this.previewOverlayStyle = {
      left: '0px',
      top: '0px',
      width: '0px',
      height: '0px',
    };
  }

  clearBatchInputs(): void {
    this.selectedInputPaths = [];
  }

  removeBatchInput(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }

    this.selectedInputPaths = this.selectedInputPaths.filter(
      (_, i) => i !== index,
    );
  }

  onJobModeChange(): void {
    if (this.currentJobMode() === JOB_MODE_BATCH) {
      this.previewRequestID++;
      this.cancelActivePreviewTask();
      this.selectedInputFile = null;
      this.selectedInputPath = '';
      this.previewMeta = null;
      this.previewStatus = 'empty';
      this.previewMessage = 'Preview available only in single mode.';
      return;
    }

    if (this.selectedInputPath.trim()) {
      void this.refreshPreview();
    }
  }

  async refreshPreview(): Promise<void> {
    if (this.currentJobMode() === JOB_MODE_BATCH) {
      this.previewRequestID++;
      this.cancelActivePreviewTask();
      this.previewMeta = null;
      this.previewStatus = 'empty';
      this.previewMessage = 'Preview available only in single mode.';
      return;
    }

    const hasFile = !!this.selectedInputFile;
    const selectedPath = this.selectedInputPath.trim();
    if (!hasFile && !selectedPath) {
      this.previewRequestID++;
      this.cancelActivePreviewTask();
      this.previewMeta = null;
      this.previewStatus = 'empty';
      this.previewMessage = 'Select a PDF to render crop preview.';
      return;
    }

    this.cancelActivePreviewTask();
    this.previewStatus = 'loading';
    this.previewMessage = 'Rendering preview...';
    const requestID = ++this.previewRequestID;

    try {
      const bytes = await this.loadPreviewPDFBytes();
      if (requestID !== this.previewRequestID) {
        return;
      }

      const workerInit = ensurePdfJsWorkerConfigured();
      if (!workerInit.ok) {
        throw workerInit.error;
      }

      const loadingTask = pdfjsLib.getDocument({
        data: bytes,
        cMapUrl: PDF_CMAP_PUBLIC_URL,
        cMapPacked: true,
        standardFontDataUrl: PDF_STANDARD_FONT_PUBLIC_URL,
        iccUrl: PDF_ICC_PUBLIC_URL,
        wasmUrl: PDF_WASM_PUBLIC_URL,
        useWasm: true,
        useWorkerFetch: true,
        isImageDecoderSupported: false,
        isOffscreenCanvasSupported: false,
      });
      this.activePreviewCancel = () => {
        void loadingTask.destroy();
      };

      const pdfDocument = await loadingTask.promise.catch((error: unknown) => {
        throw this.wrapRenderError(error, 'pdf-load');
      });
      if (requestID !== this.previewRequestID) {
        try {
          await pdfDocument.destroy();
        } catch {
          // no-op
        }
        return;
      }

      const canvas = await this.waitForPreviewCanvas();
      if (!canvas) {
        try {
          await pdfDocument.destroy();
        } catch {
          // no-op
        }
        throw new PDFPreviewRuntimeError(
          'PDF_PREVIEW_RENDER_FAILED',
          'Preview canvas is not available.',
          'canvas',
        );
      }

      const maxPreviewWidthPx = 560;
      const page = await pdfDocument.getPage(1);
      const baseViewport = page.getViewport({ scale: 1 });
      const scale = Math.min(
        1.6,
        maxPreviewWidthPx / Math.max(baseViewport.width, 1),
      );

      await this.pdfRenderer.renderPageToCanvasWithFallback(
        pdfDocument,
        1,
        canvas,
        scale,
        selectedPath,
      );

      if (requestID !== this.previewRequestID) {
        try {
          await pdfDocument.destroy();
        } catch {
          // no-op
        }
        return;
      }

      try {
        await pdfDocument.destroy();
      } catch {
        // no-op
      }

      this.previewMeta = {
        pointsToPxScale: scale,
        canvasWidthPx: canvas.width,
        canvasHeightPx: canvas.height,
      };

      this.previewStatus = 'ready';
      this.previewMessage =
        'Preview approximate — final crop may vary slightly by renderer.';
      this.updateOverlayFromCurrentForm();
    } catch (error) {
      if (requestID !== this.previewRequestID) {
        return;
      }

      this.previewMeta = null;
      this.previewStatus = 'error';
      const mapped = this.mapPreviewError(error);
      this.previewMessage = mapped.message;
      this.logPreviewIssue(mapped.code, mapped.stage, mapped.internalMessage);
    } finally {
      if (requestID === this.previewRequestID) {
        this.activePreviewCancel = null;
      }
    }
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
      ? 'Validation OK. Ready to run crop.'
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

    this.runSummarySnapshot = {
      mode: request.mode,
      range:
        (request.options['pageSelection'] as string | undefined)?.trim() ||
        'all pages',
      cropPreset: String(request.options['cropPreset'] ?? ''),
      marginsSummary: this.cropSummaryMarginsText(request),
      fileCount: request.inputPaths.length,
      outputTarget:
        request.mode === JOB_MODE_SINGLE
          ? String(request.options['outputPath'] ?? '')
          : String(request.options['outputDir'] ?? ''),
    };
    this.showRunSummaryConfirmation = true;
  }

  cancelRunSummaryConfirmation(): void {
    this.showRunSummaryConfirmation = false;
    this.submitMessage = 'Execution canceled: summary was not confirmed.';
  }

  async confirmRunSummaryAndExecute(): Promise<void> {
    if (!this.showRunSummaryConfirmation) {
      return;
    }

    const localError = this.localValidationError();
    if (localError) {
      this.showRunSummaryConfirmation = false;
      this.submitMessage = localError;
      return;
    }

    const request = this.buildRequest();
    this.showRunSummaryConfirmation = false;

    this.isSubmitting = true;
    this.statusMessage = '';
    this.jobResult = null;

    const validation = await this.wailsService.validateJobV1(request);
    if (!validation.valid) {
      this.submitMessage = this.mapJobError(
        validation.error,
        validation.message,
      );
      this.isSubmitting = false;
      return;
    }

    const runResponse = await this.wailsService.runJobV1(request);
    if (!runResponse.success || !runResponse.jobId) {
      this.submitMessage = this.mapJobError(
        runResponse.error,
        runResponse.message,
      );
      this.isSubmitting = false;
      return;
    }

    this.activeJobId = runResponse.jobId;
    this.submitMessage = `Job submitted: ${runResponse.jobId}`;
    this.startPolling();
    this.isSubmitting = false;
  }

  private cropSummaryMarginsText(request: JobRequestV1): string {
    const preset = String(request.options['cropPreset'] ?? '').trim();
    if (preset !== 'custom') {
      return `preset=${preset}`;
    }

    const margins = request.options['margins'];
    if (!margins || typeof margins !== 'object') {
      return 'preset=custom (margins missing)';
    }

    const m = margins as Record<string, unknown>;
    return `preset=custom (top=${m['top']}, right=${m['right']}, bottom=${m['bottom']}, left=${m['left']})`;
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

  private localValidationError(): string {
    const mode = this.currentJobMode();
    const inputPath = this.selectedInputPath.trim();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const pageSelection = this.form.controls.pageSelection.value.trim();
    const cropPreset = this.form.controls.cropPreset.value.trim();

    if (mode !== JOB_MODE_SINGLE && mode !== JOB_MODE_BATCH) {
      return `Unsupported mode: ${mode}`;
    }

    if (mode === JOB_MODE_SINGLE) {
      if (!inputPath) {
        return 'Select one input PDF file.';
      }
      if (!this.isPDFPath(inputPath)) {
        return `Input file must be .pdf: ${inputPath}`;
      }

      if (!outputPath) {
        return 'Output path is required.';
      }
      if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
        return 'Output path must include a .pdf filename, not only a directory.';
      }
      if (!this.isPDFPath(outputPath)) {
        return 'Output path must end with .pdf';
      }
    }

    if (mode === JOB_MODE_BATCH) {
      if (this.selectedInputPaths.length < 1) {
        return 'Select at least one PDF input file for batch mode.';
      }
      for (const batchInputPath of this.selectedInputPaths) {
        if (!this.isPDFPath(batchInputPath)) {
          return `Input file must be .pdf: ${batchInputPath}`;
        }
      }
      if (!outputDir) {
        return 'Output directory is required for batch mode.';
      }
    }

    if (!CROP_PRESETS.includes(cropPreset as CropPreset)) {
      return `Unsupported cropPreset: ${cropPreset}`;
    }

    const pageSelectionError = this.localPageSelectionError(pageSelection);
    if (pageSelectionError) {
      return pageSelectionError;
    }

    if (cropPreset === 'custom') {
      const marginKeys: Array<keyof typeof this.form.controls> = [
        'marginTop',
        'marginRight',
        'marginBottom',
        'marginLeft',
      ];

      for (const key of marginKeys) {
        const value = this.form.controls[key].value.trim();
        if (!value) {
          return 'Custom cropPreset requires all margins (top/right/bottom/left).';
        }

        const parsed = Number.parseFloat(value);
        if (!Number.isFinite(parsed)) {
          return `Margin ${key} must be a valid number.`;
        }
        if (parsed < 0) {
          return `Margin ${key} must be >= 0.`;
        }
      }
    }

    return '';
  }

  private async loadPreviewPDFBytes(): Promise<Uint8Array> {
    if (this.selectedInputFile) {
      const arrayBuffer = await this.selectedInputFile.arrayBuffer();
      return new Uint8Array(arrayBuffer);
    }

    const inputPath = this.selectedInputPath.trim();
    if (!inputPath) {
      throw new PDFPreviewRuntimeError(
        'PDF_PREVIEW_INVALID_PATH',
        'No PDF selected for preview.',
        'input',
      );
    }

    const response = await this.wailsService.getPdfPreviewSource(inputPath);
    if (!response.success || !response.dataBase64) {
      throw this.mapBackendPreviewError(response);
    }

    return this.decodeBase64ToBytes(response.dataBase64);
  }

  private decodeBase64ToBytes(base64: string): Uint8Array {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  private mapBackendPreviewError(
    response: PDFPreviewSourceResponseV1,
  ): PDFPreviewRuntimeError {
    const backendCode = response.error?.code;
    const backendMessage = response.error?.message || response.message;

    switch (backendCode) {
      case 'PDF_PREVIEW_INVALID_PATH':
        return new PDFPreviewRuntimeError(
          'PDF_PREVIEW_INVALID_PATH',
          backendMessage || 'Selected path is invalid.',
          'backend',
        );
      case 'PDF_PREVIEW_NOT_PDF':
        return new PDFPreviewRuntimeError(
          'PDF_PREVIEW_NOT_PDF',
          backendMessage || 'Preview supports only PDF files.',
          'backend',
        );
      case 'PDF_PREVIEW_TOO_LARGE':
        return new PDFPreviewRuntimeError(
          'PDF_PREVIEW_TOO_LARGE',
          backendMessage || 'PDF is too large for preview.',
          'backend',
        );
      case 'PDF_PREVIEW_READ_FAILED':
        return new PDFPreviewRuntimeError(
          'PDF_PREVIEW_READ_FAILED',
          backendMessage || 'Failed to read PDF preview source.',
          'backend',
        );
      default:
        return new PDFPreviewRuntimeError(
          'PDF_PREVIEW_READ_FAILED',
          backendMessage || 'Failed to load PDF bytes from backend.',
          'backend',
        );
    }
  }

  private mapPreviewError(error: unknown): {
    code: PDFPreviewErrorCode;
    stage: PDFPreviewStage;
    internalMessage: string;
    message: string;
  } {
    const runtimeError = this.coercePreviewRuntimeError(error);

    const uxMessage = this.mapPreviewErrorToMessage(runtimeError.code);
    return {
      code: runtimeError.code,
      stage: runtimeError.stage,
      internalMessage: runtimeError.message,
      message: `Preview failed: ${uxMessage}`,
    };
  }

  private coercePreviewRuntimeError(error: unknown): PDFPreviewRuntimeError {
    if (error instanceof PDFPreviewRuntimeError) {
      return error;
    }

    if (this.isPreviewRuntimeErrorLike(error)) {
      return new PDFPreviewRuntimeError(error.code, error.message, error.stage);
    }

    return new PDFPreviewRuntimeError(
      'PDF_PREVIEW_RENDER_FAILED',
      error instanceof Error ? error.message : 'Unknown preview error.',
      'render',
    );
  }

  private isPreviewRuntimeErrorLike(
    error: unknown,
  ): error is {
    code: PDFPreviewErrorCode;
    stage: PDFPreviewStage;
    message: string;
  } {
    if (!error || typeof error !== 'object') {
      return false;
    }

    const candidate = error as Record<string, unknown>;
    return (
      typeof candidate['code'] === 'string' &&
      typeof candidate['stage'] === 'string' &&
      typeof candidate['message'] === 'string'
    );
  }

  private mapPreviewErrorToMessage(code: PDFPreviewErrorCode): string {
    switch (code) {
      case 'PDF_PREVIEW_INVALID_PATH':
        return 'Selected path is invalid. Re-select the input PDF.';
      case 'PDF_PREVIEW_NOT_PDF':
        return 'Only .pdf files are supported for preview.';
      case 'PDF_PREVIEW_TOO_LARGE':
        return 'File is too large for preview transfer. Use a smaller PDF.';
      case 'PDF_PREVIEW_READ_FAILED':
        return 'Could not read the PDF preview source. Check file access and retry.';
      case 'PDF_PREVIEW_WORKER_MISCONFIG':
        return 'Preview worker failed to initialize. Retry preview or restart the app.';
      case 'PDF_PREVIEW_RENDER_FAILED':
        return 'PDF renderer failed to generate preview. Retry preview.';
      default:
        return 'Unknown preview error.';
    }
  }

  private wrapRenderError(
    error: unknown,
    stage: PDFPreviewStage,
  ): PDFPreviewRuntimeError {
    const message =
      error instanceof Error ? error.message : 'Unknown rendering error.';

    if (message.toLowerCase().includes('workersrc')) {
      return new PDFPreviewRuntimeError(
        'PDF_PREVIEW_WORKER_MISCONFIG',
        message,
        stage,
      );
    }

    return new PDFPreviewRuntimeError(
      'PDF_PREVIEW_RENDER_FAILED',
      message,
      stage,
    );
  }

  private cancelActivePreviewTask(): void {
    try {
      this.activePreviewCancel?.();
    } catch {
      // no-op
    }
    this.activePreviewCancel = null;
  }

  private logPreviewIssue(
    code: PDFPreviewErrorCode,
    stage: PDFPreviewStage,
    detail: string,
  ): void {
    console.warn('[pdf-crop.preview]', { code, stage, detail });
  }

  private async waitForPreviewCanvas(): Promise<HTMLCanvasElement | undefined> {
    for (let attempt = 0; attempt < 3; attempt++) {
      const canvas = this.previewCanvas?.nativeElement;
      if (canvas) {
        return canvas;
      }
      await Promise.resolve();
    }
    return this.previewCanvas?.nativeElement;
  }

  private updateOverlayFromCurrentForm(): void {
    if (!this.previewMeta) {
      return;
    }

    const cropPreset = this.form.controls.cropPreset.value as CropPreset;
    const margins = this.resolvePreviewMargins(cropPreset);

    const overlay = computePreviewOverlayRect(
      this.previewMeta.canvasWidthPx,
      this.previewMeta.canvasHeightPx,
      this.previewMeta.pointsToPxScale,
      margins,
    );

    this.previewOverlayStyle = {
      left: `${overlay.left}px`,
      top: `${overlay.top}px`,
      width: `${overlay.width}px`,
      height: `${overlay.height}px`,
    };
  }

  private resolvePreviewMargins(cropPreset: CropPreset): CropMargins {
    if (cropPreset === 'custom') {
      return {
        top: this.parsePreviewMargin(this.form.controls.marginTop.value),
        right: this.parsePreviewMargin(this.form.controls.marginRight.value),
        bottom: this.parsePreviewMargin(this.form.controls.marginBottom.value),
        left: this.parsePreviewMargin(this.form.controls.marginLeft.value),
      };
    }

    switch (cropPreset) {
      case 'none':
        return { top: 0, right: 0, bottom: 0, left: 0 };
      case 'small':
        return { top: 10, right: 10, bottom: 10, left: 10 };
      case 'medium':
        return { top: 20, right: 20, bottom: 20, left: 20 };
      case 'large':
        return { top: 40, right: 40, bottom: 40, left: 40 };
      default:
        return { top: 0, right: 0, bottom: 0, left: 0 };
    }
  }

  private parsePreviewMargin(raw: string): number {
    const parsed = Number.parseFloat(raw.trim());
    if (!Number.isFinite(parsed) || parsed < 0) {
      return 0;
    }
    return parsed;
  }

  private buildRequest(): JobRequestV1 {
    const jobMode = this.currentJobMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const pageSelection = this.form.controls.pageSelection.value.trim();
    const cropPreset = this.form.controls.cropPreset.value.trim();

    const isSingle = jobMode === JOB_MODE_SINGLE;

    return {
      toolId: PDF_CROP_TOOL_ID,
      mode: isSingle ? JOB_MODE_SINGLE : JOB_MODE_BATCH,
      inputPaths: isSingle
        ? [this.selectedInputPath.trim()]
        : this.selectedInputPaths.map((path) => path.trim()),
      outputDir: isSingle ? this.outputDirFromPath(outputPath) : outputDir,
      options: {
        ...(isSingle ? { outputPath } : { outputDir }),
        cropPreset,
        ...(pageSelection ? { pageSelection } : {}),
        ...(cropPreset === 'custom'
          ? {
              margins: {
                top: Number.parseFloat(
                  this.form.controls.marginTop.value.trim(),
                ),
                right: Number.parseFloat(
                  this.form.controls.marginRight.value.trim(),
                ),
                bottom: Number.parseFloat(
                  this.form.controls.marginBottom.value.trim(),
                ),
                left: Number.parseFloat(
                  this.form.controls.marginLeft.value.trim(),
                ),
              },
            }
          : {}),
      },
    };
  }

  private outputDirFromPath(outputPath: string): string {
    const lastSlash = Math.max(
      outputPath.lastIndexOf('/'),
      outputPath.lastIndexOf('\\'),
    );
    if (lastSlash <= 0) {
      return '';
    }
    return outputPath.slice(0, lastSlash);
  }

  private localPageSelectionError(pageSelection: string): string {
    if (!pageSelection) {
      return '';
    }

    const rawTokens = pageSelection.split(',');
    if (rawTokens.some((token) => token.trim().length === 0)) {
      return 'Page selection format is invalid (empty token). Use: 1-3,5,8-10';
    }

    const single = /^\d+$/;
    const span = /^(\d+)\s*-\s*(\d+)$/;

    for (const rawToken of rawTokens) {
      const token = rawToken.trim();
      if (single.test(token)) {
        if (Number.parseInt(token, 10) <= 0) {
          return `Page selection must be positive: ${token}`;
        }
        continue;
      }

      const match = token.match(span);
      if (!match) {
        return `Page selection format is invalid: ${token}. Use N or A-B`;
      }

      const start = Number.parseInt(match[1], 10);
      const end = Number.parseInt(match[2], 10);
      if (start <= 0 || end <= 0) {
        return `Page selection must be positive: ${token}`;
      }
      if (start > end) {
        return `Page selection start must be <= end: ${token}`;
      }
    }

    return '';
  }

  private updatePageSelectionLiveMessage(rawPageSelection: string): void {
    const pageSelection = rawPageSelection.trim();
    this.pageSelectionLiveMessage = this.localPageSelectionError(pageSelection);
  }

  private isPDFPath(path: string): boolean {
    return path.toLowerCase().endsWith('.pdf');
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

    const response: JobStatusResponseV1 =
      await this.wailsService.getJobStatusV1(this.activeJobId);
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
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    switch (error.code) {
      case 'VALIDATION_INVALID_INPUT':
        return `Validation: ${error.message}`;
      case 'RUNTIME_DEP_MISSING':
        return `Runtime dependency missing.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Crop execution failed.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Crop execution timeout.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Unsupported format.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }

  private currentJobMode(): string {
    return this.form.controls.jobMode.value.trim();
  }

  private addBatchInputPath(rawPath: string): void {
    const inputPath = rawPath.trim();
    if (!inputPath) {
      return;
    }

    if (this.selectedInputPaths.includes(inputPath)) {
      return;
    }

    this.selectedInputPaths = [...this.selectedInputPaths, inputPath];
  }
}

interface CropMargins {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

export interface PreviewOverlayRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export function computePreviewOverlayRect(
  canvasWidthPx: number,
  canvasHeightPx: number,
  pointsToPxScale: number,
  marginsPt: CropMargins,
): PreviewOverlayRect {
  const clampPx = (value: number): number => {
    if (!Number.isFinite(value) || value < 0) {
      return 0;
    }
    return Math.round(value * 100) / 100;
  };

  const left = clampPx(marginsPt.left * pointsToPxScale);
  const right = clampPx(marginsPt.right * pointsToPxScale);
  const top = clampPx(marginsPt.top * pointsToPxScale);
  const bottom = clampPx(marginsPt.bottom * pointsToPxScale);

  const width = Math.max(0, clampPx(canvasWidthPx - left - right));
  const height = Math.max(0, clampPx(canvasHeightPx - top - bottom));

  return {
    left,
    top,
    width,
    height,
  };
}
