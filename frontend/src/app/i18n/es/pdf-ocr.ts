export const pdfOcrEs = {
  'pdfOcr.page.eyebrow': 'PDF',
  'pdfOcr.page.titleLead': 'OCR de',
  'pdfOcr.page.titleTail': 'PDF',
  'pdfOcr.page.sub':
    'Haz que los PDF escaneados se puedan buscar con OCR local. Todo se ejecuta en este equipo.',
  'pdfOcr.page.localBadge': 'Solo local',
  'pdfOcr.list.and': 'y',
  'pdfOcr.capability.missing': 'Faltan {dependencies}.',
  'pdfOcr.capability.noLanguage':
    'No hay datos de idioma de Tesseract instalados; instala eng o spa.',
  'pdfOcr.capability.installMissing':
    'Instala las dependencias de OCR y recarga esta página.',
  'pdfOcr.capability.installPieces':
    'Instala las piezas que faltan y recarga esta página.',
  'pdfOcr.capability.title': 'El OCR local no está disponible.',
  'pdfOcr.language.weak':
    'Solo están instalados los datos de idioma {languages}. Instala los datos de Tesseract eng/spa para mejorar la precisión.',
  'pdfOcr.language.installed': 'Instalados: {languages}.',
  'pdfOcr.language.none':
    'No hay datos de idioma de Tesseract instalados.',
  'pdfOcr.language.noneInstalled': 'No hay datos de idioma instalados',
  'pdfOcr.intake.title': 'PDF',
  'pdfOcr.intake.sub': 'Suelta PDF escaneados o elígelos desde tu equipo.',
  'pdfOcr.intake.dropTitle': 'Suelta PDF escaneados aquí',
  'pdfOcr.intake.dropHint': 'Solo archivos PDF · El OCR se ejecuta localmente',
  'pdfOcr.intake.dropAria': 'Suelta o elige PDF escaneados',
  'pdfOcr.intake.actionsAria': 'Acciones de selección de PDF',
  'pdfOcr.intake.choose': 'Elegir PDF',
  'pdfOcr.intake.clear': 'Limpiar',
  'pdfOcr.notice.rejectedOne':
    '{name} no se agregó porque no es un PDF.',
  'pdfOcr.notice.rejectedMany':
    '{names} no se agregaron porque no son PDF.',
  'pdfOcr.notice.duplicateOne': '{name} ya está en la cola.',
  'pdfOcr.notice.duplicateMany': 'Ya hay {count} PDF en la cola.',
  'pdfOcr.settings.title': 'Ajustes',
  'pdfOcr.settings.sub':
    'Elige el idioma de reconocimiento y el destino de la salida.',
  'pdfOcr.settings.language': 'Idioma',
  'pdfOcr.settings.outputFolder': 'Carpeta de salida',
  'pdfOcr.settings.chooseFolder': 'Elegir carpeta',
  'pdfOcr.settings.outputHelp':
    'El PDF con texto buscable se guarda junto a cada PDF de origen.',
  'pdfOcr.settings.useDefault': 'Usar la carpeta predeterminada',
  'pdfOcr.settings.sidecar': 'Texto complementario',
  'pdfOcr.settings.sidecarLabel': 'Escribir un archivo de texto junto al PDF',
  'pdfOcr.settings.sidecarHelp':
    'Escribe <name>_ocr.txt junto al PDF con texto buscable.',
  'pdfOcr.settings.dpiNote':
    'Las páginas se rasterizan a 300 DPI. El texto vectorial se reemplaza por una imagen con una capa de texto invisible.',
  'pdfOcr.destination.each': 'Junto a cada PDF',
  'pdfOcr.helper.addOne': 'Añade al menos un PDF.',
  'pdfOcr.helper.unavailable':
    'El OCR local no está disponible en este equipo.',
  'pdfOcr.helper.chooseLanguage': 'Elige un idioma.',
  'pdfOcr.action.ocr': 'Aplicar OCR a PDF',
  'pdfOcr.action.ocrOne': 'Aplicar OCR a 1 PDF',
  'pdfOcr.action.ocrMany': 'Aplicar OCR a {count} PDF',
  'pdfOcr.action.recognizing': 'Haciendo que se pueda buscar…',
  'pdfOcr.action.cancel': 'Cancelar',
  'pdfOcr.progress.aria': 'Progreso del OCR',
  'pdfOcr.progress.page': 'OCR de la página {current} de {total}',
  'pdfOcr.status.waiting': 'Esperando para empezar',
  'pdfOcr.error.start': 'No se pudo iniciar el trabajo de OCR.',
  'pdfOcr.error.plan': 'No se pudo planificar la salida.',
  'pdfOcr.error.cancel': 'No se pudo cancelar el trabajo.',
  'pdfOcr.error.openFile': 'No se pudo abrir el archivo.',
  'pdfOcr.error.openFolder': 'No se pudo abrir la carpeta.',
  'pdfOcr.error.status': 'No se pudo leer el estado del trabajo.',
  'pdfOcr.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
