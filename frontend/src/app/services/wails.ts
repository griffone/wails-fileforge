import { Injectable, NgZone, inject } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Subject, Subscription, ReplaySubject, Observable } from 'rxjs';

import type * as Models from '../../../bindings/fileforge-desktop/internal/models';

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

// The DTOs below are the frontend contract. They derive from the generated
// Go models so field names can never drift; a local type only remains where
// the UI needs a stricter shape than the wire model exposes.
export type ToolManifest = Models.ToolManifest;
export type ToolRuntimeState = Models.ToolRuntimeState;
export type ToolCatalogEntry = Models.ToolCatalogEntry;

/** Generated list responses model empty Go slices as `null`. */
export interface ListToolsResponse extends Models.ListToolsResponse {
  tools: ToolCatalogEntry[];
}

export type JobError = Models.JobError;

/** The request is frontend-owned, so mode and payload fields stay required. */
export interface JobRequest extends Models.JobRequest {
  mode: JobMode;
  inputPaths: string[];
  options: Record<string, unknown>;
}

export type JobProgress = Models.JobProgress;
export type JobProgressEvent = Models.JobProgressEvent;

export type JobResultItem = Models.JobResultItem;

/** The backend owns the status vocabulary; the UI renders it as a string. */
export type JobResult = Models.JobResult;

export interface PlanJobResponse extends Models.PlanJobResponse {
  outputs: PlannedOutput[];
  effectiveOptions?: Record<string, unknown>;
  warnings?: string[];
  error?: JobError;
}

export interface ValidateJobResponse extends Models.ValidateJobResponse {
  error?: JobError;
}

export interface RunJobResponse extends Models.RunJobResponse {
  error?: JobError;
}

export interface CancelJobResponse extends Models.CancelJobResponse {
  error?: JobError;
}

export interface JobStatusResponse extends Models.JobStatusResponse {
  result?: JobResult;
  error?: JobError;
}

export interface JobListResponse extends Models.JobListResponse {
  jobs: JobResult[];
}

export interface PDFPreviewSourceResponse
  extends Models.PDFPreviewSourceResponse {
  error?: JobError;
}

export interface ImagePreviewSourceResponse
  extends Models.ImagePreviewSourceResponse {
  error?: JobError;
}

export type ImageCropPreviewRequest = Models.ImageCropPreviewRequest;

export interface ImageCropPreviewResponse
  extends Models.ImageCropPreviewResponse {
  error?: JobError;
}

export interface ImageCropSuggestionResponse
  extends Models.ImageCropSuggestionResponse {
  error?: JobError;
}

/** A base name derived from document metadata; the page applies it on confirm. */
export interface OutputNameSuggestionResponse
  extends Models.OutputNameSuggestionResponse {
  error?: JobError;
}

export type ImageAnnotateOperationType =
  | 'text'
  | 'arrow'
  | 'rect'
  | 'blur'
  | 'redact';

/** The operation type is a closed set defined by the tool options. */
export interface ImageAnnotateOperation extends Models.ImageAnnotateOperation {
  type: ImageAnnotateOperationType;
}

export interface ImageAnnotatePreviewRequest
  extends Models.ImageAnnotatePreviewRequest {
  operations: ImageAnnotateOperation[];
}

export interface ImageAnnotatePreviewResponse
  extends Models.ImageAnnotatePreviewResponse {
  error?: JobError;
}

/** UI-only shape for the Markdown → PDF options panel; not a wire model. */
export interface HeaderFooterConfig {
  enabled: boolean;
  text: string;
  align: 'left' | 'center' | 'right';
  font: 'helvetica' | 'times' | 'courier';
  marginTop: number;
  marginBottom: number;
  color: string;
}

/** UI-only shape for the Markdown → PDF options panel; not a wire model. */
export interface MarkdownToPdfOptions {
  outputPath?: string;
  header: HeaderFooterConfig;
  footer: HeaderFooterConfig;
}

export type VerificationCheck = Models.VerificationCheck;

export interface VerifiedOutput extends Models.VerifiedOutput {
  checks: VerificationCheck[];
}

