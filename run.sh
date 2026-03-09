#!/usr/bin/env bash
set -euo pipefail

# Always operate from the repo root (directory of this script)
cd "$(dirname "${BASH_SOURCE[0]:-$0}")"

# Make sure GPG can talk to the current TTY (needed for YubiKey PIN)
export GPG_TTY="${GPG_TTY:-$(tty)}"

ensure_venv() {
  if [[ -d ".venv" ]]; then
    echo "[run.sh] .venv already exists, skipping venv creation."
    return 0
  fi

  echo "[run.sh] Creating Python virtualenv in .venv..."
  python3 -m venv .venv

  echo "[run.sh] Installing Python dependencies..."
  .venv/bin/python -m pip install -r ing/requirements.txt -r rev/requirements.txt
}

decrypt_if_missing() {
  local plain="$1"
  local enc="$2"
  local mode="${3:-binary}" # "binary" or "dotenv"

  if [[ -f "$plain" ]]; then
    echo "[run.sh] $plain already present, skipping decrypt."
    return 0
  fi

  if [[ ! -f "$enc" ]]; then
    echo "[run.sh] WARNING: neither $plain nor $enc found, skipping." >&2
    return 0
  fi

  echo "[run.sh] Decrypting $enc -> $plain"
  if [[ "$mode" == "dotenv" ]]; then
    sops -d --input-type dotenv --output-type dotenv "$enc" > "$plain"
  else
    sops -d "$enc" > "$plain"
  fi
}

echo "[run.sh] Ensuring Python virtualenv and dependencies..."
ensure_venv

echo "[run.sh] Checking decrypted secrets..."

# Revolut env (dotenv-style exports)
decrypt_if_missing \
  "rev/revolut.env" \
  "rev/revolut.env.sops.env" \
  "dotenv"

# Revolut client certificate and private key
decrypt_if_missing \
  "rev/revolut-api-cert/privatecert.pem" \
  "rev/revolut-api-cert/privatecert.pem.enc"

decrypt_if_missing \
  "rev/revolut-api-cert/revolut_private.key" \
  "rev/revolut-api-cert/revolut_private.key.enc"

echo "[run.sh] Decryption checks finished."

if [[ "${1-}" == "revolut-txs" ]]; then
  echo "[run.sh] Loading Revolut env into subshell..."
  eval "$(sops -d --input-type dotenv --output-type dotenv rev/revolut.env.sops.env)"

  REV_DIR="$(pwd)/rev"
  export REVOLUT_CERT_PATH="${REVOLUT_CERT_PATH:-"$REV_DIR/revolut-api-cert/revolut_public.crt"}"
  export REVOLUT_KEY_PATH="${REVOLUT_KEY_PATH:-"$REV_DIR/revolut-api-cert/revolut_private.key"}"

  echo "[run.sh] Running Revolut transactions listing..."
  .venv/bin/python rev/revolut_list_transactions.py
  exit 0
fi

echo "[run.sh] You can now, for example:"
echo "  - load env:   eval \"\$(sops -d --input-type dotenv --output-type dotenv rev/revolut.env.sops.env)\""
echo "  - run smoke:  source .venv/bin/activate && (cd rev && python revolut_smoke.py)"
echo "  - list txs:   ./run.sh revolut-txs"

