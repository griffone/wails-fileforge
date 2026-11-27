#!/usr/bin/env bash
# Cross-build the FileForge Desktop Windows x64 installer from Linux.
#
# bimg links libvips through cgo, so the Windows build needs the libvips
# Windows development package and a MinGW cross compiler. The script downloads
# the pinned libvips package, builds the frontend, cross-compiles the exe,
# bundles only the libvips DLLs reachable from the exe and produces the NSIS
# installer.
#
# Requirements: go, node, npm, makensis, mingw-w64 (gcc, dlltool and binutils
# objdump), pkg-config, curl, python3, zip and the Wails v3 CLI (wails3).
# Code signing is optional: scripts/sign-windows.sh signs when a certificate is
# configured and warns otherwise. Set REQUIRE_SIGNING=1 to make it mandatory.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VIPS_VERSION="${VIPS_VERSION:-8.18.7}"
VIPS_MAJOR_MINOR="${VIPS_VERSION%.*}"
CACHE_DIR="${FILEFORGE_WINDOWS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/fileforge-windows}"
VIPS_ZIP="$CACHE_DIR/vips-dev-x64-all-$VIPS_VERSION.zip"
VIPS_DIR="$CACHE_DIR/vips-dev-$VIPS_MAJOR_MINOR"
VIPS_URL="https://github.com/libvips/build-win64-mxe/releases/download/v$VIPS_VERSION/vips-dev-x64-all-$VIPS_VERSION.zip"
STAGE_DIR="$CACHE_DIR/portable/FileForge Desktop"
INSTALLER="$ROOT_DIR/bin/fileforge-desktop-amd64-installer.exe"
PORTABLE="$ROOT_DIR/bin/fileforge-desktop-windows-amd64.zip"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

for command in go node npm makensis pkg-config curl python3 zip tar \
  x86_64-w64-mingw32-gcc x86_64-w64-mingw32-g++ x86_64-w64-mingw32-dlltool; do
  require_command "$command"
done

WAILS3_BIN="${WAILS3_BIN:-}"
if [ -z "$WAILS3_BIN" ]; then
  WAILS3_BIN="$(command -v wails3 || true)"
fi
if [ -z "$WAILS3_BIN" ] && [ -x "$HOME/go/bin/wails3" ]; then
  WAILS3_BIN="$HOME/go/bin/wails3"
fi
if [ -z "$WAILS3_BIN" ] || [ ! -x "$WAILS3_BIN" ]; then
  echo "Missing the Wails v3 CLI. Install it or set WAILS3_BIN." >&2
  exit 1
fi

echo "==> Preparing libvips $VIPS_VERSION for Windows x64"
mkdir -p "$CACHE_DIR"
if [ ! -f "$VIPS_ZIP" ]; then
  curl -L --fail -o "$VIPS_ZIP" "$VIPS_URL"
fi
if [ ! -d "$VIPS_DIR" ]; then
  rm -rf "$CACHE_DIR/extract"
  mkdir -p "$CACHE_DIR/extract"
  python3 -m zipfile -e "$VIPS_ZIP" "$CACHE_DIR/extract"
  rm -rf "$VIPS_DIR"
  mv "$CACHE_DIR/extract/vips-dev-$VIPS_MAJOR_MINOR" "$VIPS_DIR"
fi

# The MXE package omits the deprecated vips7 headers that bimg still includes.
# Prefer the matching system libvips headers; fall back to the libvips source.
HEADER_SOURCE=""
if [ -f /usr/include/vips/vips.h ]; then
  HEADER_SOURCE="/usr/include/vips"
else
  SOURCE_TAR="$CACHE_DIR/vips-$VIPS_VERSION.tar.xz"
  if [ ! -f "$SOURCE_TAR" ]; then
    curl -L --fail -o "$SOURCE_TAR" \
      "https://github.com/libvips/libvips/releases/download/v$VIPS_VERSION/vips-$VIPS_VERSION.tar.xz"
  fi
  rm -rf "$CACHE_DIR/source"
  mkdir -p "$CACHE_DIR/source"
  tar -xf "$SOURCE_TAR" -C "$CACHE_DIR/source" \
    "vips-$VIPS_VERSION/libvips/include/vips" 2>/dev/null || true
  HEADER_SOURCE="$CACHE_DIR/source/vips-$VIPS_VERSION/libvips/include/vips"
