import { CommonModule } from '@angular/common';
import { Component, OnDestroy } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const TOOL_ID = 'tool.doc.docx_to_pdf';
const POLLING_INTERVAL_MS = 1000;
const STANDARD_FIDELITY_WARNING =
  'Esta conversión usa fidelidad estándar. En documentos complejos puede haber diferencias de diseño (tablas, fuentes, espaciado). ¿Querés continuar?';

type JobMode = 'single' | 'batch';

@Component({
  selector: 'app-doc-docx-to-pdf',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink],
  templateUrl: './doc-docx-to-pdf.html',
  styleUrl: './doc-docx-to-pdf.css',
})
export class DocDocxToPdf implements OnDestroy {
  readonly form;

  selectedInputPaths: string[] = [];

  validationMessage = '';
  submitMessage = '';
  statusMessage = '';

  activeJobId = '';
  isSubmitting = false;
  isPolling = false;
  jobResult: JobResultV1 | null = null;

  private pollingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      mode: ['single' as JobMode, Validators.required],
      inputPath: [''],
      outputDir: [''],
      outputPath: [''],
    });
  }

  ngOnDestroy(): void {
    this.stopPolling();
  }

  currentMode(): JobMode {
    return this.form.controls.mode.value === 'batch' ? 'batch' : 'single';
  }

  onModeChanged(): void {
    this.clearMessages();
    this.jobResult = null;
    if (this.currentMode() === 'single') {
      if (this.selectedInputPaths.length > 0) {
        this.form.patchValue({ inputPath: this.selectedInputPaths[0] });
      }
      this.selectedInputPaths = [];
    }
  }

  async selectDocxFromDialog(): Promise<void> {
    const selected = (await this.wailsService.openFileDialog()).trim();
    if (!selected) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.form.patchValue({ inputPath: selected });
      return;
    }

    this.addBatchInput(selected);
  }

  async selectMultipleDocxFromDialog(): Promise<void> {
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.form.patchValue({ inputPath: selected[0].trim() });
      return;
    }

    for (const path of selected) {
      this.addBatchInput(path);
    }
  }

  async selectOutputDirectory(): Promise<void> {
    const selectedDir = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selectedDir) {
      return;
    }
    this.form.patchValue({ outputDir: selectedDir });
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
      ? 'Validation OK. Ready to run DOCX → PDF.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();
    this.jobResult = null;

    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
    }

    if (!window.confirm(STANDARD_FIDELITY_WARNING)) {
      this.submitMessage = 'Execution cancelled by user before submit.';
      return;
    }

    this.isSubmitting = true;
    const request = this.buildRequest();

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

  removeBatchInput(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.selectedInputPaths = this.selectedInputPaths.filter((_, i) => i !== index);
  }

  private addBatchInput(rawPath: string): void {
    const path = rawPath.trim();
    if (!path) {
      return;
    }
    if (!this.selectedInputPaths.includes(path)) {
      this.selectedInputPaths = [...this.selectedInputPaths, path];
    }
  }

  private localValidationError(): string {
    const mode = this.currentMode();

    if (mode === 'single') {
      const inputPath = this.form.controls.inputPath.value.trim();
      if (!inputPath) {
        return 'inputPath is required in single mode.';
      }
      if (!inputPath.toLowerCase().endsWith('.docx')) {
        return 'v1 supports only .docx input.';
      }
      const outputPath = this.form.controls.outputPath.value.trim();
      if (outputPath && !outputPath.toLowerCase().endsWith('.pdf')) {
        return 'outputPath must end in .pdf.';
      }
      return '';
    }

    if (this.selectedInputPaths.length < 1) {
      return 'Select at least one .docx file in batch mode.';
    }
    for (const inputPath of this.selectedInputPaths) {
      if (!inputPath.toLowerCase().endsWith('.docx')) {
        return `Unsupported batch input (only .docx): ${inputPath}`;
      }
    }

    const outputDir = this.form.controls.outputDir.value.trim();
    if (!outputDir) {
      return 'outputDir is required in batch mode.';
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const mode = this.currentMode();

    if (mode === 'single') {
      return {
        toolId: TOOL_ID,
        mode,
        inputPaths: [this.form.controls.inputPath.value.trim()],
        outputDir: this.form.controls.outputDir.value.trim(),
        options: {
          outputPath: this.form.controls.outputPath.value.trim(),
        },
      };
    }

    return {
      toolId: TOOL_ID,
      mode,
      inputPaths: [...this.selectedInputPaths],
      outputDir: this.form.controls.outputDir.value.trim(),
      options: {},
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
    this.statusMessage = `${response.result.status}: ${response.result.message}`;

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
    return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
  }
}
