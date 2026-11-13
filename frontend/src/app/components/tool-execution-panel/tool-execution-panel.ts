import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormGroup, ReactiveFormsModule } from '@angular/forms';
import { JobResultItemV1, JobResultV1 } from '../../services/wails';
import { JobCard } from '../job-card/job-card';
import { FEATURE_FLAGS } from '../../config/feature-flags';

export interface ExecutionPanelOption {
  value: string;
  label: string;
}

export type ExecutionPanelFieldType = 'text' | 'textarea' | 'select';

export interface ExecutionPanelField {
  controlName: string;
  label: string;
  type: ExecutionPanelFieldType;
  placeholder?: string;
  helpText?: string;
  options?: ExecutionPanelOption[];
  visibleModes?: string[];
}

@Component({
  selector: 'app-tool-execution-panel',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, JobCard],
  templateUrl: './tool-execution-panel.html',
  styleUrl: './tool-execution-panel.css',
})
export class ToolExecutionPanel {
  private static nextId = 0;

  readonly featureFlags = FEATURE_FLAGS;
  private readonly instanceId = `execution-${(ToolExecutionPanel.nextId += 1)}`;
  readonly jobHeadingId = `${this.instanceId}-job-status`;
  @Input() title = 'Tool Execution';
  @Input() subtitle = '';
  @Input() form: FormGroup = new FormGroup({});
  @Input() fields: ExecutionPanelField[] = [];
  @Input() currentMode = 'single';

  @Input() validationMessage = '';
  @Input() inlineFieldErrors: Record<string, string> = {};
  @Input() submitMessage = '';
  @Input() statusMessage = '';
  @Input() isSubmitting = false;
  @Input() isPolling = false;
  @Input() canCancel = false;
  @Input() jobResult: JobResultV1 | null = null;

  @Input() runButtonText = 'Run Job';
  @Input() submittingButtonText = 'Submitting...';

  @Output() validate = new EventEmitter<void>();
  @Output() run = new EventEmitter<void>();
  @Output() cancel = new EventEmitter<void>();

  shouldRender(field: ExecutionPanelField): boolean {
    if (!field.visibleModes || field.visibleModes.length === 0) {
      return true;
    }

    return field.visibleModes.includes(this.currentMode);
  }

  fieldError(controlName: string): string {
    return this.inlineFieldErrors[controlName] ?? '';
  }

  /** Stable id for a field's help line, used by aria-describedby. */
  helpId(field: ExecutionPanelField): string {
    return `${this.instanceId}-${field.controlName}-help`;
  }

  /** Stable id for a field's error line, used by aria-describedby. */
  errorId(field: ExecutionPanelField): string {
    return `${this.instanceId}-${field.controlName}-error`;
  }

  /** Joins the ids of the help and error text that are actually rendered. */
  describedBy(field: ExecutionPanelField): string | null {
    const ids: string[] = [];
    if (field.helpText) {
      ids.push(this.helpId(field));
    }
    if (this.fieldError(field.controlName)) {
      ids.push(this.errorId(field));
    }
    return ids.length > 0 ? ids.join(' ') : null;
  }

  progressPercent(): number {
    if (!this.jobResult || this.jobResult.progress.total <= 0) {
      return 0;
    }

    return Math.round((this.jobResult.progress.current / this.jobResult.progress.total) * 100);
  }

  itemOutputs(item: JobResultItemV1): string[] {
    if (item.outputs && item.outputs.length > 0) {
      return item.outputs;
    }
    return [];
  }

  itemStatusLabel(item: JobResultItemV1): string {
    return item.success ? 'success' : 'failed';
  }

  // Accessibility helper: announce stage transitions via aria-live region in parent components
  ariaStageAnnouncement(): string {
    return this.jobResult?.progress?.message ?? '';
  }

  statusLabel(): string {
    const status = this.jobResult?.status;
    switch (status) {
      case 'queued':
        return 'Queued';
      case 'running':
        return 'Running';
      case 'success':
        return 'Completed';
      case 'failed':
        return 'Failed';
      case 'partial_success':
        return 'Completed with errors';
      case 'cancelled':
        return 'Cancelled';
      case 'interrupted':
        return 'Interrupted';
      default:
        return status ?? 'Unknown';
    }
  }

  statusBadgeClass(): string {
    const status = this.jobResult?.status;
    switch (status) {
      case 'success':
        return 'badge badge-success';
      case 'partial_success':
        return 'badge badge-warning';
      case 'failed':
      case 'cancelled':
      case 'interrupted':
        return 'badge badge-danger';
      case 'running':
      case 'queued':
      default:
        return 'badge badge-neutral';
    }
  }

  itemRetryLabel(item: JobResultItemV1): string {
    const attempts = item.attempts ?? 1;
    const retries = item.retryCount ?? Math.max(0, attempts - 1);
    if (retries <= 0) {
      return 'none';
    }
    return retries === 1 ? `1 retry, ${attempts} attempts total` : `${retries} retries, ${attempts} attempts total`;
  }

  summaryCounts(): { success: number; failed: number; retries: number } {
    const items = this.jobResult?.items ?? [];
    let success = 0;
    let failed = 0;
    let retries = 0;
    for (const item of items) {
      if (item.success) {
        success++;
      } else {
        failed++;
      }
      retries += item.retryCount ?? Math.max(0, (item.attempts ?? 1) - 1);
    }
    return { success, failed, retries };
  }

  interruptedBanner(): string {
    if (this.jobResult?.status !== 'interrupted') {
      return '';
    }
    return 'This job was interrupted by an app restart. Automatic resume is planned for a future roadmap release; v1 does not auto-resume.';
  }

  itemErrorText(item: JobResultItemV1): string {
    if (!item.error) {
      return item.message;
    }

    const detailCode = (item.error.detail_code ?? '').toUpperCase();
    const friendlyByDetail: Record<string, string> = {
      PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS: 'The page range exceeds the number of pages available in the PDF.',
      PDF_CROP_PAGE_SELECTION_INVALID: 'The page range is not formatted correctly.',
      PDF_INVALID_INPUTS: 'One or more input PDFs are invalid or could not be read.',
      PDF_MERGE_OUTPUT_ALREADY_EXISTS: 'The output file already exists. Choose another name to continue.',
      DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED: 'The file is not a supported DOCX document.',
      DOC_MD_TO_PDF_INPUT_UNSUPPORTED: 'The file is not supported Markdown.',
    };

    const friendlyMessage = friendlyByDetail[detailCode];
    if (friendlyMessage) {
      return `${friendlyMessage} (${item.error.message})`;
    }

    const detail = item.error.detail_code ? ` [${item.error.detail_code}]` : '';
    return `${item.error.code}${detail}: ${item.error.message}`;
  }

  aggregateFileErrors(): Array<{ path: string; code: string; message: string }> {
    const raw = this.jobResult?.error?.details?.['fileErrors'];
    if (!Array.isArray(raw)) {
      return [];
    }

    const normalized: Array<{ path: string; code: string; message: string }> = [];
    for (const entry of raw) {
      if (!entry || typeof entry !== 'object') {
        continue;
      }

      const candidate = entry as Record<string, unknown>;
      const path = typeof candidate['path'] === 'string' ? candidate['path'] : '';
      const code = typeof candidate['code'] === 'string' ? candidate['code'] : '';
      const message = typeof candidate['message'] === 'string' ? candidate['message'] : '';
      if (!path && !code && !message) {
        continue;
      }

      normalized.push({ path, code, message });
    }

    return normalized;
  }
}
