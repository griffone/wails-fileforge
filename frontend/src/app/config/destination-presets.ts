/**
 * Destination presets shared by every tool that compresses to a byte target.
 *
 * Target sizes use the app's binary convention: 1 MB = 1,048,576 bytes. This
 * matches `formatBytes`, so the field, the chip and the plan all speak the
 * same unit. The exact byte values live in this file so they can never drift:
 *
 *   Discord  9.5 MB =  9,961,472 bytes
 *   WhatsApp  16 MB = 16,777,216 bytes
 *   Email     25 MB = 26,214,400 bytes
 */
export const BYTES_PER_MB = 1048576;

/** Upper bound that still produces a usable int64 target. */
export const MAX_TARGET_MB = 1_000_000;
export const MAX_TARGET_MB_LABEL = '1,000,000';

export interface DestinationPreset {
  id: string;
  /** Proper service name; a translation key may override it at render time. */
  label: string;
  /** Exact size the destination accepts, in binary bytes. */
  targetBytes: number;
}

export const DESTINATION_PRESETS: readonly DestinationPreset[] = [
  { id: 'discord', label: 'Discord', targetBytes: 9.5 * BYTES_PER_MB },
  { id: 'whatsapp', label: 'WhatsApp', targetBytes: 16 * BYTES_PER_MB },
  { id: 'email', label: 'Email', targetBytes: 25 * BYTES_PER_MB },
];

/** Converts an MB field value into the whole-byte target engines expect. */
export function mbToTargetBytes(mb: number | null): number {
  if (mb === null || !Number.isFinite(mb) || mb <= 0 || mb > MAX_TARGET_MB) {
    return 0;
  }

  const bytes = mb * BYTES_PER_MB;
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return 0;
  }

  return Math.max(1, Math.round(bytes));
}

/** The preset expressed in the field unit; exact for every preset. */
export function presetTargetMb(preset: DestinationPreset): number {
  return preset.targetBytes / BYTES_PER_MB;
}

/**
 * Target-facing label that keeps the exact field value. It finds the shortest
 * decimal that converts back to the same byte count, so a typed 9.5 shows as
 * `9.5 MB` and never rounds to a different target than the one sent.
 */
export function targetSizeLabel(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 MB';
  }

  const mb = bytes / BYTES_PER_MB;
  for (let digits = 1; digits <= 6; digits += 1) {
    const candidate = Number(mb.toFixed(digits));
    if (mbToTargetBytes(candidate) === bytes) {
      return `${candidate} MB`;
    }
  }

  return `${mb} MB`;
}

/** The preset matching a field value, or null when the value is custom. */
export function presetForTargetMb(
  mb: number | null,
): DestinationPreset | null {
  if (mb === null || !Number.isFinite(mb)) {
    return null;
  }

  return (
    DESTINATION_PRESETS.find(
      (preset) => Math.abs(presetTargetMb(preset) - mb) < 1e-9,
    ) ?? null
  );
}
