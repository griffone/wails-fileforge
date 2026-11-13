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
