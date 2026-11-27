import {
  PDF_PAGE_OPS_SHARED_COPY,
  type PdfPageOpsCopy,
} from './pdf-page-ops-config';

/**
 * Copy blocks per PDF page-operation page.
 *
 * Keys are literal so the templates stay type-checked through `i18n.t(...)`,
 * and `i18n.t` reads the locale signal during rendering, which is what makes a
 * language switch re-render the whole page.
 */
export const PDF_ROTATE_COPY: PdfPageOpsCopy = {
  ...PDF_PAGE_OPS_SHARED_COPY,
  eyebrow: 'pdfRotate.page.eyebrow',
  titleLead: 'pdfRotate.page.titleLead',
  titleTail: 'pdfRotate.page.titleTail',
  sub: 'pdfRotate.page.sub',
  outputSub: 'pdfRotate.output.sub',
  actionIdle: 'pdfRotate.action.idle',
  actionOne: 'pdfRotate.action.one',
  actionMany: 'pdfRotate.action.many',
  actionBusy: 'pdfRotate.action.busy',
  helperAddOne: 'pdfRotate.helper.addOne',
};

export const PDF_REMOVE_PAGES_COPY: PdfPageOpsCopy = {
  ...PDF_PAGE_OPS_SHARED_COPY,
  eyebrow: 'pdfRemovePages.page.eyebrow',
  titleLead: 'pdfRemovePages.page.titleLead',
  titleTail: 'pdfRemovePages.page.titleTail',
  sub: 'pdfRemovePages.page.sub',
  outputSub: 'pdfRemovePages.output.sub',
  actionIdle: 'pdfRemovePages.action.idle',
  actionOne: 'pdfRemovePages.action.one',
  actionMany: 'pdfRemovePages.action.many',
  actionBusy: 'pdfRemovePages.action.busy',
  helperAddOne: 'pdfRemovePages.helper.addOne',
};

export const PDF_REORDER_COPY: PdfPageOpsCopy = {
  ...PDF_PAGE_OPS_SHARED_COPY,
  eyebrow: 'pdfReorder.page.eyebrow',
  titleLead: 'pdfReorder.page.titleLead',
  titleTail: 'pdfReorder.page.titleTail',
  sub: 'pdfReorder.page.sub',
  outputSub: 'pdfReorder.output.sub',
  actionIdle: 'pdfReorder.action.idle',
  actionOne: 'pdfReorder.action.one',
  actionMany: 'pdfReorder.action.many',
  actionBusy: 'pdfReorder.action.busy',
  helperAddOne: 'pdfReorder.helper.addOne',
};

export const PDF_EXTRACT_IMAGES_COPY: PdfPageOpsCopy = {
  ...PDF_PAGE_OPS_SHARED_COPY,
  eyebrow: 'pdfExtractImages.page.eyebrow',
  titleLead: 'pdfExtractImages.page.titleLead',
  titleTail: 'pdfExtractImages.page.titleTail',
  sub: 'pdfExtractImages.page.sub',
  outputSub: 'pdfExtractImages.output.sub',
  actionIdle: 'pdfExtractImages.action.idle',
  actionOne: 'pdfExtractImages.action.one',
  actionMany: 'pdfExtractImages.action.many',
  actionBusy: 'pdfExtractImages.action.busy',
  helperAddOne: 'pdfExtractImages.helper.addOne',
};
