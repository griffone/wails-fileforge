import { Component, computed, signal } from '@angular/core';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { fileName } from '../../utils/format';
import { AUDIO_NORMALIZE_COPY } from './audio-copy';
import { AudioToolPage, audioToolPageDeps } from './audio-tool-page';
import {
  AUDIO_INPUT_ACCEPT,
  AUDIO_INPUT_EXTENSIONS,
  DEFAULT_LOUDNORM_TARGET,
  DEFAULT_TRUE_PEAK,
  LOUDNORM_TARGETS,
  TRUE_PEAK_MAX,
  TRUE_PEAK_MIN,
  detailNumber,
  formatLoudness,
  isLoudnormTarget,
  normalizeAudioNormalizeOptions,
  parseTruePeak,
} from './audio-tool-config';
import type { LoudnormTarget } from './audio-tool-config';

const AUDIO_NORMALIZE_TOOL_ID = 'tool.audio.normalize';

/** One measured row: the item's input and output integrated loudness. */
export interface AudioLoudnessRow {
  path: string;
  name: string;
  values: string;
}

/**
 * Loudness normalization.
 *
 * Normalize has no target-format option: the backend preserves the input
 * container (or follows an explicit outputPath, which this page does not
 * expose). The form therefore only sends `targetLufs` and `truePeak`.
 *
 * When a finished item carries a `details` map, the page shows the measured
 * input and output loudness. An item without details renders no row at all, so
 * the panel remains hidden rather than showing a placeholder.
 */
@Component({
  selector: 'app-audio-normalize',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './audio-normalize.html',
  styleUrl: './audio-tools.css',
})
export class AudioNormalize extends AudioToolPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly targets = LOUDNORM_TARGETS;
  readonly truePeakMin = TRUE_PEAK_MIN;
  readonly truePeakMax = TRUE_PEAK_MAX;

  readonly targetLufs = signal<LoudnormTarget>(DEFAULT_LOUDNORM_TARGET);
  /** Held as text so an out-of-range keystroke is never clamped silently. */
  readonly truePeakInput = signal(String(DEFAULT_TRUE_PEAK));

  readonly truePeakValue = computed(() => parseTruePeak(this.truePeakInput()));

  readonly truePeakError = computed(() => {
    if (!this.truePeakInput().trim() || this.truePeakValue() !== null) {
      return '';
    }
    return this.i18n.t('audioNormalize.truePeak.error', {
      min: TRUE_PEAK_MIN,
      max: TRUE_PEAK_MAX,
    });
  });

  readonly loudnessRows = computed<AudioLoudnessRow[]>(() => {
    const rows: AudioLoudnessRow[] = [];
    for (const item of this.terminalJob()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const details = item.details ?? null;
      if (!details) {
        continue;
      }

      const input = detailNumber(details, 'inputLufs');
      const output = detailNumber(details, 'outputLufs');
      if (input === null || output === null) {
        continue;
      }

      const path = item.outputPath || item.inputPath;
      rows.push({
        path,
        name: fileName(path),
        values: this.i18n.t('audioTool.loudness.row', {
          input: formatLoudness(input),
          output: formatLoudness(output),
        }),
      });
    }

    return rows;
  });

  constructor() {
    super(audioToolPageDeps(), {
      toolId: AUDIO_NORMALIZE_TOOL_ID,
      route: '/audio-normalize',
      copy: AUDIO_NORMALIZE_COPY,
      verbPast: 'normalized',
      acceptAttribute: AUDIO_INPUT_ACCEPT,
      acceptedExtensions: AUDIO_INPUT_EXTENSIONS,
      optionsSchemaVersion: 1,
    });
  }

  outputBadge(): string {
    const truePeak = this.truePeakValue() ?? DEFAULT_TRUE_PEAK;
    return this.i18n.t('audioTool.output.summary', {
      format: this.targetLabel(this.targetLufs()),
      detail: this.i18n.t('audioTool.truePeak.value', {
        value: truePeak.toFixed(1),
      }),
    });
  }

  targetLabel(target: LoudnormTarget): string {
    return this.i18n.t(`audioNormalize.target.${target}`);
  }

  onTargetChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (!isLoudnormTarget(value)) {
      return;
    }

    this.targetLufs.set(value);
    this.refreshPlanForOptions();
  }

  onTruePeakInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.truePeakInput.set(value);

    if (this.truePeakValue() === null) {
      // An invalid value cannot be planned, so the old plan must not linger.
      this.clearPlanForOptions();
      return;
    }

    this.refreshPlanForOptions();
  }

  protected override toolBlocked(): boolean {
    return this.truePeakValue() === null;
  }

  protected defaultOptions(): Record<string, unknown> {
    return {
      targetLufs: DEFAULT_LOUDNORM_TARGET,
      truePeak: DEFAULT_TRUE_PEAK,
    };
  }

  protected normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>> {
    return normalizeAudioNormalizeOptions(stored);
  }

  protected applyOptions(options: Record<string, unknown>): void {
    const restored = normalizeAudioNormalizeOptions(options);
    const target = restored.targetLufs ?? DEFAULT_LOUDNORM_TARGET;
    const truePeak = restored.truePeak ?? DEFAULT_TRUE_PEAK;

    this.targetLufs.set(target);
    this.truePeakInput.set(String(truePeak));
  }

  protected currentOptions(): Record<string, unknown> {
    return {
      targetLufs: this.targetLufs(),
      truePeak: this.truePeakValue() ?? DEFAULT_TRUE_PEAK,
    };
  }

  protected buildOptions(): Record<string, unknown> {
    return {
      targetLufs: this.targetLufs(),
      truePeak: this.truePeakValue() ?? DEFAULT_TRUE_PEAK,
    };
  }
}
