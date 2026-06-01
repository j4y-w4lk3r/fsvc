#!/usr/bin/env bash
set -euo pipefail

# Always operate from the repo root (directory of this script)
cd "$(dirname "${BASH_SOURCE[0]:-$0}")"

# Make sure GPG can talk to the current TTY (needed for YubiKey PIN)
export GPG_TTY="${GPG_TTY:-$(tty)}"

usage() {
  cat <<'EOF'
Usage:
  ./run.sh [--sandbox|--prod] <command> [args]
  ./run.sh -h|--help

Global options:
  --sandbox    Use Revolut Sandbox endpoints and env file (rev/revolut.sandbox.env.sops.env)
  --prod       Use Revolut Production endpoints and env file (rev/revolut.env.sops.env) [default]

Commands:
  revolut-txs            Refresh token (if possible) and list transactions
  revolut-cards          List virtual cards (use --all to include physical)
  revolut-smoke          Fetch and print accounts + cards (no formatting guarantees)
  revolut-oauth-bootstrap  Print consent URL and exchange auth code for tokens
  revolut-sandbox-env     Create encrypted sandbox env (interactive)

Revolut env files:
  - Production: rev/revolut.env.sops.env
  - Sandbox:    rev/revolut.sandbox.env.sops.env

Notes:
  - The consent/2FA step cannot be fully automated via API. We can generate the consent
    URL and then exchange the returned `code` automatically.
  - Secrets (JWT/tokens) are intentionally not printed by default.
EOF
}

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

REV_ENV="prod"
CMD=""
CMD_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --sandbox)
      REV_ENV="sandbox"
      shift
      ;;
    --prod)
      REV_ENV="prod"
      shift
      ;;
    *)
      if [[ -z "$CMD" ]]; then
        CMD="$1"
      else
        CMD_ARGS+=("$1")
      fi
      shift
      ;;
  esac
done

if [[ -z "$CMD" ]]; then
  usage
  exit 1
fi

needs_revolut=0
case "$CMD" in
  revolut-txs|revolut-cards|revolut-smoke|revolut-oauth-bootstrap)
    needs_revolut=1
    ;;
esac

REV_ENV_SOPS="rev/revolut.env.sops.env"
REV_ENV_PLAIN="rev/revolut.env"
if [[ "$REV_ENV" == "sandbox" ]]; then
  REV_ENV_SOPS="rev/revolut.sandbox.env.sops.env"
  REV_ENV_PLAIN="rev/revolut.sandbox.env"
fi

if [[ "$needs_revolut" == "1" ]]; then
  echo "[run.sh] Ensuring Python virtualenv and dependencies..."
  ensure_venv

  echo "[run.sh] Checking decrypted secrets..."

  if [[ ! -f "$REV_ENV_SOPS" && ! -f "$REV_ENV_PLAIN" ]]; then
    echo "[run.sh] ERROR: Revolut env file not found." >&2
    echo "[run.sh] Expected one of:" >&2
    echo "  - $REV_ENV_SOPS (encrypted, preferred)" >&2
    echo "  - $REV_ENV_PLAIN (plaintext, ignored by git)" >&2
    echo "[run.sh] Create the encrypted file with SOPS, or copy your env into the plaintext file." >&2
    exit 2
  fi

  # Revolut env (dotenv-style exports)
  decrypt_if_missing \
    "$REV_ENV_PLAIN" \
    "$REV_ENV_SOPS" \
    "dotenv"

  # Revolut client certificate and private key
  decrypt_if_missing \
    "rev/revolut-api-cert/privatecert.pem" \
    "rev/revolut-api-cert/privatecert.pem.enc"

  decrypt_if_missing \
    "rev/revolut-api-cert/revolut_private.key" \
    "rev/revolut-api-cert/revolut_private.key.enc"

  # Revolut public certificate
  decrypt_if_missing \
    "rev/revolut-api-cert/revolut_public.crt" \
    "rev/revolut-api-cert/revolut_public.crt.enc"

  echo "[run.sh] Decryption checks finished."

  echo "[run.sh] Loading Revolut env into subshell..."
  eval "$(sops -d --input-type dotenv --output-type dotenv "$REV_ENV_SOPS")"

  REV_DIR="$(pwd)/rev"
  export REVOLUT_CERT_PATH="$REV_DIR/revolut-api-cert/revolut_public.crt"
  export REVOLUT_KEY_PATH="$REV_DIR/revolut-api-cert/revolut_private.key"
fi

refresh_and_persist_tokens() {
  echo "[run.sh] Refreshing Revolut access token using refresh_token..."
  if .venv/bin/python rev/revolut_refresh_token.py; then
    echo "[run.sh] Updating encrypted env snapshot with latest tokens..."
    cp "$REV_ENV_PLAIN" "$REV_ENV_SOPS"
    sops --encrypt --in-place --input-type dotenv --output-type dotenv "$REV_ENV_SOPS"

    echo "[run.sh] Reloading Revolut env from updated encrypted file..."
    eval "$(sops -d --input-type dotenv --output-type dotenv "$REV_ENV_SOPS")"
    export REVOLUT_CERT_PATH="$REV_DIR/revolut-api-cert/revolut_public.crt"
    export REVOLUT_KEY_PATH="$REV_DIR/revolut-api-cert/revolut_private.key"
  else
    echo "[run.sh] WARNING: refresh_token flow failed; continuing with existing token." >&2
  fi
}

case "$CMD" in
  revolut-sandbox-env)
    echo "[run.sh] Creating encrypted sandbox env..."
    bash rev/revolut_sandbox_env_setup.sh
    ;;
  revolut-txs)
    refresh_and_persist_tokens
    echo "[run.sh] Running Revolut transactions listing..."
    .venv/bin/python rev/revolut_list_transactions.py "${CMD_ARGS[@]}"
    ;;
  revolut-cards)
    # If refresh token exists, keep the token fresh; otherwise just list using REVOLUT_TOKEN.
    if [[ -n "${REVOLUT_REFRESH_TOKEN:-}" ]]; then
      refresh_and_persist_tokens
    fi
    echo "[run.sh] Running Revolut cards listing..."
    .venv/bin/python rev/revolut_list_cards.py "${CMD_ARGS[@]}"
    ;;
  revolut-smoke)
    if [[ -n "${REVOLUT_REFRESH_TOKEN:-}" ]]; then
      refresh_and_persist_tokens
    fi
    echo "[run.sh] Running Revolut smoke..."
    .venv/bin/python rev/revolut_smoke.py "${CMD_ARGS[@]}"
    ;;
  revolut-oauth-bootstrap)
    echo "[run.sh] Running Revolut OAuth bootstrap..."
    .venv/bin/python rev/revolut_oauth_bootstrap.py "${CMD_ARGS[@]}"
    ;;
  *)
    echo "[run.sh] Unknown command: $CMD" >&2
    echo >&2
    usage >&2
    exit 2
    ;;
esac

