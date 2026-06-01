#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

OUT_SOPS="$SCRIPT_DIR/revolut.sandbox.env.sops.env"
TMP_PLAIN="$SCRIPT_DIR/.revolut.sandbox.env.tmp"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 2
  fi
}

need sops

prompt() {
  local label="$1"
  local default="${2:-}"
  local var
  if [[ -n "$default" && "$default" != "__PROMPT__" ]]; then
    read -r -p "$label [$default]: " var
    echo "${var:-$default}"
    return 0
  fi
  if [[ -z "$default" || "$default" == "__PROMPT__" ]]; then
    read -r -p "$label: " var
    echo "$var"
  else
    read -r -p "$label [$default]: " var
    echo "${var:-$default}"
  fi
}

echo "This will create/update:"
echo "  $OUT_SOPS"
echo

BASE_URL="https://sandbox-b2b.revolut.com/api/1.0"

# You can pre-seed values to avoid prompts by exporting:
#   REVOLUT_SANDBOX_CLIENT_ID, REVOLUT_SANDBOX_REDIRECT_URI, REVOLUT_SANDBOX_ISS,
#   REVOLUT_SANDBOX_CERT_PATH, REVOLUT_SANDBOX_KEY_PATH
CLIENT_ID_DEFAULT="${REVOLUT_SANDBOX_CLIENT_ID:-__PROMPT__}"
REDIRECT_URI_DEFAULT="${REVOLUT_SANDBOX_REDIRECT_URI:-https://example.com}"
ISS_DEFAULT="${REVOLUT_SANDBOX_ISS:-example.com}"

CLIENT_ID="$(prompt "Sandbox Client ID" "$CLIENT_ID_DEFAULT")"
REDIRECT_URI="$(prompt "OAuth redirect URI" "$REDIRECT_URI_DEFAULT")"
ISS="$(prompt "Issuer (iss) - must match redirect URI domain" "$ISS_DEFAULT")"

CERT_PATH_DEFAULT="$SCRIPT_DIR/revolut-api-cert/revolut_public.crt"
KEY_PATH_DEFAULT="$SCRIPT_DIR/revolut-api-cert/revolut_private.key"
CERT_PATH="$(prompt "mTLS cert path (public cert)" "${REVOLUT_SANDBOX_CERT_PATH:-$CERT_PATH_DEFAULT}")"
KEY_PATH="$(prompt "mTLS key path (private key)" "${REVOLUT_SANDBOX_KEY_PATH:-$KEY_PATH_DEFAULT}")"

cat > "$TMP_PLAIN" <<EOF
export REVOLUT_BASE_URL="$BASE_URL"
export REVOLUT_CLIENT_ID="$CLIENT_ID"
export REVOLUT_REDIRECT_URI="$REDIRECT_URI"
export REVOLUT_ISS="$ISS"
export REVOLUT_CERT_PATH="$CERT_PATH"
export REVOLUT_KEY_PATH="$KEY_PATH"
EOF

echo
echo "Encrypting env to SOPS..."
sops --encrypt --in-place --input-type dotenv --output-type dotenv "$TMP_PLAIN"
mv -f "$TMP_PLAIN" "$OUT_SOPS"

echo
echo "Done."
echo "Next:"
echo "  ./run.sh --sandbox revolut-oauth-bootstrap"
echo
echo "Note: this script does NOT create tokens/refresh_token; those are created after consent."

