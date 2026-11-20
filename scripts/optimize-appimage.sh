#!/usr/bin/env bash
# Shrink the generated Linux AppImage without changing application behavior.
#
# The Wails AppImage generator (linuxdeploy) bundles both WebKit stacks: the
# GTK4 + WebKitGTK 6.0 libraries the app links, and the GTK3 + WebKit2GTK 4.1
# libraries copied by the generator template. The app never loads the GTK3
# stack, so this script extracts the AppImage, removes it (plus the unused
# GObject introspection typelibs), and repacks the remaining AppDir with zstd.
#
# Usage:
#   optimize-appimage.sh <path/to/fileforge-desktop-*.AppImage | bin dir> [--strict]
#
# Without --strict a failed optimization is a warning and the original
# AppImage is left untouched. Set OPTIMIZE_APPIMAGE=0 to skip the whole step.
set -euo pipefail

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/fileforge-appimage"
APP_NAME="fileforge-desktop"
STRICT=0
INPUT=""
REMOVED_BYTES=0
WORK=""
NEW_IMAGE=""

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <AppImage | directory> [--strict]

Removes the unused GTK3/WebKit2GTK 4.1 stack from the FileForge AppImage and
repacks it with zstd. A directory must contain ${APP_NAME}-*.AppImage.

Options:
  --strict   exit nonzero when the optimization or validation fails
  -h, --help show this help

Environment:
  OPTIMIZE_APPIMAGE=0   skip the optimization and keep the original AppImage
EOF
}

human_bytes() {
  if command -v numfmt >/dev/null 2>&1; then
    numfmt --to=iec --suffix=B "$1"
  else
    printf '%s bytes' "$1"
  fi
}

remove_path() {
  local path
  for path in "$@"; do
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then
      continue
    fi
    REMOVED_BYTES=$((REMOVED_BYTES + $(du -sb "$path" | cut -f1)))
    rm -rf -- "$path"
  done
}

cleanup() {
  if [ -n "$WORK" ]; then
    rm -rf -- "$WORK"
  fi
  if [ -n "$NEW_IMAGE" ]; then
    rm -f -- "$NEW_IMAGE"
  fi
}

resolve_input() {
  local candidate
  [ -e "$INPUT" ] || die "AppImage not found: $INPUT"
  # Resolve to an absolute path: the AppImage is extracted from a temporary
  # directory, so a relative path would stop resolving after the cd.
  INPUT="$(cd "$(dirname "$INPUT")" && pwd)/$(basename "$INPUT")"
  if [ -d "$INPUT" ]; then
    candidate="$(find "$INPUT" -maxdepth 1 -type f -name "${APP_NAME}-*.AppImage" \
      -printf '%T@ %p\n' | sort -nr | head -n1 | cut -d' ' -f2-)"
    [ -n "$candidate" ] || die "no ${APP_NAME}-*.AppImage found in $INPUT"
    INPUT="$candidate"
  fi
  [ -f "$INPUT" ] || die "AppImage not found: $INPUT"
  [ -x "$INPUT" ] || die "AppImage is not executable: $INPUT"
  log "optimizing $(basename "$INPUT")"
}

ensure_appimagetool() {
  local arch="$1"
  local tool="$CACHE_DIR/appimagetool-${arch}.AppImage"
  local url="https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${arch}.AppImage"

  if [ ! -f "$tool" ]; then
    mkdir -p "$CACHE_DIR"
    log "downloading appimagetool for ${arch}"
    if command -v curl >/dev/null 2>&1; then
      curl -L --fail --silent --show-error -o "$tool.tmp" "$url" ||
        die "failed to download $url"
    elif command -v wget >/dev/null 2>&1; then
      wget -q -O "$tool.tmp" "$url" || die "failed to download $url"
    else
      die "curl or wget is required to download appimagetool"
    fi
    chmod +x "$tool.tmp"
    mv -f "$tool.tmp" "$tool"
  fi
  chmod +x "$tool"
  APPIMAGETOOL="$tool"
}

