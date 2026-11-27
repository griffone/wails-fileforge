import { Directive, Signal, WritableSignal, inject, signal } from '@angular/core';

import type { RunAgainStartedEvent } from '../tool-flow/result-card';
import type { FileQueueItem } from '../tool-flow/tool-flow.types';
import { ActivityService } from '../../services/activity.service';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { LastRequestService } from '../../services/last-request.service';
import { PreferencesService } from '../../services/preferences.service';
import type { ToolErrorCopy } from '../../services/tool-error';
import {
  FileInfo,
  JobError,
  JobRequest,
  JobResult,
  PlanJobResponse,
  VerificationReport,
  Wails,
} from '../../services/wails';
import { fileExtension, formatCount } from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import type { ToolOptionsStore } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';
import {
  PDF_INPUT_ACCEPT,
  PDF_INPUT_EXTENSIONS,
  describePdfFileInfo,
} from './pdf-page-ops-config';
import type { PdfPageOpsCopy } from './pdf-page-ops-config';
import {
  formatInterval,
  formatPageList,
} from './pdf-page-ops-validation';
import type {
  PageSelectionValidation,
  ReorderValidation,
} from './pdf-page-ops-validation';

/** Injected collaborators, passed explicitly so the base stays injection-free. */
export interface PdfPageOpsDeps {
  preferences: PreferencesService;
  wails: Wails;
  i18n: I18nService;
  activity: ActivityService;
  dropIntake: DropIntakeService;
  lastRequests: LastRequestService;
  initialOutputDir: string;
}

/** The static identity of one page-operation page. */
export interface PdfPageOpsIdentity {
  toolId: string;
  route: string;
  copy: PdfPageOpsCopy;
  /** Past-tense token for the shared result card, e.g. `rotated`. */
  verbPast: string;
  /** Result-card noun group: `PDF` for file outputs, `file` for folders. */
  toolLabel: string;
  optionsSchemaVersion: number;
}

/**
 * Resolves the shared collaborators. Called from a component constructor, which
 * runs inside Angular's injection context, so `inject()` is legal here and no
 * page needs to repeat the seven-field object.
 */
export function pdfPageOpsDeps(): PdfPageOpsDeps {
  const preferences = inject(PreferencesService);
  return {
    preferences,
    wails: inject(Wails),
    i18n: inject(I18nService),
    activity: inject(ActivityService),
    dropIntake: inject(DropIntakeService),
    lastRequests: inject(LastRequestService),
    initialOutputDir: preferences.defaultOutputDir(),
  };
}

/**
 * Shared lifecycle for Rotate, Remove pages, Reorder and Extract images.
 *
 * The four tools differ only in their option form and their copy: intake,
 * remembered options, plan, run, observe, verification, undo and the result
 * card are the same pipeline the audio and video pages use. Each page supplies
 * `buildOptions()`, the `createToolOptions` hooks and its own template; this
 * class owns everything else, including the inspected page counts the
 * client-side validation needs.
 *
 * The `@Directive()` decorator has no selector: it exists only so the Angular
 * compiler treats this lifecycle-owning base class as a directive.
 */
@Directive()
export abstract class PdfPageOpsToolPage {
  readonly i18n: I18nService;
  readonly copy: PdfPageOpsCopy;
  readonly acceptAttribute = PDF_INPUT_ACCEPT;
  readonly verbPast: string;
  readonly toolLabel: string;

  readonly selectedPaths: WritableSignal<string[]>;
  readonly fileItems: WritableSignal<FileQueueItem[]>;
  readonly outputDir: WritableSignal<string>;

  readonly plan: Signal<PlanJobResponse | null>;
  readonly planError: Signal<JobError | null>;
  readonly planLoading: Signal<boolean>;

  readonly jobResult: Signal<JobResult | null>;
  readonly jobError: Signal<JobError | null>;
  readonly outputInfos: Signal<Record<string, FileInfo>>;

  readonly isSubmitting: Signal<boolean>;
  readonly isPolling: Signal<boolean>;
  readonly activeJobId: Signal<string>;

  readonly notice: Signal<{ tone: 'warning' | 'danger'; text: string } | null>;
  readonly actionMessage: WritableSignal<string>;
  readonly terminalJob: Signal<JobResult | null>;
  readonly showProgress: Signal<boolean>;
  readonly jobErrorCopy: Signal<ToolErrorCopy | null>;

  readonly verification: Signal<VerificationReport | null>;
  readonly verificationLoading: Signal<boolean>;
  readonly verificationUnavailable: Signal<boolean>;

  private readonly route: string;
  private readonly settings: ToolOptionsStore<Record<string, unknown>>;
  private readonly job: ToolRunController;
  private readonly wailsService: Wails;
  /** Page counts reported by `inspectFiles`, keyed by input path. */
  private readonly inspectedPageCounts = signal<Record<string, number>>({});

