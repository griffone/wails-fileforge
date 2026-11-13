import { Component, EventEmitter, Input, Output } from '@angular/core';
import {
  Check,
  ExternalLink,
  FolderOpen,
  LucideAngularModule,
  RotateCcw,
  TriangleAlert,
  X,
} from 'lucide-angular';
import { FileInfoV1, JobErrorV1, JobResultV1 } from '../../services/wails';
import { describeToolError } from '../../services/tool-error';
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

type SummaryTone = 'success' | 'warning' | 'danger' | 'neutral';

@Component({
  selector: 'app-result-card',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './result-card.html',
  styleUrl: './result-card.css',
})
export class ResultCard {
  @Input() job: JobResultV1 | null = null;
  @Input() fileInfos: Record<string, FileInfoV1> = {};
  @Input() toolLabel = 'file';
  /** Past-tense verb for the success summary, e.g. `converted`, `split`. */
  @Input() verbPast = 'converted';

  @Output() open = new EventEmitter<string>();
  @Output() reveal = new EventEmitter<string>();
  @Output() retryFailed = new EventEmitter<void>();
  @Output() reset = new EventEmitter<void>();

  readonly checkIcon = Check;
  readonly alertIcon = TriangleAlert;
  readonly folderIcon = FolderOpen;
  readonly externalIcon = ExternalLink;
  readonly retryIcon = RotateCcw;
  readonly cancelIcon = X;

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
        return "Some files couldn't be converted. Retry them below.";
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
