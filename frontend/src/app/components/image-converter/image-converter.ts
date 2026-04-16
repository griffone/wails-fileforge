import { Component, ElementRef, OnDestroy, ViewChild } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import { CommonModule } from '@angular/common';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';

import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from '../tool-execution-panel/tool-execution-panel';
import { FileDrop } from '../file-drop/file-drop';
import { FEATURE_FLAGS } from '../../config/feature-flags';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const IMAGE_TOOL_ID = 'tool.image.convert';
const POLLING_INTERVAL_MS = 1000;

@Component({
  selector: 'app-image-converter',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel, FileDrop],
  templateUrl: './image-converter.html',
  styleUrl: './image-converter.css',
})
export class ImageConverter implements OnDestroy {
  readonly featureFlags = FEATURE_FLAGS;
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  private readonly destroy$ = new Subject<void>();

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
    },
    {
      controlName: 'outputPath',
      label: 'Output path',
      type: 'text',
      placeholder: '/path/to/output.webp',
      visibleModes: ['single'],
    },
    {
      controlName: 'outputDir',
      label: 'Output directory',
      type: 'text',
      placeholder: '/path/to/output',
      visibleModes: ['batch'],
    },
    {
      controlName: 'format',
      label: 'Target format',
      type: 'select',
      options: [
        { value: 'webp', label: 'webp' },
        { value: 'jpeg', label: 'jpeg' },
        { value: 'png', label: 'png' },
        { value: 'gif', label: 'gif' },
      ],
    },
    {
      controlName: 'quality',
      label: 'Quality (1-100)',
      type: 'text',
      placeholder: '80',
    },
    {
      controlName: 'resizeWidth',
      label: 'Resize width (optional)',
      type: 'text',
      placeholder: '1024',
    },
    {
      controlName: 'resizeHeight',
      label: 'Resize height (optional)',
      type: 'text',
      placeholder: '768',
    },
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
      jobMode: ['single', Validators.required],
      outputPath: [''],
      outputDir: [''],
      format: ['webp', Validators.required],
      quality: ['80', Validators.required],
      resizeWidth: [''],
      resizeHeight: [''],
    });

    // Subscribe to job progress events to update JobCard in real-time.
    // Keep subscription hygiene using destroy$.
    this.wailsService.jobProgress$.pipe(takeUntil(this.destroy$)).subscribe((ev) => {
      if (!ev || ev.jobId !== this.activeJobId) return;
      // convert JobProgressEvent into partial JobResultV1 update for UI
      this.jobResult = {
        jobId: ev.jobId,
        success: false,
        message: ev.progress.message,
        toolId: ev.toolId,
        status: ev.status as any,
        progress: ev.progress,
        items: this.jobResult?.items ?? [],
        startedAt: Date.now(),
      };
      // announce stage transitions via aria-live: statusMessage is rendered with role=alert
      this.statusMessage = `${ev.progress.stage}: ${ev.progress.message}`;
      if (this.isTerminalStatus(ev.status)) {
        this.activeJobId = '';
        this.stopPolling();
      }
    });
    // Note: we keep the existing polling loop for backward compatibility with older runtimes
    // and as a fallback when jobProgress$ events may not be delivered.
  }

  appendPaths(paths: string[]): void {
    for (const p of paths) {
      this.addInput(p);
    }
  }

  // New FileDrop file selected handler (accepts File[] and converts to local paths)
  onFileDropFiles(files: File[]): void {
    // Preserve local-only handling: use file.name as placeholder path for now
    // Phase2: implement thumbnail previews via local object URLs or backend preview endpoint
    const paths = files.map((f) => f.path || f.name || f.name);
    this.onFileDropPaths(paths);
  }

  onFileDropPaths(paths: string[]): void {
    if (this.currentMode() === 'single') {
      this.selectedInputPaths = [paths[0]];
      return;
    }

    this.appendPaths(paths);
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.destroy$.next();
    this.destroy$.complete();
  }

  async selectImageFromDialog(): Promise<void> {
    this.clearMessages();
    const path = (await this.wailsService.openFileDialog()).trim();
    if (!path) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPaths = [path];
      return;
    }

    this.addInput(path);
  }

  async selectMultipleImagesFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPaths = [selected[0].trim()];
      return;
    }

    for (const path of selected) {
      this.addInput(path);
    }
  }

  async selectOutputDirectory(): Promise<void> {
    const selected = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selected) {
      return;
    }

    this.form.patchValue({ outputDir: selected });
  }

  removeInput(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.selectedInputPaths = this.selectedInputPaths.filter((_, i) => i !== index);
  }

  clearInputs(): void {
    this.selectedInputPaths = [];
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
      ? 'Validation OK. Ready to run image job.'
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

  private localValidationError(): string {
    const mode = this.currentMode();
    const format = this.form.controls.format.value.trim();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const quality = this.form.controls.quality.value.trim();

    if (mode !== 'single' && mode !== 'batch') {
      return `Unsupported mode: ${mode}`;
    }

    if (mode === 'single' && this.selectedInputPaths.length !== 1) {
      return 'Select exactly one input image for single mode.';
    }
    if (mode === 'batch' && this.selectedInputPaths.length < 1) {
      return 'Select at least one input image for batch mode.';
    }

    for (const inputPath of this.selectedInputPaths) {
      if (!this.isImagePath(inputPath)) {
        return `Input image extension is not supported: ${inputPath}`;
      }
    }

    if (mode === 'single') {
      if (!outputPath) {
        return 'Output path is required in single mode.';
      }
      if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
        return 'Output path must include filename.';
      }
      if (!outputPath.toLowerCase().endsWith(`.${format}`)) {
        return `Output path must end with .${format}`;
      }
    }

    if (mode === 'batch' && !outputDir) {
      return 'Output directory is required in batch mode.';
    }

    const parsedQuality = Number.parseInt(quality, 10);
    if (!Number.isFinite(parsedQuality) || parsedQuality < 1 || parsedQuality > 100) {
      return 'Quality must be an integer between 1 and 100.';
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const format = this.form.controls.format.value.trim();
    const quality = Number.parseInt(this.form.controls.quality.value.trim(), 10);

    const options: Record<string, unknown> = {
      format,
      quality,
    };

    const resizeWidth = this.parseOptionalInt(this.form.controls.resizeWidth.value);
    const resizeHeight = this.parseOptionalInt(this.form.controls.resizeHeight.value);
    if (resizeWidth !== null) {
      options['width'] = resizeWidth;
    }
    if (resizeHeight !== null) {
      options['height'] = resizeHeight;
    }
    if (mode === 'single') {
      options['outputPath'] = outputPath;
    }

    return {
      toolId: IMAGE_TOOL_ID,
      mode,
      inputPaths: [...this.selectedInputPaths],
      outputDir: mode === 'single' ? this.outputDirFromPath(outputPath) : outputDir,
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

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    const tech = error.detail_code ? ` [${error.detail_code}]` : '';

    switch (error.code) {
      case 'VALIDATION_INVALID_INPUT':
        return `Invalid input.${tech} ${error.message}`;
      case 'RUNTIME_DEP_MISSING':
        return `Runtime dependency missing.${tech} ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Execution I/O issue.${tech} ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Execution timeout.${tech} ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Unsupported format.${tech} ${error.message}`;
      case 'CANCELLED_BY_USER':
        return `Job cancelled by user.${tech}`;
      default:
        return `${error.code}${tech}: ${error.message}`;
    }
  }

  private clearMessages(): void {
    this.validationMessage = '';
    this.submitMessage = '';
  }

  private currentMode(): 'single' | 'batch' {
    return this.form.controls.jobMode.value === 'batch' ? 'batch' : 'single';
  }

  private addInput(rawPath: string): void {
    const path = rawPath.trim();
    if (!path || this.selectedInputPaths.includes(path)) {
      return;
    }

    this.selectedInputPaths = [...this.selectedInputPaths, path];
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

  private parseOptionalInt(raw: string): number | null {
    const value = raw.trim();
    if (!value) {
      return null;
    }
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed) || parsed <= 0) {
      return null;
    }
    return parsed;
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
