# Differentiation Fifth Slice — Clean Scan (Local OCR)

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/clean-scan-ocr`
- Started: 2026-11-17
- Plan: `docs/differentiation-roadmap.md`
- Previous slices: Email PDF, Web image set, Social clip, Local automation (merged to main)
- Goal: make scanned PDFs searchable locally with a real OCR tool, a curated Clean scan recipe, capability detection and verified text

## Environment facts

- `pdftoppm`, `tesseract` 5.5.4 and `pdftotext` are installed.
- Installed Tesseract languages: `afr` and `osd` only. `eng`/`spa` data is **not** installed. The UI detects this, defaults to the first installed language and warns that `eng`/`spa` data would improve accuracy.
- `ocrmypdf` is not installed and is not used. The pipeline is explicit: pdftoppm → tesseract → pdfcpu merge/optimize.

## Scope

1. **OCR engine** (`internal/pdf/engine/ocr.go`)
   - Capability detection: pdftoppm, tesseract, optional pdftotext, installed languages (osd excluded), recommended language.
   - Pipeline for one PDF: page count → rasterize at 300 DPI → per-page tesseract PDF+TXT → merge page PDFs → optimize → verify text → atomic publish of PDF + sidecar → page-count verification.
   - If pdfcpu optimization drops the text layer, the merged OCR file is used instead.
   - Per-page progress, cancellation between pages, temp cleanup, typed errors.
2. **PDF OCR tool** (`tool.pdf.ocr`)
   - Single and batch modes; outputs `<stem>_ocr.pdf` and default `<stem>_ocr.txt`.
   - Options: language (default recommended), textSidecar (default true), DPI fixed 300 (150–600 validated).
   - Explicit outputPath for single mode; sidecar derived from the PDF name; collision-safe outputs.
   - RuntimeState degrades with actionable guidance; Plan validates dependencies/language; page-level progress via `SingleExecutorWithProgress`.
   - Registered in `internal/pdf/init.go`.
3. **Capability endpoint** `GetOCRCapabilitiesV1` with binary flags, languages and recommended language.
4. **Clean scan recipe** (`recipe.clean_scan`)
   - One PDF; outputs `<stem>_clean.pdf` + `<stem>_clean.txt`.
   - Steps: OCR every page → verify text layer.
   - Uses the requested or recommended language; warns when it is not eng/spa and that pages are rasterized.
   - Verification: source page count, text layer via pdftotext (sidecar fallback), sidecar text, generic PDF/TXT checks.
   - No text/page mismatch → `partial_success`, outputs kept; failure/cancel cleanup; both outputs recorded for undo.
5. **PDF OCR page** (`/pdf-ocr`)
   - PDF tool in the registry; tool count is now 13, PDF category 5, navigation 17.
   - File-first intake, language select from capabilities, text sidecar toggle, destination folder, six-output plan view, run/cancel/progress, verified result, undo, recognized page/character summary.
   - Capability danger banner and weak-language warning; 300 DPI rasterization warning.
6. **Clean scan UI**
   - Home recipe card and `/recipe/clean-scan` via the generic recipe page.
   - One PDF, no target, two outputs, verification and undo.
   - Home now shows 13 tools and four recipe cards.

## Explicit decisions

- **Recipe + tool page.** The OCR tool is directly usable with language selection; Clean scan is the one-click outcome.
- **Text sidecar default on** for verification and reuse; the tool page can disable it.
- **No ocrmypdf.** The explicit pipeline is debuggable and uses installed binaries.
- **Optimization inside the OCR engine**, with a text-layer safety fallback; the recipe has no separate optimize step.
- **Language capability over assumptions.** Never assume eng is installed; the page and recipe show installed languages and warnings.
- **No cloud/model downloads.** Language packs are the user's responsibility; the app only shows guidance.
- **Sidecar naming**: PDF path with `.txt` (e.g. `report_clean.pdf` → `report_clean.txt`).
- **Vector PDFs**: the UI states pages are rasterized and vector text is replaced by an image + invisible text layer.

## Acceptance criteria

1. OCR detects dependencies/languages; missing deps/languages produce a clear degraded state and plan error.
2. OCR on a text PDF produces a searchable PDF with the same page count and a non-empty sidecar; pdftotext extracts text.
3. Batch OCR produces one PDF + TXT per input with collision-safe names.
4. Cancellation stops between pages, cleans temp files and leaves no partial final outputs.
5. Clean scan runs from Home, outputs PDF + TXT, verifies page count/text, records both for undo, and reports partial when no text is recognized.
6. `/pdf-ocr` loads capabilities, language/sidecar controls, plans/runs with progress, shows verified results and undo.
7. Home shows 13 tools and four recipe cards; navigation/registry tests updated.
8. TXT outputs are classified as documents and verified.
9. Go tests, frontend specs and build pass; bindings regenerated only.
10. No regressions in previous recipes, automation, tools, undo or navigation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: OCR page capabilities/language select, plan/run/result; Clean scan recipe from Home; dark/light themes.

## Progress

- 2026-11-17: Slice scoped after smoke-testing pdftoppm → tesseract pdf+txt → merged OCR PDF → pdftotext extraction with the installed `afr` language.
- 2026-11-17: Backend implemented: OCR engine/capabilities, PDF OCR tool, TXT inspection, capability endpoint, Clean scan recipe, tests and bindings. Commit `d70fa0e`.
- 2026-11-17: Frontend implemented: `/pdf-ocr` page, Wails capabilities API, PDF OCR registry/nav entry (13 tools, 17 entries), Clean scan recipe card/page, Home/tool-count updates and specs. Commit `765aa89`.
- 2026-11-17: `AGENTS.md` updated to 13 tools, real Go test coverage and the OCR runtime-language gotcha.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass. Real OCR integration with tesseract `afr`: 2-page PDF → 2-page searchable PDF, non-empty sidecar with form feed, pdftotext extraction non-empty; blank PDF → partial_success; cancel leaves no outputs. Engine 11 tests, tool 13, recipe 9, inspect TXT.
- `go vet ./internal/pdf/... ./internal/recipes/... ./internal/app/... ./internal/inspect/... ./internal/verify/...` — clean.
- `cd frontend && npm run build:dev` — success. Pre-existing budget warnings; `pdf-ocr.css` is 2.3 kB, under budget.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 432/432 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual checks at 1440 px:
  - Home shows 13 tools, PDF count 5, four recipe cards including Clean scan;
  - `/pdf-ocr` renders intake, language/sidecar settings, destination, plan area and the 300 DPI warning in dark and light themes;
  - `/recipe/clean-scan` shows the single-PDF intake, two steps and highlights.
  - Preview mode has no IPC, so capability/language selection and actual OCR runs are not exercised in the browser.
- Pending: desktop Wails end-to-end with a real scanned PDF (language select, OCR run, searchable output, sidecar, undo) and dark/light in the native window. The backend uses real tesseract/pdftoppm/pdftotext in tests; the IPC path is the remaining unverified link.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated | capability/plan tests; degraded runtime tests |
| 2 | Automated | engine/tool integration with real ocr on a generated text PDF |
| 3 | Automated | batch OCR tool test |
| 4 | Automated | cancellation/cleanup tests |
| 5 | Automated | `clean_scan` tests (plan/run/partial/history/undo) |
| 6 | Automated + visual | PDF OCR page specs and browser layout check |
| 7 | Verified | Home/registry specs (13 tools, 17 nav entries, four recipes) |
| 8 | Automated | inspect TXT kind/MIME + verification |
| 9 | Verified | Go tests, 432 Karma, build, tsc; bindings regenerated only |
| 10 | Verified | Full Go/frontend suites including previous slices and automation |

## Next steps

1. Run the desktop OCR end-to-end pass with a real scan and record the result.
2. Merge the branch when the user requests it.
3. Remaining roadmap areas: Archive document (needs LibreOffice), transcription (needs whisper + model), optional OCR language packs and a Settings capability panel, automation polish (recursive watch, run history).
