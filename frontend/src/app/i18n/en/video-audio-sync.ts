/**
 * Audio-in-video sync page copy. The shell ships before the timeline editor so
 * the route, registry and navigation stay consistent.
 */
export const videoAudioSyncEn = {
  'videoAudioSync.page.eyebrow': 'Video audio',
  'videoAudioSync.page.title': 'Audio in Video',
  'videoAudioSync.page.sub':
    'Replace a camera track with a separately recorded source. The video stream is copied; only the audio is rebuilt.',
  'videoAudioSync.page.localBadge': 'Local only',

  'videoAudioSync.placeholder.title': 'Timeline coming next',
  'videoAudioSync.placeholder.body':
    'The sync editor will show both lanes, let you drag or nudge the source offset and preview the result before exporting.',
} as const;
