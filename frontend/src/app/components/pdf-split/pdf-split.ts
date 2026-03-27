import { CommonModule } from '@angular/common';
import { Component, ElementRef, OnDestroy, ViewChild } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
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

@Component({
  selector: 'app-pdf-split',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './pdf-split.html',
  styleUrl: './pdf-split.css',
})
export class PdfSplit implements OnDestroy {
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
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: [JOB_MODE_BATCH, Validators.required],
      outputDir: ['', Validators.required],
      strategy: [STRATEGY_EVERY_PAGE, Validators.required],
      ranges: [''],
    });
  }

  ngOnDestroy(): void {
    this.stopPolling();
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
      this.addInputPath(rawPath);
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

    for (let i = 0; i < files.length; i++) {
      const file = files.item(i);
      if (!file) {
        continue;
      }
      const filePath = (file as File & { path?: string }).path ?? file.name;
      this.addInputPath(filePath);
    }
    this.validationMessage = `${files.length} PDF input(s) selected.`;
    if (target) {
      target.value = '';
    }
  }

  removeInputPath(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }

    this.selectedInputPaths = this.selectedInputPaths.filter((_, i) => i !== index);
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

      const match = token.match(span);
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

    const response: JobStatusResponseV1 = await this.wailsService.getJobStatusV1(
      this.activeJobId
    );
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
        return `Split execution failed.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Split execution timeout.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Unsupported format.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }

  private addInputPath(rawPath: string): void {
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
