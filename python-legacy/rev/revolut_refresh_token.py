import os
from pathlib import Path

import requests
import subprocess


BASE_URL = os.getenv("REVOLUT_BASE_URL", "https://b2b.revolut.com/api/1.0")
CERT_PATH = os.environ.get("REVOLUT_CERT_PATH")
KEY_PATH = os.environ.get("REVOLUT_KEY_PATH")
REFRESH_TOKEN = os.environ.get("REVOLUT_REFRESH_TOKEN")


def ensure_mtls_env() -> None:
    missing = []
    if not CERT_PATH:
        missing.append("REVOLUT_CERT_PATH")
    if not KEY_PATH:
        missing.append("REVOLUT_KEY_PATH")
    if missing:
        raise RuntimeError(f"Missing required env vars: {', '.join(missing)}")


def ensure_refresh_env() -> None:
    ensure_mtls_env()
    if not REFRESH_TOKEN:
        raise RuntimeError("Missing required env var: REVOLUT_REFRESH_TOKEN")


def script_dir() -> Path:
    return Path(__file__).resolve().parent


def make_client_assertion() -> None:
    """Call the helper script to regenerate client_assertion.txt."""
    helper = script_dir() / "revolut_make_client_assertion.sh"
    subprocess.run([str(helper)], check=True)


def load_client_assertion() -> str:
    # Keep the assertion file alongside the repo's Revolut cert material
    assertion_path = script_dir() / "revolut-api-cert" / "client_assertion.txt"
    if not assertion_path.exists():
        raise RuntimeError(f"client_assertion.txt not found at {assertion_path}")
    return assertion_path.read_text().strip()


def request_new_access_token(client_assertion: str) -> dict:
    resp = requests.post(
        f"{BASE_URL}/auth/token",
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        data={
            "grant_type": "refresh_token",
            "refresh_token": REFRESH_TOKEN,
            "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
            "client_assertion": client_assertion,
        },
        cert=(CERT_PATH, KEY_PATH),
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def update_env_file(access_token: str, refresh_token: str | None) -> None:
    env_path = script_dir() / "revolut.env"
    lines = env_path.read_text().splitlines(keepends=True)

    def set_or_append(var: str, value: str) -> None:
        prefix = f"export {var}="
        line = f'{prefix}"{value}"\n'
        for i, existing in enumerate(lines):
            if existing.startswith(prefix):
                lines[i] = line
                break
        else:
            lines.append(line)

    set_or_append("REVOLUT_TOKEN", access_token)
    if refresh_token:
        set_or_append("REVOLUT_REFRESH_TOKEN", refresh_token)

    env_path.write_text("".join(lines))


def main() -> None:
    ensure_refresh_env()
    print("Regenerating client assertion (JWT)...")
    make_client_assertion()
    assertion = load_client_assertion()

    print("Requesting new access token using refresh_token...")
    try:
        data = request_new_access_token(assertion)
    except requests.HTTPError as exc:
        print("HTTP error while refreshing token:", exc)
        if exc.response is not None:
            print("Status:", exc.response.status_code)
            print("Body:", exc.response.text)
        return

    access_token = data.get("access_token")
    refresh_token = data.get("refresh_token")
    expires_in = data.get("expires_in")

    if not access_token:
        print("No access_token in response:", data)
        return

    update_env_file(access_token, refresh_token)

    print("Successfully refreshed token.")
    print(f"New access_token (saved to revolut.env), expires_in={expires_in} seconds.")
    if refresh_token:
        print("New refresh_token also saved to revolut.env.")
    else:
        print("No new refresh_token returned; existing one remains in revolut.env.")


if __name__ == "__main__":
    main()

