import os
import json
from datetime import datetime, timedelta, timezone
from typing import Any, Iterable

import requests

from revolut_smoke import BASE_URL, create_session, get_accounts


def iso_range() -> tuple[str, str]:
    """
    Determine the time window for transactions.

    - REVOLUT_FROM: optional ISO8601 start (e.g. 2024-01-01T00:00:00Z)
    - REVOLUT_TO:   optional ISO8601 end (defaults to now)
    - REVOLUT_DAYS_BACK: if REVOLUT_FROM not set, look back this many days (default: 365)
    """
    now = datetime.now(timezone.utc).replace(microsecond=0)

    to_iso = os.getenv("REVOLUT_TO")
    if not to_iso:
        to_iso = now.isoformat().replace("+00:00", "Z")

    from_iso = os.getenv("REVOLUT_FROM")
    if not from_iso:
        days_back = int(os.getenv("REVOLUT_DAYS_BACK", "365"))
        from_dt = now - timedelta(days=days_back)
        from_iso = from_dt.isoformat().replace("+00:00", "Z")

    return from_iso, to_iso


def get_transactions(
    session: requests.Session,
    account_id: str,
    since: str,
    until: str,
    count: int = 1000,
) -> list[dict[str, Any]]:
    """Fetch transactions for a single account using Revolut Business API."""
    params = {
        "account": account_id,
        "from": since,
        "to": until,
        "count": str(count),
    }
    resp = session.get(f"{BASE_URL}/transactions", params=params, timeout=30)
    resp.raise_for_status()
    data = resp.json()
    if isinstance(data, list):
        return data
    # Fallback: some APIs wrap in an object
    if isinstance(data, dict) and "transactions" in data and isinstance(
        data["transactions"], list
    ):
        return data["transactions"]
    return [data]


def format_amount(tx: dict[str, Any]) -> tuple[Any, Any]:
    """
    Try to extract amount/currency in a robust way.
    Revolut Business transactions often provide legs with amounts.
    """
    if "amount" in tx or "currency" in tx:
        return tx.get("amount"), tx.get("currency")

    legs = tx.get("legs")
    if isinstance(legs, Iterable):
        legs_list = list(legs)
        if legs_list:
            leg0 = legs_list[0]
            if isinstance(leg0, dict):
                return leg0.get("amount"), leg0.get("currency")

    return None, None


def print_transactions_for_account(
    account: dict[str, Any],
    txs: list[dict[str, Any]],
) -> None:
    acc_id = account.get("id")
    acc_curr = account.get("currency")
    acc_name = account.get("name") or account.get("type")

    print()
    print(f"=== Transactions for account {acc_id} ({acc_curr}) {acc_name!r} ===")

    if not txs:
        print("  (no transactions in selected range)")
        return

    for tx in txs:
        amount, curr = format_amount(tx)
        desc = tx.get("description") or tx.get("reference") or ""

        print(
            f"- id={tx.get('id')} "
            f"created_at={tx.get('created_at')} "
            f"type={tx.get('type')} "
            f"state={tx.get('state')} "
            f"amount={amount} {curr} "
            f"description={desc!r}"
        )


def main() -> None:
    session = create_session()

    since, until = iso_range()
    print(f"Using time window: from={since} to={until}")

    # Optional: limit to a specific account via env
    only_account_id = os.getenv("REVOLUT_ACCOUNT_ID")

    try:
        accounts = get_accounts(session)
    except requests.HTTPError as exc:
        print("HTTP error while fetching accounts:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)
        return

    if not isinstance(accounts, list):
        print("Unexpected accounts response:", json.dumps(accounts, indent=2))
        return

    for acc in accounts:
        acc_id = acc.get("id")
        if not acc_id:
            continue

        if only_account_id and acc_id != only_account_id:
            continue

        try:
            txs = get_transactions(session, acc_id, since, until)
        except requests.HTTPError as exc:
            print(f"HTTP error while fetching transactions for account {acc_id}:", exc)
            if exc.response is not None:
                print("Status:", exc.response.status_code)
                print("Body:", exc.response.text)
            continue

        print_transactions_for_account(acc, txs)


if __name__ == "__main__":
    main()

