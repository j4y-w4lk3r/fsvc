#!/usr/bin/env bash

# Generate a Revolut Business API mTLS keypair and optionally store it in 1Password.
# Usage:
#   ./revolut_cert_setup.sh [vault_name] [output_directory]
# Defaults:
#   vault_name       = rev
#   output_directory = "$HOME/revolut-api-cert"

set -euo pipefail

VAULT_NAME="${1:-rev}"
OUT_DIR="${2:-"$HOME/revolut-api-cert"}"

echo "Using 1Password vault: $VAULT_NAME"
echo "Output directory: $OUT_DIR"

mkdir -p "$OUT_DIR"
cd "$OUT_DIR"

if [[ -f revolut_private.key || -f revolut_public.crt ]]; then
  echo "revolut_private.key or revolut_public.crt already exists in $OUT_DIR."
  echo "Refusing to overwrite existing files. Move or delete them and rerun if needed."
  exit 1
fi

echo "Generating private key (revolut_private.key)..."
openssl genrsa -out revolut_private.key 2048

echo "Generating self-signed public certificate (revolut_public.crt)..."
openssl req -new -x509 \
  -key revolut_private.key \
  -out revolut_public.crt \
  -days 825 \
  -sha256 \
  -subj "/CN=revolut-business-api"

echo
echo "Certificate preview (first few lines):"
openssl x509 -in revolut_public.crt -noout -text | head -n 10 || true

echo
echo "Attempting to store files in 1Password using 'op' CLI..."
if command -v op >/dev/null 2>&1; then
  echo "Found 'op' CLI. Make sure you are signed in (e.g. 'eval \"\$(op signin)\"')."

  echo "Storing private key as a secure document in vault '$VAULT_NAME'..."
  op document create revolut_private.key \
    --title "Revolut Business API private key" \
    --vault "$VAULT_NAME"

  echo "Storing public certificate as a document in vault '$VAULT_NAME'..."
  op document create revolut_public.crt \
    --title "Revolut Business API public cert" \
    --vault "$VAULT_NAME"

  echo "1Password storage complete."
else
  echo "'op' CLI not found on PATH; skipping 1Password upload."
  echo "You can manually run something like:"
  echo "  op document create \"$OUT_DIR/revolut_private.key\" --title 'Revolut Business API private key' --vault '$VAULT_NAME'"
  echo "  op document create \"$OUT_DIR/revolut_public.crt\" --title 'Revolut Business API public cert' --vault '$VAULT_NAME'"
fi

echo
echo "Done."
echo "Private key:   $OUT_DIR/revolut_private.key"
echo "Public cert:   $OUT_DIR/revolut_public.crt"
echo "Next: upload the public cert to Revolut, and point REVOLUT_CERT_PATH/REVOLUT_KEY_PATH to these files."

