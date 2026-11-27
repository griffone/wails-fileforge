import type { TranslationKey } from '../../i18n';
import type { I18nService } from '../../services/i18n.service';
import type { FileInfo } from '../../services/wails';
import { formatBytes, formatCount } from '../../utils/format';

/**
 * Frontend mirror of the four PDF page-operation manifests in
 * `internal/pdf/tool_*.go`.
 *
 * The manifests are the source of truth: the accepted input extension, the
 * option keys, the rotation angles and the defaults below are copied from them
 * and must stay identical.
 */
export const PDF_INPUT_EXTENSIONS: ReadonlySet<string> = new Set(['pdf']);

export const PDF_INPUT_ACCEPT = '.pdf,application/pdf';

/** `rotateOptionPageSelection` / `removePagesOptionPageSelection` / `extractImagesOptionPageSelection`. */
export const PAGE_SELECTION_OPTION = 'pageSelection';

/** `rotateOptionAngle`. */
export const ROTATE_ANGLE_OPTION = 'angle';

/** `reorderOptionOrder`. */
export const REORDER_ORDER_OPTION = 'order';

/** `RotateAngleCW90`, `RotateAngleCW180`, `RotateAngleCW270`. */
export const PDF_ROTATE_ANGLES = [90, 180, 270] as const;
export type RotateAngle = (typeof PDF_ROTATE_ANGLES)[number];

export const DEFAULT_ROTATE_ANGLE: RotateAngle = 90;

/** The reorder shortcut accepted by `parseReorderPages`. */
export const REORDER_REVERSE_KEYWORD = 'reverse';

export function isRotateAngle(value: unknown): value is RotateAngle {
  return (
    typeof value === 'number' &&
    (PDF_ROTATE_ANGLES as readonly number[]).includes(value)
  );
}

/** The option shape rotate, remove pages and extract images share. */
export type PdfPageSelectionOptions = {
  pageSelection: string;
};

export function defaultPageSelectionOptions(): PdfPageSelectionOptions {
  return { pageSelection: '' };
}

export function normalizePageSelectionOptions(
  stored: Record<string, unknown>,
): Partial<PdfPageSelectionOptions> {
  const result: Partial<PdfPageSelectionOptions> = {};
  if (typeof stored[PAGE_SELECTION_OPTION] === 'string') {
    result.pageSelection = stored[PAGE_SELECTION_OPTION] as string;
  }
  return result;
}

export type PdfRotateOptions = {
  pageSelection: string;
  angle: RotateAngle;
};

export function defaultRotateOptions(): PdfRotateOptions {
  return { pageSelection: '', angle: DEFAULT_ROTATE_ANGLE };
}

export function normalizeRotateOptions(
  stored: Record<string, unknown>,
): Partial<PdfRotateOptions> {
  const result: Partial<PdfRotateOptions> = normalizePageSelectionOptions(
    stored,
  );
  if (isRotateAngle(stored[ROTATE_ANGLE_OPTION])) {
    result.angle = stored[ROTATE_ANGLE_OPTION] as RotateAngle;
  }
  return result;
}

export type PdfReorderOptions = {
  order: string;
};

export function defaultReorderOptions(): PdfReorderOptions {
  return { order: '' };
}

export function normalizeReorderOptions(
  stored: Record<string, unknown>,
): Partial<PdfReorderOptions> {
  const result: Partial<PdfReorderOptions> = {};
  if (typeof stored[REORDER_ORDER_OPTION] === 'string') {
    result.order = stored[REORDER_ORDER_OPTION] as string;
  }
  return result;
}

/** Queue metadata shared by the four pages: size and page count. */
export function describePdfFileInfo(
  i18n: I18nService,
  info: FileInfo,
): string {
  const parts: string[] = [];
  if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
    parts.push(formatBytes(info.sizeBytes));
  }
  if (typeof info.pageCount === 'number' && info.pageCount > 0) {
    parts.push(
      formatCount(
        info.pageCount,
        i18n.t('common.pageOne'),
        i18n.t('common.pageMany'),
      ),
    );
  }
  return parts.join(' · ');
}

/**
 * Keys every PDF page-operation page resolves through its copy block.
 * Intake strings are shared; titles, action labels and the output subtitle are
 * page-owned.
 */
export interface PdfPageOpsCopy {
  eyebrow: TranslationKey;
  titleLead: TranslationKey;
  titleTail: TranslationKey;
  sub: TranslationKey;
  intakeTitle: TranslationKey;
  intakeSub: TranslationKey;
  dropTitle: TranslationKey;
  dropHint: TranslationKey;
  dropAria: TranslationKey;
  outputTitle: TranslationKey;
  outputSub: TranslationKey;
  actionIdle: TranslationKey;
  actionOne: TranslationKey;
  actionMany: TranslationKey;
  actionBusy: TranslationKey;
  helperAddOne: TranslationKey;
}

/** Intake copy shared by the four pages; only the copy blocks reference it. */
export const PDF_PAGE_OPS_SHARED_COPY = {
  intakeTitle: 'pdfPageOps.intake.title',
  intakeSub: 'pdfPageOps.intake.sub',
  dropTitle: 'pdfPageOps.intake.dropTitle',
  dropHint: 'pdfPageOps.intake.dropHint',
  dropAria: 'pdfPageOps.intake.dropAria',
  outputTitle: 'pdfPageOps.output.title',
} as const;
