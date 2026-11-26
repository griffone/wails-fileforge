# FileForge Desktop

A local-first file workbench for Windows, macOS and Linux. FileForge wraps a growing set of conversion and editing tools in a single desktop app: image convert/crop/annotate/compress/watermark/OCR/background removal, PDF merge/split/crop/optimize/OCR/from-images/to-DOCX, Markdown and DOCX to PDF, and video convert/trim/merge/transcribe, plus curated recipes.

Everything runs on your machine. There are no uploads, accounts, or cloud calls. Background removal is an optional local pack: Settings → Tools & Models downloads its engine and model into the managed toolchain, and images never leave the machine.

## Tools

| Domain | Tools |
| --- | --- |
| Image | Convert (PNG/JPG/WebP/GIF), Crop, Annotate (text, arrow, box, blur, redact), Compress to target size, Watermark (text/logo), OCR, Remove background — single and batch |
| PDF | Merge (explicit order), Split (pages, ranges, page count or bookmarks), Crop (presets or margins), Optimize (compression + metadata), OCR (searchable PDF + text sidecar), Images → PDF, PDF → DOCX |
| Documents | Markdown → PDF (optional header/footer, single and batch), DOCX → PDF (bundled pure-Go renderer, single and batch) |
| Video | Convert, Trim, Merge, Transcribe (Whisper) |

Every batch-capable tool plans one output per input and reports partial success per item; PDF Merge, Video Merge, Images → PDF and PDF → DOCX are single-job operations.

Curated recipes compose the tools into outcomes: Email PDF, Web image set, Social clip, Clean scan and Burned-in subtitles. Folder automations run a recipe when a matching file arrives; a rule can watch subfolders and filter by extension and file-name patterns.

DOCX → PDF renders with the bundled pure-Go engine, so no office suite is
installed or required. PDF/A export post-processes the rendered PDF with the
managed Ghostscript. Standard business documents convert with their text,
styles, tables and images; complex desktop-publishing layouts, exact
floating-shape wrapping and some SmartArt diagrams may differ from Word.

## Background tasks

Tool jobs and recipe runs keep running when you switch tools. The Activity button in the topbar shows how many tasks are running and opens a drawer with their stage, progress, elapsed time, ETA, cancel and open actions. When a task finishes, the app shows an in-app toast and, if the window is in the background, a desktop notification. Recently finished jobs survive an app restart.

The drawer is event-driven: it seeds once from `ListJobs` and then follows `jobs/progress`, `jobs/state`, `recipes/progress` and `recipes/state` events. Pages subscribe to the same events instead of polling, and the elapsed-time label ticks locally without touching the backend.

Closing the window while a job or recipe is running keeps the process alive in the tray instead of killing the work. The tray menu lists active tasks with show and cancel actions, and the window title and tray tooltip carry the running count. An idle close quits; quitting is always explicit from the tray or the app menu.

## Workbench capabilities

- **Trash and job history:** Settings → Trash and history lists finished jobs with open, reveal, undo and restore actions. It shows the trash size read from disk and empties the trash only after an explicit confirmation.
- **Remembered options:** tool pages with durable options restore the last values used for that tool. The store is versioned per tool and validates the stored shape on read, so a corrupt or outdated payload falls back to defaults instead of breaking the form.
- **Run again:** Image Compress, PDF Optimize, DOCX → PDF and Video Transcribe store the last submitted request and offer "Run again with the same settings", which re-plans the same inputs and produces a collision-safe new output.
- **Drag reorder:** PDF Merge, Video Merge and multi-input recipes accept drag reordering. The move-up/move-down buttons remain as the keyboard-accessible fallback.
- **Document thumbnails:** DOCX and Markdown inputs render a first-page thumbnail through the same in-process document engines as the conversion tools, then the shared PDF preview pipeline (`pdftoppm`). Oversized or corrupt documents degrade to the typed "no preview" state instead of failing the card.
- **Capability panel:** Settings → Tools & Models reports every dependency it can detect — `pdftoppm`, `tesseract`, `pdftotext`, `gs`, `ffmpeg`, `ffprobe`, `libvips`, `cwebp`, `pdf2docx`, the background-removal pack and Whisper — with install guidance for each missing one.
- **One run pipeline:** all 20 tool pages use `ToolRunController` for plan → run → observe → verify → undo, and one shared status vocabulary (`frontend/src/app/utils/job-status.ts`) renders job states consistently in the job card, activity drawer, result card and Home.

## Stack

