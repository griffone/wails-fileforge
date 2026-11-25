import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
} from '@angular/core';
import { RouterLink } from '@angular/router';
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
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_TO_DOCX_TOOL_ID = 'tool.pdf.to_docx';
const PLAN_DEBOUNCE_MS = 250;

/** Backend codes that mean the managed pdf2docx engine is not installed. */
const RUNTIME_ERROR_CODES = new Set([
  'PDF_DOCX_RUNTIME_UNAVAILABLE',
  'RUNTIME_DEP_MISSING',
]);

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
  id: string;
  icon: LucideIconData;
  title: string;
  detail: string;
}

/** The fidelity contract shown on the page; matches what pdf2docx preserves. */
export const FIDELITY_FEATURES: readonly FidelityFeature[] = [
  {
    id: 'fonts',
    icon: Type,
    title: 'Fonts & sizes',
    detail: 'Family, size and line height',
  },
  {
    id: 'emphasis',
    icon: Bold,
    title: 'Bold & italic',
    detail: 'Weight, italic, underline and strike-through',
  },
  {
    id: 'colors',
    icon: Palette,
    title: 'Colors & alignment',
    detail: 'Text color, highlights and paragraph alignment',
  },
  {
    id: 'images',
    icon: ImageIcon,
    title: 'Images',
    detail: 'Inline and floating images',
  },
  {
    id: 'tables',
    icon: Table,
    title: 'Tables',
    detail: 'Borders, shading and merged cells',
  },
  {
    id: 'layout',
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
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_TO_DOCX_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir().trim(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () => this.selectedPaths().length === 1,
    canRun: () => this.canRun(),
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () =>
      this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    messages: {
      start: 'pdfToDocx.error.start',
      planFailed: 'pdfToDocx.error.plan',
      waiting: 'pdfToDocx.status.waiting',
      inspectFailed: 'pdfToDocx.error.inspect',
      statusMissing: 'pdfToDocx.error.status',
      cancelFailed: 'pdfToDocx.error.cancel',
      openFileFailed: 'pdfToDocx.error.openFile',
      revealFailed: 'pdfToDocx.error.openFolder',
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly fidelityFeatures = FIDELITY_FEATURES;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly outputDir = this.job.outputDir;

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

  readonly pdfCountLabel = computed(() =>
    formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    ),
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
        title: this.i18n.t('pdfToDocx.plan.engineMissingTitle'),
        detail: this.i18n.t('pdfToDocx.plan.engineMissingDetail'),
        showSettings: true,
      };
    }

    const message =
      typeof error.message === 'string' ? error.message.trim() : '';

    return {
      title: this.i18n.t('pdfToDocx.plan.failedTitle'),
      detail: message || this.i18n.t('pdfToDocx.plan.failedDetail'),
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
      ? this.i18n.t('pdfToDocx.action.converting')
      : this.i18n.t('pdfToDocx.action.convert'),
  );

  readonly destinationLabel = computed(
    () => this.outputDir().trim() || this.i18n.t('pdfToDocx.destination.nextToPdf'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfToDocx.helper.addOne');
    }
    if (this.planLoading()) {
      return this.i18n.t('pdfToDocx.helper.planning');
    }
    if (!this.plan()?.success) {
      return this.i18n.t('pdfToDocx.helper.cannotPlan');
    }
    return '';
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

  ngOnInit(): void {
    const pending = this.dropIntake.take('/pdf-to-docx');
    if (pending.length > 0) {
      this.onPathsSelected(pending);
    }
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  /** Fidelity copy resolved at render time so the locale switch applies. */
  fidelityTitle(feature: FidelityFeature): string {
    return this.i18n.tOr(
      `pdfToDocx.fidelity.${feature.id}.title`,
      feature.title,
    );
  }

  fidelityDetail(feature: FidelityFeature): string {
    return this.i18n.tOr(
      `pdfToDocx.fidelity.${feature.id}.detail`,
      feature.detail,
    );
  }

  /**
   * This tool converts one PDF at a time: the first accepted file becomes the
   * selection and any extra PDFs or other files are reported, never queued.
   */
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

    const selected = pdfs[0] ?? '';
    this.notice.set(this.intakeNotice(rejected, pdfs.length - 1, fileName(selected)));
    if (!selected) {
      return;
    }

    this.actionMessage.set('');
    this.job.replacePaths([selected], { notify: false });
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async chooseFile(): Promise<void> {
    const path = (await this.wailsService.openFileDialog()).trim();
    if (path.length > 0) {
      this.onPathsSelected([path]);
    }
  }

  clear(): void {
    this.job.clear();
  }

  removeFile(path: string): void {
    this.job.removeFile(path);
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

  usePdfFolder(): void {
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
    const failed = (this.jobResult()?.items ?? []).some((item) => !item.success);
    if (!failed) {
      return;
    }

    this.job.clearRunOutcome();
    await this.job.run();
  }

  reset(): void {
    this.job.reset();
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

  private intakeNotice(
    rejected: string[],
    extraPdfs: number,
    firstName: string,
  ): { tone: 'warning'; text: string } | null {
    const parts: string[] = [];

    if (extraPdfs > 0) {
      parts.push(
        this.i18n.t('pdfToDocx.notice.oneAtATime', { name: firstName }),
      );
    }

    if (rejected.length === 1) {
      parts.push(
        this.i18n.t('pdfToDocx.notice.rejectedOne', {
          name: fileName(rejected[0]),
        }),
      );
    } else if (rejected.length > 1) {
      const names = rejected.map((path) => fileName(path)).join(', ');
      parts.push(this.i18n.t('pdfToDocx.notice.rejectedMany', { names }));
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }
}
