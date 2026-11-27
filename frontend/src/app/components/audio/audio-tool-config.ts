import type { TranslationKey } from '../../i18n';
import type { I18nService } from '../../services/i18n.service';
import type { FileInfo } from '../../services/wails';
import { fileExtension, formatBytes, formatDuration } from '../../utils/format';

/**
 * Frontend mirror of `internal/audio/engine`.
 *
 * The backend manifests are the source of truth: the formats, the accepted
 * input extensions, the loudness presets and the defaults below are copied from
 * `go run ./cmd/fileforge-cli tools list --json` and must stay identical. When
 * the manifests change, this file changes with them.
 */
export const AUDIO_FORMATS = ['mp3', 'm4a', 'opus', 'wav', 'flac'] as const;
export type AudioFormat = (typeof AUDIO_FORMATS)[number];

/** Display labels are code-like names, identical in every locale. */
export const AUDIO_FORMAT_LABELS: Record<AudioFormat, string> = {
  mp3: 'MP3',
  m4a: 'M4A',
  opus: 'OPUS',
  wav: 'WAV',
  flac: 'FLAC',
};

/** WAV and FLAC ignore a requested bitrate; the plan emits a warning for them. */
export const LOSSLESS_AUDIO_FORMATS: ReadonlySet<AudioFormat> = new Set([
  'wav',
  'flac',
]);

/** Lossy bitrate choices in kbps; Auto means "use the format default". */
export const AUDIO_BITRATE_OPTIONS = [128, 192, 256, 320] as const;

export const DEFAULT_AUDIO_FORMAT: AudioFormat = 'mp3';

export function isAudioFormat(value: unknown): value is AudioFormat {
  return (
    typeof value === 'string' &&
    (AUDIO_FORMATS as readonly string[]).includes(value)
  );
}

export function isLosslessAudioFormat(
  format: AudioFormat | string,
): boolean {
  return LOSSLESS_AUDIO_FORMATS.has(format as AudioFormat);
}

/** `extractInputExtensions` from the extract manifest, audio and video. */
export const AUDIO_EXTRACT_INPUT_EXTENSIONS: ReadonlySet<string> = new Set([
  'mp3',
  'm4a',
  'aac',
  'opus',
  'ogg',
  'wav',
  'flac',
  'wma',
  'aiff',
  'mp4',
  'm4v',
  'mov',
  'mkv',
  'webm',
  'avi',
  'wmv',
  'flv',
  'ts',
  'mts',
  '3gp',
]);

/** `inputExtensions` from the convert and normalize manifests. */
export const AUDIO_INPUT_EXTENSIONS: ReadonlySet<string> = new Set([
  'mp3',
  'm4a',
  'aac',
  'opus',
  'ogg',
  'wav',
  'flac',
  'wma',
  'aiff',
]);

export const AUDIO_INPUT_ACCEPT =
  '.mp3,.m4a,.aac,.opus,.ogg,.wav,.flac,.wma,.aiff,audio/*';

export const AUDIO_EXTRACT_INPUT_ACCEPT =
  `${AUDIO_INPUT_ACCEPT},.mp4,.m4v,.mov,.mkv,.webm,.avi,.wmv,.flv,.ts,.mts,.3gp,video/*`;

/** Loudness presets from `internal/audio/engine/loudnorm.go`. */
export const LOUDNORM_TARGETS = ['streaming', 'broadcast', 'podcast'] as const;
export type LoudnormTarget = (typeof LOUDNORM_TARGETS)[number];

export const DEFAULT_LOUDNORM_TARGET: LoudnormTarget = 'streaming';
export const DEFAULT_TRUE_PEAK = -1.5;
export const TRUE_PEAK_MIN = -9;
export const TRUE_PEAK_MAX = 0;

export function isLoudnormTarget(value: unknown): value is LoudnormTarget {
  return (
    typeof value === 'string' &&
    (LOUDNORM_TARGETS as readonly string[]).includes(value)
  );
}

/** The option shape extract and convert share. */
export interface AudioConversionOptions {
  format: AudioFormat;
  /** null means "Auto": the key is omitted and the encoder default applies. */
  bitrateKbps: number | null;
}

export function defaultAudioConversionOptions(): AudioConversionOptions {
  return { format: DEFAULT_AUDIO_FORMAT, bitrateKbps: null };
}

export function normalizeAudioConversionOptions(
  stored: Record<string, unknown>,
): Partial<AudioConversionOptions> {
  const result: Partial<AudioConversionOptions> = {};

  const format = stored['format'];
  if (isAudioFormat(format)) {
    result.format = format;
  }

  const bitrate = stored['bitrateKbps'];
  if (
    bitrate === null ||
    (typeof bitrate === 'number' &&
      (AUDIO_BITRATE_OPTIONS as readonly number[]).includes(bitrate))
  ) {
    result.bitrateKbps = bitrate as number | null;
  }

  const effectiveFormat = result.format ?? DEFAULT_AUDIO_FORMAT;
  if (isLosslessAudioFormat(effectiveFormat)) {
    result.bitrateKbps = null;
  }

  return result;
}

