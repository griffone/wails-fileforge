export const imageCompressEs = {
  'imageCompress.page.eyebrow': 'IMAGEN',
  'imageCompress.page.titleLead': 'Comprimir',
  'imageCompress.page.titleTail': 'imágenes',
  'imageCompress.page.sub':
    'Comprime imágenes a un tamaño de archivo objetivo. La herramienta busca la calidad, usa una paleta para PNG y puede reducir la escala cuando el objetivo lo requiere.',
  'imageCompress.page.localBadge': 'Solo local',
  'imageCompress.intake.title': 'Imágenes',
  'imageCompress.intake.sub': 'Suelta imágenes o elígelas desde tu equipo.',
  'imageCompress.intake.dropTitle': 'Suelta imágenes aquí',
  'imageCompress.intake.dropAria': 'Suelta o elige imágenes para comprimir',
  'imageCompress.intake.actionsAria': 'Acciones de selección de imágenes',
  'imageCompress.intake.choose': 'Elegir archivos',
  'imageCompress.intake.clear': 'Limpiar',
  'imageCompress.intake.batchNote':
    'El mismo objetivo y los mismos ajustes se aplican a todas las imágenes.',
  'imageCompress.notice.filesOne': 'No se agregó 1 archivo.',
  'imageCompress.notice.filesMany': 'No se agregaron {count} archivos.',
  'imageCompress.notice.extensionOne': '{extension} no es compatible.',
  'imageCompress.notice.extensionsMany':
    '{head} y {tail} no son compatibles.',
  'imageCompress.output.title': 'Salida',
  'imageCompress.output.sub':
    'Define el objetivo y comprueba dónde irán los archivos.',
  'imageCompress.field.target': 'Tamaño objetivo (MB)',
  'imageCompress.field.format': 'Formato',
  'imageCompress.format.auto.label': 'Automático',
  'imageCompress.format.jpeg.label': 'JPEG',
  'imageCompress.format.webp.label': 'WebP',
  'imageCompress.format.png.label': 'PNG',
  'imageCompress.formatHelp.auto':
    'Conserva JPEG, WebP y PNG. Los demás orígenes se convierten en JPEG, o en PNG cuando tienen transparencia.',
  'imageCompress.formatHelp.png':
    'PNG no tiene pérdida; la herramienta cuantiza la paleta para alcanzar el objetivo.',
  'imageCompress.formatHelp.other':
    'Todas las imágenes se codifican como {format}.',
  'imageCompress.field.downscaling': 'Reducción de escala',
  'imageCompress.field.allowDownscale':
    'Permitir reducir la escala para alcanzar el objetivo',
  'imageCompress.field.allowDownscaleHelp':
    'Reduce el lado mayor en pasos cuando la calidad por sí sola no alcanza el objetivo. Nunca amplía.',
  'imageCompress.advanced.title': 'Avanzado',
  'imageCompress.advanced.hint':
    'Límite de dimensión, techo de calidad y metadatos',
  'imageCompress.field.maxDimension': 'Dimensión máxima (px)',
  'imageCompress.field.maxDimensionPlaceholder': 'Tamaño original',
  'imageCompress.field.maxDimensionHelp':
    'Limita el lado mayor antes de comprimir. Déjalo vacío para conservar el original.',
  'imageCompress.field.qualityCeiling': 'Techo de calidad (40–95)',
  'imageCompress.field.qualityPlaceholder': 'Valor del codificador',
  'imageCompress.field.qualityHelp':
    '40–95. Limita la búsqueda del codificador; PNG lo ignora y cuantiza en su lugar.',
  'imageCompress.field.metadata': 'Metadatos',
  'imageCompress.field.stripMetadata': 'Eliminar metadatos',
  'imageCompress.field.stripMetadataHelp':
    'Elimina EXIF, GPS y perfiles de color salvo que conservarlos sea la única forma de alcanzar el objetivo.',
  'imageCompress.advanced.reset': 'Restablecer ajustes avanzados',
  'imageCompress.note.title': 'Los objetivos son un máximo esfuerzo',
  'imageCompress.note.body':
    'Cuando una imagen no puede alcanzar el objetivo, la herramienta se detiene en el resultado más pequeño que puede producir e informa el tamaño real.',
  'imageCompress.helper.addImage':
    'Añade al menos una imagen para comprimir.',
  'imageCompress.target.errorPositive':
    'Introduce un tamaño objetivo mayor que 0 MB.',
  'imageCompress.target.errorMax':
    'Introduce un tamaño objetivo de {max} MB o menos.',
  'imageCompress.target.errorTooLarge':
    'El tamaño objetivo es demasiado grande para usarlo.',
  'imageCompress.target.noTarget': 'Sin objetivo',
  'imageCompress.target.summary': 'Objetivo de {size} por imagen',
  'imageCompress.action.compress': 'Comprimir',
  'imageCompress.action.compressOne': 'Comprimir 1 imagen',
  'imageCompress.action.compressMany': 'Comprimir {count} imágenes',
  'imageCompress.action.compressing': 'Comprimiendo…',
  'imageCompress.action.cancel': 'Cancelar',
  'imageCompress.outcome.single':
    'La salida está en el objetivo de {target} o por debajo.',
  'imageCompress.outcome.all':
    'Las {count} salidas están en el objetivo de {target} o por debajo.',
  'imageCompress.outcome.none':
    'Ninguna salida alcanzó el objetivo de {target}; los archivos son los más pequeños que el codificador pudo producir.',
  'imageCompress.outcome.partial':
    '{atTarget} de {total} salidas están en el objetivo de {target} o por debajo; el resto son las más pequeñas disponibles.',
  'imageCompress.savings.saved': 'Ahorro: {size} ({percent}%)',
  'imageCompress.savings.larger': 'La salida pesa {size} más',
  'imageCompress.destination.original': 'Junto a cada original',
  'imageCompress.progress.aria': 'Progreso de la compresión',
  'imageCompress.status.waiting': 'Esperando para empezar',
  'imageCompress.error.start': 'No se pudo iniciar la compresión.',
  'imageCompress.error.plan': 'No se pudo planificar la salida.',
  'imageCompress.error.cancel': 'No se pudo cancelar el trabajo.',
  'imageCompress.error.openFile': 'No se pudo abrir el archivo.',
  'imageCompress.error.openFolder': 'No se pudo abrir la carpeta.',
  'imageCompress.error.status': 'No se pudo leer el estado del trabajo.',
  'imageCompress.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
