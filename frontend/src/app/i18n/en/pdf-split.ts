export const pdfSplitEn = {
  'pdfSplit.page.eyebrow': 'PDF',
  'pdfSplit.page.titleLead': 'PDF',
  'pdfSplit.page.titleTail': 'Split',
  'pdfSplit.page.sub':
    'Split PDFs by page, explicit ranges, picked pages, page count or bookmarks. Everything runs on this machine.',
  'pdfSplit.page.localBadge': 'Local only',
  'pdfSplit.intake.title': 'PDFs',
  'pdfSplit.intake.sub': 'Drop PDFs or choose them from your computer.',
  'pdfSplit.intake.dropTitle': 'Drop PDFs here',
  'pdfSplit.intake.dropHint': 'PDF files only',
  'pdfSplit.intake.dropAria': 'Drop or choose PDFs to split',
  'pdfSplit.intake.actionsAria': 'PDF selection actions',
  'pdfSplit.intake.choose': 'Choose PDFs',
  'pdfSplit.intake.clear': 'Clear',
  'pdfSplit.notice.rejectedOne':
    "{name} wasn't added because it isn't a PDF.",
  'pdfSplit.notice.rejectedMany':
    "{names} weren't added because they aren't PDFs.",
  'pdfSplit.notice.duplicateOne': '{name} is already in the queue.',
  'pdfSplit.notice.duplicateMany': '{count} PDFs are already in the queue.',
  'pdfSplit.output.title': 'Output',
  'pdfSplit.output.sub':
    'Choose how to split and check where the files will go.',
  'pdfSplit.strategy.aria': 'Split strategy',
  'pdfSplit.strategy.every_page.label': 'Every page',
  'pdfSplit.strategy.every_page.hint': 'One PDF for each page.',
  'pdfSplit.strategy.ranges.label': 'Page ranges',
  'pdfSplit.strategy.ranges.hint': 'Split into the ranges you type.',
  'pdfSplit.strategy.selected.label': 'Selected pages',
  'pdfSplit.strategy.selected.hint':
    'Pick pages; consecutive ones group together',
  'pdfSplit.strategy.pages_per_file.label': 'Pages per file',
  'pdfSplit.strategy.pages_per_file.hint': 'Split every N pages.',
  'pdfSplit.strategy.bookmarks.label': 'Bookmarks',
  'pdfSplit.strategy.bookmarks.hint': 'One file per top-level bookmark.',
  'pdfSplit.ranges.label': 'Page ranges',
  'pdfSplit.ranges.placeholder': '1-3,5,8-10',
  'pdfSplit.ranges.help':
    'Use page numbers and ranges separated by commas. Duplicates and overlaps are not allowed.',
  'pdfSplit.selected.help':
    'Pick pages in the preview below. Consecutive pages become one part.',
  'pdfSplit.selected.batch':
    'The same pages apply to every PDF in the batch.',
  'pdfSplit.pagesPerFile.label': 'Pages per file',
  'pdfSplit.pagesPerFile.help':
    'Split every {count} pages into one file. The last file may hold fewer pages.',
  'pdfSplit.pagesPerFile.error': 'Enter a whole number from 1 to 1000.',
  'pdfSplit.bookmarks.help':
    "Uses the PDF's top-level outline. If it has no bookmarks, the plan will explain what to do.",
  'pdfSplit.unit.partOne': 'part',
  'pdfSplit.unit.partMany': 'parts',
  'pdfSplit.destination.each': 'Next to each PDF',
  'pdfSplit.helper.addOne': 'Add at least one PDF to split.',
  'pdfSplit.helper.ranges': 'Enter the page ranges to split.',
  'pdfSplit.helper.selection':
    'Select at least one page to build the split.',
  'pdfSplit.helper.pagesPerFile':
    'Enter a whole number of pages from 1 to 1000.',
  'pdfSplit.action.split': 'Split PDFs',
  'pdfSplit.action.splitOne': 'Split 1 PDF',
  'pdfSplit.action.splitMany': 'Split {count} PDFs',
  'pdfSplit.action.splitInto': '{base} into {count}',
  'pdfSplit.action.splitting': 'Splitting…',
  'pdfSplit.action.cancel': 'Cancel',
  'pdfSplit.preview.panelTitle': 'Resulting files',
  'pdfSplit.preview.subSelected':
    'Pick pages; consecutive pages become one file.',
  'pdfSplit.preview.subBookmarks':
    'Bookmark output is listed in the plan above.',
  'pdfSplit.preview.subDefault':
    'Preview every file the split will create.',
  'pdfSplit.preview.loading': 'Loading PDF preview…',
  'pdfSplit.preview.rendering': 'Rendering page thumbnails…',
  'pdfSplit.preview.unavailable':
    'Preview unavailable for this PDF. The split will still work.',
  'pdfSplit.preview.unavailableSelectable':
    'Switch to Page ranges to split it without a preview.',
  'pdfSplit.preview.fileLabel': 'Previewing',
  'pdfSplit.preview.selectAll': 'Select all',
  'pdfSplit.preview.clear': 'Clear',
  'pdfSplit.preview.selectionOne': '{count} selected',
  'pdfSplit.preview.selectionMany': '{count} selected',
  'pdfSplit.preview.selectPage': 'Select page {page}',
  'pdfSplit.preview.deselectPage': 'Deselect page {page}',
  'pdfSplit.preview.pageAlt': 'Page {page} preview',
  'pdfSplit.preview.pageNumber': 'Page {page}',
  'pdfSplit.preview.selectAria': 'Select pages to split',
  'pdfSplit.preview.pagesAria': 'Resulting pages',
  'pdfSplit.preview.partsAria': 'Resulting parts',
  'pdfSplit.preview.partPage': 'Part {part} · Page {page}',
  'pdfSplit.preview.partPages': 'Part {part} · Pages {start}-{end}',
  'pdfSplit.preview.bookmarkHint':
    'Bookmark preview appears after planning. The plan lists each resulting file.',
  'pdfSplit.preview.selectHint':
    'Select pages above to preview the parts they create.',
  'pdfSplit.preview.limit': 'Showing first {shown} of {total} pages',
  'pdfSplit.preview.showAll': 'Show all',
  'pdfSplit.preview.error.emptyRanges':
    'Enter page ranges to preview the resulting parts.',
  'pdfSplit.preview.error.invalidSyntax':
    'Use page numbers and ranges separated by commas, like 1-3,5,8-10.',
  'pdfSplit.preview.error.invalidRange':
    'Each range must start at a page lower than or equal to its end.',
  'pdfSplit.preview.error.outside':
    'Page {page} is outside this PDF ({total} pages).',
  'pdfSplit.preview.error.duplicate':
    'Page {page} appears more than once. Remove duplicate or overlapping ranges.',
  'pdfSplit.progress.aria': 'Split progress',
  'pdfSplit.status.waiting': 'Waiting to start',
  'pdfSplit.error.start': 'The split could not start.',
  'pdfSplit.error.plan': 'The output could not be planned.',
  'pdfSplit.error.cancel': 'The job could not be cancelled.',
  'pdfSplit.error.openFile': 'The file could not be opened.',
  'pdfSplit.error.openFolder': 'The folder could not be opened.',
  'pdfSplit.error.status': 'The job status could not be read.',
  'pdfSplit.error.inspect': 'File sizes could not be read.',
} as const;
