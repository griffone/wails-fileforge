import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Subject } from 'rxjs';
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
  JobRequest,
  JobResult,
  JobStatusResponse,
  OCRCapabilitiesResponse,
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
import { resumeToolJob } from '../../utils/resume-task';

const PDF_OCR_TOOL_ID = 'tool.pdf.ocr';
const POLLING_INTERVAL_MS = 1000;
const PLAN_DEBOUNCE_MS = 250;
/** Job progress stage emitted while tesseract works through the pages. */
const OCR_STAGE = 'ocr';
/** Languages with the best out-of-the-box accuracy. */
const RECOMMENDED_LANGUAGES = ['eng', 'spa'];
/** Banner fragment used when no Tesseract language data is installed. */
const LANGUAGE_PIECE = 'Tesseract language data (eng or spa)';

const TERMINAL_STATUSES = new Set<string>([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type CapabilityStatus = 'loading' | 'ok' | 'error';

interface Notice {
  tone: 'warning' | 'danger';
  text: string;
}

export interface CapabilityBanner {
  title: string;
  detail: string;
}

export interface RecognizedSummary {
  tone: 'success' | 'warning';
  text: string;
}

/** `['afr']` → `AFR`; `['afr','deu']` → `AFR and DEU`; more use commas. */
function joinWords(values: readonly string[]): string {
  if (values.length <= 1) {
    return values[0] ?? '';
  }
  if (values.length === 2) {
    return `${values[0]} and ${values[1]}`;
  }
  return `${values.slice(0, -1).join(', ')} and ${values[values.length - 1]}`;
}

function joinLabels(values: readonly string[]): string {
  return joinWords(values.map((value) => value.toUpperCase()));
}

@Component({
  selector: 'app-pdf-ocr',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-ocr.html',
  styleUrl: './pdf-ocr.css',
})
export class PdfOcr implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly activity = inject(ActivityService);

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly selectedPaths = signal<string[]>([]);
  readonly fileItems = signal<FileQueueItem[]>([]);
  readonly outputDir = signal(this.preferences.defaultOutputDir().trim());
  /** Empty means "let the backend pick the best installed language". */
  readonly language = signal('');
  readonly sidecar = signal(true);

  readonly capabilities = signal<OCRCapabilitiesResponse | null>(null);
  readonly capabilitiesStatus = signal<CapabilityStatus>('loading');

  readonly plan = signal<PlanJobResponse | null>(null);
  readonly planLoading = signal(false);

  readonly jobResult = signal<JobResult | null>(null);
  readonly jobError = signal<JobError | null>(null);
  readonly outputInfos = signal<Record<string, FileInfo>>({});

  readonly isSubmitting = signal(false);
  readonly isPolling = signal(false);
  readonly activeJobId = signal('');

  readonly notice = signal<Notice | null>(null);
  readonly actionMessage = signal('');

  readonly languages = computed(() => this.capabilities()?.languages ?? []);

  readonly languageOptions = computed(() =>
    this.languages().map((code) => ({ value: code, label: code.toUpperCase() })),
  );

  readonly installedLanguagesLabel = computed(() =>
    joinLabels(this.languages()),
  );

  readonly ocrAvailable = computed(
    () => this.capabilities()?.available === true,
  );

  /**
   * Only a real backend answer blocks the page: an IPC failure in preview mode
   * leaves the run enabled so the plan can report the actual problem.
   */
  readonly ocrBlocked = computed(
    () => this.capabilitiesStatus() === 'ok' && !this.ocrAvailable(),
  );

  readonly missingPieces = computed<string[]>(() => {
    const capabilities = this.capabilities();
    if (!capabilities || capabilities.available) {
      return [];
    }

    const pieces: string[] = [];
    if (!capabilities.pdfToPpm) {
      pieces.push('pdftoppm');
    }
    if (!capabilities.tesseract) {
      pieces.push('tesseract');
    }
    if (this.languages().length === 0) {
      pieces.push(LANGUAGE_PIECE);
    }
    return pieces;
  });

  readonly capabilityBanner = computed<CapabilityBanner | null>(() => {
    if (!this.ocrBlocked()) {
      return null;
    }

    const capabilities = this.capabilities();
    const pieces = this.missingPieces();
    const dependencies = pieces.filter((piece) => piece !== LANGUAGE_PIECE);
    const missingLanguages = pieces.length > dependencies.length;

    const sentences: string[] = [];
    if (dependencies.length > 0) {
      sentences.push(`Missing ${joinWords(dependencies)}.`);
    }
    if (missingLanguages) {
      sentences.push(
        'No Tesseract language data is installed; install eng or spa.',
      );
    }
    if (sentences.length === 0) {
      sentences.push(
        capabilities?.message?.trim() ||
          'Install the OCR dependencies and reload this page.',
      );
    } else {
      sentences.push('Install the missing pieces and reload this page.');
    }

    return {
      title: 'Local OCR is unavailable.',
      detail: sentences.join(' '),
    };
  });

  /** Recognition quality is noticeably weaker without eng or spa data. */
  readonly weakLanguageWarning = computed(() => {
    if (!this.ocrAvailable()) {
      return '';
    }

    const languages = this.languages();
    if (languages.length === 0) {
      return '';
    }
    if (languages.some((code) => RECOMMENDED_LANGUAGES.includes(code))) {
      return '';
    }

    return `Only ${this.installedLanguagesLabel()} language data is installed. Install eng/spa Tesseract data for better accuracy.`;
  });

  readonly languageHelp = computed(() =>
    this.languages().length > 0
      ? `Installed: ${this.installedLanguagesLabel()}.`
      : 'No Tesseract language data is installed.',
  );

  readonly languageMissing = computed(
    () => this.languages().length > 0 && !this.language(),
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.ocrBlocked() &&
      !this.languageMissing() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return 'Making searchable…';
    }
    const count = this.selectedPaths().length;
    return count === 1 ? 'OCR 1 PDF' : count > 1 ? `OCR ${count} PDFs` : 'OCR PDFs';
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || 'Next to each PDF',
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return 'Add at least one PDF.';
    }
    if (this.ocrBlocked()) {
      return 'Local OCR is unavailable on this machine.';
    }
    if (this.languageMissing()) {
      return 'Choose a language.';
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

  /** `OCR page 3 of 5` while tesseract reports page progress. */
  readonly pageProgressLabel = computed(() => {
    const job = this.jobResult();
    const progress = job?.progress;
    if (!job || this.isTerminalStatus(job.status) || !progress) {
      return '';
    }
    if (progress.stage !== OCR_STAGE || progress.total <= 0) {
      return '';
    }

    const current = Math.min(Math.max(1, progress.current || 1), progress.total);
    return `OCR page ${current} of ${progress.total}`;
  });

  /**
   * The tool reports the recognized page and character counts in the first
   * successful item message; `No text` means the scan stayed blank.
   */
  readonly recognizedSummary = computed<RecognizedSummary | null>(() => {
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
      tone: /no text/i.test(text) ? 'warning' : 'success',
      text,
    };
  });

  readonly jobErrorCopy = computed<ToolErrorCopy | null>(() => {
    const error = this.jobError();
    return error
      ? describeToolError(error, 'The OCR job could not start.')
      : null;
  });

  private pollingTimer: ReturnType<typeof setInterval> | null = null;
  private planRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private planToken = 0;
  private fileItemsGeneration = 0;

  constructor(private readonly wailsService: Wails) {
    this.wailsService.jobProgress$
      .pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        if (!event || event.jobId !== this.activeJobId()) {
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
          // Wait for the status endpoint to settle the final items. The event
          // can arrive before the job store has the complete result.
          status: this.isTerminalStatus(event.status)
            ? current.status
            : (event.status as JobExecutionStatus),
        });

        if (this.isTerminalStatus(event.status)) {
          void this.pollJobStatus();
        }
      });
  }

  ngOnInit(): void {
    void this.loadCapabilities();

    const pending = this.dropIntake.take('/pdf-ocr');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.resumeBackgroundJob();
  }

  ngOnDestroy(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.destroy$.next();
    this.destroy$.complete();
  }

  fileCountLabel(): string {
    return formatCount(this.selectedPaths().length, 'PDF', 'PDFs');
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
      if (fileExtension(path).toLowerCase() !== 'pdf') {
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
    this.schedulePlanRefresh();
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
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  removeFile(path: string): void {
    const next = this.selectedPaths().filter((candidate) => candidate !== path);
    if (next.length === this.selectedPaths().length) {
      return;
    }
    this.selectedPaths.set(next);
    this.syncFileItems();
    this.schedulePlanRefresh();
  }

  onLanguageChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (value === this.language()) {
      return;
    }
    this.language.set(value);
    this.actionMessage.set('');
    this.schedulePlanRefresh();
  }

  onSidecarToggle(event: Event): void {
    const checked = !!(event.target as HTMLInputElement | null)?.checked;
    if (checked === this.sidecar()) {
      return;
    }
    this.sidecar.set(checked);
    this.actionMessage.set('');
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

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.schedulePlanRefresh();
  }

  async run(): Promise<void> {
    if (!this.canRun() || this.planBlocked()) {
      return;
    }

    this.clearPlanRefreshTimer();
    this.actionMessage.set('');

    // The plan is the source of truth for output names and destinations, so it
    // is always refreshed with the current inputs before starting the job.
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
        message: 'The OCR job could not start.',
        jobId: '',
        status: 'failed' as const,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: 'The OCR job could not start.',
        },
      }));
    if (!response.success || !response.jobId) {
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: response.message || 'The OCR job could not start.',
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
      message: 'Waiting to start',
      toolId: PDF_OCR_TOOL_ID,
      status: 'queued',
      progress: {
        current: 0,
        total,
        stage: 'queued',
        message: 'Waiting to start',
      },
      items: [],
      startedAt: Date.now(),
    });
    this.isSubmitting.set(false);
    this.isPolling.set(true);
    this.startPolling();
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

  reset(): void {
    this.stopPolling();
    this.clearPlanRefreshTimer();
    this.planToken += 1;
    this.fileItemsGeneration += 1;

    this.selectedPaths.set([]);
    this.fileItems.set([]);
    this.plan.set(null);
    this.planLoading.set(false);
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.isSubmitting.set(false);
    this.activeJobId.set('');
    this.notice.set(null);
    this.actionMessage.set('');
  }

  private async loadCapabilities(): Promise<void> {
    try {
      const response = await this.wailsService.getOCRCapabilities();
      this.capabilities.set(response);
      this.capabilitiesStatus.set(response.success ? 'ok' : 'error');

      if (response.success && !this.language()) {
        const languages = response.languages ?? [];
        const recommended = response.recommendedLanguage?.trim() ?? '';
        if (recommended && languages.includes(recommended)) {
          this.language.set(recommended);
        } else if (languages.length > 0) {
          this.language.set(languages[0]);
        }
      }
    } catch {
      this.capabilities.set(null);
      this.capabilitiesStatus.set('error');
    }
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    return {
      toolId: PDF_OCR_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: {
        language: this.language(),
        textSidecar: this.sidecar(),
      },
    };
  }

  private async refreshPlan(): Promise<boolean> {
    this.clearPlanRefreshTimer();

    if (this.selectedPaths().length === 0 || this.ocrBlocked()) {
      this.planToken += 1;
      this.plan.set(null);
      this.planLoading.set(false);
      return false;
    }

    const token = (this.planToken += 1);
    this.planLoading.set(true);
    this.plan.set(null);

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
    // Keep the failed plan so the shared plan card shows the reason next to
    // the destination it belongs to.
    this.plan.set({ ...response, success: false, error });
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
      toolId: PDF_OCR_TOOL_ID,
      activeJobId: this.activeJobId,
      jobResult: this.jobResult,
      jobError: this.jobError,
      startPolling: () => this.startPolling(),
    });
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling.set(true);
    void this.pollJobStatus();
    this.pollingTimer = setInterval(() => {
      void this.pollJobStatus();
    }, POLLING_INTERVAL_MS);
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isPolling.set(false);
  }

  private async pollJobStatus(): Promise<void> {
    const jobId = this.activeJobId();
    if (!jobId) {
      this.stopPolling();
      return;
    }

    const response: JobStatusResponse = await this.wailsService
      .getJobStatus(jobId)
      .catch(() => ({
        success: false,
        message: 'The job status could not be read.',
        found: false,
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'JOB_STATUS_FAILED',
          message: 'The job status could not be read.',
        },
      }));
    if (this.activeJobId() !== jobId) {
      return;
    }

    if (!response.found || !response.result) {
      this.stopPolling();
      this.activeJobId.set('');
      this.jobError.set(
        response.error ?? {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'JOB_STATUS_MISSING',
          message: response.message || 'The job status could not be read.',
        },
      );
      return;
    }

    this.jobResult.set(response.result);
    if (this.isTerminalStatus(response.result.status)) {
      this.stopPolling();
      this.activeJobId.set('');
      void this.loadOutputInfos();
    }
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
    duplicates: string[],
  ): Notice | null {
    const parts: string[] = [];

    if (rejected.length === 1) {
      parts.push(`${fileName(rejected[0])} wasn't added because it isn't a PDF.`);
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(`${names} weren't added because they aren't PDFs.`);
    }

    if (duplicates.length === 1) {
      parts.push(`${fileName(duplicates[0])} is already in the queue.`);
    } else if (duplicates.length > 1) {
      parts.push(`${duplicates.length} PDFs are already in the queue.`);
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
