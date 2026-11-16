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

export interface PlannedOutputV1 {
  inputPath: string;
  outputPath: string;
  outputKind: string;
  /** Known file count for directory outputs; omitted when unknown. */
  outputCount?: number;
  exists: boolean;
  adjusted: boolean;
  warnings?: string[];
}

export interface PlanJobResponseV1 {
  success: boolean;
  message: string;
  outputs: PlannedOutputV1[];
  effectiveOptions?: Record<string, unknown>;
  warnings?: string[];
  error?: JobErrorV1;
}

export interface FileInfoV1 {
  path: string;
  name: string;
  directory: string;
  extension: string;
  sizeBytes: number;
  modifiedAt?: number;
  mimeType?: string;
  kind: string;
  width?: number;
  height?: number;
  pageCount?: number;
  pageWidthPoints?: number;
  pageHeightPoints?: number;
  durationSeconds?: number;
  exists: boolean;
  error?: string;
}

export interface InspectFilesResponseV1 {
  success: boolean;
  message: string;
  files: FileInfoV1[];
}

export interface PathActionResponseV1 {
  success: boolean;
  message: string;
}

export interface MediaSourceResponseV1 {
  success: boolean;
  url: string;
  fallbackUrl?: string;
  message: string;
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

export type ImageAnnotateOperationTypeV1 =
  | 'text'
  | 'arrow'
  | 'rect'
  | 'blur'
  | 'redact';

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

export interface VerificationCheckV1 {
  key: string;
  label: string;
  passed: boolean;
  value?: string;
  detail?: string;
}

export interface VerifiedOutputV1 {
  path: string;
  exists: boolean;
  sizeBytes: number;
  kind?: string;
  checks: VerificationCheckV1[];
  warnings?: string[];
}

export interface VerificationReportV1 {
  success: boolean;
  message: string;
  outputs: VerifiedOutputV1[];
  warnings?: string[];
}

export interface JobOutputRecordV1 {
  path: string;
  sizeBytes: number;
  modifiedAt: number;
  trashedPath?: string;
  undone?: boolean;
}

export interface JobHistoryEntryV1 {
  jobId: string;
  toolId: string;
  createdAt: number;
  undone?: boolean;
  trashEmptied?: boolean;
  outputs: JobOutputRecordV1[];
}

export interface UndoSkippedV1 {
  path: string;
  reason: string;
}

export interface UndoResultV1 {
  success: boolean;
  message: string;
  moved: string[];
  skipped: UndoSkippedV1[];
}

export interface RecipeStepV1 {
  id: string;
  name: string;
  toolId: string;
  /** Short per-step helper copy rendered in the recipe stepper. */
  detail?: string;
}

export interface RecipeV1 {
  id: string;
  name: string;
  description: string;
  category: string;
  inputKind: string;
  acceptExtensions: string[];
  /** 0 (or omitted) means the recipe accepts any number of inputs. */
  maxInputs?: number;
  /** Short capability notes shown as chips on the recipe page. */
  highlights?: string[];
  steps: RecipeStepV1[];
  defaultTargetSizeBytes: number;
}

export interface RecipeRequestV1 {
  recipeId: string;
  inputPaths: string[];
  outputDir: string;
  options: Record<string, unknown>;
}

export interface RecipePlanResponseV1 {
  success: boolean;
  message: string;
  recipe?: RecipeV1;
  inputPaths: string[];
  outputDir: string;
  outputs: PlannedOutputV1[];
  warnings?: string[];
  error?: JobErrorV1;
}

export interface RecipeStepResultV1 {
  id: string;
  name: string;
  toolId: string;
  status: string;
  message?: string;
  outputs?: string[];
  error?: JobErrorV1;
}

export interface RecipeResultV1 {
  jobId: string;
  recipeId: string;
  status: string;
  message: string;
  currentStep: number;
  totalSteps: number;
  steps: RecipeStepResultV1[];
  outputs: string[];
  targetSizeBytes?: number;
  verification?: VerificationReportV1;
  error?: JobErrorV1;
  startedAt: number;
  endedAt?: number;
}

export interface RunRecipeResponseV1 {
  success: boolean;
  message: string;
  jobId: string;
  status: string;
  error?: JobErrorV1;
}

export interface RecipeStatusResponseV1 {
  success: boolean;
  found: boolean;
  message: string;
  result?: RecipeResultV1;
  error?: JobErrorV1;
}

export interface ListRecipesResponseV1 {
  success: boolean;
  message: string;
  recipes: RecipeV1[];
}

export interface RecipeProgressEventV1 {
  jobId: string;
  recipeId: string;
  status: string;
  currentStep: number;
  totalSteps: number;
  stepName?: string;
  stage: string;
  message: string;
  percent: number;
}

@Injectable({
  providedIn: 'root',
})
export class Wails {
  private static readonly THUMBNAIL_CACHE_LIMIT = 128;
  private static readonly THUMBNAIL_POLL_ATTEMPTS = 20;
  private static readonly THUMBNAIL_POLL_DELAY_MS = 120;

