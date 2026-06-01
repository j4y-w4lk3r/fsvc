package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadAndSave_RoundTrip verifies that what we Save we can Load
// back identically. The bug-this-tests-against is "we wrote
// access_expires_at as a TOML datetime that toml-encoder mangles" —
// run it after every BurntSushi/toml bump.
func TestLoadAndSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := &Config{
		path: path,
		Revolut: RevolutConfig{
			BaseURL:         "https://b2b.revolut.com/api/1.0",
			ClientID:        "fake-client",
			ISS:             "fsvc.local",
			CertPath:        "/tmp/test.crt",
			KeyPath:         "/tmp/test.key",
			AccessToken:     "AT-secret",
			AccessExpiresAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			RefreshToken:    "RT-secret",
		},
	}

	if err := original.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	// 0600 — the file holds tokens, anything else is a security bug.
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %v; want 0600", mode)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := loaded.Revolut.ClientID, original.Revolut.ClientID; got != want {
		t.Errorf("ClientID round-trip: got %q want %q", got, want)
	}
	if got, want := loaded.Revolut.RefreshToken, original.Revolut.RefreshToken; got != want {
		t.Errorf("RefreshToken round-trip: got %q want %q", got, want)
	}
	if !loaded.Revolut.AccessExpiresAt.Equal(original.Revolut.AccessExpiresAt) {
		t.Errorf("AccessExpiresAt round-trip: got %v want %v",
			loaded.Revolut.AccessExpiresAt, original.Revolut.AccessExpiresAt)
	}
}

// TestLoad_NoConfig confirms the "first-run hint" path: we want
// callers to be able to errors.Is(err, ErrNoConfig) and render their
// own welcome message. Failing this test would hide the friendly
// onboarding under an opaque os.PathError.
func TestLoad_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(filepath.Join(dir, "does-not-exist.toml"))
	if err == nil {
		t.Fatal("want error; got nil")
	}
	if !isErrNoConfig(err) {
		t.Errorf("err = %v; want errors.Is(err, ErrNoConfig) == true", err)
	}
}

// TestExpandHome_Tilde checks that "~/foo" resolves relative to
// $HOME (the form fsvc.config.toml uses by default). The PKI lookups
// in tls.LoadX509KeyPair don't expand "~" themselves, so config has
// to do it.
func TestExpandHome_Tilde(t *testing.T) {
	t.Setenv("HOME", "/var/empty/test-home")
	if got, want := expandHome("~/foo/bar"), "/var/empty/test-home/foo/bar"; got != want {
		t.Errorf("expandHome(~/foo/bar) = %q; want %q", got, want)
	}
	if got, want := expandHome("~"), "/var/empty/test-home"; got != want {
		t.Errorf("expandHome(~) = %q; want %q", got, want)
	}
	if got, want := expandHome("/abs/path"), "/abs/path"; got != want {
		t.Errorf("expandHome(/abs/path) = %q; want %q", got, want)
	}
	if got, want := expandHome(""), ""; got != want {
		t.Errorf("expandHome(\"\") = %q; want empty", got)
	}
}

// TestApplyEnvOverrides confirms env vars override file values for
// the cred fields specifically. This is the path the legacy Python
// REVOLUT_TOKEN env-var users will continue to rely on.
func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("REVOLUT_CLIENT_ID", "from-env")
	t.Setenv("REVOLUT_TOKEN", "AT-from-env")
	t.Setenv("REVOLUT_REFRESH_TOKEN", "RT-from-env")

	cfg := &Config{
		Revolut: RevolutConfig{
			ClientID:     "from-file",
			AccessToken:  "AT-from-file",
			RefreshToken: "RT-from-file",
		},
	}
	cfg.applyEnvOverrides()

	if got, want := cfg.Revolut.ClientID, "from-env"; got != want {
		t.Errorf("ClientID = %q; want %q", got, want)
	}
	if got, want := cfg.Revolut.AccessToken, "AT-from-env"; got != want {
		t.Errorf("AccessToken = %q; want %q", got, want)
	}
	if got, want := cfg.Revolut.RefreshToken, "RT-from-env"; got != want {
		t.Errorf("RefreshToken = %q; want %q", got, want)
	}
}

// isErrNoConfig is the test-side errors.Is helper. Kept here rather
// than in the package proper because callers should use errors.Is
// directly.
func isErrNoConfig(err error) bool {
	type unwrapper interface{ Unwrap() error }
	for {
		if err == nil {
			return false
		}
		if err == ErrNoConfig {
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
}
