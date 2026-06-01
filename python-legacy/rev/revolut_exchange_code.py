import os
from pathlib import Path
import sys

import requests
import subprocess

from revolut_refresh_token import (
  ensure_mtls_env,
  load_client_assertion,
  make_client_assertion,
  script_dir,
  update_env_file,
)


def ensure_auth_code(code: str | None) -> str:
  """
  Ensure we have an authorization code from the Revolut redirect.

  Order of precedence:
  - First CLI argument
  - REVOLUT_AUTH_CODE environment variable
  """
  value = code or os.environ.get("REVOLUT_AUTH_CODE")
  if not value:
    raise RuntimeError(
      "No authorization code provided. "
      "Pass it as the first argument to this script or set REVOLUT_AUTH_CODE."
    )
  return value


def request_tokens_with_code(auth_code: str, client_assertion: str) -> dict:
  from revolut_refresh_token import BASE_URL, CERT_PATH, KEY_PATH

  resp = requests.post(
    f"{BASE_URL}/auth/token",
    headers={"Content-Type": "application/x-www-form-urlencoded"},
    data={
      "grant_type": "authorization_code",
      "code": auth_code,
      "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
      "client_assertion": client_assertion,
    },
    cert=(CERT_PATH, KEY_PATH),
    timeout=30,
  )
  resp.raise_for_status()
  return resp.json()


def main() -> None:
  # Only mTLS is required for authorization_code exchange.
  ensure_mtls_env()

  # Get the authorization code from CLI or env
  auth_code = ensure_auth_code(sys.argv[1] if len(sys.argv) > 1 else None)

  print("Regenerating client assertion (JWT) for authorization_code grant...")
  make_client_assertion()
  assertion = load_client_assertion()

  print("Exchanging authorization code for access/refresh tokens...")
  try:
    data = request_tokens_with_code(auth_code, assertion)
  except requests.HTTPError as exc:
    print("HTTP error while exchanging authorization code:", exc)
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

  # Persist both access and refresh tokens into revolut.env
  update_env_file(access_token, refresh_token)

  print("Successfully obtained tokens from authorization code.")
  print(f"New access_token (saved to revolut.env), expires_in={expires_in} seconds.")
  if refresh_token:
    print("New refresh_token also saved to revolut.env.")
  else:
    print("No refresh_token returned; you may need to re-authorise again in the future.")


if __name__ == "__main__":
  main()