export interface VerificationReport extends Models.VerificationReport {
  outputs: VerifiedOutput[];
  warnings?: string[];
}

export type JobOutputRecord = Models.JobOutputRecord;

export interface JobHistoryEntry extends Models.JobHistoryEntry {
  outputs: JobOutputRecord[];
}

/**
 * History load result. A failed IPC call must stay distinguishable from an
 * empty history so the UI can show an error state instead of "no jobs".
 */
export interface ListJobHistoryResponse {
  success: boolean;
  message: string;
  entries: JobHistoryEntry[];
}

export type TrashInfo = Models.TrashInfo;

/** Trash stats plus the transport outcome; `files`/`bytes` are real files. */
export interface TrashInfoResponse extends TrashInfo {
  success: boolean;
  message: string;
}

export type UndoSkipped = Models.UndoSkipped;

export interface UndoResult extends Models.UndoResult {
  moved: string[];
  skipped: UndoSkipped[];
}

export type RecipeStep = Models.RecipeStep;

export type RecipeOptionKind = Models.RecipeOptionKind;
export type RecipeOptionChoice = Models.RecipeOptionChoice;

export interface RecipeOptionField extends Models.RecipeOptionField {
  values?: RecipeOptionChoice[];
}

export interface Recipe extends Models.Recipe {
  acceptExtensions: string[];
  highlights?: string[];
  steps: RecipeStep[];
  optionSchema?: RecipeOptionField[];
  optionOverrides?: Record<string, unknown>;
}

export interface SaveUserRecipeRequest extends Models.SaveUserRecipeRequest {
  options: Record<string, unknown>;
}

export interface UserRecipeResponse extends Models.UserRecipeResponse {
  recipe?: Recipe;
  error?: JobError;
}

export type ExportUserRecipesResponse = Models.ExportUserRecipesResponse;

export type ImportUserRecipeIssue = Models.ImportUserRecipeIssue;

export interface ImportUserRecipesResponse
  extends Models.ImportUserRecipesResponse {
  invalid: ImportUserRecipeIssue[];
  error?: JobError;
}

export interface RecipeRequest extends Models.RecipeRequest {
  inputPaths: string[];
  options: Record<string, unknown>;
}

export interface PlannedOutput extends Models.PlannedOutput {
  warnings?: string[];
}

export interface RecipePlanResponse extends Models.RecipePlanResponse {
  recipe?: Recipe;
  inputPaths: string[];
  outputs: PlannedOutput[];
  warnings?: string[];
  error?: JobError;
}

export interface RecipeStepResult extends Models.RecipeStepResult {
  outputs?: string[];
  error?: JobError;
}

export interface RecipeResult extends Models.RecipeResult {
  steps: RecipeStepResult[];
  outputs: string[];
  verification?: VerificationReport;
  error?: JobError;
}

export interface RunRecipeResponse extends Models.RunRecipeResponse {
  error?: JobError;
}

export interface RecipeStatusResponse extends Models.RecipeStatusResponse {
  result?: RecipeResult;
  error?: JobError;
}

export interface ListRecipesResponse extends Models.ListRecipesResponse {
  recipes: Recipe[];
}

export type RecipeProgressEvent = Models.RecipeProgressEvent;

/**
 * One watch-folder rule. `runtimeStatus`/`runtimeReason` are response-only
 * dynamic state: the backend never persists them.
 */
export interface AutomationRule extends Models.AutomationRule {
  extensions?: string[];
  patterns?: string[];
  options?: Record<string, unknown>;
  lastOutputs?: string[];
}

export interface ListAutomationRulesResponse
  extends Models.ListAutomationRulesResponse {
  rules: AutomationRule[];
}

export interface AutomationRuleActionResponse
  extends Models.AutomationRuleActionResponse {
  rule?: AutomationRule;
  error?: JobError;
}

/** One planned organize move; `destination` is empty for a skipped item. */
export type OrganizePlannedMove = Models.OrganizePlannedMove;

/**
 * Dry-run plan of an organize rule. The generated model types `moves` as
 * nullable, so the UI contract narrows it to an array.
 */
