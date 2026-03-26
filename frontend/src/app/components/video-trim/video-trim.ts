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
const POLLING_INTERVAL_MS = 1000;

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
      controlName: 'outputPath',
      label: 'Output path',
      type: 'text',
      placeholder: '/path/to/output.mp4',
      helpText: 'Required. Must match selected target format extension.',
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
      helpText:
        'auto tries stream copy first and falls back to reencode only on eligible failures.',
    },
  ];

  selectedInputPath = '';
  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  progressStageLabel = '';
  progressPercentLabel = '0%';
  fallbackInfoMessage = '';

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private pollInFlight = false;
  private terminalStatusSeen = false;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      outputPath: ['', Validators.required],
      startTime: ['0', Validators.required],
      endTime: ['10', Validators.required],
      targetFormat: ['mp4', Validators.required],
      qualityPreset: ['medium', Validators.required],
      trimMode: ['auto', Validators.required],
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

    this.selectedInputPath = selected;
    this.validationMessage = 'Video input selected.';
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

    const file = files.item(0);
    if (!file) {
      return;
    }

    const filePath = (file as File & { path?: string }).path ?? file.name;
    this.selectedInputPath = filePath.trim();
    this.validationMessage = 'Video input selected.';
    if (target) {
      target.value = '';
    }
  }

  clearSelectedInput(): void {
    this.selectedInputPath = '';
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
    const inputPath = this.selectedInputPath.trim();
    const outputPath = this.form.controls.outputPath.value.trim();
    const startTimeRaw = this.form.controls.startTime.value.trim();
    const endTimeRaw = this.form.controls.endTime.value.trim();
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const trimMode = this.form.controls.trimMode.value.trim();

    if (!inputPath) {
      return 'Select one input video file.';
    }
    if (!this.isSupportedVideoInput(inputPath)) {
      return `Input video must be .mp4, .mov or .mkv: ${inputPath}`;
    }

    if (!outputPath) {
      return 'Output path is required.';
    }
    if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
      return 'Output path must include a filename.';
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

    if (!outputPath.toLowerCase().endsWith(`.${targetFormat}`)) {
      return `Output extension must match target format .${targetFormat}`;
    }

    if (qualityPreset !== 'high' && qualityPreset !== 'medium' && qualityPreset !== 'low') {
      return `Unsupported qualityPreset: ${qualityPreset}`;
    }

    if (trimMode !== 'auto' && trimMode !== 'copy' && trimMode !== 'reencode') {
      return `Unsupported trimMode: ${trimMode}`;
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const outputPath = this.form.controls.outputPath.value.trim();
    const startTime = Number(this.form.controls.startTime.value.trim());
    const endTime = Number(this.form.controls.endTime.value.trim());
    const targetFormat = this.form.controls.targetFormat.value.trim();
    const qualityPreset = this.form.controls.qualityPreset.value.trim();
    const trimMode = this.form.controls.trimMode.value.trim();

    return {
      toolId: VIDEO_TRIM_TOOL_ID,
      mode: 'single',
      inputPaths: [this.selectedInputPath.trim()],
      outputDir: this.outputDirFromPath(outputPath),
      options: {
        outputPath,
        startTime,
        endTime,
        targetFormat,
        qualityPreset,
        trimMode,
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
      if (!response.found || !response.result) {
        this.statusMessage = this.mapJobError(response.error, response.message);
        this.stopPolling();
        this.activeJobId = '';
        return;
      }

      this.jobResult = response.result;
      this.updateProgressLabels(response.result);
      this.captureFallbackInfo(response.result);
      this.statusMessage = `${response.result.status}: ${
        response.result.error
          ? this.mapJobError(response.result.error, response.result.message)
          : response.result.message
      }`;

      if (
        response.result.status === 'completed' ||
        response.result.status === 'failed' ||
        response.result.status === 'canceled'
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
      case 'VIDEO_RUNTIME_UNAVAILABLE':
      case 'VIDEO_RUNTIME_FFMPEG_NOT_FOUND':
      case 'VIDEO_RUNTIME_FFPROBE_NOT_FOUND':
        return 'FFmpeg runtime is unavailable. Install/configure FFmpeg and retry.';
      case 'VIDEO_TRIM_FORMAT_MISMATCH':
        return `Output format mismatch: ${error.message}`;
      case 'VIDEO_TRIM_INVALID_TIME_RANGE':
        return `Invalid trim time range: ${error.message}`;
      case 'VIDEO_TRIM_OUTPUT_EXISTS':
        return `Output file already exists. Choose another output path: ${error.message}`;
      case 'VIDEO_TRIM_OUTPUT_COLLIDES_INPUT':
        return 'Output path collides with input path. Use a different file name/location.';
      case 'VIDEO_TRIM_OUTPUT_DIR_NOT_FOUND':
        return 'Output directory does not exist. Create it or choose an existing folder.';
      case 'VIDEO_TRIM_OUTPUT_DIR_NOT_DIRECTORY':
        return 'Output parent path is not a directory. Choose a valid folder.';
      case 'VIDEO_TRIM_OUTPUT_DIR_NOT_WRITABLE':
        return 'Output directory is not writable. Check permissions and retry.';
      case 'VIDEO_TRIM_VALIDATION_ERROR':
      case 'VALIDATION_ERROR':
        return `Validation: ${error.message}`;
      case 'VIDEO_TRIM_INPUT_OPEN_FAILED':
        return 'FFmpeg could not read input file. Verify the file exists and is readable.';
      case 'VIDEO_TRIM_OUTPUT_WRITE_FAILED':
        return 'FFmpeg could not write output file. Check output permissions and free disk space.';
      case 'VIDEO_TRIM_CODEC_UNAVAILABLE':
        return 'Requested codec is unavailable in current FFmpeg build. Try another target format/preset.';
      case 'VIDEO_TRIM_MODE_INVALID':
        return 'Trim mode is invalid. Use auto, copy, or reencode.';
      case 'VIDEO_TRIM_COPY_FAILED':
        return 'Trim copy mode failed. Use reencode mode or auto fallback.';
      case 'VIDEO_TRIM_AUTO_FALLBACK_REENCODE_FAILED':
        return 'Auto mode failed after fallback reencode attempt. Check ffmpeg/runtime/output constraints.';
      case 'VIDEO_TRIM_EXECUTION_FAILED':
        return `Video trim execution failed: ${error.message}`;
      case 'CANCELED':
        return 'Job was canceled.';
      case 'TOOL_NOT_FOUND':
        return 'Video Trim tool is not available in backend.';
      case 'NOT_FOUND':
        return 'Job not found in runtime.';
      default:
        return `${error.code}: ${error.message}`;
    }
  }
}
