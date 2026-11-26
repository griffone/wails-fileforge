/**
 * Client-side mirror of `internal/pdf/engine/page_ops.go` and the page-range
 * grammar in `internal/pdf/engine/split.go`.
 *
 * The backend remains the authority; this module exists so an obviously
 * invalid request never reaches the plan call and so the form can explain the
 * problem in the user's language. The rules are copied from `ParseSplitRanges`
 * (selection) and `ParseReorderOrder` (order):
 *
 * - comma-separated tokens, each `N` or ascending `START-END`, pages start at 1;
 * - selection ranges may not overlap or repeat a page;
 * - an order must list every page of the document exactly once;
 * - `reverse` is accepted as the whole spec, case-insensitively.
 */

/** One parsed token, kept as an interval so a huge range is never expanded. */
export interface PageInterval {
  start: number;
  end: number;
}

export type SyntaxIssue =
  | { kind: 'emptyEntry' }
  | { kind: 'token'; token: string }
  | { kind: 'range'; token: string }
  | { kind: 'rangeOrder'; token: string };

export type PageSelectionIssue =
  | { kind: 'required' }
  | SyntaxIssue
  | { kind: 'overlap'; first: PageInterval; second: PageInterval }
  | { kind: 'outOfBounds'; pageCount: number }
  | { kind: 'allPages'; pageCount: number };

export type ReorderIssue =
  | { kind: 'required' }
  | SyntaxIssue
  | { kind: 'outOfBounds'; page: number; pageCount: number }
  | { kind: 'coverage'; pageCount: number; missing: number[]; duplicates: number[] };

export interface PageSelectionValidation {
  expression: string;
  /** Pages the selection covers; 0 for an empty selection. */
  selectedPages: number;
  issue: PageSelectionIssue | null;
}

export interface ReorderValidation {
  order: string;
  reverse: boolean;
  issue: ReorderIssue | null;
}

export interface PageSelectionOptions {
  /** Reject an empty expression instead of treating it as "all pages". */
  required?: boolean;
  /** Reject a selection that covers every page of a known document. */
  forbidAllPages?: boolean;
}

/** `Atoi` accepts a leading sign; the backend then rejects anything below 1. */
const INTEGER_TOKEN = /^[+-]?\d+$/;

const REVERSE_KEYWORD = 'reverse';

function parsePositiveInt(text: string): number | null {
  if (!INTEGER_TOKEN.test(text)) {
    return null;
  }

  const value = Number(text);
  return Number.isSafeInteger(value) && value >= 1 ? value : null;
}

interface TokenParse {
  intervals: PageInterval[];
  issue: SyntaxIssue | null;
}

/** Parses the shared `N` / `START-END` grammar without needing a page count. */
function parseTokens(expression: string): TokenParse {
  const trimmed = expression.trim();
  if (!trimmed) {
    return { intervals: [], issue: null };
  }

  const intervals: PageInterval[] = [];
  for (const rawToken of trimmed.split(',')) {
    const token = rawToken.trim();
    if (!token) {
      return { intervals: [], issue: { kind: 'emptyEntry' } };
    }

    if (!token.includes('-')) {
      const page = parsePositiveInt(token);
      if (page === null) {
        return { intervals: [], issue: { kind: 'token', token } };
      }
      intervals.push({ start: page, end: page });
      continue;
    }

    const parts = token.split('-');
    if (parts.length !== 2) {
      return { intervals: [], issue: { kind: 'range', token } };
    }

    const start = parsePositiveInt(parts[0].trim());
    const end = parsePositiveInt(parts[1].trim());
    if (start === null || end === null) {
      return { intervals: [], issue: { kind: 'range', token } };
    }
    if (start > end) {
      return { intervals: [], issue: { kind: 'rangeOrder', token } };
    }

    intervals.push({ start, end });
  }

  return { intervals, issue: null };
}

/** The backend sorts the ranges before reporting the first overlap. */
function findOverlap(
  intervals: readonly PageInterval[],
): { first: PageInterval; second: PageInterval } | null {
  if (intervals.length <= 1) {
    return null;
  }

  const sorted = [...intervals].sort((left, right) =>
    left.start === right.start
      ? left.end - right.end
      : left.start - right.start,
  );

  for (let index = 1; index < sorted.length; index += 1) {
    const previous = sorted[index - 1];
    const current = sorted[index];
    if (current.start <= previous.end) {
      return { first: previous, second: current };
    }
  }

  return null;
}

