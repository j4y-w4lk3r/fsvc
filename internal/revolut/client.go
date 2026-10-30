package revolut

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
)

// Client is the mTLS-aware Revolut Business API client. Construction
// loads the certificate + private key from the paths in cfg.Revolut
// once, so subsequent requests don't re-read disk.
//
// The same client carries all three auth layers Revolut needs:
//   - mTLS (configured at TLS handshake via the loaded keypair)
//   - Bearer access_token (per-request, set in Do)
//   - JWT client_assertion (only on /auth/token, set per-call by auth.go)
type Client struct {
	cfg   *config.Config
	httpc *http.Client
}

// New constructs a Client from the resolved Config. Returns an error
// if the cert/key pair fails to load or pair up cryptographically —
// fail fast here so the user sees "bad cert" once at startup, not
// once per API call.
func New(cfg *config.Config) (*Client, error) {
	r := &cfg.Revolut
	if r.CertPath == "" || r.KeyPath == "" {
		return nil, fmt.Errorf("revolut: cert_path and key_path must be set (run `fsvc cert` to generate)")
	}

	cert, err := tls.LoadX509KeyPair(r.CertPath, r.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("revolut: load mTLS keypair (%s + %s): %w", r.CertPath, r.KeyPath, err)
	}

	tr := &http.Transport{
		// A custom Transport does not use the environment proxy unless
		// this is set. fsvc sets HTTPS_PROXY so only these calls leave
		// through the VPS address Revolut has whitelisted.
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		// Sane defaults; Revolut is HTTP/1.1-friendly and doesn't need
		// the latest fanciness here.
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		cfg: cfg,
		httpc: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}, nil
}

// HasFreshAccessToken reports whether the cached access_token is
// non-empty AND has at least 60s of life left. Callers (e.g. the
// `revolut accounts` subcommand) use it to decide whether to refresh
// before making an API call.
//
// 60s slack rather than "is it expired right now?" — token refresh +
// API call together can take a few seconds, and Revolut clocks aren't
// guaranteed to match local clocks to the millisecond.
func (c *Client) HasFreshAccessToken() bool {
	r := &c.cfg.Revolut
	if r.AccessToken == "" {
		return false
	}
	if r.AccessExpiresAt.IsZero() {
		// We have a token but no recorded expiry — be conservative and
		// say "stale" so the caller refreshes. Saves debugging "401
		// Unauthorized" later.
		return false
	}
	return time.Until(r.AccessExpiresAt) > 60*time.Second
}

// authedGet does GET <path> with the mTLS client + Bearer token,
// JSON-decoding the response body into `out`. Path is appended to
// cfg.Revolut.BaseURL; query string can be supplied via params (use
// nil for none).
//
// Errors render the response body when present so users see Revolut's
// own error JSON (which is usually informative: "InvalidScope",
// "TokenExpired", etc.) rather than just "401 Unauthorized".
//
// We also pattern-match a couple of well-known Revolut error codes
// to surface human-friendly hints — the IP-whitelisting one in
// particular is the difference between "obvious one-step fix" and
// "spend an hour reading docs".
func (c *Client) authedGet(ctx context.Context, path string, params url.Values, out any) error {
	if c.cfg.Revolut.AccessToken == "" {
		return fmt.Errorf("revolut: no access_token set (run `fsvc revolut authorize`)")
	}

	u := strings.TrimRight(c.cfg.Revolut.BaseURL, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("revolut: build GET %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Revolut.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("revolut: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		bodyStr := strings.TrimSpace(string(body))
		// Surface a clearer message for known Revolut error codes.
		// We do this BEFORE the generic error so the hint isn't
		// buried under the raw JSON. Match on substrings rather than
		// full JSON-decode so a body shape change doesn't break the
		// hint.
		if hint := revolutErrorHint(resp.StatusCode, bodyStr); hint != "" {
			return fmt.Errorf("revolut: GET %s: %s\n\n%s\n\nRaw response: %s", path, resp.Status, hint, bodyStr)
		}
		return fmt.Errorf("revolut: GET %s: %s: %s", path, resp.Status, bodyStr)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("revolut: decode %s response: %w", path, err)
		}
	}
	return nil
}

// revolutErrorHint maps a (status, body) pair to a human-friendly
// follow-up. Empty string means "no special-case advice; let the
// caller render the raw error". Currently handles:
//
//	9002  / 33004  — IP whitelisting required / undefined.
//	                 Recovery: add public IP to the cert in Revolut
//	                 Business → Settings → APIs → <cert> → Production
//	                 IP whitelist.
//
// More codes will be added as we hit them in the wild.
func revolutErrorHint(status int, body string) string {
	if status != http.StatusForbidden {
		return ""
	}
	if strings.Contains(body, `"code":9002`) ||
		strings.Contains(body, `"code":33004`) ||
		strings.Contains(body, "IP whitelisting") ||
		strings.Contains(body, "IP whitelist") {
		return `Hint: Revolut requires your public IP to be whitelisted for this cert.

  1. Find your current public IP:
       curl -4 -s https://api.ipify.org
  2. In the Revolut Business portal:
       https://business.revolut.com/settings/apis?tab=business-api
     → click your cert → "Production IP whitelist" → Add → paste IP → Save
  3. Retry the same fsvc command (no need to re-authorize).`
	}
	return ""
}
