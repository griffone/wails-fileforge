export interface ImagePreset {
  id: string;
  label: string;
  hint: string;
  format: 'webp' | 'jpeg' | 'png' | 'gif';
  quality: number;
}

export const IMAGE_PRESETS: readonly ImagePreset[] = [
  { id: 'webp', label: 'WebP', hint: 'Smaller files', format: 'webp', quality: 80 },
  { id: 'jpeg', label: 'JPEG', hint: 'Best for photos', format: 'jpeg', quality: 85 },
  { id: 'png', label: 'PNG', hint: 'Lossless quality', format: 'png', quality: 100 },
  { id: 'gif', label: 'GIF', hint: 'Animations', format: 'gif', quality: 90 },
];
