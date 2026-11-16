# Differentiation Fourth Slice — Local Automation

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/local-automation`
- Started: 2026-11-16
- Plan: `docs/differentiation-roadmap.md`
- Previous slices: Email PDF, Web image set, Social clip (merged to main)
- Goal: make FileForge run recipes from a watched folder, locally and safely, with a real rules UI and last-run visibility

## Scope

1. **Automation rule model and store**
   - `AutomationRuleV1`: id, name, enabled, watch dir, recipe id, extensions, output dir, options, delete-source flag, timestamps, last-run state.
   - Persist to `os.UserConfigDir()/fileforge/automation_rules_v1.json` using the same atomic write pattern as job history.
   - Tolerant load, capped rule count, unique ids.
2. **fsnotify watcher service**
   - `github.com/fsnotify/fsnotify` v1.10.1.
   - One non-recursive watcher per enabled rule.
   - Ignore hidden/temp files, `.fileforge` paths, directories, unsupported extensions, files inside the rule’s output subfolder, and outputs produced by the rule.
   - Debounce duplicate create/write events and wait for a stable size/mtime before running.
   - Per-rule single-run concurrency; the recipe service caps global runs at 2.
   - Plan the recipe first to reserve its output paths against loops, then run and poll to terminal.
   - Persist last run/status/message/file/outputs; emit `automation/run/v1` on start and terminal.
   - Optional delete-source (default off): only on `success`, and only when the source fingerprint did not change since planning.
3. **App wiring**
   - Construct the service in `App.New`, start watchers in `ServiceStartup`, stop them in `Shutdown` before recipe shutdown.
   - A missing watch folder or fsnotify failure never aborts app startup; it surfaces as a degraded rule.
   - Endpoints: `ListAutomationRulesV1`, `SaveAutomationRuleV1`, `DeleteAutomationRuleV1`, `SetAutomationRuleEnabledV1`, `GetAutomationStatusV1`.
4. **Automations page** (`/automations`)
   - Rules list with enabled toggle, name, watch folder, recipe, output folder, extensions, last run/status/message, runtime badge.
   - Create/edit form: name, watch folder, recipe, extensions, output folder, delete-source warning, enabled.
   - Two-step delete confirmation, empty state, validation banners, action feedback.
   - Live updates from `automation/run/v1` (re-entered into Angular’s zone) with a stale-replay timestamp guard.
5. **Home status**
   - Fetch `GetAutomationStatusV1`; when rules exist, show “N of M rules active · last run …” with a link to Automations.
6. **Navigation**
   - Automations entry in the System nav group, not in `TOOLS`; the Home “12 tools” count is unchanged.

## Explicit decisions

- **One file = one run.** A rule runs the recipe once per arriving file. Email PDF rules process each PDF separately; multi-file merge is a manual recipe flow.
- **Default output is the recipe default** (next to the source) when the rule’s output folder is empty. An explicit output folder must already exist and be writable.
- **Loop prevention** is output-path based: planned outputs and actual outputs are added to a time-limited skip set; events inside a distinct output subfolder are ignored.
- **Stability window** is 1 second in production (test-configurable). Duplicate events reset the timer; in-flight files are not scheduled twice.
- **No recursive watch**, no file-size limit, no per-rule target-size UI in this slice; the rule passes `options` through to the recipe.
- **No app startup failure** from a broken rule; the rule is listed as degraded with a reason.
- **Delete source remains off by default** and is never applied to failed/partial runs or modified files.
- **Recipe jobs are in-memory**, so a restart loses an in-flight automation run; the rule and its last terminal state persist.
- **Areas outside the repo are untouched**: the watcher only processes direct children of the chosen folder, and generated files are skipped by exact path.

## Acceptance criteria

1. Saving an enabled rule for a temp watch folder runs the selected recipe exactly once when a matching file is written; duplicates and partial writes do not double-run.
2. Outputs are collision-safe and the rule records last status/message/file/outputs in the JSON store.
3. A generated output that matches the rule extension does not trigger a second run (loop prevention).
4. Disabling a rule stops processing; deleting it removes the watcher and the rule; re-enabling starts a fresh watcher.
5. A missing watch folder, unknown recipe, unsupported extension, or non-existent output folder shows a clear validation/degraded state and does not crash startup.
6. The `/automations` page can create, edit, enable/disable and delete rules, shows last-run state, and uses the real folder/recipe pickers and design tokens.
7. Home shows the automation status only when rules exist; the tool count remains 12.
8. Delete-source (when enabled) removes the source only after a successful run with an unchanged fingerprint.
9. Go tests, frontend specs and build pass; bindings are regenerated, never hand-edited.
10. No regressions in recipes, the 12 tools, undo, or navigation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: create a rule in `/automations`, drop a file into the watched folder, verify last run/output, disable and delete; Home status card; dark/light themes.

## Progress

- 2026-11-16: Slice scoped after mapping app lifecycle, recipe invocation, config persistence, event emission and navigation. fsnotify v1.10.1 available.
- 2026-11-16: Backend implemented: rule models, atomic store, validation, fsnotify watcher (stability, debounce, loop prevention, delete-source safety, degraded state), recipe plan/run/status integration, App lifecycle and endpoints, bindings. Commit `311a29e`.
- 2026-11-16: Real integration test: a rule watches a temp folder and runs the real Email PDF recipe end-to-end with a generated PDF. Commit `9e6e952`.
- 2026-11-16: Frontend implemented: Automations page (list, create/edit, toggle, two-step delete, live run events), Wails automation API/event stream, System navigation entry, Home status card, specs. Commit `6e2ad5e`.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass, including 24 automation tests (store, validation, watcher, delete-source, shutdown) and the real Email PDF watcher integration test.
- `go test ./internal/automation/... -race` — pass (reported by the implementation pass).
- `cd frontend && npm run build:dev` — success. Pre-existing budget warnings; `home.css` 7.72 kB and `automations.css` 4.22 kB, both under the 8 kB error cap.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 419/419 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual check at 1440×900:
  - `/automations` empty state, summary and create form render correctly in dark and light themes;
  - System navigation includes Automations;
  - preview mode cannot exercise IPC, so the page shows the safe “Open the desktop app” message instead of raw runtime HTML.
- Pending: desktop end-to-end in the Wails window (create a rule, drop a file, confirm last run/output, disable/delete). The backend integration test runs real fsnotify + the real Email PDF recipe, so the IPC path is the remaining unverified link.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated | watcher tests with duplicate/partial writes and the real Email PDF integration test |
| 2 | Automated | store last-run tests + integration test output path |
| 3 | Automated | loop-prevention watcher test |
| 4 | Automated | enable/disable/delete/shutdown watcher tests |
| 5 | Automated | validation tests + degraded-start test |
| 6 | Automated + visual | automations page specs (CRUD/toggle/delete/live event) and visual check |
| 7 | Automated | Home status spec; 12-tool assertions unchanged |
| 8 | Automated | delete-source tests (off by default, success only, fingerprint guard) |
| 9 | Verified | Go tests, 419 Karma specs, build, tsc; bindings regenerated only |
| 10 | Verified | Full Go and frontend suites, including recipes/tools/undo/navigation |

## Next steps

1. Run the desktop end-to-end pass with a real folder and record the result.
2. Merge the branch when the user requests it.
3. Next roadmap areas: Clean scan (OCR), Archive document (needs LibreOffice), or automation polish (recursive watch, per-rule options, run history).
