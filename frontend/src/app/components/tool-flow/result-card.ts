import {
  Component,
  DoCheck,
  EventEmitter,
  Input,
  Output,
  inject,
} from '@angular/core';
import {
  Check,
  ExternalLink,
  FolderOpen,
  LucideAngularModule,
  Redo2,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  TriangleAlert,
  Undo2,
  X,
} from 'lucide-angular';
import {
  FileInfo,
  JobError,
  JobRequest,
  JobResult,
  PlanJobResponse,
  RunJobResponse,
  UndoResult,
  UndoSkipped,
  VerificationCheck,
  VerificationReport,
  Wails,
} from '../../services/wails';
import {
  describeToolError,
  localizeToolError,
  ToolErrorCopy,
} from '../../services/tool-error';
import {
  RecentConversionsService,
  isTerminalJobStatus,
} from '../../services/recent-conversions.service';
import {
  LastRequestService,
  LastSubmittedRequest,
} from '../../services/last-request.service';
import { I18nService } from '../../services/i18n.service';
import type { TranslationKey } from '../../i18n';
import { fileDir, fileName, formatBytes, formatCount } from '../../utils/format';

const GROUP_OUTPUT_LIMIT = 4;

const NOUN_KEYS: Readonly<
  Record<string, { one: TranslationKey; many: TranslationKey }>
> = {
  PDF: { one: 'common.pdfOne', many: 'common.pdfMany' },
  image: { one: 'common.imageOne', many: 'common.imageMany' },
  video: { one: 'common.videoOne', many: 'common.videoMany' },
  document: { one: 'common.documentOne', many: 'common.documentMany' },
  DOCX: { one: 'common.docxOne', many: 'common.docxMany' },
  file: { one: 'common.fileOne', many: 'common.fileMany' },
};

/** Host tokens that cannot be turned into a key by hyphenating the words. */
const VERB_KEYS: Readonly<Record<string, TranslationKey>> = {
  'made searchable': 'resultCard.verb.madeSearchable',
};

export interface ResultOutputRow {
  path: string;
  name: string;
  meta: string;
  grouped?: boolean;
}

export interface ResultFailedRow {
  path: string;
  name: string;
  description: string;
}

export interface ResultVerifyOutputRow {
  path: string;
  name: string;
  size: string;
  passed: boolean;
  checksPassed: number;
  totalChecks: number;
  warning?: string;
  targetActual?: string;
  targetLimit?: string;
  targetPassed?: boolean;
}

export interface ResultUndoSkippedRow {
  path: string;
  name: string;
  reason: string;
}

/** Emitted after the card planned and started a repeated job. */
export interface RunAgainStartedEvent {
  jobId: string;
  request: JobRequest;
  plan: PlanJobResponse;
}

type SummaryTone = 'success' | 'warning' | 'danger' | 'neutral';
type UndoMode = 'undo' | 'restore';
const RUN_AGAIN_DESTINATION_LIMIT = 3;

@Component({
  selector: 'app-result-card',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './result-card.html',
  styleUrl: './result-card.css',
})
export class ResultCard implements DoCheck {
  private jobValue: JobResult | null = null;
  private pendingRecord = false;
  private pendingVerify = false;
  private recordRecentValue = true;
  private verificationInputProvided = false;
  private verificationGeneration = 0;
  private readonly verificationRequestedJobIds = new Set<string>();

  verifyReport: VerificationReport | null = null;
  verifyLoading = false;
  verifyUnavailable = false;

  undoBusy = false;
  undoMessage = '';
  undoTone: 'success' | 'warning' = 'success';
  undoSkipped: UndoSkipped[] = [];
  undoMode: UndoMode = 'undo';
  showAllOutputs = false;

  runAgainBusy = false;
  runAgainPending: PlanJobResponse | null = null;
  runAgainError: JobError | null = null;
  private runAgainRequestValue: JobRequest | null = null;
  private runAgainLookup: {
    jobId: string;
    stored: LastSubmittedRequest | null;
  } | null = null;

