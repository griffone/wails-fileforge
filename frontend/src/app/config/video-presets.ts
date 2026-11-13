export interface VideoPreset {
  id: string;
  label: string;
  hint: string;
  targetFormat: 'mp4' | 'webm';
  qualityPreset: 'high' | 'medium' | 'low';
}

export const VIDEO_PRESETS: readonly VideoPreset[] = [
  {
    id: 'mp4-balanced',
    label: 'MP4',
    hint: 'Plays everywhere',
    targetFormat: 'mp4',
    qualityPreset: 'medium',
  },
  {
    id: 'mp4-high',
    label: 'MP4 HD',
    hint: 'Best quality',
    targetFormat: 'mp4',
    qualityPreset: 'high',
  },
  {
    id: 'mp4-small',
    label: 'MP4 Small',
    hint: 'Smaller file',
    targetFormat: 'mp4',
    qualityPreset: 'low',
  },
  {
    id: 'webm-balanced',
    label: 'WebM',
    hint: 'For the web',
    targetFormat: 'webm',
    qualityPreset: 'medium',
  },
];
