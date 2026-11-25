export const pdfOcrEn = {
  'pdfOcr.page.eyebrow': 'PDF',
  'pdfOcr.page.titleLead': 'PDF',
  'pdfOcr.page.titleTail': 'OCR',
  'pdfOcr.page.sub':
    'Make scanned PDFs searchable with local OCR. Everything runs on this machine.',
  'pdfOcr.page.localBadge': 'Local only',
  'pdfOcr.list.and': 'and',
  'pdfOcr.capability.missing': 'Missing {dependencies}.',
  'pdfOcr.capability.noLanguage':
    'No Tesseract language data is installed; install eng or spa.',
  'pdfOcr.capability.installMissing':
    'Install the OCR dependencies and reload this page.',
  'pdfOcr.capability.installPieces':
    'Install the missing pieces and reload this page.',
  'pdfOcr.capability.title': 'Local OCR is unavailable.',
  'pdfOcr.language.weak':
    'Only {languages} language data is installed. Install eng/spa Tesseract data for better accuracy.',
  'pdfOcr.language.installed': 'Installed: {languages}.',
  'pdfOcr.language.none': 'No Tesseract language data is installed.',
  'pdfOcr.language.noneInstalled': 'No language data installed',
  'pdfOcr.intake.title': 'PDFs',
  'pdfOcr.intake.sub':
    'Drop scanned PDFs or choose them from your computer.',
  'pdfOcr.intake.dropTitle': 'Drop scanned PDFs here',
  'pdfOcr.intake.dropHint': 'PDF files only · OCR runs locally',
  'pdfOcr.intake.dropAria': 'Drop or choose scanned PDFs',
  'pdfOcr.intake.actionsAria': 'PDF selection actions',
  'pdfOcr.intake.choose': 'Choose PDFs',
  'pdfOcr.intake.clear': 'Clear',
  'pdfOcr.notice.rejectedOne':
    "{name} wasn't added because it isn't a PDF.",
  'pdfOcr.notice.rejectedMany':
    "{names} weren't added because they aren't PDFs.",
  'pdfOcr.notice.duplicateOne': '{name} is already in the queue.',
  'pdfOcr.notice.duplicateMany': '{count} PDFs are already in the queue.',
  'pdfOcr.settings.title': 'Settings',
  'pdfOcr.settings.sub':
    'Pick the recognition language and where the output goes.',
  'pdfOcr.settings.language': 'Language',
  'pdfOcr.settings.outputFolder': 'Output folder',
  'pdfOcr.settings.chooseFolder': 'Choose folder',
  'pdfOcr.settings.outputHelp':
    'The searchable PDF is saved next to each source PDF.',
  'pdfOcr.settings.useDefault': 'Use the default folder',
  'pdfOcr.settings.sidecar': 'Text sidecar',
  'pdfOcr.settings.sidecarLabel': 'Write a text file next to the PDF',
  'pdfOcr.settings.sidecarHelp':
    'Writes <name>_ocr.txt next to the searchable PDF.',
  'pdfOcr.settings.dpiNote':
    'Pages are rasterized at 300 DPI. Vector text is replaced by an image with an invisible text layer.',
  'pdfOcr.destination.each': 'Next to each PDF',
  'pdfOcr.helper.addOne': 'Add at least one PDF.',
  'pdfOcr.helper.unavailable': 'Local OCR is unavailable on this machine.',
  'pdfOcr.helper.chooseLanguage': 'Choose a language.',
  'pdfOcr.action.ocr': 'OCR PDFs',
  'pdfOcr.action.ocrOne': 'OCR 1 PDF',
  'pdfOcr.action.ocrMany': 'OCR {count} PDFs',
  'pdfOcr.action.recognizing': 'Making searchable…',
  'pdfOcr.action.cancel': 'Cancel',
  'pdfOcr.progress.aria': 'OCR progress',
  'pdfOcr.progress.page': 'OCR page {current} of {total}',
  'pdfOcr.status.waiting': 'Waiting to start',
  'pdfOcr.error.start': 'The OCR job could not start.',
  'pdfOcr.error.plan': 'The output could not be planned.',
  'pdfOcr.error.cancel': 'The job could not be cancelled.',
  'pdfOcr.error.openFile': 'The file could not be opened.',
  'pdfOcr.error.openFolder': 'The folder could not be opened.',
  'pdfOcr.error.status': 'The job status could not be read.',
  'pdfOcr.error.inspect': 'File sizes could not be read.',
} as const;
