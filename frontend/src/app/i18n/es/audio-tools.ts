/**
 * Spanish copy for the three audio tools. Keys mirror `en/audio-tools.ts`.
 * Technical units (LUFS, dBTP, kbps) and format names stay as-is.
 */
export const audioToolsEs = {
  'audioTool.page.localBadge': 'Solo local',
  'audioTool.intake.choose': 'Elegir archivos',
  'audioTool.intake.clear': 'Limpiar',
  'audioTool.intake.actionsAria': 'Acciones de selección de archivos',
  'audioTool.notice.filesOne': 'No se agregó 1 archivo.',
  'audioTool.notice.filesMany': 'No se agregaron {count} archivos.',
  'audioTool.notice.extensionOne': '{extension} no es compatible.',
  'audioTool.notice.extensionsMany':
    '{head} y {tail} no son compatibles.',
  'audioTool.output.title': 'Salida',
  'audioTool.output.summary': '{format} · {detail}',
  'audioTool.output.destinationEach': 'Junto a cada original',
  'audioTool.output.useOriginalFolders': 'Usar las carpetas originales',
  'audioTool.bitrate.label': 'Tasa de bits',
  'audioTool.bitrate.auto': 'Automática',
  'audioTool.bitrate.help':
    'Automática usa el valor predeterminado del formato: 192 kbps, o 128 kbps para Opus.',
  'audioTool.bitrate.option': '{value} kbps',
  'audioTool.bitrate.losslessWarning':
    'La tasa de bits se ignora para la salida {format} porque el formato no tiene pérdida.',
  'audioTool.losslessShort': 'Sin pérdida',
  'audioTool.action.cancel': 'Cancelar',
  'audioTool.progress.aria': 'Progreso del trabajo de audio',
  'audioTool.status.waiting': 'Esperando para empezar',
  'audioTool.loudness.title': 'Sonoridad medida',
  'audioTool.loudness.sub':
    'Sonoridad de entrada y salida medida por el filtro de sonoridad.',
  'audioTool.loudness.row': 'Entrada {input} LUFS → Salida {output} LUFS',
  'audioTool.truePeak.value': '{value} dBTP',

  'audioExtract.page.eyebrow': 'AUDIO',
  'audioExtract.page.titleLead': 'Extraer',
  'audioExtract.page.titleTail': 'audio',
  'audioExtract.page.sub':
    'Extrae la pista de audio de un vídeo o archivo de audio. Todo se ejecuta en este equipo.',
  'audioExtract.intake.title': 'Vídeos y audio',
  'audioExtract.intake.sub':
    'Suelta vídeos o archivos de audio, o elígelos desde tu equipo.',
  'audioExtract.intake.dropTitle': 'Suelta vídeos o audio aquí',
  'audioExtract.intake.dropHint':
    'MP4 · MOV · MKV · WebM · MP3 · M4A · WAV · FLAC',
  'audioExtract.intake.dropAria':
    'Suelta o elige archivos para extraer el audio',
  'audioExtract.output.sub':
    'Elige el formato de audio y comprueba dónde irán los archivos.',
  'audioExtract.format.label': 'Formato de audio',
  'audioExtract.format.help':
    'MP3, M4A y Opus tienen pérdida; WAV y FLAC no.',
  'audioExtract.action.idle': 'Extraer audio',
  'audioExtract.action.one': 'Extraer el audio de 1 archivo',
  'audioExtract.action.many': 'Extraer el audio de {count} archivos',
  'audioExtract.action.busy': 'Extrayendo…',
  'audioExtract.helper.addOne': 'Añade al menos un vídeo o archivo de audio.',
  'audioExtract.error.start': 'No se pudo iniciar la extracción de audio.',
  'audioExtract.error.plan': 'No se pudo planificar la salida.',
  'audioExtract.error.cancel': 'No se pudo cancelar el trabajo.',
  'audioExtract.error.openFile': 'No se pudo abrir el archivo.',
  'audioExtract.error.openFolder': 'No se pudo abrir la carpeta.',
  'audioExtract.error.status': 'No se pudo leer el estado del trabajo.',
  'audioExtract.error.inspect': 'No se pudieron leer los tamaños de archivo.',

  'audioConvert.page.eyebrow': 'AUDIO',
  'audioConvert.page.titleLead': 'Convertir',
  'audioConvert.page.titleTail': 'audio',
  'audioConvert.page.sub':
    'Convierte audio entre MP3, M4A, Opus, WAV y FLAC. Todo se ejecuta en este equipo.',
  'audioConvert.intake.title': 'Archivos de audio',
  'audioConvert.intake.sub':
    'Suelta archivos de audio o elígelos desde tu equipo.',
  'audioConvert.intake.dropTitle': 'Suelta audio aquí',
  'audioConvert.intake.dropHint':
    'MP3 · M4A · AAC · OPUS · OGG · WAV · FLAC · WMA · AIFF',
  'audioConvert.intake.dropAria': 'Suelta o elige archivos de audio para convertir',
  'audioConvert.output.sub':
    'Elige el formato de audio y comprueba dónde irán los archivos.',
  'audioConvert.format.label': 'Formato de audio',
  'audioConvert.format.help':
    'MP3, M4A y Opus tienen pérdida; WAV y FLAC no.',
  'audioConvert.action.idle': 'Convertir audio',
  'audioConvert.action.one': 'Convertir 1 archivo de audio',
  'audioConvert.action.many': 'Convertir {count} archivos de audio',
  'audioConvert.action.busy': 'Convirtiendo…',
  'audioConvert.helper.addOne': 'Añade al menos un archivo de audio.',
  'audioConvert.error.start': 'No se pudo iniciar la conversión de audio.',
  'audioConvert.error.plan': 'No se pudo planificar la salida.',
  'audioConvert.error.cancel': 'No se pudo cancelar el trabajo.',
  'audioConvert.error.openFile': 'No se pudo abrir el archivo.',
  'audioConvert.error.openFolder': 'No se pudo abrir la carpeta.',
  'audioConvert.error.status': 'No se pudo leer el estado del trabajo.',
  'audioConvert.error.inspect': 'No se pudieron leer los tamaños de archivo.',

  'audioNormalize.page.eyebrow': 'AUDIO',
  'audioNormalize.page.titleLead': 'Normalizar',
  'audioNormalize.page.titleTail': 'audio',
  'audioNormalize.page.sub':
    'Lleva el audio a un objetivo de sonoridad para streaming, difusión o pódcast. Todo se ejecuta en este equipo.',
  'audioNormalize.intake.title': 'Archivos de audio',
  'audioNormalize.intake.sub':
    'Suelta archivos de audio o elígelos desde tu equipo.',
  'audioNormalize.intake.dropTitle': 'Suelta audio aquí',
  'audioNormalize.intake.dropHint':
    'MP3 · M4A · AAC · OPUS · OGG · WAV · FLAC · WMA · AIFF',
  'audioNormalize.intake.dropAria':
    'Suelta o elige archivos de audio para normalizar',
  'audioNormalize.output.sub':
    'Elige un objetivo de sonoridad y comprueba dónde irán los archivos.',
  'audioNormalize.target.label': 'Objetivo de sonoridad',
  'audioNormalize.target.streaming': 'Streaming (-14 LUFS)',
  'audioNormalize.target.broadcast': 'Difusión (-23 LUFS)',
  'audioNormalize.target.podcast': 'Pódcast (-16 LUFS)',
  'audioNormalize.target.help':
    'Normalización EBU R128 en dos pasadas. Se mantiene el contenedor de entrada.',
  'audioNormalize.truePeak.label': 'Límite de pico real',
  'audioNormalize.truePeak.help':
    'Entre -9 y 0 dBTP. El valor predeterminado es -1.5 dBTP.',
  'audioNormalize.truePeak.error':
    'Introduce un pico real entre {min} y {max} dBTP.',
  'audioNormalize.container.help':
    'Se mantiene el contenedor de entrada: OGG se guarda como Opus, AAC como M4A y el resto como MP3.',
  'audioNormalize.action.idle': 'Normalizar audio',
  'audioNormalize.action.one': 'Normalizar 1 archivo de audio',
  'audioNormalize.action.many': 'Normalizar {count} archivos de audio',
  'audioNormalize.action.busy': 'Normalizando…',
  'audioNormalize.helper.addOne': 'Añade al menos un archivo de audio.',
  'audioNormalize.error.start':
    'No se pudo iniciar la normalización de sonoridad.',
  'audioNormalize.error.plan': 'No se pudo planificar la salida.',
  'audioNormalize.error.cancel': 'No se pudo cancelar el trabajo.',
  'audioNormalize.error.openFile': 'No se pudo abrir el archivo.',
  'audioNormalize.error.openFolder': 'No se pudo abrir la carpeta.',
  'audioNormalize.error.status': 'No se pudo leer el estado del trabajo.',
  'audioNormalize.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
