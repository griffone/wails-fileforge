import { Component, Input, inject } from '@angular/core';
import {
  Check,
  LucideAngularModule,
  TriangleAlert,
} from 'lucide-angular';

import { JobResult } from '../../services/wails';
import { I18nService } from '../../services/i18n.service';
import { fileName, formatBytes } from '../../utils/format';
import { videoDetailNumber } from '../../utils/video-target';

/** One finished item whose target outcome the backend reported. */
export interface VideoTargetReportRow {
  path: string;
  name: string;
  /** `actual of target target · N kbps`, ready to render. */
  values: string;
  /** Empty when the quality floor did not apply. */
  floor: string;
  met: boolean;
}

/**
 * Honest target report for the three video pages.
 *
 * The backend attaches a `details` map to every item encoded for a target
 * (targetSizeBytes, actualSizeBytes, targetBitrateKbps, floorKbps,
 * floorApplied, targetMet, attempts). The panel renders only what that map
 * contains: an item without details (no target, older job, failed file) adds
 * no row, and a job with no such item renders no panel at all. It never
 * guesses a met state from the output size.
 */
@Component({
  selector: 'app-video-target-report',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './video-target-report.html',
  styleUrl: './video-target-report.css',
})
export class VideoTargetReport {
  readonly i18n = inject(I18nService);

  readonly checkIcon = Check;
  readonly alertIcon = TriangleAlert;

  @Input() job: JobResult | null = null;

  rows(): VideoTargetReportRow[] {
    const rows: VideoTargetReportRow[] = [];
    for (const item of this.job?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const details = item.details ?? null;
      if (!details) {
        continue;
      }

      const target = videoDetailNumber(details, 'targetSizeBytes');
      const actual = videoDetailNumber(details, 'actualSizeBytes');
      const bitrate = videoDetailNumber(details, 'targetBitrateKbps');
      if (target === null || actual === null || bitrate === null) {
        continue;
      }

      const floorKbps = videoDetailNumber(details, 'floorKbps');
      const floorApplied = details['floorApplied'] === true;
      const path = item.outputPath || item.inputPath;

      rows.push({
        path,
        name: fileName(path),
        values: this.i18n.t('videoTarget.result.values', {
          actual: formatBytes(actual),
          target: formatBytes(target),
          bitrate: Math.round(bitrate),
        }),
        floor:
          floorApplied && floorKbps !== null
            ? this.i18n.t('videoTarget.result.floor', {
                floor: Math.round(floorKbps),
              })
            : '',
        met: details['targetMet'] === true,
      });
    }

    return rows;
  }
}
