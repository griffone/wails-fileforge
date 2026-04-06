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
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';

const VIDEO_CONVERT_TOOL_ID = 'tool.video.convert';
const POLLING_INTERVAL_MS = 1000;

type JobMode = 'single' | 'batch';
type ActiveJobKind = 'convert';

@Component({
  selector: 'app-video-convert',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './video-convert.html',
  styleUrl: './video-convert.css',
})
export class VideoConvert implements OnDestroy {
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

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
      placeholder: '/path/to/output.mp4',
      helpText: 'Required in single mode.',
      visibleModes: ['single'],
    },
    {
      controlName: 'outputDir',
      label: 'Output directory',
      type: 'text',
      placeholder: '/path/to/output',
      helpText: 'Required in batch mode.',
      visibleModes: ['batch'],
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
  ];

  selectedInputPaths: string[] = [];
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  progressStageLabel = '';
  progressPercentLabel = '0%';
  etaLabel = '—';

  private activeJobKind: ActiveJobKind = 'convert';
  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private pollInFlight = false;
  private terminalStatusSeen = false;
  private unsubscribeProgressEvent: (() => void) | null = null;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: ['single', Validators.required],
      outputPath: ['', Validators.required],
      outputDir: [''],
      targetFormat: ['mp4', Validators.required],
      qualityPreset: ['medium', Validators.required],
    });
  }

  ngOnDestroy(): void {
    this.unsubscribeProgressEvent?.();
    this.stopPolling();
  }

  async selectVideoFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = (await this.wailsService.openFileDialog()).trim();
    if (!selected) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPaths = [selected];
    } else {
      this.addInputPath(selected);
    }
    this.validationMessage = 'Video input selected.';
  }

  async selectMultipleVideosFromDialog(): Promise<void> {
    this.clearMessages();
    const selected = await this.wailsService.openMultipleFilesDialog();
    if (!selected || selected.length === 0) {
      return;
    }

    if (this.currentMode() === 'single') {
      this.selectedInputPaths = [selected[0].trim()];
    } else {
      for (const path of selected) {
        this.addInputPath(path);
      }
    }

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

    if (this.currentMode() === 'single') {
      const file = files.item(0);
      if (!file) {
        return;
      }
      const filePath = (file as File & { path?: string }).path ?? file.name;
      this.selectedInputPaths = [filePath.trim()];
    } else {
      for (let index = 0; index < files.length; index += 1) {
        const file = files.item(index);
        if (!file) {
          continue;
        }
        const filePath = (file as File & { path?: string }).path ?? file.name;
        this.addInputPath(filePath);
      }
    }

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

  clearSelectedInput(): void {
    this.selectedInputPaths = [];
  }

  onModeChanged(): void {
    if (this.currentMode() === 'single' && this.selectedInputPaths.length > 1) {
      this.selectedInputPaths = [this.selectedInputPaths[0]];
    }
  }

  async validate(): Promise<void> {
    this.clearMessages();

    const localError = this.localValidationError();
    if (localError) {
      this.validationMessage = localError;
      return;
    }

    const response = await this.wailsService.validateJobV1(this.buildConvertRequest());
    this.validationMessage = response.valid
      ? 'Validation OK. Ready to run convert.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();

    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
    }

    const request = this.buildConvertRequest();
    this.isSubmitting = true;
    this.statusMessage = '';
    this.jobResult = null;
    this.terminalStatusSeen = false;
    this.activeJobKind = 'convert';

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
    this.submitMessage = `Convert job submitted: ${runResponse.jobId}`;
    this.startPolling();
    this.isSubmitting = false;
  }

  async cancel(): Promise<void> {
    if (!this.activeJobId) {
      return;
    }

    const response = await this.wailsService.cancelJobV1(this.activeJobId);
    this.statusMessage = response.success
      ? `Cancel requested for ${this.activeJobKind} job.`
      : this.mapJobError(response.error, response.message);
  }

  private localValidationError(): string {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();

    if (mode === 'single' && this.selectedInputPaths.length !== 1) {
      return 'Select one input video file.';
    }
    if (mode === 'batch' && this.selectedInputPaths.length < 1) {
      return 'Select at least one input video file.';
    }

    for (const inputPath of this.selectedInputPaths) {
      if (!this.isSupportedVideoInput(inputPath)) {
        return `Input video must be .mp4, .mov or .mkv: ${inputPath}`;
      }
    }

    if (mode === 'single') {
      if (!outputPath) {
        return 'Output path is required.';
      }
      if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
        return 'Output path must include a filename.';
      }
      if (!outputPath.toLowerCase().endsWith(`.${targetFormat}`)) {
        return `Output extension must match target format .${targetFormat}`;
      }
    }

    if (mode === 'batch' && !outputDir) {
      return 'Output directory is required in batch mode.';
    }

    if (targetFormat !== 'mp4' && targetFormat !== 'webm') {
      return `Unsupported targetFormat: ${targetFormat}`;
    }

    if (qualityPreset !== 'high' && qualityPreset !== 'medium' && qualityPreset !== 'low') {
      return `Unsupported qualityPreset: ${qualityPreset}`;
    }

    return '';
  }

  private buildConvertRequest(): JobRequestV1 {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();

    return {
      toolId: VIDEO_CONVERT_TOOL_ID,
      mode,
      inputPaths: [...this.selectedInputPaths],
      outputDir: mode === 'single' ? this.outputDirFromPath(outputPath) : outputDir,
      options: {
        ...(mode === 'single' ? { outputPath } : {}),
        targetFormat,
        qualityPreset,
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
    return lower.endsWith('.mp4') || lower.endsWith('.mov') || lower.endsWith('.mkv');
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling = true;
    this.unsubscribeProgressEvent?.();
    this.unsubscribeProgressEvent = this.wailsService.subscribeJobProgressV1(
      (event: JobProgressEventV1) => {
        this.handleProgressEvent(event);
      }
    );
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
    this.unsubscribeProgressEvent?.();
    this.unsubscribeProgressEvent = null;
  }

  private handleProgressEvent(event: JobProgressEventV1): void {
    if (!this.activeJobId || event.jobId !== this.activeJobId) {
      return;
    }

    const stage = event.progress.stage?.trim();
    const current = event.progress.current;
    const total = event.progress.total;
    const percent =
      total > 0 ? Math.max(0, Math.min(100, Math.round((current / total) * 100))) : 0;
    this.progressStageLabel = stage || event.status || this.progressStageLabel;
    this.progressPercentLabel = `${percent}%`;
    this.etaLabel = this.formatEta(event.progress.etaSeconds);
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
      if (!response) {
        this.statusMessage = 'status polling failed: empty response';
        this.stopPolling();
        this.activeJobId = '';
        return;
      }
      if (!response.found || !response.result) {
        this.statusMessage = this.mapJobError(response.error, response.message);
        this.stopPolling();
        this.activeJobId = '';
        return;
      }

      this.jobResult = response.result;

      this.updateProgressLabels(response.result);
      this.statusMessage = `${this.activeJobKind} ${response.result.status}: ${
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
    this.progressStageLabel = '';
    this.progressPercentLabel = '0%';
    this.etaLabel = '—';
  }

  private updateProgressLabels(result: JobResultV1): void {
    const current = result.progress.current;
    const total = result.progress.total;
    const stage = result.progress.stage || result.status;
    const percent = total > 0 ? Math.max(0, Math.min(100, Math.round((current / total) * 100))) : 0;
    this.progressStageLabel = stage;
    this.progressPercentLabel = `${percent}%`;
    this.etaLabel = this.formatEta(result.progress.etaSeconds);
  }

  private formatEta(etaSeconds: number | undefined): string {
    if (!etaSeconds || etaSeconds <= 0) {
      return '—';
    }

    const minutes = Math.floor(etaSeconds / 60);
    const seconds = etaSeconds % 60;
    if (minutes <= 0) {
      return `${seconds}s`;
    }

    return `${minutes}m ${seconds}s`;
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    switch (error.code) {
      case 'RUNTIME_DEP_MISSING':
        return 'FFmpeg runtime is unavailable. Install/configure FFmpeg and retry.';
      case 'VALIDATION_INVALID_INPUT':
        return `Validation: ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Video convert execution failed: ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Video convert execution timeout: ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Output format mismatch: ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }

  private currentMode(): JobMode {
    return this.form.controls.jobMode.value === 'batch' ? 'batch' : 'single';
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