export interface AutomationOrganizePreviewResponse
  extends Models.AutomationOrganizePreviewResponse {
  moves: OrganizePlannedMove[];
  error?: JobError;
}

export type AutomationStatusResponse = Models.AutomationStatusResponse;

export type AutomationRunEvent = Models.AutomationRunEvent;

/**
 * Detected state of the local OCR pipeline. `languages` excludes osd; the UI
 * always iterates a list, so the nullable wire slice is narrowed here.
 */
export interface OCRCapabilitiesResponse extends Models.OCRCapabilitiesResponse {
  languages: string[];
}

/**
 * Detected state of the local Whisper transcription pipeline. `languages` is
 * narrowed for the same reason as the OCR capabilities.
 */
export interface TranscriptionCapabilitiesResponse
  extends Models.TranscriptionCapabilitiesResponse {
  languages: string[];
}

export type LocalCapability = Models.LocalCapability;

/**
 * Snapshot of the local runtime surface. Optional missing tools are guidance,
 * never an app failure.
 */
export interface LocalCapabilitiesResponse
  extends Models.LocalCapabilitiesResponse {
  checks: LocalCapability[];
  ocrLanguages: string[];
}

export type AppInfoResponse = Models.AppInfoResponse;

/** One terminal automation run kept in the local run history. */
export interface AutomationRunRecord extends Models.AutomationRunRecord {
  outputs?: string[];
}

export interface ListAutomationRunsResponse
  extends Models.ListAutomationRunsResponse {
  runs: AutomationRunRecord[];
}

export type AutomationRunsActionResponse = Models.AutomationRunsActionResponse;

/**
 * Read-only duplicate scan request. Directories are folders, not files, and
 * nothing on disk is ever written.
 */
export type DuplicateScanRequest = Models.DuplicateScanRequest;

/** Generated empty slices model as `null`; the UI always iterates a list. */
export interface DuplicateScanGroup extends Models.DuplicateScanGroup {
  paths: string[];
}

export type DuplicateScanTotals = Models.DuplicateScanTotals;

export type DuplicateScanWarning = Models.DuplicateScanWarning;

export type DuplicateScanProgressEvent = Models.DuplicateScanProgressEvent;

export interface DuplicateScanState extends Models.DuplicateScanState {
  groups: DuplicateScanGroup[];
  warnings: DuplicateScanWarning[];
}

export type DuplicateStartResponse = Models.DuplicateStartResponse;

export interface DuplicateScanStatusResponse
  extends Models.DuplicateScanStatusResponse {
  scan?: DuplicateScanState;
}

export interface DuplicateScanListResponse
  extends Models.DuplicateScanListResponse {
  scans: DuplicateScanState[];
}

export type DuplicateScanActionResponse = Models.DuplicateScanActionResponse;

export type ToolchainComponent = Models.ToolchainComponent;

/**
 * Managed toolchain snapshot. It is safe to poll while an install runs; a
 * failed install reports `success: false` with `error` set, while an
 * unsupported platform reports `success: true` with a message and no ready
 * components.
 */
export interface ToolchainStatus extends Models.ToolchainStatus {
  components: ToolchainComponent[];
  ocrLanguages: string[];
}

/** Install selection; an empty model means `small`, empty languages mean both. */
export interface ToolchainInstallOptions extends Models.ToolchainInstallOptions {
  ocrLanguages: string[];
}

export type ToolchainInstallEvent = Models.ToolchainInstallEvent;

export type PathActionResponse = Models.PathActionResponse;

export interface InspectFilesResponse extends Models.InspectFilesResponse {
  files: FileInfo[];
}

export type FileInfo = Models.FileInfo;