function selectionPageCount(intervals: readonly PageInterval[]): number {
  return intervals.reduce(
    (total, interval) => total + (interval.end - interval.start + 1),
    0,
  );
}

/** Minimum known page count: the smallest input is the first to fail. */
function smallestPageCount(pageCounts: readonly number[]): number | null {
  const known = pageCounts.filter((count) => count > 0);
  return known.length > 0 ? Math.min(...known) : null;
}

/**
 * Validates a rotate, remove-pages or extract-images selection.
 *
 * `pageCounts` holds every inspected document in queue order. Unknown counts
 * are simply absent, which leaves the bound (and the remove-every-page check)
 * to the backend.
 */
export function validatePageSelection(
  expression: string,
  pageCounts: readonly number[],
  options: PageSelectionOptions = {},
): PageSelectionValidation {
  const trimmed = expression.trim();
  if (!trimmed) {
    return {
      expression: trimmed,
      selectedPages: 0,
      issue: options.required ? { kind: 'required' } : null,
    };
  }

  const parsed = parseTokens(trimmed);
  if (parsed.issue) {
    return { expression: trimmed, selectedPages: 0, issue: parsed.issue };
  }

  const overlap = findOverlap(parsed.intervals);
  if (overlap) {
    return { expression: trimmed, selectedPages: 0, issue: { kind: 'overlap', ...overlap } };
  }

  const selectedPages = selectionPageCount(parsed.intervals);
  const smallest = smallestPageCount(pageCounts);
  if (smallest !== null) {
    // The backend rejects the first range that ends past the page count, so
    // the smallest inspected document decides whether the request survives.
    for (const interval of parsed.intervals) {
      if (interval.end > smallest) {
        return {
          expression: trimmed,
          selectedPages,
          issue: { kind: 'outOfBounds', pageCount: smallest },
        };
      }
    }
  }

  if (options.forbidAllPages) {
    const covered = pageCounts.find(
      (count) => count > 0 && count === selectedPages,
    );
    if (covered !== undefined) {
      return {
        expression: trimmed,
        selectedPages,
        issue: { kind: 'allPages', pageCount: covered },
      };
    }
  }

  return { expression: trimmed, selectedPages, issue: null };
}

/**
 * Validates a reorder spec. `reverse` is valid without a page count; coverage
 * is only checked for inspected documents, and every inspected count is
 * checked because the operation fails when any input cannot be covered.
 */
export function validateReorderOrder(
  order: string,
  pageCounts: readonly number[],
): ReorderValidation {
  const trimmed = order.trim();
  if (!trimmed) {
    return { order: trimmed, reverse: false, issue: { kind: 'required' } };
  }

  if (trimmed.toLowerCase() === REVERSE_KEYWORD) {
    return { order: trimmed, reverse: true, issue: null };
  }

  const parsed = parseTokens(trimmed);
  if (parsed.issue) {
    return { order: trimmed, reverse: false, issue: parsed.issue };
  }

  const smallest = smallestPageCount(pageCounts);
  if (smallest === null) {
    return { order: trimmed, reverse: false, issue: null };
  }

  for (const interval of parsed.intervals) {
    if (interval.end > smallest) {
      const page = interval.start > smallest ? interval.start : smallest + 1;
      return {
        order: trimmed,
        reverse: false,
        issue: { kind: 'outOfBounds', page, pageCount: smallest },
      };
    }
  }

  for (const pageCount of pageCounts) {
    if (pageCount <= 0) {
      continue;
    }

    const seen = new Array<number>(pageCount + 1).fill(0);
    for (const interval of parsed.intervals) {
      for (let page = interval.start; page <= interval.end; page += 1) {
        seen[page] += 1;
      }
    }

    const missing: number[] = [];
    const duplicates: number[] = [];
    for (let page = 1; page <= pageCount; page += 1) {
      if (seen[page] === 0) {
        missing.push(page);
      } else if (seen[page] > 1) {
        duplicates.push(page);
      }
    }

    if (missing.length > 0 || duplicates.length > 0) {
      return {
        order: trimmed,
        reverse: false,
        issue: { kind: 'coverage', pageCount, missing, duplicates },
      };
    }
  }

  return { order: trimmed, reverse: false, issue: null };
}

/** `4, 5, 6` — the same list format the backend uses in its messages. */
export function formatPageList(pages: readonly number[]): string {
  return pages.join(', ');
}

/** `1-3,5` for one interval pair; used by the overlap message. */
export function formatInterval(interval: PageInterval): string {
  return interval.start === interval.end
    ? String(interval.start)
    : `${interval.start}-${interval.end}`;
}