  constructor(deps: PdfPageOpsDeps, identity: PdfPageOpsIdentity) {
    this.i18n = deps.i18n;
    this.copy = identity.copy;
    this.verbPast = identity.verbPast;
    this.toolLabel = identity.toolLabel;
    this.route = identity.route;
    this.wailsService = deps.wails;

    this.settings = createToolOptions(deps.preferences, {
      toolId: identity.toolId,
      schemaVersion: identity.optionsSchemaVersion,
      defaults: () => this.defaultOptions(),
      normalize: (stored) => this.normalizeOptions(stored),
    });

    this.job = new ToolRunController({
      toolId: identity.toolId,
      wails: deps.wails,
      i18n: deps.i18n,
      activity: deps.activity,
      dropIntake: deps.dropIntake,
      lastRequests: deps.lastRequests,
      initialOutputDir: deps.initialOutputDir,
      buildRequest: () => this.buildRequest(identity.toolId),
      acceptsPath: (path) =>
        PDF_INPUT_EXTENSIONS.has(fileExtension(path).toLowerCase()),
      canPlan: () => this.selectedPaths().length > 0 && !this.toolBlocked(),
      canRun: () => this.canRun(),
      alwaysRefreshPlanBeforeRun: false,
      loadThumbnails: false,
      describeFileInfo: (info) => describePdfFileInfo(deps.i18n, info),
      collectOutputPaths: () => this.collectOutputPaths(),
      onFilesInspected: (infos) => this.measurePageCounts(infos),
      messages: {
        start: 'pdfPageOps.error.start',
        planFailed: 'pdfPageOps.error.plan',
        waiting: 'pdfPageOps.status.waiting',
        inspectFailed: 'pdfPageOps.error.inspect',
        statusMissing: 'pdfPageOps.error.status',
        cancelFailed: 'pdfPageOps.error.cancel',
        openFileFailed: 'pdfPageOps.error.openFile',
        revealFailed: 'pdfPageOps.error.openFolder',
      },
      notice: {
        rejectedOne: 'pdfPageOps.notice.rejectedOne',
        rejectedMany: 'pdfPageOps.notice.rejectedMany',
        duplicateOne: 'pdfPageOps.notice.duplicateOne',
        duplicateMany: 'pdfPageOps.notice.duplicateMany',
        rejectedManyFormat: 'names',
      },
      onRunAccepted: () => {
        this.settings.save(this.currentOptions());
      },
    });

    this.selectedPaths = this.job.selectedPaths;
    this.fileItems = this.job.fileItems;
    this.outputDir = this.job.outputDir;

    this.plan = this.job.plan;
    this.planError = this.job.planError;
    this.planLoading = this.job.planLoading;

    this.jobResult = this.job.jobResult;
    this.jobError = this.job.jobError;
    this.outputInfos = this.job.outputInfos;

    this.isSubmitting = this.job.isSubmitting;
    this.isPolling = this.job.isPolling;
    this.activeJobId = this.job.activeJobId;

    this.notice = this.job.notice;
    this.actionMessage = this.job.actionMessage;
    this.terminalJob = this.job.terminalJob;
    this.showProgress = this.job.showProgress;
    this.jobErrorCopy = this.job.jobErrorCopy;

    this.verification = this.job.verification;
    this.verificationLoading = this.job.verificationLoading;
    this.verificationUnavailable = this.job.verificationUnavailable;
  }

  ngOnInit(): void {
    this.applyOptions(this.settings.restore());
    this.job.takePendingDrop(this.route);
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  /** Option payload for plan and run; page-owned. */
  protected abstract buildOptions(): Record<string, unknown>;

  /** Fresh defaults for the remembered options. */
  protected abstract defaultOptions(): Record<string, unknown>;

  /** Maps a stored payload onto the page's option shape. */
  protected abstract normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>>;

  /** Applies restored options to the page signals. */
  protected abstract applyOptions(options: Record<string, unknown>): void;

  /** The exact values the result card should remember. */
  protected abstract currentOptions(): Record<string, unknown>;

  /** Header badge summarising the current output configuration. */
  abstract outputBadge(): string;

  /** Page-owned blocking (an invalid selection or order, for example). */
  protected toolBlocked(): boolean {
    return false;
  }

  /** Extra helper copy shown once files are queued. */
  protected extraHelper(): string {
    return '';
  }

  /**
   * Output paths to inspect after a job. Pages that write folders override
   * this: a folder has no size to show, and its files are already verified.
   */
  protected collectOutputPaths(): string[] {
    const paths = new Set<string>();
    for (const item of this.jobResult()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs = (item.outputs ?? []).filter((path) => !!path);
      if (outputs.length > 0) {
        for (const output of outputs) {
          paths.add(output);
        }
      } else if (item.outputPath) {
        paths.add(item.outputPath);
      }
    }
    return [...paths];
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  canRun(): boolean {
    return (
      this.selectedPaths().length > 0 &&
      !this.isSubmitting() &&
      !this.isPolling() &&
      !this.toolBlocked()
    );
  }

  primaryLabel(): string {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t(this.copy.actionBusy);
    }

    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t(this.copy.actionIdle);
    }
    return count === 1
      ? this.i18n.t(this.copy.actionOne)
      : this.i18n.t(this.copy.actionMany, { count });
  }

