# Managed Toolchain Installer + Global Settings — Slice

- Status: implemented on branch; pending native overlay pass and merge
- Branch: `feat/toolchain-installer` (stacked on `feat/transcription-whisper`)
- Started: 2026-11-17
- Goal: on first run (AppImage or Windows), install the local tools and a good Whisper model into the app data directory with a visible progress bar, without admin rights, system PATH changes or hardcoded user paths; expose a global Settings panel for tools, models and language packs.

## Product requirements

1. First run must install:
   - whisper.cpp `whisper-cli` + a good multilingual Whisper model (default `medium`; small/large selectable).
   - Tesseract + English/Spanish language data.
   - ffmpeg/ffprobe, pdftoppm/pdftotext (Poppler) and Ghostscript.
   - LibreOffice is explicitly **not** installed.
2. A blocking first-run progress screen must say “Installing tools” and show the current phase and a progress bar. Failures show a clear error, Retry, and an option to continue with whatever is already installed.
3. The installer must work on Linux (AppImage) and Windows without hardcoded paths, admin rights or system-wide changes. macOS should follow the same design.
4. Global Settings must expose:
   - Tools & Models: status, install/repair, progress, model quality, OCR languages, storage location.
   - Transcription: model/language defaults and paths.
   - Existing Output, Appearance, Local capabilities and History sections.
5. Subsequent launches verify the installed toolchain and skip installation when ready.

## Architecture

### Managed data directory (no hardcoded user paths)

- Windows: `%LOCALAPPDATA%\FileForge` (`os.UserCacheDir`/known folder semantics; local, not roaming).
- Linux: `${XDG_DATA_HOME:-$HOME/.local/share}/fileforge`.
- macOS: `~/Library/Application Support/FileForge`.
- Layout:
  ```
  toolchain/
    micromamba(.exe)
    env/                    # micromamba prefix
    whisper/                # extracted prebuilt release, contains whisper-cli(.exe)
    models/ggml-medium.bin  # selected model(s)
    tessdata/eng.traineddata, spa.traineddata
    manifest.json           # installed components, versions, model, timestamp
  ```
- The app prepends the managed `env` bin dirs and `whisper/` to the process `PATH` and sets `TESSDATA_PREFIX`. No system PATH or registry changes.

### Portable installers

- **micromamba** (static portable package manager) installs into the managed prefix:
  `micromamba create -y -p <env> -c conda-forge ffmpeg tesseract poppler ghostscript`
  - Linux binary: `micromamba-linux-64`.
  - Windows binary: `micromamba-win-64.exe`.
  - No admin; everything under the app data dir. This avoids per-OS package managers and portable-binary hunting.
- **whisper.cpp prebuilt** release asset (pinned):
  - Linux x64: `whisper-bin-ubuntu-x64.tar.gz`.
  - Windows x64: `whisper-bin-x64.zip`.
  - Extract/flatten into `toolchain/whisper` (shared libs/DLLs stay beside the CLI).
- **Models** from Hugging Face whisper.cpp:
  - `ggml-medium.bin` (~1.5 GB, default “good quality”),
  - `ggml-small.bin` (~466 MB, lighter download),
  - `ggml-medium.bin` (~1.5 GB, high quality),
  - `ggml-large-v3.bin` (~3.1 GB, maximum quality).
- **Tesseract languages** from `tessdata_fast`: `eng.traineddata`, `spa.traineddata`.
- **No LibreOffice/Pandoc installation.** They remain optional system diagnostics.

### Install phases and progress

- `micromamba` download 0–5% (byte progress).
- environment creation 5–40% (indeterminate animation; phase message lists packages).
- whisper download/extract 40–50% (byte progress).
- model download 50–93% (byte progress; resumable where possible).
- tessdata downloads 93–100% (byte progress).
- Events `toolchain/install/v1` with `phase`, `percent`, `indeterminate`, `message`, `done`, `error`.
- Status endpoint persists the current install state so a late/subscribed UI can render it immediately.
- Cancel kills the active command/download, cleans temp/staging files and marks the install cancelled.

### Backend

- New `internal/toolchain` package:
  - platform resolution and data dir,
  - manifest/status,
  - HTTP downloader with progress,
  - safe zip/tar.gz extraction,
  - micromamba env creation,
  - whisper/model/tessdata installation,
  - environment application (`PATH`, `TESSDATA_PREFIX`),
  - install state machine and events.
- New models: `ToolchainComponentV1`, `ToolchainStatusV1`, `ToolchainInstallOptionsV1`, `ToolchainInstallEventV1`.
- App endpoints:
  - `GetToolchainStatusV1()`
  - `StartToolchainInstallV1(options)`
  - `CancelToolchainInstallV1()`
  - `GetToolchainInstallStateV1()`
- Startup:
  - `ServiceStartup` applies the managed environment synchronously, checks status, and starts the install automatically when required components are missing.
  - Never fail app startup; a failed install is a state with Retry.
- Capability/tool resolution:
  - The managed PATH/env is visible to all subprocesses (ffmpeg, gs, tesseract, pdftoppm, pdftotext).
  - Whisper resolver prefers `toolchain/whisper/whisper-cli` and `toolchain/models/<selected>.bin`.
  - Ghostscript on Windows may be `gswin64c.exe`; add a multi-name resolver instead of assuming `gs`.
- Tests:
  - platform path/URL selection with injected GOOS/GOARCH,
  - safe archive extraction (zip/tar.gz, traversal),
  - download progress with a fake HTTP server,
  - install orchestration with fake downloader/runner and a temp data dir,
  - status/manifest/env application,
  - real optional install test skipped unless `FILEFORGE_TOOLCHAIN_E2E=1` (network/large downloads).

