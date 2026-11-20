import { Injectable, NgZone, inject } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Subject, Subscription, ReplaySubject, Observable } from 'rxjs';

import { WailsIpc } from './wails-ipc';

export type ToolRuntimeStatus = 'enabled' | 'disabled' | 'degraded';
export type JobMode = 'single' | 'batch';
export type JobExecutionStatus =
  | 'queued'
  | 'running'
  | 'success'
  | 'failed'
  | 'partial_success'
  | 'cancelled'
  | 'interrupted';

export type CanonicalErrorCode =
  | 'VALIDATION_INVALID_INPUT'
  | 'RUNTIME_DEP_MISSING'
  | 'EXEC_IO_TRANSIENT'
  | 'EXEC_TIMEOUT_TRANSIENT'
  | 'UNSUPPORTED_FORMAT'
  | 'CANCELLED_BY_USER';

export interface ToolManifest {
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

export interface ToolRuntimeState {
  status: ToolRuntimeStatus;
  reason?: string;
  healthy: boolean;
}

export interface ToolCatalogEntry {
  manifest: ToolManifest;
  state: ToolRuntimeState;
}

export interface ListToolsResponse {
  success: boolean;
  message: string;
  tools: ToolCatalogEntry[];
}

export interface JobError {
  code: CanonicalErrorCode | string;
  detail_code?: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface JobRequest {
  toolId: string;
  mode: JobMode;
  inputPaths: string[];
  outputDir: string;
  options: Record<string, unknown>;
  workers?: number;
}

export interface PlannedOutput {
  inputPath: string;
  outputPath: string;
  outputKind: string;
  /** Known file count for directory outputs; omitted when unknown. */
  outputCount?: number;
  exists: boolean;
  adjusted: boolean;
  warnings?: string[];
}

export interface PlanJobResponse {
  success: boolean;
  message: string;
  outputs: PlannedOutput[];
  effectiveOptions?: Record<string, unknown>;
  warnings?: string[];
  error?: JobError;
}

export interface FileInfo {
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
  videoCodec?: string;
  audioCodec?: string;
  exists: boolean;
  error?: string;
}

export interface InspectFilesResponse {
  success: boolean;
  message: string;
  files: FileInfo[];
}

export interface PathActionResponse {
  success: boolean;
  message: string;
}

export interface MediaSourceResponse {
  success: boolean;
  url: string;
  fallbackUrl?: string;
  message: string;
}

export interface JobProgress {
  current: number;
  total: number;
  stage: string;
  message: string;
  etaSeconds?: number;
}

export interface JobProgressEvent {
  jobId: string;
  toolId: string;
  status: JobExecutionStatus | string;
  progress: JobProgress;
}

export interface JobResultItem {
  inputPath: string;
  outputPath: string;
  outputs?: string[];
  outputCount?: number;
  attempts?: number;
  retryCount?: number;
  success: boolean;
  message: string;
  error?: JobError;
}

export interface JobResult {
  jobId: string;
  success: boolean;
  message: string;
  toolId: string;
  status: JobExecutionStatus;
  progress: JobProgress;
  items: JobResultItem[];
  error?: JobError;
  startedAt: number;
  endedAt?: number;
}

export interface ValidateJobResponse {
  success: boolean;
  message: string;
  valid: boolean;
  error?: JobError;
}

export interface RunJobResponse {
  success: boolean;
  message: string;
  jobId: string;
  status: JobExecutionStatus;
  error?: JobError;
}

export interface CancelJobResponse {
  success: boolean;
  message: string;
  jobId: string;
  error?: JobError;
}

export interface JobStatusResponse {
  success: boolean;
  message: string;
  found: boolean;
  result?: JobResult;
  error?: JobError;
}

export interface JobListResponse {
  success: boolean;
  message: string;
  jobs: JobResult[];
}

export interface PDFPreviewSourceResponse {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  error?: JobError;
}

export interface ImagePreviewSourceResponse {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobError;
}

export interface ImageCropPreviewRequest {
  inputPath: string;
  x: number;
  y: number;
  width: number;
  height: number;
  ratioPreset?: string;
  format?: string;
}

export interface ImageCropPreviewResponse {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobError;
}

export type ImageAnnotateOperationType =
  | 'text'
  | 'arrow'
  | 'rect'
  | 'blur'
  | 'redact';

export interface ImageAnnotateOperation {
  type: ImageAnnotateOperationType;
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

export interface ImageAnnotatePreviewRequest {
  inputPath: string;
  operations: ImageAnnotateOperation[];
  format?: string;
}

export interface ImageAnnotatePreviewResponse {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  width?: number;
  height?: number;
  error?: JobError;
}

export interface HeaderFooterConfig {
  enabled: boolean;
  text: string;
  align: 'left' | 'center' | 'right';
  font: 'helvetica' | 'times' | 'courier';
  marginTop: number;
  marginBottom: number;
  color: string;
}

export interface MarkdownToPdfOptions {
  outputPath?: string;
  header: HeaderFooterConfig;
  footer: HeaderFooterConfig;
}

export interface VerificationCheck {
  key: string;
  label: string;
  passed: boolean;
  value?: string;
  detail?: string;
}

export interface VerifiedOutput {
  path: string;
  exists: boolean;
  sizeBytes: number;
  kind?: string;
  checks: VerificationCheck[];
  warnings?: string[];
}

export interface VerificationReport {
  success: boolean;
  message: string;
  outputs: VerifiedOutput[];
  warnings?: string[];
}

export interface JobOutputRecord {
  path: string;
  sizeBytes: number;
  modifiedAt: number;
  trashedPath?: string;
  undone?: boolean;
}

export interface JobHistoryEntry {
  jobId: string;
  toolId: string;
  createdAt: number;
  undone?: boolean;
  trashEmptied?: boolean;
  outputs: JobOutputRecord[];
}

export interface UndoSkipped {
  path: string;
  reason: string;
}

export interface UndoResult {
  success: boolean;
  message: string;
  moved: string[];
  skipped: UndoSkipped[];
}

export interface RecipeStep {
  id: string;
  name: string;
  toolId: string;
  /** Short per-step helper copy rendered in the recipe stepper. */
  detail?: string;
}

export interface Recipe {
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
  steps: RecipeStep[];
  defaultTargetSizeBytes: number;
}

export interface RecipeRequest {
  recipeId: string;
  inputPaths: string[];
  outputDir: string;
  options: Record<string, unknown>;
}

export interface RecipePlanResponse {
  success: boolean;
  message: string;
  recipe?: Recipe;
  inputPaths: string[];
  outputDir: string;
  outputs: PlannedOutput[];
  warnings?: string[];
  error?: JobError;
}

export interface RecipeStepResult {
  id: string;
  name: string;
  toolId: string;
  status: string;
  message?: string;
  outputs?: string[];
  error?: JobError;
}

export interface RecipeResult {
  jobId: string;
  recipeId: string;
  status: string;
  message: string;
  currentStep: number;
  totalSteps: number;
  steps: RecipeStepResult[];
  outputs: string[];
  targetSizeBytes?: number;
  verification?: VerificationReport;
  error?: JobError;
  startedAt: number;
  endedAt?: number;
}

export interface RunRecipeResponse {
  success: boolean;
  message: string;
  jobId: string;
  status: string;
  error?: JobError;
}

export interface RecipeStatusResponse {
  success: boolean;
  found: boolean;
  message: string;
  result?: RecipeResult;
  error?: JobError;
}

export interface ListRecipesResponse {
  success: boolean;
  message: string;
  recipes: Recipe[];
}

export interface RecipeProgressEvent {
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

/**
 * One watch-folder rule. `runtimeStatus`/`runtimeReason` are response-only
 * dynamic state: the backend never persists them.
 */
export interface AutomationRule {
  id: string;
  name: string;
  enabled: boolean;
  watchDir: string;
  recipeId: string;
  extensions?: string[];
  outputDir?: string;
  options?: Record<string, unknown>;
  deleteSource?: boolean;
  createdAt: number;
  updatedAt?: number;
  lastRunAt?: number;
  lastStatus?: string;
  lastMessage?: string;
  lastFile?: string;
  lastOutputs?: string[];
  runtimeStatus?: string;
  runtimeReason?: string;
}

export interface ListAutomationRulesResponse {
  success: boolean;
  message: string;
  rules: AutomationRule[];
}

export interface AutomationRuleActionResponse {
  success: boolean;
  message: string;
  rule?: AutomationRule;
  error?: JobError;
}

export interface AutomationStatusResponse {
  success: boolean;
  message: string;
  totalRules: number;
  activeRules: number;
  lastRunAt?: number;
  lastStatus?: string;
  lastMessage?: string;
  lastFile?: string;
}

export interface AutomationRunEvent {
  ruleId: string;
  ruleName: string;
  filePath: string;
  status: string;
  message: string;
  outputs?: string[];
  at: number;
}

/**
 * Detected state of the local OCR pipeline. `available` requires pdftoppm,
 * tesseract and at least one installed language; `languages` excludes osd.
 * Field names mirror the Go DTO JSON tags.
 */
export interface OCRCapabilitiesResponse {
  success: boolean;
  message: string;
  available: boolean;
  pdfToPpm: boolean;
  tesseract: boolean;
  pdfToText: boolean;
  languages: string[];
  recommendedLanguage: string;
}

/**
 * Detected state of the local Whisper transcription pipeline. `available`
 * requires both the whisper-cli binary and a model file. Paths are resolved:
 * a user override wins when it exists, otherwise the installed defaults.
 * Field names mirror the Go DTO JSON tags.
 */
export interface TranscriptionCapabilitiesResponse {
  success: boolean;
  message: string;
  available: boolean;
  whisperPath: string;
  whisperPresent: boolean;
  modelPath: string;
  modelPresent: boolean;
  modelName?: string;
  modelSizeBytes?: number;
  languages: string[];
  defaultLanguage?: string;
}

/** One external binary the local tools can rely on. */
export interface LocalCapability {
  id: string;
  label: string;
  available: boolean;
  required: boolean;
  detail: string;
  suggestion?: string;
}

/**
 * Snapshot of the local runtime surface. Optional missing tools are guidance,
 * never an app failure. Field names mirror the Go DTO JSON tags.
 */
export interface LocalCapabilitiesResponse {
  success: boolean;
  message: string;
  checks: LocalCapability[];
  ocrLanguages: string[];
  recommendedOcrLanguage?: string;
}

/** One terminal automation run kept in the local run history. */
export interface AutomationRunRecord {
  id: string;
  ruleId: string;
  ruleName: string;
  filePath: string;
  status: string;
  message: string;
  outputs?: string[];
  at: number;
}

export interface ListAutomationRunsResponse {
  success: boolean;
  message: string;
  runs: AutomationRunRecord[];
}

export interface AutomationRunsActionResponse {
  success: boolean;
  message: string;
}

/**
 * One portable component of the managed local toolchain. `ready` means the
 * file was verified on disk; required components gate the overall readiness.
 * Field names mirror the Go DTO JSON tags.
 */
export interface ToolchainComponent {
  id: string;
  label: string;
  ready: boolean;
  required: boolean;
  path?: string;
  version?: string;
  detail?: string;
}

/**
 * Managed toolchain snapshot. It is safe to poll while an install runs; a
 * failed install reports `success: false` with `error` set, while an
 * unsupported platform reports `success: true` with a message and no ready
 * components.
 */
export interface ToolchainStatus {
  success: boolean;
  message: string;
  ready: boolean;
  installing: boolean;
  cancelling?: boolean;
  percent: number;
  phase?: string;
  detail?: string;
  error?: string;
  storageDir: string;
  components: ToolchainComponent[];
  model: string;
  modelPath?: string;
  ocrLanguages: string[];
}

/** Install selection; an empty model means `small`, empty languages mean both. */
export interface ToolchainInstallOptions {
  model: string;
  ocrLanguages: string[];
  force?: boolean;
}

/** One installer progress update emitted on `toolchain/install`. */
export interface ToolchainInstallEvent {
  phase: string;
  percent: number;
  indeterminate?: boolean;
  message: string;
  done?: boolean;
  error?: string;
}

@Injectable({
  providedIn: 'root',
})
export class Wails {
  private static readonly THUMBNAIL_CACHE_LIMIT = 128;
  private static readonly THUMBNAIL_POLL_ATTEMPTS = 20;
  private static readonly THUMBNAIL_POLL_DELAY_MS = 120;

