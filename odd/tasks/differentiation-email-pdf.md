# Differentiation First Slice — Email PDF

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/differentiation-email-pdf`
- Started: 2026-11-16
- Plan: `docs/differentiation-roadmap.md`
- Goal: prove the differentiator loop end-to-end: **recipe → target size → verified output → undo**

## Scope

Implement the first vertical slice from the differentiation roadmap:

1. **Email PDF recipe**
   - Input: 1+ PDFs.
   - Steps: merge when >1, optimize to a target size (default 10 MB), verify.
   - Output: one PDF next to the first input or a chosen folder, collision-safe.
   - UI: Home recipe card + dedicated `/recipe/email-pdf` run flow with step progress, result and undo.
2. **PDF target size**
   - Extend `tool.pdf.optimize` with `targetSizeBytes`.
   - Best effort: pdfcpu optimize → Ghostscript `/ebook` → `/screen`; report when the target cannot be met.
   - Reuse the output pipeline, metadata, collision safety and partial success.
3. **Verified outputs**
   - `VerifyJobV1(jobId)` for normal jobs; output checks: exists, size, format, PDF pages, image dimensions, video duration.
   - Recipe result includes verification checks and target-size pass/fail.
   - ResultCard shows a compact Verified section.
4. **Undo**
   - `internal/jobhistory` records terminal job outputs with size/mtime fingerprints.
   - `UndoJobV1(jobId)` moves only untouched outputs to an app-managed trash; `RestoreJobV1`, `EmptyTrashV1`.
   - ResultCard exposes Undo; Recent conversions keeps working.
5. **Recipe engine foundation**
   - `internal/recipes` with a curated Email PDF recipe.
   - `ListRecipesV1`, `PlanRecipeV1`, `RunRecipeV1`, `GetRecipeStatusV1`, `CancelRecipeV1`.
   - `recipes/progress/v1` event with step index/name/stage/percent.
   - Temp workspace for intermediates; cleanup on failure/cancel.
   - Uses existing tools (PDF Merge, PDF Optimize) so the recipe is orchestration, not a fork of tool logic.

## Deferred

- Other recipes (Web image set, Archive document, Social clip).
- Watch-folder automation.
- OCR/transcription.
- General recipe builder.
- Normal-job undo UI in Recent conversions beyond the ResultCard (can add cheaply later).
- Target-size control on the standalone PDF Optimize page (the option is implemented; the recipe page is the first UI).
- `recipes/progress/v1` live subscription in the UI (the page polls `GetRecipeStatusV1` every second).

## Acceptance criteria

1. Email PDF with 1 PDF optimizes it to ≤ target when possible and outputs one verified PDF.
2. Email PDF with 3 PDFs merges them in order, optimizes, verifies page count and outputs one PDF.
3. Target unreachable returns the smallest best-effort output with a clear warning, not a silent pass.
4. Recipe progress shows Merge and Optimize steps; cancel stops between/within steps where the tool supports it; temp files are cleaned.
5. A finished job can be verified and undone; modified outputs are skipped on undo; restore works.
6. Go tests, frontend specs and build pass; no regressions in the 12 existing tools.
7. No generated binding hand edits; no build artifacts.

## Checks

- `go test ./...`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- Manual: Email PDF with 1 and 3 PDFs, target 10MB/2MB, undo, restore, modified-output refusal.
- `npx agent-browser` visual check of the recipe page, Verified block and Undo state.

## Progress

- 2026-11-16: Slice scoped. Backend foundations first (target size, verification, job history/undo), then recipe runner, then UI.
- 2026-11-16: PDF target compression implemented in `tool.pdf.optimize` (pdfcpu → Ghostscript `/ebook` → `/screen`, smallest candidate, best-effort message, metadata after compression). Commit `af70008`.
- 2026-11-16: Verification (`internal/verify`), job history/undo/restore/trash, orchestrator completion hook, recipe service and Email PDF recipe, App endpoints and regenerated bindings. Commit `c407e6f`.
- 2026-11-16: ResultCard now shows a real Verified block with checks, auto-fetches `VerifyJobV1` for normal jobs, skips fetch for `recipe.*`, and exposes Undo/Restore with skipped-file reporting. Commit `dddec0d`.
- 2026-11-16: Home Recipes section and `/recipe/email-pdf` page (file-first intake, target MB, output folder, step stepper, polled progress, cancel, ResultCard with recipe verification, open/reveal). Commit `9cabe52`.
- 2026-11-16: Integration tests run the recipe, undo the output, restore it, and verify a modified output is refused. Commit `ab39d05`.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass (including `internal/recipes`, `internal/jobhistory`, `internal/verify`, `internal/pdf`).
- `cd frontend && npm run build:dev` — success. Pre-existing budget warnings remain (initial bundle; `home.css` 7.00 kB, `result-card.css` 5.34 kB, other component styles), all under the 8 kB error cap.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 397/397 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual check at 1440×900: Home Recipes card, recipe page in dark and light themes, header, intake, settings, steps and plan/action panels render correctly. This ran against `ng serve` (preview only), so it did not exercise IPC.
- Pending: desktop end-to-end with real PDFs (1 and 3 inputs, 10 MB and 2 MB targets, undo/restore, modified-output refusal) in the Wails window. The IPC path is covered by the Go integration tests and mocked frontend specs.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated | `TestRunEmailPDFSingleInput`, recipe page spec |
| 2 | Automated | `TestRunEmailPDFMergesMultipleInputs` (2+3+1 pages), frontend plan/run specs |
| 3 | Automated | `TestRunEmailPDFTinyTargetReturnsPartialSuccess`, PDF target tests |
| 4 | Automated | Step events/status, cancel cleanup test, failure cleanup test; UI stepper specs |
| 5 | Automated | `TestRecipeOutputCanBeUndoneAndRestored`, `TestRecipeUndoSkipsModifiedOutput`, history and ResultCard specs |
| 6 | Verified | Go tests, 397 Karma specs, production/dev build |
| 7 | Verified | Bindings regenerated only; `git status` clean of build artifacts |

## Next steps

1. Run the desktop end-to-end pass with real PDFs and record the result.
2. Merge the branch when the user requests it.
3. Start the next slice: Web image set (fan-out publish bundle), reusing the recipe runner, verification and undo.