### Frontend

- Wails types/methods for status/install/cancel/state and the event stream.
- First-run overlay component in the app shell:
  - shown while installing or when required components are missing,
  - “Installing local tools” title, phase list, progress bar, byte/percent text, current message,
  - error state with Retry and “Continue without tools”,
  - dismisses automatically when ready.
- Global Settings:
  - Tools & Models panel at the top: component statuses, install/repair, progress bar/cancel, model quality selector (Small default, Medium, Large), English/Spanish OCR checkboxes, storage folder + open action, refresh.
  - Transcription panel reflects the selected model and paths; no contradictory overrides.
  - Existing Output/Appearance/Local capabilities/History sections remain.
- No new tool count change; this is infrastructure.

## Explicit decisions

- **Managed prefix, not system install.** No admin, no registry, no system PATH changes.
- **Micromamba for portable dependencies.** It is the most reliable way to get ffmpeg/tesseract/poppler/ghostscript on Windows and Linux without per-OS packaging.
- **Prebuilt whisper + Hugging Face model.** No runtime compilation; no cloud transcription.
- **Default model medium.** The user requested a good model over a basic one; small and large-v3 remain selectable in Settings.
- **Download at first run, not bundle.** Keeps the AppImage/installer small; the progress screen makes the download explicit.
- **Never install LibreOffice.**
- **TLS only.** Downloads use HTTPS; checksums are recorded where practical.
- **Atomic-ish installs.** Downloads/extractions go to staging; the manifest marks a component ready only after verification.

## Acceptance criteria

1. Fresh data dir + first launch starts the installer automatically and the UI shows “Installing tools” with a progress bar and changing phase messages.
2. The installer creates a working micromamba env with ffmpeg/ffprobe, tesseract, pdftoppm/pdftotext and Ghostscript; a good Whisper model and eng/spa tessdata are installed.
3. Transcription works with the managed whisper/model; OCR works with managed tesseract/eng/spa; video/PDF tools find managed ffmpeg/gs/poppler.
4. No hardcoded user paths; the same code resolves Windows/Linux/macOS data dirs and executable names.
5. A failed download shows an actionable error, Retry and a continue option; no app crash, no partial tool marked ready.
6. Subsequent launches verify the manifest and skip the installer.
7. Settings shows component status, can install/repair, switch the model quality and choose OCR languages, with progress.
8. The AppImage/Windows build only needs the Go app; the toolchain is installed at first run.
9. Go tests, frontend specs and build pass; no regressions in tools/recipes/OCR/transcription/automation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual: fresh data dir install on Linux, progress/overlay, retry/cancel, Settings repair/model switch, subsequent launch skip.
- Windows: build/package and validate path/URL selection; full install if a Windows environment is available.

## Progress

- 2026-11-17: Design validated on Linux: micromamba 2.10.0 creates an env with ffmpeg 9.0.2, tesseract 5.5.4, poppler, ghostscript 10.08.0; the prebuilt whisper `whisper-bin-ubuntu-x64.tar.gz` transcribes JFK with the local model. URLs for Windows assets are reachable.
- 2026-11-17: Slice scoped. Backend installer first, then overlay + Settings.

## Implementation notes

- Pin the whisper release tag (`b5454`) and model URLs; use micromamba latest binary URLs.
- Use `archive/zip`, `archive/tar`, `compress/gzip` only; no new extraction dependency.
- For micromamba env creation, run the downloaded micromamba binary with `MAMBA_ROOT_PREFIX` under the toolchain dir; capture output for errors.
- Apply `PATH` with `os.PathListSeparator`; Windows bins: `env/Library/bin`, `env/Scripts`, `env/bin`; Linux: `env/bin`.
- Set `TESSDATA_PREFIX` to `toolchain/tessdata`.
- Add `gswin64c`/`gswin32c` fallbacks to the PDF Ghostscript lookup.
- Keep the installer cancelable; a canceled install must not mark components ready.
- The first-run overlay should poll `GetToolchainInstallStateV1` and subscribe to events; events may fire before the UI loads.

## Final status and verification

- Status: implemented on `feat/toolchain-installer`; frontend and backend committed (`5163113`, `c8bd938`).
- Real end-to-end install on Linux passed (`FILEFORGE_TOOLCHAIN_E2E=1`, 52.29s) into a fresh temp data dir: micromamba env (ffmpeg/tesseract/poppler/ghostscript), prebuilt whisper, the then-default small model and eng/spa tessdata, status ready. The current default is medium; the medium path is covered by the fake-downloader tests.
- Platform matrix: Linux amd64/arm64 and Windows amd64 are supported; Windows ARM64 and macOS return a clear unsupported reason instead of guessing.
- First-run behavior: startup applies the managed PATH/TESSDATA_PREFIX, starts the install when missing, and the overlay shows the progress bar/phase. Subsequent launches skip when ready.
- Global Settings: Tools & Models panel with components, model quality (medium default, small/large optional), OCR languages, install/repair, progress/cancel, storage folder and refresh. No LibreOffice installation.
- Checks: `go test ./...` pass (including toolchain, archive/downloader, env, integration, 474 Karma specs and `tsc --noEmit`). Visual Settings/Tools panel checked in preview; the overlay requires a real Tauri/Wails runtime because preview has no IPC.
- Pending: native overlay visual pass, Windows package validation, and merge.
