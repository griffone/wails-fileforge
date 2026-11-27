export const imageUpscaleEs = {
  'imageUpscale.page.eyebrow': 'IMAGEN',
  'imageUpscale.page.titleLead': 'Ampliar',
  'imageUpscale.page.titleTail': 'imagen',
  'imageUpscale.page.sub':
    'Amplía imágenes 2x o 4x con un modelo ONNX local. Los modelos se descargan una sola vez y cada píxel se calcula en este equipo.',
  'imageUpscale.page.localBadge': 'Solo local',
  'imageUpscale.pack.engineFallback': 'Motor de ampliación',
  'imageUpscale.pack.modelFallback': 'Modelo de ampliación para fotos',
  'imageUpscale.pack.illustrationModelFallback':
    'Modelo de ampliación para ilustraciones',
  'imageUpscale.pack.checkErrorTitle':
    'No se pudo comprobar el paquete de ampliación.',
  'imageUpscale.pack.checkErrorDetail':
    'Vuelve a comprobar el paquete o ejecuta Instalar / reparar en Ajustes.',
  'imageUpscale.pack.needsRepairTitle':
    'El paquete de ampliación necesita reparación.',
  'imageUpscale.pack.missingTitle':
    'El paquete de ampliación no está instalado.',
  'imageUpscale.pack.detail':
    'Descarga el motor y los dos modelos una sola vez ({size}) y se ejecuta por completo en este equipo.',
  'imageUpscale.pack.size': 'unos 73 MB',
  'imageUpscale.pack.modelsSize': 'unos 22,5 MB',
  'imageUpscale.pack.install': 'Instalar paquete de ampliación ({size} de modelos)',
  'imageUpscale.pack.installFailed':
    'No se pudo iniciar la instalación del paquete de ampliación. Inténtalo de nuevo o usa Ajustes.',
  'imageUpscale.pack.cancel': 'Cancelar instalación',
  'imageUpscale.pack.cancelling': 'Cancelando…',
  'imageUpscale.pack.progressAria':
    'Progreso de instalación del paquete de ampliación',
  'imageUpscale.pack.badge.checking': 'Comprobando paquete…',
  'imageUpscale.pack.badge.ready': 'Paquete listo',
  'imageUpscale.pack.badge.missing': 'Paquete ausente',
  'imageUpscale.pack.openSettings': 'Abrir Ajustes',
  'imageUpscale.pack.recheck': 'Volver a comprobar',
  'imageUpscale.pack.rechecking': 'Comprobando…',
  'imageUpscale.intake.title': 'Imágenes',
  'imageUpscale.intake.sub': 'Suelta imágenes o elígelas desde tu equipo.',
  'imageUpscale.intake.dropTitle': 'Suelta imágenes aquí',
  'imageUpscale.intake.dropAria': 'Suelta o elige imágenes para ampliarlas',
  'imageUpscale.intake.actionsAria': 'Acciones de selección de imágenes',
  'imageUpscale.intake.choose': 'Elegir archivos',
  'imageUpscale.intake.clear': 'Limpiar',
  'imageUpscale.intake.batchNote':
    'La misma escala y el mismo modelo se aplican a todas las imágenes.',
  'imageUpscale.notice.filesOne': 'No se agregó 1 archivo.',
  'imageUpscale.notice.filesMany': 'No se agregaron {count} archivos.',
  'imageUpscale.notice.extensionOne': '{extension} no es compatible.',
  'imageUpscale.notice.extensionsMany':
    '{head} y {tail} no son compatibles.',
  'imageUpscale.output.title': 'Salida',
  'imageUpscale.output.sub':
    'Elige la escala, el modelo y dónde irán los archivos ampliados.',
  'imageUpscale.field.scale': 'Escala',
  'imageUpscale.scale.2x': '2x',
  'imageUpscale.scale.4x': '4x',
  'imageUpscale.field.scaleHelp':
    '2x se produce reduciendo la salida 4x del modelo, así que ambas opciones comparten un solo modelo.',
  'imageUpscale.field.model': 'Modelo',
  'imageUpscale.model.general': 'Foto',
  'imageUpscale.model.anime': 'Ilustración y dibujos',
  'imageUpscale.field.modelHelp':
    'Foto es para fotografías y capturas con textura real. Ilustración y dibujos es para dibujos, logos, cómics y arte digital, y conserva líneas duras y color plano.',
  'imageUpscale.field.tile': 'Tamaño de tesela',
  'imageUpscale.tile.64': '64 px',
  'imageUpscale.tile.128': '128 px',
  'imageUpscale.tile.256': '256 px',
  'imageUpscale.tile.512': '512 px',
  'imageUpscale.field.tileHelp':
    'El modelo de fotos trabaja con teselas fijas de 128 px. El tamaño de tesela se usa con el modelo de ilustraciones y con modelos propios de entrada dinámica.',
  'imageUpscale.field.format': 'Formato',
  'imageUpscale.format.png': 'PNG (sin pérdida)',
  'imageUpscale.format.jpeg': 'JPEG',
  'imageUpscale.format.webp': 'WebP',
  'imageUpscale.field.quality': 'Calidad',
  'imageUpscale.field.qualityAria': 'Calidad de salida',
  'imageUpscale.field.qualityHelp':
    'Se aplica a JPEG y WebP. PNG siempre es sin pérdida.',
  'imageUpscale.field.animeModel': 'Archivo del modelo de ilustraciones',
  'imageUpscale.animeModel.configured': 'Usando tu archivo propio: {path}',
  'imageUpscale.animeModel.included':
    'El modelo de ilustraciones está incluido en el paquete.',
  'imageUpscale.animeModel.help':
    'Avanzado: puedes usar tu propia exportación ONNX RealESRGAN x4 en esta opción. Si no, se usa el modelo incluido.',
  'imageUpscale.animeModel.advanced': 'Elegir un archivo propio en Ajustes',
  'imageUpscale.note.title': 'Una descarga y luego sin conexión',
  'imageUpscale.note.body':
    'El motor y los dos modelos se descargan una vez en la configuración inicial o desde Ajustes → Herramientas y modelos, y se verifican con sus sumas de comprobación. La inferencia se ejecuta localmente con ONNX Runtime; no se sube nada. Las salidas se llaman <name>_2x o <name>_4x.',
  'imageUpscale.note.limit':
    'Las salidas están limitadas a 64 megapíxeles. Para 4x, parte de una imagen menor a 4000 × 4000 px.',
  'imageUpscale.helper.addImage': 'Añade al menos una imagen.',
  'imageUpscale.helper.checking': 'Comprobando el paquete de ampliación…',
  'imageUpscale.helper.notReady':
    'El motor de ampliación o el modelo elegido no está instalado. Usa Instalar paquete de ampliación arriba.',
  'imageUpscale.helper.anime':
    'Falta el modelo de ilustraciones. Usa Instalar paquete de ampliación arriba.',
  'imageUpscale.action.upscale': 'Ampliar imagen',
  'imageUpscale.action.upscaleOne': 'Ampliar 1 imagen',
  'imageUpscale.action.upscaleMany': 'Ampliar {count} imágenes',
  'imageUpscale.action.upscaling': 'Ampliando…',
  'imageUpscale.action.cancel': 'Cancelar',
  'imageUpscale.destination.original': 'Junto a cada imagen',
  'imageUpscale.progress.aria': 'Progreso de la ampliación',
  'imageUpscale.status.waiting': 'Esperando para empezar',
  'imageUpscale.error.start': 'No se pudo iniciar la ampliación.',
  'imageUpscale.error.plan': 'No se pudo planificar la salida.',
  'imageUpscale.error.cancel': 'No se pudo cancelar el trabajo.',
  'imageUpscale.error.openFile': 'No se pudo abrir el archivo.',
  'imageUpscale.error.openFolder': 'No se pudo abrir la carpeta.',
  'imageUpscale.error.status': 'No se pudo leer el estado del trabajo.',
  'imageUpscale.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
