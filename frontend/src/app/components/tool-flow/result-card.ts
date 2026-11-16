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
  FileInfoV1,
  JobErrorV1,
  JobResultV1,
  UndoResultV1,
  UndoSkippedV1,
  VerificationCheckV1,
  VerificationReportV1,
  Wails,
} from '../../services/wails';
import { describeToolError } from '../../services/tool-error';
import {
  RecentConversionsService,
  isTerminalJobStatus,
} from '../../services/recent-conversions.service';
import { fileDir, fileName, formatBytes, formatCount } from '../../utils/format';

const GROUP_OUTPUT_LIMIT = 4;

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
}

export interface ResultUndoSkippedRow {
  path: string;
  name: string;
  reason: string;
}

type SummaryTone = 'success' | 'warning' | 'danger' | 'neutral';
type UndoMode = 'undo' | 'restore';

@Component({
  selector: 'app-result-card',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './result-card.html',
  styleUrl: './result-card.css',
})
export class ResultCard implements DoCheck {
  private jobValue: JobResultV1 | null = null;
  private pendingRecord = false;
  private pendingVerify = false;
  private recordRecentValue = true;
  private verificationInputProvided = false;
  private verificationGeneration = 0;
  private readonly verificationRequestedJobIds = new Set<string>();

  verifyReport: VerificationReportV1 | null = null;
  verifyLoading = false;
  verifyUnavailable = false;

  undoBusy = false;
  undoMessage = '';
  undoTone: 'success' | 'warning' = 'success';
  undoSkipped: UndoSkippedV1[] = [];
  undoMode: UndoMode = 'undo';

  @Input()
  set job(value: JobResultV1 | null) {
    const previousJobId = this.jobValue?.jobId ?? '';
    this.jobValue = value;

    if (previousJobId !== (value?.jobId ?? '')) {
      this.resetAutoVerification();
      this.resetUndoState();
    }

    this.pendingRecord = true;
    this.pendingVerify = true;
    if (!value || !isTerminalJobStatus(value.status)) {
      this.pendingVerify = false;
      this.cancelVerificationRequest();
    }
  }

  get job(): JobResultV1 | null {
    return this.jobValue;
  }

  @Input()
  set verification(value: VerificationReportV1 | null) {
    this.verificationInputProvided = true;
    this.verifyReport = value;
    this.pendingVerify = false;
    this.cancelVerificationRequest();
    this.verifyUnavailable = false;
  }