fi
if [ -d "$HEADER_SOURCE" ]; then
  for header in "$HEADER_SOURCE"/*.h; do
    name="$(basename "$header")"
    if [ ! -f "$VIPS_DIR/include/vips/$name" ]; then
      cp "$header" "$VIPS_DIR/include/vips/$name"
    fi
  done
fi

echo "==> Preparing pkg-config metadata and MinGW import libraries"
while IFS= read -r -d '' pc; do
  sed -i "s|^prefix=.*|prefix=$VIPS_DIR|" "$pc"
done < <(find "$VIPS_DIR/lib/pkgconfig" -name '*.pc' -print0)
# gettext is linked statically into the Windows glib build; the -lintl flag has
# no import library in the package.
sed -i 's/ -lintl//g' "$VIPS_DIR/lib/pkgconfig/glib-2.0.pc"

for def in "$VIPS_DIR"/lib/*.def; do
  base="$(basename "$def" .def)"
  dll="$(awk '/^LIBRARY/{gsub(/"/,"",$2); print $2; exit}' "$def")"
  [ -n "$dll" ] || dll="$base.dll"
  x86_64-w64-mingw32-dlltool -m i386:x86-64 -d "$def" \
    -l "$VIPS_DIR/lib/lib$base.dll.a" -D "$dll" >/dev/null
done

export PKG_CONFIG_ALLOW_CROSS=1
export PKG_CONFIG_LIBDIR="$VIPS_DIR/lib/pkgconfig"
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++
export CGO_ENABLED=1
export GOOS=windows
export GOARCH=amd64
export CGO_CFLAGS
CGO_CFLAGS="$(pkg-config --cflags vips)"
export CGO_LDFLAGS
CGO_LDFLAGS="$(pkg-config --libs vips)"

echo "==> Building the frontend"
(
  cd "$ROOT_DIR/frontend"
  if [ ! -d node_modules ]; then
    npm ci
  fi
  npm run build
)

# Resolve the build identity once so the desktop exe and the CLI share one
# version, commit and timestamp. GOOS is already windows here, so reset it for
# the host-side helper.
VERSION_LDFLAGS="$(cd "$ROOT_DIR" && GOOS= GOARCH= CGO_ENABLED=0 go run ./scripts/versionflags)"

echo "==> Generating the Windows resources and executable"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"
SYSO="$ROOT_DIR/wails_windows_amd64.syso"
trap 'rm -f "$SYSO"' EXIT
"$WAILS3_BIN" generate syso \
  -arch amd64 \
  -icon "$ROOT_DIR/build/windows/icon.ico" \
  -manifest "$ROOT_DIR/build/windows/wails.exe.manifest" \
  -info "$ROOT_DIR/build/windows/info.json" \
  -out "$SYSO"
(
  cd "$ROOT_DIR"
  go build -tags production -trimpath -buildvcs=false \
    -ldflags="-w -s -H windowsgui $VERSION_LDFLAGS" \
    -o "$STAGE_DIR/fileforge-desktop.exe" .
)

# The headless CLI links the same bimg/libvips cgo stack, so it builds with the
# environment exported above. It stays a console binary (no -H windowsgui) and
# is staged next to the desktop exe so the NSIS file copy and the portable ZIP
# both pick it up.
echo "==> Building the Windows headless CLI"
(
  cd "$ROOT_DIR"
  go build -trimpath -buildvcs=false \
    -ldflags="-w -s $VERSION_LDFLAGS" \
    -o "$STAGE_DIR/fileforge-cli.exe" ./cmd/fileforge-cli
)

echo "==> Bundling the libvips runtime"
python3 "$ROOT_DIR/scripts/prune-windows-dlls.py" \
  --source "$VIPS_DIR/bin" \
  --dest "$STAGE_DIR"

# Sign the staged executables before makensis runs, so the installer packages
# signed binaries and the portable ZIP below picks them up as well. The hook
# warns and continues when no certificate is configured.
echo "==> Signing the staged executables"
"$ROOT_DIR/scripts/sign-windows.sh" \
  "$STAGE_DIR/fileforge-desktop.exe" \
  "$STAGE_DIR/fileforge-cli.exe"

echo "==> Building the NSIS installer"
BOOTSTRAPPER="$ROOT_DIR/build/windows/nsis/MicrosoftEdgeWebview2Setup.exe"
if [ ! -f "$BOOTSTRAPPER" ]; then
  "$WAILS3_BIN" generate webview2bootstrapper -dir "$ROOT_DIR/build/windows/nsis"
fi
mkdir -p "$ROOT_DIR/bin"
rm -f "$INSTALLER"
(
  cd "$ROOT_DIR/build/windows/nsis"
  makensis \
    -DARG_WAILS_AMD64_BINARY="$STAGE_DIR/fileforge-desktop.exe" \
    -DFILEFORGE_CLI_BINARY="$STAGE_DIR/fileforge-cli.exe" \
    -DLIBVIPS_DIR="$STAGE_DIR" \
    project.nsi
)

# Sign the installer after makensis produced it. The uninstaller NSIS embeds
# stays unsigned: signing it would need the NSIS !uninstfinalize hook.
echo "==> Signing the NSIS installer"
"$ROOT_DIR/scripts/sign-windows.sh" "$INSTALLER"

# The portable ZIP bundles the already-signed executables; the ZIP itself is
# not signed.
echo "==> Creating the portable ZIP"
rm -f "$PORTABLE"
(
  cd "$CACHE_DIR/portable"
  zip -9 -qr "$PORTABLE" "FileForge Desktop"
)

echo
echo "Installer: $INSTALLER"
echo "Portable:  $PORTABLE"
