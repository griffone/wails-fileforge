# Windows build and install layout

## One-command build

From the repository root on Linux:

```bash
task package
```

`task package` runs the Windows cross-build described here and then builds the
optimized Linux AppImage, so one command produces the Windows installer, the
portable ZIP and `bin/fileforge-desktop-x86_64.AppImage`. To build only the
Windows artifacts, run `task package-windows` (or `task build:windows`).

The Windows task runs `scripts/build-windows.sh`, which performs the complete
pipeline:

1. Checks the cross-build tools (`go`, `node`, `npm`, `wails3`, `makensis`,
   `mingw-w64`, `pkg-config`, `curl`, `python3`, `zip`).
2. Downloads and prepares the pinned **libvips 8.18.7** Windows x64 package.
3. Builds the Angular frontend.
4. Generates the Windows resources (`.syso`) and cross-compiles the `.exe` with
   cgo enabled.
5. Builds the headless CLI (`fileforge-cli.exe`) with the same cross environment
   and stages it next to the desktop executable.
6. Computes the libvips DLL closure from the executable and copies only the
   reachable DLLs next to it (`scripts/prune-windows-dlls.py`); the
   `vips-modules-*` directories are not bundled.
7. Generates the WebView2 bootstrapper when missing and builds the NSIS
   installer with the committed branded bitmaps.
8. Creates a portable ZIP with the same exe, CLI and DLLs (`zip -9`).

Artifacts:

```text
bin/fileforge-desktop-amd64-installer.exe   (~22.1 MiB)
bin/fileforge-desktop-windows-amd64.zip     (~33.2 MiB)
```

The staged bundle is about 83 MiB: the desktop executable, the 20.4 MiB CLI and
48 libvips DLLs (29.8 MiB). The sizes above are the optimized ones: the
installer uses a 64 MB LZMA dictionary and the ZIP is written with maximum
deflate. The ZIP contains a `FileForge Desktop/` folder with both executables
and the pruned DLL set. The optional libvips modules are not bundled because the
app only uses the core loaders (JPEG, PNG, WebP, GIF, TIFF, BMP); JXL,
ImageMagick, Poppler and OpenSlide are unnecessary. The version metadata comes
from `build/windows/info.json` and the `INFO_PRODUCTVERSION` define in
`build/windows/nsis/project.nsi`. The libvips
cache lives in `~/.cache/fileforge-windows`; override it with
`FILEFORGE_WINDOWS_CACHE`. The Wails CLI is resolved from `PATH`, then
`$HOME/go/bin/wails3`; override it with `WAILS3_BIN`.

The installer bitmaps under `build/windows/nsis/assets/` are generated from
`build/appicon.png` by `./scripts/generate-installer-assets.sh` (ImageMagick).
They are committed, so the Windows build never needs ImageMagick; rerun the
script only when the artwork or the installer copy changes.

### Bundle pruning

`scripts/prune-windows-dlls.py` parses the PE import tables with
`x86_64-w64-mingw32-objdump` (falling back to `objdump`), walks the transitive
DLL closure from the staged `.exe`, and copies only the reachable DLLs from the
libvips `bin` directory. It fails when a non-system import is missing from the
source. The current closure keeps 48 of 61 DLLs (29.8 MiB) and drops the 13
unreachable ones (11.7 MiB): JXL, ImageMagick, Poppler, OpenSlide, sqlite,
brotli, dicom and libvips-cpp.

## Why cgo and libvips are required

The image tools and the images-to-PDF renderer use `bimg`, which links libvips
through cgo. The Windows build therefore cannot use `CGO_ENABLED=0`. The exe
loads `libvips-42.dll` at startup, so the DLLs must sit next to the executable.
The in-app toolchain cannot install libvips retroactively because the process
has already loaded its imports.

The installer bundles:

- `fileforge-desktop.exe`
- `fileforge-cli.exe` (the headless CLI, installed in the same folder)
- libvips DLLs (the pruned set reachable from the exe)
- the WebView2 bootstrapper
- shortcuts and the uninstaller