  @Input()
  set job(value: JobResult | null) {
    const previousJobId = this.jobValue?.jobId ?? '';
    this.jobValue = value;

    if (previousJobId !== (value?.jobId ?? '')) {
      this.resetAutoVerification();
      this.resetUndoState();
      this.resetRunAgainState();
      this.showAllOutputs = false;
    }

    this.pendingRecord = true;
    this.pendingVerify = true;
    if (!value || !isTerminalJobStatus(value.status)) {
      this.pendingVerify = false;
      this.cancelVerificationRequest();
    }
  }

  get job(): JobResult | null {
    return this.jobValue;
  }

  @Input()
  set verification(value: VerificationReport | null) {
    this.verificationInputProvided = true;
    this.verifyReport = value;
    this.pendingVerify = false;
    this.cancelVerificationRequest();
    this.verifyUnavailable = false;
  }

  get verification(): VerificationReport | null {
    return this.verifyReport;
  }

  @Input()
  set recordRecent(value: boolean) {
    this.recordRecentValue = value !== false;
  }

  get recordRecent(): boolean {
    return this.recordRecentValue;
  }

  @Input() undoEnabled = true;
  @Input() fileInfos: Record<string, FileInfo> = {};
  @Input() toolLabel = 'file';
  /** Past-tense verb for the success summary, e.g. `converted`, `split`. */
  @Input() verbPast = 'converted';
  /**
   * Backend explanation for a `partial_success` result, e.g. a target size
   * miss. Recipes use it because nothing was retryable in those cases.
   */
  @Input() partialDetail = '';

  @Output() open = new EventEmitter<string>();
  @Output() reveal = new EventEmitter<string>();
  @Output() retryFailed = new EventEmitter<void>();
  @Output() reset = new EventEmitter<void>();
  @Output() runAgainStarted = new EventEmitter<RunAgainStartedEvent>();

  private readonly recentConversions = inject(RecentConversionsService);
  private readonly lastRequests = inject(LastRequestService);
  private readonly wailsService = inject(Wails);
  readonly i18n = inject(I18nService);
  private readonly recordedJobIds = new Set<string>();

  readonly checkIcon = Check;
  readonly alertIcon = TriangleAlert;
  readonly folderIcon = FolderOpen;
  readonly externalIcon = ExternalLink;
  readonly retryIcon = RotateCcw;
  readonly cancelIcon = X;
  readonly verifyOkIcon = ShieldCheck;
  readonly verifyAttentionIcon = ShieldAlert;
  readonly undoIcon = Undo2;
  readonly restoreIcon = Redo2;

  ngDoCheck(): void {
    if (this.pendingRecord) {
      this.pendingRecord = false;
      this.recordTerminalJob(this.jobValue);
    }

    if (this.pendingVerify) {
      this.pendingVerify = false;
      void this.startVerification();
    }
  }

  private recordTerminalJob(job: JobResult | null): void {
    if (
      !this.recordRecentValue ||
      !job ||
      !job.jobId ||
      !isTerminalJobStatus(job.status)
    ) {
      return;
    }

    if (this.recordedJobIds.has(job.jobId)) {
      return;
    }

    this.recordedJobIds.add(job.jobId);
    this.recentConversions.record(job);
  }

  private cancelVerificationRequest(): void {
    this.verificationGeneration += 1;
    this.verifyLoading = false;
  }

  private resetAutoVerification(): void {
    this.cancelVerificationRequest();
    this.verifyUnavailable = false;
    if (!this.verificationInputProvided) {
      this.verifyReport = null;
    }
  }

