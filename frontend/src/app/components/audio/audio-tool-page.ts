import {
  Directive,
  Signal,
  WritableSignal,
  inject,
} from '@angular/core';

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
  audioRejectionNotice,
  describeAudioFileInfo,
} from './audio-tool-config';
import type { AudioToolPageCopy } from './audio-tool-config';

/** Injected collaborators, passed explicitly so the base stays injection-free. */
export interface AudioToolPageDeps {
  preferences: PreferencesService;
  wails: Wails;
  i18n: I18nService;
  activity: ActivityService;
  dropIntake: DropIntakeService;
  lastRequests: LastRequestService;
  initialOutputDir: string;
}

/** The static identity of one audio page. */
export interface AudioToolIdentity {
  toolId: string;
  route: string;
  copy: AudioToolPageCopy;
  /** Past-tense verb for the shared result card, e.g. `extracted`. */
  verbPast: string;
  acceptAttribute: string;
  acceptedExtensions: ReadonlySet<string>;
  optionsSchemaVersion: number;
}

/**
 * Resolves the shared collaborators. Called from a component constructor, which
 * runs inside Angular's injection context, so `inject()` is legal here and no
 * page needs to repeat the seven-field object.
 */
export function audioToolPageDeps(): AudioToolPageDeps {
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
 * Shared lifecycle for the three audio pages.
 *
 * Extract, Convert and Normalize differ only in their option form and their
 * copy. Everything else — intake, remembered options, plan, run, observe,
 * verification, undo and the result card — is the same pipeline the video
 * pages use, so it lives here once and each page supplies:
 *
 * - `buildOptions()` for the plan/run request,
 * - `defaultOptions()`/`normalizeOptions()`/`applyOptions()`/`currentOptions()`
 *   for the `createToolOptions` store,
 * - `outputBadge()` for the header badge,
 * - optional `toolBlocked()` and `extraHelper()` for page-owned gating.
 *
 * The `@Directive()` decorator has no selector: it exists only so the Angular
 * compiler treats this lifecycle-owning base class as a directive. The derived
 * components remain the page and expose the inherited members to the template.
 */
@Directive()
export abstract class AudioToolPage {
  readonly i18n: I18nService;
  readonly copy: AudioToolPageCopy;
  readonly acceptAttribute: string;
  readonly verbPast: string;

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

  constructor(deps: AudioToolPageDeps, identity: AudioToolIdentity) {
    this.i18n = deps.i18n;
    this.copy = identity.copy;
    this.acceptAttribute = identity.acceptAttribute;
    this.verbPast = identity.verbPast;
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
        identity.acceptedExtensions.has(fileExtension(path).toLowerCase()),
      canPlan: () => this.selectedPaths().length > 0,
      canRun: () => this.canRun(),
      alwaysRefreshPlanBeforeRun: false,
      loadThumbnails: false,
      describeFileInfo: (info) => describeAudioFileInfo(info),
      noticeText: (rejected) =>
        rejected.length > 0
          ? { tone: 'warning', text: audioRejectionNotice(deps.i18n, rejected) }
          : null,
      messages: {
        start: this.copy.errorStart,
        planFailed: this.copy.errorPlan,
        waiting: 'audioTool.status.waiting',
        inspectFailed: this.copy.errorInspect,
        statusMissing: this.copy.errorStatus,
        cancelFailed: this.copy.errorCancel,
        openFileFailed: this.copy.errorOpenFile,
        revealFailed: this.copy.errorOpenFolder,
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
  protected abstract applyOptions(
    options: Record<string, unknown>,
  ): void;

  /** The exact values the result card should remember. */
  protected abstract currentOptions(): Record<string, unknown>;

  /** Header badge summarising the current output configuration. */
  abstract outputBadge(): string;

  /** Page-owned blocking (an invalid numeric field, for example). */
  protected toolBlocked(): boolean {
    return false;
  }

  /** Extra helper copy shown once files are queued. */
  protected extraHelper(): string {
    return '';
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
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
      this.outputDir().trim() || this.i18n.t('audioTool.output.destinationEach')
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

  /** Re-plans after an option changed; debounced like the video pages. */
  protected refreshPlanForOptions(): void {
    this.job.schedulePlanRefresh(200);
  }

  /** Drops a stale plan when the options are temporarily invalid. */
  protected clearPlanForOptions(): void {
    this.job.clearPlan();
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
