#!/usr/bin/env bash
# Authenticode-sign FileForge Windows PE files.
#
# The Linux cross-build host signs with osslsigncode; a Windows host signs with
# signtool. Both use SHA-256 and an RFC 3161 timestamp. Credentials come from
# the environment and reach the signer through stdin or a file, never through
# the command line, so the password never shows up in `ps` output or history.
#
# Usage:
#   sign-windows.sh [--check | --dry-run] <file>...
#
#   --check    validate the environment and print the signing plan; accepts no
#              files and never touches one
#   --dry-run  print the exact commands for the given files; never touches them
#
# Environment:
#   WINDOWS_CERT_PFX            PKCS#12 (.pfx/.p12) file with the signing key
#   WINDOWS_CERT_PASSWORD       PFX password, read from the environment
#   WINDOWS_CERT_PASSWORD_FILE  file holding the PFX password (wins over the
#                               environment variable)
#   WINDOWS_CERT_THUMBPRINT     signtool only: SHA-1 thumbprint of a certificate
#                               already in Cert:\CurrentUser\My
#   WINDOWS_TIMESTAMP_URL       RFC 3161 timestamp endpoint
#                               (default http://timestamp.digicert.com)
#   SIGN_WINDOWS=0              skip signing on purpose
#   SIGN_WINDOWS_TOOL           auto (default), osslsigncode or signtool
#   REQUIRE_SIGNING=1           fail when the certificate or tool is missing
#
# Without a usable certificate the script warns once and exits 0, so the regular
# build keeps producing unsigned artifacts. REQUIRE_SIGNING=1 makes that fatal.
set -euo pipefail
# Never trace: the password passes through this process.
set +x

REQUIRE="${REQUIRE_SIGNING:-0}"
SKIP="${SIGN_WINDOWS:-1}"
TOOL_CHOICE="${SIGN_WINDOWS_TOOL:-auto}"
TS_URL="${WINDOWS_TIMESTAMP_URL:-http://timestamp.digicert.com}"
PFX="${WINDOWS_CERT_PFX:-}"
THUMB="${WINDOWS_CERT_THUMBPRINT:-}"
PASS_FILE="${WINDOWS_CERT_PASSWORD_FILE:-}"

MODE="sign"
FILES=()
TOOL=""
TOOL_PATH=""
TOOL_PRESENT=0
TOOL_REASON=""
PASS_MODE="none"
CERT_SOURCE="not configured"
PASS_SOURCE="not configured"
CRED_OK=0
REASON=""
READY=0
SIGN_THUMB=""
IMPORTED_CERT=0
PS_BIN=""
TMP_FILES=()

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Usage: $(basename "$0") [--check | --dry-run] <file>...

Signs one or more Windows PE files with Authenticode (SHA-256, RFC 3161
timestamp) using osslsigncode or signtool.

Options:
  --check     validate the environment and print the plan; no file needed
  --dry-run   print the exact commands; no file is touched
  -h, --help  show this help

Environment:
  WINDOWS_CERT_PFX            PKCS#12 file with the signing key
  WINDOWS_CERT_PASSWORD       PFX password (never printed, never on argv)
  WINDOWS_CERT_PASSWORD_FILE  file with the PFX password (takes precedence)
  WINDOWS_CERT_THUMBPRINT     signtool: SHA-1 thumbprint in CurrentUser\My
  WINDOWS_TIMESTAMP_URL       RFC 3161 endpoint (default
                              http://timestamp.digicert.com)
  SIGN_WINDOWS=0              skip signing
  SIGN_WINDOWS_TOOL           auto | osslsigncode | signtool
  REQUIRE_SIGNING=1           fail when signing is unavailable
EOF
}

