import argparse
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlencode

import requests

from revolut_refresh_token import ensure_mtls_env, load_client_assertion, make_client_assertion
from revolut_smoke import create_session, get_accounts


def script_dir() -> Path:
    return Path(__file__).resolve().parent


def env(name: str) -> str:
    val = os.getenv(name)
    if not val:
        raise RuntimeError(f"Missing required env var: {name}")
    return val


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description=(
            "Bootstrap Revolut Business API OAuth tokens.\n\n"
            "This can automate everything except the consent/2FA step, which must be done "
            "in the Revolut Business web/app UI."
        ),
        formatter_class=argparse.RawTextHelpFormatter,
    )
    p.add_argument(
        "--app-confirm-base",
        default=os.getenv("REVOLUT_APP_CONFIRM_BASE", "").strip() or None,
        help=(
            "Base URL for the consent page.\n"
            "Defaults: sandbox -> https://sandbox-business.revolut.com, prod -> https://business.revolut.com"
        ),
    )
    p.add_argument(
        "--scope",
        default=os.getenv("REVOLUT_SCOPE", "").strip() or None,
        help="Optional scope, e.g. READ or READ,WRITE (comma-separated).",
    )
    p.add_argument(
        "--code",
        default=os.getenv("REVOLUT_AUTH_CODE"),
        help="Authorization code (if you already have it).",
    )
    p.add_argument(
        "--exchange-only",
        action="store_true",
        help="Skip printing the consent URL; only exchange --code for tokens.",
    )
    return p.parse_args()


def default_app_confirm_base() -> str:
    base_url = os.getenv("REVOLUT_BASE_URL", "")
    if "sandbox-" in base_url:
        return "https://sandbox-business.revolut.com"
    return "https://business.revolut.com"


def build_consent_url(
    app_confirm_base: str, client_id: str, redirect_uri: str, scope: str | None
) -> str:
    qs = {
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "response_type": "code",
    }
    if scope:
        qs["scope"] = scope
    return f"{app_confirm_base.rstrip('/')}/app-confirm?{urlencode(qs)}"


def exchange_code_with_python(code: str) -> None:
    # We reuse the existing script so token persistence stays in one place.
    subprocess.run(
        [sys.executable, str(script_dir() / "revolut_exchange_code.py"), code], check=True
    )


def smoke_accounts() -> None:
    session = create_session()
    accounts = get_accounts(session)
    if isinstance(accounts, list):
        print()
        print("Accounts:")
        for acc in accounts:
            print(
                f"- id={acc.get('id')} "
                f"currency={acc.get('currency')} "
                f"balance={acc.get('balance')}"
            )
        return
    print()
    print("Accounts response (non-list):")
    print(accounts)


def main() -> None:
    args = parse_args()

    # Ensure mTLS and (for exchange) token plumbing is ready.
    ensure_mtls_env()

    client_id = env("REVOLUT_CLIENT_ID")
    redirect_uri = env("REVOLUT_REDIRECT_URI")

    app_confirm_base = args.app_confirm_base or default_app_confirm_base()

    print("Regenerating client assertion (JWT)...")
    make_client_assertion()
    _ = load_client_assertion()

    if not args.exchange_only:
        print()
        print("Consent URL (open in browser, authorise, then copy the `code=` param):")
        print(build_consent_url(app_confirm_base, client_id, redirect_uri, args.scope))

    if not args.code:
        print()
        print("Next step:")
        print("  - Re-run with: REVOLUT_AUTH_CODE=... python rev/revolut_oauth_bootstrap.py --exchange-only")
        return

    print()
    print("Exchanging authorization code for tokens (writes to rev/revolut.env)...")
    exchange_code_with_python(args.code)

    print()
    print("Trying an authenticated /accounts request with the new token...")
    try:
        smoke_accounts()
    except requests.HTTPError as exc:
        print("HTTP error during /accounts smoke:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)


if __name__ == "__main__":
    main()