  private async startVerification(): Promise<void> {
    const job = this.jobValue;
    if (
      !job ||
      !job.jobId ||
      !isTerminalJobStatus(job.status) ||
      this.verificationInputProvided ||
      job.toolId.startsWith('recipe.') ||
      this.verificationRequestedJobIds.has(job.jobId)
    ) {
      return;
    }

    this.verificationRequestedJobIds.add(job.jobId);
    const generation = ++this.verificationGeneration;
    this.verifyReport = null;
    this.verifyUnavailable = false;
    this.verifyLoading = true;

    let report: VerificationReport | null = null;
    try {
      report = await this.wailsService.verifyJob(job.jobId);
    } catch {
      report = null;
    }

    if (
      generation !== this.verificationGeneration ||
      this.jobValue?.jobId !== job.jobId
    ) {
      return;
    }

    this.verifyLoading = false;
    if (
      !report ||
      !Array.isArray(report.outputs) ||
      report.outputs.length === 0
    ) {
      this.verifyReport = null;
      this.verifyUnavailable = true;
      return;
    }

    this.verifyReport = report;
    this.verifyUnavailable = false;
  }

  showVerifyBlock(): boolean {
    return (
      this.outputRows().length > 0 &&
      (this.verifyLoading ||
        this.verifyUnavailable ||
        this.verifyReport !== null)
    );
  }

  verifyTone(): SummaryTone {
    if (this.verifyLoading || this.verifyUnavailable || !this.verifyReport) {
      return 'neutral';
    }
    return this.verificationPassed(this.verifyReport) ? 'success' : 'warning';
  }

  verifyTitle(): string {
    return this.verificationPassed(this.verifyReport)
      ? this.i18n.t('resultCard.verified')
      : this.i18n.t('resultCard.needsAttention');
  }

  verifyIcon() {
    return this.verificationPassed(this.verifyReport)
      ? this.verifyOkIcon
      : this.verifyAttentionIcon;
  }

  /**
   * Advisory checks (such as a best-effort target miss) never invalidate an
   * output. A report whose only failures are advisory is still verified, even
   * if an older producer left `success` false.
   */
  private verificationPassed(report: VerificationReport | null): boolean {
    if (!report) {
      return false;
    }
    if (report.success) {
      return true;
    }

    const outputs = report.outputs ?? [];
    if (outputs.length === 0) {
      return false;
    }
    return outputs.every((output) =>
      (output.checks ?? []).every((check) => check.passed || check.advisory),
    );
  }

  verifySummary(): string {
    const report = this.verifyReport;
    if (!report) {
      return '';
    }

    const totals = this.verificationTotals(report);
    if (totals.total === 0) {
      return report.message;
    }
    return this.i18n.t('resultCard.checksPassed', {
      passed: totals.passed,
      total: totals.total,
    });
  }

  verifyOutputs(): ResultVerifyOutputRow[] {
    const report = this.verifyReport;
    if (!report || this.verifyLoading || this.verifyUnavailable) {
      return [];
    }

    return report.outputs.slice(0, 3).map((output) => {
      const checks = (output.checks ?? []).filter((check) => !check.advisory);
      const checksPassed = checks.filter((check) => check.passed).length;
      const passed =
        checks.length === 0 ? output.exists : checksPassed === checks.length;
      const targetBytes = output.targetBytes ?? 0;
      const actualBytes = output.actualBytes ?? output.sizeBytes;

      return {
        path: output.path,
        name: fileName(output.path),
        size: formatBytes(output.sizeBytes),
        passed,
        checksPassed,
        totalChecks: checks.length,
        warning: !passed
          ? (output.warnings?.[0] ?? this.i18n.t('resultCard.checkFailed'))
          : undefined,
        targetActual: targetBytes > 0 ? formatBytes(actualBytes) : undefined,
        targetLimit: targetBytes > 0 ? formatBytes(targetBytes) : undefined,
        targetPassed: targetBytes > 0 ? actualBytes <= targetBytes : undefined,
      };
    });
  }

  verifyChecks(): VerificationCheck[] {
    const outputs = this.verifyReport?.outputs ?? [];
    if (outputs.length === 0) {
      return [];
    }

    return (outputs[0].checks ?? [])
      .filter((check) => !check.advisory)
      .slice(0, 4);
  }

