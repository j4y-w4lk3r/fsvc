#!/usr/bin/env python3
"""
Fetch ING account balance via AIS API (PolishAPI 2.1).

Flow:
  1. POST getAccounts → get accountNumber(s)
  2. POST getAccount(accountNumber) → get detailed account info including balance

Requires:
  - ING_ACCESS_TOKEN (Bearer token from ING consent flow)
  - Request body signing: set ING_JWS_GET_ACCOUNTS and ING_JWS_GET_ACCOUNT,
    or set ING_SIGN_SCRIPT to a script that reads JSON body from stdin and prints JWS to stdout.

Optional:
  - ING_BASE_URL (default: sandbox)
  - ING_ACCOUNT_NUMBER (if set, skip getAccounts and call getAccount only; then only ING_JWS_GET_ACCOUNT is needed)
  - ING_REQUEST_ID (UUID; use fixed value so signatures are reproducible)
"""

import json
import os
import subprocess
import sys
import uuid

import requests

# -----------------------------------------------------------------------------
# Config from env
# -----------------------------------------------------------------------------
BASE_URL = os.environ.get(
    "ING_BASE_URL",
    "https://devportal.ing.pl/sandbox/api",
).rstrip("/")
ACCESS_TOKEN = os.environ.get("ING_ACCESS_TOKEN")
ACCOUNT_NUMBER = os.environ.get("ING_ACCOUNT_NUMBER")  # optional: skip getAccounts
REQUEST_ID = os.environ.get("ING_REQUEST_ID") or str(uuid.uuid4())
JWS_GET_ACCOUNTS = os.environ.get("ING_JWS_GET_ACCOUNTS")
JWS_GET_ACCOUNT = os.environ.get("ING_JWS_GET_ACCOUNT")
SIGN_SCRIPT = os.environ.get("ING_SIGN_SCRIPT")

# API paths (ING uses v2_1.1)
PATH_GET_ACCOUNTS = "/api/v2_1.1/accounts/v2_1.1/getAccounts"
PATH_GET_ACCOUNT = "/api/v2_1.1/accounts/v2_1.1/getAccount"


def build_request_header():
    return {
        "requestId": REQUEST_ID,
        "userAgent": "ING-Balance-Script/1.0",
        "ipAddress": "127.0.0.1",
        "sendDate": "",  # optional; bank may accept without
        "tppId": os.environ.get("ING_TPP_ID", ""),
        "token": ACCESS_TOKEN or "",
        "isDirectPsu": True,
        "callbackURL": os.environ.get("ING_CALLBACK_URL", ""),
        "apiKey": os.environ.get("ING_API_KEY", ""),
    }


def sign_body(body_json: str) -> str:
    if not SIGN_SCRIPT:
        return ""
    try:
        out = subprocess.run(
            [SIGN_SCRIPT],
            input=body_json.encode("utf-8"),
            capture_output=True,
            timeout=30,
        )
        if out.returncode != 0:
            print("Sign script stderr:", out.stderr.decode("utf-8", replace="replace"), file=sys.stderr)
            return ""
        return out.stdout.decode("utf-8").strip()
    except Exception as e:
        print("Sign script error:", e, file=sys.stderr)
        return ""


def post(path: str, body: dict, jws_signature: str) -> requests.Response:
    url = BASE_URL + path
    body_json = json.dumps(body, separators=(",", ":"))
    headers = {
        "Authorization": f"Bearer {ACCESS_TOKEN}",
        "Accept-Encoding": "gzip, deflate",
        "Accept-Language": "pl",
        "Accept-Charset": "utf-8",
        "Content-Type": "application/json",
        "X-REQUEST-ID": REQUEST_ID,
    }
    if jws_signature:
        headers["X-JWS-SIGNATURE"] = jws_signature
    return requests.post(url, data=body_json, headers=headers, timeout=30)


def get_accounts() -> list:
    body = {
        "requestHeader": build_request_header(),
        "pageId": "",
        "perPage": 25,
    }
    body_json = json.dumps(body, separators=(",", ":"))
    sig = JWS_GET_ACCOUNTS or sign_body(body_json)
    resp = post(PATH_GET_ACCOUNTS, body, sig)
    if resp.status_code != 200:
        print("getAccounts failed:", resp.status_code, resp.text, file=sys.stderr)
        if not sig:
            print("\nTo fix: sign the request body and set ING_JWS_GET_ACCOUNTS (or use ING_SIGN_SCRIPT).", file=sys.stderr)
        sys.exit(1)
    data = resp.json()
    accounts = data.get("accounts") or []
    return accounts


def get_account(account_number: str) -> dict:
    body = {
        "requestHeader": build_request_header(),
        "accountNumber": account_number,
    }
    body_json = json.dumps(body, separators=(",", ":"))
    sig = JWS_GET_ACCOUNT or sign_body(body_json)
    resp = post(PATH_GET_ACCOUNT, body, sig)
    if resp.status_code != 200:
        print("getAccount failed:", resp.status_code, resp.text, file=sys.stderr)
        if not sig:
            print("\nTo fix: sign the request body and set ING_JWS_GET_ACCOUNT (or use ING_SIGN_SCRIPT).", file=sys.stderr)
        sys.exit(1)
    return resp.json()


def extract_balance(account_response: dict) -> None:
    """Print balance from getAccount response. PolishAPI may use 'balance' or 'balances' array."""
    account = account_response.get("account") or account_response
    # Single balance object
    balance = account.get("balance")
    if balance is not None:
        amount = balance.get("amount") or balance.get("value")
        currency = balance.get("currency") or balance.get("currencyCode") or ""
        if amount is not None:
            print(f"Balance: {amount} {currency}".strip())
            return
    # Balances array (e.g. available, booked)
    balances = account.get("balances") or []
    for b in balances:
        amount = b.get("amount") or b.get("value")
        currency = b.get("currency") or b.get("currencyCode") or ""
        btype = b.get("type") or b.get("balanceType") or ""
        if amount is not None:
            label = f" ({btype})" if btype else ""
            print(f"Balance{label}: {amount} {currency}".strip())
    if not balances and balance is None:
        print("Balance field not found in response. Raw account keys:", list(account.keys()))
        print("Full response (account):", json.dumps(account, indent=2))


def main():
    if not ACCESS_TOKEN:
        print("Set ING_ACCESS_TOKEN (Bearer token from ING consent flow).", file=sys.stderr)
        sys.exit(1)

    if ACCOUNT_NUMBER:
        # Direct getAccount
        detail = get_account(ACCOUNT_NUMBER)
        extract_balance(detail)
        return

    accounts = get_accounts()
    if not accounts:
        print("No accounts returned.", file=sys.stderr)
        sys.exit(1)

    first = accounts[0]
    account_number = first.get("accountNumber") or first.get("id")
    if not account_number:
        print("No accountNumber in first account:", first, file=sys.stderr)
        sys.exit(1)

    print("Account:", account_number, first.get("accountTypeName", ""), file=sys.stderr)
    detail = get_account(account_number)
    extract_balance(detail)


if __name__ == "__main__":
    main()