  get verification(): VerificationReportV1 | null {
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
  @Input() fileInfos: Record<string, FileInfoV1> = {};
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

  private readonly recentConversions = inject(RecentConversionsService);
  private readonly wailsService = inject(Wails);
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

  private recordTerminalJob(job: JobResultV1 | null): void {
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

    let report: VerificationReportV1 | null = null;
    try {
      report = await this.wailsService.verifyJobV1(job.jobId);
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
    return this.verifyReport.success ? 'success' : 'warning';
  }

  verifyTitle(): string {
    return this.verifyReport?.success ? 'Verified' : 'Needs attention';
  }

  verifyIcon() {
    return this.verifyReport?.success
      ? this.verifyOkIcon
      : this.verifyAttentionIcon;
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
    return `${totals.passed}/${totals.total} checks passed`;
  }

  verifyOutputs(): ResultVerifyOutputRow[] {
    const report = this.verifyReport;
    if (!report || this.verifyLoading || this.verifyUnavailable) {
      return [];
    }

    return report.outputs.slice(0, 3).map((output) => {
      const checks = output.checks ?? [];
      const checksPassed = checks.filter((check) => check.passed).length;
      const passed =
        checks.length === 0 ? output.exists : checksPassed === checks.length;

      return {
        path: output.path,
        name: fileName(output.path),
        size: formatBytes(output.sizeBytes),
        passed,
        checksPassed,
        totalChecks: checks.length,
        warning: !passed
          ? (output.warnings?.[0] ?? 'One or more checks failed.')
          : undefined,
      };
    });
  }

  verifyChecks(): VerificationCheckV1[] {
    const outputs = this.verifyReport?.outputs ?? [];
    return outputs.length > 0 ? (outputs[0].checks ?? []).slice(0, 4) : [];
  }

  verifyCheckText(check: VerificationCheckV1): string {
    return check.value ? `${check.label} · ${check.value}` : check.label;
  }

  verifyWarnings(): string[] {
    return (this.verifyReport?.warnings ?? []).slice(0, 3);
  }

  private verificationTotals(report: VerificationReportV1): {
    passed: number;
    total: number;
  } {
    let passed = 0;
    let total = 0;
    for (const output of report.outputs) {
      for (const check of output.checks ?? []) {
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
    return this.undoMode === 'undo' ? 'Undo' : 'Restore';
  }

  undoActionIcon() {
    return this.undoMode === 'undo' ? this.undoIcon : this.restoreIcon;
  }

  undoSkippedRows(): ResultUndoSkippedRow[] {
    return this.undoSkipped.slice(0, 2).map((skip) => ({
      path: skip.path,
      name: skip.path ? fileName(skip.path) : 'A file',
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
          ? await this.wailsService.undoJobV1(job.jobId)
          : await this.wailsService.restoreJobV1(job.jobId);
      this.applyUndoResult(result, action);
    } catch {
      this.undoMessage =
        action === 'undo' ? 'Undo failed.' : 'Restore failed.';
      this.undoTone = 'warning';
      this.undoSkipped = [];
    } finally {
      this.undoBusy = false;
    }
  }

  private applyUndoResult(
    result: UndoResultV1 | null | undefined,
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

  summaryTitle(job: JobResultV1 | null): string {
    if (!job) {
      return '';
    }

    switch (job.status) {
      case 'success':
        return `${formatCount(this.successCount(), this.toolLabel)} ${this.verbPast}`;
      case 'partial_success':
        return `${this.successCount()} of ${this.totalCount()} ${this.verbPast}`;
      case 'failed':
        return "Couldn't finish the conversion";
      case 'cancelled':
        return 'Cancelled';
      case 'interrupted':
        return 'The job was interrupted';
      default:
        return job.message || 'Conversion finished';
    }
  }

  summaryDetail(job: JobResultV1 | null): string {
    if (!job) {
      return '';
    }

    switch (job.status) {
      case 'partial_success':
        return (
          this.partialDetail.trim() ||
          "Some files couldn't be converted. Retry them below."
        );
      case 'failed':
        return describeToolError(job.error).description;
      case 'cancelled':
        return 'No files were changed.';
      case 'interrupted':
        return 'The app stopped before the job finished.';
      default:
        return '';
    }
  }

  summaryTone(job: JobResultV1 | null): SummaryTone {
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

  summaryIcon(job: JobResultV1 | null) {
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

    for (const item of this.job?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const outputs = (item.outputs ?? []).filter(
        (path): path is string => !!path,
      );
      const outputCount = Math.max(item.outputCount ?? 0, outputs.length);

      if (outputCount > GROUP_OUTPUT_LIMIT) {
        const directory = item.outputPath || fileDir(outputs[0] ?? '');
        if (!directory || seen.has(directory)) {
          continue;
        }
        seen.add(directory);
        rows.push({
          path: directory,
          name: `${formatCount(outputCount, 'file')} in ${fileName(directory)}`,
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
            ? formatCount(item.outputCount ?? 0, 'file')
            : this.sizeLabel(path),
        });
      }
    }

    return rows;
  }

  failedRows(): ResultFailedRow[] {
    return (this.job?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => ({
        path: item.inputPath,
        name: fileName(item.inputPath),
        description: describeToolError(item.error).description,
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

    const addError = (error?: JobErrorV1): void => {
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
    return new Date(value).toLocaleString();
  }
}
