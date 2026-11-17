# Differentiation Fifth Slice — Clean Scan (Local OCR)

- Status: in progress
- Branch: `feat/clean-scan-ocr`
- Started: 2026-11-17
- Plan: `docs/differentiation-roadmap.md`
- Previous slices: Email PDF, Web image set, Social clip, Local automation (merged to main)
- Goal: make scanned PDFs searchable locally with a real OCR tool, a curated Clean scan recipe, capability detection and verified text

## Environment facts

- `pdftoppm`, `tesseract` 5.5.4 and `pdftotext` are installed.
- Installed Tesseract languages: `afr` and `osd` only. `eng`/`spa` data is **not** installed.
- The tool must detect installed languages, default to the best available (`eng` → `spa` → first non-`osd`), and show a clear capability warning. Tests use the first installed language and skip when none exists.
- `ocrmypdf` is not installed and is not used; the pipeline is explicit: pdftoppm → tesseract → pdfcpu merge/optimize.

## Scope

1. **OCR engine** (`internal/pdf/engine/ocr.go`)
   - Capability detection: `pdftoppm`, `tesseract`, optional `pdftotext`, installed languages (exclude `osd`), recommended language.
   - Pipeline for one PDF:
     1. count pages with pdfcpu;
     2. rasterize at 300 DPI with `pdftoppm -png`;
     3. run `tesseract <page.png> <base> -l <lang> --dpi 300 pdf txt` per page;
     4. merge per-page PDFs in order with pdfcpu;
     5. optimize the merged PDF with pdfcpu; if optimization fails, keep the merged file and warn;
     6. verify the final PDF is searchable with `pdftotext` (fallback to the sidecar text with a warning when `pdftotext` is unavailable);
     7. write the searchable PDF and optional text sidecar atomically;
     8. verify the final page count equals the source.
   - Per-page progress callback, context cancellation between pages, temp workspace cleanup.
   - Clear typed errors for missing runtime, missing language, rasterize/recognize/merge/verify failures.
2. **PDF OCR tool** (`tool.pdf.ocr`)
   - Single and batch modes; input PDFs; outputs `<stem>_ocr.pdf` and (default) `<stem>_ocr.txt`.
   - Options: `language` (default recommended), `textSidecar` (default true), `dpi` fixed at 300.
   - Explicit `outputPath` supported for single mode; the sidecar derives from the PDF name.
   - `RuntimeState` degrades with an actionable reason when pdftoppm/tesseract/languages are missing.
   - `Plan` validates inputs, language, dependencies and collision-safe outputs; returns PDF + TXT planned outputs.
   - `ExecuteSingle` / `ExecuteBatch` use `SingleExecutorWithProgress` for page-level job progress.
   - Register in `internal/pdf/init.go`.
3. **Capability endpoint**
   - `GetOCRCapabilitiesV1` returns available, pdftoppm/tesseract/pdftotext flags, installed languages, recommended language and a message.
4. **Clean scan recipe** (`recipe.clean_scan`)
   - One PDF; outputs `<stem>_clean.pdf` + `<stem>_clean.txt`.
   - Steps: `OCR every page` (tool.pdf.ocr) → `Verify text layer` (recipe-owned).
   - Uses the requested language or the recommended installed one; warns when the recommended language is not `eng`/`spa`.
   - Verification: page count matches source, text layer is non-empty (pdftotext; sidecar fallback), generic PDF/TXT checks.
   - No text recognized → `partial_success`, outputs kept and clearly reported.
   - History/undo for both outputs; cleanup on failure/cancel.
5. **PDF OCR page** (`/pdf-ocr`)
   - New PDF tool in the registry; the tool count becomes 13, PDF category 5.
   - File-first intake, language select from capabilities, text-sidecar toggle, destination folder, output plan, run/cancel/progress, verified result and undo.
   - Capability banner with install guidance when deps or languages are missing.
   - Warning that pages are rasterized at 300 DPI and vector text is replaced by an image + invisible text layer.
   - Show recognized page/character count from the job result.
6. **Clean scan recipe UI**
   - Home recipe card and `/recipe/clean-scan` using the generic recipe page.
   - One PDF, no target, two outputs, verification and undo.
   - Home Recipes section shows four cards; tool grid shows 13 tools.

## Explicit decisions

- **Recipe first + tool page.** The OCR tool is directly usable with language selection; the Clean scan recipe is the one-click outcome.
- **Text sidecar default on**: it is useful and makes verification independent of PDF text extraction. The tool page can turn it off.
- **No ocrmypdf**: the explicit pipeline is debuggable and uses installed binaries.
- **Optimize inside the OCR engine**: pdfcpu optimization happens after merging, before publishing; if it fails, the searchable merged PDF is still delivered with a warning.
- **Language capability over assumptions**: never assume `eng` is installed. The UI and plan show the installed languages; tests use the first available.
- **No cloud/model downloads.** Optional language packs are the user's responsibility; the app shows guidance only.
- **Sidecar naming**: the text file is the PDF path with `.txt` instead of `.pdf` (e.g., `report_clean.pdf` → `report_clean.txt`).
- **Vector PDFs**: rasterization is explicit in the UI copy; OCR is intended for scans, not for preserving vector text.

## Acceptance criteria

1. The OCR tool detects dependencies and languages; missing deps/languages produce a clear degraded state and plan error.
2. Running OCR on a text PDF produces a searchable PDF with the same page count and a non-empty text sidecar; `pdftotext` can extract text from the output.
3. Batch OCR produces one PDF + TXT per input with collision-safe names.
4. Cancellation stops between pages, cleans temp files and leaves no partial final outputs.
5. The Clean scan recipe runs from Home, outputs a searchable PDF + text sidecar, verifies page count and text, records both for undo, and reports `partial_success` when no text is recognized.
6. The `/pdf-ocr` page loads capabilities, lets the user choose an installed language and sidecar, plans/runs with progress, and shows verified results and undo.
7. Home shows 13 tools and four recipe cards; navigation and registry tests are updated.
8. TXT outputs are classified as documents and verified (kind/size).
9. Go tests, frontend specs and build pass; bindings regenerated, never hand-edited.
10. No regressions in the previous recipes, automation, tools, undo or navigation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: OCR page capabilities/language select, plan/run/result; Clean scan recipe from Home; dark/light themes.

## Progress

- 2026-11-17: Slice scoped after verifying pdftoppm/tesseract/pdftotext and the installed `afr` language. The explicit pipeline was smoke-tested: pdftoppm → tesseract pdf+txt → merged OCR PDF → pdftotext extracted the visible text.

## Implementation notes

- Backend packages: `internal/pdf/engine/ocr.go`, `internal/pdf/tool_ocr.go`, `internal/models/ocr_v1.go`, `internal/recipes/clean_scan.go`.
- Add `.txt` to `inspect.kindFromExtension` as `document` and `text/plain` MIME so verification passes for sidecars.
- Use a single pdfcpu merge call; keep page order from the numeric page suffix.
- The OCR tool's page progress is a `JobProgressV1` through `SingleExecutorWithProgress`; the recipe maps it to its own progress.
- The recipe's verify step uses the same `pdftotext` helper as the engine (export a small verification function or duplicate a minimal command).
- The tool page reuses the standard `planJobV1`/`runJobV1`/status polling/ResultCard pattern; no new job API.
- Bindings: new `GetOCRCapabilitiesV1`; report the binding ID.