  verifyCheckText(check: VerificationCheck): string {
    return check.value ? `${check.label} · ${check.value}` : check.label;
  }

  verifyWarnings(): string[] {
    return (this.verifyReport?.warnings ?? []).slice(0, 3);
  }

  private verificationTotals(report: VerificationReport): {
    passed: number;
    total: number;
  } {
    let passed = 0;
    let total = 0;
    for (const output of report.outputs) {
      for (const check of output.checks ?? []) {
        if (check.advisory) {
          continue;
        }
        total += 1;
        if (check.passed) {
          passed += 1;
        }
      }
    }
    return { passed, total };
  }

  canUndoAction(): boolean {
    const job = this.jobValue;
    return (
      this.undoEnabled &&
      !!job &&
      !!job.jobId &&
      isTerminalJobStatus(job.status) &&
      this.outputRows().length > 0
    );
  }

  undoLabel(): string {
    return this.undoMode === 'undo'
      ? this.i18n.t('resultCard.undo')
      : this.i18n.t('resultCard.restore');
  }

  undoActionIcon() {
    return this.undoMode === 'undo' ? this.undoIcon : this.restoreIcon;
  }

  undoSkippedRows(): ResultUndoSkippedRow[] {
    return this.undoSkipped.slice(0, 2).map((skip) => ({
      path: skip.path,
      name: skip.path ? fileName(skip.path) : this.i18n.t('resultCard.aFile'),
      reason: skip.reason,
    }));
  }

  async runUndoAction(): Promise<void> {
    const job = this.jobValue;
    if (!job || !job.jobId || this.undoBusy) {
      return;
    }

    const action = this.undoMode;
    this.undoBusy = true;
    this.undoMessage = '';
    this.undoSkipped = [];

    try {
      const result =
        action === 'undo'
          ? await this.wailsService.undoJob(job.jobId)
          : await this.wailsService.restoreJob(job.jobId);
      this.applyUndoResult(result, action);
    } catch {
      this.undoMessage =
        action === 'undo'
          ? this.i18n.t('resultCard.undoFailed')
          : this.i18n.t('resultCard.restoreFailed');
      this.undoTone = 'warning';
      this.undoSkipped = [];
    } finally {
      this.undoBusy = false;
    }
  }

  private applyUndoResult(
    result: UndoResult | null | undefined,
    action: UndoMode,
  ): void {
    const skipped = Array.isArray(result?.skipped) ? result.skipped : [];
    this.undoSkipped = skipped;
    this.undoMessage = result?.message ?? '';
    this.undoTone =
      result?.success && skipped.length === 0 ? 'success' : 'warning';

    if (result?.success) {
      this.undoMode = action === 'undo' ? 'restore' : 'undo';
    }
  }

  private resetUndoState(): void {
    this.undoBusy = false;
    this.undoMode = 'undo';
    this.undoMessage = '';
    this.undoSkipped = [];
    this.undoTone = 'success';
  }

  /** The stored request for the visible terminal job, when it has inputs. */
  private currentRunAgainRequest(): LastSubmittedRequest | null {
    const job = this.jobValue;
    if (
      !job ||
      !job.jobId ||
      !isTerminalJobStatus(job.status) ||
      job.toolId.startsWith('recipe.')
    ) {
      return null;
    }

    if (!this.runAgainLookup || this.runAgainLookup.jobId !== job.jobId) {
      this.runAgainLookup = {
        jobId: job.jobId,
        stored: this.lastRequests.get(job.toolId),
      };
    }

    const stored = this.runAgainLookup.stored;
    return stored && stored.request.inputPaths.length > 0 ? stored : null;
  }

  /** True when the card can offer to repeat the finished job. */
  canRunAgain(): boolean {
    return (
      !this.runAgainBusy &&
      !this.runAgainPending &&
      this.currentRunAgainRequest() !== null
    );
  }

