import { CommonModule } from '@angular/common';
import { Component, ElementRef, OnDestroy, ViewChild } from '@angular/core';
import { FEATURE_FLAGS } from '../../config/feature-flags';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
import { PdfRendererService } from '../../services/pdf-renderer.service';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const PDF_SPLIT_TOOL_ID = 'tool.pdf.split';
const STRATEGY_EVERY_PAGE = 'every_page';
const STRATEGY_RANGES = 'ranges';
const JOB_MODE_SINGLE = 'single';
const JOB_MODE_BATCH = 'batch';
const POLLING_INTERVAL_MS = 1000;
const SOURCE_PREVIEW_MAX_WIDTH_PX = 560;
const BLOCK_PREVIEW_MAX_WIDTH_PX = 240;
const PREVIEW_SAMPLE_SCALE = 0.18;

interface SplitPreviewBlock {
  key: string;
  label: string;
  pageRange: string;
  pageNumber: number;
}

interface PreviewPdfDocument {
  numPages: number;
  getPage(pageNumber: number): Promise<any>;
  destroy?(): Promise<void>;
}

@Component({
  selector: 'app-pdf-split',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './pdf-split.html',
  styleUrl: './pdf-split.css',
})
export class PdfSplit implements OnDestroy {
  private readonly destroy$ = new Subject<void>();
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  readonly form;
  readonly panelFields: ExecutionPanelField[] = [
    {
      controlName: 'outputDir',
      label: 'Output Directory',
      type: 'text',
      placeholder: '/path/to/output/folder',
      helpText: 'Required. The split files will be created in this directory.',
    },
    {
      controlName: 'strategy',
      label: 'Split Strategy',
      type: 'select',
      options: [
        { value: STRATEGY_EVERY_PAGE, label: 'Every page' },
        { value: STRATEGY_RANGES, label: 'Ranges' },
      ],
      helpText:
        'Choose every_page or ranges. Examples: 1-3,5,8-10. No overlaps or duplicates.',
    },
    {
      controlName: 'ranges',
      label: 'Page ranges',
      type: 'text',
      placeholder: '1-3,5,8-10',
      helpText:
        'Required only for strategy=ranges. Format: N or A-B, comma-separated. Example: 1-3,5,8-10',
      visibleModes: [STRATEGY_RANGES],
    },
  ];

  readonly jobModes = [
    { value: JOB_MODE_SINGLE, label: 'Single input' },
    { value: JOB_MODE_BATCH, label: 'Batch multi-input' },
  ];

  selectedInputPaths: string[] = [];
  readonly featureFlags = FEATURE_FLAGS;
  previewMessage = 'Select a PDF to generate split thumbnails.';
  previewImageDataUrl: string | null = null;
  splitPreviewBlocks: SplitPreviewBlock[] = [];
  splitPreviewUrls: Record<string, string | null> = {};
  private previewRequested = false;
  private previewRequestGeneration = 0;
  private previewSourceBytes: Uint8Array | null = null;
  private previewSourcePageCount = 0;
  private previewSourcePdf: PreviewPdfDocument | null = null;
  private previewRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  previewStatus = '';
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';

