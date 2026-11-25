import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Subscription } from 'rxjs';
import {
  FolderOpen,
  Info,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { FileQueueItem } from '../tool-flow/tool-flow.types';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../../services/tool-error';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
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
import { fileExtension, fileName, formatBytes, formatCount } from '../../utils/format';
import { observeJob } from '../../utils/job-stream';
import { resumeToolJob } from '../../utils/resume-task';

const DOCX_TO_PDF_TOOL_ID = 'tool.doc.docx_to_pdf';
const PLAN_DEBOUNCE_MS = 250;

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

@Component({
  selector: 'app-doc-docx-to-pdf',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './doc-docx-to-pdf.html',
  styleUrl: './doc-docx-to-pdf.css',
})
export class DocDocxToPdf implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly infoIcon = Info;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly outputDir = signal(this.preferences.defaultOutputDir());

  /** Page range applied to every input; empty keeps all pages. */
  readonly pageSelection = signal('');

  /** PDF/A archival export; LibreOffice only, off by default. */
  readonly pdfa = signal(false);

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

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  /** A failed plan (for example an invalid page range) blocks the action. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('docDocxToPdf.action.converting');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('docDocxToPdf.action.convert');
    }
    return count === 1
      ? this.i18n.t('docDocxToPdf.action.convertOne')
      : this.i18n.t('docDocxToPdf.action.convertMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('docDocxToPdf.destination.each'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly terminalJob = computed<JobResult | null>(() => {
    const job = this.jobResult();
    return job && this.isTerminalStatus(job.status) ? job : null;
  });

  readonly showProgress = computed(() => {
    const job = this.jobResult();
    return !!job && !this.isTerminalStatus(job.status);
  });

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(error, this.i18n.t('docDocxToPdf.error.start')),
        )
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planDirty = false;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {}

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/doc-docx-to-pdf');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('docDocxToPdf.count.none');
    }
    return formatCount(
      count,
      this.i18n.t('common.docxOne'),
      this.i18n.t('docDocxToPdf.count.many'),
    );
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const rejected: string[] = [];
    const duplicates: string[] = [];
    const added: string[] = [];
    const known = new Set(this.selectedPaths().map((path) => this.pathKey(path)));

    for (const path of incoming) {
      if (fileExtension(path).toLowerCase() !== 'docx') {
        rejected.push(path);
        continue;
      }

      const key = this.pathKey(path);
      if (known.has(key)) {
        duplicates.push(path);
        continue;
      }

      known.add(key);
      added.push(path);
    }

    this.notice.set(this.intakeNotice(rejected, duplicates));

    if (added.length === 0) {
      return;
    }

    this.actionMessage.set('');
    this.selectedPaths.set([...this.selectedPaths(), ...added]);
    this.syncFileItems();
    void this.refreshPlan();
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.onPathsSelected(paths);
    }
  }

  clear(): void {
    if (this.selectedPaths().length === 0) {
      return;
    }
    this.selectedPaths.set([]);
    this.notice.set(null);
    this.actionMessage.set('');
    this.clearPageSelection();
    this.syncFileItems();
    void this.refreshPlan();
  }

  removeFile(path: string): void {
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }
    this.selectedPaths.set(next);
    if (next.length === 0) {
      this.clearPageSelection();
    }
    this.syncFileItems();
    void this.refreshPlan();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.refreshPlan();
  }

  onPageSelectionInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    if (value === this.pageSelection()) {
      return;
    }
    this.pageSelection.set(value);
    this.actionMessage.set('');
    this.planDirty = true;
    this.schedulePlanRefresh();
  }

  onPdfaToggle(event: Event): void {
    const enabled = !!(event.target as HTMLInputElement | null)?.checked;
    if (enabled === this.pdfa()) {
      return;
    }
    this.pdfa.set(enabled);
    this.actionMessage.set('');
    // The plan probes LibreOffice for PDF/A, so the refresh is immediate.
    void this.refreshPlan();
  }

  async run(): Promise<void> {
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    if (this.planLoading() || this.planDirty || !this.plan()?.success) {
      const planned = await this.refreshPlan();
      if (!planned) {
        return;
      }
    }

    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(true);

    const response: RunJobResponse = await this.wailsService
      .runJob(this.buildRequest())
      .catch(() => ({
        success: false,
        message: this.i18n.t('docDocxToPdf.error.start'),
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: this.i18n.t('docDocxToPdf.error.start'),
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message:
            response.message || this.i18n.t('docDocxToPdf.error.start'),
        },
      );
      this.isSubmitting.set(false);
      return;
    }

    const total = this.selectedPaths().length;
    this.activeJobId.set(response.jobId);
    this.jobResult.set({
      jobId: response.jobId,
      success: false,
      message: this.i18n.t('docDocxToPdf.status.waiting'),
      toolId: DOCX_TO_PDF_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: this.i18n.t('docDocxToPdf.status.waiting'),
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
        response.message || this.i18n.t('docDocxToPdf.error.cancel'),
      );
    }
  }

  async onOpen(path: string): Promise<void> {
    const response = await this.wailsService.openPath(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('docDocxToPdf.error.openFile'),
    );
  }

  async onReveal(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    this.actionMessage.set(
      response.success
        ? ''
        : response.message || this.i18n.t('docDocxToPdf.error.openFolder'),
    );
  }

  /** Reruns only the inputs that failed, as a fresh job. */
  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    this.selectedPaths.set([...new Set(failed)]);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');
    this.syncFileItems();

    await this.refreshPlan();
    await this.run();
  }

  reset(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.pageSelection.set('');
    this.pdfa.set(false);
    this.planDirty = false;
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
    const paths = this.selectedPaths();
    const options: Record<string, unknown> = {};
    const pageSelection = this.pageSelection().trim();
    if (pageSelection) {
      options['pageSelection'] = pageSelection;
    }
    // PDF/A is opt-in; leaving it out keeps the regular PDF export.
    if (this.pdfa()) {
      options['pdfa'] = true;
    }

    return {
      toolId: DOCX_TO_PDF_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  private clearPageSelection(): void {
    this.clearPlanRefreshTimer();
    this.pageSelection.set('');
    this.planDirty = false;
  }

  private schedulePlanRefresh(): void {
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

  private async refreshPlan(): Promise<boolean> {
    if (this.selectedPaths().length === 0) {
      this.planToken += 1;
      this.planDirty = false;
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
        message: this.i18n.t('docDocxToPdf.error.plan'),
        outputs: [],
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PLAN_JOB_FAILED',
          message: this.i18n.t('docDocxToPdf.error.plan'),
        },
      }));
    if (token !== this.planToken) {
      return false;
    }

    this.planLoading.set(false);
    this.planDirty = false;

    if (response.success && !response.error) {
      this.plan.set(response);
      return true;
    }

    const error: JobError = response.error ?? {
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PLAN_JOB_FAILED',
      message: response.message || this.i18n.t('docDocxToPdf.error.plan'),
    };
    // Keep the failed plan so the shared plan card can show the error in
    // context, next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private resumeBackgroundJob(): void {
    resumeToolJob({
      activity: this.activity,
      toolId: DOCX_TO_PDF_TOOL_ID,
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
        message: response.message || this.i18n.t('docDocxToPdf.error.status'),
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
        message: this.i18n.t('docDocxToPdf.error.inspect'),
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

      const infos = new Map(response.files.map((info) => [info.path, info]));
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
    return parts.join(' · ');
  }

  private intakeNotice(rejected: string[], duplicates: string[]): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        this.i18n.t('docDocxToPdf.notice.rejectedOne', {
          name: fileName(rejected[0]),
        }),
      );
    } else if (rejected.length > 1) {
      parts.push(
        this.i18n.t('docDocxToPdf.notice.rejectedMany', {
          count: rejected.length,
        }),
      );
    }

    if (duplicates.length === 1) {
      parts.push(
        this.i18n.t('docDocxToPdf.notice.duplicateOne', {
          name: fileName(duplicates[0]),
        }),
      );
    } else if (duplicates.length > 1) {
      parts.push(
        this.i18n.t('docDocxToPdf.notice.duplicateMany', {
          count: duplicates.length,
        }),
      );
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  /**
   * Windows and UNC paths are case-insensitive, so the same file dropped with
   * different casing is still a duplicate there.
   */
  private pathKey(path: string): string {
    const normalized = path.trim().replace(/\\/g, '/');
    return /^[a-zA-Z]:\//.test(normalized) || normalized.startsWith('//')
      ? normalized.toLowerCase()
      : normalized;
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
