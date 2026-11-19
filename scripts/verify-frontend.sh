#!/bin/sh
# Verify the Angular frontend: production build plus the Karma suite.
#
# Karma needs a Chromium-based browser. The caller may set CHROME_BIN; when it
# is missing this script resolves a known browser from PATH so the verification
# works without per-machine environment setup.
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
FRONTEND_DIR="$ROOT_DIR/frontend"

if [ -n "${CHROME_BIN:-}" ]; then
  echo "verify-frontend: using CHROME_BIN=$CHROME_BIN"
else
  for candidate in brave brave-browser chromium chromium-browser google-chrome google-chrome-stable; do
    if resolved=$(command -v "$candidate" 2>/dev/null); then
      CHROME_BIN="$resolved"
      export CHROME_BIN
      echo "verify-frontend: using $CHROME_BIN"
      break
    fi
  done
fi

if [ -z "${CHROME_BIN:-}" ]; then
  echo "verify-frontend: no Chromium-based browser found on PATH." >&2
  echo "verify-frontend: install Chrome/Chromium or set CHROME_BIN to its executable." >&2
  exit 1
fi

cd "$FRONTEND_DIR"
npm run build
npx ng test --watch=false
