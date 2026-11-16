# Tool Advanced Features

- Status: PDF optimized/split advanced, DOCX PDF/A and video crossfade complete; multi-track deferred
- Branch: `feat/tool-advanced`
- Started: 2026-11-16
- Related: `odd/tasks/tool-completeness.md`

## Purpose

Take the table-stakes tools to the next level with the advanced features explicitly deferred from the completeness pass:

1. PDF Split by page count / bookmarks (size as a possible later addition).
2. PDF Optimize: compression + metadata editing.
3. DOCX → PDF/A archival export.
4. Video Merge transitions (crossfade) and audio behavior; multi-track remains a separate future tool.

## Phase 1 — PDF advanced

### PDF Split strategies

- Add `pages_per_file`:
  * Option `pagesPerFile` int, 1–1000.
  * Plan chunks page ranges (`1-5`, `6-10`, …), one output per chunk, output dir `<stem>_pages`, collision-safe.
  * Result count and output names reflect the chunks.
- Add `bookmarks`:
  * Read top-level PDF bookmarks with pdfcpu (`api.Bookmarks`).
  * One output per bookmark using `PageFrom`/`PageThru`; if `PageThru` is 0, use the last page.
  * Sanitized bookmark title in the output name; fall back to `part_00N`.
  * If the PDF has no bookmarks, return a clear `PDF_SPLIT_NO_BOOKMARKS` error.
- Keep `every_page` and `ranges`; selected pages remains a frontend ranges builder.
- Plan returns directory output(s) and output counts; execution uses `TrimFile` selections.
- Frontend:
  * Strategy cards: Every page, Page ranges, Selected pages, Pages per file, Bookmarks.
  * Pages-per-file numeric input; bookmarks mode shows `One file per top-level bookmark` and a clear no-bookmarks state.
  * Preview: page-count chunks can reuse existing thumbnails; bookmarks preview may be a list of bookmark titles/page counts (PDF.js outline if feasible; otherwise a plan-level list).
  * Plan/run/result unchanged.
- Size-based splitting is deferred unless the page-count/bookmark work leaves room; document the decision.

### PDF Optimize tool

- New tool `tool.pdf.optimize`:
  * Inputs: 1+ PDFs, single/batch.
  * Options: `compress` bool (default true), metadata `{ title, author, subject, keywords }` (optional strings).
  * Output: `<stem>_optimized.pdf` next to each input or chosen folder; collision-safe.
  * Plan validates inputs and metadata length; returns file outputs.
  * Execution:
    - `api.OptimizeFile(input, temp, nil)` for compression.
    - `api.AddPropertiesFile(temp, output, properties, nil)` for metadata.
    - Clean temp files; never overwrite.
  * Result items with output paths/count; error codes and plain messages.
- Registry:
  * Add the tool to `config/tools.ts`, Home grid, sidebar PDF group, command palette, and routes (`/pdf-optimize`).
  * Home count becomes 12; update specs/tests.
- Frontend:
  * File-first flow: PDFs queue, output plan, Advanced metadata fields + Compress toggle, one action.
  * Result card; recent conversions automatically records via ResultCard.
  * Tests for plan/run/result.

## Phase 2 — DOCX PDF/A

- Add `pdfa` bool option to DOCX → PDF.
- LibreOffice export filter for PDF/A (`SelectPdfVersion`), with clear errors if the primary engine fails and fallback cannot honor PDF/A.
- Frontend Advanced toggle with helper; plan payload; tests for args/plan. Execution depends on LibreOffice; keep tests stubbed.

## Phase 3 — Video Merge transitions

- Add `transition` (`none` default | `crossfade`) and `transitionDuration` (0.1–3s, default 0.5).
- Force re-encode when a transition is selected; build an ffmpeg `xfade` filter chain using probed clip durations and audio `acrossfade`/`amix` as appropriate.
- Plan includes transition options; validate durations and minimum clip lengths.
- Frontend Advanced: transition select + duration; keep remove-audio.
- Multi-track/PiP is a separate future feature; do not half-implement it.

## Acceptance criteria

1. PDF Split supports page-count and bookmark strategies with correct plan/outputs/errors.
2. PDF Optimize compresses and edits metadata safely, with plan/run/result UI.
3. DOCX PDF/A export is available when LibreOffice is present and fails clearly otherwise.
4. Video Merge crossfade produces a valid merged file with the requested duration/audio policy.
5. Go tests, frontend specs and build pass; no regressions.
6. No generated binding hand edits; no committed build artifacts.

## Checks

- `go test ./...`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false`
- Manual (desktop): split by pages/bookmarks, optimize a PDF with metadata, export DOCX PDF/A, merge videos with crossfade.

## Progress

- 2026-11-16: Advanced phase started on `feat/tool-advanced`; PDF Split and PDF Optimize scoped first.
- 2026-11-16: PDF advanced complete. Split supports `pages_per_file` and `bookmarks` (top-level outline, sanitized names, clear no-bookmark/invalid errors); PDF Optimize tool added with compression + title/author/subject/keywords/creator metadata, collision-safe outputs, batch partial success, registry/home/routes/frontend flow and global-drop recommendation.
- 2026-11-16: DOCX PDF/A complete. `pdfa` option uses the LibreOffice PDF/A export filter; PDF/A only requires LibreOffice (PDFAProbe), Pandoc fallback is refused with a clear unsupported error, and page-selection + PDF/A emits a compliance warning. Frontend Advanced toggle with requirement copy.
- 2026-11-16: Video Merge crossfade complete. `transition`/`transitionDuration` options, ffprobe duration probing, xfade/acrossfade filter chain with validated offsets, forced re-encode, remove-audio support; frontend Advanced select/duration with validation.
- 2026-11-16: Verification: Go build/tests pass; frontend 379/379 Karma specs; build passes; visual checks for PDF Optimize/split strategies, DOCX PDF/A and video crossfade. LibreOffice is not installed in this environment, so PDF/A execution was verified with stubbed runners; a real desktop smoke test is pending.
- Multi-track/PiP video remains a separate future tool; it is not half-implemented.