  /** False while a confirmation is open; drives the inline form. */
  showRunAgainConfirmation(): boolean {
    return this.runAgainPending !== null;
  }

  runAgainToolName(): string {
    const toolId = this.jobValue?.toolId ?? '';
    return this.i18n.tOr(`tools.${toolId}.name`, toolId);
  }

  runAgainInputCount(): number {
    const stored = this.currentRunAgainRequest();
    return stored?.request.inputPaths.length ?? 0;
  }

  runAgainMeta(): string {
    const nouns = this.nounPair(this.toolLabel);
    return this.i18n.t('resultCard.runAgain.meta', {
      tool: this.runAgainToolName(),
      counted: formatCount(this.runAgainInputCount(), nouns.one, nouns.many),
    });
  }

  /** Unique planned destinations, capped for the compact confirmation. */
  runAgainDestinations(): string[] {
    const seen = new Set<string>();
    const destinations: string[] = [];
    for (const output of this.runAgainPending?.outputs ?? []) {
      const path = output.outputPath?.trim() ?? '';
      if (!path || seen.has(path)) {
        continue;
      }
      seen.add(path);
      destinations.push(path);
    }
    return destinations.slice(0, RUN_AGAIN_DESTINATION_LIMIT);
  }

  runAgainDestinationOverflow(): number {
    const all = new Set(
      (this.runAgainPending?.outputs ?? [])
        .map((output) => output.outputPath?.trim() ?? '')
        .filter((path) => path.length > 0),
    );
    return Math.max(0, all.size - RUN_AGAIN_DESTINATION_LIMIT);
  }