cleanup() {
  local file
  for file in ${TMP_FILES[@]+"${TMP_FILES[@]}"}; do
    rm -f -- "$file"
  done
  if [ "$IMPORTED_CERT" -eq 1 ] && [ -n "$SIGN_THUMB" ] && [ -n "$PS_BIN" ]; then
    "$PS_BIN" -NoProfile -NonInteractive -Command \
      "Remove-Item -LiteralPath ('Cert:\\CurrentUser\\My\\' + '$SIGN_THUMB') -Force -ErrorAction SilentlyContinue" \
      >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

resolve_tool() {
  local found=""
  case "$TOOL_CHOICE" in
    auto)
      if command -v osslsigncode >/dev/null 2>&1; then
        found="osslsigncode"
      elif command -v signtool >/dev/null 2>&1; then
        found="signtool"
      fi
      if [ -n "$found" ]; then
        TOOL="$found"
        TOOL_PRESENT=1
      else
        # Prefer the tool of this build host in the plan even when missing.
        TOOL="osslsigncode"
        TOOL_REASON="osslsigncode is not installed and signtool was not found"
      fi
      ;;
    osslsigncode)
      TOOL="osslsigncode"
      if command -v osslsigncode >/dev/null 2>&1; then
        TOOL_PRESENT=1
      else
        TOOL_REASON="osslsigncode is not installed (install it with your package manager)"
      fi
      ;;
    signtool)
      TOOL="signtool"
      if command -v signtool >/dev/null 2>&1; then
        TOOL_PRESENT=1
      else
        TOOL_REASON="signtool is not on PATH (install the Windows SDK and use a Developer prompt)"
      fi
      ;;
    *)
      die "SIGN_WINDOWS_TOOL must be auto, osslsigncode or signtool (got: $TOOL_CHOICE)"
      ;;
  esac
  if [ "$TOOL_PRESENT" -eq 1 ]; then
    TOOL_PATH="$(command -v "$TOOL")"
  fi
}

check_credentials() {
  if [ -n "$PASS_FILE" ]; then
    PASS_MODE="file"
  elif [ -n "${WINDOWS_CERT_PASSWORD+x}" ]; then
    PASS_MODE="env"
  else
    PASS_MODE="none"
  fi

  # signtool can use a certificate already installed in the user store, which
  # needs no password at all. osslsigncode cannot.
  if [ "$TOOL" = "signtool" ] && [ -n "$THUMB" ]; then
    CERT_SOURCE="certificate store thumbprint $THUMB"
    PASS_SOURCE="not needed"
    CRED_OK=1
    return
  fi

  if [ -n "$THUMB" ] && [ -z "$PFX" ]; then
    REASON="WINDOWS_CERT_THUMBPRINT only works with signtool; set WINDOWS_CERT_PFX for osslsigncode"
    return
  fi
  if [ -z "$PFX" ]; then
    REASON="no signing certificate configured (set WINDOWS_CERT_PFX)"
    return
  fi
  if [ ! -f "$PFX" ]; then
    REASON="WINDOWS_CERT_PFX does not exist: $PFX"
    return
  fi
  CERT_SOURCE="$PFX"

  case "$PASS_MODE" in
    file)
      if [ ! -r "$PASS_FILE" ]; then
        REASON="WINDOWS_CERT_PASSWORD_FILE is not readable: $PASS_FILE"
        return
      fi
      if [ "$TOOL" = "signtool" ]; then
        PASS_SOURCE="file $PASS_FILE (read by PowerShell)"
      else
        PASS_SOURCE="file $PASS_FILE"
      fi
      ;;
    env)
      if [ "$TOOL" = "signtool" ]; then
        PASS_SOURCE="WINDOWS_CERT_PASSWORD (read by PowerShell from the environment)"
      else
        PASS_SOURCE="WINDOWS_CERT_PASSWORD (written to the signer on stdin)"
      fi
      ;;
    none)
      REASON="no password source (set WINDOWS_CERT_PASSWORD or WINDOWS_CERT_PASSWORD_FILE)"
      return
      ;;
  esac
  CRED_OK=1
}

find_powershell() {
  command -v powershell 2>/dev/null ||
    command -v powershell.exe 2>/dev/null ||
    command -v pwsh 2>/dev/null ||
    true
}

