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
# The resulting JWT is printed to stdout at the end.

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

# Ensure we have a private key named privatecert.pem (as per docs)
if [[ ! -f privatecert.pem ]]; then
  if [[ -f revolut_private.key ]]; then
    echo "privatecert.pem not found; copying from revolut_private.key..."
    cp revolut_private.key privatecert.pem
  else
    echo "Error: privatecert.pem not found and revolut_private.key not present in $CERT_DIR" >&2
    exit 1
  fi
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
cat client_assertion.txt | tr -d '\n' | tr -d '\r' | openssl dgst -sha256 -sign privatecert.pem | openssl enc -base64 -A | tr +/ -_ | tr -d '=' > sign.txt
echo -n "." >> client_assertion.txt
# Add signature
cat sign.txt >> client_assertion.txt

echo
echo "client_assertion.txt generated in $CERT_DIR."
echo
echo "JWT (client_assertion):"
cat client_assertion.txt
echo

