import argparse
import json
from typing import Any

import requests

from revolut_smoke import create_session, get_cards


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="List Revolut Business cards (optionally virtual-only)."
    )
    p.add_argument(
        "--all",
        action="store_true",
        help="Show all cards (default: only virtual cards).",
    )
    p.add_argument(
        "--json",
        action="store_true",
        help="Print full JSON response (after filtering).",
    )
    return p.parse_args()


def is_virtual(card: dict[str, Any]) -> bool:
    if card.get("virtual") is True:
        return True
    t = card.get("type")
    return isinstance(t, str) and t.lower() == "virtual"


def main() -> None:
    args = parse_args()
    session: requests.Session = create_session()

    try:
        cards = get_cards(session)
    except requests.HTTPError as exc:
        print("HTTP error when fetching cards:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)
        return

    if not isinstance(cards, list):
        print("Unexpected cards response:", json.dumps(cards, indent=2))
        return

    filtered = cards if args.all else [c for c in cards if isinstance(c, dict) and is_virtual(c)]

    if args.json:
        print(json.dumps(filtered, indent=2))
        return

    if not filtered:
        print("(no cards)")
        return

    for card in filtered:
        print(
            f"- id={card.get('id')} "
            f"type={card.get('type')} "
            f"virtual={card.get('virtual')} "
            f"last4={card.get('last4')} "
            f"state={card.get('state')}"
        )


if __name__ == "__main__":
    main()

