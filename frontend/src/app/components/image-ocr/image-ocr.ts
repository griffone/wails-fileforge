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
  RotateCcw,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
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

const IMAGE_OCR_TOOL_ID = 'tool.image.ocr';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;
const DEFAULT_DPI = 300;
const MIN_DPI = 150;
const MAX_DPI = 600;
/** Banner fragment used when no Tesseract language data is installed. */
const LANGUAGE_PIECE = 'Tesseract language data (eng or spa)';

const SUPPORTED_IMAGE_EXTENSIONS = new Set([
  'jpg',
  'jpeg',
  'png',
  'webp',
  'bmp',
  'tiff',
  'tif',
  'gif',
]);

type CapabilityStatus = 'loading' | 'ok' | 'error';

export interface CapabilityBanner {
  title: string;
  detail: string;
}

export interface RecognizedSummary {
  tone: 'success' | 'warning';
  text: string;
}

type ImageOcrOptions = {
  language: string;
  writePdf: boolean;
  dpi: number;
};

function defaultOptions(): ImageOcrOptions {
  return { language: '', writePdf: false, dpi: DEFAULT_DPI };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<ImageOcrOptions> {
  const result: Partial<ImageOcrOptions> = {};
  if (typeof stored['language'] === 'string') {
    result.language = stored['language'];
  }
  if (typeof stored['writePdf'] === 'boolean') {
    result.writePdf = stored['writePdf'];
  }
  const dpi = stored['dpi'];
  if (typeof dpi === 'number' && Number.isFinite(dpi)) {
    result.dpi = Math.min(MAX_DPI, Math.max(MIN_DPI, Math.round(dpi)));
  }
  return result;
}

/** `['afr']` → `AFR`; `['afr','deu']` → `AFR and DEU`; more use commas. */
function joinWordsWith(
  values: readonly string[],
  conjunction: string,
): string {
  if (values.length <= 1) {
    return values[0] ?? '';
  }
  if (values.length === 2) {
    return `${values[0]} ${conjunction} ${values[1]}`;
  }
  return `${values.slice(0, -1).join(', ')} ${conjunction} ${values[values.length - 1]}`;
}

@Component({
  selector: 'app-image-ocr',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './image-ocr.html',
  styleUrl: './image-ocr.css',
})
export class ImageOcr implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: IMAGE_OCR_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: IMAGE_OCR_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      SUPPORTED_IMAGE_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () => this.selectedPaths().length > 0 && !this.ocrBlocked(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () =>
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    noticeText: (rejected) =>
      rejected.length > 0
        ? { tone: 'warning', text: this.rejectionNotice(rejected) }
        : null,
    messages: {
      start: 'imageOcr.error.start',
      planFailed: 'imageOcr.error.plan',
      waiting: 'imageOcr.status.waiting',
      inspectFailed: 'imageOcr.error.inspect',
      statusMissing: 'imageOcr.error.status',
      cancelFailed: 'imageOcr.error.cancel',
      openFileFailed: 'imageOcr.error.openFile',
      revealFailed: 'imageOcr.error.openFolder',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly resetIcon = RotateCcw;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;

  /** Empty means "let the backend pick the best installed language". */
  readonly language = signal('');
  readonly writePdf = signal(false);
  readonly dpi = signal(DEFAULT_DPI);
  readonly outputDir = this.job.outputDir;
  readonly advancedOpen = signal(false);

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

  /**
   * Image OCR needs Tesseract and language data only. The shared
   * `available` flag also requires pdftoppm, which belongs to the PDF
   * pipeline and must not block this page.
   */
  readonly ocrAvailable = computed(() => {
    const capabilities = this.capabilities();
    return (
      !!capabilities &&
      capabilities.tesseract === true &&
      (capabilities.languages?.length ?? 0) > 0
    );
  });

  /**
   * Only a real backend answer blocks the page: an IPC failure in preview mode
   * leaves the run enabled so the plan can report the actual problem.
   */
  readonly ocrBlocked = computed(
    () => this.capabilitiesStatus() === 'ok' && !this.ocrAvailable(),
  );

  readonly missingPieces = computed<string[]>(() => {
    const capabilities = this.capabilities();
    if (!capabilities || this.ocrAvailable()) {
      return [];
    }

    const pieces: string[] = [];
    if (!capabilities.tesseract) {
      pieces.push('Tesseract');
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
        this.i18n.t('imageOcr.capability.missing', {
          dependencies: this.joinWords(dependencies),
        }),
      );
    }
    if (missingLanguages) {
      sentences.push(this.i18n.t('imageOcr.capability.noLanguage'));
    }
    if (sentences.length === 0) {
      sentences.push(
        capabilities?.message?.trim() ||
          this.i18n.t('imageOcr.capability.installMissing'),
      );
    } else {
      sentences.push(this.i18n.t('imageOcr.capability.installPieces'));
    }

    return {
      title: this.i18n.t('imageOcr.capability.title'),
      detail: sentences.join(' '),
    };
  });

  readonly languageHelp = computed(() =>
    this.languages().length > 0
      ? this.i18n.t('imageOcr.language.installed', {
          languages: this.installedLanguagesLabel(),
        })
      : this.i18n.t('imageOcr.language.none'),
  );

  readonly languageMissing = computed(
    () => this.languages().length > 0 && !this.language(),
  );

  readonly dpiValid = computed(
    () => this.dpi() >= MIN_DPI && this.dpi() <= MAX_DPI,
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('imageOcr.action.recognizing');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('imageOcr.action.ocr');
    }
    return count === 1
      ? this.i18n.t('imageOcr.action.ocrOne')
      : this.i18n.t('imageOcr.action.ocrMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('imageOcr.destination.original'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.ocrBlocked() &&
      !this.languageMissing() &&
      this.dpiValid() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('imageOcr.helper.addImage');
    }
    if (this.ocrBlocked()) {
      return this.i18n.t('imageOcr.helper.unavailable');
    }
    if (this.languageMissing()) {
      return this.i18n.t('imageOcr.helper.chooseLanguage');
    }
    if (!this.dpiValid()) {
      return this.i18n.t('imageOcr.helper.dpi', {
        min: MIN_DPI,
        max: MAX_DPI,
      });
    }
    return '';
  });

  /**
   * The tool reports the recognized character count in the first successful
   * item message; a zero count means the scan stayed blank.
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

    this.job.takePendingDrop('/image-ocr');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  }

  private joinWords(values: readonly string[]): string {
    return joinWordsWith(values, this.i18n.t('imageOcr.list.and'));
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

  onWritePdfToggle(event: Event): void {
    const checked = this.readChecked(event);
    if (checked === this.writePdf()) {
      return;
    }
    this.writePdf.set(checked);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onDpiInput(event: Event): void {
    const value = this.readNumber(event);
    if (value === null) {
      this.dpi.set(DEFAULT_DPI);
    } else {
      this.dpi.set(Math.min(MAX_DPI, Math.max(MIN_DPI, value)));
    }
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  resetAdvanced(): void {
    this.dpi.set(DEFAULT_DPI);
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

  private currentOptions(): ImageOcrOptions {
    return {
      language: this.language(),
      writePdf: this.writePdf(),
      dpi: this.dpi(),
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.language.set(options.language);
    this.writePdf.set(options.writePdf);
    this.dpi.set(options.dpi);
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
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
    } catch {
      this.capabilities.set(null);
      this.capabilitiesStatus.set('error');
    }
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    return {
      toolId: IMAGE_OCR_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: {
        language: this.language(),
        writePdf: this.writePdf(),
        dpi: this.dpi(),
      },
    };
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.width === 'number' && typeof info.height === 'number') {
      parts.push(`${info.width} × ${info.height}`);
    }
    return parts.join(' · ');
  }

  private rejectionNotice(rejected: readonly string[]): string {
    const extensions = [
      ...new Set(
        rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
      ),
    ];

    const count = rejected.length;
    const filesCopy =
      count === 1
        ? this.i18n.t('imageOcr.notice.filesOne')
        : this.i18n.t('imageOcr.notice.filesMany', { count });

    let extensionsCopy: string;
    if (extensions.length === 1) {
      extensionsCopy = this.i18n.t('imageOcr.notice.extensionOne', {
        extension: extensions[0],
      });
    } else {
      const head = extensions.slice(0, -1).join(', ');
      extensionsCopy = this.i18n.t('imageOcr.notice.extensionsMany', {
        head,
        tail: extensions[extensions.length - 1],
      });
    }

    return `${filesCopy} ${extensionsCopy}`;
  }

  private readNumber(event: Event): number | null {
    const raw = this.readValue(event);
    if (raw.trim() === '') {
      return null;
    }
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) ? parsed : null;
  }

  private readValue(event: Event): string {
    return (event.target as HTMLInputElement | null)?.value ?? '';
  }

  private readChecked(event: Event): boolean {
    return !!(event.target as HTMLInputElement | null)?.checked;
  }
}