- **Backend:** Go 1.26 + Wails v3.1 (stable), typed Wails services and generated TypeScript bindings
- **Job engine:** one queue with per-class concurrency, cancellation, retries, persistence and typed events
- **Frontend:** Angular 21, standalone components, TypeScript strict
- **Styling:** Tailwind CSS v4 + a token-based design system (`frontend/src/styles.css`)
- **Icons:** `lucide-angular`
- **Fonts:** Archivo Variable, Instrument Serif, IBM Plex Mono (bundled via `@fontsource`, no CDN)
- **Tests:** 1,184 Go test functions under `internal/`; Karma + Jasmine (746 specs)

## Quick start

```bash
# Install frontend dependencies
cd frontend && npm install

# Run the desktop app in dev mode (frontend + Wails)
task dev

# Production build
task build

# Headless CLI for the host (bin/fileforge-cli)
task build:cli

# Optimized Windows installer + portable ZIP and Linux AppImage
task package

# Package only the current OS
task package-native

# Full Linux package set (AppImage, deb, rpm, aur)
task package-linux-full
```

### Version identity

`internal/version` holds `Version`, `Commit` and `BuiltAt`. Every build path
(`Taskfile.yml`, the Linux/Windows/macOS Taskfiles and
`scripts/build-windows.sh`) calls `go run ./scripts/versionflags` and passes the
printed `-X` flags to `go build`, so one helper stamps both the desktop app and
the CLI.

- **Version:** `git describe --tags --always --dirty` when git is available.
  A source tarball with no `.git` falls back to the `version` in `wails.json`.
- **Commit:** `git rev-parse --short HEAD`, or `none`.
- **Build time:** the current UTC time at build, in RFC 3339 format.

`wails.json` stays the release-facing version. `build/config.yml` and
`build/windows/info.json` mirror it for the Wails and Windows metadata; bump all
three together when cutting a release and let the git tag provide the compiled
identity. `internal/version` has a test that fails when those three copies
drift, and, when a release version is injected, when it does not match
`wails.json`. Development builds and dirty trees keep `dev`/`none`/`unknown`
and `version.IsRelease()` returns false, so a future updater can refuse to
compare them.

The Windows build and the Windows install layout are documented in
[docs/windows-build.md](docs/windows-build.md).

## Release artifacts

`task package` builds the Windows artifacts and the Linux AppImage from Linux
in one run: the optimized Windows installer and portable ZIP (both cross-built),
the optimized AppImage, and the headless CLI.

| Artifact | Size |
| --- | --- |
| `bin/fileforge-desktop-amd64-installer.exe` | ~22.1 MiB |
| `bin/fileforge-desktop-windows-amd64.zip` | ~33.2 MiB |
| `bin/fileforge-desktop-x86_64.AppImage` | ~116.2 MiB |
| `bin/fileforge-cli` | ~20.0 MiB |

`fileforge-cli` is the headless CLI built from `cmd/fileforge-cli`. It runs the
same tools and recipes as the desktop app without booting Wails. It ships per
platform:

| Platform | Where the CLI ships | Installed path |
| --- | --- | --- |
| Windows | inside the installer and the portable ZIP (`FileForge Desktop/fileforge-cli.exe`) | `%ProgramFiles%\FileForge\FileForge Desktop\fileforge-cli.exe` |
| Linux (AppImage) | `bin/fileforge-cli`, beside the AppImage | wherever the two files are copied together |
| Linux (deb/rpm/arch) | inside the package | `/usr/local/bin/fileforge-cli` |

The Linux CLI is deliberately **not** embedded in the AppImage: the generated
`AppRun` launches the desktop app only, so a second binary inside the bundle
would be unreachable. Run it from the extracted directory instead.

### MCP server

`fileforge-cli mcp serve` exposes the toolbox to MCP clients (Claude Desktop,
Cursor, OpenCode, any stdio client) with a terminal-managed folder allowlist.
The server offers six tools — `list_tools`, `plan_job`, `run_job`,
`list_recipes`, `plan_recipe`, `run_recipe` — and nothing that moves or deletes
existing files. Prefer `plan_*` before `run_*`: the plan shows the exact
destination without writing anything.

```bash
fileforge-cli mcp allow ~/Documents/Exports   # grant a folder first
fileforge-cli mcp list
```

An empty allowlist denies everything, paths are resolved through symlinks before
the check, and every run is appended to `mcp_audit.log`. No MCP tool can grant
permissions; only the terminal can. See [docs/mcp.md](docs/mcp.md) for client
configuration, the tool arguments and the limitations.