compute_ready() {
  READY=0
  if [ "$CRED_OK" -ne 1 ]; then
    return
  fi
  if [ "$TOOL_PRESENT" -ne 1 ]; then
    REASON="$TOOL_REASON"
    return
  fi
  # signtool has no stdin password option: the PFX is imported through
  # PowerShell before signing. A pre-imported certificate (thumbprint) skips it.
  if [ "$TOOL" = "signtool" ] && [ -z "$THUMB" ] && [ -z "$(find_powershell)" ]; then
    REASON="signtool with a PFX needs PowerShell for the temporary import; set WINDOWS_CERT_THUMBPRINT to skip it"
    return
  fi
  READY=1
}

# Quote a value for display. Placeholders such as <pfx> stay readable.
quote() {
  case "$1" in
    '<'*'>') printf '%s' "$1" ;;
    *) printf '%q' "$1" ;;
  esac
}

# Print the osslsigncode command; never expand a password.
describe_osslsigncode_sign() {
  local file="$1"
  local pfx_q pass_q
  pfx_q="$(quote "${PFX:-<pfx>}")"
  if [ "$PASS_MODE" = "file" ]; then
    pass_q="$(quote "${PASS_FILE:-<password-file>}")"
    printf 'osslsigncode sign -pkcs12 %s -h sha256 -ts %s -readpass %s -in %s -out %s\n' \
      "$pfx_q" "$(quote "$TS_URL")" "$pass_q" "$(quote "$file")" "$(quote "$file.signed")"
  else
    printf 'printf %%s "$WINDOWS_CERT_PASSWORD" | osslsigncode sign -pkcs12 %s -h sha256 -ts %s -readpass - -in %s -out %s\n' \
      "$pfx_q" "$(quote "$TS_URL")" "$(quote "$file")" "$(quote "$file.signed")"
  fi
}

describe_signtool_import() {
  if [ -n "$THUMB" ]; then
    return
  fi
  printf '# import WINDOWS_CERT_PFX into Cert:\\CurrentUser\\My with the password from %s,\n' \
    "$PASS_SOURCE"
  printf '# read its thumbprint, sign, then remove the certificate\n'
}

describe_signtool_sign() {
  local file="$1"
  printf 'signtool sign /sha1 %s /fd sha256 /tr %s /td sha256 %s\n' \
    "$(quote "${THUMB:-<thumbprint>}")" "$(quote "$TS_URL")" "$(quote "$file")"
}

describe_verify() {
  local file="$1"
  case "$TOOL" in
    signtool)
      printf 'signtool verify /pa /all %s\n' "$(quote "$file")"
      ;;
    *)
      printf 'osslsigncode verify -in %s\n' "$(quote "$file")"
      ;;
  esac
}

print_plan() {
  local status="ready"
  if [ "$READY" -ne 1 ]; then
    status="not ready: $REASON"
  fi
  local tool_display="$TOOL"
  if [ "$TOOL_PRESENT" -eq 1 ]; then
    tool_display="$TOOL ($TOOL_PATH)"
  else
    tool_display="$TOOL (not installed)"
  fi

  printf 'Windows code signing plan\n'
  printf '  tool:          %s\n' "$tool_display"
  printf '  certificate:   %s\n' "$CERT_SOURCE"
  printf '  password:      %s\n' "$PASS_SOURCE"
  printf '  timestamp URL: %s\n' "$TS_URL"
  printf '  digest:        SHA-256 with an RFC 3161 timestamp\n'
  printf '  status:        %s\n' "$status"
}

