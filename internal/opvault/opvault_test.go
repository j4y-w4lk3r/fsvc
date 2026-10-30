package opvault

import (
	"context"
	"strings"
	"testing"
)

func TestFetchReadsVaultFieldsAndDocuments(t *testing.T) {
	note := []byte(`{
	  "fields": [
	    {"label": "base_url", "value": "https://b2b.revolut.com/api/1.0"},
	    {"label": "client_id", "value": "client-1"},
	    {"label": "iss", "value": "localhost"},
	    {"label": "redirect_uri", "value": "http://localhost:8080/callback"},
	    {"label": "refresh_token", "value": "refresh-secret"},
	    {"label": "notesPlain", "value": "ignore me"}
	  ]
	}`)
	cert := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	key := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----\n")

	c := &Client{
		Vault: DefaultVault,
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "fsvc revolut config"):
				if !strings.Contains(joined, "--vault x-fsvc") {
					t.Fatalf("vault not passed: %s", joined)
				}
				return note, nil
			case strings.Contains(joined, "rev bus api public cert"):
				return cert, nil
			case strings.Contains(joined, "rev bus api private key"):
				return key, nil
			default:
				t.Fatalf("unexpected op args: %s", joined)
				return nil, nil
			}
		},
	}

	got, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != "client-1" || got.RefreshToken != "refresh-secret" {
		t.Fatalf("fields not copied: %+v", got.ClientID)
	}
	if !bytesEqual(got.CertPEM, cert) || !bytesEqual(got.KeyPEM, key) {
		t.Fatal("documents not copied")
	}
}

func TestFetchRejectsMissingClientID(t *testing.T) {
	c := &Client{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "fsvc revolut config") {
			return []byte(`{"fields":[{"label":"base_url","value":"https://example"}]}`), nil
		}
		t.Fatal("should have stopped at the config note")
		return nil, nil
	}}
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("expected missing client_id to fail")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
