#!/usr/bin/env bash

# Generate a client_assertion JWT for the Revolut Business API.
# Usage:
#   ./revolut_make_client_assertion.sh [CLIENT_ID] [ISS] [EXP_MINUTES] [CERT_DIR]
# Defaults / env support:
#   CLIENT_ID   = first arg, or $REVOLUT_CLIENT_ID
#   ISS         = second arg, or $REVOLUT_ISS
#   EXP_MINUTES = 10
#   CERT_DIR    = "<repo>/rev/revolut-api-cert" (by default)
#
# It will create/update the following files in CERT_DIR:
#   - header.json
#   - payload.json
#   - client_assertion.txt
#
# By default, the resulting JWT is NOT printed (avoid leaking secrets to logs).
# Set REVOLUT_PRINT_JWT=1 to print it.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"

CLIENT_ID="${1:-${REVOLUT_CLIENT_ID:-}}"
ISS="${2:-${REVOLUT_ISS:-}}"
EXP_MINUTES="${3:-10}"
CERT_DIR="${4:-"$SCRIPT_DIR/revolut-api-cert"}"

if [[ -z "$CLIENT_ID" || -z "$ISS" ]]; then
  echo "Usage: $0 [CLIENT_ID] [ISS] [EXP_MINUTES] [CERT_DIR]" >&2
  echo "Either pass CLIENT_ID and ISS as arguments or set REVOLUT_CLIENT_ID and REVOLUT_ISS in the environment." >&2
  exit 1
fi

echo "Client ID:   $CLIENT_ID"
echo "Issuer (iss): $ISS"
echo "Expiry:      now + ${EXP_MINUTES} minutes"
echo "Cert dir:    $CERT_DIR"

mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

# openssl dgst -sign needs a *private* key PEM. This repo keeps it as revolut_private.key;
# privatecert.pem may be a separate bundle (or cert-only), so prefer the explicit key file.
if [[ -f revolut_private.key ]]; then
  SIGN_KEY="revolut_private.key"
elif [[ -f privatecert.pem ]]; then
  SIGN_KEY="privatecert.pem"
else
  echo "Error: need revolut_private.key or privatecert.pem in $CERT_DIR" >&2
  exit 1
fi

if ! openssl pkey -in "$SIGN_KEY" -noout >/dev/null 2>&1; then
  echo "Error: $SIGN_KEY is not a usable private key PEM (openssl pkey -in failed)." >&2
  exit 1
fi

# macOS-specific date syntax (-v)
EXP_TS="$(date -v+"${EXP_MINUTES}"M +%s)"

cat <<EOF > header.json
{
  "alg": "RS256",
  "typ": "JWT"
}
EOF

cat <<EOF > payload.json
{
  "iss": "$ISS",
  "sub": "$CLIENT_ID",
  "aud": "https://revolut.com",
  "exp": $EXP_TS
}
EOF

echo "Generating client_assertion.txt..."

# Add encoded and normalised header
cat header.json | tr -d '\n' | tr -d '\r' | openssl enc -base64 -A | tr +/ -_ | tr -d '=' > client_assertion.txt
echo -n "." >> client_assertion.txt
# Add encoded and normalised payload
cat payload.json | tr -d '\n' | tr -d '\r' | openssl enc -base64 -A | tr +/ -_ | tr -d '=' >> client_assertion.txt
# Generate signature
cat client_assertion.txt | tr -d '\n' | tr -d '\r' | openssl dgst -sha256 -sign "$SIGN_KEY" | openssl enc -base64 -A | tr +/ -_ | tr -d '=' > sign.txt
echo -n "." >> client_assertion.txt
# Add signature
cat sign.txt >> client_assertion.txt

echo
echo "client_assertion.txt generated in $CERT_DIR."
if [[ "${REVOLUT_PRINT_JWT:-0}" == "1" ]]; then
  echo
  echo "JWT (client_assertion):"
  cat client_assertion.txt
  echo
else
  echo "[info] JWT not printed (set REVOLUT_PRINT_JWT=1 to print)."
fi

