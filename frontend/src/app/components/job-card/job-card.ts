import { CommonModule } from '@angular/common';
import { Component, Input, inject } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { JobResult } from '../../services/wails';

@Component({
  selector: 'app-job-card',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './job-card.html',
  styleUrl: './job-card.css',
})
export class JobCard {
  @Input() job: JobResult | null = null;

  readonly i18n = inject(I18nService);

  currentStage(): string {
    return this.job?.progress?.stage ?? (this.job ? this.job.status : 'queued');
  }

  stageLabel(): string {
    const s = this.currentStage();
    switch (s) {
      case 'uploading':
        return this.i18n.t('jobCard.stage.uploading');
      case 'processing':
        return this.i18n.t('jobCard.stage.processing');
      case 'finalizing':
        return this.i18n.t('jobCard.stage.finalizing');
      case 'done':
      case 'transcribe.done':
      case 'success':
        return this.i18n.t('jobCard.stage.done');
      case 'failed':
        return this.i18n.t('jobCard.stage.failed');
      case 'cancelled':
        return this.i18n.t('jobCard.stage.cancelled');
      case 'queued':
        return this.i18n.t('jobCard.stage.queued');
      case 'running':
        return this.i18n.t('jobCard.stage.running');
      case 'ocr':
        return this.i18n.t('jobCard.stage.ocr');
      case 'extract':
        return this.i18n.t('jobCard.stage.extract');
      case 'transcribe':
        return this.i18n.t('jobCard.stage.transcribe');
      case 'publish':
        return this.i18n.t('jobCard.stage.publish');
      case 'partial_success':
        return this.i18n.t('jobCard.stage.partial');
      case 'interrupted':
        return this.i18n.t('jobCard.stage.interrupted');
      default:
        return s;
    }
  }

  stageMessage(): string {
    return this.job?.progress?.message ?? this.job?.message ?? '';
  }

  /** Human copy for the status line; falls back to stage-specific wording. */
  messageText(): string {
    const message = this.stageMessage().trim();
    if (message) {
      return message;
    }

    switch (this.currentStage()) {
      case 'queued':
        return this.i18n.t('jobCard.message.queued');
      case 'uploading':
        return this.i18n.t('jobCard.message.uploading');
      case 'processing':
        return this.i18n.t('jobCard.message.processing');
      case 'finalizing':
        return this.i18n.t('jobCard.message.finalizing');
      case 'done':
        return this.i18n.t('jobCard.message.done');
      case 'failed':
        return this.i18n.t('jobCard.message.failed');
      case 'cancelled':
        return this.i18n.t('jobCard.message.cancelled');
      default:
        return this.i18n.t('jobCard.message.pending');
    }
  }

  overallPercent(): number {
    const p = this.job?.progress;
    if (!p || typeof p.current !== 'number' || typeof p.total !== 'number' || p.total <= 0) return 0;
    return Math.round((p.current / p.total) * 100);
  }

  /** Stage + percent announced by the progressbar. */
  progressValueText(): string {
    return `${this.stageLabel()} ${this.overallPercent()}%`;
  }

  isFinalizing(): boolean {
    return this.currentStage() === 'finalizing';
  }

  /** Active stages with no measurable progress render a moving bar. */
  isIndeterminate(): boolean {
    if (this.overallPercent() > 0) {
      return false;
    }

    return ['queued', 'uploading', 'processing', 'finalizing'].includes(this.currentStage());
  }

  /** Secondary line: byte counts, item counts or the finalizing hint. */
  detailText(): string {
    switch (this.currentStage()) {
      case 'uploading':
        return this.bytesInfo() ?? '';
      case 'processing':
        return this.processingInfo();
      case 'finalizing':
        return this.i18n.t('jobCard.detail.writing');
      default:
        return '';
    }
  }

  bytesInfo(): string | null {
    const p = this.job?.progress;
    if (!p) return null;
    const bt = (p as any).bytesTotal;
    const b = (p as any).bytesTransferred;
    if (typeof bt === 'number' && typeof b === 'number') {
      return `${this.humanBytes(b)} / ${this.humanBytes(bt)}`;
    }
    return null;
  }

  processingInfo(): string {
    const p = this.job?.progress;
    if (!p) return '';
    if (typeof p.current === 'number' && typeof p.total === 'number' && p.total > 0) {
      return `${p.current} / ${p.total}`;
    }
    // fallback to items
    const items = this.job?.items ?? [];
    const processed = items.filter((i) => i.success || i.message === 'done' || i.outputPath).length;
    return items.length > 0 ? `${processed} / ${items.length}` : '';
  }

  etaText(): string | null {
    const eta = this.job?.progress?.etaSeconds;
    if (typeof eta === 'number' && !isNaN(eta)) {
      const mins = Math.floor(eta / 60);
      const secs = Math.round(eta % 60);
      return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
    }
    return null;
  }

  private humanBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  }
}
