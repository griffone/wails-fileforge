export const pdfSplitEs = {
  'pdfSplit.page.eyebrow': 'PDF',
  'pdfSplit.page.titleLead': 'Dividir',
  'pdfSplit.page.titleTail': 'PDF',
  'pdfSplit.page.sub':
    'Divide PDF por página, rangos explícitos, páginas elegidas, cantidad de páginas o marcadores. Todo se ejecuta en este equipo.',
  'pdfSplit.page.localBadge': 'Solo local',
  'pdfSplit.intake.title': 'PDF',
  'pdfSplit.intake.sub': 'Suelta PDF o elígelos desde tu equipo.',
  'pdfSplit.intake.dropTitle': 'Suelta PDF aquí',
  'pdfSplit.intake.dropHint': 'Solo archivos PDF',
  'pdfSplit.intake.dropAria': 'Suelta o elige PDF para dividir',
  'pdfSplit.intake.actionsAria': 'Acciones de selección de PDF',
  'pdfSplit.intake.choose': 'Elegir PDF',
  'pdfSplit.intake.clear': 'Limpiar',
  'pdfSplit.notice.rejectedOne':
    '{name} no se agregó porque no es un PDF.',
  'pdfSplit.notice.rejectedMany':
    '{names} no se agregaron porque no son PDF.',
  'pdfSplit.notice.duplicateOne': '{name} ya está en la cola.',
  'pdfSplit.notice.duplicateMany': 'Ya hay {count} PDF en la cola.',
  'pdfSplit.output.title': 'Salida',
  'pdfSplit.output.sub':
    'Elige cómo dividir y comprueba dónde irán los archivos.',
  'pdfSplit.strategy.aria': 'Estrategia de división',
  'pdfSplit.strategy.every_page.label': 'Cada página',
  'pdfSplit.strategy.every_page.hint': 'Un PDF por cada página.',
  'pdfSplit.strategy.ranges.label': 'Rangos de páginas',
  'pdfSplit.strategy.ranges.hint': 'Divide en los rangos que escribas.',
  'pdfSplit.strategy.selected.label': 'Páginas seleccionadas',
  'pdfSplit.strategy.selected.hint':
    'Elige páginas; las consecutivas se agrupan',
  'pdfSplit.strategy.pages_per_file.label': 'Páginas por archivo',
  'pdfSplit.strategy.pages_per_file.hint': 'Divide cada N páginas.',
  'pdfSplit.strategy.bookmarks.label': 'Marcadores',
  'pdfSplit.strategy.bookmarks.hint':
    'Un archivo por marcador de nivel superior.',
  'pdfSplit.ranges.label': 'Rangos de páginas',
  'pdfSplit.ranges.placeholder': '1-3,5,8-10',
  'pdfSplit.ranges.help':
    'Usa números de página y rangos separados por comas. No se permiten duplicados ni superposiciones.',
  'pdfSplit.selected.help':
    'Elige páginas en la vista previa de abajo. Las páginas consecutivas forman una parte.',
  'pdfSplit.selected.batch':
    'Las mismas páginas se aplican a cada PDF del lote.',
  'pdfSplit.pagesPerFile.label': 'Páginas por archivo',
  'pdfSplit.pagesPerFile.help':
    'Divide cada {count} páginas en un archivo. El último archivo puede tener menos páginas.',
  'pdfSplit.pagesPerFile.error': 'Introduce un número entero de 1 a 1000.',
  'pdfSplit.bookmarks.help':
    'Usa el esquema de nivel superior del PDF. Si no tiene marcadores, el plan explicará qué hacer.',
  'pdfSplit.unit.partOne': 'parte',
  'pdfSplit.unit.partMany': 'partes',
  'pdfSplit.destination.each': 'Junto a cada PDF',
  'pdfSplit.helper.addOne': 'Añade al menos un PDF para dividir.',
  'pdfSplit.helper.ranges':
    'Introduce los rangos de páginas que quieres dividir.',
  'pdfSplit.helper.selection':
    'Selecciona al menos una página para construir la división.',
  'pdfSplit.helper.pagesPerFile':
    'Introduce un número entero de páginas de 1 a 1000.',
  'pdfSplit.action.split': 'Dividir PDF',
  'pdfSplit.action.splitOne': 'Dividir 1 PDF',
  'pdfSplit.action.splitMany': 'Dividir {count} PDF',
  'pdfSplit.action.splitInto': '{base} en {count}',
  'pdfSplit.action.splitting': 'Dividiendo…',
  'pdfSplit.action.cancel': 'Cancelar',
  'pdfSplit.preview.panelTitle': 'Archivos resultantes',
  'pdfSplit.preview.subSelected':
    'Elige páginas; las páginas consecutivas forman un archivo.',
  'pdfSplit.preview.subBookmarks':
    'La salida por marcadores se lista en el plan de arriba.',
  'pdfSplit.preview.subDefault':
    'Previsualiza cada archivo que creará la división.',
  'pdfSplit.preview.loading': 'Cargando vista previa del PDF…',
  'pdfSplit.preview.rendering': 'Generando miniaturas de páginas…',
  'pdfSplit.preview.unavailable':
    'La vista previa no está disponible para este PDF. La división funcionará igualmente.',
  'pdfSplit.preview.unavailableSelectable':
    'Cambia a Rangos de páginas para dividirlo sin vista previa.',
  'pdfSplit.preview.fileLabel': 'Vista previa de',
  'pdfSplit.preview.selectAll': 'Seleccionar todo',
  'pdfSplit.preview.clear': 'Limpiar',
  'pdfSplit.preview.selectionOne': '{count} seleccionada',
  'pdfSplit.preview.selectionMany': '{count} seleccionadas',
  'pdfSplit.preview.selectPage': 'Seleccionar página {page}',
  'pdfSplit.preview.deselectPage': 'Deseleccionar página {page}',
  'pdfSplit.preview.pageAlt': 'Vista previa de la página {page}',
  'pdfSplit.preview.pageNumber': 'Página {page}',
  'pdfSplit.preview.selectAria': 'Selecciona páginas para dividir',
  'pdfSplit.preview.pagesAria': 'Páginas resultantes',
  'pdfSplit.preview.partsAria': 'Partes resultantes',
  'pdfSplit.preview.partPage': 'Parte {part} · Página {page}',
  'pdfSplit.preview.partPages': 'Parte {part} · Páginas {start}-{end}',
  'pdfSplit.preview.bookmarkHint':
    'La vista previa de marcadores aparece después de planificar. El plan lista cada archivo resultante.',
  'pdfSplit.preview.selectHint':
    'Selecciona páginas arriba para previsualizar las partes que crean.',
  'pdfSplit.preview.limit': 'Mostrando las primeras {shown} de {total} páginas',
  'pdfSplit.preview.showAll': 'Mostrar todo',
  'pdfSplit.preview.error.emptyRanges':
    'Introduce rangos de páginas para previsualizar las partes resultantes.',
  'pdfSplit.preview.error.invalidSyntax':
    'Usa números de página y rangos separados por comas, como 1-3,5,8-10.',
  'pdfSplit.preview.error.invalidRange':
    'Cada rango debe empezar en una página menor o igual que su final.',
  'pdfSplit.preview.error.outside':
    'La página {page} está fuera de este PDF ({total} páginas).',
  'pdfSplit.preview.error.duplicate':
    'La página {page} aparece más de una vez. Elimina los rangos duplicados o superpuestos.',
  'pdfSplit.progress.aria': 'Progreso de la división',
  'pdfSplit.status.waiting': 'Esperando para empezar',
  'pdfSplit.error.start': 'No se pudo iniciar la división.',
  'pdfSplit.error.plan': 'No se pudo planificar la salida.',
  'pdfSplit.error.cancel': 'No se pudo cancelar el trabajo.',
  'pdfSplit.error.openFile': 'No se pudo abrir el archivo.',
  'pdfSplit.error.openFolder': 'No se pudo abrir la carpeta.',
  'pdfSplit.error.status': 'No se pudo leer el estado del trabajo.',
  'pdfSplit.error.inspect': 'No se pudieron leer los tamaños de archivo.',
} as const;