The Windows build stages the exe and copies only the libvips DLLs reachable
from it (`scripts/prune-windows-dlls.py`), writes the portable ZIP with maximum
deflate and uses a 64 MB LZMA dictionary for the installer. The CLI is built in
`scripts/build-windows.sh` with the same mingw + libvips cross environment,
staged next to the desktop exe, installed by NSIS into the same folder and
picked up by the portable ZIP. The Linux build post-processes the generated
AppImage (`scripts/optimize-appimage.sh`): it removes the unused GTK3/WebKit2GTK
4.1 stack and the GObject-introspection typelibs, then repacks with zstd. Set
`OPTIMIZE_APPIMAGE=0` to keep the generated AppImage unchanged.

### Windows installer

`bin/fileforge-desktop-amd64-installer.exe` is a branded NSIS wizard for
non-technical users: Welcome, Components, Destination Folder, Install and
Finish. It installs for all users, keeps the desktop and Start Menu shortcuts
optional (both selected by default), remembers the previous destination and
offers to launch the app on Finish. The welcome and header bitmaps are
generated by `scripts/generate-installer-assets.sh` from `build/appicon.png`;
the BMPs are committed, so ImageMagick is only needed when the artwork changes.

The uninstaller removes the application, shortcuts, the managed toolchain
(FFmpeg, Tesseract, Poppler, Ghostscript, Python, Whisper, models and the
background-removal pack), settings, job history, app trash, the preview cache
and the app's WebView2 profile for the current Windows user. Your converted
files are never deleted, and the shared Microsoft Edge WebView2 runtime stays
installed for other apps.

Builds are unsigned. Code signing with `signtool` is a release follow-up;
unsigned builds are fine for local testing and internal distribution.

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
task verify:frontend  # npm run build + Karma (746 specs)
task verify:all       # both gates
```

The backend suite currently holds 1,184 `Test*` functions under `internal/`.

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
- **Shared components:** `components/file-drop`, `components/tool-flow/file-queue`, `components/tool-flow/output-plan-card`, `components/tool-flow/result-card`, `components/job-card`, `components/activity-panel`, `components/settings/trash-history`, `components/toast-host`.
- **Run lifecycle:** `frontend/src/app/utils/tool-run-controller.ts` — one plan → run → observe → verify → undo pipeline, plus the intake, progress, cancellation and resume helpers that every tool page used to copy.
- **Status vocabulary:** `frontend/src/app/utils/job-status.ts` — the single mapping from backend job status to label and tone for the job card, activity drawer, result card and Home.
- **Option memory:** `frontend/src/app/utils/tool-options.ts` — versioned per-tool option stores on top of `PreferencesService`; `LastRequestService` keeps the last submitted request for "Run again" on the four tools that expose it.
- **File-first tool flow:** drop/choose files → file cards → preset or advanced outcome → `OutputPlanCard` with the exact destination → one primary action → `ResultCard` with open/reveal/retry and, where available, "Run again with the same settings". Backend `PlanJob` is the source of truth; there is no separate Validate step and no free-text output path.
- **Page recipe:** `.page` → `.page-head` (eyebrow, title, subtitle) → `.panel` sections → `.field`/`.input` forms → `.banner` messages → `.btn` actions.

See [`docs/ui-ux.md`](docs/ui-ux.md) for the token contract, page conventions and a checklist for adding a tool.

## Project structure

```
main.go                          Wails bootstrap, service registration, asset embedding
internal/
  app/                           Composition root + Wails services + desktop lifecycle
    tool_service.go              Catalog, plan, run, cancel, status, verify, history, undo/restore
    recipe_service.go            Recipe list, plan, run, status, cancel
    automation_service.go        Rules CRUD, enable/disable, status, runs
    toolchain_service.go         Toolchain status, install, cancel
    preview_service.go           Image/PDF/video/document preview jobs
    system_service.go            Dialogs, open/reveal, inspect, media, notifications
    events.go, eventbus.go       Typed event registration and injection
    tray.go                      Tray menu, close-to-hide, running-task count
    tray_platform_linux.go       StatusNotifierWatcher availability probe
  jobs/                          Priority queue, class limits, job state machine, persistence
  services/                      Job execution facade: plan, run, verify, history, activity
  recipes/                       Curated recipes and step execution on the job engine
  automation/                    Watchers, recursive/pattern matching and rule/run stores
  preview/                       Preview jobs, document thumbnails and cache
  toolchain/                     Installer, pinned downloads and layout
  capabilities/                  Runtime dependency detection for the Settings panel
  verify/                        Output verification and target-size checks
  jobhistory/                    Job history, undo/restore and app trash
  inspect/                       Input inspection for file cards
  outputplan/                    Collision-safe output naming
  registry/                      Tool registry instance
  media/                         Token registry and streaming server
  image|pdf|video|doc/           Per-domain tools, engines and tests
  tools/                         Tool contract and shared tool helpers
  platform/                      Shared adapters
    fsx/                         Atomic file ops, unique paths, probes, path keys
    execx/                       Bounded process runner, process groups, timeouts
    jsonstore/                   Atomic JSON store with a debounced writer
    logx/                        Structured logging and correlation IDs
    progress/                    Coalescing progress reporter
    eventbus/                    Event bus contract used below the Wails layer
  models/                        Wire DTOs
  testutil/, utils/              Test helpers and shared utilities
