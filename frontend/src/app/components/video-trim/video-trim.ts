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

const VIDEO_TRIM_TOOL_ID = 'tool.video.trim';
const VIDEO_MERGE_TOOL_ID = 'tool.video.merge';
const POLLING_INTERVAL_MS = 1000;

type JobMode = 'single' | 'batch';
type ActiveJobKind = 'trim' | 'merge';

@Component({
  selector: 'app-video-trim',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink, ToolExecutionPanel],
  templateUrl: './video-trim.html',
  styleUrl: './video-trim.css',
})
export class VideoTrim implements OnDestroy {
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
      controlName: 'startTime',
      label: 'Start time (seconds)',
      type: 'text',
      placeholder: '0.0',
      helpText: 'Must be >= 0.',
    },
    {
      controlName: 'endTime',
      label: 'End time (seconds)',
      type: 'text',
      placeholder: '10.0',
      helpText: 'Must be > start time.',
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
      controlName: 'trimMode',
      label: 'Trim mode',
      type: 'select',
      options: [
        { value: 'auto', label: 'auto (copy then fallback reencode)' },
        { value: 'copy', label: 'copy (no fallback)' },
        { value: 'reencode', label: 'reencode (direct)' },
      ],
    },
    {
      controlName: 'mergeOutputs',
      label: 'Merge outputs when batch completes',
      type: 'select',
      options: [
        { value: 'no', label: 'no' },
        { value: 'yes', label: 'yes' },
      ],
      visibleModes: ['batch'],
    },
    {
      controlName: 'mergeOutputPath',
      label: 'Merge output path',
      type: 'text',
      placeholder: '/path/to/merged.mp4',
      visibleModes: ['batch'],
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
      visibleModes: ['batch'],
    },
  ];

  selectedInputPaths: string[] = [];
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  mergeChainMessage = '';
  jobResult: JobResultV1 | null = null;
  mergeJobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  progressStageLabel = '';
  progressPercentLabel = '0%';
  fallbackInfoMessage = '';

  private activeJobKind: ActiveJobKind = 'trim';
  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private pollInFlight = false;
  private terminalStatusSeen = false;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      jobMode: ['single', Validators.required],
      outputPath: ['', Validators.required],
      outputDir: [''],
      startTime: ['0', Validators.required],
      endTime: ['10', Validators.required],
      targetFormat: ['mp4', Validators.required],
      qualityPreset: ['medium', Validators.required],
      trimMode: ['auto', Validators.required],
      mergeOutputs: ['no', Validators.required],
      mergeOutputPath: [''],
      mergeMode: ['auto', Validators.required],
    });
  }

  ngOnDestroy(): void {
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

    const response = await this.wailsService.validateJobV1(this.buildTrimRequest());
    this.validationMessage = response.valid
      ? 'Validation OK. Ready to run trim.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();

    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
    }

    const request = this.buildTrimRequest();
    this.isSubmitting = true;
    this.statusMessage = '';
    this.jobResult = null;
    this.mergeJobResult = null;
    this.terminalStatusSeen = false;
    this.activeJobKind = 'trim';

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
    this.submitMessage = `Trim job submitted: ${runResponse.jobId}`;
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
    const startTimeRaw = this.form.controls.startTime.value.trim();
    const endTimeRaw = this.form.controls.endTime.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const trimMode = this.form.controls.trimMode.value.trim();

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

    const startTime = Number(startTimeRaw);
    const endTime = Number(endTimeRaw);
    if (!Number.isFinite(startTime) || !Number.isFinite(endTime)) {
      return 'Start/end time must be numeric values (seconds).';
    }
    if (startTime < 0) {
      return 'Start time must be >= 0.';
    }
    if (endTime <= startTime) {
      return 'End time must be greater than start time.';
    }

    if (targetFormat !== 'mp4' && targetFormat !== 'webm') {
      return `Unsupported targetFormat: ${targetFormat}`;
    }
    if (qualityPreset !== 'high' && qualityPreset !== 'medium' && qualityPreset !== 'low') {
      return `Unsupported qualityPreset: ${qualityPreset}`;
    }
    if (trimMode !== 'auto' && trimMode !== 'copy' && trimMode !== 'reencode') {
      return `Unsupported trimMode: ${trimMode}`;
    }

    if (mode === 'batch' && this.shouldMergeOutputs()) {
      const mergeOutputPath = this.form.controls.mergeOutputPath.value.trim();
      const mergeMode = this.form.controls.mergeMode.value.trim();
      if (!mergeOutputPath) {
        return 'Merge output path is required when merge chaining is enabled.';
      }
      if (!mergeOutputPath.toLowerCase().endsWith(`.${targetFormat}`)) {
        return `Merge output extension must match target format .${targetFormat}`;
      }
      if (mergeMode !== 'auto' && mergeMode !== 'copy' && mergeMode !== 'reencode') {
        return `Unsupported mergeMode: ${mergeMode}`;
      }
    }

    return '';
  }

  private buildTrimRequest(): JobRequestV1 {
    const mode = this.currentMode();
    const outputPath = this.form.controls.outputPath.value.trim();
    const outputDir = this.form.controls.outputDir.value.trim();
    const startTime = Number(this.form.controls.startTime.value.trim());
    const endTime = Number(this.form.controls.endTime.value.trim());
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const trimMode = this.form.controls.trimMode.value.trim();

    return {
      toolId: VIDEO_TRIM_TOOL_ID,
      mode,
      inputPaths: [...this.selectedInputPaths],
      outputDir: mode === 'single' ? this.outputDirFromPath(outputPath) : outputDir,
      options: {
        ...(mode === 'single' ? { outputPath } : {}),
        startTime,
        endTime,
        targetFormat,
        qualityPreset,
        trimMode,
      },
    };
  }

  private buildMergeRequest(mergeInputs: string[]): JobRequestV1 {
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const mergeMode = this.form.controls.mergeMode.value.trim();
    const outputPath = this.form.controls.mergeOutputPath.value.trim();

    return {
      toolId: VIDEO_MERGE_TOOL_ID,
      mode: 'single',
      inputPaths: mergeInputs,
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
    return lower.endsWith('.mp4') || lower.endsWith('.mov') || lower.endsWith('.mkv');
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

      if (this.activeJobKind === 'trim') {
        this.jobResult = response.result;
        this.captureFallbackInfo(response.result);
      } else {
        this.mergeJobResult = response.result;
      }

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

        if (this.activeJobKind === 'trim') {
          await this.handleTrimTerminal(response.result);
        }
      }
    } finally {
      this.pollInFlight = false;
    }
  }

  private async handleTrimTerminal(result: JobResultV1): Promise<void> {
    if (
      this.currentMode() !== 'batch' ||
      !this.shouldMergeOutputs() ||
      result.status !== 'success' ||
      !result.success
    ) {
      return;
    }

    const mergeInputs = this.extractSuccessfulOutputs(result);
    if (mergeInputs.length < 2) {
      this.mergeChainMessage =
        'Merge chain skipped: batch completed but produced fewer than 2 successful outputs.';
      return;
    }

    const mergeRequest = this.buildMergeRequest(mergeInputs);
    const validation = await this.wailsService.validateJobV1(mergeRequest);
    if (!validation.valid) {
      this.mergeChainMessage = `Merge chain validation failed: ${this.mapJobError(
        validation.error,
        validation.message
      )}`;
      return;
    }

    const runResponse = await this.wailsService.runJobV1(mergeRequest);
    if (!runResponse.success || !runResponse.jobId) {
      this.mergeChainMessage = `Merge chain failed to submit: ${this.mapJobError(
        runResponse.error,
        runResponse.message
      )}`;
      return;
    }

    this.activeJobKind = 'merge';
    this.activeJobId = runResponse.jobId;
    this.terminalStatusSeen = false;
    this.mergeChainMessage = `Merge chain started: ${runResponse.jobId}`;
    this.startPolling();
  }

  private extractSuccessfulOutputs(result: JobResultV1): string[] {
    const outputs: string[] = [];
    for (const item of result.items) {
      if (!item.success) {
        continue;
      }
      if (item.outputs && item.outputs.length > 0) {
        outputs.push(...item.outputs);
      }
    }
    return outputs.filter((path) => !!path.trim());
  }

  private clearMessages(): void {
    this.validationMessage = '';
    this.submitMessage = '';
    this.mergeChainMessage = '';
    this.fallbackInfoMessage = '';
    this.progressStageLabel = '';
    this.progressPercentLabel = '0%';
  }

  private updateProgressLabels(result: JobResultV1): void {
    const current = result.progress.current;
    const total = result.progress.total;
    const stage = result.progress.stage || result.status;
    const percent = total > 0 ? Math.max(0, Math.min(100, Math.round((current / total) * 100))) : 0;
    this.progressStageLabel = stage;
    this.progressPercentLabel = `${percent}%`;
  }

  private captureFallbackInfo(result: JobResultV1): void {
    const detailFallback = result.error?.details?.['fallbackStatus'];
    if (detailFallback === 'failed') {
      this.fallbackInfoMessage =
        'Auto fallback was triggered but reencode failed. Check runtime and output conditions.';
      return;
    }

    const source = `${result.message} ${(result.progress?.message ?? '')}`.toLowerCase();
    this.fallbackInfoMessage = source.includes('fallback')
      ? 'Auto fallback used: copy path failed, reencode path completed the trim.'
      : '';
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    switch (error.code) {
      case 'RUNTIME_DEP_MISSING':
        return 'FFmpeg runtime is unavailable. Install/configure FFmpeg and retry.';
      case 'UNSUPPORTED_FORMAT':
        return `Output format mismatch: ${error.message}`;
      case 'VALIDATION_INVALID_INPUT':
        return `Invalid trim time range: ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `Video trim execution failed: ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `Video trim execution timeout: ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'Job was canceled.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }

  private currentMode(): JobMode {
    return this.form.controls.jobMode.value === 'batch' ? 'batch' : 'single';
  }

  private shouldMergeOutputs(): boolean {
    return this.form.controls.mergeOutputs.value === 'yes';
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
