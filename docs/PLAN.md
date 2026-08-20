# fsvc — future tasks & bootstrap plan

Living doc for multi-machine setup, secrets storage, and automation.
Last updated: 2026-08-20.

---

## Current state (working)

| Piece | Location |
|---|---|
| OAuth redirect | `https://oauth.n1nj4.tech/rev/callback` (Caddy on NAS) |
| DNS | `oauth.n1nj4.tech` → home public IP (`83.5.204.23`) |
| Revolut cert | New API cert, client_id in `~/.config/fsvc/config.toml` |
| `iss` | `oauth.n1nj4.tech` (host of redirect URI) |
| Production IP whitelist | `83.5.204.23` (home Orange egress) |
| Local creds (this Mac) | `~/.config/fsvc/config.toml` + `revolut-api-cert/` |
| 1Password backup | `x-fsvc` vault — public cert + private key (duplicate items; needs cleanup) |

---

## Architecture decisions

### Can Revolut whitelist a domain (`n1nj4.tech`) instead of an IP?

**No.** Production IP whitelist accepts **IPv4/IPv6 addresses only**.
Revolut checks the **source IP of the TLS connection**, not DNS or hostname.
Pointing `n1nj4.tech` at `83.5.204.23` does not help — a laptop on a café Wi‑Fi still
egresses from a different IP.

**Alternatives for stable whitelist:**

1. Whitelist home IP `83.5.204.23` (works for all devices on that NAT).
2. Run `fsvc` on **NAS** — always same home egress IP.
3. Run `fsvc` on **Hostinger VPS** (`147.93.121.136`) — static IP, whitelist once.
4. Route outbound traffic via **Tailscale exit node** on VPS — Revolut sees VPS IP.

### Does Headscale/Tailscale replace IP whitelist or local creds?

**No.** Tailscale gives private reachability (`100.x.x.x`). Revolut API calls go over
the public internet and Revolut sees **public egress IP**.

Tailscale **does** help with this pattern:

```
New Mac (Tailscale) ──SSH──► NAS (Tailscale + fsvc installed)
                                  │
                                  └── HTTPS ──► Revolut (source IP = 83.5.204.23)
```

**Pros:** no secrets on every laptop; one whitelist IP; NAS always on.
**Cons:** must SSH (or alias); OAuth paste-back still needs a browser on first setup.

### Recommended target architecture

**Primary:** `fsvc` installed on **NAS** (`j4y@nas.tail.d0j0.dev`).

```bash
# From any Tailscale-connected device:
ssh j4y@nas.tail.d0j0.dev fsvc revolut cards
```

**Secrets canonical store:** 1Password vault **`x-fsvc`** (short term) →
`fsvc bootstrap` from B2 + `ykw` (long term, per README v0.1.0).

---

## 1Password vault schema (`x-fsvc`)

### Target items (consolidate duplicates)

| 1Password item | Fields / attachments | Maps to |
|---|---|---|
| **fsvc revolut config** | `client_id`, `iss`, `redirect_uri`, `refresh_token`, `base_url` | `config.toml` `[revolut]` |
| **rev bus api private key** | file: `revolut_private.key` | `~/.config/fsvc/revolut-api-cert/revolut_private.key` |
| **rev bus api public cert** | file: `revolut_public.crt` | `~/.config/fsvc/revolut-api-cert/revolut_public.crt` |

Optional metadata fields on config item:

- `production_ip_whitelist` — note only (`83.5.204.23`); Revolut portal is source of truth
- `revolut_portal_url` — `https://business.revolut.com/settings/apis?tab=business-api`
- `oauth_callback_url` — `https://oauth.n1nj4.tech/rev/callback`

### Cleanup task

