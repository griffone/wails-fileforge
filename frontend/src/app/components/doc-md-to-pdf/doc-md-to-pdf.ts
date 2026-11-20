import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule } from '@angular/forms';
import { Subject, Subscription } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import {
  FolderOpen,
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
import { fileExtension, fileName, formatBytes } from '../../utils/format';
import { observeJob } from '../../utils/job-stream';
import { resumeToolJob } from '../../utils/resume-task';

const MD_TO_PDF_TOOL_ID = 'tool.doc.md_to_pdf';
const BAND_PLAN_DEBOUNCE_MS = 250;
const PAGE_SETUP_DEBOUNCE_MS = 200;

/** Mirrors the backend defaults: A4 portrait with 20/15/18/15 mm margins. */
const PAGE_SETUP_DEFAULTS = {
  pageSize: 'A4',
  orientation: 'P',
  marginTop: 20,
  marginRight: 15,
  marginBottom: 18,
  marginLeft: 15,
} as const;

const PAGE_SETUP_MARGIN_CONTROLS = [
  'marginTop',
  'marginRight',
  'marginBottom',
  'marginLeft',
] as const;

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

type BandRole = 'header' | 'footer';

@Component({
  selector: 'app-doc-md-to-pdf',
  standalone: true,
  imports: [
    ReactiveFormsModule,
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './doc-md-to-pdf.html',
  styleUrl: './doc-md-to-pdf.css',
})
export class DocMdToPdf implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  private readonly fb = inject(FormBuilder);

  /**
   * Header and footer are a fixed object with seven keys and the backend
   * rejects a partial object.
   */
  readonly form = this.fb.nonNullable.group({
    header: this.fb.nonNullable.group({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    }),
    footer: this.fb.nonNullable.group({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    }),
  });

  /**
   * Page setup stays a separate form so selects can refresh the plan at once
   * while margin edits debounce like every other numeric field.
   */
  readonly pageSetupForm = this.fb.nonNullable.group({
    pageSize: 'A4',
    orientation: 'P',
    marginTop: 20,
    marginRight: 15,
    marginBottom: 18,
    marginLeft: 15,
  });

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly outputDir = signal(this.preferences.defaultOutputDir());

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
      this.selectedPaths().length === 1 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  /** A failed plan (for example invalid margins) blocks the action. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly primaryLabel = computed(() =>
    this.isSubmitting() || this.isPolling() ? 'Converting…' : 'Convert to PDF',
  );

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to the Markdown file',
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
      ? describeToolError(error, 'The conversion could not start.')
      : null;
  });

  private jobSubscription: Subscription | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {
    this.form.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => this.schedulePlanRefresh());

    this.pageSetupForm.controls.pageSize.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => this.refreshPlanImmediately());
    this.pageSetupForm.controls.orientation.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => this.refreshPlanImmediately());
    for (const name of PAGE_SETUP_MARGIN_CONTROLS) {
      this.pageSetupForm.controls[name].valueChanges
        .pipe(takeUntil(this.destroy$))
        .subscribe(() =>
          this.schedulePlanRefresh(PAGE_SETUP_DEBOUNCE_MS),
        );
    }

  }

  ngOnDestroy(): void {
    this.stopObserving();
    this.clearPlanRefreshTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    const pending = this.dropIntake.take('/doc-md-to-pdf');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  fileCountLabel(): string {
    return this.selectedPaths().length === 1 ? '1 Markdown' : 'No file';
  }

  onPathsSelected(paths: string[]): void {
    const incoming = (Array.isArray(paths) ? paths : [])
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (incoming.length === 0) {
      return;
    }

    const supported = incoming.filter((path) => this.isMarkdown(path));
    const rejected = incoming.filter((path) => !this.isMarkdown(path));
    const chosen = supported[0] ?? '';
    const previous = this.selectedPaths()[0] ?? '';

    this.notice.set(
      this.intakeNotice(rejected, chosen, previous, supported.length),
    );

    if (!chosen) {
      return;
    }

    if (chosen === previous) {
      return;
    }

    this.actionMessage.set('');
    this.selectedPaths.set([chosen]);
    this.syncFileItems();
    void this.refreshPlan();
  }

  async chooseFiles(): Promise<void> {
    const path = (await this.wailsService.openFileDialog()).trim();
    if (path) {
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
    void this.refreshPlan();
  }

  removeFile(path: string): void {
    if (this.selectedPaths()[0] !== path) {
      return;
    }
    this.selectedPaths.set([]);
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

  useMarkdownFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.refreshPlan();
  }

  async run(): Promise<void> {
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.actionMessage.set('');

    if (this.planLoading() || !this.plan()?.success) {
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
      toolId: MD_TO_PDF_TOOL_ID,
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

  /** A failed conversion has no partial queue to retry: rerun the same file. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');

    const planned = await this.refreshPlan();
    if (!planned) {
      return;
    }

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

  /** Quiet summary shown in the page setup disclosure header. */
  pageSetupSummary(): string {
    const setup = this.pageSetupForm.getRawValue();
    const margins = [
      setup.marginTop,
      setup.marginRight,
      setup.marginBottom,
      setup.marginLeft,
    ].map((value) => this.marginText(value));
    const marginSummary = margins.every((margin) => margin === margins[0])
      ? `${margins[0]} mm margins`
      : `${margins.join('/')} mm margins`;
    return `${setup.pageSize} · ${this.orientationLabel()} · ${marginSummary}`;
  }

  pageSetupLabel(): string {
    return `${this.pageSetupForm.controls.pageSize.value} ${this.orientationLabel().toLowerCase()}`;
  }

  resetPageSetup(): void {
    this.clearPlanRefreshTimer();
    this.pageSetupForm.setValue({ ...PAGE_SETUP_DEFAULTS }, { emitEvent: false });
    this.actionMessage.set('');
    void this.refreshPlan();
  }

  previewEnabled(role: BandRole): boolean {
    return this.bandGroup(role).enabled.value;
  }

  previewText(role: BandRole): string {
    const text = this.bandGroup(role).text.value;
    const now = new Date();
    const date = `${now.getFullYear()}-${`${now.getMonth() + 1}`.padStart(
      2,
      '0',
    )}-${`${now.getDate()}`.padStart(2, '0')}`;

    return text
      .replaceAll('{page}', '1')
      .replaceAll('{totalPages}', '?')
      .replaceAll('{date}', date)
      .replaceAll('{fileName}', this.previewFileName() || 'document.md');
  }

  previewStyle(role: BandRole): Record<string, string> {
    const group = this.bandGroup(role);
    return {
      color: group.color.value,
      'font-family': this.previewFont(group.font.value),
      'text-align': group.align.value,
      'margin-top': `${group.marginTop.value}px`,
      'margin-bottom': `${group.marginBottom.value}px`,
    };
  }

  private bandGroup(role: BandRole) {
    return role === 'header'
      ? this.form.controls.header.controls
      : this.form.controls.footer.controls;
  }

  private previewFileName(): string {
    return fileName(this.selectedPaths()[0] ?? '');
  }

  private previewFont(font: string): string {
    switch (font) {
      case 'times':
        return 'Times New Roman, serif';
      case 'courier':
        return 'Courier New, monospace';
      default:
        return 'Helvetica, Arial, sans-serif';
    }
  }

  private orientationLabel(): string {
    return this.pageSetupForm.controls.orientation.value === 'L'
      ? 'Landscape'
      : 'Portrait';
  }

  private marginText(value: number): string {
    const numeric = Number(value);
    return Number.isFinite(numeric)
      ? String(Math.round(numeric * 10) / 10)
      : '0';
  }

  private buildRequest(): JobRequest {
    const header = this.bandGroup('header');
    const footer = this.bandGroup('footer');
    const setup = this.pageSetupForm.getRawValue();

    return {
      toolId: MD_TO_PDF_TOOL_ID,
      mode: 'single',
      inputPaths: [this.selectedPaths()[0] ?? ''],
      outputDir: this.outputDir(),
      options: {
        header: {
          enabled: header.enabled.value,
          text: header.text.value,
          align: header.align.value,
          font: header.font.value,
          marginTop: Number(header.marginTop.value),
          marginBottom: Number(header.marginBottom.value),
          color: header.color.value,
        },
        footer: {
          enabled: footer.enabled.value,
          text: footer.text.value,
          align: footer.align.value,
          font: footer.font.value,
          marginTop: Number(footer.marginTop.value),
          marginBottom: Number(footer.marginBottom.value),
          color: footer.color.value,
        },
        // Always send the resolved setup: the backend validates it and applies
        // the documented defaults when a value is missing.
        pageSize: setup.pageSize,
        orientation: setup.orientation,
        margins: {
          top: Number(setup.marginTop) || 0,
          right: Number(setup.marginRight) || 0,
          bottom: Number(setup.marginBottom) || 0,
          left: Number(setup.marginLeft) || 0,
        },
      },
    };
  }

  private async refreshPlan(): Promise<boolean> {
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
    // Keep the failed plan so the shared plan card can show the error in
    // context, next to the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
    this.planError.set(error);
    return false;
  }

  private schedulePlanRefresh(delay = BAND_PLAN_DEBOUNCE_MS): void {
    this.clearPlanRefreshTimer();
    this.planRefreshTimer = setTimeout(() => {
      this.planRefreshTimer = null;
      void this.refreshPlan();
    }, delay);
  }

  private refreshPlanImmediately(): void {
    this.clearPlanRefreshTimer();
    void this.refreshPlan();
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
      toolId: MD_TO_PDF_TOOL_ID,
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
    this.fileItems.set(paths.map((path) => ({ path, name: fileName(path) })));

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

  private intakeNotice(
    rejected: string[],
    chosen: string,
    previous: string,
    supportedCount: number,
  ): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(
        `${fileName(rejected[0])} wasn't added because it isn't a Markdown file.`,
      );
    } else if (rejected.length > 1) {
      parts.push(`${rejected.length} files weren't added because they aren't Markdown files.`);
    }

    if (supportedCount > 1) {
      parts.push(`Only one Markdown file is used. Using ${fileName(chosen)}.`);
    } else if (previous && chosen && previous !== chosen) {
      parts.push(`Replaced ${fileName(previous)}.`);
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }

  private isMarkdown(path: string): boolean {
    const extension = fileExtension(path).toLowerCase();
    return extension === 'md' || extension === 'markdown';
  }

  private isTerminalStatus(status: string): boolean {
    return TERMINAL_STATUSES.has(status);
  }
}
