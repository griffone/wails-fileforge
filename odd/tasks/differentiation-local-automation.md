# Differentiation Fourth Slice — Local Automation

- Status: in progress
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
   - `github.com/fsnotify/fsnotify` (cached v1.10.1; no network dependency needed).
   - One non-recursive watcher per enabled rule.
   - Ignore hidden/temp files, `.fileforge` paths, directories, unsupported extensions, files inside the rule’s output dir, and outputs produced by the rule.
   - Debounce duplicate create/write events and wait for a stable size/mtime before running.
   - Per-rule single-run concurrency; the recipe service already caps global runs at 2.
   - Plan the recipe first to learn its output paths and reserve them against loops, then run and poll to terminal.
   - Persist last run/status/message/file/outputs; emit `automation/run/v1` on start and terminal.
   - Optional delete-source (default off): only on `success`, and only when the source fingerprint did not change since planning.
3. **App wiring**
   - Construct the service in `App.New`, start watchers in `ServiceStartup`, stop them in `Shutdown` before recipe shutdown.
   - Never let a missing watch folder or fsnotify failure abort app startup; surface it as a degraded rule state.
   - Endpoints:
     - `ListAutomationRulesV1`
     - `SaveAutomationRuleV1`
     - `DeleteAutomationRuleV1`
     - `SetAutomationRuleEnabledV1`
     - `GetAutomationStatusV1`
4. **Automations page** (`/automations`)
   - Rules list with enabled toggle, name, watch folder, recipe, output folder, extensions, last run/status/message.
   - Create/edit form: name, watch folder, recipe, extensions, output folder, delete-source warning, enabled.
   - Two-step delete confirmation, empty state, validation banners, action feedback.
   - Live updates from `automation/run/v1` (re-entered into Angular’s zone) plus a periodic status refresh fallback.
5. **Home status**
   - Fetch `GetAutomationStatusV1`; when rules exist, show “N active · last run …” with a link to Automations.
6. **Navigation**
   - Add an Automations entry to the System navigation group; it must NOT increase the Home “12 tools” count (not part of `TOOLS`).

## Explicit decisions

- **One file = one run.** A rule runs the recipe once per arriving file. Email PDF rules process each PDF separately; multi-file merge is a manual recipe flow.
- **Default output is the recipe default** (next to the source) when the rule’s output folder is empty. An explicit output folder must already exist.
- **Loop prevention** is output-path based: planned outputs and actual outputs are added to a time-limited skip set; events inside a distinct output folder are ignored.
- **Stability window** is 1 second in production; configurable in tests. Duplicate events reset the timer.
- **No recursive watch**, no file-size limit, no per-rule target-size UI in this slice; the rule passes `options` through to the recipe.
- **No app startup failure** from a broken rule; the rule is listed as degraded with a reason.
- **Delete source remains off by default** and is never applied to failed/partial runs or modified files.
- **Recipe jobs are in-memory**, so a restart loses an in-flight automation run; the rule and its last terminal state persist.

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

- 2026-11-16: Slice scoped after mapping app lifecycle, recipe invocation, config persistence, event emission and navigation. fsnotify v1.10.1 is available in the module cache.

## Implementation notes

- Backend packages: `internal/automation` (store, service, watcher) and `internal/models/automation_v1.go`.
- `RecipeRunner` interface: `List`, `Plan`, `Run`, `Status`; `*recipes.Service` satisfies it.
- Event name: `automation/run/v1`; payload `AutomationRunEventV1`.
- UI reuses the generic page grammar and token classes; no modal component exists, so delete uses a two-step inline confirmation.
- Dynamic state (watching/degraded) is response-only; persisted rules carry only durable fields.
