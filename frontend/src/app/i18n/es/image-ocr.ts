export const imageOcrEs = {
  'imageOcr.page.eyebrow': 'IMAGEN',
  'imageOcr.page.titleLead': 'Reconocer texto',
  'imageOcr.page.titleTail': 'en imágenes',
  'imageOcr.page.sub':
    'Extrae texto de capturas y fotos con Tesseract local. Escribe un archivo de texto sin formato o añade un PDF con texto reconocido para cada imagen.',
  'imageOcr.page.localBadge': 'Solo local',
  'imageOcr.capability.missing': 'Faltan dependencias: {dependencies}.',
  'imageOcr.capability.noLanguage':
    'No hay datos de idioma de Tesseract instalados; instala eng o spa.',
  'imageOcr.capability.installMissing':
    'Instala las dependencias de OCR que faltan y vuelve a cargar esta página.',
  'imageOcr.capability.installPieces':
    'Instala las piezas que faltan y vuelve a cargar esta página.',
  'imageOcr.capability.title':
    'El OCR de imágenes local no está disponible.',
  'imageOcr.language.installed': 'Instalados: {languages}.',
  'imageOcr.language.none':
    'No hay datos de idioma de Tesseract instalados.',
  'imageOcr.list.and': 'y',
  'imageOcr.intake.title': 'Imágenes',
  'imageOcr.intake.sub': 'Suelta imágenes o elígelas desde tu equipo.',
  'imageOcr.intake.dropTitle': 'Suelta imágenes aquí',
  'imageOcr.intake.dropAria':
    'Suelta o elige imágenes para extraer texto',
  'imageOcr.intake.actionsAria': 'Acciones de selección de imágenes',
  'imageOcr.intake.choose': 'Elegir archivos',
  'imageOcr.intake.clear': 'Limpiar',
  'imageOcr.intake.batchNote':
    'El mismo idioma y los mismos ajustes de salida se aplican a todas las imágenes.',
  'imageOcr.notice.filesOne': 'No se agregó 1 archivo.',
  'imageOcr.notice.filesMany': 'No se agregaron {count} archivos.',
  'imageOcr.notice.extensionOne': '{extension} no es compatible.',
  'imageOcr.notice.extensionsMany': '{head} y {tail} no son compatibles.',
  'imageOcr.output.title': 'Salida',
  'imageOcr.output.sub':
    'Elige el idioma y dónde irán los archivos de texto.',
  'imageOcr.field.language': 'Idioma',
  'imageOcr.language.noneInstalled': 'No hay datos de idioma instalados',
  'imageOcr.field.searchablePdf': 'PDF con texto reconocido',
  'imageOcr.field.writePdf': 'Escribir también un PDF con texto reconocido',
  'imageOcr.field.writePdfHelp':
    'Añade <name>_ocr.pdf junto al archivo <name>.txt.',
  'imageOcr.advanced.title': 'Avanzado',
  'imageOcr.advanced.hint': 'Resolución de reconocimiento',
  'imageOcr.field.dpi': 'DPI',
  'imageOcr.field.dpiHelp':
    '150–600. Un DPI más alto ayuda con textos pequeños, pero tarda más. Valor predeterminado: 300.',
  'imageOcr.advanced.reset': 'Restablecer ajustes avanzados',
  'imageOcr.note.title': 'El texto siempre se guarda',
  'imageOcr.note.body':
    'Cada imagen genera un archivo <name>.txt. Un resultado vacío se informa como advertencia, no como éxito.',
  'imageOcr.helper.addImage': 'Añade al menos una imagen.',
  'imageOcr.helper.unavailable':
    'El OCR de imágenes local no está disponible en este equipo.',
  'imageOcr.helper.chooseLanguage': 'Elige un idioma.',
  'imageOcr.helper.dpi': 'Introduce un DPI entre {min} y {max}.',
  'imageOcr.action.ocr': 'Reconocer imágenes',
  'imageOcr.action.ocrOne': 'Reconocer 1 imagen',
  'imageOcr.action.ocrMany': 'Reconocer {count} imágenes',
  'imageOcr.action.recognizing': 'Reconociendo…',
  'imageOcr.action.cancel': 'Cancelar',
  'imageOcr.destination.original': 'Junto a cada imagen',
  'imageOcr.progress.aria': 'Progreso de OCR',
  'imageOcr.status.waiting': 'Esperando para empezar',
  'imageOcr.error.start': 'No se pudo iniciar el trabajo de OCR.',
  'imageOcr.error.plan': 'No se pudo planificar la salida.',
  'imageOcr.error.cancel': 'No se pudo cancelar el trabajo.',
  'imageOcr.error.openFile': 'No se pudo abrir el archivo.',
  'imageOcr.error.openFolder': 'No se pudo abrir la carpeta.',
  'imageOcr.error.status': 'No se pudo leer el estado del trabajo.',
  'imageOcr.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
