/**
 * Download page copy. Links are pasted, probed for their real formats, then
 * downloaded locally; every failure the page shows comes from the backend.
 */
export const webDownloadEn = {
  'webDownload.page.eyebrow': 'Download media',
  'webDownload.page.titleLead': 'Download',
  'webDownload.page.titleTail': 'media',
  'webDownload.page.sub':
    'Paste one link per line, choose a resolution the link really offers or an MP3 bitrate, and download it on this machine.',
  'webDownload.page.localBadge': 'Local only',

  'webDownload.intake.title': 'Links',
  'webDownload.intake.sub':
    'One URL per line. A playlist link expands into the links it contains.',
  'webDownload.intake.label': 'Links to download',
  'webDownload.intake.placeholder':
    'https://www.youtube.com/watch?v=...\nhttps://vimeo.com/...',
  'webDownload.intake.add': 'Add links',
  'webDownload.intake.clear': 'Clear',
  'webDownload.intake.actionsAria': 'Link queue actions',
  'webDownload.intake.emptyTitle': 'No links yet',
  'webDownload.intake.emptyHint':
    'Paste a link above to read the formats it offers.',
  'webDownload.intake.countOne': '1 link',
  'webDownload.intake.countMany': '{count} links',

  'webDownload.notice.emptyOne': '1 empty line ignored.',
  'webDownload.notice.emptyMany': '{count} empty lines ignored.',
  'webDownload.notice.duplicateOne': '1 duplicate link ignored.',
  'webDownload.notice.duplicateMany': '{count} duplicate links ignored.',
  'webDownload.notice.playlistExpanded': 'Playlist expanded into {count} links.',
  'webDownload.notice.failedOne':
    '{name} could not be read. Retry or remove it before running.',
  'webDownload.notice.failedMany':
    '{count} links could not be read: {names}. Retry or remove them before running.',
  'webDownload.notice.qualityConflict':
    'The links ask for different qualities. Use "Apply to all links" so every row requests the same format.',

  'webDownload.row.probing': 'Reading formats…',
  'webDownload.row.retry': 'Retry',
  'webDownload.row.remove': 'Remove link',
  'webDownload.row.removeAria': 'Remove {name}',
  'webDownload.row.playlistItem': 'Playlist item',
  'webDownload.row.durationAria': 'Duration {duration}',
  'webDownload.row.warningPlaylistEmpty':
    'This playlist reported no links, so it will be downloaded as one item.',

  'webDownload.kind.label': 'Format',
  'webDownload.kind.video': 'Video (MP4)',
  'webDownload.kind.audio': 'Audio (MP3)',
  'webDownload.kind.videoSummary': 'Video · MP4',
  'webDownload.kind.audioSummary': 'Audio · MP3',
  'webDownload.kind.help':
    'Video downloads the best stream up to the chosen height and merges it with the best audio. Audio converts the best track to MP3.',

  'webDownload.quality.label': 'Quality',
  'webDownload.quality.aria': 'Quality for {name}',
  'webDownload.quality.bestVideo': 'Best available',
  'webDownload.quality.bestAudio': 'Best (VBR)',
  'webDownload.quality.fps': '{fps} fps',
  'webDownload.quality.audioOption': '{bitrate} kbps',
  'webDownload.quality.applyAll': 'Apply to all links',
  'webDownload.quality.applyAllAria': 'Apply this quality to every link',
  'webDownload.quality.summaryVideo': 'Up to {height}p',
  'webDownload.quality.summaryAudio': 'MP3 at {bitrate} kbps',
  'webDownload.quality.summaryBest': 'Best available',

  'webDownload.output.title': 'Output',
  'webDownload.output.sub':
    'The real file names are planned before the download starts.',
  'webDownload.output.noDestination': 'Choose a folder for the downloads',
  'webDownload.output.destinationHelp':
    'Downloads need a destination folder. Choose one and the file names are planned.',

  'webDownload.legal.title': 'Your responsibility',
  'webDownload.legal.body':
    'Downloading is your responsibility. Only download content you have the rights to.',

  'webDownload.action.download': 'Download',
  'webDownload.action.downloadOne': 'Download 1 link',
  'webDownload.action.downloadMany': 'Download {count} links',
  'webDownload.action.downloading': 'Downloading…',
  'webDownload.action.cancel': 'Cancel',

  'webDownload.helper.addLinks': 'Add at least one link to download.',
  'webDownload.helper.probing':
    'Reading the link formats before the download can be planned.',
  'webDownload.helper.failed':
    'Retry or remove the links that could not be read.',
  'webDownload.helper.destination':
    'Choose a destination folder for the downloads.',
  'webDownload.helper.qualityConflict':
    'Use "Apply to all links" so every row asks for the same format.',
  'webDownload.helper.planning': 'Checking the planned file names.',

  'webDownload.progress.aria': 'Download progress',

  'webDownload.error.start': 'The download could not start.',
  'webDownload.error.plan': 'The download could not be planned.',
  'webDownload.error.inspect': 'The downloaded files could not be inspected.',
  'webDownload.error.status': 'The download status is unavailable.',
  'webDownload.error.cancel': 'The download could not be canceled.',
  'webDownload.error.openFile': 'The file could not be opened.',
  'webDownload.error.openFolder': 'The folder could not be opened.',
  'webDownload.error.probe': 'This link could not be read.',

  'webDownload.status.waiting': 'Waiting to download…',
} as const;