frontend/src/app/
  app.ts|html|css                Workbench shell
  config/tools.ts                Tool registry data
  services/                      IPC wrappers, job/recipe observers, preferences, theme, registry
  utils/                         Run controller, status vocabulary, option memory, formatting
  components/                    Shell palette, tool pages, shared tool-flow and settings panels
  styles.css                     Design system and global styles
frontend/bindings/               Generated Wails bindings (do not edit)
scripts/                         Build and verification helpers
docs/                            Product and UI documentation
odd/tasks/                       Organic development task tracking
```

## Known limitations

- **DOCX → PDF is standard fidelity, not Word-identical.** Complex desktop-publishing layouts, exact floating-shape wrapping and some SmartArt diagrams may differ. The tool manifest and the page both state "standard fidelity" (`internal/doc/tool_docx_to_pdf.go`, `doc-docx-to-pdf.spec.ts`).
- **The DOCX renderer is pinned at a pre-1.0 release.** `github.com/bobyeoh/docx2pdf-go v0.3.0` is pinned in `go.mod` and isolated behind `engine.DOCXRenderer`; PDF/A export also requires Ghostscript, and a missing `gs` is a dependency error rather than a silent downgrade.
- **PDF Merge has no page-level progress.** `pdfcpu` merges in one call and exposes no page hook, so the job reports honest per-input stages instead of a percentage (`internal/pdf/tool_merge.go`, `internal/pdf/engine/merge.go`).
- **Target sizes cover images, PDFs and video, with different guarantees.** Image Compress bisects quality and PNG is best effort; PDF Optimize tries pdfcpu then Ghostscript tiers; the video tools compute a bitrate, apply a per-resolution floor and report "not met (best effort)" honestly when the floor binds (`internal/video/engine/target.go`, `internal/pdf/engine/optimize.go`).
- **Video targets always re-encode.** Copy and remux modes cannot honour a size, so the plan switches to a two-pass encode and says so; merging measures against the summed input duration, so crossfades can land above the target.
- **Image target size is best effort.** JPEG and WebP use a quality search; PNG uses palette quantisation and cannot use the quality knob (`internal/image/tool_compress.go`).
- **The Linux tray needs a status-notifier host.** Wails exports a StatusNotifierItem, so a desktop without `org.kde.StatusNotifierWatcher` (KDE, or GNOME with the AppIndicator extension) gets no tray icon; close then quits instead of hiding (`internal/app/tray_platform_linux.go`).
- **Drag reorder is pointer-only.** The queues use native HTML5 drag events with no touch path; the move-up/move-down buttons remain the accessible and touch fallback (`components/tool-flow/file-queue.ts`).
- **Option memory covers the pages that have an option model.** Image Annotate, PDF Merge and PDF to DOCX keep per-input or per-run state by design. "Run again" is available on every tool page because the run controller stores the last request centrally (`utils/tool-options.ts`, `utils/tool-run-controller.ts`).
- **Redaction asks before it runs.** Any annotation set containing a `redact` operation goes through a blocking confirmation that states the change is irreversible (`components/image-annotate/`).
- **MCP reads its allowlist at startup.** Changing grants requires restarting the MCP client; calls are synchronous with no progress notifications or cancel tool, and there is a 50-input cap (`internal/mcp/`).
- **The CLI is not inside the Linux AppImage.** The generated `AppRun` launches the app only, so the CLI ships beside the AppImage instead of being unreachable inside it.
- **Auto-update needs a public release feed and a pinned key.** Development builds and builds without an injected public key report updates as unavailable, an AppImage replaces itself, and a deb/rpm/arch install is told to use its package manager and is never offered a download (`internal/update/gate.go`).
- **Windows auto-update is unverified.** The path downloads the signed installer and runs it after consent, but it has not been exercised on a Windows host.
- **Undo skips outputs modified after the conversion.** The history records size and modification time and refuses to move a file that no longer matches, so a user edit is never trashed (`internal/jobhistory/history.go`).

## Generated bindings

`frontend/bindings/` is generated. Never edit it by hand. After changing a Go-exposed API:

```bash
wails3 generate bindings -clean=true -ts -i -names
```

## License

See the repository for licensing information.