prune_appdir() {
  local appdir="$1"

  shopt -s nullglob
  local gtk3_paths=(
    "$appdir/usr/lib/webkit2gtk-4.1"
    "$appdir/usr/lib/webkit2gtk-4.0"
    "$appdir/usr/lib/libwebkit2gtk-4.1.so"*
    "$appdir/usr/lib/libjavascriptcoregtk-4.1.so"*
    "$appdir/usr/lib/libgtk-3.so"*
    "$appdir/usr/lib/libgdk-3.so"*
    "$appdir/usr/lib/libatk-1.0.so"*
    "$appdir/usr/lib/libatk-bridge-2.0.so"*
    "$appdir/usr/lib/libwebkit2gtkinjectedbundle.so"
    "$appdir/usr/lib/girepository-1.0"
  )
  remove_path "${gtk3_paths[@]}"

  local webkit6_libs=( "$appdir/usr/lib/libwebkitgtk-6.0.so"* )
  if [ ${#webkit6_libs[@]} -eq 0 ]; then
    warn "WebKitGTK 6.0 library not found in the AppDir; check the pruning list"
  fi
}

validate_appdir() {
  local appdir="$1"
  local library_path="$appdir/usr/lib"
  local targets=()
  local file output

  [ -f "$appdir/usr/bin/${APP_NAME}" ] || {
    warn "missing $appdir/usr/bin/${APP_NAME}"
    return 1
  }
  targets+=( "$appdir/usr/bin/${APP_NAME}" )

  while IFS= read -r -d '' file; do
    targets+=( "$file" )
  done < <(find "$appdir/usr/lib/webkitgtk-6.0" -type f -print0 2>/dev/null)

  local failed=0
  for file in "${targets[@]}"; do
    if command -v file >/dev/null 2>&1 && ! file -b "$file" | grep -q '^ELF'; then
      continue
    fi
    output="$(LD_LIBRARY_PATH="$library_path${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
      ldd "$file" 2>&1 || true)"
    if printf '%s\n' "$output" | grep -qE 'not found|cannot open shared object|error while loading'; then
      warn "missing dependencies for ${file#"$appdir"/}"
      printf '%s\n' "$output" | grep -E 'not found|cannot open shared object|error while loading' |
        sed 's/^/    /' >&2
      failed=1
    fi
  done
  return "$failed"
}

optimize() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | aarch64) ;;
    *)
      log "unsupported architecture ${arch}; skipping AppImage optimization"
      exit 0
      ;;
  esac

  if [ "${OPTIMIZE_APPIMAGE:-1}" = "0" ]; then
    log "OPTIMIZE_APPIMAGE=0; keeping the AppImage unchanged"
    exit 0
  fi

  resolve_input

  ensure_appimagetool "$arch"
  local appimagetool="$APPIMAGETOOL"

  WORK="$(mktemp -d "${TMPDIR:-/tmp}/fileforge-appimage.XXXXXX")"
  NEW_IMAGE="${INPUT}.optimized.$$"
  trap cleanup EXIT INT TERM

  log "extracting the AppImage"
  (
    cd "$WORK"
    "$INPUT" --appimage-extract >/dev/null
  ) || die "failed to extract $INPUT"
  local appdir="$WORK/squashfs-root"
  [ -d "$appdir" ] || die "extraction did not create $appdir"

  log "removing the unused GTK3/WebKit2GTK 4.1 stack"
  prune_appdir "$appdir"
  log "removed $(human_bytes "$REMOVED_BYTES")"

  # appimagetool refuses world-inaccessible directories, and the AppImage
  # runtime may have extracted them with mode 0700.
  chmod -R a+rX "$appdir"

  log "validating the pruned AppDir"
  validate_appdir "$appdir" || die "the pruned AppDir has missing dependencies"

  log "repacking with appimagetool (zstd)"
  ARCH="$arch" "$appimagetool" --appimage-extract-and-run --comp zstd \
    --no-appstream "$appdir" "$NEW_IMAGE" ||
    die "appimagetool failed to repack the AppImage"
  [ -s "$NEW_IMAGE" ] || die "appimagetool did not produce an AppImage"

  log "verifying the repacked AppImage"
  mkdir -p "$WORK/verify"
  (
    cd "$WORK/verify"
    "$NEW_IMAGE" --appimage-extract AppRun >/dev/null
  ) || die "the repacked AppImage cannot be extracted"

  local before after
  before="$(stat -c '%s' "$INPUT")"
  after="$(stat -c '%s' "$NEW_IMAGE")"
  mv -f -- "$NEW_IMAGE" "$INPUT"
  log "replaced $(basename "$INPUT"): $(human_bytes "$before") -> $(human_bytes "$after")"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --strict)
      STRICT=1
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    --*)
      die "unknown option: $1"
      ;;
    *)
      [ -z "$INPUT" ] || die "expected a single AppImage or directory argument"
      INPUT="$1"
      ;;
  esac
  shift
done
[ -n "$INPUT" ] || {
  usage
  exit 2
}

# Run the optimizer in a subshell with its own errexit setting so a failure
# can be downgraded to a warning when --strict is not set.
set +e
( set -e; optimize )
status=$?
set -e

if [ "$status" -ne 0 ]; then
  if [ "$STRICT" -eq 1 ]; then
    die "AppImage optimization failed (--strict)"
  fi
  warn "AppImage optimization failed; keeping the original AppImage unchanged"
  exit 0
fi