The installer does **not** bundle the OCR/transcription toolchain (Tesseract,
Whisper, FFmpeg, models) or the optional background removal pack. The app
downloads that on first run into the current user's LocalAppData.

## Native Windows build

The supported and tested path is the Linux cross-build above. A native Windows
build is also possible with:

- Go (matching `go.mod`), Node/npm, the Wails v3 CLI.
- `vips-dev-x64-all-8.18.7` extracted on the machine, with
  `PKG_CONFIG_PATH` pointing at its `lib/pkgconfig` directory.
- MinGW-w64 and NSIS (`makensis`) on `PATH`.
- `CGO_ENABLED=1` and `GOOS=windows`/`GOARCH=amd64`.
- `build/windows/info.json`, `build/windows/wails.exe.manifest` and
  `build/windows/nsis/project.nsi` from this repository.

Build the frontend, generate the `.syso`, run `go build -tags production` with
the libvips cgo flags, build `./cmd/fileforge-cli` into `bin/fileforge-cli.exe`
with the same environment, run `scripts/prune-windows-dlls.py` to stage the
reachable DLLs, generate the WebView2 bootstrapper and run `makensis` with
`-DLIBVIPS_DIR=<pruned stage>` and `-DFILEFORGE_CLI_BINARY=<cli exe>`. The script
contains the exact commands and is the reference implementation; the native
`build/windows/Taskfile.yml` performs the same two `go build` steps so
`task package-native` on Windows also produces an installer with the CLI.

Do **not** run `wails3 update build-assets` without re-applying the NSIS
changes: it regenerates template files and would drop the libvips bundling.

## Windows install paths

### Application

The NSIS installer defaults to a per-machine install:

```text
%ProgramFiles%\FileForge\FileForge Desktop\
  fileforge-desktop.exe
  fileforge-cli.exe
  libvips-42.dll + dependencies
  uninstall.exe
```

### Installer experience

The installer is a five-page NSIS wizard: Welcome, Components, Destination
Folder, Install and Finish. The welcome page states that everything runs
locally, with no accounts or uploads. The Components page keeps the app section
required and offers **Desktop shortcut** and **Start Menu shortcut**, both
selected by default. The destination page includes a Browse button and starts at
the previous install location, read from the `InstallDir` value stored with the
uninstall data, so an upgrade reuses the chosen folder (and a first install uses
the path above). The install page shows one friendly status line per step
(WebView2 runtime, app files, local image engine and shortcuts) with the log
hidden. The Finish page offers **Launch FileForge Desktop** and explains that
the conversion tools download on first use from Settings → Tools & Models.

The installer requests administrator rights. The portable ZIP contains the
same folder and can be extracted anywhere; keep the exe and DLLs together.

### Managed toolchain

The app installs its local tools under the current user's LocalAppData:

```text
%LOCALAPPDATA%\FileForge\toolchain\
  micromamba.exe
  env\
    Library\bin\ffmpeg.exe
    Library\bin\ffprobe.exe
    Library\bin\tesseract.exe
    Library\bin\pdftoppm.exe
    Library\bin\pdftotext.exe
    Library\bin\gswin64c.exe
    Library\share\tessdata\
    python.exe
    Scripts\pip.exe
  pdf2docx-venv\
    Scripts\python.exe
    Scripts\pdf2docx.exe
    Lib\site-packages\pdf2docx\
  rembg-venv\
    Scripts\python.exe
    Lib\site-packages\rembg\
  whisper\
    whisper-cli.exe
    ...DLLs
  models\
    ggml-medium.bin        (default)
  rembg\
    .ready
    models\
      isnet-general-use\
        isnet-general-use.onnx
  tessdata\
    eng.traineddata
    spa.traineddata
    configs\pdf
    configs\txt
    pdf.ttf
  manifest.json
  staging\
  mamba-root\
```

