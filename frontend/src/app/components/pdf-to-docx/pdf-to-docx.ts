import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Subscription } from 'rxjs';
import {
  Bold,
  FolderOpen,
  Image as ImageIcon,
  LayoutTemplate,
  type LucideIconData,
  LucideAngularModule,
  Palette,
  ShieldCheck,
  Table,
  TriangleAlert,
  Type,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  InspectFilesResponse,
  JobError,
  JobExecutionStatus,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  RunJobResponse,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { observeJob } from '../../utils/job-stream';
import { resumeToolJob } from '../../utils/resume-task';

const PDF_TO_DOCX_TOOL_ID = 'tool.pdf.to_docx';
const PLAN_DEBOUNCE_MS = 250;

/** Backend codes that mean the managed pdf2docx engine is not installed. */
const RUNTIME_ERROR_CODES = new Set([
  'PDF_DOCX_RUNTIME_UNAVAILABLE',
  'RUNTIME_DEP_MISSING',
]);

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

export interface PlanBanner {
  title: string;
  detail: string;
  showSettings: boolean;
}

export interface ResultSummary {
  tone: 'success' | 'warning';
  text: string;
}

export interface FidelityFeature {
  icon: LucideIconData;
  title: string;
  detail: string;
}

/** The fidelity contract shown on the page; matches what pdf2docx preserves. */
export const FIDELITY_FEATURES: readonly FidelityFeature[] = [
  { icon: Type, title: 'Fonts & sizes', detail: 'Family, size and line height' },
  {
    icon: Bold,
    title: 'Bold & italic',
    detail: 'Weight, italic, underline and strike-through',
  },
  {
    icon: Palette,
    title: 'Colors & alignment',
    detail: 'Text color, highlights and paragraph alignment',
  },
  { icon: ImageIcon, title: 'Images', detail: 'Inline and floating images' },
  { icon: Table, title: 'Tables', detail: 'Borders, shading and merged cells' },
  {
    icon: LayoutTemplate,
    title: 'Page layout',
    detail: 'Page size, margins and simple columns',
  },
];

@Component({
  selector: 'app-pdf-to-docx',
  standalone: true,
  imports: [
    RouterLink,
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-to-docx.html',
  styleUrl: './pdf-to-docx.css',
})
export class PdfToDocx implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly fidelityFeatures = FIDELITY_FEATURES;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly outputDir = signal(this.preferences.defaultOutputDir().trim());

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planError = signal<JobError | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly pdfCountLabel = computed(() =>
    formatCount(this.selectedPaths().length, 'PDF'),
  );

  /** Actionable copy for a plan failure, with a Settings route for the engine. */
  readonly planBanner = computed<PlanBanner | null>(() => {
    const error = this.planError();
    if (!error) {
      return null;
    }

    const codes = [error.detail_code, error.code]
      .filter((code): code is string => typeof code === 'string')
      .map((code) => code.trim().toUpperCase());
    if (codes.some((code) => RUNTIME_ERROR_CODES.has(code))) {
      return {
        title: 'The PDF to DOCX engine is not installed.',
        detail: 'Run Install / repair in Settings, then try again.',
        showSettings: true,
      };
    }

    const message =
      typeof error.message === 'string' ? error.message.trim() : '';

    return {
      title: 'The output could not be planned.',
      detail: message || 'Try again with another PDF.',
      showSettings: false,
    };
  });

  readonly canRun = computed(
    () =>
      this.selectedPaths().length === 1 &&
      this.plan()?.success === true &&
      !this.planError() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() =>
    this.isSubmitting() || this.isPolling()
      ? 'Converting…'
      : 'Convert to DOCX',
  );

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to the PDF',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return 'Add one PDF to convert.';
    }
    if (this.planLoading()) {
      return 'Planning the DOCX output…';
    }
    if (!this.plan()?.success) {
      return 'The DOCX output cannot be planned yet.';
    }
    return '';
  });

  readonly terminalJob = computed<JobResult | null>(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
  });

  /** Backend stats line for the converted DOCX, e.g. paragraph and image counts. */
  readonly resultSummary = computed<ResultSummary | null>(() => {
    const job = this.terminalJob();
    if (!job) {
      return null;
    }

    const item = (job.items ?? []).find(
      (candidate) => candidate.success && candidate.message?.trim(),
    );
    if (!item) {
      return null;
    }

    const text = item.message.trim();
    return {
      tone: /no text|scanned/i.test(text) ? 'warning' : 'success',
      text,
    };
  });

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? describeToolError(error, 'The conversion could not start.')
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {}

  ngOnInit(): void {
    const pending = this.dropIntake.take('/pdf-to-docx');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const pdfs: string[] = [];
    const rejected: string[] = [];
    for (const path of incoming) {
      if (fileExtension(path).toLowerCase() === 'pdf') {
        if (!pdfs.includes(path)) {
          pdfs.push(path);
        }
      } else {
        rejected.push(path);
      }
    }

    if (pdfs.length === 0) {
      this.notice.set(this.intakeNotice(rejected, 0, ''));
      return;
    }

    const selected = pdfs[0];
    this.notice.set(
      this.intakeNotice(rejected, pdfs.length - 1, fileName(selected)),
    );
    this.actionMessage.set('');
    this.selectedPaths.set([selected]);
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  async chooseFile(): Promise<void> {
    const path = (await this.wailsService.openFileDialog()).trim();
    if (path.length > 0) {
      this.onPathsSelected([path]);
    }
  }

  clear(): void {
    if (this.selectedPaths().length === 0) {
      return;
    }
    this.selectedPaths.set([]);
    this.notice.set(null);
    this.actionMessage.set('');
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  removeFile(path: string): void {
    if (!this.selectedPaths().includes(path)) {
      return;
    }
    this.selectedPaths.set([]);
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  usePdfFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.schedulePlanRefresh();
  }

  async run(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for the output name, so it is always
    // refreshed with the current input before the job starts.
    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponse = await this.wailsService
      .runJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The conversion could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The conversion could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The conversion could not start.',
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    this.activeJobId.set(response.jobId);
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: 'Waiting to start',
      toolId: PDF_TO_DOCX_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total: 1,
        stage: 'queued',
        message: 'Waiting to start',
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isSubmitting.set(false);
    this.isPolling.set(true);
    this.startObserving();
  }

  async cancel(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }
    const response = await this.wailsService.cancelJob(jobId);
    if (!response.success) {
      this.actionMessage.set(
        response.message || 'The job could not be cancelled.',
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success ? '' : response.message || 'The file could not be opened.',
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || 'The folder could not be opened.',
    );
  }

  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? []).some((item) => !item.success);
    if (!failed) {
      return;
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    await this.run();
  }

  reset(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.plan.set(null);
    this.planError.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  private buildRequest(): JobRequest {
    return {
      toolId: PDF_TO_DOCX_TOOL_ID,
      mode: 'single',
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options: {},
    };
  }

  private async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (this.selectedPaths().length !== 1) {
      this.planToken += 1;
      this.plan.set(null);
      this.planError.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);
    this.planError.set(null);

    const response: PlanJobResponse = await this.wailsService
      .planJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: 'The output could not be planned.',
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: 'The output could not be planned.',
        },
      }));
    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobError = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || 'The output could not be planned.',
    };
    // Keep the failed plan so the shared plan card can show the reason next to
    // the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private schedulePlanRefresh(): void {
    this.planToken += 1;
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, PLAN_DEBOUNCE_MS);
  }

  private clearPlanRefreshTimer(): void {
    if (this.planRefreshTimer) {
      clearTimeout(this.planRefreshTimer);
      this.planRefreshTimer = null;
    }
  }

  private resumeBackgroundJob(): void {
    resumeToolJob({
      activity: this.activity,
      toolId: PDF_TO_DOCX_TOOL_ID,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startObserving: () => this.startObserving(),
    });
  }

  private startObserving(): void {
    this.stopObserving();
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    this.isPolling.set(true);
    this.jobSubscription = observeJob(this.wailsService, jobId, {
      onProgress: (event) => this.applyJobProgress(event),
      onState: (result) => this.applyJobStatus(result),
      onMissing: (response) => this.handleMissingJob(response),
    });
  }

  private stopObserving(): void {
    if (this.jobSubscription) {
      this.jobSubscription.unsubscribe();
      this.jobSubscription = null;
    }
    this.isPolling.set(false);
  }

  private applyJobProgress(event: JobProgressEvent): void {
    if (!event) {
      return;
    }

    const current = this.jobResult();
    if (!current) {
      return;
    }

    this.jobResult.set({
      ...current,
      message: event.progress.message,
      progress: event.progress,
      // The terminal snapshot arrives on jobs/state; a progress event can
      // overtake the complete result, so the current status is kept.
      status: this.isTerminalStatus(event.status)
        ? current.status
        : (event.status as JobExecutionStatus),
    });

    if (this.isTerminalStatus(event.status)) {
      void this.reconcileJobStatus();
    }
  }

  private applyJobStatus(result: JobResult): void {
    if (!result) {
      return;
    }

    this.jobResult.set(result);
    if (this.isTerminalStatus(result.status)) {
      this.stopObserving();
      this.activeJobId.set('');
      void this.loadOutputInfos();
    }
  }

  /** The backend no longer tracks the adopted job (restart or retention). */
  private handleMissingJob(response: JobStatusResponse): void {
    if (!this.activeJobId()) {
      return;
    }

    this.stopObserving();
    this.activeJobId.set('');
    this.jobError.set(
      response.error ?? {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'JOB_STATUS_MISSING',
        message: response.message || 'The job status could not be read.',
      },
    );
  }

  /**
   * One-shot recovery when a terminal progress event arrives before its
   * jobs/state snapshot. This is never a timer, and it is a no-op once the
   * page has already settled the job.
   */
  private async reconcileJobStatus(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      return;
    }

    const response = await this.wailsService
      .getJobStatus(jobId)
      .catch(() => null);
    if (!response || this.activeJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.handleMissingJob(response);
      return;
    }

    this.applyJobStatus(response.result);
  }

  private async loadOutputInfos(): Promise<void> {
    const paths = this.collectOutputPaths();
    if (paths.length === 0) {
      this.outputInfos.set({});
      return;
    }

    const response: InspectFilesResponse = await this.wailsService
      .inspectFiles(paths)
      .catch(() => ({
        success: false,
        message: 'File sizes could not be read.',
        files: [],
      }));
    if (!response.success) {
      this.outputInfos.set({});
      return;
    }

    const infos: Record<string, FileInfo> = {};
    for (const info of response.files) {
      if (info.path) {
        infos[info.path] = info;
      }
    }
    this.outputInfos.set(infos);
  }

  private collectOutputPaths(): string[] {
    const paths = new Set<string>();
    for (const item of this.jobResult()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs =
        item.outputs && item.outputs.length > 0
          ? item.outputs
          : item.outputPath
            ? [item.outputPath]
            : [];
      for (const output of outputs) {
        if (output) {
          paths.add(output);
        }
      }
    }
    return [...paths];
  }

  private syncFileItems(): void {
    const paths = this.selectedPaths();
    const generation = (this.fileItemsGeneration += 1);
    const existing = new Map(this.fileItems().map((item) => [item.path, item]));
    this.fileItems.set(
      paths.map(
        (path) => existing.get(path) ?? { path, name: fileName(path) },
      ),
    );

    if (paths.length > 0) {
      void this.enrichFileItems(paths, generation);
    }
  }

  private async enrichFileItems(
    paths: string[],
    generation: number,
  ): Promise<void> {
    try {
      const response = await this.wailsService.inspectFiles(paths);
      if (generation !== this.fileItemsGeneration || !response.success) {
        return;
      }

      const infos = new Map<string, FileInfo>();
      for (const info of response.files) {
        infos.set(info.path, info);
      }

      this.fileItems.update((items) =>
        items.map((item) => {
          const info = infos.get(item.path);
          const meta = info ? this.describeFileInfo(info) : '';
          return meta ? { ...item, meta } : item;
        }),
      );
    } catch {
      // Inspection is optional: the queue keeps the file name and path.
    }
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(formatCount(info.pageCount, 'page'));
    }
    return parts.join(' · ');
  }

  private intakeNotice(
    rejected: string[],
    extraPdfs: number,
    firstName: string,
  ): Notice | null {
    const parts: string[] = [];

    if (extraPdfs > 0) {
      parts.push(
        `PDF to DOCX converts one PDF at a time. Only ${firstName} was added.`,
      );
    }

    if (rejected.length === 1) {
      parts.push(`${fileName(rejected[0])} wasn't added because it isn't a PDF.`);
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(`${names} weren't added because they aren't PDFs.`);
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
