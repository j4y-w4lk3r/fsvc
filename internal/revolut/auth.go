package revolut

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenResponse is what /auth/token returns for both the
// authorization_code and refresh_token grants. Revolut sometimes
// rotates the refresh_token (issuing a new one alongside the new
// access_token); when that happens we persist BOTH back to disk.
//
// `expires_in` is seconds from now; we convert to an absolute time
// in cfg.Revolut.AccessExpiresAt so HasFreshAccessToken can be a
// pure comparison instead of "did I refresh recently?".
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
}

// ExchangeAuthCode performs the one-off authorization_code grant: the
// user has just clicked "Authorize" in Revolut Business → APIs and
// been redirected to <redirect_uri>?code=<CODE>. We trade that code
// (which is single-use, valid ~2 minutes) for a long-lived
// refresh_token plus a 40-min access_token.
//
// On success the new tokens are written to cfg AND saved to disk via
// cfg.Save(), so subsequent `fsvc revolut <cmd>` calls find them
// already in place.
func (c *Client) ExchangeAuthCode(ctx context.Context, authCode string) (*TokenResponse, error) {
	if authCode == "" {
		return nil, fmt.Errorf("revolut: auth code is empty")
	}

	assertion, err := MakeClientAssertion(
		c.cfg.Revolut.ISS,
		c.cfg.Revolut.ClientID,
		c.cfg.Revolut.KeyPath,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)

	tok, err := c.postTokenForm(ctx, form)
	if err != nil {
		return nil, err
	}

	// Persist both tokens and the absolute expiry — this is the moment
	// the user's machine first has a usable Revolut session, so a crash
	// here would mean re-doing the OAuth consent flow. cfg.Save() uses
	// the atomic-rename pattern so partial writes are impossible.
	c.cfg.Revolut.AccessToken = tok.AccessToken
	c.cfg.Revolut.AccessExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if tok.RefreshToken != "" {
		c.cfg.Revolut.RefreshToken = tok.RefreshToken
	}
	if err := c.cfg.Save(); err != nil {
		// Don't drop the token — return it so a power user can pipe it
		// somewhere even if disk write failed. Most callers will just
		// surface the error.
		return tok, fmt.Errorf("revolut: tokens obtained but Save failed: %w", err)
	}
	return tok, nil
}

// RefreshAccessToken trades the stored refresh_token for a fresh
// access_token. The standard "I have access_token expired, give me a
// new one" flow used by every `fsvc revolut <cmd>` call when
// HasFreshAccessToken returns false.
//
// If Revolut rotates the refresh_token in the response (which they
// occasionally do for security), the new value is persisted to
// config.toml. Otherwise the old refresh_token stays put.
func (c *Client) RefreshAccessToken(ctx context.Context) (*TokenResponse, error) {
	if c.cfg.Revolut.RefreshToken == "" {
		return nil, fmt.Errorf("revolut: no refresh_token set; run `fsvc revolut exchange-code <CODE>` after the OAuth consent flow")
	}

	assertion, err := MakeClientAssertion(
		c.cfg.Revolut.ISS,
		c.cfg.Revolut.ClientID,
		c.cfg.Revolut.KeyPath,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.cfg.Revolut.RefreshToken)
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)

	tok, err := c.postTokenForm(ctx, form)
	if err != nil {
		return nil, err
	}

	c.cfg.Revolut.AccessToken = tok.AccessToken
	c.cfg.Revolut.AccessExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if tok.RefreshToken != "" {
		c.cfg.Revolut.RefreshToken = tok.RefreshToken
	}
	if err := c.cfg.Save(); err != nil {
		return tok, fmt.Errorf("revolut: refresh succeeded but Save failed: %w", err)
	}
	return tok, nil
}

// EnsureAccessToken refreshes the access_token if (and only if) the
// cached one is stale. Idempotent — safe to call before every API
// request, which is exactly how the `accounts`/`cards`/`txs`
// subcommands use it.
func (c *Client) EnsureAccessToken(ctx context.Context) error {
	if c.HasFreshAccessToken() {
		return nil
	}
	_, err := c.RefreshAccessToken(ctx)
	return err
}

// postTokenForm POSTs the form-encoded payload to /auth/token and
// decodes the JSON response. Shared by both ExchangeAuthCode and
// RefreshAccessToken because the wire format is identical — only the
// `grant_type` and the secret-bearing field differ.
func (c *Client) postTokenForm(ctx context.Context, form url.Values) (*TokenResponse, error) {
	u := strings.TrimRight(c.cfg.Revolut.BaseURL, "/") + "/auth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("revolut: build POST /auth/token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("revolut: POST /auth/token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("revolut: POST /auth/token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("revolut: decode /auth/token response: %w (body: %s)", err, strings.TrimSpace(string(body)))
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("revolut: /auth/token returned no access_token (body: %s)", strings.TrimSpace(string(body)))
	}
	return &tok, nil
}
