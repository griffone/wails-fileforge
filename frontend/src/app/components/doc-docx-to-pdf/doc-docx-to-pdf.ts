import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
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
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import { fileExtension, fileName, formatBytes, formatCount } from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const DOCX_TO_PDF_TOOL_ID = 'tool.doc.docx_to_pdf';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;

type DocxToPdfOptions = {
  pageSelection: string;
  pdfa: boolean;
};

function defaultOptions(): DocxToPdfOptions {
  return { pageSelection: '', pdfa: false };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<DocxToPdfOptions> {
  const result: Partial<DocxToPdfOptions> = {};

  const pageSelection = stored['pageSelection'];
  if (typeof pageSelection === 'string') {
    result.pageSelection = pageSelection;
  }

  if (typeof stored['pdfa'] === 'boolean') {
    result.pdfa = stored['pdfa'];
  }

  return result;
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
  private readonly lastRequests = inject(LastRequestService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: DOCX_TO_PDF_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: DOCX_TO_PDF_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: this.lastRequests,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'docx',
    canPlan: () => this.selectedPaths().length > 0,
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: (_previous, paths) => {
      if (paths.length === 0) {
        this.clearPageSelection();
      }
      void this.job.refreshPlan();
    },
    messages: {
      start: 'docDocxToPdf.error.start',
      planFailed: 'docDocxToPdf.error.plan',
      waiting: 'docDocxToPdf.status.waiting',
      inspectFailed: 'docDocxToPdf.error.inspect',
      statusMissing: 'docDocxToPdf.error.status',
      cancelFailed: 'docDocxToPdf.error.cancel',
      openFileFailed: 'docDocxToPdf.error.openFile',
      revealFailed: 'docDocxToPdf.error.openFolder',
    },
    notice: {
      rejectedOne: 'docDocxToPdf.notice.rejectedOne',
      rejectedMany: 'docDocxToPdf.notice.rejectedMany',
      duplicateOne: 'docDocxToPdf.notice.duplicateOne',
      duplicateMany: 'docDocxToPdf.notice.duplicateMany',
      rejectedManyFormat: 'count',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
    onRunAgainAdopted: (request) => {
      this.applyRequestOptions(request.options);
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly infoIcon = Info;

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly outputDir = this.job.outputDir;

  /** Page range applied to every input; empty keeps all pages. */
  readonly pageSelection = signal('');

  /** PDF/A archival export; post-processed with Ghostscript, off by default. */
  readonly pdfa = signal(false);

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

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/doc-docx-to-pdf');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
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

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.job.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
  }

  onPageSelectionInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    if (value === this.pageSelection()) {
      return;
    }
    this.pageSelection.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onPdfaToggle(event: Event): void {
    const enabled = !!(event.target as HTMLInputElement | null)?.checked;
    if (enabled === this.pdfa()) {
      return;
    }
    this.pdfa.set(enabled);
    this.actionMessage.set('');
    // The plan is refreshed immediately so the result reflects the new export mode.
    void this.job.refreshPlan();
  }

  async run(): Promise<void> {
    await this.job.run();
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

  /** Reruns only the inputs that failed, as a fresh job. */
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
    this.restoreOptions();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
  }

  private currentOptions(): DocxToPdfOptions {
    return { pageSelection: this.pageSelection(), pdfa: this.pdfa() };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.pageSelection.set(options.pageSelection);
    this.pdfa.set(options.pdfa);
  }

  private applyRequestOptions(options: Record<string, unknown>): void {
    const stored = normalizeOptions(options);
    if (stored.pageSelection !== undefined) {
      this.pageSelection.set(stored.pageSelection);
    }
    if (stored.pdfa !== undefined) {
      this.pdfa.set(stored.pdfa);
    }
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
    this.pageSelection.set('');
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    return parts.join(' · ');
  }
}
