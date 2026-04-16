import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormGroup, ReactiveFormsModule } from '@angular/forms';
import { JobResultItemV1, JobResultV1 } from '../../services/wails';
import { JobCard } from '../job-card/job-card';

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

  statusLabel(): string {
    const status = this.jobResult?.status;
    switch (status) {
      case 'queued':
        return 'En cola';
      case 'running':
        return 'En ejecución';
      case 'success':
        return 'Completado';
      case 'failed':
        return 'Falló';
      case 'partial_success':
        return 'Completado con fallos';
      case 'cancelled':
        return 'Cancelado';
      case 'interrupted':
        return 'Interrumpido por reinicio';
      default:
        return status ?? 'Desconocido';
    }
  }

  statusBadgeClass(): string {
    const status = this.jobResult?.status;
    switch (status) {
      case 'success':
        return 'badge-success';
      case 'partial_success':
        return 'badge-warning';
      case 'failed':
      case 'cancelled':
      case 'interrupted':
        return 'badge-danger';
      case 'running':
      case 'queued':
      default:
        return 'badge-neutral';
    }
  }

  itemRetryLabel(item: JobResultItemV1): string {
    const attempts = item.attempts ?? 1;
    const retries = item.retryCount ?? Math.max(0, attempts - 1);
    if (retries <= 0) {
      return 'sin reintentos';
    }
    return `${retries} reintento(s), ${attempts} intento(s) total`;
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
    return 'Este job quedó interrumpido por reinicio de la app. La reanudación automática queda para un roadmap futuro (v1 no auto-resume).';
  }

  itemErrorText(item: JobResultItemV1): string {
    if (!item.error) {
      return item.message;
    }

    const detailCode = (item.error.detail_code ?? '').toUpperCase();
    const friendlyByDetail: Record<string, string> = {
      PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS: 'El rango de páginas excede la cantidad disponible en el PDF.',
      PDF_CROP_INVALID_PAGE_SELECTION: 'El rango de páginas tiene formato inválido.',
      PDF_MERGE_INVALID_INPUTS: 'Uno o más PDFs de entrada son inválidos o no se pudieron leer.',
      DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED: 'El archivo no es un DOCX soportado.',
      DOC_MD_TO_PDF_INPUT_UNSUPPORTED: 'El archivo no es Markdown soportado.',
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