  runAgainErrorCopy(): ToolErrorCopy | null {
    return this.runAgainError
      ? localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(
            this.runAgainError,
            this.i18n.t('resultCard.runAgain.failed'),
          ),
        )
      : null;
  }

  /** Plans the remembered request; the card never runs an unchecked request. */
  async startRunAgain(): Promise<void> {
    if (this.runAgainBusy) {
      return;
    }

    const stored = this.currentRunAgainRequest();
    if (!stored) {
      return;
    }

    this.runAgainBusy = true;
    this.runAgainError = null;
    this.runAgainPending = null;
    this.runAgainRequestValue = stored.request;

    let plan: PlanJobResponse;
    try {
      plan = await this.wailsService.planJob(stored.request);
    } catch {
      plan = {
        success: false,
        message: this.i18n.t('resultCard.runAgain.failed'),
        outputs: [],
      };
    }

    this.runAgainBusy = false;

    if (
      !plan.success ||
      plan.error ||
      (plan.outputs ?? []).length === 0
    ) {
      this.runAgainPending = null;
      this.runAgainError = plan.error ?? {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'PLAN_JOB_FAILED',
        message: plan.message || this.i18n.t('resultCard.runAgain.failed'),
      };
      return;
    }

    this.runAgainPending = plan;
  }

  cancelRunAgain(): void {
    if (this.runAgainBusy) {
      return;
    }
    this.runAgainPending = null;
    this.runAgainError = null;
    this.runAgainRequestValue = null;
  }

  /** Starts the job once the user confirms the planned destinations. */
  async confirmRunAgain(): Promise<void> {
    const request = this.runAgainRequestValue;
    const plan = this.runAgainPending;
    if (this.runAgainBusy || !request || !plan) {
      return;
    }

    this.runAgainBusy = true;
    this.runAgainError = null;

    let response: RunJobResponse;
    try {
      response = await this.wailsService.runJob(request);
    } catch {
      response = {
        success: false,
        message: this.i18n.t('resultCard.runAgain.startFailed'),
        jobId: '',
        status: 'failed',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'RUN_JOB_FAILED',
          message: this.i18n.t('resultCard.runAgain.startFailed'),
        },
      };
    }

    this.runAgainBusy = false;

    if (!response.success || !response.jobId) {
      this.runAgainError = response.error ?? {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'RUN_JOB_FAILED',
        message:
          response.message || this.i18n.t('resultCard.runAgain.startFailed'),
      };
      return;
    }

    // A fresh submission refreshes the remembered timestamp for the tool.
    this.lastRequests.save(request);
    this.runAgainPending = null;
    this.runAgainRequestValue = null;
    this.runAgainLookup = null;
    this.runAgainStarted.emit({
      jobId: response.jobId,
      request,
      plan,
    });
  }

  private resetRunAgainState(): void {
    this.runAgainBusy = false;
    this.runAgainPending = null;
    this.runAgainError = null;
    this.runAgainRequestValue = null;
    this.runAgainLookup = null;
  }

  private nounPair(label: string): { one: string; many: string } {
    const keys = NOUN_KEYS[label];
    return keys
      ? { one: this.i18n.t(keys.one), many: this.i18n.t(keys.many) }
      : { one: label, many: label };
  }

  private verbFor(token: string): string {
    const key =
      VERB_KEYS[token] ??
      (`resultCard.verb.${token.replace(/\s+/g, '-')}` as TranslationKey);
    return this.i18n.tOr(key, token);
  }

  summaryTitle(job: JobResult | null): string {
    if (!job) {
      return '';
    }

    const nouns = this.nounPair(this.toolLabel);
    const verb = this.verbFor(this.verbPast);

    switch (job.status) {
      case 'success':
        return this.i18n.t('resultCard.summary.success', {
          counted: formatCount(this.successCount(), nouns.one, nouns.many),
          verb,
        });
      case 'partial_success':
        return this.i18n.t('resultCard.summary.partial', {
          done: this.successCount(),
          total: this.totalCount(),
          verb,
        });
      case 'failed':
        return this.i18n.t('resultCard.summary.failed');
      case 'cancelled':
        return this.i18n.t('resultCard.summary.cancelled');
      case 'interrupted':
        return this.i18n.t('resultCard.summary.interrupted');
      default:
        return job.message || this.i18n.t('resultCard.summary.finished');
    }
  }

  summaryDetail(job: JobResult | null): string {
    if (!job) {
      return '';
    }

    switch (job.status) {
      case 'partial_success':
        return (
          this.partialDetail.trim() || this.i18n.t('resultCard.detail.partial')
        );
      case 'failed':
        return localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(job.error),
        ).description;
      case 'cancelled':
        return this.i18n.t('resultCard.detail.cancelled');
      case 'interrupted':
        return this.i18n.t('resultCard.detail.interrupted');
      default:
        return '';
    }
  }

  statusLabel(job: JobResult): string {
    switch (job.status) {
      case 'success':
        return this.i18n.t('home.status.success');
      case 'partial_success':
        return this.i18n.t('home.status.partial');
      case 'failed':
        return this.i18n.t('home.status.failed');
      case 'cancelled':
        return this.i18n.t('home.status.cancelled');
      case 'interrupted':
        return this.i18n.t('home.status.interrupted');
      default:
        return job.status;
    }
  }

  summaryTone(job: JobResult | null): SummaryTone {
    switch (job?.status) {
      case 'success':
        return 'success';
      case 'partial_success':
      case 'interrupted':
        return 'warning';
      case 'failed':
        return 'danger';
      default:
        return 'neutral';
    }
  }

  summaryIcon(job: JobResult | null) {
    switch (job?.status) {
      case 'success':
        return this.checkIcon;
      case 'failed':
      case 'partial_success':
      case 'interrupted':
        return this.alertIcon;
      default:
        return this.cancelIcon;
    }
  }

  outputRows(): ResultOutputRow[] {
    const rows: ResultOutputRow[] = [];
    const seen = new Set<string>();
    const nouns = this.nounPair('file');

    for (const item of this.job?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs = (item.outputs ?? []).filter(
        (path): path is string => !!path,
      );
      const outputCount = Math.max(item.outputCount ?? 0, outputs.length);

      if (outputCount > GROUP_OUTPUT_LIMIT && !this.showAllOutputs) {
        const directory = item.outputPath || fileDir(outputs[0] ?? '');
        if (!directory || seen.has(directory)) {
          continue;
        }
        seen.add(directory);
        rows.push({
          path: directory,
          name: this.i18n.t('resultCard.folderSummary', {
            count: formatCount(outputCount, nouns.one, nouns.many),
            folder: fileName(directory),
          }),
          meta: '',
          grouped: true,
        });
        continue;
      }

      const paths =
        outputs.length > 0
          ? outputs
          : item.outputPath
            ? [item.outputPath]
            : [];

      for (const path of paths) {
        if (!path || seen.has(path)) {
          continue;
        }
        seen.add(path);
        // A directory summary has no file size to report; show its count.
        const isDirectorySummary =
          outputs.length === 0 &&
          path === item.outputPath &&
          (item.outputCount ?? 0) > 0;
        rows.push({
          path,
          name: fileName(path),
          meta: isDirectorySummary
            ? formatCount(item.outputCount ?? 0, nouns.one, nouns.many)
            : this.sizeLabel(path),
        });
      }
    }

    return rows;
  }

  /** Total output count used by the "show all" toggle label. */
  totalOutputCount(): number {
    return (this.job?.items ?? []).reduce((total, item) => {
      if (!item.success) {
        return total;
      }
      const outputs = (item.outputs ?? []).filter((path) => !!path);
      return total + Math.max(item.outputCount ?? 0, outputs.length);
    }, 0);
  }

  /** True when at least one successful item collapses more than four outputs. */
  showAllToggleVisible(): boolean {
    return (this.job?.items ?? []).some((item) => {
      if (!item.success) {
        return false;
      }
      const outputs = (item.outputs ?? []).filter((path) => !!path);
      return Math.max(item.outputCount ?? 0, outputs.length) > GROUP_OUTPUT_LIMIT;
    });
  }

  toggleAllOutputs(): void {
    this.showAllOutputs = !this.showAllOutputs;
  }

  failedRows(): ResultFailedRow[] {
    return (this.job?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => ({
        path: item.inputPath,
        name: fileName(item.inputPath),
        description: localizeToolError(
          (key) => this.i18n.t(key),
          describeToolError(item.error),
        ).description,
      }));
  }

  successCount(): number {
    const items = this.job?.items ?? [];
    if (items.length === 0) {
      return this.outputRows().length;
    }
    return items.filter((item) => item.success).length;
  }

  totalCount(): number {
    const items = this.job?.items ?? [];
    return items.length > 0 ? items.length : this.successCount();
  }

  hasFailedRows(): boolean {
    return this.failedRows().length > 0;
  }

  errorLines(): string[] {
    const lines: string[] = [];
    const seen = new Set<string>();

    const addError = (error?: JobError | null): void => {
      if (!error) {
        return;
      }
      const code = [error.detail_code, error.code]
        .filter((part): part is string => !!part)
        .join(' · ');
      const line = code
        ? `${code}${error.message ? ` — ${error.message}` : ''}`
        : error.message;
      if (line && !seen.has(line)) {
        seen.add(line);
        lines.push(line);
      }
    };

    addError(this.job?.error);
    for (const item of this.job?.items ?? []) {
      addError(item.error);
    }

    return lines;
  }

  startedAt(): string {
    return this.formatTimestamp(this.job?.startedAt);
  }

  endedAt(): string {
    return this.formatTimestamp(this.job?.endedAt);
  }

  private sizeLabel(path: string): string {
    const info = this.fileInfos[path];
    return info && typeof info.sizeBytes === 'number'
      ? formatBytes(info.sizeBytes)
      : '';
  }

  private formatTimestamp(value?: number): string {
    if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
      return '—';
    }
    return new Date(value).toLocaleString(this.i18n.locale());
  }
}
