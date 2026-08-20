#!/usr/bin/env bash
# Materialize ~/.config/fsvc/ from 1Password vault x-fsvc.
#
# Usage:
#   ./scripts/bootstrap-from-1pass.sh          # install to ~/.config/fsvc
#   ./scripts/bootstrap-from-1pass.sh -f       # overwrite existing config.toml
#   FSVC_CONFIG_DIR=/path ./scripts/bootstrap-from-1pass.sh
#
# Requires: op (1Password CLI), python3, fsvc on PATH (optional, for status check)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VAULT="${FSVC_1PASS_VAULT:-x-fsvc}"
CONFIG_ITEM="${FSVC_1PASS_CONFIG_ITEM:-fsvc revolut config}"
KEY_ITEM="${FSVC_1PASS_KEY_ITEM:-rev bus api private key}"
CERT_ITEM="${FSVC_1PASS_CERT_ITEM:-rev bus api public cert}"
CONFIG_DIR="${FSVC_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/fsvc}"
CERT_DIR="$CONFIG_DIR/revolut-api-cert"
FORCE=0

usage() {
  sed -n '2,8p' "$0"
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--force) FORCE=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "unknown arg: $1" >&2; usage 1 ;;
  esac
done

if ! command -v op >/dev/null 2>&1; then
  echo "bootstrap-from-1pass: op (1Password CLI) not found" >&2
  exit 1
fi

if ! op whoami >/dev/null 2>&1; then
  echo "bootstrap-from-1pass: 1Password CLI not signed in." >&2
  echo "  Run: eval \"\$(op signin)\"   # or export OP_SERVICE_ACCOUNT_TOKEN" >&2
  exit 1
fi

if [[ -f "$CONFIG_DIR/config.toml" && "$FORCE" -ne 1 ]]; then
  echo "bootstrap-from-1pass: $CONFIG_DIR/config.toml exists (use -f to overwrite)" >&2
  exit 1
fi

mkdir -p "$CERT_DIR"
chmod 700 "$CONFIG_DIR" "$CERT_DIR"

echo "[*] Fetching config from 1Password vault: $VAULT / $CONFIG_ITEM"
op item get "$CONFIG_ITEM" --vault "$VAULT" --format json \
  | python3 "$SCRIPT_DIR/render-config-toml.py" "$CONFIG_DIR" \
  > "$CONFIG_DIR/config.toml.tmp"
chmod 600 "$CONFIG_DIR/config.toml.tmp"
mv "$CONFIG_DIR/config.toml.tmp" "$CONFIG_DIR/config.toml"

echo "[*] Downloading private key: $KEY_ITEM"
op document get "$KEY_ITEM" --vault "$VAULT" > "$CERT_DIR/revolut_private.key.tmp"
chmod 600 "$CERT_DIR/revolut_private.key.tmp"
mv "$CERT_DIR/revolut_private.key.tmp" "$CERT_DIR/revolut_private.key"

echo "[*] Downloading public cert: $CERT_ITEM"
op document get "$CERT_ITEM" --vault "$VAULT" > "$CERT_DIR/revolut_public.crt.tmp"
chmod 644 "$CERT_DIR/revolut_public.crt.tmp"
mv "$CERT_DIR/revolut_public.crt.tmp" "$CERT_DIR/revolut_public.crt"

echo "[✓] Wrote $CONFIG_DIR/config.toml"
echo "[✓] Wrote $CERT_DIR/revolut_private.key (mode 0600)"
echo "[✓] Wrote $CERT_DIR/revolut_public.crt (mode 0644)"

if command -v fsvc >/dev/null 2>&1; then
  echo
  fsvc revolut status || true
  echo
  echo "Next: fsvc revolut auth    # refresh access_token if needed"
else
  echo
  echo "Install fsvc, then run: fsvc revolut status"
fi
