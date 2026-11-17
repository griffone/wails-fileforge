import { CommonModule } from '@angular/common';
import { Component, Input } from '@angular/core';
import { JobResultV1 } from '../../services/wails';

@Component({
  selector: 'app-job-card',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './job-card.html',
  styleUrl: './job-card.css',
})
export class JobCard {
  @Input() job: JobResultV1 | null = null;

  currentStage(): string {
    return this.job?.progress?.stage ?? (this.job ? this.job.status : 'queued');
  }

  stageLabel(): string {
    const s = this.currentStage();
    switch (s) {
      case 'uploading':
        return 'Uploading';
      case 'processing':
        return 'Processing';
      case 'finalizing':
        return 'Finalizing';
      case 'done':
        return 'Done';
      case 'failed':
        return 'Failed';
      case 'cancelled':
        return 'Cancelled';
      case 'queued':
        return 'Queued';
      case 'running':
        return 'Running';
      case 'ocr':
        return 'OCR';
      case 'success':
        return 'Done';
      case 'partial_success':
        return 'Partial success';
      case 'interrupted':
        return 'Interrupted';
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
        return 'Waiting in the queue';
      case 'uploading':
        return 'Sending files to the engine';
      case 'processing':
        return 'Working on your files';
      case 'finalizing':
        return 'Finalizing output';
      case 'done':
        return 'Job completed';
      case 'failed':
        return 'Job failed';
      case 'cancelled':
        return 'Job cancelled';
      default:
        return 'Pending';
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
        return 'Writing results';
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
