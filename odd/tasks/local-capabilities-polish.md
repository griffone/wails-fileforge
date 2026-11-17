# Local Capabilities + Automation History — Polish Slice

- Status: in progress
- Branch: `feat/local-capabilities-polish`
- Started: 2026-11-17
- Plan: `docs/differentiation-roadmap.md`
- Goal: make optional runtime capabilities and background automation observable from inside the app

## Scope

1. **Local capability detection**
   - New `internal/capabilities` package that checks:
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
   - New “Local capabilities” panel with a refresh action, status dots, detail/suggestion lines and OCR language chips.
   - Loading/error states; no automatic downloads.
3. **Automation run history**
   - New persistent run store at `os.UserConfigDir()/fileforge/automation_runs_v1.json`, capped at 100 entries, newest first, atomic writes.
   - `AutomationRunRecordV1`: id, rule id/name, file path, status, message, outputs, timestamp.
   - Record every terminal automation run (success, partial, failed, cancelled) in the service’s existing `recordRun` path.
   - Endpoints `ListAutomationRunsV1(limit)` and `ClearAutomationRunsV1`.
4. **Automations page history**
   - “Recent runs” panel under the rules: status badge, rule name, file, relative time, message, output count.
   - Live terminal events prepend a run locally; refresh loads the persisted list.
   - Two-step clear history.
5. **Tests and docs**
   - Capability detection with a fake lookpath/OCR provider.
   - Run store cap/order/persistence; service records terminal runs; page specs.
   - Update the task doc and `AGENTS.md` where needed.

## Explicit decisions

- **No versions in the panel.** Availability and guidance matter more than parsing `--version`; avoids slow or inconsistent output.
- **Tesseract availability requires language data.** A binary without any non-`osd` language cannot run OCR, so the check is unavailable and tells the user how to fix it.
- **Optional checks are labeled.** Missing optional tools do not look like app failures.
- **Run history is separate from rule state.** Rules keep their last run; the history store keeps the last 100 terminal runs and can be cleared independently.
- **No network/model downloads.** Guidance only.
- **History is not a job log.** It is a compact automation audit list; outputs remain undoable through the shared job history.

## Acceptance criteria

1. `GetLocalCapabilitiesV1` reports the installed/missing tools and OCR languages accurately in this environment (LibreOffice/Pandoc missing; OCR languages afr).
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

- 2026-11-17: Slice scoped after the OCR/automation work. Environment reports afr-only OCR and missing LibreOffice/Pandoc, which the panel should surface honestly.

## Implementation notes

- Capability package: `internal/capabilities/capabilities.go`, options with `LookPath` and an OCR capabilities provider so tests can inject.
- Automation run store: `internal/automation/runstore.go`; service gets an optional run store via `SetRunStore` to avoid breaking existing constructors.
- App endpoints return typed models; bindings regenerate.
- Settings uses `Wails.getLocalCapabilitiesV1()`; Automations uses the two new run endpoints.
- Keep the UI token-based and under the existing style budgets.
