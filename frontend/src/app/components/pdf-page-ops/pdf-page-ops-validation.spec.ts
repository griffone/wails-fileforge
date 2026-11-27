import {
  formatInterval,
  formatPageList,
  validatePageSelection,
  validateReorderOrder,
} from './pdf-page-ops-validation';

describe('pdf page operation validation', () => {
  describe('page selection', () => {
    it('treats an empty selection as every page unless it is required', () => {
      const optional = validatePageSelection('', [5]);
      expect(optional.issue).toBeNull();
      expect(optional.selectedPages).toBe(0);

      const required = validatePageSelection('', [5], { required: true });
      expect(required.issue?.kind).toBe('required');
    });

    it('accepts numbers and ascending ranges', () => {
      const result = validatePageSelection('1-3,5', [10]);
      expect(result.issue).toBeNull();
      expect(result.selectedPages).toBe(4);
    });

    it('mirrors the backend syntax errors', () => {
      expect(validatePageSelection('1,,2', []).issue?.kind).toBe('emptyEntry');
      expect(validatePageSelection('0', []).issue?.kind).toBe('token');
      expect(validatePageSelection('a', []).issue?.kind).toBe('token');
      expect(validatePageSelection('1-3-5', []).issue?.kind).toBe('range');
      expect(validatePageSelection('1-', []).issue?.kind).toBe('range');
      expect(validatePageSelection('5-2', []).issue?.kind).toBe('rangeOrder');
    });

    it('rejects overlapping or repeated pages', () => {
      const overlap = validatePageSelection('1-3,2-4', [10]);
      expect(overlap.issue).toEqual({
        kind: 'overlap',
        first: { start: 1, end: 3 },
        second: { start: 2, end: 4 },
      });
      expect(validatePageSelection('2,2', [10]).issue?.kind).toBe('overlap');
    });

    it('checks bounds against the smallest inspected document', () => {
      const issue = validatePageSelection('1-4', [3, 9]).issue;
      expect(issue).toEqual({ kind: 'outOfBounds', pageCount: 3 });
    });

    it('refuses a selection that covers every page of any document', () => {
      const issue = validatePageSelection('1-3', [3, 9], {
        forbidAllPages: true,
      }).issue;
      expect(issue).toEqual({ kind: 'allPages', pageCount: 3 });
    });
  });

  describe('reorder order', () => {
    it('requires a spec and accepts reverse in any case', () => {
      expect(validateReorderOrder('', [6]).issue?.kind).toBe('required');
      expect(validateReorderOrder('   ', [6]).issue?.kind).toBe('required');

      for (const spec of ['reverse', 'REVERSE', ' Reverse ']) {
        const result = validateReorderOrder(spec, [6]);
        expect(result.issue).toBeNull();
        expect(result.reverse).toBeTrue();
      }
    });

    it('accepts a spec that covers every page exactly once', () => {
      expect(validateReorderOrder('3,1,2,4-6', [6]).issue).toBeNull();
    });

    it('reports out-of-bounds pages before coverage', () => {
      const issue = validateReorderOrder('1,2,3,4,5,7', [6]).issue;
      expect(issue).toEqual({ kind: 'outOfBounds', page: 7, pageCount: 6 });

      const insideRange = validateReorderOrder('1-9', [6]).issue;
      expect(insideRange).toEqual({
        kind: 'outOfBounds',
        page: 7,
        pageCount: 6,
      });
    });

    it('reports missing pages', () => {
      const issue = validateReorderOrder('1-5', [6]).issue;
      expect(issue).toEqual({
        kind: 'coverage',
        pageCount: 6,
        missing: [6],
        duplicates: [],
      });
    });

    it('reports repeated pages', () => {
      const issue = validateReorderOrder('1,2,3,4,5,6,6', [6]).issue;
      expect(issue).toEqual({
        kind: 'coverage',
        pageCount: 6,
        missing: [],
        duplicates: [6],
      });
    });

    it('checks every inspected page count', () => {
      const issue = validateReorderOrder('1-5', [5, 7]).issue;
      expect(issue).toEqual({
        kind: 'coverage',
        pageCount: 7,
        missing: [6, 7],
        duplicates: [],
      });
    });

    it('leaves coverage to the backend when the page count is unknown', () => {
      expect(validateReorderOrder('9', []).issue).toBeNull();
    });

    it('formats page lists the way the backend does', () => {
      expect(formatPageList([4, 5, 6])).toBe('4, 5, 6');
      expect(formatInterval({ start: 2, end: 2 })).toBe('2');
      expect(formatInterval({ start: 2, end: 5 })).toBe('2-5');
    });
  });
});
