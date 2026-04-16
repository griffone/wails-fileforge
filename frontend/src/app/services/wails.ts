import { Injectable } from '@angular/core';
import { Call, Events } from '@wailsio/runtime';
import { Subject, Subscription, ReplaySubject, Observable } from 'rxjs';

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
  // Job progress Subject / registration tracking to ensure we only register
  // the Wails Events.On handler once and avoid duplicate listeners.
  // We expose a public Observable that replays the last event for late
  // subscribers while keeping the existing subscribeJobProgressV1 API.
  private jobProgressSubject: ReplaySubject<JobProgressEventV1> | null = null;
  private jobProgressUnsubscribe: (() => void) | null = null;
  private jobProgressRefCount = 0;

  // Public observable that consumers can subscribe to. It will ensure the
  // native Events.On handler is registered while there are active
  // subscribers and will replay the last value for late subscribers.
  public readonly jobProgress$: Observable<JobProgressEventV1> = new Observable(
    (subscriber) => {
      // Ensure the shared subject and native listener are registered.
      const unregister = this.ensureJobProgressRegistered();

      // At this point jobProgressSubject is guaranteed to exist.
      const sub = this.jobProgressSubject!.subscribe({
        next: (v) => subscriber.next(v),
        error: (e) => subscriber.error(e),
        complete: () => subscriber.complete(),
      });

      return () => {
        try {
          sub.unsubscribe();
        } catch {
          // ignore
        }
        // Decrement refcount and cleanup if needed
        unregister();
      };
    }
  );
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

  // Frontend wrappers for PDF split feature.
  // NOTE: The Go backend methods are TODOs and must be implemented server-side.
  // We provide typed wrappers here so the UI can call them later. They currently
  // call Call.ByID with placeholder IDs — backend dev must expose the matching
  // bindings and update these numeric IDs accordingly.
  async getPdfBytes(inputPath: string): Promise<PDFPreviewSourceResponseV1> {
    try {
      // TODO: Replace 0 with generated binding ID for backend method that returns PDF bytes base64
      return await this.callByID(0, inputPath);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('getPdfBytes failed', error),
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_PDF_BYTES_FAILED', error),
      };
    }
  }

  async splitPdfBackend(inputPath: string, ranges: Array<{ from: number; to: number }>): Promise<RunJobResponseV1> {
    try {
      // TODO: Replace 0 with generated binding ID for backend split job
      return await this.callByID(0, inputPath, ranges);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('splitPdfBackend failed', error),
        jobId: '',
        status: 'failed',
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_PDF_SPLIT_FAILED', error),
      };
    }
  }

  // Preview service wrappers (StartPreview / GetPreviewStatus / GetPreview / CancelPreview)
  // These wrap generated bindings that call into the backend preview service.
  async StartPreview(req: { path: string; width: number; height: number; format: string }): Promise<any> {
    try {
      // bindings use a generated ID; Call.ByID will resolve to the preview model
      return await this.callByID(1123038030, req);
    } catch (error) {
      return { success: false, message: this.formatMessage('StartPreview failed', error) };
    }
  }

  async GetPreviewStatus(jobID: string): Promise<any> {
    try {
      return await this.callByID(1885578308, jobID);
    } catch (error) {
      return { status: 'failed', progress: 0, message: this.formatMessage('GetPreviewStatus failed', error) };
    }
  }

  async GetPreview(jobID: string): Promise<any> {
    try {
      return await this.callByID(1152353978, jobID);
    } catch (error) {
      return { success: false, message: this.formatMessage('GetPreview failed', error) };
    }
  }

  async CancelPreview(jobID: string): Promise<any> {
    try {
      return await this.callByID(2053488540, jobID);
    } catch (error) {
      return { success: false, message: this.formatMessage('CancelPreview failed', error) };
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
    // Use the public observable internally to keep behavior consistent and
    // backward-compatible. subscribeJobProgressV1 should subscribe to the
    // jobProgress$ Observable and return an unsubscribe function.
    const unregister = this.ensureJobProgressRegistered();

    const subscription: Subscription = this.jobProgressSubject!.subscribe(callback);

    return () => {
      try {
        subscription.unsubscribe();
      } catch {
        // ignore
      }
      // decrement refcount / cleanup
      unregister();
    };
  }

  // Ensure the replay subject exists and the native Events.On handler is
  // registered. Increments an internal refcount and returns an unregister
  // function that should be called when the consumer unsubscribes.
  private ensureJobProgressRegistered(): () => void {
    // Lazy-create subject if necessary
    if (!this.jobProgressSubject) {
      this.jobProgressSubject = new ReplaySubject<JobProgressEventV1>(1);
      // Register the native event listener - store the unsubscribe fn
      this.jobProgressUnsubscribe = Events.On('jobs/progress/v1', (ev: { data?: unknown }) => {
        const payload = ev?.data;
        if (!payload || typeof payload !== 'object') {
          return;
        }

        const candidate = payload as Partial<JobProgressEventV1>;
        if (typeof candidate.jobId !== 'string' || !candidate.progress) {
          return;
        }

        this.jobProgressSubject?.next({
          jobId: candidate.jobId,
          toolId: typeof candidate.toolId === 'string' ? candidate.toolId : '',
          status: typeof candidate.status === 'string' ? candidate.status : 'running',
          progress: candidate.progress,
        });
      });
    }

    this.jobProgressRefCount += 1;

    // Return unregister function
    return () => {
      this.jobProgressRefCount -= 1;
      if (this.jobProgressRefCount <= 0) {
        // cleanup
        try {
          this.jobProgressUnsubscribe?.();
        } catch {
          // ignore
        }
        this.jobProgressUnsubscribe = null;
        try {
          this.jobProgressSubject?.complete();
        } catch {
          // ignore
        }
        this.jobProgressSubject = null;
        this.jobProgressRefCount = 0;
      }
    };
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