unavailable() {
  local reason="$1"
  if [ "$REQUIRE" = "1" ]; then
    cat >&2 <<EOF
error: Windows code signing is required (REQUIRE_SIGNING=1) but unavailable: $reason.

  Linux:   install osslsigncode, then export WINDOWS_CERT_PFX and
           WINDOWS_CERT_PASSWORD or WINDOWS_CERT_PASSWORD_FILE.
  Windows: install signtool from the Windows SDK, then either export
           WINDOWS_CERT_PFX plus a password source, or import the PFX and
           export WINDOWS_CERT_THUMBPRINT.
  The timestamp service defaults to http://timestamp.digicert.com and can be
  changed with WINDOWS_TIMESTAMP_URL.
  Unset REQUIRE_SIGNING, or set SIGN_WINDOWS=0 for an explicit unsigned build.
EOF
    exit 1
  fi
  warn "Windows code signing skipped: $reason"
  warn "set WINDOWS_CERT_PFX and WINDOWS_CERT_PASSWORD(_FILE) to sign, or REQUIRE_SIGNING=1 to make this fatal"
  exit 0
}

# --- osslsigncode path -------------------------------------------------------

sign_osslsigncode() {
  local file="$1"
  local out="$file.signing.$$"
  local args=(sign -pkcs12 "$PFX" -h sha256 -ts "$TS_URL")

  if [ "$PASS_MODE" = "file" ]; then
    args+=(-readpass "$PASS_FILE")
  else
    args+=(-readpass -)
  fi
  args+=(-in "$file" -out "$out")

  TMP_FILES+=("$out")
  log "signing $(basename "$file") with osslsigncode (SHA-256, RFC 3161)"
  if [ "$PASS_MODE" = "file" ]; then
    osslsigncode "${args[@]}"
  else
    # The password travels through the pipe, never through argv.
    printf '%s\n' "$WINDOWS_CERT_PASSWORD" | osslsigncode "${args[@]}"
  fi
  if [ ! -s "$out" ]; then
    die "osslsigncode did not produce a signed file for $file"
  fi
  chmod --reference="$file" "$out" 2>/dev/null || true
  mv -f -- "$out" "$file"
}

verify_osslsigncode() {
  local file="$1"
  local output=""
  if output="$(osslsigncode verify -in "$file" 2>&1)"; then
    log "verified $(basename "$file")"
    return
  fi
  warn "full verification failed for $(basename "$file"); retrying without timestamp and revocation checks"
  if output="$(osslsigncode verify -ignore-timestamp -ignore-cdp -ignore-crl -in "$file" 2>&1)"; then
    warn "signature and digest verified for $(basename "$file"); the timestamp signature and revocation status were not checked locally"
    return
  fi
  printf '%s\n' "$output" >&2
  die "signature verification failed for $file"
}

# --- signtool path -----------------------------------------------------------

prepare_signtool_identity() {
  if [ -n "$THUMB" ]; then
    SIGN_THUMB="$THUMB"
    return
  fi
  PS_BIN="$(find_powershell)"
  if [ -z "$PS_BIN" ]; then
    die "signtool with a PFX needs PowerShell to import it; install PowerShell or set WINDOWS_CERT_THUMBPRINT"
  fi
  log "importing the PFX into Cert:\\CurrentUser\\My for the duration of the signing step"
  # The password stays in the process environment; the script text only reads
  # it by name, so it never appears on a command line.
  local script
  script='
$ErrorActionPreference = "Stop"
if ($env:WINDOWS_CERT_PASSWORD_FILE) {
  $pw = [System.IO.File]::ReadAllText($env:WINDOWS_CERT_PASSWORD_FILE).Trim()
} else {
  $pw = $env:WINDOWS_CERT_PASSWORD
}
$secure = ConvertTo-SecureString -String $pw -AsPlainText -Force
$cert = Import-PfxCertificate -FilePath $env:WINDOWS_CERT_PFX -CertStoreLocation "Cert:\CurrentUser\My" -Password $secure
Write-Output $cert.Thumbprint
'
  if ! SIGN_THUMB="$("$PS_BIN" -NoProfile -NonInteractive -Command "$script" | tr -d '\r' | tail -n 1)"; then
    die "PowerShell failed to import WINDOWS_CERT_PFX"
  fi
  SIGN_THUMB="${SIGN_THUMB//[[:space:]]/}"
  if [ -z "$SIGN_THUMB" ]; then
    die "PowerShell did not return a certificate thumbprint after importing WINDOWS_CERT_PFX"
  fi
  IMPORTED_CERT=1
}

