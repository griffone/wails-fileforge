/**
 * Operaciones de páginas de PDF: rotar, eliminar páginas, reordenar y extraer
 * imágenes. Los mensajes de validación reflejan las razones del backend.
 */
export const pdfPageOpsEs = {
  'pdfPageOps.page.localBadge': 'Solo local',
  'pdfPageOps.intake.title': 'PDF',
  'pdfPageOps.intake.sub': 'Suelta PDF o elígelos desde tu equipo.',
  'pdfPageOps.intake.dropTitle': 'Suelta PDF aquí',
  'pdfPageOps.intake.dropHint': 'Solo archivos PDF',
  'pdfPageOps.intake.dropAria': 'Suelta o elige PDF',
  'pdfPageOps.intake.actionsAria': 'Acciones de selección de PDF',
  'pdfPageOps.intake.choose': 'Elegir PDF',
  'pdfPageOps.intake.clear': 'Limpiar',
  'pdfPageOps.notice.rejectedOne': '{name} no se agregó porque no es un PDF.',
  'pdfPageOps.notice.rejectedMany':
    '{names} no se agregaron porque no son PDF.',
  'pdfPageOps.notice.duplicateOne': '{name} ya está en la cola.',
  'pdfPageOps.notice.duplicateMany': 'Ya hay {count} PDF en la cola.',
  'pdfPageOps.output.title': 'Salida',
  'pdfPageOps.output.useOriginalFolders': 'Usar las carpetas originales',
  'pdfPageOps.destination.each': 'Junto a cada PDF',
  'pdfPageOps.badge.angle': '{angle}°',
  'pdfPageOps.badge.summary': '{value} · {detail}',
  'pdfPageOps.pages.label': 'Páginas',
  'pdfPageOps.pages.placeholder': '1-3,5,8-10',
  'pdfPageOps.pages.help':
    'Deja el campo vacío para aplicar a todas las páginas.',
  'pdfPageOps.pages.error.required':
    'Introduce las páginas, por ejemplo 1-3,5.',
  'pdfPageOps.pages.error.emptyEntry':
    'La selección de páginas tiene una entrada vacía. Elimina la coma sobrante.',
  'pdfPageOps.pages.error.token':
    '"{token}" no es una página. Las páginas empiezan en 1.',
  'pdfPageOps.pages.error.range':
    '"{token}" no es un rango válido. Usa inicio-fin, por ejemplo 4-6.',
  'pdfPageOps.pages.error.rangeOrder':
    'Escribe el rango "{token}" en orden ascendente, por ejemplo 4-6.',
  'pdfPageOps.pages.error.overlap':
    'Las páginas {first} y {second} se superponen. Indica cada página una sola vez.',
  'pdfPageOps.pages.error.outOfBounds':
    'La selección supera el documento de {count} páginas. Elige páginas hasta {count}.',
  'pdfPageOps.pages.error.allPages':
    'No se pueden eliminar todas las páginas: la selección cubre las {count} páginas. Conserva al menos una página.',
  'pdfPageOps.order.error.required':
    'Introduce el orden de páginas, por ejemplo 3,1,2,4-6, o usa Invertir.',
  'pdfPageOps.order.error.emptyEntry':
    'El orden tiene una entrada vacía. Elimina la coma sobrante.',
  'pdfPageOps.order.error.token':
    '"{token}" no es una página. Las páginas empiezan en 1.',
  'pdfPageOps.order.error.range':
    '"{token}" no es un rango válido. Usa inicio-fin, por ejemplo 4-6.',
  'pdfPageOps.order.error.rangeOrder':
    'Escribe el rango "{token}" en orden ascendente, por ejemplo 4-6.',
  'pdfPageOps.order.error.outOfBounds':
    'La página {page} no existe. El PDF tiene {count} páginas.',
  'pdfPageOps.order.error.missing':
    'El orden debe incluir cada página exactamente una vez. Faltan las páginas: {missing}.',
  'pdfPageOps.order.error.duplicates':
    'El orden debe incluir cada página exactamente una vez. Se repiten las páginas: {duplicates}.',
  'pdfPageOps.order.error.missingAndDuplicates':
    'El orden debe incluir cada página exactamente una vez. Faltan las páginas: {missing}. Se repiten: {duplicates}.',
  'pdfPageOps.action.cancel': 'Cancelar',
  'pdfPageOps.progress.aria': 'Progreso de la operación de páginas',
  'pdfPageOps.status.waiting': 'Esperando para empezar',
  'pdfPageOps.error.start': 'La operación de páginas no pudo empezar.',
  'pdfPageOps.error.plan': 'La salida no pudo planificarse.',
  'pdfPageOps.error.cancel': 'El trabajo no pudo cancelarse.',
  'pdfPageOps.error.openFile': 'No se pudo abrir el archivo.',
  'pdfPageOps.error.openFolder': 'No se pudo abrir la carpeta.',
  'pdfPageOps.error.status': 'No se pudo leer el estado del trabajo.',
  'pdfPageOps.error.inspect': 'No se pudieron leer los tamaños de archivo.',

  'pdfRotate.page.eyebrow': 'PDF',
  'pdfRotate.page.titleLead': 'Rotar',
  'pdfRotate.page.titleTail': 'PDF',
  'pdfRotate.page.sub':
    'Gira todas las páginas, o solo las que elijas, en sentido horario 90, 180 o 270 grados. Todo se ejecuta en este equipo.',
  'pdfRotate.output.sub':
    'Elige el ángulo y las páginas, y comprueba dónde irá el archivo.',
  'pdfRotate.angle.label': 'Ángulo',
  'pdfRotate.angle.90.label': '90°',
  'pdfRotate.angle.90.hint': 'Un cuarto de vuelta horario',
  'pdfRotate.angle.180.label': '180°',
  'pdfRotate.angle.180.hint': 'Boca abajo',
  'pdfRotate.angle.270.label': '270°',
  'pdfRotate.angle.270.hint': 'Tres cuartos de vuelta horario',
  'pdfRotate.badge.pagesAll': 'Todas las páginas',
  'pdfRotate.badge.pagesSelected': 'Páginas {pages}',
  'pdfRotate.action.idle': 'Rotar PDF',
  'pdfRotate.action.one': 'Rotar 1 PDF',
  'pdfRotate.action.many': 'Rotar {count} PDF',
  'pdfRotate.action.busy': 'Rotando…',
  'pdfRotate.helper.addOne': 'Añade al menos un PDF para rotar.',

  'pdfRemovePages.page.eyebrow': 'PDF',
  'pdfRemovePages.page.titleLead': 'Eliminar',
  'pdfRemovePages.page.titleTail': 'páginas',
  'pdfRemovePages.page.sub':
    'Elimina las páginas que indiques y conserva el resto del documento. Todo se ejecuta en este equipo.',
  'pdfRemovePages.output.sub':
    'Elige las páginas que quieres eliminar y comprueba dónde irá el archivo.',
  'pdfRemovePages.pages.label': 'Páginas a eliminar',
  'pdfRemovePages.pages.help':
    'Usa números de página y rangos separados por comas, por ejemplo 1-3,5.',
  'pdfRemovePages.pages.note':
    'Debe quedar al menos una página en cada documento.',
  'pdfRemovePages.badge.none': 'Sin páginas elegidas',
  'pdfRemovePages.badge.pages': 'Eliminar {pages}',
  'pdfRemovePages.action.idle': 'Eliminar páginas',
  'pdfRemovePages.action.one': 'Eliminar páginas de 1 PDF',
  'pdfRemovePages.action.many': 'Eliminar páginas de {count} PDF',
  'pdfRemovePages.action.busy': 'Eliminando páginas…',
  'pdfRemovePages.helper.addOne':
    'Añade al menos un PDF del que eliminar páginas.',
  'pdfRemovePages.helper.pages': 'Introduce las páginas que quieres eliminar.',

  'pdfReorder.page.eyebrow': 'PDF',
  'pdfReorder.page.titleLead': 'Reordenar',
  'pdfReorder.page.titleTail': 'páginas',
  'pdfReorder.page.sub':
    'Reescribe el documento en un orden de páginas explícito, o inviértelo. Cada página debe aparecer exactamente una vez.',
  'pdfReorder.output.sub':
    'Escribe el orden o usa Invertir, y comprueba dónde irá el archivo.',
  'pdfReorder.order.label': 'Orden de páginas',
  'pdfReorder.order.placeholder': '3,1,2,4-6',
  'pdfReorder.order.help':
    'Indica cada página exactamente una vez con números y rangos, o usa Invertir.',
  'pdfReorder.order.reverse': 'Invertir',
  'pdfReorder.order.missing': 'Faltan páginas: {pages}',
  'pdfReorder.order.duplicates': 'Páginas repetidas: {pages}',
  'pdfReorder.badge.empty': 'Sin orden todavía',
  'pdfReorder.badge.reverse': 'Orden invertido',
  'pdfReorder.badge.custom': 'Orden personalizado',
  'pdfReorder.action.idle': 'Reordenar páginas',
  'pdfReorder.action.one': 'Reordenar 1 PDF',
  'pdfReorder.action.many': 'Reordenar {count} PDF',
  'pdfReorder.action.busy': 'Reordenando páginas…',
  'pdfReorder.helper.addOne': 'Añade al menos un PDF para reordenar.',
  'pdfReorder.helper.order': 'Introduce el orden de páginas, o usa Invertir.',

  'pdfExtractImages.page.eyebrow': 'PDF',
  'pdfExtractImages.page.titleLead': 'Extraer',
  'pdfExtractImages.page.titleTail': 'imágenes',
  'pdfExtractImages.page.sub':
    'Guarda cada imagen incrustada de las páginas que elijas en su propia carpeta. Todo se ejecuta en este equipo.',
  'pdfExtractImages.output.sub':
    'Elige las páginas que quieres revisar y comprueba dónde irá la carpeta.',
  'pdfExtractImages.pages.label': 'Páginas a revisar',
  'pdfExtractImages.pages.help':
    'Deja el campo vacío para revisar todas las páginas.',
  'pdfExtractImages.folderNote':
    'La salida es una carpeta, no un PDF: cada entrada recibe su propia carpeta <stem>_images con las imágenes extraídas.',
  'pdfExtractImages.badge': 'Salida en carpeta',
  'pdfExtractImages.action.idle': 'Extraer imágenes',
  'pdfExtractImages.action.one': 'Extraer imágenes de 1 PDF',
  'pdfExtractImages.action.many': 'Extraer imágenes de {count} PDF',
  'pdfExtractImages.action.busy': 'Extrayendo imágenes…',
  'pdfExtractImages.helper.addOne':
    'Añade al menos un PDF del que extraer imágenes.',
  'pdfExtractImages.result.title': 'Carpetas extraídas',
  'pdfExtractImages.result.sub':
    'La carpeta y la cantidad de imágenes verificadas dentro de ella.',
  'pdfExtractImages.result.aria': 'Carpetas extraídas',
} as const;
