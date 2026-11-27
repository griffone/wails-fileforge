export const automationsEs = {
  'automations.page.titleLead': 'Automatizaciones',
  'automations.page.titleTail': 'locales',
  'automations.page.sub':
    'Vigila una carpeta y ejecuta una receta cada vez que llegue un archivo compatible. Todo se queda en este equipo.',
  'automations.page.localBadge': 'Solo local',
  'automations.rules.title': 'Reglas de carpeta',
  'automations.rules.subtitle':
    'Una ejecución de regla por cada archivo que llega.',
  'automations.rules.new': 'Nueva regla',
  'automations.stats.total': 'Reglas totales',
  'automations.stats.active': 'Reglas activas',
  'automations.stats.lastRun': 'Última ejecución',
  'automations.stats.noRuns': 'Aún no hay ejecuciones',
  'automations.loading.rules': 'Cargando reglas de automatización…',
  'automations.form.titleEdit': 'Editar regla',
  'automations.form.subtitle':
    'Un archivo en la carpeta vigilada ejecuta la regla una vez.',
  'automations.form.badgeEditing': 'Editando',
  'automations.form.badgeDraft': 'Borrador',
  'automations.form.name': 'Nombre',
  'automations.form.namePlaceholder': 'PDF del correo',
  'automations.form.action': 'Acción',
  'automations.form.actionRecipe': 'Procesar con una receta',
  'automations.form.actionOrganize': 'Organizar archivos',
  'automations.form.actionHelp':
    'Procesar ejecuta una receta sobre cada archivo compatible. Organizar mueve los archivos a una carpeta destino sin modificar su contenido.',
  'automations.form.watchFolder': 'Carpeta vigilada',
  'automations.form.noFolder': 'Ninguna carpeta elegida',
  'automations.form.chooseFolder': 'Elegir carpeta',
  'automations.form.watchFolderHelp':
    'Los archivos nuevos de esta carpeta se procesan automáticamente.',
  'automations.form.recursive': 'Incluir subcarpetas',
  'automations.form.recursiveHelp':
    'También vigila las subcarpetas de esta carpeta, incluidas las que se creen después. Sin esta opción, solo se procesan los archivos colocados directamente en la carpeta.',
  'automations.form.recipe': 'Receta',
  'automations.form.chooseRecipe': 'Elige una receta',
  'automations.form.accepts': 'Acepta {extensions}.',
  'automations.form.extensions': 'Extensiones',
  'automations.form.extensionsHelp':
    'Separadas por comas. Déjalo vacío para usar los tipos aceptados por la receta.',
  'automations.form.extensionsHelpOrganize':
    'Separadas por comas. Déjalo vacío para mover todos los archivos de la carpeta vigilada.',
  'automations.form.extensionsPlaceholderOrganize': 'pdf, jpg',
  'automations.form.patterns': 'Patrones de nombre de archivo',
  'automations.form.patternsPlaceholder': 'factura-*.pdf, informe-*.xlsx',
  'automations.form.patternsHelp':
    'Opcional, separados por comas. Comodines de estilo shell que se comparan solo con el nombre del archivo, por ejemplo factura-*.pdf. Las subcarpetas dependen de la opción anterior, nunca del patrón. Si también defines extensiones, se deben cumplir ambas condiciones.',
  'automations.form.outputFolder': 'Carpeta de salida',
  'automations.form.nextToSource': 'Junto al origen',
  'automations.form.useSourceFolder': 'Usar la carpeta de origen',
  'automations.form.outputFolderHelp':
    'Déjalo vacío para guardar junto al origen.',
  'automations.form.targetFolder': 'Carpeta destino',
  'automations.form.targetFolderHelp':
    'Los archivos se mueven aquí. Elige una carpeta fuera de la carpeta vigilada.',
  'automations.form.folderTemplate': 'Plantilla de carpetas',
  'automations.form.folderTemplatePlaceholder': '{year}/{month}',
  'automations.form.folderTemplateHelp':
    'Subcarpetas opcionales dentro de la carpeta destino. Déjalo vacío para escribir directamente en ella.',
  'automations.form.nameTemplate': 'Plantilla de nombre',
  'automations.form.nameTemplatePlaceholder': '{name}-{date}{ext}',
  'automations.form.nameTemplateHelp':
    'Nombre nuevo opcional. Déjalo vacío para conservar el nombre original.',
  'automations.form.collisionPolicy': 'Si el destino ya existe',
  'automations.form.collisionSkip': 'Omitir el archivo',
  'automations.form.collisionSuffix': 'Agregar un sufijo numérico',
  'automations.form.collisionHelp':
    'Nunca se sobrescribe nada. El sufijo nombra el archivo -2, -3 y así sucesivamente.',
  'automations.form.enabled': 'Activada',
  'automations.form.deleteSource': 'Eliminar el origen al completar',
  'automations.form.deleteSourceHelp':
    'Elimina el archivo de origen después de una ejecución correcta. Esta acción no se puede deshacer.',
  'automations.form.saving': 'Guardando…',
  'automations.form.saveChanges': 'Guardar cambios',
  'automations.form.create': 'Crear regla',
  'automations.action.cancel': 'Cancelar',
  'automations.validation.name': 'Dale un nombre a la regla.',
  'automations.validation.watchFolder': 'Elige una carpeta para vigilar.',
  'automations.validation.recipe': 'Elige una receta para ejecutar.',
  'automations.validation.patternsScope':
    'Los patrones solo coinciden con el nombre del archivo. Quita los separadores de carpeta y usa la opción de subcarpetas.',
  'automations.validation.patternInvalid':
    'El patrón «{pattern}» no es válido. Usa comodines de estilo shell, por ejemplo factura-*.pdf.',
  'automations.validation.targetFolder': 'Elige una carpeta destino.',
  'automations.validation.templateUnknownToken':
    'La plantilla usa el token desconocido «{token}». Tokens disponibles: {tokens}.',
  'automations.validation.templateUnclosedBrace':
    'La plantilla tiene una «{» sin su «}» correspondiente.',
  'automations.validation.templateStrayBrace':
    'La plantilla tiene una «}» sin su «{» correspondiente.',
  'automations.validation.folderTemplateAbsolute':
    'La plantilla de carpetas debe ser relativa a la carpeta destino, no absoluta.',
  'automations.validation.folderTemplateEmptySegment':
    'La plantilla de carpetas tiene un segmento vacío. Quita el separador repetido o final.',
  'automations.validation.folderTemplateDotSegment':
    'La plantilla de carpetas no puede contener segmentos «.» ni «..».',
  'automations.validation.folderTemplateColon':
    'La plantilla de carpetas no puede contener «:».',
  'automations.validation.nameTemplateSeparator':
    'La plantilla de nombre no puede contener un separador de carpetas. Usa la plantilla de carpetas para crear carpetas.',
  'automations.validation.nameTemplateDot':
    'La plantilla de nombre no puede ser «.» ni «..».',
  'automations.empty.title': 'Aún no hay reglas de carpeta',
  'automations.empty.body':
    'Vigila una carpeta y FileForge ejecutará la receta cada vez que llegue un archivo. Nada sale de este equipo.',
  'automations.empty.action': 'Crea tu primera regla',
  'automations.rule.enable': 'Activar',
  'automations.rule.disable': 'Desactivar',
  'automations.rule.enableAria': 'Activar {name}',
  'automations.rule.disableAria': 'Desactivar {name}',
  'automations.rule.editAria': 'Editar {name}',
  'automations.rule.deleteAria': 'Eliminar {name}',
  'automations.rule.recipeDefaults': 'Valores de la receta',
  'automations.rule.actionProcess': 'Procesar',
  'automations.rule.actionOrganize': 'Organizar',
  'automations.rule.previewNeeded': 'Falta la vista previa',
  'automations.rule.subfoldersIncluded': 'Incluidas',
  'automations.rule.never': 'Nunca',
  'automations.rule.lastOutputs': 'Últimos resultados',
  'automations.runtime.watching': 'Vigilando',
  'automations.runtime.degraded': 'Degradado',
  'automations.runtime.stopped': 'Detenido',
  'automations.status.notRun': 'Sin ejecutar',
  'automations.confirm.delete': '¿Eliminar «{name}»?',
  'automations.confirm.deleteAction': 'Confirmar eliminación',
  'automations.runs.title': 'Ejecuciones recientes',
  'automations.runs.clearGroupAria':
    'Confirmar borrado del historial de ejecuciones',
  'automations.runs.clear': 'Borrar historial',
  'automations.runs.confirmClear': 'Confirmar borrado',
  'automations.runs.loading': 'Cargando ejecuciones recientes…',
  'automations.runs.empty': 'Aún no hay ejecuciones de automatización.',
  'automations.runs.listAria': 'Ejecuciones de automatización recientes',
  'automations.runs.unknownFile': 'Archivo desconocido',
  'automations.runs.organizeRule': 'Organizar archivos',
  'automations.organize.previewTitle': 'Vista previa',
  'automations.organize.preview': 'Ver movimientos',
  'automations.organize.previewing': 'Calculando…',
  'automations.organize.previewHelp':
    'Enumera cada archivo que la regla movería ahora mismo. No se mueve ni se crea nada.',
  'automations.organize.previewStored':
    'Vista previa guardada. La regla ya se puede activar.',
  'automations.organize.previewNotStored':
    'Esta vista previa corresponde a cambios sin guardar. Guarda la regla y vuelve a generar la vista previa para activarla.',
  'automations.organize.previewStale':
    'La configuración cambió después de la última vista previa. Vuelve a generarla para ver el plan actual.',
  'automations.organize.previewEmpty':
    'Ningún archivo coincide con esta regla en este momento.',
  'automations.organize.previewListAria': 'Movimientos previstos',
  'automations.organize.totalMoves': '{count} movimientos previstos',
  'automations.organize.totalSkips': '{count} omitidos',
  'automations.organize.totalSize': '{size} por mover',
  'automations.organize.showAll': 'Ver los {count}',
  'automations.organize.showLess': 'Ver menos',
  'automations.organize.actionMove': 'Mover',
  'automations.organize.actionSuffix': 'Sufijo',
  'automations.organize.actionSkip': 'Omitir',
  'automations.organize.tokensTitle': 'Tokens disponibles',
  'automations.organize.token.name': 'Nombre del archivo sin su extensión.',
  'automations.organize.token.ext': 'Extensión con el punto, por ejemplo .pdf.',
  'automations.organize.token.year': 'Año de la fecha del archivo, con cuatro dígitos.',
  'automations.organize.token.month': 'Mes con dos dígitos, de 01 a 12.',
  'automations.organize.token.date': 'Fecha del archivo como AAAA-MM-DD.',
  'automations.organize.token.title':
    'Título del documento cuando el formato lo tiene; si no, el nombre del archivo.',
  'automations.organize.allFiles': 'Todos los archivos',
  'automations.organize.noFolderTemplate':
    'Directamente en la carpeta destino',
  'automations.organize.keepName': 'Conservar el nombre original',
  'automations.organize.collisionSkip': 'Omitir si ya existe',
  'automations.organize.collisionSuffix': 'Agregar sufijo si ya existe',
  'automations.organize.gatePreview':
    'Genera una vista previa de la configuración actual para activar esta regla.',
  'automations.organize.gateChanged':
    'La configuración cambió después de la última vista previa. Vuelve a generarla para activar esta regla.',
  'automations.organize.gateSaveFirst':
    'Guarda esta regla una vez y luego genera la vista previa de la configuración guardada para activarla.',
  'automations.organize.gateNotStored':
    'Esta vista previa corresponde a cambios sin guardar. Guarda la regla y vuelve a generarla para activarla.',
  'automations.organize.gateCard':
    'Genera una vista previa de la configuración actual antes de activar esta regla.',
  'automations.error.recipes': 'No se pudo leer el catálogo de recetas.',
  'automations.error.rules':
    'No se pudieron cargar las reglas de automatización.',
  'automations.error.status':
    'No se pudo leer el estado de las automatizaciones.',
  'automations.error.runs':
    'No se pudo cargar el historial de ejecuciones de automatización.',
  'automations.error.rulesUnavailable':
    'No se pudieron cargar las reglas de automatización. Abre la aplicación de escritorio para gestionar las reglas de carpeta.',
  'automations.error.runsUnavailable':
    'No se pudo cargar el historial de ejecuciones de automatización. Abre la aplicación de escritorio para consultar las ejecuciones anteriores.',
  'automations.error.save': 'No se pudo guardar la regla.',
  'automations.error.update': 'No se pudo actualizar la regla.',
  'automations.error.delete': 'No se pudo eliminar la regla.',
  'automations.error.clearRuns':
    'No se pudo borrar el historial de ejecuciones.',
  'automations.error.preview': 'No se pudo completar la vista previa.',
  'automations.success.saved': 'Regla de automatización guardada.',
  'automations.success.enabled': 'Regla de automatización activada.',
  'automations.success.disabled': 'Regla de automatización desactivada.',
  'automations.success.deleted': 'Regla de automatización eliminada.',
  'automations.success.runsCleared':
    'Historial de ejecuciones de automatización borrado.',
  'automations.success.previewStored':
    'Vista previa lista. La regla ya se puede activar.',
};
