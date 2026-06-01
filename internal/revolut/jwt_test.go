package revolut

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestMakeClientAssertion_RoundTrip confirms the JWT we emit is
// well-formed (parseable) and verifiable with the matching public
// key. Doesn't talk to Revolut — that smoke test happens later, with
// real credentials.
//
// What it does check:
//   - the token is RS256 (Revolut requires this exact alg)
//   - the iss / sub / aud / exp claims are correct
//   - the signature verifies against the public half of the key we
//     wrote to disk
//
// If any of these break we want to know in `go test`, not from a 401
// from /auth/token.
func TestMakeClientAssertion_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test.key")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	const (
		iss      = "fsvc.local"
		clientID = "test-client-id"
	)
	signed, err := MakeClientAssertion(iss, clientID, keyPath, 5*time.Minute)
	if err != nil {
		t.Fatalf("MakeClientAssertion: %v", err)
	}
	if signed == "" {
		t.Fatal("got empty signed token")
	}

	parsed, err := jwt.Parse(signed, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return &priv.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("Parse: token reported as invalid")
	}
	if got := parsed.Method.Alg(); got != "RS256" {
		t.Fatalf("alg = %q; want RS256", got)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("Claims has unexpected type %T", parsed.Claims)
	}
	if got, _ := claims["iss"].(string); got != iss {
		t.Errorf("iss = %q; want %q", got, iss)
	}
	if got, _ := claims["sub"].(string); got != clientID {
		t.Errorf("sub = %q; want %q", got, clientID)
	}
	if got, _ := claims["aud"].(string); got != "https://revolut.com" {
		t.Errorf("aud = %q; want %q", got, "https://revolut.com")
	}
	expF, _ := claims["exp"].(float64)
	if expF == 0 {
		t.Fatal("exp claim missing")
	}
	exp := time.Unix(int64(expF), 0)
	if delta := time.Until(exp); delta < 4*time.Minute || delta > 6*time.Minute {
		t.Errorf("exp horizon = %v; want ~5m", delta)
	}
}

// TestMakeClientAssertion_BadKey confirms we surface a useful error
// when the key path is missing, empty, or not parseable PEM.
func TestMakeClientAssertion_BadKey(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		setup   func() string
		wantErr string
	}{
		{
			name:    "missing file",
			setup:   func() string { return filepath.Join(dir, "nope.key") },
			wantErr: "read key",
		},
		{
			name: "not pem",
			setup: func() string {
				p := filepath.Join(dir, "not-pem.key")
				_ = os.WriteFile(p, []byte("hello world\n"), 0o600)
				return p
			},
			wantErr: "not PEM-encoded",
		},
		{
			name: "wrong key type",
			setup: func() string {
				p := filepath.Join(dir, "junk.key")
				_ = os.WriteFile(p, []byte("-----BEGIN GARBAGE-----\nAAAA\n-----END GARBAGE-----\n"), 0o600)
				return p
			},
			wantErr: "not a parseable RSA private key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MakeClientAssertion("iss", "cid", tc.setup(), time.Minute)
			if err == nil {
				t.Fatal("want error; got nil")
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q; want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// contains is a tiny strings.Contains shim so this test file doesn't
// need to import strings just for one call.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
