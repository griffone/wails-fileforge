export const automationsEn = {
  'automations.page.titleLead': 'Local',
  'automations.page.titleTail': 'Automations',
  'automations.page.sub':
    'Watch a folder and run a recipe whenever a matching file arrives. Everything stays on this machine.',
  'automations.page.localBadge': 'Local only',
  'automations.rules.title': 'Folder rules',
  'automations.rules.subtitle': 'One rule run per arriving file.',
  'automations.rules.new': 'New rule',
  'automations.stats.total': 'Total rules',
  'automations.stats.active': 'Active rules',
  'automations.stats.lastRun': 'Last run',
  'automations.stats.noRuns': 'No runs yet',
  'automations.loading.rules': 'Loading automation rules…',
  'automations.form.titleEdit': 'Edit rule',
  'automations.form.subtitle': 'A file in the watch folder runs the rule once.',
  'automations.form.badgeEditing': 'Editing',
  'automations.form.badgeDraft': 'Draft',
  'automations.form.name': 'Name',
  'automations.form.namePlaceholder': 'Inbox PDFs',
  'automations.form.action': 'Action',
  'automations.form.actionRecipe': 'Process with a recipe',
  'automations.form.actionOrganize': 'Organize files',
  'automations.form.actionHelp':
    'Process runs a recipe on every matching file. Organize moves the files into a target folder, without touching their contents.',
  'automations.form.watchFolder': 'Watch folder',
  'automations.form.noFolder': 'No folder chosen',
  'automations.form.chooseFolder': 'Choose folder',
  'automations.form.watchFolderHelp':
    'New files in this folder are processed automatically.',
  'automations.form.recursive': 'Include subfolders',
  'automations.form.recursiveHelp':
    'Also watch the subfolders of this folder, including ones created later. Without this, only files placed directly in the folder are processed.',
  'automations.form.recipe': 'Recipe',
  'automations.form.chooseRecipe': 'Choose a recipe',
  'automations.form.accepts': 'Accepts {extensions}.',
  'automations.form.extensions': 'Extensions',
  'automations.form.extensionsHelp':
    "Comma-separated. Leave empty to use the recipe's accepted types.",
  'automations.form.extensionsHelpOrganize':
    'Comma-separated. Leave empty to move every file in the watch folder.',
  'automations.form.extensionsPlaceholderOrganize': 'pdf, jpg',
  'automations.form.patterns': 'File name patterns',
  'automations.form.patternsPlaceholder': 'invoice-*.pdf, report-*.xlsx',
  'automations.form.patternsHelp':
    'Optional, comma-separated. Shell-style globs matched against the file name only, for example invoice-*.pdf. Subfolders come from the toggle above, never from the pattern. When extensions are also set, both must match.',
  'automations.form.outputFolder': 'Output folder',
  'automations.form.nextToSource': 'Next to the source',
  'automations.form.useSourceFolder': 'Use the source folder',
  'automations.form.outputFolderHelp':
    'Leave empty to save next to the source.',
  'automations.form.targetFolder': 'Target folder',
  'automations.form.targetFolderHelp':
    'Files are moved here. Choose a folder outside the watch folder.',
  'automations.form.folderTemplate': 'Folder template',
  'automations.form.folderTemplatePlaceholder': '{year}/{month}',
  'automations.form.folderTemplateHelp':
    'Optional subfolders inside the target folder. Leave empty to write directly into it.',
  'automations.form.nameTemplate': 'Name template',
  'automations.form.nameTemplatePlaceholder': '{name}-{date}{ext}',
  'automations.form.nameTemplateHelp':
    'Optional new name. Leave empty to keep the original file name.',
  'automations.form.collisionPolicy': 'If the destination exists',
  'automations.form.collisionSkip': 'Skip the file',
  'automations.form.collisionSuffix': 'Add a numeric suffix',
  'automations.form.collisionHelp':
    'Nothing is ever overwritten. A suffix names the file -2, -3 and so on.',
  'automations.form.enabled': 'Enabled',
  'automations.form.deleteSource': 'Delete source after success',
  'automations.form.deleteSourceHelp':
    'Deletes the source file after a successful run. This cannot be undone.',
  'automations.form.saving': 'Saving…',
  'automations.form.saveChanges': 'Save changes',
  'automations.form.create': 'Create rule',
  'automations.action.cancel': 'Cancel',
  'automations.validation.name': 'Give the rule a name.',
  'automations.validation.watchFolder': 'Choose a folder to watch.',
  'automations.validation.recipe': 'Choose a recipe to run.',
  'automations.validation.patternsScope':
    'Patterns match the file name only. Remove folder separators and use the subfolder toggle instead.',
  'automations.validation.patternInvalid':
    'The pattern “{pattern}” is not valid. Use shell-style globs such as invoice-*.pdf.',
  'automations.validation.targetFolder': 'Choose a target folder.',
  'automations.validation.templateUnknownToken':
    'The template uses the unknown token “{token}”. Available tokens: {tokens}.',
  'automations.validation.templateUnclosedBrace':
    'The template has a “{” without a matching “}”.',
  'automations.validation.templateStrayBrace':
    'The template has a “}” without a matching “{”.',
  'automations.validation.folderTemplateAbsolute':
    'The folder template must be relative to the target folder, not absolute.',
  'automations.validation.folderTemplateEmptySegment':
    'The folder template has an empty folder segment. Remove the repeated or trailing separator.',
  'automations.validation.folderTemplateDotSegment':
    'The folder template cannot contain “.” or “..” segments.',
  'automations.validation.folderTemplateColon':
    'The folder template cannot contain “:”.',
  'automations.validation.nameTemplateSeparator':
    'The name template cannot contain a folder separator. Build folders with the folder template.',
  'automations.validation.nameTemplateDot':
    'The name template cannot be “.” or “..”.',
  'automations.empty.title': 'No folder rules yet',
  'automations.empty.body':
    'Watch a folder and FileForge runs the recipe whenever a file lands in it. Nothing leaves this machine.',
  'automations.empty.action': 'Create your first rule',
  'automations.rule.enable': 'Enable',
  'automations.rule.disable': 'Disable',
  'automations.rule.enableAria': 'Enable {name}',
  'automations.rule.disableAria': 'Disable {name}',
  'automations.rule.editAria': 'Edit {name}',
  'automations.rule.deleteAria': 'Delete {name}',
  'automations.rule.recipeDefaults': 'Recipe defaults',
  'automations.rule.actionProcess': 'Process',
  'automations.rule.actionOrganize': 'Organize',
  'automations.rule.previewNeeded': 'Preview needed',
  'automations.rule.subfoldersIncluded': 'Included',
  'automations.rule.never': 'Never',
  'automations.rule.lastOutputs': 'Last outputs',
  'automations.runtime.watching': 'Watching',
  'automations.runtime.degraded': 'Degraded',
  'automations.runtime.stopped': 'Stopped',
  'automations.status.notRun': 'Not run',
  'automations.confirm.delete': 'Delete “{name}”?',
  'automations.confirm.deleteAction': 'Confirm delete',
  'automations.runs.title': 'Recent runs',
  'automations.runs.clearGroupAria': 'Confirm clear run history',
  'automations.runs.clear': 'Clear history',
  'automations.runs.confirmClear': 'Confirm clear',
  'automations.runs.loading': 'Loading recent runs…',
  'automations.runs.empty': 'No automation runs yet.',
  'automations.runs.listAria': 'Recent automation runs',
  'automations.runs.unknownFile': 'Unknown file',
  'automations.runs.organizeRule': 'Organize files',
  'automations.organize.previewTitle': 'Preview',
  'automations.organize.preview': 'Preview moves',
  'automations.organize.previewing': 'Previewing…',
  'automations.organize.previewHelp':
    'Lists every file the rule would move right now. Nothing is moved or created.',
  'automations.organize.previewStored':
    'Preview stored. The rule can be enabled now.',
  'automations.organize.previewNotStored':
    'This preview covers unsaved settings. Save the rule, then preview it again to enable it.',
  'automations.organize.previewStale':
    'The settings changed after the last preview. Preview again to see the current plan.',
  'automations.organize.previewEmpty': 'No file matches this rule right now.',
  'automations.organize.previewListAria': 'Planned organize moves',
  'automations.organize.totalMoves': '{count} moves planned',
  'automations.organize.totalSkips': '{count} skipped',
  'automations.organize.totalSize': '{size} to move',
  'automations.organize.showAll': 'Show all {count}',
  'automations.organize.showLess': 'Show fewer',
  'automations.organize.actionMove': 'Move',
  'automations.organize.actionSuffix': 'Suffix',
  'automations.organize.actionSkip': 'Skip',
  'automations.organize.tokensTitle': 'Available tokens',
  'automations.organize.token.name': 'File name without its extension.',
  'automations.organize.token.ext': 'Extension with the dot, such as .pdf.',
  'automations.organize.token.year': 'Four-digit year of the file date.',
  'automations.organize.token.month': 'Two-digit month, from 01 to 12.',
  'automations.organize.token.date': 'File date as YYYY-MM-DD.',
  'automations.organize.token.title':
    'Document title when the format has one, otherwise the file name.',
  'automations.organize.allFiles': 'All files',
  'automations.organize.noFolderTemplate': 'Directly in the target folder',
  'automations.organize.keepName': 'Keep the original name',
  'automations.organize.collisionSkip': 'Skip on collision',
  'automations.organize.collisionSuffix': 'Suffix on collision',
  'automations.organize.gatePreview':
    'Preview the current settings to enable this rule.',
  'automations.organize.gateChanged':
    'The settings changed after the last preview. Preview again to enable this rule.',
  'automations.organize.gateSaveFirst':
    'Save this rule once, then preview the saved settings to enable it.',
  'automations.organize.gateNotStored':
    'This preview covers unsaved settings. Save the rule, then preview it again to enable it.',
  'automations.organize.gateCard':
    'Run a preview of the current settings before enabling this rule.',
  'automations.error.recipes': 'The recipe catalog could not be read.',
  'automations.error.rules': 'The automation rules could not be loaded.',
  'automations.error.status': 'The automation status could not be read.',
  'automations.error.runs': 'The automation run history could not be loaded.',
  'automations.error.rulesUnavailable':
    'The automation rules could not be loaded. Open the desktop app to manage folder rules.',
  'automations.error.runsUnavailable':
    'The automation run history could not be loaded. Open the desktop app to inspect past runs.',
  'automations.error.save': 'The rule could not be saved.',
  'automations.error.update': 'The rule could not be updated.',
  'automations.error.delete': 'The rule could not be deleted.',
  'automations.error.clearRuns': 'The run history could not be cleared.',
  'automations.error.preview': 'The preview could not be completed.',
  'automations.success.saved': 'Automation rule saved.',
  'automations.success.enabled': 'Automation rule enabled.',
  'automations.success.disabled': 'Automation rule disabled.',
  'automations.success.deleted': 'Automation rule deleted.',
  'automations.success.runsCleared': 'Automation run history cleared.',
  'automations.success.previewStored':
    'Preview ready. The rule can be enabled now.',
} as const;
