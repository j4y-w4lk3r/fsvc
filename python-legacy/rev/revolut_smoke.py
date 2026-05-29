import os
import requests


BASE_URL = os.getenv("REVOLUT_BASE_URL", "https://b2b.revolut.com/api/1.0")
CERT_PATH = os.environ.get("REVOLUT_CERT_PATH")
KEY_PATH = os.environ.get("REVOLUT_KEY_PATH")
TOKEN = os.environ.get("REVOLUT_TOKEN")


def create_session() -> requests.Session:
    if not CERT_PATH or not KEY_PATH:
        raise RuntimeError("REVOLUT_CERT_PATH and REVOLUT_KEY_PATH must be set")
    if not TOKEN:
        raise RuntimeError("REVOLUT_TOKEN must be set")

    session = requests.Session()
    session.cert = (CERT_PATH, KEY_PATH)
    session.headers.update(
        {
            "Authorization": f"Bearer {TOKEN}",
            "Accept": "application/json",
        }
    )
    return session


def get_accounts(session: requests.Session):
    resp = session.get(f"{BASE_URL}/accounts", timeout=30)
    resp.raise_for_status()
    return resp.json()


def get_cards(session: requests.Session):
    resp = session.get(f"{BASE_URL}/cards", timeout=30)
    resp.raise_for_status()
    return resp.json()


def main() -> None:
    session = create_session()
    try:
        accounts = get_accounts(session)
    except requests.HTTPError as exc:
        print("HTTP error:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)
        return

    print("Accounts response:")
    for acc in accounts:
        print(
            f"- id={acc.get('id')} "
            f"currency={acc.get('currency')} "
            f"balance={acc.get('balance')}"
        )

    print()

    try:
        cards = get_cards(session)
    except requests.HTTPError as exc:
        print("HTTP error when fetching cards:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)
        return

    print("Cards response:")
    for card in cards:
        print(
            f"- id={card.get('id')} "
            f"type={card.get('type')} "
            f"virtual={card.get('virtual')} "
            f"last4={card.get('last4')} "
            f"state={card.get('state')}"
        )


if __name__ == "__main__":
    main()

