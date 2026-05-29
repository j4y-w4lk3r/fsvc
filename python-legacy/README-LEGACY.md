# python-legacy/ — preserved historical Python codebase

This directory holds the original Python implementation of the f-service
project (the predecessor to fsvc), preserved verbatim for reference during
the v0.1.0 Go rewrite.

**This code is not built or installed by the fsvc Go module.** It is here
solely as documentation of the working OAuth + mTLS + JWT flow and as a
porting reference. The encrypted SOPS artifacts under `rev/` are
**unrecoverable** — they were bound to a YubiKey GPG fingerprint that was
wiped during the Phase 0a YubiKey factory-reset, so the encrypted blobs
cannot be decrypted by any currently-provisioned key.

## What's here

```
python-legacy/
├── .gitignore                # protective gitignore (pem/key/crt/env)
├── .sops.yaml                # SOPS rules (bound to wiped GPG fpr — DEAD)
├── run.sh                    # bash orchestrator: venv + sops decrypt + dispatch
├── ing/                      # ING (PolishAPI 2.1) — read-only get_balance
│   ├── README.md             # detailed PolishAPI explanation
│   ├── get_balance.py        # POST getAccounts → POST getAccount → balance
│   └── requirements.txt      # requests
└── rev/                      # Revolut Business API (mTLS + JWT client_assertion)
    ├── requirements.txt      # certifi, requests, urllib3
    ├── revolut_cert_setup.sh         # generate mTLS keypair (→ port to `fsvc cert`)
    ├── revolut_make_client_assertion.sh  # build RS256 JWT (→ port to internal/revolut/jwt.go)
    ├── revolut_exchange_code.py      # one-shot OAuth code → token exchange
    ├── revolut_refresh_token.py      # refresh_token → new access_token (→ port to `fsvc revolut auth`)
    ├── revolut_list_transactions.py  # GET /transactions (→ port to `fsvc revolut txs`)
    ├── revolut_smoke.py              # accounts + cards (→ port to `fsvc revolut accounts/cards`)
    ├── revolut.env.sops.env          # SOPS-encrypted env (DEAD — wiped GPG fpr)
    └── revolut-api-cert/
        ├── privatecert.pem.enc       # DEAD
        ├── revolut_private.key.enc   # DEAD
        └── revolut_public.crt.enc    # DEAD
```

## Why we're rewriting in Go rather than reviving

See the main [README.md](../README.md#why-a-python--go-rewrite-rather-than-reviving-the-existing-scripts).

## Porting checklist for v0.1.0

| Python file | Maps to | Notes |
|-------------|---------|-------|
| `rev/revolut_cert_setup.sh` | `cmd: fsvc cert` | Use `crypto/rsa` + `crypto/x509` instead of openssl shell. Same output: `revolut_private.key` + `revolut_public.crt`. |
| `rev/revolut_make_client_assertion.sh` | `internal/revolut/jwt.go` | Use `github.com/golang-jwt/jwt/v5` for RS256 signing. Header / payload / sign all in-process; no openssl shell. |
| `rev/revolut_refresh_token.py` | `cmd: fsvc revolut auth` | Same flow: regenerate JWT → POST /auth/token (mTLS) → update revolut.env. |
| `rev/revolut_smoke.py:get_accounts` | `cmd: fsvc revolut accounts` | mTLS http.Client + bearer token. |
| `rev/revolut_smoke.py:get_cards` | `cmd: fsvc revolut cards` | Same client. |
| `rev/revolut_list_transactions.py` | `cmd: fsvc revolut txs` | Add `--account ID --from DATE --to DATE` flags. |
| `run.sh:decrypt_if_missing` (sops) | `internal/storage` | Replace SOPS with `ykw decrypt` shell-out. Plaintext lives only in `$XDG_CONFIG_HOME/fsvc/` after `fsvc bootstrap`. |

ING (`ing/`) ports later — v0.3.0 at the earliest. The PolishAPI 2.1 flow is more involved (PSD2 TPP registration, JWS body signing), and Revolut alone covers the immediate use case.