Notes:

- `TESSDATA_PREFIX` is set by the app for its child processes to
  `%LOCALAPPDATA%\FileForge\toolchain\tessdata`. It is not a permanent system
  environment variable.
- The app also prepends the conda `Library\bin`, `Scripts` and `bin` folders,
  plus `toolchain\whisper`, to the child-process `PATH`.
- The managed `tessdata` deliberately includes `configs\pdf`, `configs\txt` and
  `pdf.ttf`. Without them Tesseract exits successfully but writes no searchable
  PDF; the installer and the toolchain readiness check now guarantee them.
- The default Whisper model is medium (`ggml-medium.bin`, about 1.5 GB). Small
  and large-v3 can be installed from Settings → Tools & Models.
- DOCX to PDF uses a renderer bundled in the application binary (pure Go, no
  office suite). Ghostscript, already present in the managed toolchain, is used
  only for PDF/A post-processing. Complex desktop-publishing layouts, exact
  floating-shape wrapping and some SmartArt diagrams may differ from Word; the
  tool advertises standard fidelity, not pixel-identical output.
- The PDF to DOCX engine uses the conda Python 3.11 only as its base
  interpreter. pdf2docx and its dependencies live in the isolated
  `pdf2docx-venv` (about 350 MB) with pinned versions; they are never installed
  into the conda environment. The engine is optional: if it is missing, only
  PDF to DOCX is degraded and Settings → Tools & Models can repair it.
- pdf2docx is MIT-licensed; its PDF parser PyMuPDF is AGPL-3.0 (or a commercial
  Artifex license). The dependency is downloaded into the user's toolchain at
  install time, not embedded in the application binary.
- The background removal pack is optional and downloaded at runtime; the
  installer bundles neither Python/rembg nor the model. Installed, it is about
  630 MB (roughly 450-550 MB for the venv plus the 171 MB model). A missing pack
  degrades only Remove Background.
- The `rembg-venv` uses the conda Python 3.11 only as its base interpreter. The
  pinned `rembg[cpu]==2.0.87`, `onnxruntime==1.32.0` and their transitive pins
  live only in that venv, never in the conda environment.
- Background removal child processes receive `REMBG_HOME` and `U2NET_HOME`
  pointing at the managed `rembg` folder, overriding inherited values. The
  engine forces `CPUExecutionProvider` and refuses to start without the managed
  model, so rembg/pooch cannot download weights at run time.
- The default `isnet-general-use` model is Apache-2.0. The non-commercial
  `bria-rmbg` model is deliberately not used.
- `onnxruntime` requires the Microsoft Visual C++ 2019+ redistributable on
  Windows.
- Readiness requires the venv interpreter, the rembg package, onnxruntime, a
  non-empty `.ready` marker and the exact model size. Settings Install/repair
  removes only the venv and keeps the model.
- The toolchain is per user. On a shared machine each Windows user downloads
  their own copy.

### Configuration and history

```text
%APPDATA%\fileforge\job_history.json
%APPDATA%\fileforge\jobs.json
%APPDATA%\fileforge\recipe_runs.json
%APPDATA%\fileforge\automation_rules.json
%APPDATA%\fileforge\automation_runs.json
```

Legacy `*_v1.json` files from earlier versions are copied to the unversioned
names on first load and kept on disk as a backup.

WebView2 stores its profile under `%APPDATA%\fileforge-desktop.exe`. The
uninstaller removes that folder together with the matching LocalAppData
variants; it never removes the shared Microsoft Edge WebView2 runtime.

Job scratch directories are created under `%TEMP%\fileforge-*` and removed when
the job finishes or is cancelled. Outputs are written where the user chooses;
the default is next to the input file.

## First run

The app installs the local toolchain automatically when needed. You can re-run
it from **Settings → Tools & Models → Install / repair**. The first install
needs internet access and downloads:

