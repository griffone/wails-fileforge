export const videoTranscribeEn = {
  'videoTranscribe.page.eyebrow': 'VIDEO',
  'videoTranscribe.page.titleLead': 'Video',
  'videoTranscribe.page.titleTail': 'Transcribe',
  'videoTranscribe.page.sub':
    'Transcribe video and audio into subtitles or plain text with local Whisper. Everything runs on this machine.',
  'videoTranscribe.page.localBadge': 'Local only',
  'videoTranscribe.list.and': 'and',
  'videoTranscribe.capability.title':
    'Local transcription is unavailable.',
  'videoTranscribe.capability.detail':
    'Whisper binary: {whisper} · Model: {model}. {guidance}',
  'videoTranscribe.capability.installMissing':
    'Install {missing}, then choose the paths in Settings.',
  'videoTranscribe.capability.installFallback':
    'Install whisper.cpp and choose a ggml model file in Settings.',
  'videoTranscribe.capability.missingWhisper':
    'the whisper.cpp binary',
  'videoTranscribe.capability.missingModel': 'a Whisper model',
  'videoTranscribe.capability.notFound': 'not found',
  'videoTranscribe.capability.choosePaths': 'Choose paths in Settings',
  'videoTranscribe.model.ready': 'Whisper ready',
  'videoTranscribe.intake.title': 'Video or audio',
  'videoTranscribe.intake.sub':
    'Drop files or choose them from your computer.',
  'videoTranscribe.intake.dropTitle': 'Drop video or audio here',
  'videoTranscribe.intake.dropHint':
    'Video or audio · Whisper runs locally',
  'videoTranscribe.intake.dropAria':
    'Drop or choose video and audio files',
  'videoTranscribe.intake.actionsAria': 'File selection actions',
  'videoTranscribe.intake.choose': 'Choose files',
  'videoTranscribe.intake.clear': 'Clear',
  'videoTranscribe.settings.title': 'Settings',
  'videoTranscribe.settings.sub':
    'Pick the spoken language, the outputs and where they go.',
  'videoTranscribe.settings.language': 'Language',
  'videoTranscribe.settings.languageHelp':
    'Auto detects the spoken language. The model supports English and Spanish.',
  'videoTranscribe.settings.outputFolder': 'Output folder',
  'videoTranscribe.settings.chooseFolder': 'Choose folder',
  'videoTranscribe.settings.outputHelp':
    'Outputs are saved next to each source file.',
  'videoTranscribe.settings.useDefault': 'Use the default folder',
  'videoTranscribe.settings.formats': 'Formats',
  'videoTranscribe.settings.formatsRequired':
    'Choose at least one output format.',
  'videoTranscribe.settings.formatsHelp':
    'Subtitles and plain text are written for every file.',
  'videoTranscribe.settings.localNote':
    'Whisper runs locally. Long files take longer on CPU.',
  'videoTranscribe.output.destinationEach': 'Next to each source',
  'videoTranscribe.language.auto': 'Auto',
  'videoTranscribe.language.en': 'English',
  'videoTranscribe.language.es': 'Spanish',
  'videoTranscribe.notice.rejectedOne':
    "{name} wasn't added because it isn't a video or audio file.",
  'videoTranscribe.notice.rejectedMany':
    "{names} weren't added because they aren't video or audio files.",
  'videoTranscribe.notice.duplicateOne':
    '{name} is already in the queue.',
  'videoTranscribe.notice.duplicateMany':
    '{count} files are already in the queue.',
  'videoTranscribe.action.transcribeFiles': 'Transcribe files',
  'videoTranscribe.action.transcribeOne': 'Transcribe 1 file',
  'videoTranscribe.action.transcribeMany': 'Transcribe {count} files',
  'videoTranscribe.action.transcribing': 'Transcribing…',
  'videoTranscribe.action.cancel': 'Cancel',
  'videoTranscribe.helper.addOne':
    'Add at least one video or audio file.',
  'videoTranscribe.helper.unavailable':
    'Local transcription is unavailable on this machine.',
  'videoTranscribe.helper.chooseFormat':
    'Choose at least one output format.',
  'videoTranscribe.progress.aria': 'Transcription progress',
  'videoTranscribe.status.waiting': 'Waiting to start',
  'videoTranscribe.error.start':
    'The transcription job could not start.',
  'videoTranscribe.error.plan': 'The output could not be planned.',
  'videoTranscribe.error.cancel': 'The job could not be cancelled.',
  'videoTranscribe.error.openFile': 'The file could not be opened.',
  'videoTranscribe.error.openFolder': 'The folder could not be opened.',
  'videoTranscribe.error.status': 'The job status could not be read.',
  'videoTranscribe.error.inspect': 'File sizes could not be read.',
} as const;
