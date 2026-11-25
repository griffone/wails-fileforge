export const videoTranscribeEs = {
  'videoTranscribe.page.eyebrow': 'VIDEO',
  'videoTranscribe.page.titleLead': 'Transcribir',
  'videoTranscribe.page.titleTail': 'video',
  'videoTranscribe.page.sub':
    'Transcribe vídeo y audio a subtítulos o texto plano con Whisper local. Todo se ejecuta en este equipo.',
  'videoTranscribe.page.localBadge': 'Solo local',
  'videoTranscribe.list.and': 'y',
  'videoTranscribe.capability.title':
    'La transcripción local no está disponible.',
  'videoTranscribe.capability.detail':
    'Binario de Whisper: {whisper} · Modelo: {model}. {guidance}',
  'videoTranscribe.capability.installMissing':
    'Instala {missing} y elige las rutas en Ajustes.',
  'videoTranscribe.capability.installFallback':
    'Instala whisper.cpp y elige un archivo de modelo ggml en Ajustes.',
  'videoTranscribe.capability.missingWhisper':
    'el binario de whisper.cpp',
  'videoTranscribe.capability.missingModel': 'un modelo de Whisper',
  'videoTranscribe.capability.notFound': 'no encontrado',
  'videoTranscribe.capability.choosePaths': 'Elegir rutas en Ajustes',
  'videoTranscribe.model.ready': 'Whisper listo',
  'videoTranscribe.intake.title': 'Vídeo o audio',
  'videoTranscribe.intake.sub':
    'Suelta archivos o elígelos desde tu equipo.',
  'videoTranscribe.intake.dropTitle': 'Suelta vídeo o audio aquí',
  'videoTranscribe.intake.dropHint':
    'Vídeo o audio · Whisper se ejecuta localmente',
  'videoTranscribe.intake.dropAria':
    'Suelta o elige archivos de vídeo y audio',
  'videoTranscribe.intake.actionsAria': 'Acciones de selección de archivos',
  'videoTranscribe.intake.choose': 'Elegir archivos',
  'videoTranscribe.intake.clear': 'Limpiar',
  'videoTranscribe.settings.title': 'Ajustes',
  'videoTranscribe.settings.sub':
    'Elige el idioma hablado, las salidas y dónde se guardan.',
  'videoTranscribe.settings.language': 'Idioma',
  'videoTranscribe.settings.languageHelp':
    'Automático detecta el idioma hablado. El modelo admite inglés y español.',
  'videoTranscribe.settings.outputFolder': 'Carpeta de salida',
  'videoTranscribe.settings.chooseFolder': 'Elegir carpeta',
  'videoTranscribe.settings.outputHelp':
    'Las salidas se guardan junto a cada archivo de origen.',
  'videoTranscribe.settings.useDefault': 'Usar la carpeta predeterminada',
  'videoTranscribe.settings.formats': 'Formatos',
  'videoTranscribe.settings.formatsRequired':
    'Elige al menos un formato de salida.',
  'videoTranscribe.settings.formatsHelp':
    'Se escriben subtítulos y texto plano para cada archivo.',
  'videoTranscribe.settings.localNote':
    'Whisper se ejecuta localmente. Los archivos largos tardan más en la CPU.',
  'videoTranscribe.output.destinationEach': 'Junto a cada origen',
  'videoTranscribe.language.auto': 'Automático',
  'videoTranscribe.language.en': 'Inglés',
  'videoTranscribe.language.es': 'Español',
  'videoTranscribe.notice.rejectedOne':
    '{name} no se agregó porque no es un vídeo ni un archivo de audio.',
  'videoTranscribe.notice.rejectedMany':
    '{names} no se agregaron porque no son vídeos ni archivos de audio.',
  'videoTranscribe.notice.duplicateOne':
    '{name} ya está en la cola.',
  'videoTranscribe.notice.duplicateMany':
    'Ya hay {count} archivos en la cola.',
  'videoTranscribe.action.transcribeFiles': 'Transcribir archivos',
  'videoTranscribe.action.transcribeOne': 'Transcribir 1 archivo',
  'videoTranscribe.action.transcribeMany': 'Transcribir {count} archivos',
  'videoTranscribe.action.transcribing': 'Transcribiendo…',
  'videoTranscribe.action.cancel': 'Cancelar',
  'videoTranscribe.helper.addOne':
    'Añade al menos un vídeo o archivo de audio.',
  'videoTranscribe.helper.unavailable':
    'La transcripción local no está disponible en este equipo.',
  'videoTranscribe.helper.chooseFormat':
    'Elige al menos un formato de salida.',
  'videoTranscribe.progress.aria': 'Progreso de la transcripción',
  'videoTranscribe.status.waiting': 'Esperando para empezar',
  'videoTranscribe.error.start':
    'No se pudo iniciar el trabajo de transcripción.',
  'videoTranscribe.error.plan': 'No se pudo planificar la salida.',
  'videoTranscribe.error.cancel': 'No se pudo cancelar el trabajo.',
  'videoTranscribe.error.openFile': 'No se pudo abrir el archivo.',
  'videoTranscribe.error.openFolder': 'No se pudo abrir la carpeta.',
  'videoTranscribe.error.status': 'No se pudo leer el estado del trabajo.',
  'videoTranscribe.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
