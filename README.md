# FileForge Desktop

A local-first file workbench for Windows, macOS and Linux. FileForge wraps a growing set of conversion and editing tools in a single desktop app: image convert/crop/annotate/compress/watermark/OCR/background removal, PDF merge/split/crop/optimize/OCR/from-images/to-DOCX, Markdown and DOCX to PDF, and video convert/trim/merge/transcribe, plus curated recipes.

Everything runs on your machine. There are no uploads, accounts, or cloud calls. Background removal is an optional local pack: Settings → Tools & Models downloads its engine and model into the managed toolchain, and images never leave the machine.

## Tools

| Domain | Tools |
| --- | --- |
| Image | Convert (PNG/JPG/WebP/GIF), Crop, Annotate (text, arrow, box, blur, redact), Compress to target size, Watermark (text/logo), OCR, Remove background — single and batch |
| PDF | Merge (explicit order), Split (pages, ranges, page count or bookmarks), Crop (presets or margins), Optimize (compression + metadata), OCR (searchable PDF + text sidecar), Images → PDF, PDF → DOCX |
| Documents | Markdown → PDF (optional header/footer, single and batch), DOCX → PDF (LibreOffice + Pandoc fallback, single and batch) |
| Video | Convert, Trim, Merge, Transcribe (Whisper) |

Every batch-capable tool plans one output per input and reports partial success per item; Merge, Images → PDF, and PDF → DOCX stay single-job operations.

## Background tasks

Tool jobs and recipe runs keep running when you switch tools. The Activity button in the topbar shows how many tasks are running and opens a drawer with their stage, progress, elapsed time, ETA, cancel and open actions. When a task finishes, the app shows an in-app toast and, if the window is in the background, a desktop notification. Recently finished jobs survive an app restart.

The drawer is event-driven: it seeds once from `ListJobs` and then follows `jobs/progress`, `jobs/state`, `recipes/progress` and `recipes/state` events. Pages subscribe to the same events instead of polling, and the elapsed-time label ticks locally without touching the backend.

## Stack

- **Backend:** Go 1.26 + Wails v3.1 (stable), typed Wails services and generated TypeScript bindings
- **Job engine:** one queue with per-class concurrency, cancellation, retries, persistence and typed events
- **Frontend:** Angular 21, standalone components, TypeScript strict
- **Styling:** Tailwind CSS v4 + a token-based design system (`frontend/src/styles.css`)
- **Icons:** `lucide-angular`
- **Fonts:** Archivo Variable, Instrument Serif, IBM Plex Mono (bundled via `@fontsource`, no CDN)
- **Tests:** Karma + Jasmine (580 specs)

## Quick start

```bash
# Install frontend dependencies
cd frontend && npm install

# Run the desktop app in dev mode (frontend + Wails)
task dev

# Production build
task build

# Windows installer + portable ZIP (cross-build from Linux)
task build:windows
```

The Windows build and the Windows install layout are documented in
[docs/windows-build.md](docs/windows-build.md).

`task dev` delegates to the `wails3` binary from `PATH`. Install the CLI once with:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Then run it directly when needed:

```bash
wails3 dev -config ./build/config.yml -port 9245
```

### Frontend only

```bash
cd frontend
npm run dev        # Angular dev server on :4200
npm run build:dev  # development build
npm run build      # production build
```

The Angular dev server is useful for UI work, but Wails IPC is unavailable there. Pages fall back to the static tool registry and show a `Preview only` runtime state.

## Tests

```bash
task verify           # go build ./..., go vet ./..., go test ./...
task verify:frontend  # npm run build + Karma (580 specs)
task verify:all       # both gates
```

`task verify:frontend` runs `scripts/verify-frontend.sh`, which resolves a Chromium-based browser automatically: it honors `CHROME_BIN` when set and otherwise looks for `brave`, `brave-browser`, `chromium`, `chromium-browser`, `google-chrome`, or `google-chrome-stable` on `PATH`. Karma falls back to the Chromium bundled by Puppeteer when one is installed.

Override the browser explicitly when needed:

```bash
CHROME_BIN=/usr/bin/brave task verify:frontend
```

Frontend only:

```bash
cd frontend
npm run build
npx ng test --watch=false
```

Run one spec:

```bash
cd frontend
npx ng test --watch=false --include src/app/components/home/home.spec.ts
```

Go tests:

```bash
go test ./... -count=1
go test -race ./internal/app ./internal/jobs ./internal/pdf/... -count=1
```

## UI architecture

The app is a single workbench shell with a persistent navigation rail, a command palette, and tool pages built from one composition recipe.

- **Design system:** `frontend/src/styles.css` — semantic tokens for dark/light themes, typography, surfaces, buttons, fields, banners, badges and page composition. Existing Tailwind gray/blue/amber/red/slate utilities are remapped to these tokens, so every screen follows the active theme.
- **Shell:** `frontend/src/app/app.ts|html` — brand rail grouped by domain, top bar with breadcrumb, `Ctrl/Cmd+K` palette trigger, theme toggle and runtime status.
- **Command palette:** `frontend/src/app/components/command-palette/` — recent tools, grouped results, full keyboard navigation.
- **Tool registry:** `frontend/src/app/config/tools.ts` + `services/tool-registry.service.ts` — the single source of truth for routes, names, descriptions, icons, formats and keywords.
- **Theme:** `frontend/src/app/services/theme.service.ts` — persists `ff-theme`, follows the OS on first load.
- **Recent tools:** `frontend/src/app/services/recent-tools.service.ts` — last four destinations, persisted locally.
- **Shared components:** `components/file-drop`, `components/file-queue`, `components/output-plan-card`, `components/result-card`, `components/job-card`.
- **File-first tool flow:** drop/choose files → file cards → preset or advanced outcome → `OutputPlanCard` with the exact destination → one primary action → `ResultCard` with open/reveal/retry. Backend `PlanJob` is the source of truth; there is no separate Validate step and no free-text output path.
- **Page recipe:** `.page` → `.page-head` (eyebrow, title, subtitle) → `.panel` sections → `.field`/`.input` forms → `.banner` messages → `.btn` actions.

See [`docs/ui-ux.md`](docs/ui-ux.md) for the token contract, page conventions and a checklist for adding a tool.

## Project structure

```
main.go                          Wails bootstrap, service registration, asset embedding
internal/
  app/                           Composition root + Wails services
    tool_service.go              Catalog, plan, run, cancel, status, verify, history, undo/restore
    recipe_service.go            Recipe list, plan, run, status, cancel
    automation_service.go        Rules CRUD, enable/disable, status, runs
    toolchain_service.go         Toolchain status, install, cancel
    preview_service.go           Image/PDF/video preview jobs
    system_service.go            Dialogs, open/reveal, inspect, media, notifications
    events.go, eventbus.go       Typed event registration and injection
  jobs/                          Queue, scheduler, job state machine, persistence
  recipes/                       Recipe definitions and step execution on the job engine
  automation/                    Watchers and rule/run stores
  preview/                       Preview jobs and cache
  toolchain/                     Installer, pinned downloads and layout
  media/                         Token registry and streaming server
  tools/                         Tool contract, shared helpers, per-domain implementations
  platform/                      Shared adapters
    fsx/                         Atomic file ops, unique paths, probes, path keys
    execx/                       Bounded process runner, process groups, timeouts
    jsonstore/                   Atomic JSON store with a debounced writer
    logx/                        Structured logging and correlation IDs
    progress/                    Coalescing progress reporter
    eventbus/                    Event bus contract used below the Wails layer
  models/                        Wire DTOs
frontend/src/app/
  app.ts|html|css                Workbench shell
  config/tools.ts                Tool registry data
  services/                      IPC wrappers, job/recipe observers, theme, registry
  components/                    Shell palette + tool pages + shared UI
  styles.css                     Design system and global styles
frontend/bindings/               Generated Wails bindings (do not edit)
scripts/                         Build and verification helpers
docs/                            Product and UI documentation
odd/tasks/                       Organic development task tracking
```

## Generated bindings

`frontend/bindings/` is generated. Never edit it by hand. After changing a Go-exposed API:

```bash
wails3 generate bindings -clean=true -ts -i -names
```

## License

See the repository for licensing information.
