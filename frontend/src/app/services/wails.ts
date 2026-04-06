import { Injectable } from '@angular/core';
import { Call, Events } from '@wailsio/runtime';

export type ToolRuntimeStatusV1 = 'enabled' | 'disabled' | 'degraded';
export type JobModeV1 = 'single' | 'batch';
export type JobExecutionStatusV1 =
  | 'queued'
  | 'running'
  | 'success'
  | 'failed'
  | 'partial_success'
  | 'cancelled'
  | 'interrupted';

export type CanonicalErrorCodeV1 =
  | 'VALIDATION_INVALID_INPUT'
  | 'RUNTIME_DEP_MISSING'
  | 'EXEC_IO_TRANSIENT'
  | 'EXEC_TIMEOUT_TRANSIENT'
  | 'UNSUPPORTED_FORMAT'
  | 'CANCELLED_BY_USER';

export interface ToolManifestV1 {
  toolId: string;
  name: string;
  description: string;
  domain: string;
  capability: string;
  version: string;
  supportsSingle: boolean;
  supportsBatch: boolean;
  inputExtensions: string[];
  outputExtensions: string[];
  runtimeDependencies: string[];
  tags: string[];
}

export interface ToolRuntimeStateV1 {
  status: ToolRuntimeStatusV1;
  reason?: string;
  healthy: boolean;
}

export interface ToolCatalogEntryV1 {
  manifest: ToolManifestV1;
  state: ToolRuntimeStateV1;
}

export interface ListToolsResponseV1 {
  success: boolean;
  message: string;
  tools: ToolCatalogEntryV1[];
}

export interface JobErrorV1 {
  code: CanonicalErrorCodeV1 | string;
  detail_code?: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface JobRequestV1 {
  toolId: string;
  mode: JobModeV1;
  inputPaths: string[];
  outputDir: string;
  options: Record<string, unknown>;
  workers?: number;
}

export interface JobProgressV1 {
  current: number;
  total: number;
  stage: string;
  message: string;
  etaSeconds?: number;
}

export interface JobProgressEventV1 {
  jobId: string;
  toolId: string;
  status: JobExecutionStatusV1 | string;
  progress: JobProgressV1;
}

export interface JobResultItemV1 {
  inputPath: string;
  outputPath: string;
  outputs?: string[];
  outputCount?: number;
  attempts?: number;
  retryCount?: number;
  success: boolean;
  message: string;
  error?: JobErrorV1;
}

export interface JobResultV1 {
  jobId: string;
  success: boolean;
  message: string;
  toolId: string;
  status: JobExecutionStatusV1;
  progress: JobProgressV1;
  items: JobResultItemV1[];
  error?: JobErrorV1;
  startedAt: number;
  endedAt?: number;
}

export interface ValidateJobResponseV1 {
  success: boolean;
  message: string;
  valid: boolean;
  error?: JobErrorV1;
}

export interface RunJobResponseV1 {
  success: boolean;
  message: string;
  jobId: string;
  status: JobExecutionStatusV1;
  error?: JobErrorV1;
}

export interface CancelJobResponseV1 {
  success: boolean;
  message: string;
  jobId: string;
  error?: JobErrorV1;
}

export interface JobStatusResponseV1 {
  success: boolean;
  message: string;
  found: boolean;
  result?: JobResultV1;
  error?: JobErrorV1;
}

export interface PDFPreviewSourceResponseV1 {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  error?: JobErrorV1;
}

export interface ImagePreviewSourceResponseV1 {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobErrorV1;
}

export interface ImageCropPreviewRequestV1 {
  inputPath: string;
  x: number;
  y: number;
  width: number;
  height: number;
  ratioPreset?: string;
  format?: string;
}

export interface ImageCropPreviewResponseV1 {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobErrorV1;
}

export type ImageAnnotateOperationTypeV1 = 'text' | 'arrow' | 'rect' | 'blur' | 'redact';

export interface ImageAnnotateOperationV1 {
  type: ImageAnnotateOperationTypeV1;
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  x2?: number;
  y2?: number;
  text?: string;
  color?: string;
  opacity?: number;
  strokeWidth?: number;
  fontSize?: number;
  blurIntensity?: number;
}

export interface ImageAnnotatePreviewRequestV1 {
  inputPath: string;
  operations: ImageAnnotateOperationV1[];
  format?: string;
}

export interface ImageAnnotatePreviewResponseV1 {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobErrorV1;
}

export interface HeaderFooterConfigV1 {
  enabled: boolean;
  text: string;
  align: 'left' | 'center' | 'right';
  font: 'helvetica' | 'times' | 'courier';
  marginTop: number;
  marginBottom: number;
  color: string;
}

export interface MarkdownToPdfOptionsV1 {
  outputPath?: string;
  header: HeaderFooterConfigV1;
  footer: HeaderFooterConfigV1;
}

@Injectable({
  providedIn: 'root',
})
export class Wails {
  private callByID(id: number, ...args: unknown[]): Promise<any> {
    return Call.ByID(id, ...args);
  }