  // Job progress Subject / registration tracking to ensure we only register
  // the Wails Events.On handler once and avoid duplicate listeners.
  // We expose a public Observable that replays the last event for late
  // subscribers while keeping the existing subscribeJobProgressV1 API.
  private jobProgressSubject: ReplaySubject<JobProgressEventV1> | null = null;
  private jobProgressUnsubscribe: (() => void) | null = null;
  private jobProgressRefCount = 0;

  // Recipe progress follows the same lazy registration pipeline as job
  // progress but keeps its own subject, refcount and native handler so the two
  // streams never interfere with each other.
  private recipeProgressSubject: ReplaySubject<RecipeProgressEventV1> | null =
    null;
  private recipeProgressUnsubscribe: (() => void) | null = null;
  private recipeProgressRefCount = 0;

  // Bounded thumbnail cache: key is `${path}:${size}`, value is a data URL or
  // null when the preview could not be produced (browser dev mode, bad file).
  private readonly thumbnailCache = new Map<string, string | null>();

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
    },
  );

  // Same contract as jobProgress$ for the recipes/progress/v1 stream: the
  // native handler only lives while at least one consumer is subscribed and
  // the last event is replayed to late subscribers.
  public readonly recipeProgress$: Observable<RecipeProgressEventV1> =
    new Observable((subscriber) => {
      const unregister = this.ensureRecipeProgressRegistered();

      const sub = this.recipeProgressSubject!.subscribe({
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
        unregister();
      };
    });

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
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IPC_VALIDATE_FAILED',
          error,
        ),
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
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IPC_CANCEL_FAILED',
          error,
        ),
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
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IPC_STATUS_FAILED',
          error,
        ),
      };
    }
  }

  async verifyJobV1(jobId: string): Promise<VerificationReportV1> {
    try {
      return await this.callByID(1020044031, jobId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Verify job failed', error),
        outputs: [],
      };
    }
  }

  async listJobHistoryV1(): Promise<JobHistoryEntryV1[]> {
    try {
      return await this.callByID(2396679858);
    } catch {
      return [];
    }
  }

  async undoJobV1(jobId: string): Promise<UndoResultV1> {
    try {
      return await this.callByID(2249209138, jobId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Undo job failed', error),
        moved: [],
        skipped: [],
      };
    }
  }

  async restoreJobV1(jobId: string): Promise<UndoResultV1> {
    try {
      return await this.callByID(666662266, jobId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Restore job failed', error),
        moved: [],
        skipped: [],
      };
    }
  }

  async emptyTrashV1(): Promise<PathActionResponseV1> {
    try {
      return await this.callByID(1508461378);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Empty trash failed', error),
      };
    }
  }

  async getPdfPreviewSource(
    inputPath: string,
  ): Promise<PDFPreviewSourceResponseV1> {
    try {
      return await this.callByID(632882064, inputPath);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get PDF preview source failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'PDF_PREVIEW_READ_FAILED',
          error,
        ),
      };
    }
  }

  async registerMediaPath(path: string): Promise<MediaSourceResponseV1> {
    try {
      return await this.callByID(1853528726, path);
    } catch (error) {
      return {
        success: false,
        url: '',
        message: this.formatMessage('Register media path failed', error),
      };
    }
  }

  // Preview service wrappers (StartPreview / GetPreviewStatus / GetPreview / CancelPreview)
  // These wrap generated bindings that call into the backend preview service.
  async StartPreview(req: {
    path: string;
    width: number;
    height: number;
    format: string;
    pageRange?: { start: number; end: number };
    pageOffset?: number;
  }): Promise<any> {
    try {
      // bindings use a generated ID; Call.ByID will resolve to the preview model
      return await this.callByID(1123038030, req);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('StartPreview failed', error),
      };
    }
  }

  async GetPreviewStatus(jobID: string): Promise<any> {
    try {
      return await this.callByID(1885578308, jobID);
    } catch (error) {
      return {
        status: 'failed',
        progress: 0,
        message: this.formatMessage('GetPreviewStatus failed', error),
      };
    }
  }

  async GetPreview(jobID: string): Promise<any> {
    try {
      return await this.callByID(1152353978, jobID);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('GetPreview failed', error),
      };
    }
  }

  async CancelPreview(jobID: string): Promise<any> {
    try {
      return await this.callByID(2053488540, jobID);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('CancelPreview failed', error),
      };
    }
  }

  async getImagePreviewSourceV1(
    inputPath: string,
  ): Promise<ImagePreviewSourceResponseV1> {
    try {
      return await this.callByID(2617279209, inputPath);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image preview source failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IMAGE_PREVIEW_READ_FAILED',
          error,
        ),
      };
    }
  }

  async getImageCropPreviewV1(
    request: ImageCropPreviewRequestV1,
  ): Promise<ImageCropPreviewResponseV1> {
    try {
      return await this.callByID(4006508154, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image crop preview failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IMAGE_CROP_PREVIEW_EXECUTION',
          error,
        ),
      };
    }
  }

  async getImageAnnotatePreviewV1(
    request: ImageAnnotatePreviewRequestV1,
  ): Promise<ImageAnnotatePreviewResponseV1> {
    try {
      return await this.callByID(372302096, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image annotate preview failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IMAGE_ANNOTATE_PREVIEW_EXECUTION',
          error,
        ),
      };
    }
  }

  async planJobV1(request: JobRequestV1): Promise<PlanJobResponseV1> {
    try {
      return await this.callByID(254724065, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Plan job failed', error),
        outputs: [],
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_PLAN_FAILED', error),
      };
    }
  }

  async inspectFilesV1(paths: string[]): Promise<InspectFilesResponseV1> {
    try {
      return await this.callByID(3131092044, paths);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Inspect files failed', error),
        files: [],
      };
    }
  }

  async listRecipesV1(): Promise<ListRecipesResponseV1> {
    try {
      return await this.callByID(388441352);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List recipes failed', error),
        recipes: [],
      };
    }
  }

  async planRecipeV1(
    request: RecipeRequestV1,
  ): Promise<RecipePlanResponseV1> {
    try {
      return await this.callByID(2840622370, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Plan recipe failed', error),
        inputPaths: [],
        outputDir: '',
        outputs: [],
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'RECIPE_PLAN_FAILED',
          error,
        ),
      };
    }
  }

  async runRecipeV1(request: RecipeRequestV1): Promise<RunRecipeResponseV1> {
    try {
      return await this.callByID(1732206072, request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Run recipe failed', error),
        jobId: '',
        status: 'failed',
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'RECIPE_RUN_FAILED',
          error,
        ),
      };
    }
  }

  async getRecipeStatusV1(jobId: string): Promise<RecipeStatusResponseV1> {
    try {
      return await this.callByID(938816715, jobId);
    } catch (error) {
      return {
        success: false,
        found: false,
        message: this.formatMessage('Get recipe status failed', error),
      };
    }
  }

  async cancelRecipeV1(jobId: string): Promise<RecipeStatusResponseV1> {
    try {
      return await this.callByID(2728388739, jobId);
    } catch (error) {
      return {
        success: false,
        found: false,
        message: this.formatMessage('Cancel recipe failed', error),
      };
    }
  }

  async openPath(path: string): Promise<PathActionResponseV1> {
    try {
      return await this.callByID(12694641, path);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Open path failed', error),
      };
    }
  }

  async revealInFolder(path: string): Promise<PathActionResponseV1> {
    try {
      return await this.callByID(1778953856, path);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Reveal in folder failed', error),
      };
    }
  }

  /**
   * Produces a small preview image for a file and returns it as a data URL.
   * Returns null instead of throwing: previews are always optional.
   */
  async getThumbnail(path: string, size = 160): Promise<string | null> {
    const trimmed = typeof path === 'string' ? path.trim() : '';
    if (!trimmed) {
      return null;
    }

    const key = `${trimmed}:${size}`;
    if (this.thumbnailCache.has(key)) {
      return this.thumbnailCache.get(key) ?? null;
    }

    const thumbnail = await this.loadThumbnail(trimmed, size);
    this.rememberThumbnail(key, thumbnail);
    return thumbnail;
  }

  private async loadThumbnail(path: string, size: number): Promise<string | null> {
    try {
      const started = await this.StartPreview({
        path,
        width: size,
        height: size,
        format: 'webp',
        pageRange: { start: 1, end: 1 },
        pageOffset: 0,
      });

      const jobID =
        started && started.success && typeof started.jobID === 'string'
          ? started.jobID
          : '';
      if (!jobID) {
        return null;
      }

      for (
        let attempt = 0;
        attempt < Wails.THUMBNAIL_POLL_ATTEMPTS;
        attempt += 1
      ) {
        const status = await this.GetPreviewStatus(jobID);
        const state = typeof status?.status === 'string' ? status.status : '';

        if (state === 'succeeded') {
          const preview = await this.GetPreview(jobID);
          if (
            preview?.success &&
            typeof preview.data === 'string' &&
            preview.data.length > 0
          ) {
            const contentType =
              typeof preview.contentType === 'string' && preview.contentType
                ? preview.contentType
                : 'image/webp';
            return `data:${contentType};base64,${preview.data}`;
          }
          return null;
        }

        if (state === 'failed' || state === 'canceled' || state === 'timedout') {
          return null;
        }

        if (attempt < Wails.THUMBNAIL_POLL_ATTEMPTS - 1) {
          await this.delay(Wails.THUMBNAIL_POLL_DELAY_MS);
        }
      }

      return null;
    } catch {
      return null;
    }
  }

  private rememberThumbnail(key: string, value: string | null): void {
    if (this.thumbnailCache.size >= Wails.THUMBNAIL_CACHE_LIMIT) {
      const oldest = this.thumbnailCache.keys().next().value;
      if (oldest !== undefined) {
        this.thumbnailCache.delete(oldest);
      }
    }
    this.thumbnailCache.set(key, value);
  }

  private delay(milliseconds: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, milliseconds));
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

  subscribeJobProgressV1(
    callback: (event: JobProgressEventV1) => void,
  ): () => void {
    // Use the public observable internally to keep behavior consistent and
    // backward-compatible. subscribeJobProgressV1 should subscribe to the
    // jobProgress$ Observable and return an unsubscribe function.
    const unregister = this.ensureJobProgressRegistered();

    const subscription: Subscription =
      this.jobProgressSubject!.subscribe(callback);

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

  subscribeRecipeProgressV1(
    callback: (event: RecipeProgressEventV1) => void,
  ): () => void {
    const unregister = this.ensureRecipeProgressRegistered();

    const subscription: Subscription =
      this.recipeProgressSubject!.subscribe(callback);

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
      this.jobProgressUnsubscribe = Events.On(
        'jobs/progress/v1',
        (ev: { data?: unknown }) => {
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
            toolId:
              typeof candidate.toolId === 'string' ? candidate.toolId : '',
            status:
              typeof candidate.status === 'string'
                ? candidate.status
                : 'running',
            progress: candidate.progress,
          });
        },
      );
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

  // Ensure the recipe replay subject exists and the native recipe event
  // handler is registered. Mirrors ensureJobProgressRegistered so both streams
  // share the same lazy lifecycle without sharing state.
  private ensureRecipeProgressRegistered(): () => void {
    if (!this.recipeProgressSubject) {
      this.recipeProgressSubject = new ReplaySubject<RecipeProgressEventV1>(1);
      this.recipeProgressUnsubscribe = Events.On(
        'recipes/progress/v1',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<RecipeProgressEventV1>;
          // A job id and a usable percent are the minimum needed to drive the
          // stepper; anything else can be defaulted.
          if (
            typeof candidate.jobId !== 'string' ||
            candidate.jobId.trim().length === 0 ||
            typeof candidate.percent !== 'number' ||
            !Number.isFinite(candidate.percent)
          ) {
            return;
          }

          this.recipeProgressSubject?.next({
            jobId: candidate.jobId,
            recipeId:
              typeof candidate.recipeId === 'string' ? candidate.recipeId : '',
            status:
              typeof candidate.status === 'string'
                ? candidate.status
                : 'running',
            currentStep: this.toStepCount(candidate.currentStep),
            totalSteps: this.toStepCount(candidate.totalSteps),
            stepName:
              typeof candidate.stepName === 'string' ? candidate.stepName : '',
            stage: typeof candidate.stage === 'string' ? candidate.stage : '',
            message:
              typeof candidate.message === 'string' ? candidate.message : '',
            percent: candidate.percent,
          });
        },
      );
    }

    this.recipeProgressRefCount += 1;

    return () => {
      this.recipeProgressRefCount -= 1;
      if (this.recipeProgressRefCount <= 0) {
        try {
          this.recipeProgressUnsubscribe?.();
        } catch {
          // ignore
        }
        this.recipeProgressUnsubscribe = null;
        try {
          this.recipeProgressSubject?.complete();
        } catch {
          // ignore
        }
        this.recipeProgressSubject = null;
        this.recipeProgressRefCount = 0;
      }
    };
  }

  /** Coerces a step counter to a non-negative integer, else 0. */
  private toStepCount(value: unknown): number {
    const numeric = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(numeric) || numeric <= 0) {
      return 0;
    }
    return Math.trunc(numeric);
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
    error: unknown,
  ): JobErrorV1 {
    return {
      code,
      detail_code: detailCode,
      message: error instanceof Error ? error.message : 'Unknown error',
    };
  }
}
