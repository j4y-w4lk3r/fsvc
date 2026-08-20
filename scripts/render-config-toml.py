#!/usr/bin/env python3
"""Render ~/.config/fsvc/config.toml from 1Password item JSON on stdin."""

from __future__ import annotations

import json
import sys
from pathlib import Path


def field_map(item: dict) -> dict[str, str]:
    out: dict[str, str] = {}
    for f in item.get("fields", []):
        label = f.get("label") or f.get("id") or ""
        if not label or label == "notesPlain":
            continue
        out[label] = f.get("value") or ""
    return out


def main() -> int:
    item = json.load(sys.stdin)
    vals = field_map(item)

    required = ("client_id", "iss", "redirect_uri", "refresh_token")
    missing = [k for k in required if not vals.get(k)]
    if missing:
        print(f"missing required fields: {', '.join(missing)}", file=sys.stderr)
        return 1

    base_url = vals.get("base_url") or "https://b2b.revolut.com/api/1.0"
    config_dir = Path(
        sys.argv[1] if len(sys.argv) > 1 else f"{Path.home()}/.config/fsvc"
    )
    cert_path = config_dir / "revolut-api-cert" / "revolut_public.crt"
    key_path = config_dir / "revolut-api-cert" / "revolut_private.key"

    print("[revolut]")
    print(f'base_url = "{base_url}"')
    print(f'client_id = "{vals["client_id"]}"')
    print(f'iss = "{vals["iss"]}"')
    print(f'redirect_uri = "{vals["redirect_uri"]}"')
    print(f'cert_path = "{cert_path}"')
    print(f'key_path = "{key_path}"')
    print('access_token = ""')
    print('refresh_token = "{}"'.format(vals["refresh_token"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
