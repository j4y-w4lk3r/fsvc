# ING Bank Śląski – Account balance via AIS API

How to get the **amount of money (balance)** on your ING account using the [ING Developer Portal](https://devportal.ing.pl/#/services/2/100) AIS (Account Information Service) API, PolishAPI 2.1.

## How you get the balance

1. **List accounts** → `POST /v2_1.1/accounts/v2_1.1/getAccounts`  
   Returns a list of your payment accounts (each has `accountNumber`).

2. **Get one account (with balance)** → `POST /v2_1.1/accounts/v2_1.1/getAccount`  
   Send the chosen `accountNumber` in the body. The response contains **detailed account info, including balance** (e.g. available balance, booked balance, currency).

So: **the balance comes from the `getAccount` response**, not from `getAccounts`. Use `getAccounts` only to get `accountNumber`(s), then call `getAccount` for the account you care about to get the amount of money.

## Where do I get the access token? (It’s not in the ING app)

**It’s not like an API key** you can copy from the ING mobile or web banking app. There is no “API key” or “Developer” section in your normal ING account.

The access token comes from **ING’s developer / PSD2 flow**:

1. **Sandbox (testing)**  
   On the **Polish ING Developer Portal** ([devportal.ing.pl](https://devportal.ing.pl)) you typically:
   - Register (or use sandbox registration),
   - Use the portal’s **auth/consent** or **“Scenarios”** (Scenarios Testowe) to simulate a user granting access,
   - The portal or the auth endpoint then gives you a **Bearer access token** (or an authorization code you exchange for a token).  
   So you get the token **from the dev portal / sandbox flow**, not from the ING banking app.

2. **Production (real account)**  
   For your real ING account:
   - Your app (as a registered TPP) redirects the user to ING’s login/consent page,
   - The user logs in with their normal ING credentials and agrees to share account info,
   - ING redirects back with a **code** (or similar); your backend exchanges it for an **access token**.  
   So the token is **issued by ING after the user consents**, not shown as a static key in the app.

**Summary:** There is no place “inside my ING app” to copy a key. You get the token from the **ING Developer Portal** (sandbox) or from the **OAuth/consent flow** (production) after the user approves access.

### Sandbox only: getting a test token (step-by-step)

For **testing**, use the sandbox so you don't touch a real account:

1. Open **[devportal.ing.pl](https://devportal.ing.pl)** and go to the AIS service (e.g. [services/2/100](https://devportal.ing.pl/#/services/2/100)).
2. In the left menu, check **"Opis Implementacji"** (implementation description) and **"Scenarios Testowe"** (test scenarios). The docs there describe the **register** and **auth** endpoints (e.g. `auth/v2_1.1/register`, then the consent/authorization step).
3. **Register** your TPP in the sandbox (if required) and call the **auth** endpoint to simulate a user granting access. The response (or the token endpoint you call with the auth code) will contain the **Bearer access token** for sandbox.
4. Use that token as `ING_ACCESS_TOKEN` when running `get_balance.py`. Base URL is already set to sandbox by default.

Sandbox may also provide test credentials or a "get token" helper in the portal—look for "Scenarios Testowe" or "Środowisko testowe".

## What you need before calling the API

- **Access token**  
  From the consent/authorization flow above (sandbox via dev portal, production via your app’s redirect + token exchange). All AIS calls use this as `Authorization: Bearer <token>`.

- **Request signing (X-JWS-SIGNATURE)**  
  ING requires a **detached JWS** signature of the request body, produced with your TPP (Third Party Provider) certificate/private key. So you need to be registered as a TPP and have the key used for signing.

- **Correct base URL**
  - Production: `https://api.ing.pl/gateway`
  - Sandbox: `https://devportal.ing.pl/sandbox/api`  
  Example full URL for getAccount:  
  `https://api.ing.pl/gateway/api/v2_1.1/accounts/v2_1.1/getAccount`

## Quick reference – getAccount (where the balance is)

- **URL:** `https://api.ing.pl/gateway/api/v2_1.1/accounts/v2_1.1/getAccount` (prod) or same path on sandbox base.
- **Method:** POST  
- **Headers:**
  - `Authorization: Bearer <your_access_token>`
  - `Accept-Encoding: gzip, deflate`
  - `Accept-Language: pl` (or your preference)
  - `Accept-Charset: utf-8`
  - `X-JWS-SIGNATURE: <detached JWS of request body>`
  - `X-REQUEST-ID: <UUID, same as requestHeader.requestId>`
  - `Content-Type: application/json`
- **Body (example):**
  - `requestHeader`: `requestId`, `userAgent`, `ipAddress`, `sendDate`, `tppId`, `token` (same as Bearer), `isDirectPsu`, etc.
  - `accountNumber`: the account ID from `getAccounts`.

The response body is signed (bank sends `X-JWS-SIGNATURE`); after verifying it, you parse the JSON. The account object in the response should contain balance information (e.g. a `balance` or `balances` array with `amount` and `currency`). The script in this repo tries to extract and print that.

## Setup (venv + deps)

Create and use a virtual environment so `pip` is available:

```bash
cd /Users/wgm0/px/ing
python3 -m venv .venv
source .venv/bin/activate   # on Windows: .venv\Scripts\activate
pip install -r requirements.txt
```

The project already has a `.venv`; activate it and install deps as above.

## Running the script

- With the venv activated, set env:
  - `ING_ACCESS_TOKEN` – your Bearer token from the consent flow.
  - Optionally `ING_BASE_URL` (defaults to sandbox).
- If you have a way to produce the JWS signature, set `ING_JWS_SIGNATURE` for the request you’re sending; otherwise the script shows what body it would send so you can sign it externally (e.g. with your TPP key) and then call the API with that signature.

Run (with venv activated):

```bash
python get_balance.py
```

The script will:

1. Call **getAccounts** (with the body/signature you provide or placeholders) to get `accountNumber`(s).
2. Call **getAccount** for the first account (or the one you set) to get details.
3. Parse and print **balance** from the getAccount response (e.g. amount and currency).

If you don’t have TPP signing set up yet, the script still shows the exact request bodies and response parsing logic so you can see how the “amount of money” is obtained from the API.

## What about Revolut?

**Revolut can be easier** for this use case: their [developer docs](https://developer.revolut.com/docs/open-banking/get-accounts-account-id-balances) are clear, and the Open Banking API uses a straightforward flow (consent → auth code → access token → `GET /accounts/{accountId}/balances`). No request-body signing like ING’s JWS.

- **ING Poland:** PolishAPI 2.1, JWS signing required, good if your account or target users are with ING Śląski.
- **Revolut:** Open Banking (UK-style), well-documented REST API; good if you use Revolut or want a single integration that works across Revolut users.

If you want to **use Revolut instead** (or in addition), we can add a small script that gets balance via Revolut’s Open Banking API using the same pattern: token in env, then call accounts + balances.

## Links

- [ING Developer Portal – AIS v2_1.1](https://devportal.ing.pl/#/services/2/100)
- [Production API base](https://api.ing.pl/gateway)
- [Service status / planned maintenance](https://www.ing.pl/psd2supportinfo)
- [Revolut – Retrieve an account balance](https://developer.revolut.com/docs/open-banking/get-accounts-account-id-balances)
