/**
 * Copy for the three audio tools.
 *
 * Extract and Convert share the intake, notice and action scaffolding through
 * `audioTool.*`; only the page heading, drop hint and action verb differ.
 * Normalize adds the loudness target and the measured-loudness panel.
 */
export const audioToolsEn = {
  'audioTool.page.localBadge': 'Local only',
  'audioTool.intake.choose': 'Choose files',
  'audioTool.intake.clear': 'Clear',
  'audioTool.intake.actionsAria': 'File selection actions',
  'audioTool.notice.filesOne': "1 file wasn't added.",
  'audioTool.notice.filesMany': "{count} files weren't added.",
  'audioTool.notice.extensionOne': "{extension} isn't supported.",
  'audioTool.notice.extensionsMany':
    "{head} and {tail} aren't supported.",
  'audioTool.output.title': 'Output',
  'audioTool.output.summary': '{format} · {detail}',
  'audioTool.output.destinationEach': 'Next to each original',
  'audioTool.output.useOriginalFolders': 'Use original folders',
  'audioTool.bitrate.label': 'Bitrate',
  'audioTool.bitrate.auto': 'Auto',
  'audioTool.bitrate.help':
    'Auto uses the format default: 192 kbps, or 128 kbps for Opus.',
  'audioTool.bitrate.option': '{value} kbps',
  'audioTool.bitrate.losslessWarning':
    'Bitrate is ignored for {format} output because the format is lossless.',
  'audioTool.losslessShort': 'Lossless',
  'audioTool.action.cancel': 'Cancel',
  'audioTool.progress.aria': 'Audio job progress',
  'audioTool.status.waiting': 'Waiting to start',
  'audioTool.loudness.title': 'Measured loudness',
  'audioTool.loudness.sub':
    'Input and output loudness measured by the loudness filter.',
  'audioTool.loudness.row': 'Input {input} LUFS → Output {output} LUFS',
  'audioTool.truePeak.value': '{value} dBTP',

  'audioExtract.page.eyebrow': 'AUDIO',
  'audioExtract.page.titleLead': 'Extract',
  'audioExtract.page.titleTail': 'audio',
  'audioExtract.page.sub':
    'Pull the audio track out of a video or audio file. Everything runs on this machine.',
  'audioExtract.intake.title': 'Videos and audio',
  'audioExtract.intake.sub':
    'Drop videos or audio files, or choose them from your computer.',
  'audioExtract.intake.dropTitle': 'Drop videos or audio here',
  'audioExtract.intake.dropHint':
    'MP4 · MOV · MKV · WebM · MP3 · M4A · WAV · FLAC',
  'audioExtract.intake.dropAria': 'Drop or choose files to extract audio from',
  'audioExtract.output.sub':
    'Choose the audio format and check where the files will go.',
  'audioExtract.format.label': 'Audio format',
  'audioExtract.format.help':
    'MP3, M4A and Opus are lossy; WAV and FLAC are lossless.',
  'audioExtract.action.idle': 'Extract audio',
  'audioExtract.action.one': 'Extract audio from 1 file',
  'audioExtract.action.many': 'Extract audio from {count} files',
  'audioExtract.action.busy': 'Extracting…',
  'audioExtract.helper.addOne': 'Add at least one video or audio file.',
  'audioExtract.error.start': 'The audio extraction could not start.',
  'audioExtract.error.plan': 'The output could not be planned.',
  'audioExtract.error.cancel': 'The job could not be cancelled.',
  'audioExtract.error.openFile': 'The file could not be opened.',
  'audioExtract.error.openFolder': 'The folder could not be opened.',
  'audioExtract.error.status': 'The job status could not be read.',
  'audioExtract.error.inspect': 'File sizes could not be read.',

  'audioConvert.page.eyebrow': 'AUDIO',
  'audioConvert.page.titleLead': 'Convert',
  'audioConvert.page.titleTail': 'audio',
  'audioConvert.page.sub':
    'Convert audio between MP3, M4A, Opus, WAV and FLAC. Everything runs on this machine.',
  'audioConvert.intake.title': 'Audio files',
  'audioConvert.intake.sub':
    'Drop audio files or choose them from your computer.',
  'audioConvert.intake.dropTitle': 'Drop audio here',
  'audioConvert.intake.dropHint':
    'MP3 · M4A · AAC · OPUS · OGG · WAV · FLAC · WMA · AIFF',
  'audioConvert.intake.dropAria': 'Drop or choose audio files to convert',
  'audioConvert.output.sub':
    'Choose the audio format and check where the files will go.',
  'audioConvert.format.label': 'Audio format',
  'audioConvert.format.help':
    'MP3, M4A and Opus are lossy; WAV and FLAC are lossless.',
  'audioConvert.action.idle': 'Convert audio',
  'audioConvert.action.one': 'Convert 1 audio file',
  'audioConvert.action.many': 'Convert {count} audio files',
  'audioConvert.action.busy': 'Converting…',
  'audioConvert.helper.addOne': 'Add at least one audio file.',
  'audioConvert.error.start': 'The audio conversion could not start.',
  'audioConvert.error.plan': 'The output could not be planned.',
  'audioConvert.error.cancel': 'The job could not be cancelled.',
  'audioConvert.error.openFile': 'The file could not be opened.',
  'audioConvert.error.openFolder': 'The folder could not be opened.',
  'audioConvert.error.status': 'The job status could not be read.',
  'audioConvert.error.inspect': 'File sizes could not be read.',

  'audioNormalize.page.eyebrow': 'AUDIO',
  'audioNormalize.page.titleLead': 'Normalize',
  'audioNormalize.page.titleTail': 'audio',
  'audioNormalize.page.sub':
    'Bring audio to a streaming, broadcast or podcast loudness target. Everything runs on this machine.',
  'audioNormalize.intake.title': 'Audio files',
  'audioNormalize.intake.sub':
    'Drop audio files or choose them from your computer.',
  'audioNormalize.intake.dropTitle': 'Drop audio here',
  'audioNormalize.intake.dropHint':
    'MP3 · M4A · AAC · OPUS · OGG · WAV · FLAC · WMA · AIFF',
  'audioNormalize.intake.dropAria': 'Drop or choose audio files to normalize',
  'audioNormalize.output.sub':
    'Pick a loudness target and check where the files will go.',
  'audioNormalize.target.label': 'Loudness target',
  'audioNormalize.target.streaming': 'Streaming (-14 LUFS)',
  'audioNormalize.target.broadcast': 'Broadcast (-23 LUFS)',
  'audioNormalize.target.podcast': 'Podcast (-16 LUFS)',
  'audioNormalize.target.help':
    'Two-pass EBU R128 normalization. The input container is preserved.',
  'audioNormalize.truePeak.label': 'True peak ceiling',
  'audioNormalize.truePeak.help':
    'Between -9 and 0 dBTP. The default is -1.5 dBTP.',
  'audioNormalize.truePeak.error':
    'Enter a true peak between {min} and {max} dBTP.',
  'audioNormalize.container.help':
    'The input container is kept: OGG saves as Opus, AAC as M4A and anything else as MP3.',
  'audioNormalize.action.idle': 'Normalize audio',
  'audioNormalize.action.one': 'Normalize 1 audio file',
  'audioNormalize.action.many': 'Normalize {count} audio files',
  'audioNormalize.action.busy': 'Normalizing…',
  'audioNormalize.helper.addOne': 'Add at least one audio file.',
  'audioNormalize.error.start': 'The loudness normalization could not start.',
  'audioNormalize.error.plan': 'The output could not be planned.',
  'audioNormalize.error.cancel': 'The job could not be cancelled.',
  'audioNormalize.error.openFile': 'The file could not be opened.',
  'audioNormalize.error.openFolder': 'The folder could not be opened.',
  'audioNormalize.error.status': 'The job status could not be read.',
  'audioNormalize.error.inspect': 'File sizes could not be read.',
} as const;