  // Native Wails events can arrive outside the Angular zone; new state streams
  // run their emissions inside it so signal consumers re-render reliably.
  private readonly zone = inject(NgZone);

  // Every RPC goes through the generated bindings; no numeric method IDs.
  private readonly ipc = inject(WailsIpc);

  // Job progress Subject / registration tracking to ensure we only register
  // the Wails Events.On handler once and avoid duplicate listeners.
  // We expose a public Observable that replays the last event for late
  // subscribers while keeping the existing subscribeJobProgress API.
  private jobProgressSubject: ReplaySubject<JobProgressEvent> | null = null;
  private jobProgressUnsubscribe: (() => void) | null = null;
  private jobProgressRefCount = 0;

  // Recipe progress follows the same lazy registration pipeline as job
  // progress but keeps its own subject, refcount and native handler so the two
  // streams never interfere with each other.
  private recipeProgressSubject: ReplaySubject<RecipeProgressEvent> | null =
    null;
  private recipeProgressUnsubscribe: (() => void) | null = null;
  private recipeProgressRefCount = 0;

  // Full job/recipe snapshots follow the same lazy registration contract as
  // the progress streams. The backend emits one snapshot on submit and another
  // on every terminal status, so the activity view can rebuild its list without
  // polling each job id.
  private jobStateSubject: ReplaySubject<JobResult> | null = null;
  private jobStateUnsubscribe: (() => void) | null = null;
  private jobStateRefCount = 0;