  destinationLabel(): string {
    return (
      this.outputDir().trim() || this.i18n.t('pdfPageOps.destination.each')
    );
  }

  isDefaultDestination(): boolean {
    return this.outputDir().trim().length === 0;
  }

  actionHelper(): string {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t(this.copy.helperAddOne);
    }

    return this.extraHelper();
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

  async run(): Promise<void> {
    await this.job.run();
  }

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

  /** Re-plans after an option changed; debounced like the other tool pages. */
  protected refreshPlanForOptions(): void {
    this.job.schedulePlanRefresh(200);
  }

  /** Drops a stale plan when the options are temporarily invalid. */
  protected clearPlanForOptions(): void {
    this.job.clearPlan();
  }

  /** Page counts of the queued PDFs that `inspectFiles` already reported. */
  protected knownPageCounts(): number[] {
    const counts = this.inspectedPageCounts();
    const result: number[] = [];
    for (const path of this.selectedPaths()) {
      const count = counts[path];
      if (typeof count === 'number' && count > 0) {
        result.push(count);
      }
    }
    return result;
  }

  /** Localized explanation for a rotate/remove/extract selection issue. */
  protected pageSelectionMessage(validation: PageSelectionValidation): string {
    const issue = validation.issue;
    if (!issue) {
      return '';
    }

    switch (issue.kind) {
      case 'required':
        return this.i18n.t('pdfPageOps.pages.error.required');
      case 'emptyEntry':
        return this.i18n.t('pdfPageOps.pages.error.emptyEntry');
      case 'token':
        return this.i18n.t('pdfPageOps.pages.error.token', {
          token: issue.token,
        });
      case 'range':
        return this.i18n.t('pdfPageOps.pages.error.range', {
          token: issue.token,
        });
      case 'rangeOrder':
        return this.i18n.t('pdfPageOps.pages.error.rangeOrder', {
          token: issue.token,
        });
      case 'overlap':
        return this.i18n.t('pdfPageOps.pages.error.overlap', {
          first: formatInterval(issue.first),
          second: formatInterval(issue.second),
        });
      case 'outOfBounds':
        return this.i18n.t('pdfPageOps.pages.error.outOfBounds', {
          count: issue.pageCount,
        });
      case 'allPages':
        return this.i18n.t('pdfPageOps.pages.error.allPages', {
          count: issue.pageCount,
        });
    }
  }

  /** Localized explanation for a reorder spec issue. */
  protected reorderMessage(validation: ReorderValidation): string {
    const issue = validation.issue;
    if (!issue) {
      return '';
    }

    switch (issue.kind) {
      case 'required':
        return this.i18n.t('pdfPageOps.order.error.required');
      case 'emptyEntry':
        return this.i18n.t('pdfPageOps.order.error.emptyEntry');
      case 'token':
        return this.i18n.t('pdfPageOps.order.error.token', {
          token: issue.token,
        });
      case 'range':
        return this.i18n.t('pdfPageOps.order.error.range', {
          token: issue.token,
        });
      case 'rangeOrder':
        return this.i18n.t('pdfPageOps.order.error.rangeOrder', {
          token: issue.token,
        });
      case 'outOfBounds':
        return this.i18n.t('pdfPageOps.order.error.outOfBounds', {
          page: issue.page,
          count: issue.pageCount,
        });
      case 'coverage': {
        const missing = formatPageList(issue.missing);
        const duplicates = formatPageList(issue.duplicates);
        if (issue.missing.length > 0 && issue.duplicates.length > 0) {
          return this.i18n.t('pdfPageOps.order.error.missingAndDuplicates', {
            missing,
            duplicates,
          });
        }
        if (issue.duplicates.length > 0) {
          return this.i18n.t('pdfPageOps.order.error.duplicates', {
            duplicates,
          });
        }
        return this.i18n.t('pdfPageOps.order.error.missing', { missing });
      }
    }
  }

  /**
   * Page counts arrive asynchronously; when they change the verdict on an
   * option, the plan follows so the card never contradicts the form.
   */
  private measurePageCounts(infos: FileInfo[]): void {
    const wasBlocked = this.toolBlocked();

    const next: Record<string, number> = {};
    for (const info of infos) {
      if (
        info.path &&
        typeof info.pageCount === 'number' &&
        info.pageCount > 0
      ) {
        next[info.path] = info.pageCount;
      }
    }
    this.inspectedPageCounts.set(next);

    if (this.selectedPaths().length === 0) {
      return;
    }

    const isBlocked = this.toolBlocked();
    if (isBlocked && !wasBlocked) {
      this.clearPlanForOptions();
    } else if (!isBlocked && wasBlocked) {
      this.refreshPlanForOptions();
    }
  }

  private buildRequest(toolId: string): JobRequest {
    const paths = this.selectedPaths();
    return {
      toolId,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: this.buildOptions(),
    };
  }
}
