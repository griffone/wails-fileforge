export const automationsEs = {
  'automations.page.titleLead': 'Automatizaciones',
  'automations.page.titleTail': 'locales',
  'automations.page.sub':
    'Vigila una carpeta y ejecuta una receta cada vez que llegue un archivo compatible. Todo se queda en este equipo.',
  'automations.page.localBadge': 'Solo local',
  'automations.rules.title': 'Reglas de carpeta',
  'automations.rules.subtitle':
    'Una ejecución de receta por cada archivo que llega.',
  'automations.rules.new': 'Nueva regla',
  'automations.stats.total': 'Reglas totales',
  'automations.stats.active': 'Reglas activas',
  'automations.stats.lastRun': 'Última ejecución',
  'automations.stats.noRuns': 'Aún no hay ejecuciones',
  'automations.loading.rules': 'Cargando reglas de automatización…',
  'automations.form.titleEdit': 'Editar regla',
  'automations.form.subtitle':
    'Un archivo en la carpeta vigilada ejecuta la receta una vez.',
  'automations.form.badgeEditing': 'Editando',
  'automations.form.badgeDraft': 'Borrador',
  'automations.form.name': 'Nombre',
  'automations.form.namePlaceholder': 'PDF del correo',
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
  'automations.form.patterns': 'Patrones de nombre de archivo',
  'automations.form.patternsPlaceholder': 'factura-*.pdf, informe-*.xlsx',
  'automations.form.patternsHelp':
    'Opcional, separados por comas. Comodines de estilo shell que se comparan solo con el nombre del archivo, por ejemplo factura-*.pdf. Las subcarpetas dependen de la opción anterior, nunca del patrón. Si también defines extensiones, se deben cumplir ambas condiciones.',
  'automations.form.outputFolder': 'Carpeta de salida',
  'automations.form.nextToSource': 'Junto al origen',
  'automations.form.useSourceFolder': 'Usar la carpeta de origen',
  'automations.form.outputFolderHelp':
    'Déjalo vacío para guardar junto al origen.',
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
  'automations.success.saved': 'Regla de automatización guardada.',
  'automations.success.enabled': 'Regla de automatización activada.',
  'automations.success.disabled': 'Regla de automatización desactivada.',
  'automations.success.deleted': 'Regla de automatización eliminada.',
  'automations.success.runsCleared':
    'Historial de ejecuciones de automatización borrado.',
};
