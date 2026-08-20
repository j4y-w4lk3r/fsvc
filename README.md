# fsvc — financial-services CLI for the j4y suite

A focused CLI for personal banking APIs (Revolut Business today, ING tomorrow), with the same secrets-management story as the rest of the suite: encrypted at rest via [`ykw encrypt`](https://github.com/j4y-w4lk3r/ykw), decrypted on demand with a YubiKey, never written to disk in plaintext.

Sister tool to [`rui`](https://github.com/j4y-w4lk3r/rui) (router TUI), [`bbm`](https://github.com/j4y-w4lk3r/bbm) (Backblaze B2 manager), and [`ykw`](https://github.com/j4y-w4lk3r/ykw) (YubiKey GPG workflow). Same packaging shape, same release cadence, same single-tag-triggers-three-channels distribution (planned for v0.1.0+).

## Status: v0.0.2-rc.1 — Revolut Business read-only, live-tested against production

The Revolut Business client is implemented, unit-tested, and validated against the production b2b.revolut.com endpoint (accounts + cards + 30-day txs all returning real data). What you can do today:

```bash
fsvc cert --copy                     # generate the mTLS keypair + copy public PEM
                                     # (paste into Revolut Business → Settings → APIs → Add cert)
fsvc init --client-id <ID> \
          --redirect-uri <URI>       # write ~/.config/fsvc/config.toml
fsvc revolut authorize               # opens browser, captures code, exchanges for tokens
                                     # (auto-detects http://localhost vs https:// redirect URI)
fsvc revolut accounts                # list accounts (with per-currency totals)
fsvc revolut cards                   # list cards
fsvc revolut txs --days 30           # list recent transactions
fsvc revolut txs --json | jq .       # raw JSON for piping
fsvc revolut status                  # local diagnostic (token freshness, config sanity)
```

`fsvc revolut authorize` runs in two modes depending on your `redirect_uri`:

- **localhost mode** (`http://localhost:PORT/...`): spawns a tiny one-shot HTTP server, opens the browser, captures the code automatically. Best for sandbox/dev.
- **paste-back mode** (any `https://` URL — Revolut production refuses HTTP redirect URIs): opens the browser, prompts you to paste the redirected URL or just the code. Best for production.

403 responses with code `9002` / `33004` (IP whitelisting) get a friendly hint pointing at the Revolut portal — including the `curl ifconfig.me` one-liner to fetch your current public IP.

Distribution (Homebrew + AUR + meta-suite) is queued for v0.1.0; the tag triggers when goreleaser + CI are wired.

## Bootstrap from 1Password (`x-fsvc` vault)

Materialize `~/.config/fsvc/` on a new machine:

```bash
./scripts/bootstrap-from-1pass.sh -f   # requires `op` signed in
fsvc revolut auth                      # refresh access_token
```

Vault **`x-fsvc`** items:

| Item | Purpose |
|---|---|
| `fsvc revolut config` | `client_id`, `iss`, `redirect_uri`, `refresh_token`, `base_url` |
| `rev bus api private key` | `revolut_private.key` document |
| `rev bus api public cert` | `revolut_public.crt` document |

The public cert in 1Password **must match** the cert uploaded in Revolut Business
→ Settings → APIs for the active `client_id`.

See [docs/PLAN.md](./docs/PLAN.md) for NAS + Tailscale setup.

What's preserved under [`python-legacy/`](./python-legacy):

- `rev/revolut_cert_setup.sh` — port lives in `internal/command/cert.go`
- `rev/revolut_make_client_assertion.sh` — port lives in `internal/revolut/jwt.go`
- `rev/revolut_refresh_token.py` + `revolut_exchange_code.py` — port lives in `internal/revolut/auth.go`
- `rev/revolut_smoke.py` + `revolut_list_transactions.py` — port lives in `internal/revolut/api.go` + `internal/command/revolut.go`
- `ing/` — kept verbatim for the v0.3.0+ ING port

Why a Python → Go rewrite rather than reviving the existing scripts:

1. **Distribution.** `rui` / `bbm` / `ykw` ship as a single binary via Homebrew + AUR. A Python codebase with a venv + mTLS PEMs + an OAuth bash orchestrator is much more friction at install time. One binary is cleaner.
2. **Secrets story.** The legacy code uses [SOPS](https://github.com/getsops/sops). The whole `bu`-bundle / `ykw encrypt` story is built around per-recipient YubiKey-bound GPG, and `ykw` already does this end-to-end. Re-encrypting through `ykw` removes the SOPS dependency and ties fsvc into the suite's encryption + bootstrap story.
3. **TUI room.** The eventual shape of fsvc is interactive — see balances, scrub through transactions, manually classify. A Bubble Tea TUI is far easier in Go than Python.

The `python-legacy/` directory is preserved for reference. It is **not** built or installed.

## Roadmap

### v0.0.1 — scaffold
- [x] Go module structure (`cmd/fsvc`, `internal/{command,config,revolut}`)
- [x] Subcommand dispatcher with placeholders
- [x] LICENSE + .gitignore + README
- [x] Python codebase preserved under `python-legacy/`

### v0.0.2-rc.1 — Revolut Business read-only (live-tested)
- [x] `fsvc cert` — generate Revolut mTLS keypair + `--print` / `--copy` for portal handoff
- [x] `fsvc init` — write `~/.config/fsvc/config.toml` from CLI flags
- [x] `fsvc revolut authorize` — automated localhost / paste-back OAuth code-for-tokens flow
- [x] `fsvc revolut exchange-code <CODE>` — manual one-shot variant (still supported)
- [x] `fsvc revolut auth` — JWT client_assertion + refresh_token rotation
- [x] `fsvc revolut accounts` — list accounts with per-currency totals
- [x] `fsvc revolut cards` — list cards
- [x] `fsvc revolut txs` — list transactions (`--days`, `--from`, `--to`, `--account`, `--json`)
- [x] `fsvc revolut status` — local diagnostic (token freshness, config sanity)
- [x] Friendly hint on Revolut error codes 9002 / 33004 (IP whitelisting)
- [x] Config in `$XDG_CONFIG_HOME/fsvc/config.toml`; atomic-rename Save on token rotation
- [x] Unit tests: JWT round-trip, config save/load, env override, code-extraction parser
- [x] Live-API smoke test against production Revolut Business endpoint

### v0.1.0 — distribution
- [ ] `fsvc bootstrap` — pull all `b2://j4y-bu/fsvc/*.gpg` via `bbm`, decrypt via `ykw`
- [ ] goreleaser + Homebrew tap (`j4y-w4lk3r/homebrew-fsvc`) + AUR (`fsvc-bin`)
- [ ] CI workflows (`go vet`, `go test`, goreleaser snapshot build)

### v0.2.0 — Bubble Tea TUI
- [ ] Interactive accounts / cards / transactions browser
- [ ] Filter + search across transactions
- [ ] Manual transaction classification + export

### v0.3.0+ — ING bank
- [ ] PolishAPI 2.1 client (port of `python-legacy/ing/get_balance.py`)
- [ ] PSD2 TPP registration helper
- [ ] JWS body signing for ING request bodies

Open-ended (no committed version):
- Wise / N26 / other neobanks via the same plugin shape
- Categorization rules engine (e.g. "merchant matches `^uber\\b` → category `transport`")

## Architecture (target shape, v0.1.0)

```
fsvc
├── cmd/fsvc/main.go              # dispatcher
├── internal/
│   ├── command/                  # one file per subcommand (cert.go, revolut.go, ...)
│   ├── config/                   # TOML config + cred cascade (file → env → flag)
│   └── revolut/
│       ├── client.go             # mTLS http.Client + auth header
│       ├── jwt.go                # client_assertion JWT signing (RS256)
│       └── api.go                # accounts/cards/txs endpoints
├── python-legacy/                # preserved historical reference
└── ...
```

Cred storage:

```
~/.config/fsvc/                   # plaintext, materialized by `fsvc bootstrap`
├── config.toml
├── revolut.env                   # base_url, client_id, iss, refresh_token, ...
└── revolut-api-cert/
    ├── revolut_private.key
    └── revolut_public.crt

b2://j4y-bu/fsvc/                 # encrypted-at-rest canonical store
├── config.toml.gpg
├── revolut.env.gpg
├── revolut_private.key.gpg
└── revolut_public.crt.gpg
```

Each `.gpg` is encrypted via `ykw encrypt`, so any of the configured YubiKeys can decrypt. `fsvc bootstrap` chains `bbm pull → ykw decrypt → materialize` for every blob in the prefix. Same shape as `ykw bootstrap` but for fsvc creds rather than the suite-wide `bu` bundle.

## Why a separate B2 prefix from `bu/`?

The `bu/` bundle is small (~10 KB) and meant to fit in a single `ykw bootstrap` decrypt operation. Adding service-specific creds (revolut + future ING + N26 + ...) inside it would balloon the bundle and couple unrelated rotation cadences. Keeping `b2://j4y-bu/fsvc/` separate means:

- fsvc can rotate its certs without touching the bu bundle.
- `ykw bootstrap` stays small + fast (just `secret.txt` + `pubkeys/`).
- `fsvc bootstrap` is independent and can be run on demand.

## Install (planned, v0.1.0+)

### macOS / Linux (Homebrew)

```bash
brew install j4y-w4lk3r/fsvc/fsvc
```

### Arch Linux (AUR)

```bash
yay -S fsvc-bin   # or paru, etc.
```

### From source

```bash
git clone https://github.com/j4y-w4lk3r/fsvc
cd fsvc
go install ./cmd/fsvc
```

## Configuration (planned, v0.1.0+)

`fsvc` reads config from a cascade. Per-field, the first non-empty wins:

1. CLI flags
2. Process env vars (`REVOLUT_*`, `ING_*`, ...)
3. Plaintext file (`$XDG_CONFIG_HOME/fsvc/revolut.env`, etc.) — materialized by `fsvc bootstrap`
4. Encrypted file in B2 (`b2://j4y-bu/fsvc/revolut.env.gpg`) — fetched + decrypted on demand

Plaintext files are only created at runtime by `fsvc bootstrap`, in mode 0600, under `$XDG_CONFIG_HOME/fsvc/` (default `~/.config/fsvc/`). They are never committed to this repo (the `.gitignore` enforces that).

## License

MIT — see [LICENSE](./LICENSE).
