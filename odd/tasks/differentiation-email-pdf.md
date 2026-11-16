# Differentiation First Slice — Email PDF

- Status: in progress
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
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false`
- Manual: Email PDF with 1 and 3 PDFs, target 10MB/2MB, undo, restore, modified-output refusal.
- `npx agent-browser` visual check of the recipe page, Verified block and Undo state.

## Progress

- 2026-11-16: Slice scoped. Backend foundations first (target size, verification, job history/undo), then recipe runner, then UI.
