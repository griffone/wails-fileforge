/**
 * Spanish copy for the download page. Keys mirror `en/web-download.ts`, which
 * is the source of truth.
 */
export const webDownloadEs = {
  'webDownload.page.eyebrow': 'Descargar contenido',
  'webDownload.page.titleLead': 'Descargar',
  'webDownload.page.titleTail': 'contenido',
  'webDownload.page.sub':
    'Pega un enlace por línea, elige una resolución que el enlace realmente ofrezca o una tasa de MP3, y descárgalo en este equipo.',
  'webDownload.page.localBadge': 'Solo local',

  'webDownload.intake.title': 'Enlaces',
  'webDownload.intake.sub':
    'Una URL por línea. Un enlace de lista se expande en los enlaces que contiene.',
  'webDownload.intake.label': 'Enlaces para descargar',
  'webDownload.intake.placeholder':
    'https://www.youtube.com/watch?v=...\nhttps://vimeo.com/...',
  'webDownload.intake.add': 'Agregar enlaces',
  'webDownload.intake.clear': 'Limpiar',
  'webDownload.intake.actionsAria': 'Acciones de la cola de enlaces',
  'webDownload.intake.emptyTitle': 'Sin enlaces',
  'webDownload.intake.emptyHint':
    'Pega un enlace arriba para leer los formatos que ofrece.',
  'webDownload.intake.countOne': '1 enlace',
  'webDownload.intake.countMany': '{count} enlaces',

  'webDownload.notice.emptyOne': 'Se ignoró 1 línea vacía.',
  'webDownload.notice.emptyMany': 'Se ignoraron {count} líneas vacías.',
  'webDownload.notice.duplicateOne': 'Se ignoró 1 enlace duplicado.',
  'webDownload.notice.duplicateMany': 'Se ignoraron {count} enlaces duplicados.',
  'webDownload.notice.playlistExpanded':
    'La lista se expandió en {count} enlaces.',
  'webDownload.notice.failedOne':
    'No se pudo leer {name}. Reintenta o quítalo antes de descargar.',
  'webDownload.notice.failedMany':
    'No se pudieron leer {count} enlaces: {names}. Reintenta o quítalos antes de descargar.',
  'webDownload.notice.qualityConflict':
    'Los enlaces piden calidades distintas. Usa "Aplicar a todos los enlaces" para que todas las filas soliciten el mismo formato.',

  'webDownload.row.probing': 'Leyendo formatos…',
  'webDownload.row.retry': 'Reintentar',
  'webDownload.row.remove': 'Quitar enlace',
  'webDownload.row.removeAria': 'Quitar {name}',
  'webDownload.row.playlistItem': 'Elemento de lista',
  'webDownload.row.durationAria': 'Duración {duration}',
  'webDownload.row.warningPlaylistEmpty':
    'Esta lista no informó enlaces, así que se descargará como un solo elemento.',

  'webDownload.kind.label': 'Formato',
  'webDownload.kind.video': 'Video (MP4)',
  'webDownload.kind.audio': 'Audio (MP3)',
  'webDownload.kind.videoSummary': 'Video · MP4',
  'webDownload.kind.audioSummary': 'Audio · MP3',
  'webDownload.kind.help':
    'Video descarga la mejor pista hasta la altura elegida y la combina con el mejor audio. Audio convierte la mejor pista a MP3.',

  'webDownload.quality.label': 'Calidad',
  'webDownload.quality.aria': 'Calidad para {name}',
  'webDownload.quality.bestVideo': 'Mejor disponible',
  'webDownload.quality.bestAudio': 'Mejor (VBR)',
  'webDownload.quality.fps': '{fps} fps',
  'webDownload.quality.audioOption': '{bitrate} kbps',
  'webDownload.quality.applyAll': 'Aplicar a todos los enlaces',
  'webDownload.quality.applyAllAria': 'Aplicar esta calidad a todos los enlaces',
  'webDownload.quality.summaryVideo': 'Hasta {height}p',
  'webDownload.quality.summaryAudio': 'MP3 a {bitrate} kbps',
  'webDownload.quality.summaryBest': 'Mejor disponible',

  'webDownload.output.title': 'Salida',
  'webDownload.output.sub':
    'Los nombres de archivo reales se planifican antes de iniciar la descarga.',
  'webDownload.output.noDestination': 'Elige una carpeta para las descargas',
  'webDownload.output.destinationHelp':
    'Las descargas necesitan una carpeta de destino. Elige una y se planifican los nombres de archivo.',

  'webDownload.legal.title': 'Tu responsabilidad',
  'webDownload.legal.body':
    'La descarga es tu responsabilidad. Descarga solo contenido sobre el que tengas derechos.',

  'webDownload.action.download': 'Descargar',
  'webDownload.action.downloadOne': 'Descargar 1 enlace',
  'webDownload.action.downloadMany': 'Descargar {count} enlaces',
  'webDownload.action.downloading': 'Descargando…',
  'webDownload.action.cancel': 'Cancelar',

  'webDownload.helper.addLinks': 'Agrega al menos un enlace para descargar.',
  'webDownload.helper.probing':
    'Leyendo los formatos de los enlaces antes de planificar la descarga.',
  'webDownload.helper.failed':
    'Reintenta o quita los enlaces que no se pudieron leer.',
  'webDownload.helper.destination':
    'Elige una carpeta de destino para las descargas.',
  'webDownload.helper.qualityConflict':
    'Usa "Aplicar a todos los enlaces" para que todas las filas pidan el mismo formato.',
  'webDownload.helper.planning': 'Comprobando los nombres de archivo planificados.',

  'webDownload.progress.aria': 'Progreso de la descarga',

  'webDownload.error.start': 'La descarga no pudo iniciarse.',
  'webDownload.error.plan': 'La descarga no pudo planificarse.',
  'webDownload.error.inspect': 'No se pudieron inspeccionar los archivos descargados.',
  'webDownload.error.status': 'El estado de la descarga no está disponible.',
  'webDownload.error.cancel': 'La descarga no pudo cancelarse.',
  'webDownload.error.openFile': 'No se pudo abrir el archivo.',
  'webDownload.error.openFolder': 'No se pudo abrir la carpeta.',
  'webDownload.error.probe': 'No se pudo leer este enlace.',

  'webDownload.status.waiting': 'Esperando para descargar…',
} as const;