sign_signtool() {
  local file="$1"
  log "signing $(basename "$file") with signtool (SHA-256, RFC 3161)"
  signtool sign \
    /sha1 "$SIGN_THUMB" \
    /fd sha256 \
    /tr "$TS_URL" \
    /td sha256 \
    "$file"
}

verify_signtool() {
  local file="$1"
  signtool verify /pa /all "$file" >/dev/null ||
    die "signtool could not verify the signature on $file"
  log "verified $(basename "$file")"
}

# --- main --------------------------------------------------------------------

while [ "$#" -gt 0 ]; do
  case "$1" in
    --check)
      [ "$MODE" = "sign" ] || die "--check and --dry-run are mutually exclusive"
      MODE="check"
      ;;
    --dry-run)
      [ "$MODE" = "sign" ] || die "--check and --dry-run are mutually exclusive"
      MODE="dry-run"
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    --)
      shift
      while [ "$#" -gt 0 ]; do
        FILES+=("$1")
        shift
      done
      continue
      ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      FILES+=("$1")
      ;;
  esac
  shift
done

if [ "$SKIP" = "0" ]; then
  if [ "$REQUIRE" = "1" ]; then
    die "SIGN_WINDOWS=0 disables signing but REQUIRE_SIGNING=1 requires it; unset one of them"
  fi
  log "SIGN_WINDOWS=0; skipping Windows code signing"
  exit 0
fi

resolve_tool
check_credentials
compute_ready

if [ "$MODE" = "check" ]; then
  print_plan
  if [ "$READY" -eq 1 ]; then
    printf '  commands:\n'
    case "$TOOL" in
      signtool)
        { describe_signtool_import; describe_signtool_sign "<file>"; } | sed 's/^/    /'
        ;;
      *)
        describe_osslsigncode_sign "<file>" | sed 's/^/    /'
        ;;
    esac
    exit 0
  fi
  unavailable "$REASON"
fi

if [ "$MODE" = "dry-run" ]; then
  if [ "${#FILES[@]}" -eq 0 ]; then
    die "no input files; pass one or more PE files to preview"
  fi
  for file in "${FILES[@]}"; do
    [ -f "$file" ] || die "file not found: $file"
  done
  print_plan
  for file in "${FILES[@]}"; do
    printf '  # %s\n' "$file"
    if [ "$TOOL" = "signtool" ]; then
      { describe_signtool_import; describe_signtool_sign "$file"; describe_verify "$file"; } |
        sed 's/^/  /'
    else
      { describe_osslsigncode_sign "$file"; describe_verify "$file"; } | sed 's/^/  /'
    fi
  done
  if [ "$READY" -ne 1 ]; then
    if [ "$REQUIRE" = "1" ]; then
      unavailable "$REASON"
    fi
    warn "Windows code signing is not configured ($REASON); these commands are a preview only"
  fi
  exit 0
fi

if [ "$READY" -ne 1 ]; then
  unavailable "$REASON"
fi
if [ "${#FILES[@]}" -eq 0 ]; then
  die "no input files; pass one or more PE files to sign"
fi
for file in "${FILES[@]}"; do
  [ -f "$file" ] || die "file not found: $file"
done

if [ "$TOOL" = "signtool" ]; then
  prepare_signtool_identity
fi

for file in "${FILES[@]}"; do
  case "$TOOL" in
    osslsigncode)
      sign_osslsigncode "$file"
      verify_osslsigncode "$file"
      ;;
    signtool)
      sign_signtool "$file"
      verify_signtool "$file"
      ;;
  esac
done

log "signed and verified ${#FILES[@]} file(s) with $TOOL"
