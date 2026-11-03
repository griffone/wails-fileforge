# Tool Suite Integrity Fixes

## Objective

Fix the defects that make the tool suite lie about its results: lost batch results, false failures,
silent overwrites, and a retry policy that never fires or duplicates outputs.

## Problem

An audit of all 11 registered tools found:

- `tool.pdf.split` batch discards successful items when any input fails, so the job reports `failed`
  instead of `partial_success`.
- `tool.doc.docx_to_pdf` returns the primary engine error even when the fallback engine produced a
  valid PDF, so the job reports failure with the output on disk.
- `tool.pdf.merge` silently overwrites an existing output file, unlike every other tool.
- The retry policy table references detail codes that no tool emits (`VIDEO_*_EXECUTION`,
  `PDF_CROP_EXECUTION`, ...), so retries never fire for those tools; image/PDF batch retries re-run
  the whole batch, duplicating `*_cropped-2` outputs.
- Three frontend error-code maps point to codes the backend never emits, so friendly messages are
  unreachable.

## Why

These are correctness defects in a file-manipulation product. A job that says "failed" while it
wrote a file, or "success" while it silently replaced a user's document, destroys trust faster than
any missing feature.

## Scope

In scope:
- `internal/pdf/engine/split.go`, `internal/pdf/tool_split.go`
- `internal/doc/engine/docx_to_pdf.go`
- `internal/pdf/engine/merge.go`, `internal/pdf/tool_merge.go`
- `internal/jobs/orchestrator.go`, `internal/jobs/retry.go`
- Frontend error maps, catalog route, and pdf.js asset URLs
- New Go tests for every fixed behavior.

Out of scope:
- New tools or features.
- Adding a libvips runtime probe: `bimg` is cgo-linked, so a machine without libvips cannot start
  the binary at all. The earlier audit claim was wrong and is retracted; nothing to probe.
- Full backend test coverage; only the fixed paths get tests.
- The uncommitted preview/image changes already present in the worktree.

## Constraints

- Non-destructive output naming: never overwrite an existing output file.
- V1-only architecture: no legacy paths, no client-side conversion fallbacks.
- Errors stay normalized: `error.code` + `detail_code` + actionable message.
- Retry policy keeps max 3 attempts and the existing backoff schedule.
- UI copy stays consistent with the surrounding Spanish map.

## Tasks

### F1 — pdf.split batch reports partial success — DONE (`24e22bc`)
- [x] `engine.SplitBatch` continues after a per-item plan or execution failure and returns the item
      error in the result.
- [x] `tool.pdf.split` builds one `JobResultItemV1` per input, aggregates `details.fileErrors[]`, and
      returns the aggregate error so the orchestrator derives `partial_success`.
- [x] Batch-shape validation (output dir, perInputDir conflicts, cross-item output collisions, range
      syntax) still fails the whole job before execution; per-file PDF validity moved to
      execution-time, matching `tool.pdf.crop`.
- Evidence: `TestSplitBatchReportsPartialSuccess`, `TestSplitBatchRequestIsShapeOnly`,
  `TestSplitToolExecuteBatchReportsPartialSuccess`.

### F2 — docx_to_pdf never reports failure after a successful fallback — DONE (`ce29ae4`)
- [x] When LibreOffice fails and Pandoc produces the PDF, `ConvertDOCX` returns success with
      `EngineUsed=pandoc` and `PrimaryError` kept as diagnostic.
- [x] Font-substitution detection on the engine that produced the delivered file still returns the
      documented `DOC_DOCX_TO_PDF_FONTS_MISSING_OR_SUBSTITUTED` error.
- Evidence: `TestConvertDOCXFallbackSucceedsAfterFontPrimaryError`,
  `TestConvertDOCXFallbackSucceedsAfterPrimaryExecutionFailure`, `TestConvertDOCXFailsWhenBothEnginesFail`.

### F3 — pdf.merge refuses to overwrite — DONE (`774bfbe`)
- [x] Validation and execution both reject an output path that already exists.
- [x] New detail code `PDF_MERGE_OUTPUT_ALREADY_EXISTS` with an actionable message.
- [x] Frontend friendly message for that code.
- Evidence: `TestMergeRefusesToOverwriteExistingOutput`, `TestValidateMergePathsRejectsExistingOutput`,
  `TestMergeSucceedsWhenOutputDoesNotExist`.

### F4 — retry policy fires once, per item — DONE (`0073fd9`)
- [x] Retry table entries match detail codes actually emitted by each engine.
- [x] Batch retry re-executes only the failed inputs, merges results preserving original order, and
      records `attempts`/`retryCount` per item.
- [x] No duplicate suffixed outputs on retry.
- [x] Cancellation during backoff still marks the job `cancelled`.
- Evidence: `TestRunBatchRetriesOnlyFailedItems` (retry receives only the failed input, attempts
  `[1,2,1]`, exactly 2 executions), `TestRunBatchDoesNotRetryNonRetryableErrors`,
  `TestRunBatchStopsRetryingWhenNoItemsReturned`,
  `TestIsRetryableErrorMatchesEmittedDetailCodes`, `TestIsRetryableErrorFallsBackToCode`.

### F5 — frontend contract drift — DONE (`bb92b17`, `3f76a9c`)
- [x] Friendly maps use `PDF_INVALID_INPUTS`, `PDF_CROP_PAGE_SELECTION_INVALID`,
      `IMAGE_CROP_RATIO_INVALID`.
- [x] Tool catalog can open `tool.image.convert`.
- [x] Removed `iccUrl`/`wasmUrl`/`useWasm`: pdfjs-dist 4.10.38 ships no `iccs/` or `wasm/` directories.

### F6 — verification — DONE
- [x] `go build ./...`
- [x] `go vet ./...`
- [x] `go test ./... -count=1` → `pdf`, `pdf/engine`, `doc/engine`, `jobs` pass.
- [x] `npx tsc -p tsconfig.spec.json --noEmit`
- [x] `npm run build` (pre-existing bundle-budget warning only)
- [ ] Karma specs: blocked, no Chrome/Chromium installed in this environment.

## Known remaining

- `pdf-renderer.service.spec.ts` depends on `test-fixtures/CV_Ivan_Decima_EN.pdf` and
  `test-fixtures/factura_auriculares.pdf`; neither exists in the repo. Fixing it needs committed
  fixtures or an in-memory PDF generated with `pdf-lib`, and Chrome to verify.
- Packages still without tests: `app`, `image`, `preview`, `registry`, `services`, `tools`, `video`,
  `video/engine`.

## Delivery

Strategy: single branch, one commit per fix (user decision). Branch `fix/tool-suite-integrity`.
Not pushed; no PR opened.

## Progress

All five fixes implemented, committed, and verified at the Go build/vet/test and frontend
type-check/build level. 18 files changed, +1163/−113.