- micromamba
- the conda environment with FFmpeg, Tesseract, Poppler, Ghostscript and Python 3.11
- the isolated pdf2docx venv
- the optional background removal venv and pinned model
- whisper.cpp and the default model
- the Tesseract language data

Expect roughly 3 GB of disk usage, or about 3.6 GB with the background removal
pack; most of it is the medium Whisper model and the Python environments. After
that, conversion and transcription run locally.

## Uninstall and cleanup

Uninstall from **Settings → Apps → Installed apps → FileForge Desktop** or run
`uninstall.exe` from the install folder. The uninstaller is a four-page wizard
(Welcome, Confirm, Uninstall and Finish). The confirmation page warns that the
following data for the current Windows user is removed:

```text
%ProgramFiles%\FileForge\FileForge Desktop\    app, CLI, libvips DLLs, uninstaller
%LOCALAPPDATA%\FileForge                        managed toolchain and models
%LOCALAPPDATA%\fileforge                        local cache fallback
%APPDATA%\fileforge                             settings, job history, automation, trash
%APPDATA%\fileforge-desktop.exe                 WebView2 profile
%LOCALAPPDATA%\fileforge-desktop.exe            WebView2 profile variant
%LOCALAPPDATA%\fileforge-desktop.exe.WebView2   WebView2 profile variant
%TEMP%\fileforge-previews                       preview cache
```

Shortcuts and the uninstall registry key are removed as well. The managed
toolchain covers FFmpeg, Tesseract, Poppler, Ghostscript, Python, Whisper, the
Whisper models and the optional background-removal pack. App data covers the
settings, job history, recipe runs, automation rules and runs, and the app trash.

Converted files are never touched: they live wherever you saved them. The
Microsoft Edge WebView2 runtime is shared with other applications and stays
installed. If the app is running, the installer and the uninstaller ask you to
close it before continuing.

## Troubleshooting

- **The exe does not start / missing libvips DLL**: use the installer or the
  portable ZIP, not the bare `fileforge-desktop.exe`. The DLLs must be in the
  same folder.
- **`fileforge-cli.exe` does not start / missing libvips DLL**: run it from the
  install folder or the extracted portable ZIP. It links the same libvips stack
  and loads `libvips-42.dll` from its own folder, so it cannot be copied out on
  its own.
- **WebView2 missing**: the installer runs the WebView2 bootstrapper. On an
  offline machine, install the Evergreen WebView2 Runtime first.
- **OCR fails with a merge error**: open Settings → Tools & Models and run
  Install / repair. The `tesseract-pdf` component checks `configs\pdf`,
  `configs\txt` and `pdf.ttf`.
- **PDF to DOCX says the engine is not installed**: run Settings → Tools &
  Models → Install / repair. The installer recreates `pdf2docx-venv` with the
  pinned packages; check the network/proxy if the pip download fails. A failed
  venv install does not block the rest of the app.
- **PDF to DOCX output differs from the PDF**: text-based PDFs reconstruct
  paragraphs, styles, images and tables. Scanned PDFs need PDF OCR first;
  multi-column layouts, equations and forms may need manual cleanup.
- **Remove Background is disabled**: open Settings → Tools & Models and read the
  `rembg` and `rembg-model` component details. Run Install / repair. If the
  engine installs but cannot start, install the Microsoft Visual C++ 2019+
  redistributable and check that antivirus/EDR is not quarantining the
  `onnxruntime` DLLs under `rembg-venv\Lib\site-packages\onnxruntime\`.
- **First-run downloads fail**: check the proxy/firewall. The downloads come
  from GitHub, conda-forge and Hugging Face. Re-run Install / repair.
- **SmartScreen warns**: the installer is unsigned. Code signing is a release
  follow-up: sign the exe, installer and uninstaller with `signtool` (the NSIS
  script has commented `!finalize`/`!uninstfinalize` hooks). Unsigned builds are
  fine for local testing and internal distribution.
- **Windows ARM64**: not built. The toolchain currently supports
  `windows/amd64`.
