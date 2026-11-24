export const imageOcrEn = {
  'imageOcr.page.eyebrow': 'IMAGE',
  'imageOcr.page.titleLead': 'Image',
  'imageOcr.page.titleTail': 'OCR',
  'imageOcr.page.sub':
    'Extract text from screenshots and photos with local Tesseract. Write a plain text file, or add a searchable PDF for each image.',
  'imageOcr.page.localBadge': 'Local only',
  'imageOcr.capability.missing': 'Missing {dependencies}.',
  'imageOcr.capability.noLanguage':
    'No Tesseract language data is installed; install eng or spa.',
  'imageOcr.capability.installMissing':
    'Install the missing OCR dependencies and reload this page.',
  'imageOcr.capability.installPieces':
    'Install the missing pieces and reload this page.',
  'imageOcr.capability.title': 'Local image OCR is unavailable.',
  'imageOcr.language.installed': 'Installed: {languages}.',
  'imageOcr.language.none':
    'No Tesseract language data is installed.',
  'imageOcr.list.and': 'and',
  'imageOcr.intake.title': 'Images',
  'imageOcr.intake.sub': 'Drop images or choose them from your computer.',
  'imageOcr.intake.dropTitle': 'Drop images here',
  'imageOcr.intake.dropAria':
    'Drop or choose images to extract text from',
  'imageOcr.intake.actionsAria': 'Image selection actions',
  'imageOcr.intake.choose': 'Choose files',
  'imageOcr.intake.clear': 'Clear',
  'imageOcr.intake.batchNote':
    'The same language and output settings apply to every image.',
  'imageOcr.notice.filesOne': "1 file wasn't added.",
  'imageOcr.notice.filesMany': "{count} files weren't added.",
  'imageOcr.notice.extensionOne': "{extension} isn't supported.",
  'imageOcr.notice.extensionsMany':
    "{head} and {tail} aren't supported.",
  'imageOcr.output.title': 'Output',
  'imageOcr.output.sub':
    'Pick the language and where the text files go.',
  'imageOcr.field.language': 'Language',
  'imageOcr.language.noneInstalled': 'No language data installed',
  'imageOcr.field.searchablePdf': 'Searchable PDF',
  'imageOcr.field.writePdf': 'Also write a searchable PDF',
  'imageOcr.field.writePdfHelp':
    'Adds <name>_ocr.pdf next to the <name>.txt file.',
  'imageOcr.advanced.title': 'Advanced',
  'imageOcr.advanced.hint': 'Recognition resolution',
  'imageOcr.field.dpi': 'DPI',
  'imageOcr.field.dpiHelp':
    '150–600. Higher DPI helps small text but takes longer. Default 300.',
  'imageOcr.advanced.reset': 'Reset advanced settings',
  'imageOcr.note.title': 'Text is always saved',
  'imageOcr.note.body':
    'Each image gets a <name>.txt file. A blank result is reported as a warning, not as a success.',
  'imageOcr.helper.addImage': 'Add at least one image.',
  'imageOcr.helper.unavailable':
    'Local image OCR is unavailable on this machine.',
  'imageOcr.helper.chooseLanguage': 'Choose a language.',
  'imageOcr.helper.dpi': 'Enter a DPI between {min} and {max}.',
  'imageOcr.action.ocr': 'OCR images',
  'imageOcr.action.ocrOne': 'OCR 1 image',
  'imageOcr.action.ocrMany': 'OCR {count} images',
  'imageOcr.action.recognizing': 'Recognizing…',
  'imageOcr.action.cancel': 'Cancel',
  'imageOcr.destination.original': 'Next to each image',
  'imageOcr.progress.aria': 'OCR progress',
  'imageOcr.status.waiting': 'Waiting to start',
  'imageOcr.error.start': 'The OCR job could not start.',
  'imageOcr.error.plan': 'The output could not be planned.',
  'imageOcr.error.cancel': 'The job could not be cancelled.',
  'imageOcr.error.openFile': 'The file could not be opened.',
  'imageOcr.error.openFolder': 'The folder could not be opened.',
  'imageOcr.error.status': 'The job status could not be read.',
  'imageOcr.error.inspect': 'File sizes could not be read.',
} as const;