/** The option shape normalize uses. */
export interface AudioNormalizeOptions {
  targetLufs: LoudnormTarget;
  truePeak: number;
}

export function defaultAudioNormalizeOptions(): AudioNormalizeOptions {
  return { targetLufs: DEFAULT_LOUDNORM_TARGET, truePeak: DEFAULT_TRUE_PEAK };
}

export function normalizeAudioNormalizeOptions(
  stored: Record<string, unknown>,
): Partial<AudioNormalizeOptions> {
  const result: Partial<AudioNormalizeOptions> = {};

  const target = stored['targetLufs'];
  if (isLoudnormTarget(target)) {
    result.targetLufs = target;
  }

  if (typeof stored['truePeak'] === 'number' && isTruePeak(stored['truePeak'])) {
    result.truePeak = stored['truePeak'];
  }

  return result;
}

/** True peak must stay inside the backend's -9..0 dBTP window. */
export function isTruePeak(value: number): boolean {
  return (
    Number.isFinite(value) && value >= TRUE_PEAK_MIN && value <= TRUE_PEAK_MAX
  );
}

/** Parses a typed true peak, returning null for empty or out-of-range values. */
export function parseTruePeak(raw: string): number | null {
  const text = raw.trim();
  if (!text) {
    return null;
  }

  const value = Number(text);
  return isTruePeak(value) ? value : null;
}

/** One decimal is the precision the loudnorm filter reports. */
export function formatLoudness(value: number): string {
  if (!Number.isFinite(value)) {
    return '';
  }
  const rounded = Math.abs(value) < 0.05 ? 0 : value;
  return rounded.toFixed(1);
}

/** Reads a finite number from the item's details map, or null. */
export function detailNumber(
  details: Record<string, unknown> | null | undefined,
  key: string,
): number | null {
  const value = details?.[key];
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

/** Queue metadata shared by the three audio pages: size and duration. */
export function describeAudioFileInfo(info: FileInfo): string {
  const parts: string[] = [];
  if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
    parts.push(formatBytes(info.sizeBytes));
  }
  if (typeof info.durationSeconds === 'number' && info.durationSeconds > 0) {
    parts.push(formatDuration(info.durationSeconds));
  }
  return parts.join(' · ');
}

/** Intake notice for files this page cannot accept, shared by the trio. */
export function audioRejectionNotice(
  i18n: I18nService,
  rejected: readonly string[],
): string {
  const extensions = [
    ...new Set(
      rejected.map((path) => fileExtension(path).toUpperCase() || 'UNKNOWN'),
    ),
  ];

  const count = rejected.length;
  const filesCopy =
    count === 1
      ? i18n.t('audioTool.notice.filesOne')
      : i18n.t('audioTool.notice.filesMany', { count });

  let extensionsCopy: string;
  if (extensions.length === 1) {
    extensionsCopy = i18n.t('audioTool.notice.extensionOne', {
      extension: extensions[0],
    });
  } else {
    const head = extensions.slice(0, -1).join(', ');
    extensionsCopy = i18n.t('audioTool.notice.extensionsMany', {
      head,
      tail: extensions[extensions.length - 1],
    });
  }

  return `${filesCopy} ${extensionsCopy}`;
}

/** Keys shared by every audio page, resolved through the copy blocks below. */
export interface AudioToolPageCopy {
  eyebrow: TranslationKey;
  titleLead: TranslationKey;
  titleTail: TranslationKey;
  sub: TranslationKey;
  intakeTitle: TranslationKey;
  intakeSub: TranslationKey;
  dropTitle: TranslationKey;
  dropHint: TranslationKey;
  dropAria: TranslationKey;
  outputSub: TranslationKey;
  actionIdle: TranslationKey;
  actionOne: TranslationKey;
  actionMany: TranslationKey;
  actionBusy: TranslationKey;
  helperAddOne: TranslationKey;
  errorStart: TranslationKey;
  errorPlan: TranslationKey;
  errorCancel: TranslationKey;
  errorOpenFile: TranslationKey;
  errorOpenFolder: TranslationKey;
  errorStatus: TranslationKey;
  errorInspect: TranslationKey;
}

/** Extract and convert add the format and bitrate fields. */
export interface AudioConversionPageCopy extends AudioToolPageCopy {
  formatLabel: TranslationKey;
  formatHelp: TranslationKey;
}
