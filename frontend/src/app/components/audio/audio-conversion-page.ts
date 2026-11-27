import { computed, signal } from '@angular/core';

import { AudioToolPage } from './audio-tool-page';
import type { AudioConversionPageCopy } from './audio-tool-config';
import {
  AUDIO_BITRATE_OPTIONS,
  AUDIO_FORMAT_LABELS,
  AUDIO_FORMATS,
  DEFAULT_AUDIO_FORMAT,
  isAudioFormat,
  isLosslessAudioFormat,
  normalizeAudioConversionOptions,
  defaultAudioConversionOptions,
} from './audio-tool-config';
import type { AudioFormat } from './audio-tool-config';

/**
 * Shared option form for extract and convert.
 *
 * Both tools send the same two backend keys: `format` (one of the manifest's
 * five formats, defaulting to mp3) and an optional `bitrateKbps`. When the
 * format is lossless the bitrate control is disabled, the key is omitted from
 * the request, and the page states the same warning the backend plan emits
 * instead of dropping the value silently.
 */
export abstract class AudioConversionPage extends AudioToolPage {
  declare readonly copy: AudioConversionPageCopy;

  readonly formats = AUDIO_FORMATS;
  readonly formatLabels = AUDIO_FORMAT_LABELS;
  readonly bitrateOptions = AUDIO_BITRATE_OPTIONS;

  readonly format = signal<AudioFormat>(DEFAULT_AUDIO_FORMAT);
  readonly bitrateKbps = signal<number | null>(null);

  readonly isLossless = computed(() =>
    isLosslessAudioFormat(this.format()),
  );

  outputBadge(): string {
    const detail = this.isLossless()
      ? this.i18n.t('audioTool.losslessShort')
      : this.bitrateKbps() !== null
        ? this.i18n.t('audioTool.bitrate.option', {
            value: this.bitrateKbps() ?? 0,
          })
        : this.i18n.t('audioTool.bitrate.auto');

    return this.i18n.t('audioTool.output.summary', {
      format: AUDIO_FORMAT_LABELS[this.format()],
      detail,
    });
  }

  /** Empty for lossy formats; the backend's lossless-bitrate warning otherwise. */
  losslessWarning(): string {
    return this.isLossless()
      ? this.i18n.t('audioTool.bitrate.losslessWarning', {
          format: AUDIO_FORMAT_LABELS[this.format()],
        })
      : '';
  }

  onFormatChange(event: Event): void {
    const value = this.readSelect(event);
    if (!isAudioFormat(value)) {
      return;
    }

    this.format.set(value);
    // A lossless output ignores bitrate, so the control never carries a value
    // the backend would warn about.
    if (isLosslessAudioFormat(value)) {
      this.bitrateKbps.set(null);
    }
    this.refreshPlanForOptions();
  }

  onBitrateChange(event: Event): void {
    const value = this.readSelect(event);
    if (value === '') {
      this.bitrateKbps.set(null);
      this.refreshPlanForOptions();
      return;
    }

    const kbps = Number(value);
    if (!(AUDIO_BITRATE_OPTIONS as readonly number[]).includes(kbps)) {
      return;
    }
    this.bitrateKbps.set(kbps);
    this.refreshPlanForOptions();
  }

  protected readSelect(event: Event): string {
    return (event.target as HTMLSelectElement | null)?.value ?? '';
  }

  protected defaultOptions(): Record<string, unknown> {
    return { ...defaultAudioConversionOptions() };
  }

  protected normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>> {
    return normalizeAudioConversionOptions(stored);
  }

  protected applyOptions(options: Record<string, unknown>): void {
    const restored = normalizeAudioConversionOptions(options);
    const format = restored.format ?? DEFAULT_AUDIO_FORMAT;

    this.format.set(format);
    this.bitrateKbps.set(
      isLosslessAudioFormat(format) ? null : (restored.bitrateKbps ?? null),
    );
  }

  protected currentOptions(): Record<string, unknown> {
    return {
      format: this.format(),
      bitrateKbps: this.bitrateKbps(),
    };
  }

  protected buildOptions(): Record<string, unknown> {
    const options: Record<string, unknown> = { format: this.format() };
    const bitrateKbps = this.bitrateKbps();
    if (!this.isLossless() && bitrateKbps !== null) {
      options['bitrateKbps'] = bitrateKbps;
    }

    return options;
  }
}
