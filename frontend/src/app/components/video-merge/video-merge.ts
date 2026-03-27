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

const VIDEO_MERGE_TOOL_ID = 'tool.video.merge';
const POLLING_INTERVAL_MS = 1000;

@Component({
  selector: 'app-video-merge',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './video-merge.html',
  styleUrl: './video-merge.css',
})
export class VideoMerge implements OnDestroy {
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  readonly form;
  readonly panelFields: ExecutionPanelField[] = [
    {
      controlName: 'outputPath',
      label: 'Output path',
      type: 'text',
      placeholder: '/path/to/output.mp4',
      helpText: 'Required. Must match selected target format extension.',
    },
    {
      controlName: 'targetFormat',
      label: 'Target format',
      type: 'select',
      options: [
        { value: 'mp4', label: 'mp4' },
        { value: 'webm', label: 'webm' },
      ],
    },
    {
      controlName: 'qualityPreset',
      label: 'Quality preset',
      type: 'select',
      options: [
        { value: 'high', label: 'high' },
        { value: 'medium', label: 'medium' },
        { value: 'low', label: 'low' },
      ],
    },
    {
      controlName: 'mergeMode',
      label: 'Merge mode',
      type: 'select',
      options: [
        { value: 'auto', label: 'auto (copy then fallback reencode)' },
        { value: 'copy', label: 'copy (no fallback)' },
        { value: 'reencode', label: 'reencode (direct)' },
      ],
      helpText:
        'auto tries concat copy first and falls back to reencode only on eligible failures.',
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
  private pollInFlight = false;
  private terminalStatusSeen = false;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      outputPath: ['', Validators.required],
      targetFormat: ['mp4', Validators.required],
      qualityPreset: ['medium', Validators.required],
      mergeMode: ['auto', Validators.required],
    });
  }

  ngOnDestroy(): void {
    this.stopPolling();
  }

  async selectVideosFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    this.selectedInputPaths = selected
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    this.validationMessage = `Selected ${this.selectedInputPaths.length} video input(s).`;
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

    const next: string[] = [];
    for (let index = 0; index < files.length; index += 1) {
      const file = files.item(index);
      if (!file) {
        continue;
      }
      const path = (file as File & { path?: string }).path ?? file.name;
      const trimmed = path.trim();
      if (trimmed) {
        next.push(trimmed);
      }
    }

    this.selectedInputPaths = next;
    this.validationMessage = `Selected ${this.selectedInputPaths.length} video input(s).`;
    if (target) {
      target.value = '';
    }
  }

  removeInput(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.selectedInputPaths = this.selectedInputPaths.filter((_, i) => i !== index);
  }

  moveInputUp(index: number): void {
    if (index <= 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    const next = [...this.selectedInputPaths];
    const tmp = next[index - 1];
    next[index - 1] = next[index];
    next[index] = tmp;
    this.selectedInputPaths = next;
  }

  moveInputDown(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length - 1) {
      return;
    }
    const next = [...this.selectedInputPaths];
    const tmp = next[index + 1];
    next[index + 1] = next[index];
    next[index] = tmp;
    this.selectedInputPaths = next;
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
      ? 'Validation OK. Ready to run merge.'
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
    this.terminalStatusSeen = false;

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
    const outputPath = this.form.controls.outputPath.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const mergeMode = this.form.controls.mergeMode.value.trim();

    if (this.selectedInputPaths.length < 2) {
      return 'Select at least 2 input video files in the desired order.';
    }

    for (const inputPath of this.selectedInputPaths) {
      if (!this.isSupportedVideoInput(inputPath)) {
        return `Input video must be .mp4, .mov, .mkv or .webm: ${inputPath}`;
      }
    }

    if (!outputPath) {
      return 'Output path is required.';
    }
    if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
      return 'Output path must include a filename.';
    }

    if (targetFormat !== 'mp4' && targetFormat !== 'webm') {
      return `Unsupported targetFormat: ${targetFormat}`;
    }

    if (!outputPath.toLowerCase().endsWith(`.${targetFormat}`)) {
      return `Output extension must match target format .${targetFormat}`;
    }

    if (qualityPreset !== 'high' && qualityPreset !== 'medium' && qualityPreset !== 'low') {
      return `Unsupported qualityPreset: ${qualityPreset}`;
    }

    if (mergeMode !== 'auto' && mergeMode !== 'copy' && mergeMode !== 'reencode') {
      return `Unsupported mergeMode: ${mergeMode}`;
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const outputPath = this.form.controls.outputPath.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const mergeMode = this.form.controls.mergeMode.value.trim();

    return {
      toolId: VIDEO_MERGE_TOOL_ID,
      mode: 'single',
      inputPaths: [...this.selectedInputPaths],
      outputDir: this.outputDirFromPath(outputPath),
      options: {
        outputPath,
        targetFormat,
        qualityPreset,
        mergeMode,
      },
    };
  }

  private outputDirFromPath(outputPath: string): string {
    const lastSlash = Math.max(outputPath.lastIndexOf('/'), outputPath.lastIndexOf('\\'));
    if (lastSlash <= 0) {
      return '';
    }

    return outputPath.slice(0, lastSlash);
  }

  private isSupportedVideoInput(path: string): boolean {
    const lower = path.toLowerCase();
    return (
      lower.endsWith('.mp4') ||
      lower.endsWith('.mov') ||
      lower.endsWith('.mkv') ||
      lower.endsWith('.webm')
    );
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling = true;
    this.pollInFlight = false;
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
    this.pollInFlight = false;
  }

  private async pollJobStatus(): Promise<void> {
    if (!this.activeJobId) {
      this.stopPolling();
      return;
    }

    if (this.terminalStatusSeen || this.pollInFlight) {
      return;
    }

    this.pollInFlight = true;

    try {
      const response: JobStatusResponseV1 = await this.wailsService.getJobStatusV1(
        this.activeJobId
      );
      if (!response.found || !response.result) {
        this.statusMessage = this.mapJobError(response.error, response.message);
        this.stopPolling();
        this.activeJobId = '';
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
        this.terminalStatusSeen = true;
        this.stopPolling();
        this.activeJobId = '';
      }
    } finally {
      this.pollInFlight = false;
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
      case 'RUNTIME_DEP_MISSING':
        return 'FFmpeg runtime is unavailable. Install/configure FFmpeg and retry.';
      case 'VALIDATION_INVALID_INPUT':
        return 'At least 2 input videos are required to merge.';
      case 'UNSUPPORTED_FORMAT':
        return `Output format mismatch: ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Video merge execution failed: ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Video merge execution timeout: ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }
}