- [x] Remove duplicate private key / public cert items in `x-fsvc` (kept March 2026 copies).
- [x] Create **fsvc revolut config** item (`q7cjcd5tszkx6nlnn2pdg6of4e`).
- [ ] **Important:** 1Password key/cert must match the cert uploaded in Revolut portal.
      If you uploaded a different public cert to Revolut than the one in `x-fsvc`,
      either update Revolut (add new cert) or update 1Password documents before bootstrap.

---

## Future tasks

### P0 — NAS as fsvc host

- [x] Install `fsvc` on NAS (`yay -S fsvc-bin`).
- [x] Materialize `~/.config/fsvc/` on NAS (rsync; bootstrap once `op signin` works over SSH).
- [ ] Verify from Tailscale Mac: `ssh j4y@nas.tail.d0j0.dev fsvc revolut cards`.
- [ ] Optional shell alias on Mac: `alias fsvc-nas='ssh j4y@nas.tail.d0j0.dev fsvc'`.

### P1 — 1Password bootstrap script

- [x] `scripts/bootstrap-from-1pass.sh` + `scripts/render-config-toml.py`
- [x] Vault: **`x-fsvc`** (override: `FSVC_1PASS_VAULT`)
- [x] Item **`fsvc revolut config`**
- [x] Duplicate cert/key items removed (kept March 2026 copies)
- [ ] NAS: `eval "$(op signin)"` then `~/px/fsvc-scripts/bootstrap-from-1pass.sh -f`
- [ ] Document in README

**Usage (Mac, signed into 1Password):**

```bash
cd /Users/wgm0/px/x-j4y/fsvc
./scripts/bootstrap-from-1pass.sh -f
fsvc revolut auth
```

**Warning:** bootstrap overwrites local keypair with 1Password documents. The public
cert in `x-fsvc` must match the cert registered in Revolut for the active `client_id`.

### P1 — Sync config back to 1Password after token refresh

- [ ] Hook or helper: after `fsvc revolut auth` / API auto-refresh, update
  `refresh_token` (and optionally `access_token`) in 1Password item.
- [ ] Prevents drift when NAS holds live config but 1Password has stale refresh token.

### P2 — Native `fsvc bootstrap` (README v0.1.0)

- [ ] `bbm pull` from `b2://j4y-bu/fsvc/*.gpg`
- [ ] `ykw decrypt` → materialize `~/.config/fsvc/`
- [ ] 1Password bootstrap remains valid fallback for machines without YubiKey/B2.

### P2 — OAuth on new machines

- [ ] Document re-auth flow: `fsvc revolut authorize` → paste URL from
  `https://oauth.n1nj4.tech/rev/callback?code=...`
- [ ] Optional: short link or internal doc for non-technical steps.

### P2 — Dynamic home IP

- [ ] Script: detect public IP change → update Hostinger DNS (if needed) +
  prompt to update Revolut whitelist (Hostinger API + manual Revolut step).
- [ ] Or migrate API egress to VPS static IP to avoid churn.

### P3 — ING, TUI, distribution

- [ ] Per README roadmap (v0.2.0 TUI, v0.3.0 ING, goreleaser v0.1.0).

---

## New Mac quick reference (target state)

```bash
# 1. Tailscale / Headscale connected
# 2. Option A — remote fsvc on NAS (preferred)
ssh j4y@nas.tail.d0j0.dev fsvc revolut cards

# 2. Option B — local fsvc
brew install j4y-w4lk3r/fsvc/fsvc
./scripts/bootstrap-from-1pass.sh   # once implemented
fsvc revolut cards                  # only if egress IP is whitelisted
```

**Required for local fsvc:** binary + `~/.config/fsvc/` + whitelisted public IP.
**Required for NAS fsvc over Tailscale:** SSH access only (creds stay on NAS).

---

## Security notes

- `revolut_private.key` and `refresh_token` are long-lived secrets — treat like passwords.
- 1Password vault `x-fsvc` should have minimal ACL.
- Do not commit `config.toml` or keys to git (already in `.gitignore`).
- OAuth redirect URL is not secret; mTLS key + refresh token are.