  async listToolsV1(): Promise<ListToolsResponseV1> {
    try {
      return await this.callByID(517184612);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Failed to list tools', error),
        tools: [],
      };
    }
  }

  async validateJobV1(request: JobRequestV1): Promise<ValidateJobResponseV1> {
    try {
      return await this.callByID(1505194326, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Validation call failed', error),
        valid: false,
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_VALIDATE_FAILED', error),
      };
    }
  }

  async runJobV1(request: JobRequestV1): Promise<RunJobResponseV1> {
    try {
      return await this.callByID(162380599, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Run job failed', error),
        jobId: '',
        status: 'failed',
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_RUN_FAILED', error),
      };
    }
  }

  async cancelJobV1(jobId: string): Promise<CancelJobResponseV1> {
    try {
      return await this.callByID(3378080914, jobId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Cancel job failed', error),
        jobId,
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_CANCEL_FAILED', error),
      };
    }
  }

  async getJobStatusV1(jobId: string): Promise<JobStatusResponseV1> {
    try {
      return await this.callByID(1961277890, jobId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get job status failed', error),
        found: false,
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_STATUS_FAILED', error),
      };
    }
  }

  async getPdfPreviewSource(inputPath: string): Promise<PDFPreviewSourceResponseV1> {
    try {
      return await this.callByID(632882064, inputPath);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get PDF preview source failed', error),
        error: this.defaultError('EXEC_IO_TRANSIENT', 'PDF_PREVIEW_READ_FAILED', error),
      };
    }
  }

  async getImagePreviewSourceV1(inputPath: string): Promise<ImagePreviewSourceResponseV1> {
    try {
      return await this.callByID(2617279209, inputPath);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image preview source failed', error),
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IMAGE_PREVIEW_READ_FAILED', error),
      };
    }
  }

  async getImageCropPreviewV1(
    request: ImageCropPreviewRequestV1
  ): Promise<ImageCropPreviewResponseV1> {
    try {
      return await this.callByID(4006508154, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image crop preview failed', error),
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IMAGE_CROP_PREVIEW_EXECUTION', error),
      };
    }
  }

  async getImageAnnotatePreviewV1(
    request: ImageAnnotatePreviewRequestV1
  ): Promise<ImageAnnotatePreviewResponseV1> {
    try {
      return await this.callByID(372302096, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image annotate preview failed', error),
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IMAGE_ANNOTATE_PREVIEW_EXECUTION', error),
      };
    }
  }

  async openFileDialog(): Promise<string> {
    try {
      return (await this.callByID(2246342916)) ?? '';
    } catch {
      return '';
    }
  }

  async openMultipleFilesDialog(): Promise<string[]> {
    try {
      return (await this.callByID(3029276213)) ?? [];
    } catch {
      return [];
    }
  }

  async openDirectoryDialog(): Promise<string> {
    try {
      return (await this.callByID(261105429)) ?? '';
    } catch {
      return '';
    }
  }

  subscribeJobProgressV1(callback: (event: JobProgressEventV1) => void): () => void {
    return Events.On('jobs/progress/v1', (ev: { data?: unknown }) => {
      const payload = ev?.data;
      if (!payload || typeof payload !== 'object') {
        return;
      }

      const candidate = payload as Partial<JobProgressEventV1>;
      if (typeof candidate.jobId !== 'string' || !candidate.progress) {
        return;
      }

      callback({
        jobId: candidate.jobId,
        toolId: typeof candidate.toolId === 'string' ? candidate.toolId : '',
        status: typeof candidate.status === 'string' ? candidate.status : 'running',
        progress: candidate.progress,
      });
    });
  }

  isRuntimeAvailable(): boolean {
    return (
      typeof window !== 'undefined' &&
      (window as any)._wails &&
      typeof Call?.ByID === 'function'
    );
  }

  private formatMessage(prefix: string, error: unknown): string {
    return `${prefix}: ${error instanceof Error ? error.message : 'Unknown error'}`;
  }

  private defaultError(
    code: CanonicalErrorCodeV1,
    detailCode: string,
    error: unknown
  ): JobErrorV1 {
    return {
      code,
      detail_code: detailCode,
      message: error instanceof Error ? error.message : 'Unknown error',
    };
  }
}
