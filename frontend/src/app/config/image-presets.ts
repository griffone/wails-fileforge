export type ImageOutputFormat =
  | 'auto'
  | 'webp'
  | 'jpeg'
  | 'png'
  | 'gif'
  | 'tiff'
  | 'avif';

export interface ImagePreset {
  id: string;
  label: string;
  hint: string;
  format: ImageOutputFormat;
  quality: number;
}

export interface ImageFormatOption {
  value: ImageOutputFormat;
  label: string;
}

/**
 * Every format the backend can encode, plus `auto` to keep each input's own
 * format. `auto` falls back to PNG for sources without an encoder (for
 * example BMP).
 */
export const IMAGE_FORMAT_OPTIONS: readonly ImageFormatOption[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'webp', label: 'WebP' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'png', label: 'PNG' },
  { value: 'gif', label: 'GIF' },
  { value: 'tiff', label: 'TIFF' },
  { value: 'avif', label: 'AVIF' },
];

export const IMAGE_PRESETS: readonly ImagePreset[] = [
  { id: 'original', label: 'Original', hint: 'Keep each format', format: 'auto', quality: 85 },
  { id: 'webp', label: 'WebP', hint: 'Smaller files', format: 'webp', quality: 80 },
  { id: 'jpeg', label: 'JPEG', hint: 'Best for photos', format: 'jpeg', quality: 85 },
  { id: 'png', label: 'PNG', hint: 'Lossless quality', format: 'png', quality: 100 },
  { id: 'gif', label: 'GIF', hint: 'Animations', format: 'gif', quality: 90 },
];