  private recipeStateSubject: ReplaySubject<RecipeResult> | null = null;
  private recipeStateUnsubscribe: (() => void) | null = null;
  private recipeStateRefCount = 0;

  // Automation run events follow the same lazy lifecycle as recipe progress.
  // A rule can run more than one file over time, so consumers filter by ruleId
  // and must tolerate a stale replayed event.
  private automationRunSubject: ReplaySubject<AutomationRunEvent> | null =
    null;
  private automationRunUnsubscribe: (() => void) | null = null;
  private automationRunRefCount = 0;

  // Toolchain installer progress follows the same lazy lifecycle. A late UI
  // gets the last event replayed, then polls the install state for the rest.
  private toolchainInstallSubject: ReplaySubject<ToolchainInstallEvent> | null =
    null;
  private toolchainInstallUnsubscribe: (() => void) | null = null;
  private toolchainInstallRefCount = 0;

  // Bounded thumbnail cache: key is `${path}:${size}`, value is a data URL or
  // null when the preview could not be produced (browser dev mode, bad file).
  private readonly thumbnailCache = new Map<string, string | null>();

  // Public observable that consumers can subscribe to. It will ensure the
  // native Events.On handler is registered while there are active
  // subscribers and will replay the last value for late subscribers.
  public readonly jobProgress$: Observable<JobProgressEvent> = new Observable(
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

  // Same contract as jobProgress$ for the recipes/progress stream: the
  // native handler only lives while at least one consumer is subscribed and
  // the last event is replayed to late subscribers.
  public readonly recipeProgress$: Observable<RecipeProgressEvent> =
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

  // Full snapshot streams. `jobs/state` and `recipes/state` carry a
  // complete JobResult / RecipeResult on submit and on terminal status.
  // They mirror recipeProgress$: lazy native registration, last snapshot
  // replayed to late subscribers, and refcount-based cleanup.
  public readonly jobState$: Observable<JobResult> = new Observable(
    (subscriber) => {
      const unregister = this.ensureJobStateRegistered();

      const sub = this.jobStateSubject!.subscribe({
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
    },
  );

  public readonly recipeState$: Observable<RecipeResult> = new Observable(
    (subscriber) => {
      const unregister = this.ensureRecipeStateRegistered();

      const sub = this.recipeStateSubject!.subscribe({
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
    },
  );

  // Same contract as recipeProgress$ for the automation/run stream: the
  // native handler only lives while at least one consumer is subscribed and
  // the last event is replayed to late subscribers. The replayed event may
  // belong to another rule, so consumers filter by ruleId.
  public readonly automationRun$: Observable<AutomationRunEvent> =
    new Observable((subscriber) => {
      const unregister = this.ensureAutomationRunRegistered();

      const sub = this.automationRunSubject!.subscribe({
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

  // Same contract as recipeProgress$ for the toolchain/install stream. The
  // installer may fire events before the UI loads, so late subscribers get the
  // last event replayed and the status endpoint fills in the rest.
  public readonly toolchainInstall$: Observable<ToolchainInstallEvent> =
    new Observable((subscriber) => {
      const unregister = this.ensureToolchainInstallRegistered();

      const sub = this.toolchainInstallSubject!.subscribe({
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

  async listTools(): Promise<ListToolsResponse> {
    try {
      return this.asLegacyDto<ListToolsResponse>(
        await this.ipc.listTools(),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Failed to list tools', error),
        tools: [],
      };
    }
  }

  async validateJob(request: JobRequest): Promise<ValidateJobResponse> {
    try {
      return this.asLegacyDto<ValidateJobResponse>(
        await this.ipc.validateJob(request),
      );
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

  async runJob(request: JobRequest): Promise<RunJobResponse> {
    try {
      return this.asLegacyDto<RunJobResponse>(
        await this.ipc.runJob(request),
      );
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

  async cancelJob(jobId: string): Promise<CancelJobResponse> {
    try {
      return this.asLegacyDto<CancelJobResponse>(
        await this.ipc.cancelJob(jobId),
      );
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

  async getJobStatus(jobId: string): Promise<JobStatusResponse> {
    try {
      return this.asLegacyDto<JobStatusResponse>(
        await this.ipc.getJobStatus(jobId),
      );
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

  async listJobs(limit = 50): Promise<JobListResponse> {
    try {
      return this.asLegacyDto<JobListResponse>(
        await this.ipc.listJobs(limit),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List jobs failed', error),
        jobs: [],
      };
    }
  }

  async verifyJob(jobId: string): Promise<VerificationReport> {
    try {
      return this.asLegacyDto<VerificationReport>(
        await this.ipc.verifyJob(jobId),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Verify job failed', error),
        outputs: [],
      };
    }
  }

  async listJobHistory(): Promise<JobHistoryEntry[]> {
    try {
      return this.asLegacyDto<JobHistoryEntry[]>(
        await this.ipc.listJobHistory(),
      );
    } catch {
      return [];
    }
  }

  async undoJob(jobId: string): Promise<UndoResult> {
    try {
      return this.asLegacyDto<UndoResult>(
        await this.ipc.undoJob(jobId),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Undo job failed', error),
        moved: [],
        skipped: [],
      };
    }
  }

  async restoreJob(jobId: string): Promise<UndoResult> {
    try {
      return this.asLegacyDto<UndoResult>(
        await this.ipc.restoreJob(jobId),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Restore job failed', error),
        moved: [],
        skipped: [],
      };
    }
  }

  async emptyTrash(): Promise<PathActionResponse> {
    try {
      return await this.ipc.emptyTrash();
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Empty trash failed', error),
      };
    }
  }

  async getPdfPreviewSource(
    inputPath: string,
  ): Promise<PDFPreviewSourceResponse> {
    try {
      return this.asLegacyDto<PDFPreviewSourceResponse>(
        await this.ipc.getPdfPreviewSource(inputPath),
      );
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

  async registerMediaPath(path: string): Promise<MediaSourceResponse> {
    try {
      return this.asLegacyDto<MediaSourceResponse>(
        await this.ipc.registerMediaPath(path),
      );
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
      return await this.ipc.startPreview(req);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('StartPreview failed', error),
      };
    }
  }

  async GetPreviewStatus(jobID: string): Promise<any> {
    try {
      return await this.ipc.getPreviewStatus(jobID);
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
      return await this.ipc.getPreview(jobID);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('GetPreview failed', error),
      };
    }
  }

  async CancelPreview(jobID: string): Promise<any> {
    try {
      return await this.ipc.cancelPreview(jobID);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('CancelPreview failed', error),
      };
    }
  }

  async getImagePreviewSource(
    inputPath: string,
  ): Promise<ImagePreviewSourceResponse> {
    try {
      return this.asLegacyDto<ImagePreviewSourceResponse>(
        await this.ipc.getImagePreviewSource(inputPath),
      );
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

  async getImageCropPreview(
    request: ImageCropPreviewRequest,
  ): Promise<ImageCropPreviewResponse> {
    try {
      return this.asLegacyDto<ImageCropPreviewResponse>(
        await this.ipc.getImageCropPreview(request),
      );
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

  async getImageAnnotatePreview(
    request: ImageAnnotatePreviewRequest,
  ): Promise<ImageAnnotatePreviewResponse> {
    try {
      return this.asLegacyDto<ImageAnnotatePreviewResponse>(
        await this.ipc.getImageAnnotatePreview(request),
      );
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

  async planJob(request: JobRequest): Promise<PlanJobResponse> {
    try {
      return this.asLegacyDto<PlanJobResponse>(
        await this.ipc.planJob(request),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Plan job failed', error),
        outputs: [],
        error: this.defaultError('EXEC_IO_TRANSIENT', 'IPC_PLAN_FAILED', error),
      };
    }
  }

  async inspectFiles(paths: string[]): Promise<InspectFilesResponse> {
    try {
      return this.asLegacyDto<InspectFilesResponse>(
        await this.ipc.inspectFiles(paths),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Inspect files failed', error),
        files: [],
      };
    }
  }

  async getOCRCapabilities(): Promise<OCRCapabilitiesResponse> {
    try {
      return this.asLegacyDto<OCRCapabilitiesResponse>(
        await this.ipc.getOcrCapabilities(),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get OCR capabilities failed', error),
        available: false,
        pdfToPpm: false,
        tesseract: false,
        pdfToText: false,
        languages: [],
        recommendedLanguage: '',
      };
    }
  }

  async getTranscriptionCapabilities(
    whisperPath: string,
    modelPath: string,
  ): Promise<TranscriptionCapabilitiesResponse> {
    try {
      return this.asLegacyDto<TranscriptionCapabilitiesResponse>(
        await this.ipc.getTranscriptionCapabilities(whisperPath, modelPath),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage(
          'Get transcription capabilities failed',
          error,
        ),
        available: false,
        whisperPath: '',
        whisperPresent: false,
        modelPath: '',
        modelPresent: false,
        languages: [],
      };
    }
  }

  async getLocalCapabilities(): Promise<LocalCapabilitiesResponse> {
    try {
      return this.asLegacyDto<LocalCapabilitiesResponse>(
        await this.ipc.getLocalCapabilities(),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get local capabilities failed', error),
        checks: [],
        ocrLanguages: [],
      };
    }
  }

  async getToolchainStatus(): Promise<ToolchainStatus> {
    try {
      return this.asLegacyDto<ToolchainStatus>(
        await this.ipc.getToolchainStatus(),
      );
    } catch (error) {
      return this.toolchainFallback('Get toolchain status failed', error);
    }
  }

  async getToolchainInstallState(): Promise<ToolchainStatus> {
    try {
      return this.asLegacyDto<ToolchainStatus>(
        await this.ipc.getToolchainInstallState(),
      );
    } catch (error) {
      return this.toolchainFallback('Get toolchain install state failed', error);
    }
  }

  async startToolchainInstall(
    options: ToolchainInstallOptions,
  ): Promise<ToolchainStatus> {
    try {
      return this.asLegacyDto<ToolchainStatus>(
        await this.ipc.startToolchainInstall(options),
      );
    } catch (error) {
      return this.toolchainFallback('Start toolchain install failed', error);
    }
  }

  async cancelToolchainInstall(): Promise<ToolchainStatus> {
    try {
      return this.asLegacyDto<ToolchainStatus>(
        await this.ipc.cancelToolchainInstall(),
      );
    } catch (error) {
      return this.toolchainFallback('Cancel toolchain install failed', error);
    }
  }

  async listRecipes(): Promise<ListRecipesResponse> {
    try {
      return this.asLegacyDto<ListRecipesResponse>(
        await this.ipc.listRecipes(),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List recipes failed', error),
        recipes: [],
      };
    }
  }

  async planRecipe(
    request: RecipeRequest,
  ): Promise<RecipePlanResponse> {
    try {
      return this.asLegacyDto<RecipePlanResponse>(
        await this.ipc.planRecipe(request),
      );
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

  async runRecipe(request: RecipeRequest): Promise<RunRecipeResponse> {
    try {
      return this.asLegacyDto<RunRecipeResponse>(
        await this.ipc.runRecipe(request),
      );
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

  async getRecipeStatus(jobId: string): Promise<RecipeStatusResponse> {
    try {
      return this.asLegacyDto<RecipeStatusResponse>(
        await this.ipc.getRecipeStatus(jobId),
      );
    } catch (error) {
      return {
        success: false,
        found: false,
        message: this.formatMessage('Get recipe status failed', error),
      };
    }
  }

  async listRecipeRuns(limit = 50): Promise<RecipeResult[]> {
    try {
      return this.asLegacyDto<RecipeResult[]>(
        await this.ipc.listRecipeRuns(limit),
      );
    } catch {
      return [];
    }
  }

  async cancelRecipe(jobId: string): Promise<RecipeStatusResponse> {
    try {
      return this.asLegacyDto<RecipeStatusResponse>(
        await this.ipc.cancelRecipe(jobId),
      );
    } catch (error) {
      return {
        success: false,
        found: false,
        message: this.formatMessage('Cancel recipe failed', error),
      };
    }
  }

  async listAutomationRules(): Promise<ListAutomationRulesResponse> {
    try {
      return this.asLegacyDto<ListAutomationRulesResponse>(
        await this.ipc.listAutomationRules(),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List automation rules failed', error),
        rules: [],
      };
    }
  }

  async saveAutomationRule(
    rule: AutomationRule,
  ): Promise<AutomationRuleActionResponse> {
    try {
      return this.asLegacyDto<AutomationRuleActionResponse>(
        await this.ipc.saveAutomationRule(rule),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Save automation rule failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'AUTOMATION_RULE_SAVE_FAILED',
          error,
        ),
      };
    }
  }

  async deleteAutomationRule(
    id: string,
  ): Promise<AutomationRuleActionResponse> {
    try {
      return this.asLegacyDto<AutomationRuleActionResponse>(
        await this.ipc.deleteAutomationRule(id),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Delete automation rule failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'AUTOMATION_RULE_DELETE_FAILED',
          error,
        ),
      };
    }
  }

  async setAutomationRuleEnabled(
    id: string,
    enabled: boolean,
  ): Promise<AutomationRuleActionResponse> {
    try {
      return this.asLegacyDto<AutomationRuleActionResponse>(
        await this.ipc.setAutomationRuleEnabled(id, enabled),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Update automation rule failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'AUTOMATION_RULE_UPDATE_FAILED',
          error,
        ),
      };
    }
  }

  async getAutomationStatus(): Promise<AutomationStatusResponse> {
    try {
      return await this.ipc.getAutomationStatus();
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get automation status failed', error),
        totalRules: 0,
        activeRules: 0,
      };
    }
  }

  async listAutomationRuns(
    limit: number,
  ): Promise<ListAutomationRunsResponse> {
    try {
      return this.asLegacyDto<ListAutomationRunsResponse>(
        await this.ipc.listAutomationRuns(limit),
      );
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List automation runs failed', error),
        runs: [],
      };
    }
  }

  async clearAutomationRuns(): Promise<AutomationRunsActionResponse> {
    try {
      return await this.ipc.clearAutomationRuns();
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Clear automation runs failed', error),
      };
    }
  }

  async openPath(path: string): Promise<PathActionResponse> {
    try {
      return await this.ipc.openPath(path);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Open path failed', error),
      };
    }
  }

  async revealInFolder(path: string): Promise<PathActionResponse> {
    try {
      return await this.ipc.revealInFolder(path);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Reveal in folder failed', error),
      };
    }
  }

  async notify(title: string, body: string): Promise<PathActionResponse> {
    try {
      return await this.ipc.notify(title, body);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Send notification failed', error),
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
      return (await this.ipc.openFileDialog()) ?? '';
    } catch {
      return '';
    }
  }

  async openMultipleFilesDialog(): Promise<string[]> {
    try {
      return (await this.ipc.openMultipleFilesDialog()) ?? [];
    } catch {
      return [];
    }
  }

  async openDirectoryDialog(): Promise<string> {
    try {
      return (await this.ipc.openDirectoryDialog()) ?? '';
    } catch {
      return '';
    }
  }

  subscribeJobProgress(
    callback: (event: JobProgressEvent) => void,
  ): () => void {
    // Use the public observable internally to keep behavior consistent and
    // backward-compatible. subscribeJobProgress should subscribe to the
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

  subscribeRecipeProgress(
    callback: (event: RecipeProgressEvent) => void,
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

  subscribeAutomationRun(
    callback: (event: AutomationRunEvent) => void,
  ): () => void {
    const unregister = this.ensureAutomationRunRegistered();

    const subscription: Subscription =
      this.automationRunSubject!.subscribe(callback);

    return () => {
      try {
        subscription.unsubscribe();
      } catch {
        // ignore
      }
      unregister();
    };
  }

  subscribeToolchainInstall(
    callback: (event: ToolchainInstallEvent) => void,
  ): () => void {
    const unregister = this.ensureToolchainInstallRegistered();

    const subscription: Subscription =
      this.toolchainInstallSubject!.subscribe(callback);

    return () => {
      try {
        subscription.unsubscribe();
      } catch {
        // ignore
      }
      unregister();
    };
  }

  // Ensure the replay subject exists and the native Events.On handler is
  // registered. Increments an internal refcount and returns an unregister
  // function that should be called when the consumer unsubscribes.
  private ensureJobProgressRegistered(): () => void {
    // Lazy-create subject if necessary
    if (!this.jobProgressSubject) {
      this.jobProgressSubject = new ReplaySubject<JobProgressEvent>(1);
      // Register the native event listener - store the unsubscribe fn
      this.jobProgressUnsubscribe = Events.On(
        'jobs/progress',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<JobProgressEvent>;
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
      this.recipeProgressSubject = new ReplaySubject<RecipeProgressEvent>(1);
      this.recipeProgressUnsubscribe = Events.On(
        'recipes/progress',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<RecipeProgressEvent>;
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

  // Ensure the job snapshot replay subject exists and the native handler is
  // registered. Mirrors ensureRecipeProgressRegistered; a snapshot needs a job
  // id and a status to be usable, everything else can be defaulted.
  private ensureJobStateRegistered(): () => void {
    if (!this.jobStateSubject) {
      this.jobStateSubject = new ReplaySubject<JobResult>(1);
      this.jobStateUnsubscribe = Events.On(
        'jobs/state',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<JobResult>;
          if (
            typeof candidate.jobId !== 'string' ||
            candidate.jobId.trim().length === 0 ||
            typeof candidate.status !== 'string' ||
            candidate.status.trim().length === 0
          ) {
            return;
          }

          this.emitJobState({
            jobId: candidate.jobId,
            success: candidate.success === true,
            message:
              typeof candidate.message === 'string' ? candidate.message : '',
            toolId: typeof candidate.toolId === 'string' ? candidate.toolId : '',
            status: candidate.status as JobExecutionStatus,
            progress: candidate.progress ?? {
              current: 0,
              total: 0,
              stage: candidate.status,
              message: '',
            },
            items: Array.isArray(candidate.items) ? candidate.items : [],
            error: candidate.error,
            startedAt:
              typeof candidate.startedAt === 'number' ? candidate.startedAt : 0,
            endedAt:
              typeof candidate.endedAt === 'number'
                ? candidate.endedAt
                : undefined,
          });
        },
      );
    }

    this.jobStateRefCount += 1;

    return () => {
      this.jobStateRefCount -= 1;
      if (this.jobStateRefCount <= 0) {
        try {
          this.jobStateUnsubscribe?.();
        } catch {
          // ignore
        }
        this.jobStateUnsubscribe = null;
        try {
          this.jobStateSubject?.complete();
        } catch {
          // ignore
        }
        this.jobStateSubject = null;
        this.jobStateRefCount = 0;
      }
    };
  }

  // Ensure the recipe snapshot replay subject exists and the native handler is
  // registered. Only the job id and status are required.
  private ensureRecipeStateRegistered(): () => void {
    if (!this.recipeStateSubject) {
      this.recipeStateSubject = new ReplaySubject<RecipeResult>(1);
      this.recipeStateUnsubscribe = Events.On(
        'recipes/state',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<RecipeResult>;
          if (
            typeof candidate.jobId !== 'string' ||
            candidate.jobId.trim().length === 0 ||
            typeof candidate.status !== 'string' ||
            candidate.status.trim().length === 0
          ) {
            return;
          }

          this.emitRecipeState({
            jobId: candidate.jobId,
            recipeId:
              typeof candidate.recipeId === 'string' ? candidate.recipeId : '',
            status: candidate.status,
            message:
              typeof candidate.message === 'string' ? candidate.message : '',
            currentStep: this.toStepCount(candidate.currentStep),
            totalSteps: this.toStepCount(candidate.totalSteps),
            steps: Array.isArray(candidate.steps) ? candidate.steps : [],
            outputs: Array.isArray(candidate.outputs)
              ? candidate.outputs.filter(
                  (output): output is string => typeof output === 'string',
                )
              : [],
            targetSizeBytes:
              typeof candidate.targetSizeBytes === 'number'
                ? candidate.targetSizeBytes
                : undefined,
            verification: candidate.verification,
            error: candidate.error,
            startedAt:
              typeof candidate.startedAt === 'number' ? candidate.startedAt : 0,
            endedAt:
              typeof candidate.endedAt === 'number'
                ? candidate.endedAt
                : undefined,
          });
        },
      );
    }

    this.recipeStateRefCount += 1;

    return () => {
      this.recipeStateRefCount -= 1;
      if (this.recipeStateRefCount <= 0) {
        try {
          this.recipeStateUnsubscribe?.();
        } catch {
          // ignore
        }
        this.recipeStateUnsubscribe = null;
        try {
          this.recipeStateSubject?.complete();
        } catch {
          // ignore
        }
        this.recipeStateSubject = null;
        this.recipeStateRefCount = 0;
      }
    };
  }

  // Native events may be delivered outside the Angular zone. Running the
  // emission inside it lets signal consumers schedule change detection.
  private emitJobState(snapshot: JobResult): void {
    try {
      this.zone.run(() => this.jobStateSubject?.next(snapshot));
    } catch {
      // ignore
    }
  }

  private emitRecipeState(snapshot: RecipeResult): void {
    try {
      this.zone.run(() => this.recipeStateSubject?.next(snapshot));
    } catch {
      // ignore
    }
  }

  // Ensure the automation replay subject exists and the native event handler
  // is registered. Mirrors ensureRecipeProgressRegistered; the event stream
  // only requires a rule id and a status string to be usable.
  private ensureAutomationRunRegistered(): () => void {
    if (!this.automationRunSubject) {
      this.automationRunSubject = new ReplaySubject<AutomationRunEvent>(1);
      this.automationRunUnsubscribe = Events.On(
        'automation/run',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<AutomationRunEvent>;
          if (
            typeof candidate.ruleId !== 'string' ||
            candidate.ruleId.trim().length === 0 ||
            typeof candidate.status !== 'string'
          ) {
            return;
          }

          this.automationRunSubject?.next({
            ruleId: candidate.ruleId,
            ruleName:
              typeof candidate.ruleName === 'string' ? candidate.ruleName : '',
            filePath:
              typeof candidate.filePath === 'string' ? candidate.filePath : '',
            status: candidate.status,
            message:
              typeof candidate.message === 'string' ? candidate.message : '',
            outputs: Array.isArray(candidate.outputs)
              ? candidate.outputs.filter(
                  (output): output is string => typeof output === 'string',
                )
              : [],
            at:
              typeof candidate.at === 'number' &&
              Number.isFinite(candidate.at) &&
              candidate.at > 0
                ? candidate.at
                : 0,
          });
        },
      );
    }

    this.automationRunRefCount += 1;

    return () => {
      this.automationRunRefCount -= 1;
      if (this.automationRunRefCount <= 0) {
        try {
          this.automationRunUnsubscribe?.();
        } catch {
          // ignore
        }
        this.automationRunUnsubscribe = null;
        try {
          this.automationRunSubject?.complete();
        } catch {
          // ignore
        }
        this.automationRunSubject = null;
        this.automationRunRefCount = 0;
      }
    };
  }

  // Ensure the toolchain replay subject exists and the native install event
  // handler is registered. Mirrors the other streams; only a phase string, a
  // finite percent and a message are required to drive the progress UI.
  private ensureToolchainInstallRegistered(): () => void {
    if (!this.toolchainInstallSubject) {
      this.toolchainInstallSubject =
        new ReplaySubject<ToolchainInstallEvent>(1);
      this.toolchainInstallUnsubscribe = Events.On(
        'toolchain/install',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<ToolchainInstallEvent>;
          if (
            typeof candidate.phase !== 'string' ||
            typeof candidate.percent !== 'number' ||
            !Number.isFinite(candidate.percent) ||
            typeof candidate.message !== 'string'
          ) {
            return;
          }

          this.toolchainInstallSubject?.next({
            phase: candidate.phase,
            percent: candidate.percent,
            indeterminate: candidate.indeterminate === true,
            message: candidate.message,
            done: candidate.done === true,
            error:
              typeof candidate.error === 'string' ? candidate.error : undefined,
          });
        },
      );
    }

    this.toolchainInstallRefCount += 1;

    return () => {
      this.toolchainInstallRefCount -= 1;
      if (this.toolchainInstallRefCount <= 0) {
        try {
          this.toolchainInstallUnsubscribe?.();
        } catch {
          // ignore
        }
        this.toolchainInstallUnsubscribe = null;
        try {
          this.toolchainInstallSubject?.complete();
        } catch {
          // ignore
        }
        this.toolchainInstallSubject = null;
        this.toolchainInstallRefCount = 0;
      }
    };
  }

  /** Safe fallback for toolchain calls when IPC is unavailable (preview). */
  private toolchainFallback(
    prefix: string,
    error: unknown,
  ): ToolchainStatus {
    return {
      success: false,
      message: this.formatMessage(prefix, error),
      ready: false,
      installing: false,
      percent: 0,
      storageDir: '',
      components: [],
      model: 'medium',
      ocrLanguages: [],
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
    return typeof window !== 'undefined' && !!(window as any)._wails;
  }

  // The generated bindings model nullable slices and string-typed enums, while
  // the legacy DTO layer keeps the narrower shapes it shipped with. Phase 6
  // deletes the DTO layer; until then the wire value is asserted here.
  private asLegacyDto<T>(value: unknown): T {
    return value as T;
  }

  private formatMessage(prefix: string, error: unknown): string {
    return `${prefix}: ${error instanceof Error ? error.message : 'Unknown error'}`;
  }

  private defaultError(
    code: CanonicalErrorCode,
    detailCode: string,
    error: unknown,
  ): JobError {
    return {
      code,
      detail_code: detailCode,
      message: error instanceof Error ? error.message : 'Unknown error',
    };
  }
}
