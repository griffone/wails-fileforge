import { CommonModule } from '@angular/common';
import { Component, Input } from '@angular/core';
import { JobResultV1 } from '../../services/wails';

@Component({
  selector: 'app-job-card',
  standalone: true,
  imports: [CommonModule],
  template: `
    <div class="job-card">
      <div class="flex justify-between items-center">
        <div>
          <p class="text-sm font-semibold">Stage: {{ stageLabel() }}</p>
          <p class="text-xs text-gray-600">{{ stageMessage() }}</p>
        </div>
        <div class="text-sm">{{ overallPercent() }}%</div>
      </div>

      <div class="mt-2">
        <ng-container [ngSwitch]="currentStage()">
          <div *ngSwitchCase="'uploading'">
            <div *ngIf="bytesInfo(); else indetUpload">
              <p class="text-xs">{{ bytesInfo() }}</p>
              <div class="h-2 w-full overflow-hidden rounded bg-gray-200">
                <div class="h-full bg-blue-500" role="progressbar" aria-valuemin="0" aria-valuemax="100" [attr.aria-valuenow]="overallPercent()" [style.width.%]="overallPercent()"></div>
              </div>
            </div>
            <ng-template #indetUpload>
              <div role="status">Uploading…</div>
            </ng-template>
          </div>

          <div *ngSwitchCase="'processing'">
            <p class="text-xs">{{ processingInfo() }}</p>
            <div class="h-2 w-full overflow-hidden rounded bg-gray-200">
              <div class="h-full bg-green-500" role="progressbar" aria-valuemin="0" aria-valuemax="100" [attr.aria-valuenow]="overallPercent()" [style.width.%]="overallPercent()"></div>
            </div>
          </div>

          <div *ngSwitchCase="'finalizing'">
            <div role="status" class="flex items-center gap-2">
              <div class="spinner" aria-hidden="true"></div>
              <span class="text-xs">{{ stageMessage() || 'Finalizing…' }}</span>
            </div>
          </div>

          <div *ngSwitchCase="'done'">
            <p class="text-xs">Done. {{ stageMessage() }}</p>
          </div>

          <div *ngSwitchCase="'failed'">
            <p class="text-xs text-red-700">Failed. {{ stageMessage() }}</p>
          </div>

          <div *ngSwitchCase="'cancelled'">
            <p class="text-xs">Cancelled.</p>
          </div>

          <div *ngSwitchDefault>
            <p class="text-xs">{{ stageMessage() || 'Pending' }}</p>
          </div>
        </ng-container>
      </div>

      <div *ngIf="etaText()" class="mt-2 text-xs text-gray-500">ETA: {{ etaText() }}</div>
    </div>
  `,
  styles: [
    `.spinner { width: 16px; height: 16px; border: 2px solid rgba(0,0,0,0.1); border-top-color: rgba(0,0,0,0.6); border-radius: 50%; animation: spin 1s linear infinite } @keyframes spin { to { transform: rotate(360deg) } }`,
  ],
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
      default:
        return s;
    }
  }

  stageMessage(): string {
    return this.job?.progress?.message ?? this.job?.message ?? '';
  }

  overallPercent(): number {
    const p = this.job?.progress;
    if (!p || typeof p.current !== 'number' || typeof p.total !== 'number' || p.total <= 0) return 0;
    return Math.round((p.current / p.total) * 100);
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