export type MediaSourceResponse = Models.MediaSourceResponse;

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

  // Duplicate scans emit progress and a full snapshot like a job, but carry a
  // scan id instead of a job id. Same lazy lifecycle and replay contract.
  private duplicateProgressSubject: ReplaySubject<DuplicateScanProgressEvent> | null =
    null;
  private duplicateProgressUnsubscribe: (() => void) | null = null;
  private duplicateProgressRefCount = 0;

  private duplicateStateSubject: ReplaySubject<DuplicateScanState> | null =
    null;
  private duplicateStateUnsubscribe: (() => void) | null = null;
  private duplicateStateRefCount = 0;

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

  // Duplicate scan streams. A late subscriber gets the last snapshot replayed;
  // the page filters by scan id because a replay may belong to an older scan.
  public readonly duplicateProgress$: Observable<DuplicateScanProgressEvent> =
    new Observable((subscriber) => {
      const unregister = this.ensureDuplicateProgressRegistered();

      const sub = this.duplicateProgressSubject!.subscribe({
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

  public readonly duplicateState$: Observable<DuplicateScanState> =
    new Observable((subscriber) => {
      const unregister = this.ensureDuplicateStateRegistered();

      const sub = this.duplicateStateSubject!.subscribe({
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
      const response = await this.ipc.listTools();
      return { ...response, tools: response.tools ?? [] };
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
      const response = await this.ipc.validateJob(request);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.runJob(request);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.cancelJob(jobId);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.getJobStatus(jobId);
      return {
        ...response,
        result: response.result ?? undefined,
        error: this.optionalError(response.error),
      };
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
      const response = await this.ipc.listJobs(limit);
      return { ...response, jobs: response.jobs ?? [] };
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
      return this.normalizeVerificationReport(await this.ipc.verifyJob(jobId));
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Verify job failed', error),
        outputs: [],
      };
    }
  }

  async listJobHistory(): Promise<ListJobHistoryResponse> {
    try {
      const entries = await this.ipc.listJobHistory();
      return {
        success: true,
        message: '',
        entries: entries.map((entry) => ({
          ...entry,
          outputs: entry.outputs ?? [],
        })),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List job history failed', error),
        entries: [],
      };
    }
  }

  async trashInfo(): Promise<TrashInfoResponse> {
    try {
      const info = await this.ipc.trashInfo();
      return {
        success: true,
        message: '',
        files: info?.files ?? 0,
        bytes: info?.bytes ?? 0,
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Read trash info failed', error),
        files: 0,
        bytes: 0,
      };
    }
  }

  async undoJob(jobId: string): Promise<UndoResult> {
    try {
      const response = await this.ipc.undoJob(jobId);
      return {
        ...response,
        moved: response.moved ?? [],
        skipped: response.skipped ?? [],
      };
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
      const response = await this.ipc.restoreJob(jobId);
      return {
        ...response,
        moved: response.moved ?? [],
        skipped: response.skipped ?? [],
      };
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
      const response = await this.ipc.getPdfPreviewSource(inputPath);
      return { ...response, error: this.optionalError(response.error) };
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
      return await this.ipc.registerMediaPath(path);
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
      const response = await this.ipc.getImagePreviewSource(inputPath);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.getImageCropPreview(request);
      return { ...response, error: this.optionalError(response.error) };
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

  async getImageCropSuggestion(
    inputPath: string,
  ): Promise<ImageCropSuggestionResponse> {
    try {
      const response = await this.ipc.getImageCropSuggestion(inputPath);
      return { ...response, error: this.optionalError(response.error) };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get image crop suggestion failed', error),
        found: false,
        x: 0,
        y: 0,
        width: 0,
        height: 0,
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'IMAGE_CROP_SUGGESTION_READ_FAILED',
          error,
        ),
      };
    }
  }

  async suggestOutputName(
    toolId: string,
    inputPath: string,
  ): Promise<OutputNameSuggestionResponse> {
    try {
      const response = await this.ipc.suggestOutputName(toolId, inputPath);
      return { ...response, error: this.optionalError(response.error) };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Suggest output name failed', error),
        found: false,
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'OUTPUT_NAME_SUGGESTION_READ_FAILED',
          error,
        ),
      };
    }
  }

  async getImageAnnotatePreview(
    request: ImageAnnotatePreviewRequest,
  ): Promise<ImageAnnotatePreviewResponse> {
    try {
      const response = await this.ipc.getImageAnnotatePreview(request);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.planJob(request);
      return {
        ...response,
        outputs: (response.outputs ?? []).map((output) => ({
          ...output,
          warnings: output.warnings ?? undefined,
        })),
        effectiveOptions: response.effectiveOptions ?? undefined,
        warnings: response.warnings ?? undefined,
        error: this.optionalError(response.error),
      };
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
      const response = await this.ipc.inspectFiles(paths);
      return { ...response, files: response.files ?? [] };
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
      const response = await this.ipc.getOcrCapabilities();
      return { ...response, languages: response.languages ?? [] };
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
      const response = await this.ipc.getTranscriptionCapabilities(
        whisperPath,
        modelPath,
      );
      return { ...response, languages: response.languages ?? [] };
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
      const response = await this.ipc.getLocalCapabilities();
      return {
        ...response,
        checks: response.checks ?? [],
        ocrLanguages: response.ocrLanguages ?? [],
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get local capabilities failed', error),
        checks: [],
        ocrLanguages: [],
      };
    }
  }

  async getAppInfo(): Promise<AppInfoResponse> {
    try {
      return await this.ipc.getAppInfo();
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get app info failed', error),
        version: 'dev',
        commit: 'none',
        builtAt: 'unknown',
        release: false,
        updateKeyConfigured: false,
      };
    }
  }

  async getToolchainStatus(): Promise<ToolchainStatus> {
    try {
      return this.normalizeToolchainStatus(await this.ipc.getToolchainStatus());
    } catch (error) {
      return this.toolchainFallback('Get toolchain status failed', error);
    }
  }

  async getToolchainInstallState(): Promise<ToolchainStatus> {
    try {
      return this.normalizeToolchainStatus(
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
      return this.normalizeToolchainStatus(
        await this.ipc.startToolchainInstall(options),
      );
    } catch (error) {
      return this.toolchainFallback('Start toolchain install failed', error);
    }
  }

  async cancelToolchainInstall(): Promise<ToolchainStatus> {
    try {
      return this.normalizeToolchainStatus(
        await this.ipc.cancelToolchainInstall(),
      );
    } catch (error) {
      return this.toolchainFallback('Cancel toolchain install failed', error);
    }
  }

  async listRecipes(): Promise<ListRecipesResponse> {
    try {
      const response = await this.ipc.listRecipes();
      return {
        ...response,
        recipes: (response.recipes ?? []).map((recipe) =>
          this.normalizeRecipe(recipe),
        ),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List recipes failed', error),
        recipes: [],
      };
    }
  }

  async planRecipe(request: RecipeRequest): Promise<RecipePlanResponse> {
    try {
      const response = await this.ipc.planRecipe(request);
      return {
        ...response,
        recipe: response.recipe
          ? this.normalizeRecipe(response.recipe)
          : undefined,
        inputPaths: response.inputPaths ?? [],
        outputs: (response.outputs ?? []).map((output) => ({
          ...output,
          warnings: output.warnings ?? undefined,
        })),
        warnings: response.warnings ?? undefined,
        error: this.optionalError(response.error),
      };
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
      const response = await this.ipc.runRecipe(request);
      return { ...response, error: this.optionalError(response.error) };
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
      const response = await this.ipc.getRecipeStatus(jobId);
      return {
        ...response,
        result: response.result
          ? this.normalizeRecipeResult(response.result)
          : undefined,
        error: this.optionalError(response.error),
      };
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
      const results = await this.ipc.listRecipeRuns(limit);
      return results.map((result) => this.normalizeRecipeResult(result));
    } catch {
      return [];
    }
  }

  async cancelRecipe(jobId: string): Promise<RecipeStatusResponse> {
    try {
      const response = await this.ipc.cancelRecipe(jobId);
      return {
        ...response,
        result: response.result
          ? this.normalizeRecipeResult(response.result)
          : undefined,
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        found: false,
        message: this.formatMessage('Cancel recipe failed', error),
      };
    }
  }

  async saveUserRecipe(
    request: SaveUserRecipeRequest,
  ): Promise<UserRecipeResponse> {
    try {
      const response = await this.ipc.saveUserRecipe(request);
      return {
        ...response,
        recipe: response.recipe
          ? this.normalizeRecipe(response.recipe)
          : undefined,
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Save user recipe failed', error),
        error: this.defaultError(
          'VALIDATION_INVALID_INPUT',
          'RECIPE_USER_SAVE_FAILED',
          error,
        ),
      };
    }
  }

  async renameUserRecipe(
    id: string,
    name: string,
  ): Promise<UserRecipeResponse> {
    try {
      const response = await this.ipc.renameUserRecipe(id, name);
      return {
        ...response,
        recipe: response.recipe
          ? this.normalizeRecipe(response.recipe)
          : undefined,
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Rename user recipe failed', error),
        error: this.defaultError(
          'VALIDATION_INVALID_INPUT',
          'RECIPE_USER_RENAME_FAILED',
          error,
        ),
      };
    }
  }

  async deleteUserRecipe(id: string): Promise<UserRecipeResponse> {
    try {
      const response = await this.ipc.deleteUserRecipe(id);
      return {
        ...response,
        recipe: response.recipe
          ? this.normalizeRecipe(response.recipe)
          : undefined,
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Delete user recipe failed', error),
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'RECIPE_USER_DELETE_FAILED',
          error,
        ),
      };
    }
  }

  async exportUserRecipes(
    path: string,
    ids: string[],
  ): Promise<ExportUserRecipesResponse> {
    try {
      const response = await this.ipc.exportUserRecipes(path, ids);
      return { ...response, error: this.optionalError(response.error) };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Export user recipes failed', error),
        path: '',
        exported: 0,
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'RECIPE_EXPORT_FAILED',
          error,
        ),
      };
    }
  }

  async importUserRecipes(
    path: string,
    overwrite: boolean,
  ): Promise<ImportUserRecipesResponse> {
    try {
      const response = await this.ipc.importUserRecipes(path, overwrite);
      return {
        ...response,
        invalid: response.invalid ?? [],
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Import user recipes failed', error),
        imported: 0,
        skipped: 0,
        invalid: [],
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'RECIPE_IMPORT_READ_FAILED',
          error,
        ),
      };
    }
  }

  async listAutomationRules(): Promise<ListAutomationRulesResponse> {
    try {
      const response = await this.ipc.listAutomationRules();
      return {
        ...response,
        rules: (response.rules ?? []).map((rule) =>
          this.normalizeAutomationRule(rule),
        ),
      };
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
      const response = await this.ipc.saveAutomationRule(rule);
      return {
        ...response,
        rule: response.rule
          ? this.normalizeAutomationRule(response.rule)
          : undefined,
        error: this.optionalError(response.error),
      };
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
      const response = await this.ipc.deleteAutomationRule(id);
      return {
        ...response,
        rule: response.rule
          ? this.normalizeAutomationRule(response.rule)
          : undefined,
        error: this.optionalError(response.error),
      };
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
      const response = await this.ipc.setAutomationRuleEnabled(id, enabled);
      return {
        ...response,
        rule: response.rule
          ? this.normalizeAutomationRule(response.rule)
          : undefined,
        error: this.optionalError(response.error),
      };
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

  async previewAutomationRule(
    rule: AutomationRule,
  ): Promise<AutomationOrganizePreviewResponse> {
    try {
      const response = await this.ipc.previewAutomationRule(rule);
      return {
        ...response,
        moves: (response.moves ?? []).map((move) => ({ ...move })),
        error: this.optionalError(response.error),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Preview automation rule failed', error),
        moves: [],
        previewStored: false,
        error: this.defaultError(
          'EXEC_IO_TRANSIENT',
          'AUTOMATION_RULE_PREVIEW_FAILED',
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
      const response = await this.ipc.listAutomationRuns(limit);
      return {
        ...response,
        runs: (response.runs ?? []).map((run) => ({
          ...run,
          outputs: run.outputs ?? undefined,
        })),
      };
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

  async startDuplicateScan(
    request: DuplicateScanRequest,
  ): Promise<DuplicateStartResponse> {
    try {
      return await this.ipc.startDuplicateScan(request);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Start duplicate scan failed', error),
      };
    }
  }

  async getDuplicateScan(scanId: string): Promise<DuplicateScanStatusResponse> {
    try {
      const response = await this.ipc.getDuplicateScan(scanId);
      return {
        ...response,
        scan: response.scan
          ? this.normalizeDuplicateScanState(response.scan)
          : undefined,
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Get duplicate scan failed', error),
        found: false,
      };
    }
  }

  async cancelDuplicateScan(
    scanId: string,
  ): Promise<DuplicateScanActionResponse> {
    try {
      return await this.ipc.cancelDuplicateScan(scanId);
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('Cancel duplicate scan failed', error),
        scanId,
      };
    }
  }

  async listDuplicateScans(): Promise<DuplicateScanListResponse> {
    try {
      const response = await this.ipc.listDuplicateScans();
      return {
        ...response,
        scans: (response.scans ?? []).map((scan) =>
          this.normalizeDuplicateScanState(scan),
        ),
      };
    } catch (error) {
      return {
        success: false,
        message: this.formatMessage('List duplicate scans failed', error),
        scans: [],
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

  async openImportFileDialog(): Promise<string> {
    try {
      return (await this.ipc.openImportFileDialog()) ?? '';
    } catch {
      return '';
    }
  }

  async saveFileDialog(defaultFilename: string): Promise<string> {
    try {
      return (await this.ipc.saveFileDialog(defaultFilename)) ?? '';
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

          const candidate = payload as Partial<Models.JobResult>;
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
            error: this.optionalError(candidate.error),
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

          const candidate = payload as Partial<Models.RecipeResult>;
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
            steps: Array.isArray(candidate.steps)
              ? candidate.steps.map((step) => ({
                  ...step,
                  outputs: step.outputs ?? undefined,
                  error: this.optionalError(step.error),
                }))
              : [],
            outputs: Array.isArray(candidate.outputs)
              ? candidate.outputs.filter(
                  (output): output is string => typeof output === 'string',
                )
              : [],
            targetSizeBytes:
              typeof candidate.targetSizeBytes === 'number'
                ? candidate.targetSizeBytes
                : undefined,
            verification: candidate.verification
              ? this.normalizeVerificationReport(candidate.verification)
              : undefined,
            error: this.optionalError(candidate.error),
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

  // Duplicate scan progress only needs a scan id and a progress payload, the
  // same minimum the job progress stream checks for.
  private ensureDuplicateProgressRegistered(): () => void {
    if (!this.duplicateProgressSubject) {
      this.duplicateProgressSubject =
        new ReplaySubject<DuplicateScanProgressEvent>(1);
      this.duplicateProgressUnsubscribe = Events.On(
        'duplicates/progress',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<DuplicateScanProgressEvent>;
          if (
            typeof candidate.scanId !== 'string' ||
            candidate.scanId.trim().length === 0 ||
            !candidate.progress
          ) {
            return;
          }

          this.duplicateProgressSubject?.next({
            scanId: candidate.scanId,
            kind: typeof candidate.kind === 'string' ? candidate.kind : '',
            status:
              typeof candidate.status === 'string'
                ? candidate.status
                : 'running',
            progress: candidate.progress,
          });
        },
      );
    }

    this.duplicateProgressRefCount += 1;

    return () => {
      this.duplicateProgressRefCount -= 1;
      if (this.duplicateProgressRefCount <= 0) {
        try {
          this.duplicateProgressUnsubscribe?.();
        } catch {
          // ignore
        }
        this.duplicateProgressUnsubscribe = null;
        try {
          this.duplicateProgressSubject?.complete();
        } catch {
          // ignore
        }
        this.duplicateProgressSubject = null;
        this.duplicateProgressRefCount = 0;
      }
    };
  }

  // A duplicate snapshot needs a scan id and a status; the lists are normalized
  // so consumers never branch on null.
  private ensureDuplicateStateRegistered(): () => void {
    if (!this.duplicateStateSubject) {
      this.duplicateStateSubject = new ReplaySubject<DuplicateScanState>(1);
      this.duplicateStateUnsubscribe = Events.On(
        'duplicates/state',
        (ev: { data?: unknown }) => {
          const payload = ev?.data;
          if (!payload || typeof payload !== 'object') {
            return;
          }

          const candidate = payload as Partial<Models.DuplicateScanState>;
          if (
            typeof candidate.scanId !== 'string' ||
            candidate.scanId.trim().length === 0 ||
            typeof candidate.status !== 'string' ||
            candidate.status.trim().length === 0
          ) {
            return;
          }

          this.emitDuplicateState(
            this.normalizeDuplicateScanState(
              candidate as Models.DuplicateScanState,
            ),
          );
        },
      );
    }

    this.duplicateStateRefCount += 1;

    return () => {
      this.duplicateStateRefCount -= 1;
      if (this.duplicateStateRefCount <= 0) {
        try {
          this.duplicateStateUnsubscribe?.();
        } catch {
          // ignore
        }
        this.duplicateStateUnsubscribe = null;
        try {
          this.duplicateStateSubject?.complete();
        } catch {
          // ignore
        }
        this.duplicateStateSubject = null;
        this.duplicateStateRefCount = 0;
      }
    };
  }

  // Native events may arrive outside the Angular zone; the snapshot is emitted
  // inside it so signal consumers re-render.
  private emitDuplicateState(snapshot: DuplicateScanState): void {
    try {
      this.zone.run(() => this.duplicateStateSubject?.next(snapshot));
    } catch {
      // ignore
    }
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

  // Generated models use nullable slices for empty Go values; the UI contract
  // narrows them to empty lists so components never branch on null.
  private optionalError(error?: Models.JobError | null): JobError | undefined {
    return error ?? undefined;
  }

  private normalizeRecipe(recipe: Models.Recipe): Recipe {
    return {
      ...recipe,
      acceptExtensions: recipe.acceptExtensions ?? [],
      highlights: recipe.highlights ?? undefined,
      steps: recipe.steps ?? [],
      optionSchema: (recipe.optionSchema ?? undefined) as
        | RecipeOptionField[]
        | undefined,
      optionOverrides: (recipe.optionOverrides ?? undefined) as
        | Record<string, unknown>
        | undefined,
    };
  }

  private normalizeRecipeResult(result: Models.RecipeResult): RecipeResult {
    return {
      ...result,
      steps: (result.steps ?? []).map((step) => ({
        ...step,
        outputs: step.outputs ?? undefined,
        error: this.optionalError(step.error),
      })),
      outputs: result.outputs ?? [],
      verification: result.verification
        ? this.normalizeVerificationReport(result.verification)
        : undefined,
      error: this.optionalError(result.error),
    };
  }

  private normalizeAutomationRule(rule: Models.AutomationRule): AutomationRule {
    return {
      ...rule,
      extensions: rule.extensions ?? undefined,
      patterns: rule.patterns ?? undefined,
      options: rule.options ?? undefined,
      lastOutputs: rule.lastOutputs ?? undefined,
    };
  }

  private normalizeDuplicateScanState(
    scan: Models.DuplicateScanState,
  ): DuplicateScanState {
    return {
      ...scan,
      groups: (scan.groups ?? []).map((group) => ({
        ...group,
        paths: group.paths ?? [],
      })),
      warnings: scan.warnings ?? [],
    };
  }

  private normalizeVerificationReport(
    report: Models.VerificationReport,
  ): VerificationReport {
    return {
      ...report,
      outputs: (report.outputs ?? []).map((output) => ({
        ...output,
        checks: output.checks ?? [],
        warnings: output.warnings ?? undefined,
      })),
      warnings: report.warnings ?? undefined,
    };
  }

  private normalizeToolchainStatus(
    status: Models.ToolchainStatus,
  ): ToolchainStatus {
    return {
      ...status,
      components: status.components ?? [],
      ocrLanguages: status.ocrLanguages ?? [],
    };
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
