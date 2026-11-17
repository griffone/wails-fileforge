# Local Capabilities + Automation History — Polish Slice

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/local-capabilities-polish`
- Started: 2026-11-17
- Plan: `docs/differentiation-roadmap.md`
- Goal: make optional runtime capabilities and background automation observable from inside the app

## Scope

1. **Local capability detection**
   - New `internal/capabilities` package checks:
     - `pdftoppm` — OCR rasterizer (required for OCR)
     - `tesseract` — OCR engine plus installed language data (required for OCR)
     - `pdftotext` — OCR text verification (optional)
     - `gs` — Ghostscript PDF compression (recommended for target sizes)
     - `ffmpeg` / `ffprobe` — video tools (required for video)
     - `libreoffice` — DOCX → PDF/A (optional)
     - `pandoc` — DOCX fallback (optional)
   - Response includes per-check id, label, availability, required/optional flag, detail and suggestion, plus OCR languages and recommended language.
   - `GetLocalCapabilitiesV1` App endpoint.
2. **Settings capability panel**
   - New “Local capabilities” panel with refresh, status dots, detail/suggestion lines and OCR language chips.
   - Loading/error states; no automatic downloads.
3. **Automation run history**
   - New persistent run store at `os.UserConfigDir()/fileforge/automation_runs_v1.json`, capped at 100 entries, newest first, atomic writes.
   - `AutomationRunRecordV1`: id, rule id/name, file path, status, message, outputs, timestamp.
   - Every terminal automation run is recorded through the service’s existing `recordRun` path.
   - Endpoints `ListAutomationRunsV1(limit)` and `ClearAutomationRunsV1`.
4. **Automations page history**
   - “Recent runs” panel under the rules: status badge, rule name, file, relative time, message, output summary.
   - Live terminal events prepend a run locally; refresh loads the persisted list.
   - Two-step clear history.
5. **Tests and docs**
   - Capability detection with fake lookpath/OCR provider; run store cap/order/persistence; service records terminal runs; page specs.
   - Task doc and AGENTS.md updated where needed.

## Explicit decisions

- **No versions in the panel.** Availability and guidance matter more than parsing `--version`; avoids slow/inconsistent output.
- **Tesseract availability requires language data.** A binary with no non-`osd` language cannot run OCR, so the check is unavailable with guidance.
- **Optional checks are labeled.** Missing optional tools do not look like app failures.
- **Run history is separate from rule state.** Rules keep their last run; history keeps the last 100 terminal runs and can be cleared independently.
- **No network/model downloads.** Guidance only.
- **History is not a job log.** It is a compact automation audit list; outputs remain undoable through the shared job history.

## Acceptance criteria

1. `GetLocalCapabilitiesV1` reports installed/missing tools and OCR languages accurately in this environment (LibreOffice/Pandoc missing; OCR languages afr).
2. Settings shows the capability panel with available/optional/missing states, guidance and refresh.
3. Every terminal automation run is persisted with rule/file/status/message/outputs/time; the store survives restart and caps at 100.
4. The Automations page shows recent runs, updates on live terminal events, and clears with confirmation.
5. Go tests, frontend specs and build pass; bindings regenerated only.
6. No regressions in OCR, automation, recipes, tools, undo or navigation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: Settings capability panel; Automations recent runs/clear; dark/light.

## Progress

- 2026-11-17: Slice scoped after the OCR/automation work. The environment reports afr-only OCR and missing LibreOffice/Pandoc, which the panel surfaces honestly.
- 2026-11-17: Backend implemented: capabilities package/endpoint, automation run store/service recording/history endpoints, tests and bindings. Commit `dc09594`.
- 2026-11-17: Frontend implemented: Settings capabilities panel, Automations recent runs/clear, Wails types/methods and specs. Commit `71e2b1c`.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass, including 6 capability tests, 7 run-store tests and 2 service history tests; race run on the new automation tests reported clean.
- `go vet ./internal/capabilities/... ./internal/automation/... ./internal/app/... ./internal/pdf/... ./internal/recipes/...` — clean.
- Runtime capability probe on this machine: OCR `afr` (recommended `afr`), LibreOffice and Pandoc missing with suggestions; pdftoppm/tesseract/pdftotext/gs/ffmpeg/ffprobe available.
- `cd frontend && npm run build:dev` — success. Existing budget warnings only; `automations.css` is 5.37 kB, under the 8 kB error cap.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 439/439 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual checks:
  - Settings shows the Local capabilities panel between Appearance and History; in preview mode it shows the friendly “Open FileForge in the desktop app” error and Retry instead of raw IPC HTML.
  - Automations shows the Recent runs panel with count, empty state and disabled Clear history; preview mode shows a friendly history error.
  - Existing pages/navigation unchanged.
- Pending: desktop Wails pass to see the live capability rows, refresh, real recent runs and clear history. The backend uses real runtime probes and the tests cover the data paths.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated + probed | 6 capabilities tests; runtime probe on this machine |
| 2 | Automated + visual | Settings specs and panel layout check |
| 3 | Automated | Run store tests (order/cap/persistence/clear) + service tests |
| 4 | Automated + visual | Automations specs (load/live prepend/clear) and panel layout check |
| 5 | Verified | Go tests, 439 Karma, build, tsc; bindings regenerated only |
| 6 | Verified | Full Go/frontend suites including OCR, automation, recipes and navigation |

## Next steps

1. Run the desktop pass to see the actual capability rows and run history in the native app.
2. Merge the branch when the user requests it.
3. Remaining roadmap areas: Archive document (needs LibreOffice), transcription (needs whisper + model), automation polish (recursive watch, per-rule options), optional language pack guidance.
