import { Component, OnDestroy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const TOOL_ID = 'tool.doc.md_to_pdf';
const POLLING_INTERVAL_MS = 1000;

@Component({
  selector: 'app-doc-md-to-pdf',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink],
  templateUrl: './doc-md-to-pdf.html',
  styleUrl: './doc-md-to-pdf.css',
})
export class DocMdToPdf implements OnDestroy {
  readonly form;

  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  previewNotice = 'Preview is approximate and not pixel-perfect with final PDF.';

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
      inputPath: ['', Validators.required],
      outputDir: [''],
      outputPath: [''],
      header: this.createHeaderFooterGroup(),
      footer: this.createHeaderFooterGroup(),
    });
  }

  ngOnDestroy(): void {
    this.stopPolling();
  }

  private createHeaderFooterGroup() {
    return this.fb.nonNullable.group({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    });
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
      ? 'Validation OK. Ready to run Markdown → PDF.'
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

    const request = this.buildRequest();
    this.isSubmitting = true;

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

  async selectMarkdownFromDialog(): Promise<void> {
    const selectedPath = (await this.wailsService.openFileDialog()).trim();
    if (!selectedPath) {
      return;
    }
    this.form.patchValue({ inputPath: selectedPath });
  }

  async selectOutputDirectory(): Promise<void> {
    const selectedDir = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selectedDir) {
      return;
    }

    this.form.patchValue({ outputDir: selectedDir });
  }

  previewHeaderText(): string {
    return this.previewBlockText('header');
  }

  previewFooterText(): string {
    return this.previewBlockText('footer');
  }

  previewAlign(role: 'header' | 'footer'): string {
    const value = role === 'header' ? this.form.controls.header.controls.align.value : this.form.controls.footer.controls.align.value;
    return value;
  }

  previewStyle(role: 'header' | 'footer'): Record<string, string> {
    const group = role === 'header' ? this.form.controls.header.controls : this.form.controls.footer.controls;
    return {
      color: group.color.value,
      fontFamily: this.previewFont(group.font.value),
      textAlign: group.align.value,
      marginTop: `${group.marginTop.value}px`,
      marginBottom: `${group.marginBottom.value}px`,
    };
  }

  private previewFont(font: string): string {
    switch (font) {
      case 'times':
        return 'Times New Roman, serif';
      case 'courier':
        return 'Courier New, monospace';
      default:
        return 'Helvetica, Arial, sans-serif';
    }
  }

  private previewBlockText(role: 'header' | 'footer'): string {
    const text = role === 'header' ? this.form.controls.header.controls.text.value : this.form.controls.footer.controls.text.value;
    const now = new Date();
    const date = `${now.getFullYear()}-${`${now.getMonth() + 1}`.padStart(2, '0')}-${`${now.getDate()}`.padStart(2, '0')}`;
    const fileName = this.fileNameFromPath(this.form.controls.inputPath.value);

    return text
      .replaceAll('{page}', '1')
      .replaceAll('{totalPages}', '?')
      .replaceAll('{date}', date)
      .replaceAll('{fileName}', fileName || 'document.md');
  }

  private fileNameFromPath(path: string): string {
    const normalized = path.replace(/\\/g, '/');
    const idx = normalized.lastIndexOf('/');
    return idx >= 0 ? normalized.slice(idx + 1) : normalized;
  }

  private localValidationError(): string {
    const inputPath = this.form.controls.inputPath.value.trim();
    if (!inputPath) {
      return 'inputPath is required.';
    }

    if (!inputPath.toLowerCase().endsWith('.md') && !inputPath.toLowerCase().endsWith('.markdown')) {
      return 'inputPath must be .md or .markdown.';
    }

    const outputPath = this.form.controls.outputPath.value.trim();
    if (outputPath && !outputPath.toLowerCase().endsWith('.pdf')) {
      return 'outputPath must end in .pdf.';
    }

    const headerError = this.validateHeaderFooterBlock('header');
    if (headerError) {
      return headerError;
    }

    const footerError = this.validateHeaderFooterBlock('footer');
    if (footerError) {
      return footerError;
    }

    return '';
  }

  private validateHeaderFooterBlock(role: 'header' | 'footer'): string {
    const group = role === 'header' ? this.form.controls.header.controls : this.form.controls.footer.controls;
    const blockName = role;

    if (!['left', 'center', 'right'].includes(group.align.value)) {
      return `${blockName}.align must be left/center/right.`;
    }
    if (!['helvetica', 'times', 'courier'].includes(group.font.value)) {
      return `${blockName}.font must be helvetica/times/courier.`;
    }
    if (!/^#[0-9a-fA-F]{6}$/.test(group.color.value)) {
      return `${blockName}.color must use #RRGGBB.`;
    }
    if (group.marginTop.value < 0 || group.marginBottom.value < 0) {
      return `${blockName}.marginTop/marginBottom must be >= 0.`;
    }

    const placeholders = group.text.value.match(/\{[^}]+\}/g) ?? [];
    for (const ph of placeholders) {
      if (!['{page}', '{totalPages}', '{date}', '{fileName}'].includes(ph)) {
        return `${blockName}.text has unsupported placeholder: ${ph}`;
      }
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const header = this.form.controls.header.controls;
    const footer = this.form.controls.footer.controls;

    return {
      toolId: TOOL_ID,
      mode: 'single',
      inputPaths: [this.form.controls.inputPath.value.trim()],
      outputDir: this.form.controls.outputDir.value.trim(),
      options: {
        outputPath: this.form.controls.outputPath.value.trim(),
        header: {
          enabled: header.enabled.value,
          text: header.text.value,
          align: header.align.value,
          font: header.font.value,
          marginTop: Number(header.marginTop.value),
          marginBottom: Number(header.marginBottom.value),
          color: header.color.value,
        },
        footer: {
          enabled: footer.enabled.value,
          text: footer.text.value,
          align: footer.align.value,
          font: footer.font.value,
          marginTop: Number(footer.marginTop.value),
          marginBottom: Number(footer.marginBottom.value),
          color: footer.color.value,
        },
      },
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
