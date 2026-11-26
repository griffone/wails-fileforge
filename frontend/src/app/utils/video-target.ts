import { computed, signal } from '@angular/core';

import {
  DestinationPreset,
  MAX_TARGET_MB,
  MAX_TARGET_MB_LABEL,
  mbToTargetBytes,
  presetForTargetMb,
  presetTargetMb,
  targetSizeLabel,
} from '../config/destination-presets';
import { I18nService } from '../services/i18n.service';

/**
 * Target-size form state shared by video convert, trim and merge.
 *
 * It reuses the destination presets and the binary MB convention of the image
 * and PDF pages, so `1 MB` means the same 1,048,576 bytes everywhere. A null
 * field is "no target" and stays valid: unlike image compress, the video pages
 * only lower the size when the user asks for it.
 *
 * A typed value that cannot become a byte target (zero, negative, too large)
 * is invalid and blocks the plan and the run, exactly like the other pages.
 * The backend validates `targetSizeBytes` on its own, so the form never sends
 * an invalid number.
 */
export class VideoTarget {
  readonly targetMb = signal<number | null>(null);

  constructor(private readonly i18n: I18nService) {}

  /** Whole bytes the engine expects; 0 when there is no usable target. */
  readonly sizeBytes = computed(() => {
    const mb = this.targetMb();
    if (mb === null || mb <= 0 || mb > MAX_TARGET_MB) {
      return 0;
    }
    return mbToTargetBytes(mb);
  });

  readonly error = computed<string | null>(() => {
    const mb = this.targetMb();
    if (mb === null) {
      return null;
    }
    if (!Number.isFinite(mb) || mb <= 0) {
      return this.i18n.t('videoTarget.target.errorPositive');
    }
    if (mb > MAX_TARGET_MB) {
      return this.i18n.t('videoTarget.target.errorMax', {
        max: MAX_TARGET_MB_LABEL,
      });
    }
    if (this.sizeBytes() <= 0) {
      return this.i18n.t('videoTarget.target.errorTooLarge');
    }
    return null;
  });

  /** False only for a typed value that no byte target can represent. */
  readonly valid = computed(() => this.error() === null);

  /** True when the form will send a target with the request. */
  readonly hasTarget = computed(() => this.valid() && this.sizeBytes() > 0);

  readonly activePreset = computed<DestinationPreset | null>(() =>
    this.hasTarget() ? presetForTargetMb(this.targetMb()) : null,
  );

  readonly activePresetId = computed<string | null>(
    () => this.activePreset()?.id ?? null,
  );

  readonly label = computed(() =>
    this.hasTarget()
      ? targetSizeLabel(this.sizeBytes())
      : this.i18n.t('videoTarget.target.noTarget'),
  );

  readonly summary = computed(() =>
    this.hasTarget()
      ? this.i18n.t('videoTarget.target.summary', { size: this.label() })
      : '',
  );

  /** Preset shown on the plan card so the choice survives to confirmation. */
  readonly planPresetLabel = computed(() => {
    const preset = this.activePreset();
    return preset ? this.presetLabel(preset) : '';
  });

  readonly planValue = computed(() => (this.hasTarget() ? this.label() : ''));

  /** Preset copy resolved at render time so the locale switch applies. */
  presetLabel(preset: DestinationPreset): string {
    return this.i18n.tOr(`destinationPreset.${preset.id}.label`, preset.label);
  }

  presetSizeLabel(preset: DestinationPreset): string {
    return targetSizeLabel(preset.targetBytes);
  }

  /** A preset writes its exact MB value into the field. */
  selectPreset(preset: DestinationPreset): void {
    this.targetMb.set(presetTargetMb(preset));
  }

  /** Adopts a remembered value; anything unusable becomes "no target". */
  restore(value: number | null | undefined): void {
    this.targetMb.set(
      typeof value === 'number' && Number.isFinite(value) ? value : null,
    );
  }
}

/**
 * Parses the MB field. An empty field means "no target"; a malformed number
 * is treated the same as empty, as the image page does.
 */
export function parseTargetMb(raw: string): number | null {
  const text = raw.trim();
  if (text === '') {
    return null;
  }
  const value = Number.parseFloat(text);
  return Number.isFinite(value) ? value : null;
}

/**
 * Maps a remembered value onto the range the field accepts. `null` is kept so
 * a remembered "no target" survives; an invalid value is dropped so the
 * defaults apply.
 */
export function normalizeTargetMb(
  value: unknown,
): number | null | undefined {
  if (value === null) {
    return null;
  }
  if (
    typeof value === 'number' &&
    Number.isFinite(value) &&
    value > 0 &&
    value <= MAX_TARGET_MB
  ) {
    return value;
  }
  return undefined;
}

/** Reads a finite number from a result item's details map, or null. */
export function videoDetailNumber(
  details: Record<string, unknown> | null | undefined,
  key: string,
): number | null {
  const value = details?.[key];
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}
