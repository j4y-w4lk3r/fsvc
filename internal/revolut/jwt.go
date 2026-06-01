// Package revolut implements a minimal client for the Revolut Business
// API: mTLS-authenticated calls signed with a per-request JWT
// `client_assertion` (RS256) for OAuth, and a Bearer-token-protected
// REST surface for accounts/cards/transactions afterward.
//
// Why hand-rolled instead of an SDK: Revolut doesn't ship a Go SDK, the
// surface area we use is tiny (token endpoint + 3 GETs), and the auth
// shape is unusual enough (mTLS + JWT client_assertion + Bearer all at
// once) that wrapping it in dependencies adds more moving parts than
// it saves.
//
// Files:
//
//	jwt.go     — RS256 client_assertion JWT signing
//	client.go  — mTLS http.Client + helpers shared by all endpoints
//	auth.go    — POST /auth/token (refresh_token + authorization_code grants)
//	api.go     — GET /accounts, GET /cards, GET /transactions
package revolut

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MakeClientAssertion builds the RS256-signed JWT that Revolut's
// /auth/token endpoint demands as `client_assertion` for both
// `authorization_code` and `refresh_token` grants.
//
// Claims (mirroring revolut_make_client_assertion.sh):
//
//	iss = caller-supplied (matches the redirect_uri host registered
//	      with Revolut)
//	sub = client_id
//	aud = "https://revolut.com" (constant per Revolut's docs, NOT
//	      the API base URL)
//	exp = now + lifetime (default 10 min in the legacy bash script;
//	      we let callers pick to keep the function pure)
//
// The header is the standard {"alg":"RS256","typ":"JWT"}.
//
// Output is a compact base64url-encoded JWT with no padding (the form
// `header.payload.signature`) ready to drop into the form-encoded
// POST body.
func MakeClientAssertion(iss, clientID, keyPath string, lifetime time.Duration) (string, error) {
	if iss == "" {
		return "", errors.New("revolut: iss is empty")
	}
	if clientID == "" {
		return "", errors.New("revolut: client_id is empty")
	}
	if keyPath == "" {
		return "", errors.New("revolut: key_path is empty")
	}
	if lifetime <= 0 {
		// Match the legacy default: 10 minutes is plenty for the round
		// trip + any pinentry pause; longer = larger replay window.
		lifetime = 10 * time.Minute
	}

	priv, err := loadRSAPrivateKey(keyPath)
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": iss,
		"sub": clientID,
		"aud": "https://revolut.com",
		"exp": now.Add(lifetime).Unix(),
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("revolut: sign jwt: %w", err)
	}
	return signed, nil
}

// loadRSAPrivateKey reads a PEM-encoded RSA private key from disk and
// returns the parsed key. Accepts both PKCS#1 (`BEGIN RSA PRIVATE KEY`)
// and PKCS#8 (`BEGIN PRIVATE KEY`) — `fsvc cert` writes PKCS#1 by
// default, but a user-supplied key may be either.
func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("revolut: read key %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("revolut: %s is not PEM-encoded", path)
	}

	// Try PKCS#1 first (what `openssl genrsa` writes, what
	// revolut_cert_setup.sh produced, what `fsvc cert` will write).
	if priv, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return priv, nil
	}
	// Fall back to PKCS#8 (the modern default for many tools).
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("revolut: %s is a PKCS#8 key but not RSA", path)
	}
	return nil, fmt.Errorf("revolut: %s is not a parseable RSA private key (need PKCS#1 or PKCS#8 PEM)", path)
}
