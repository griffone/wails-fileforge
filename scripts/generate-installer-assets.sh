#!/usr/bin/env bash
# Generate the NSIS installer bitmaps from build/appicon.png.
#
# The generated BMPs are committed, so the Windows build never needs
# ImageMagick. Run this script only when the brand artwork or the copy changes.
#
# Requirements: ImageMagick 7 (`magick`) or 6 (`convert`) and the DejaVu fonts
# (or any fontconfig sans-serif fallback).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_ICON="$ROOT_DIR/build/appicon.png"
ASSET_DIR="$ROOT_DIR/build/windows/nsis/assets"

if command -v magick >/dev/null 2>&1; then
  IMAGE_MAGICK=(magick)
  IDENTIFY=(magick identify)
elif command -v convert >/dev/null 2>&1; then
  IMAGE_MAGICK=(convert)
  if ! command -v identify >/dev/null 2>&1; then
    echo "Missing the ImageMagick 'identify' command." >&2
    exit 1
  fi
  IDENTIFY=(identify)
else
  echo "Missing ImageMagick: install the 'magick' (ImageMagick 7) or 'convert' (ImageMagick 6) command." >&2
  exit 1
fi

if [ ! -f "$SOURCE_ICON" ]; then
  echo "Missing source icon: $SOURCE_ICON" >&2
  exit 1
fi

# Prefer fontconfig so the script works when the DejaVu family is absent.
resolve_font() {
  local pattern="$1"
  local fallback="$2"
  if command -v fc-match >/dev/null 2>&1; then
    local resolved
    resolved="$(fc-match -f '%{file}' "$pattern" 2>/dev/null || true)"
    if [ -n "$resolved" ] && [ -f "$resolved" ]; then
      printf '%s' "$resolved"
      return
    fi
  fi
  printf '%s' "$fallback"
}

FONT_BOLD="$(resolve_font 'sans-serif:style=Bold' 'DejaVu-Sans-Bold')"
FONT_REGULAR="$(resolve_font 'sans-serif' 'DejaVu-Sans')"

mkdir -p "$ASSET_DIR"

echo "==> Rendering the installer bitmaps"
# Welcome/finish page bitmap: 164x314, dark slate gradient, centered icon.
"${IMAGE_MAGICK[@]}" \
  -size 164x314 gradient:'#0f172a-#1e293b' \
  \( "$SOURCE_ICON" -resize 96x96 \) -geometry +34+24 -composite \
  -font "$FONT_BOLD" -pointsize 18 -fill white \
  -gravity North -annotate +0+136 "FileForge" \
  -font "$FONT_REGULAR" -pointsize 10 -fill '#94a3b8' \
  -gravity North -annotate +0+164 "Local file toolkit" \
  -alpha off -type TrueColor \
  "BMP3:$ASSET_DIR/welcome.bmp"

# Header bitmap: 150x57, same background, small icon on the left.
"${IMAGE_MAGICK[@]}" \
  -size 150x57 gradient:'#0f172a-#1e293b' \
  \( "$SOURCE_ICON" -resize 32x32 \) -geometry +10+12 -composite \
  -font "$FONT_BOLD" -pointsize 13 -fill white \
  -gravity West -annotate +50+0 "FileForge" \
  -alpha off -type TrueColor \
  "BMP3:$ASSET_DIR/header.bmp"

# identify reports an 8-bit TrueColor image with three channels for a 24-bit
# BMP3 file. Fail loudly instead of committing a bitmap NSIS cannot show.
verify_bitmap() {
  local path="$1"
  local expected_geometry="$2"
  local label="$3"

  if [ ! -f "$path" ]; then
    echo "Missing generated bitmap: $path" >&2
    exit 1
  fi

  local format geometry depth channels image_type
  format="$("${IDENTIFY[@]}" -format '%m' "$path")"
  geometry="$("${IDENTIFY[@]}" -format '%wx%h' "$path")"
  depth="$("${IDENTIFY[@]}" -format '%z' "$path")"
  channels="$("${IDENTIFY[@]}" -format '%[channels]' "$path")"
  image_type="$("${IDENTIFY[@]}" -format '%[type]' "$path")"

  if [ "$format" != "BMP3" ]; then
    echo "$label is $format, expected BMP3" >&2
    exit 1
  fi
  if [ "$geometry" != "$expected_geometry" ]; then
    echo "$label is $geometry, expected $expected_geometry" >&2
    exit 1
  fi
  if [ "$depth" != "8" ] || [ "$image_type" != "TrueColor" ] || [[ "$channels" != srgb* ]]; then
    echo "$label is not a 24-bit BMP (depth=$depth type=$image_type channels=$channels)" >&2
    exit 1
  fi

  echo "OK: $label $geometry, 24-bit BMP ($path)"
}

verify_bitmap "$ASSET_DIR/welcome.bmp" "164x314" "welcome.bmp"
verify_bitmap "$ASSET_DIR/header.bmp" "150x57" "header.bmp"

echo "Installer assets written to $ASSET_DIR"
