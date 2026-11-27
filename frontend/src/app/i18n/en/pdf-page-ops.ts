/**
 * PDF page operations: rotate, remove pages, reorder and extract images.
 *
 * The client-side validation copy mirrors the reasons in
 * `internal/pdf/engine/page_ops.go`; it explains the same rule instead of
 * letting the plan fail.
 */
export const pdfPageOpsEn = {
  'pdfPageOps.page.localBadge': 'Local only',
  'pdfPageOps.intake.title': 'PDFs',
  'pdfPageOps.intake.sub': 'Drop PDFs or choose them from your computer.',
  'pdfPageOps.intake.dropTitle': 'Drop PDFs here',
  'pdfPageOps.intake.dropHint': 'PDF files only',
  'pdfPageOps.intake.dropAria': 'Drop or choose PDFs',
  'pdfPageOps.intake.actionsAria': 'PDF selection actions',
  'pdfPageOps.intake.choose': 'Choose PDFs',
  'pdfPageOps.intake.clear': 'Clear',
  'pdfPageOps.notice.rejectedOne':
    "{name} wasn't added because it isn't a PDF.",
  'pdfPageOps.notice.rejectedMany':
    "{names} weren't added because they aren't PDFs.",
  'pdfPageOps.notice.duplicateOne': '{name} is already in the queue.',
  'pdfPageOps.notice.duplicateMany': '{count} PDFs are already in the queue.',
  'pdfPageOps.output.title': 'Output',
  'pdfPageOps.output.useOriginalFolders': 'Use the original folders',
  'pdfPageOps.destination.each': 'Next to each PDF',
  'pdfPageOps.badge.angle': '{angle}°',
  'pdfPageOps.badge.summary': '{value} · {detail}',
  'pdfPageOps.pages.label': 'Pages',
  'pdfPageOps.pages.placeholder': '1-3,5,8-10',
  'pdfPageOps.pages.help': 'Leave the field empty to apply to every page.',
  'pdfPageOps.pages.error.required':
    'Enter the pages, for example 1-3,5.',
  'pdfPageOps.pages.error.emptyEntry':
    'The page selection has an empty entry. Remove the extra comma.',
  'pdfPageOps.pages.error.token': '"{token}" is not a page. Pages start at 1.',
  'pdfPageOps.pages.error.range':
    '"{token}" is not a valid range. Use start-end, such as 4-6.',
  'pdfPageOps.pages.error.rangeOrder':
    'Write the range "{token}" in ascending order, such as 4-6.',
  'pdfPageOps.pages.error.overlap':
    'Pages {first} and {second} overlap. List each page once.',
  'pdfPageOps.pages.error.outOfBounds':
    'The selection exceeds the {count}-page document. Choose pages up to {count}.',
  'pdfPageOps.pages.error.allPages':
    'Refusing to remove every page: the selection covers all {count} pages. Keep at least one page.',
  'pdfPageOps.order.error.required':
    'Enter the page order, for example 3,1,2,4-6, or use Reverse.',
  'pdfPageOps.order.error.emptyEntry':
    'The order has an empty entry. Remove the extra comma.',
  'pdfPageOps.order.error.token': '"{token}" is not a page. Pages start at 1.',
  'pdfPageOps.order.error.range':
    '"{token}" is not a valid range. Use start-end, such as 4-6.',
  'pdfPageOps.order.error.rangeOrder':
    'Write the range "{token}" in ascending order, such as 4-6.',
  'pdfPageOps.order.error.outOfBounds':
    'Page {page} does not exist. The PDF has {count} pages.',
  'pdfPageOps.order.error.missing':
    'The order must list every page exactly once. Missing pages: {missing}.',
  'pdfPageOps.order.error.duplicates':
    'The order must list every page exactly once. Repeated pages: {duplicates}.',
  'pdfPageOps.order.error.missingAndDuplicates':
    'The order must list every page exactly once. Missing pages: {missing}. Repeated pages: {duplicates}.',
  'pdfPageOps.action.cancel': 'Cancel',
  'pdfPageOps.progress.aria': 'Page operation progress',
  'pdfPageOps.status.waiting': 'Waiting to start',
  'pdfPageOps.error.start': 'The page operation could not start.',
  'pdfPageOps.error.plan': 'The output could not be planned.',
  'pdfPageOps.error.cancel': 'The job could not be cancelled.',
  'pdfPageOps.error.openFile': 'The file could not be opened.',
  'pdfPageOps.error.openFolder': 'The folder could not be opened.',
  'pdfPageOps.error.status': 'The job status could not be read.',
  'pdfPageOps.error.inspect': 'File sizes could not be read.',

  'pdfRotate.page.eyebrow': 'PDF',
  'pdfRotate.page.titleLead': 'Rotate',
  'pdfRotate.page.titleTail': 'PDF',
  'pdfRotate.page.sub':
    'Turn every page, or only the pages you pick, clockwise by 90, 180 or 270 degrees. Everything runs on this machine.',
  'pdfRotate.output.sub':
    'Pick the angle and the pages, then check where the file will go.',
  'pdfRotate.angle.label': 'Angle',
  'pdfRotate.angle.90.label': '90°',
  'pdfRotate.angle.90.hint': 'Clockwise quarter turn',
  'pdfRotate.angle.180.label': '180°',
  'pdfRotate.angle.180.hint': 'Upside down',
  'pdfRotate.angle.270.label': '270°',
  'pdfRotate.angle.270.hint': 'Three quarters clockwise',
  'pdfRotate.badge.pagesAll': 'All pages',
  'pdfRotate.badge.pagesSelected': 'Pages {pages}',
  'pdfRotate.action.idle': 'Rotate PDF',
  'pdfRotate.action.one': 'Rotate 1 PDF',
  'pdfRotate.action.many': 'Rotate {count} PDFs',
  'pdfRotate.action.busy': 'Rotating…',
  'pdfRotate.helper.addOne': 'Add at least one PDF to rotate.',

  'pdfRemovePages.page.eyebrow': 'PDF',
  'pdfRemovePages.page.titleLead': 'Remove',
  'pdfRemovePages.page.titleTail': 'pages',
  'pdfRemovePages.page.sub':
    'Delete the pages you list and keep the rest of the document. Everything runs on this machine.',
  'pdfRemovePages.output.sub':
    'Pick the pages to delete, then check where the file will go.',
  'pdfRemovePages.pages.label': 'Pages to delete',
  'pdfRemovePages.pages.help':
    'Use page numbers and ranges separated by commas, such as 1-3,5.',
  'pdfRemovePages.pages.note':
    'At least one page must remain in each document.',
  'pdfRemovePages.badge.none': 'No pages picked',
  'pdfRemovePages.badge.pages': 'Delete {pages}',
  'pdfRemovePages.action.idle': 'Remove pages',
  'pdfRemovePages.action.one': 'Remove pages from 1 PDF',
  'pdfRemovePages.action.many': 'Remove pages from {count} PDFs',
  'pdfRemovePages.action.busy': 'Removing pages…',
  'pdfRemovePages.helper.addOne':
    'Add at least one PDF to remove pages from.',
  'pdfRemovePages.helper.pages': 'Enter the pages you want to delete.',

  'pdfReorder.page.eyebrow': 'PDF',
  'pdfReorder.page.titleLead': 'Reorder',
  'pdfReorder.page.titleTail': 'pages',
  'pdfReorder.page.sub':
    'Rewrite the document in an explicit page order, or reverse it. Every page must appear exactly once.',
  'pdfReorder.output.sub':
    'Type the order or use Reverse, then check where the file will go.',
  'pdfReorder.order.label': 'Page order',
  'pdfReorder.order.placeholder': '3,1,2,4-6',
  'pdfReorder.order.help':
    'List every page exactly once using numbers and ranges, or use Reverse.',
  'pdfReorder.order.reverse': 'Reverse',
  'pdfReorder.order.missing': 'Missing pages: {pages}',
  'pdfReorder.order.duplicates': 'Repeated pages: {pages}',
  'pdfReorder.badge.empty': 'No order yet',
  'pdfReorder.badge.reverse': 'Reverse order',
  'pdfReorder.badge.custom': 'Custom order',
  'pdfReorder.action.idle': 'Reorder pages',
  'pdfReorder.action.one': 'Reorder 1 PDF',
  'pdfReorder.action.many': 'Reorder {count} PDFs',
  'pdfReorder.action.busy': 'Reordering pages…',
  'pdfReorder.helper.addOne': 'Add at least one PDF to reorder.',
  'pdfReorder.helper.order': 'Enter the page order, or use Reverse.',

  'pdfExtractImages.page.eyebrow': 'PDF',
  'pdfExtractImages.page.titleLead': 'Extract',
  'pdfExtractImages.page.titleTail': 'images',
  'pdfExtractImages.page.sub':
    'Write every embedded image of the pages you choose into its own folder. Everything runs on this machine.',
  'pdfExtractImages.output.sub':
    'Choose the pages to search, then check where the folder will go.',
  'pdfExtractImages.pages.label': 'Pages to search',
  'pdfExtractImages.pages.help':
    'Leave the field empty to search every page.',
  'pdfExtractImages.folderNote':
    'The output is a folder, not a PDF: each input gets its own <stem>_images folder with the extracted images.',
  'pdfExtractImages.badge': 'Folder output',
  'pdfExtractImages.action.idle': 'Extract images',
  'pdfExtractImages.action.one': 'Extract images from 1 PDF',
  'pdfExtractImages.action.many': 'Extract images from {count} PDFs',
  'pdfExtractImages.action.busy': 'Extracting images…',
  'pdfExtractImages.helper.addOne':
    'Add at least one PDF to extract images from.',
  'pdfExtractImages.result.title': 'Extracted folders',
  'pdfExtractImages.result.sub':
    'The folder and the number of images verified inside it.',
  'pdfExtractImages.result.aria': 'Extracted folders',
} as const;