  private pollingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(
    private readonly fb: FormBuilder,
    private readonly pdfRenderer: PdfRendererService,
    private readonly wailsService: Wails,
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: [JOB_MODE_BATCH, Validators.required],
      outputDir: ['', Validators.required],
      strategy: [STRATEGY_EVERY_PAGE, Validators.required],
      ranges: [''],
    });

    this.form.controls.jobMode.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => {
        this.scheduleSplitPreviewRefresh();
      });
    this.form.controls.strategy.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => {
        this.scheduleSplitPreviewRefresh();
      });
    this.form.controls.ranges.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => {
        this.scheduleSplitPreviewRefresh();
      });
  }

  async ensureSourcePreview(): Promise<void> {
    if (!this.featureFlags.uiux_overhaul_v1) return;
    if (this.previewRequested) return;
    if (this.selectedInputPaths.length === 0) return;

    const requestGeneration = ++this.previewRequestGeneration;
    const source = this.selectedInputPaths[0];
    this.previewRequested = true;
    this.previewStatus = 'loading';
    this.previewMessage = 'Loading source PDF preview...';
    try {
      const resp = await this.wailsService.getPdfPreviewSource(source);
      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      if (!resp.success || !resp.dataBase64) {
        throw new Error(resp.message || 'Preview source unavailable.');
      }

      this.previewSourceBytes = this.base64ToBytes(resp.dataBase64);
      await this.loadPreviewPdf(source);
      if (!this.previewSourcePdf) {
        throw new Error('Unable to load preview PDF.');
      }

      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      this.previewSourcePageCount = this.previewSourcePdf?.numPages ?? 0;
      const previewPageNumber = await this.pickPreviewPageNumber(
        this.previewSourcePdf,
        this.buildPageNumbers(this.previewSourcePageCount),
        source,
        requestGeneration,
      );
      this.previewImageDataUrl = await this.renderPageToDataUrl(
        this.previewSourcePdf,
        previewPageNumber,
        SOURCE_PREVIEW_MAX_WIDTH_PX,
        source,
      );
      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      this.previewStatus = 'ready';
      this.previewMessage =
        'Source preview ready. Building block thumbnails...';
      await this.renderSplitPreviewBlocks(source, requestGeneration);
    } catch (err) {
      this.previewStatus = 'error';
      const previewErrorMessage =
        err instanceof Error ? err.message : 'Source preview unavailable.';
      this.previewMessage = previewErrorMessage;
      this.previewImageDataUrl = null;
      this.previewSourceBytes = null;
      this.previewSourcePageCount = 0;
      this.splitPreviewBlocks = [];
      this.splitPreviewUrls = {};
      this.previewRequested = false;
      await this.destroyPreviewPdf();
      console.warn('[pdf-split.preview] preview request exception', err);
    }
  }

  private scheduleSplitPreviewRefresh(): void {
    if (!this.featureFlags.uiux_overhaul_v1) return;

    if (this.previewRefreshTimer) {
      clearTimeout(this.previewRefreshTimer);
    }

    this.previewRefreshTimer = setTimeout(() => {
      this.previewRefreshTimer = null;
      void this.refreshSplitPreviewBlocks();
    }, 120);
  }

  private async refreshSplitPreviewBlocks(): Promise<void> {
    if (!this.featureFlags.uiux_overhaul_v1) return;
    if (this.selectedInputPaths.length === 0) {
      this.resetPreviewState();
      return;
    }

    if (!this.previewSourcePdf) {
      if (!this.previewRequested) {
        void this.ensureSourcePreview();
      }
      return;
    }

    await this.renderSplitPreviewBlocks(
      this.selectedInputPaths[0],
      this.previewRequestGeneration,
    );
  }

  private async renderSplitPreviewBlocks(
    sourcePath: string,
    requestGeneration: number,
  ): Promise<void> {
    if (
      !this.previewSourcePdf ||
      requestGeneration !== this.previewRequestGeneration
    ) {
      return;
    }

    const blocks = this.buildSplitPreviewBlocks();
    this.splitPreviewBlocks = blocks;
    this.splitPreviewUrls = {};

    if (blocks.length === 0) {
      this.previewMessage =
        this.form.controls.strategy.value.trim() === STRATEGY_RANGES
          ? 'Enter valid page ranges to preview block thumbnails.'
          : 'No pages available to preview.';
      return;
    }

    this.previewMessage = `Rendering ${blocks.length} block thumbnail(s)...`;

    for (const block of blocks) {
      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      await this.renderSplitPreviewBlock(
        block.key,
        sourcePath,
        block.pageRange,
        block.pageNumber,
        requestGeneration,
      );
    }

    if (requestGeneration === this.previewRequestGeneration) {
      this.previewMessage = `${blocks.length} block thumbnail(s) ready.`;
    }
  }

  ngOnDestroy(): void {
    this.stopPolling();
    if (this.previewRefreshTimer) {
      clearTimeout(this.previewRefreshTimer);
      this.previewRefreshTimer = null;
    }
    void this.destroyPreviewPdf();
    this.destroy$.next();
    this.destroy$.complete();
  }

  async selectPdfFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = (await this.wailsService.openFileDialog()).trim();
    if (!selected) {
      return;
    }
    this.addInputPath(selected);
    this.validationMessage = 'PDF input selected.';
  }

  async selectMultiplePdfsFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    for (const rawPath of selected) {
      this.addInputPath(rawPath, false);
    }
    void this.ensureSourcePreview();
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

    for (let i = 0; i < files.length; i++) {
      const file = files.item(i);
      if (!file) {
        continue;
      }
      const filePath = (file as File & { path?: string }).path ?? file.name;
      this.addInputPath(filePath, false);
    }
    void this.ensureSourcePreview();
    this.validationMessage = `${files.length} PDF input(s) selected.`;
    if (target) {
      target.value = '';
    }
  }

  removeInputPath(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }

    this.selectedInputPaths = this.selectedInputPaths.filter(
      (_, i) => i !== index,
    );
    this.resetPreviewState();
    if (this.selectedInputPaths.length > 0) {
      void this.ensureSourcePreview();
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
      ? 'Validation OK. Ready to run split.'
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
    const mode = this.form.controls.jobMode.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const strategy = this.form.controls.strategy.value.trim();
    const ranges = this.form.controls.ranges.value.trim();

    if (mode !== JOB_MODE_SINGLE && mode !== JOB_MODE_BATCH) {
      return `Unsupported mode: ${mode}`;
    }

    if (mode === JOB_MODE_SINGLE && this.selectedInputPaths.length !== 1) {
      return 'Select exactly one PDF input file.';
    }

    if (mode === JOB_MODE_BATCH && this.selectedInputPaths.length < 1) {
      return 'Select at least one PDF input file for batch mode.';
    }

    for (const inputPath of this.selectedInputPaths) {
      if (!this.isPDFPath(inputPath)) {
        return `Input file must be .pdf: ${inputPath}`;
      }
    }

    if (!outputDir) {
      return 'Output directory is required.';
    }

    if (strategy !== STRATEGY_EVERY_PAGE && strategy !== STRATEGY_RANGES) {
      return `Unsupported strategy: ${strategy}`;
    }

    if (strategy === STRATEGY_RANGES) {
      const rangesError = this.localRangesValidationError(ranges);
      if (rangesError) {
        return rangesError;
      }
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const jobMode = this.form.controls.jobMode.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const strategy = this.form.controls.strategy.value.trim();
    const ranges = this.form.controls.ranges.value.trim();

    return {
      toolId: PDF_SPLIT_TOOL_ID,
      mode: jobMode === JOB_MODE_SINGLE ? JOB_MODE_SINGLE : JOB_MODE_BATCH,
      inputPaths: this.selectedInputPaths.map((path) => path.trim()),
      outputDir,
      options: {
        outputDir,
        strategy,
        ...(strategy === STRATEGY_RANGES ? { ranges } : {}),
        perInputDir: jobMode === JOB_MODE_BATCH,
      },
    };
  }

  private localRangesValidationError(ranges: string): string {
    if (!ranges) {
      return 'Ranges are required when strategy is ranges.';
    }

    const rawTokens = ranges.split(',');
    if (rawTokens.some((token) => token.trim().length === 0)) {
      return 'Ranges format is invalid (empty token). Use: 1-3,5,8-10';
    }

    const single = /^\d+$/;
    const span = /^(\d+)\s*-\s*(\d+)$/;

    for (const rawToken of rawTokens) {
      const token = rawToken.trim();
      if (single.test(token)) {
        const n = Number.parseInt(token, 10);
        if (n <= 0) {
          return `Ranges must be positive: ${token}`;
        }
        continue;
      }

      const match = span.exec(token);
      if (!match) {
        return `Ranges format is invalid: ${token}. Use N or A-B`;
      }

      const start = Number.parseInt(match[1], 10);
      const end = Number.parseInt(match[2], 10);
      if (start <= 0 || end <= 0) {
        return `Ranges must be positive: ${token}`;
      }
      if (start > end) {
        return `Range start must be <= end: ${token}`;
      }
    }

    return '';
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
    const resultMessage = response.result.error
      ? this.mapJobError(response.result.error, response.result.message)
      : response.result.message;
    this.statusMessage = `${response.result.status}: ${resultMessage}`;

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

    const detail = error.detail_code ? ` [${error.detail_code}]` : '';

    switch (error.code) {
      case 'VALIDATION_INVALID_INPUT':
        return `Validation: ${error.message}`;
      case 'RUNTIME_DEP_MISSING':
        return `Runtime dependency missing.${detail} ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Split execution failed.${detail} ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Split execution timeout.${detail} ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Unsupported format.${detail} ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${detail}: ${error.message}`;
    }
  }

  private addInputPath(rawPath: string, refreshPreview = true): void {
    const inputPath = rawPath.trim();
    if (!inputPath) {
      return;
    }

    if (this.selectedInputPaths.includes(inputPath)) {
      return;
    }

    this.selectedInputPaths = [...this.selectedInputPaths, inputPath];
    this.resetPreviewState();
    if (refreshPreview) {
      void this.ensureSourcePreview();
    }
  }

  private async renderSplitPreviewBlock(
    splitKey: string,
    inputPath: string,
    pageRange: string | undefined,
    pageNumber: number,
    requestGeneration: number,
  ): Promise<void> {
    if (this.splitPreviewUrls[splitKey]) {
      return;
    }

    try {
      const pdf = await this.loadPreviewPdf(inputPath);
      if (!pdf || requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      const previewPageNumber = await this.pickPreviewPageNumber(
        pdf,
        this.buildBlockCandidatePageNumbers(pageRange, pageNumber),
        inputPath,
        requestGeneration,
      );
      const targetPage =
        previewPageNumber > 0
          ? previewPageNumber
          : this.pageNumberFromRange(pageRange);
      const previewUrl = await this.renderPageToDataUrl(
        pdf,
        targetPage,
        BLOCK_PREVIEW_MAX_WIDTH_PX,
        inputPath,
      );
      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      this.splitPreviewUrls[splitKey] = previewUrl;
    } catch (err) {
      if (requestGeneration !== this.previewRequestGeneration) {
        return;
      }

      console.warn('[pdf-split.preview] split preview task failed', err);
      this.splitPreviewUrls[splitKey] = this.previewImageDataUrl;
    }
  }

  private buildSplitPreviewBlocks(): SplitPreviewBlock[] {
    const pageCount = this.previewSourcePageCount;
    if (pageCount <= 0) {
      return [];
    }

    const strategy = this.form.controls.strategy.value.trim();
    if (strategy === STRATEGY_EVERY_PAGE) {
      return Array.from({ length: pageCount }, (_, index) => {
        const pageNumber = index + 1;
        return {
          key: `page-${pageNumber}`,
          label: `Page ${pageNumber}`,
          pageRange: `${pageNumber}`,
          pageNumber,
        };
      });
    }

    const ranges = this.parsePreviewRanges(
      this.form.controls.ranges.value.trim(),
    );
    if (!ranges) {
      return [];
    }

    return ranges.map((range, index) => {
      const pageRange =
        range.from === range.to ? `${range.from}` : `${range.from}-${range.to}`;
      return {
        key: `range-${index + 1}-${pageRange}`,
        label: `Block ${index + 1}`,
        pageRange,
        pageNumber: range.from,
      };
    });
  }

  private parsePreviewRanges(
    ranges: string,
  ): Array<{ from: number; to: number }> | null {
    if (!ranges) {
      return null;
    }

    const parsed: Array<{ from: number; to: number }> = [];
    const rawTokens = ranges.split(',');
    const single = /^\d+$/;
    const span = /^(\d+)\s*-\s*(\d+)$/;

    for (const rawToken of rawTokens) {
      const token = rawToken.trim();
      if (!token) {
        return null;
      }

      if (single.test(token)) {
        const value = Number.parseInt(token, 10);
        if (value <= 0) {
          return null;
        }
        parsed.push({ from: value, to: value });
        continue;
      }

      const match = span.exec(token);
      if (!match) {
        return null;
      }

      const from = Number.parseInt(match[1], 10);
      const to = Number.parseInt(match[2], 10);
      if (from <= 0 || to <= 0 || from > to) {
        return null;
      }

      parsed.push({ from, to });
    }

    return parsed;
  }

  private pageNumberFromRange(pageRange: string | undefined): number {
    if (!pageRange) {
      return 1;
    }

    const [startToken] = pageRange.split('-');
    const pageNumber = Number.parseInt(startToken.trim(), 10);
    return Number.isFinite(pageNumber) && pageNumber > 0 ? pageNumber : 1;
  }

  private buildPageNumbers(pageCount: number): number[] {
    return Array.from(
      { length: Math.max(0, pageCount) },
      (_, index) => index + 1,
    );
  }

  private buildBlockCandidatePageNumbers(
    pageRange: string | undefined,
    fallbackPageNumber: number,
  ): number[] {
    if (!pageRange) {
      return [fallbackPageNumber];
    }

    const rangePattern = /^(\d+)(?:\s*-\s*(\d+))?$/;
    const match = rangePattern.exec(pageRange);
    if (!match) {
      return [fallbackPageNumber];
    }

    const from = Number.parseInt(match[1], 10);
    const to = Number.parseInt(match[2] ?? match[1], 10);
    if (
      !Number.isFinite(from) ||
      !Number.isFinite(to) ||
      from <= 0 ||
      to < from
    ) {
      return [fallbackPageNumber];
    }

    const pages: number[] = [];
    for (let pageNumber = from; pageNumber <= to; pageNumber++) {
      pages.push(pageNumber);
    }

    return pages.length > 0 ? pages : [fallbackPageNumber];
  }

  private async pickPreviewPageNumber(
    pdf: PreviewPdfDocument,
    candidatePages: number[],
    sourcePath: string,
    requestGeneration: number,
  ): Promise<number> {
    const pages = candidatePages.filter(
      (pageNumber) => Number.isFinite(pageNumber) && pageNumber > 0,
    );

    if (pages.length === 0) {
      return 1;
    }

    for (const pageNumber of pages) {
      if (requestGeneration !== this.previewRequestGeneration) {
        return pages[0] ?? 1;
      }

      const sampleCanvas = document.createElement('canvas');
      try {
        await this.pdfRenderer.renderPageToCanvasWithFallback(
          pdf,
          pageNumber,
          sampleCanvas,
          PREVIEW_SAMPLE_SCALE,
          sourcePath,
        );
      } catch {
        continue;
      }

      if (this.canvasHasVisibleContent(sampleCanvas)) {
        return pageNumber;
      }
    }

    return pages[0] ?? 1;
  }

  private canvasHasVisibleContent(canvas: HTMLCanvasElement): boolean {
    const context = canvas.getContext('2d');
    if (!context || canvas.width === 0 || canvas.height === 0) {
      return false;
    }

    const { data } = context.getImageData(0, 0, canvas.width, canvas.height);
    let visibleSamples = 0;

    for (let offset = 0; offset < data.length; offset += 16) {
      const alpha = data[offset + 3] ?? 0;
      if (
        alpha > 32 &&
        ((data[offset] ?? 255) < 245 ||
          (data[offset + 1] ?? 255) < 245 ||
          (data[offset + 2] ?? 255) < 245)
      ) {
        visibleSamples++;
        if (visibleSamples >= 2) {
          return true;
        }
      }
    }

    return false;
  }

  private async renderPageToDataUrl(
    pdf: PreviewPdfDocument,
    pageNumber: number,
    maxWidthPx: number,
    sourcePath: string,
  ): Promise<string> {
    const page = await pdf.getPage(pageNumber);
    const baseViewport = page.getViewport({ scale: 1 });
    const scale = Math.min(1.6, maxWidthPx / Math.max(baseViewport.width, 1));
    const viewport = page.getViewport({ scale });
    const canvas = document.createElement('canvas');
    const context = canvas.getContext('2d');
    if (!context) {
      throw new Error('Unable to create preview canvas.');
    }

    canvas.width = Math.max(1, Math.round(viewport.width));
    canvas.height = Math.max(1, Math.round(viewport.height));
    canvas.style.width = `${canvas.width}px`;
    canvas.style.height = `${canvas.height}px`;

    await this.pdfRenderer.renderPageToCanvasWithFallback(
      pdf,
      pageNumber,
      canvas,
      scale,
      sourcePath,
    );
    return canvas.toDataURL('image/png');
  }

  private async loadPreviewPdf(
    inputPath: string,
  ): Promise<PreviewPdfDocument | null> {
    if (this.previewSourcePdf) {
      return this.previewSourcePdf;
    }

    if (!this.previewSourceBytes) {
      const resp = await this.wailsService.getPdfPreviewSource(inputPath);
      if (!resp.success || !resp.dataBase64) {
        return null;
      }

      this.previewSourceBytes = this.base64ToBytes(resp.dataBase64);
    }

    this.previewSourcePdf = await this.pdfRenderer.loadFromBytes(
      this.previewSourceBytes,
    );
    return this.previewSourcePdf;
  }

  private async destroyPreviewPdf(): Promise<void> {
    const previewPdf = this.previewSourcePdf;
    this.previewSourcePdf = null;
    if (!previewPdf) {
      return;
    }

    await this.pdfRenderer.destroy(previewPdf);
  }

  private resetPreviewState(): void {
    this.previewRequestGeneration++;
    this.previewRequested = false;
    this.previewStatus = '';
    this.previewMessage = 'Select a PDF to generate split thumbnails.';
    this.previewImageDataUrl = null;
    this.previewSourceBytes = null;
    this.previewSourcePageCount = 0;
    this.splitPreviewBlocks = [];
    this.splitPreviewUrls = {};
    void this.destroyPreviewPdf();
  }

  private base64ToBytes(dataBase64: string): Uint8Array {
    const binary = globalThis.atob(dataBase64);
    return Uint8Array.from(
      binary,
      (character) => character.codePointAt(0) ?? 0,
    );
  }
}
