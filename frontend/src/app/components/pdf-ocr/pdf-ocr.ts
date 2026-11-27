import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
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
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  JobRequest,
  OCRCapabilitiesResponse,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_OCR_TOOL_ID = 'tool.pdf.ocr';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;
/** Job progress stage emitted while tesseract works through the pages. */
const OCR_STAGE = 'ocr';
/** Languages with the best out-of-the-box accuracy. */
const RECOMMENDED_LANGUAGES = ['eng', 'spa'];
/** Banner fragment used when no Tesseract language data is installed. */
const LANGUAGE_PIECE = 'Tesseract language data (eng or spa)';

type CapabilityStatus = 'loading' | 'ok' | 'error';

export interface CapabilityBanner {
  title: string;
  detail: string;
}

export interface RecognizedSummary {
  tone: 'success' | 'warning';
  text: string;
}

type PdfOcrOptions = {
  language: string;
  sidecar: boolean;
};

function defaultOptions(): PdfOcrOptions {
  return { language: '', sidecar: true };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfOcrOptions> {
  const result: Partial<PdfOcrOptions> = {};
  if (typeof stored['language'] === 'string') {
    result.language = stored['language'];
  }
  if (typeof stored['sidecar'] === 'boolean') {
    result.sidecar = stored['sidecar'];
  }
  return result;
}

/** `['afr']` → `AFR`; `['afr','deu']` → `AFR and DEU`; more use commas. */
function joinWords(values: readonly string[], conjunction: string): string {
  if (values.length <= 1) {
    return values[0] ?? '';
  }
  if (values.length === 2) {
    return `${values[0]} ${conjunction} ${values[1]}`;
  }
  return `${values.slice(0, -1).join(', ')} ${conjunction} ${values[values.length - 1]}`;
}

@Component({
  selector: 'app-pdf-ocr',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
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
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_OCR_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_OCR_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir().trim(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () => this.selectedPaths().length > 0 && !this.ocrBlocked(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () =>
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    messages: {
      start: 'pdfOcr.error.start',
      planFailed: 'pdfOcr.error.plan',
      waiting: 'pdfOcr.status.waiting',
      inspectFailed: 'pdfOcr.error.inspect',
      statusMissing: 'pdfOcr.error.status',
      cancelFailed: 'pdfOcr.error.cancel',
      openFileFailed: 'pdfOcr.error.openFile',
      revealFailed: 'pdfOcr.error.openFolder',
    },
    notice: {
      rejectedOne: 'pdfOcr.notice.rejectedOne',
      rejectedMany: 'pdfOcr.notice.rejectedMany',
      duplicateOne: 'pdfOcr.notice.duplicateOne',
      duplicateMany: 'pdfOcr.notice.duplicateMany',
      rejectedManyFormat: 'names',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly outputDir = this.job.outputDir;
  /** Empty means "let the backend pick the best installed language". */
  readonly language = signal('');
  readonly sidecar = signal(true);

  readonly capabilities = signal<OCRCapabilitiesResponse | null>(null);
  readonly capabilitiesStatus = signal<CapabilityStatus>('loading');

  readonly plan = this.job.plan;
  readonly planError = this.job.planError;
  readonly planLoading = this.job.planLoading;

  readonly jobResult = this.job.jobResult;
  readonly jobError = this.job.jobError;
  readonly outputInfos = this.job.outputInfos;

  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly activeJobId = this.job.activeJobId;

  readonly notice = this.job.notice;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;

  readonly languages = computed(() => this.capabilities()?.languages ?? []);

  readonly languageOptions = computed(() =>
    this.languages().map((code) => ({ value: code, label: code.toUpperCase() })),
  );

  readonly installedLanguagesLabel = computed(() =>
    this.joinLabels(this.languages()),
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
      sentences.push(
        this.i18n.t('pdfOcr.capability.missing', {
          dependencies: this.joinWords(dependencies),
        }),
      );
    }
    if (missingLanguages) {
      sentences.push(this.i18n.t('pdfOcr.capability.noLanguage'));
    }
    if (sentences.length === 0) {
      sentences.push(
        capabilities?.message?.trim() ||
          this.i18n.t('pdfOcr.capability.installMissing'),
      );
    } else {
      sentences.push(this.i18n.t('pdfOcr.capability.installPieces'));
    }

    return {
      title: this.i18n.t('pdfOcr.capability.title'),
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

    return this.i18n.t('pdfOcr.language.weak', {
      languages: this.installedLanguagesLabel(),
    });
  });

  readonly languageHelp = computed(() =>
    this.languages().length > 0
      ? this.i18n.t('pdfOcr.language.installed', {
          languages: this.installedLanguagesLabel(),
        })
      : this.i18n.t('pdfOcr.language.none'),
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
      return this.i18n.t('pdfOcr.action.recognizing');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('pdfOcr.action.ocr');
    }
    return count === 1
      ? this.i18n.t('pdfOcr.action.ocrOne')
      : this.i18n.t('pdfOcr.action.ocrMany', { count });
  });

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || this.i18n.t('pdfOcr.destination.each'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfOcr.helper.addOne');
    }
    if (this.ocrBlocked()) {
      return this.i18n.t('pdfOcr.helper.unavailable');
    }
    if (this.languageMissing()) {
      return this.i18n.t('pdfOcr.helper.chooseLanguage');
    }
    return '';
  });

  /** `OCR page 3 of 5` while tesseract reports page progress. */
  readonly pageProgressLabel = computed(() => {
    const job = this.jobResult();
    const progress = job?.progress;
    if (!job || this.terminalJob() || !progress) {
      return '';
    }
    if (progress.stage !== OCR_STAGE || progress.total <= 0) {
      return '';
    }

    const current = Math.min(Math.max(1, progress.current || 1), progress.total);
    return this.i18n.t('pdfOcr.progress.page', {
      current,
      total: progress.total,
    });
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

  ngOnInit(): void {
    this.restoreOptions();
    void this.loadCapabilities();

    this.job.takePendingDrop('/pdf-ocr');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  private joinWords(values: readonly string[]): string {
    return joinWords(values, this.i18n.t('pdfOcr.list.and'));
  }

  private joinLabels(values: readonly string[]): string {
    return this.joinWords(values.map((value) => value.toUpperCase()));
  }

  onPathsSelected(paths: string[]): void {
    this.job.onPathsSelected(paths);
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.job.onPathsSelected(paths);
    }
  }

  clear(): void {
    this.job.clear();
  }

  removeFile(path: string): void {
    this.job.removeFile(path);
  }

  onLanguageChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (value === this.language()) {
      return;
    }
    this.language.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onSidecarToggle(event: Event): void {
    const checked = !!(event.target as HTMLInputElement | null)?.checked;
    if (checked === this.sidecar()) {
      return;
    }
    this.sidecar.set(checked);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
  }

  async cancel(): Promise<void> {
    await this.job.cancel();
  }

  async onOpen(path: string): Promise<void> {
    await this.job.openPath(path);
  }

  async onReveal(path: string): Promise<void> {
    await this.job.revealPath(path);
  }

  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    this.job.replacePaths([...new Set(failed)], { notify: false });
    this.job.clearRunOutcome();

    const planned = await this.job.refreshPlan();
    if (!planned) {
      return;
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
  }

  private currentOptions(): PdfOcrOptions {
    return { language: this.language(), sidecar: this.sidecar() };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.language.set(options.language);
    this.sidecar.set(options.sidecar);
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

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(
        formatCount(
          info.pageCount,
          this.i18n.t('common.pageOne'),
          this.i18n.t('common.pageMany'),
        ),
      );
    }
    return parts.join(' · ');
  }
}
