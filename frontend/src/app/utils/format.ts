import type { Locale, TranslationKey, TranslationParams } from '../i18n';

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;

/** Human-readable binary size, e.g. `1.2 MB`, `840 KB`, `0 B`. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B';
  }

  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < BYTE_UNITS.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  const rounded = value >= 100 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${BYTE_UNITS[unitIndex]}`;
}

function trimTrailingSeparators(path: string): string {
  return path.replace(/[\\/]+$/, '');
}

function lastSeparatorIndex(path: string): number {
  return Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
}

/** File name including extension, supporting both `/` and `\` separators. */
export function fileName(path: string): string {
  const trimmed = trimTrailingSeparators(path ?? '');
  const index = lastSeparatorIndex(trimmed);
  return index >= 0 ? trimmed.slice(index + 1) : trimmed;
}

/** Parent directory without a trailing separator. */
export function fileDir(path: string): string {
  const trimmed = trimTrailingSeparators(path ?? '');
  const index = lastSeparatorIndex(trimmed);
  if (index < 0) {
    return '';
  }
  if (index === 0) {
    return '/';
  }
  const directory = trimmed.slice(0, index);
  // A file directly in a Windows drive root keeps the separator: `C:\`.
  return /^[a-zA-Z]:$/.test(directory) ? `${directory}\\` : directory;
}

/** File name without its final extension. */
export function fileStem(path: string): string {
  const name = fileName(path);
  const index = name.lastIndexOf('.');
  return index > 0 ? name.slice(0, index) : name;
}

/** Extension without the leading dot; empty when the name has none. */
export function fileExtension(path: string): string {
  const name = fileName(path);
  const index = name.lastIndexOf('.');
  return index > 0 ? name.slice(index + 1) : '';
}

/** `1 image`, `3 images`, or a caller-provided plural form. */
export function formatCount(
  count: number,
  singular: string,
  plural?: string,
): string {
  const label = count === 1 ? singular : (plural ?? `${singular}s`);
  return `${count} ${label}`;
}

function padTwo(value: number): string {
  return String(value).padStart(2, '0');
}

/**
 * Clock-style duration: `0:07`, `1:23`, `1:02:03`. Invalid or non-positive
 * values return an em dash so unknown durations never look like `0:00`.
 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '—';
  }

  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;

  return hours > 0
    ? `${hours}:${padTwo(minutes)}:${padTwo(rest)}`
    : `${minutes}:${padTwo(rest)}`;
}

/** Translator signature accepted by the shared relative-time helper. */
export type TranslateFn = (
  key: TranslationKey,
  params?: TranslationParams,
) => string;

/**
 * Relative timestamp shared by every history surface: `Just now`, `5 min ago`,
 * `2 h ago`, `Yesterday`, then a short localized date. Invalid or non-positive
 * timestamps return an empty string.
 */
export function formatRelativeTime(
  timestamp: number,
  locale: Locale,
  t: TranslateFn,
): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) {
    return '';
  }

  const now = Date.now();
  const diff = Math.max(0, now - timestamp);

  if (diff < 60_000) {
    return t('home.time.justNow');
  }

  if (diff < 3_600_000) {
    return t('home.time.minutesAgo', {
      count: Math.max(1, Math.floor(diff / 60_000)),
    });
  }

  if (diff < 86_400_000) {
    return t('home.time.hoursAgo', {
      count: Math.max(1, Math.floor(diff / 3_600_000)),
    });
  }

  const date = new Date(timestamp);
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);

  return date.toDateString() === yesterday.toDateString()
    ? t('home.time.yesterday')
    : date.toLocaleDateString(locale, {
        month: 'short',
        day: 'numeric',
      });
}

const SECONDS_PART = /^\d+(?:\.\d+)?$/;

/**
 * Parses a user-typed time into seconds. Accepts `SS`, `MM:SS`, `HH:MM:SS`
 * and decimal seconds. Returns null for empty, negative or malformed values.
 */
export function parseTimeToSeconds(value: string): number | null {
  const trimmed = (value ?? '').trim();
  if (!trimmed) {
    return null;
  }

  const parts = trimmed.split(':');
  if (parts.length < 1 || parts.length > 3) {
    return null;
  }
  if (!parts.every((part) => SECONDS_PART.test(part))) {
    return null;
  }

  const numbers = parts.map((part) => Number(part));
  if (numbers.some((number) => !Number.isFinite(number) || number < 0)) {
    return null;
  }

  if (parts.length === 1) {
    return numbers[0];
  }

  if (parts.length === 2) {
    const [minutes, seconds] = numbers;
    if (seconds >= 60) {
      return null;
    }
    return minutes * 60 + seconds;
  }

  const [hours, minutes, seconds] = numbers;
  if (minutes >= 60 || seconds >= 60) {
    return null;
  }
  return hours * 3600 + minutes * 60 + seconds;
}
