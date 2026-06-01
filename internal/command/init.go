package command

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
)

// RunInit handles `fsvc init` — scaffold a fresh ~/.config/fsvc/config.toml
// from CLI flags. Designed to take everything the user has just collected
// from the Revolut Business → APIs page (CLIENT_ID, redirect URI, iss) in
// one command, so they don't have to hand-edit TOML.
//
// Defaults assume the suite-standard layout:
//
//	cert_path = ~/.config/fsvc/revolut-api-cert/revolut_public.crt
//	key_path  = ~/.config/fsvc/revolut-api-cert/revolut_private.key
//	base_url  = https://b2b.revolut.com/api/1.0
//	redirect  = http://localhost:8080/callback
//
// If --iss is empty, it's derived from the redirect_uri host (which is
// what Revolut expects 99% of the time anyway — it just saves a flag).
func RunInit(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	clientID := fs.String("client-id", "", "Revolut Business CLIENT_ID issued after cert upload")
	iss := fs.String("iss", "", "JWT issuer claim (defaults to host of --redirect-uri)")
	redirectURI := fs.String("redirect-uri", "http://localhost:8080/callback", "OAuth redirect URI (must match the value registered with Revolut byte-for-byte)")
	baseURL := fs.String("base-url", "https://b2b.revolut.com/api/1.0", "Revolut Business API base URL")
	certPath := fs.String("cert-path", "", "path to public cert PEM (default: alongside config under revolut-api-cert/)")
	keyPath := fs.String("key-path", "", "path to private key PEM (default: alongside config under revolut-api-cert/)")
	force := fs.Bool("f", false, "overwrite an existing config file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *clientID == "" {
		return errors.New("fsvc init: --client-id is required (you get this from Revolut Business → Settings → APIs after uploading the public cert)")
	}
	if *redirectURI == "" {
		return errors.New("fsvc init: --redirect-uri must not be empty")
	}

	// If --iss wasn't supplied, derive from redirect_uri host. Saves
	// the user one flag in the common case (matches what Revolut's
	// portal already shows for the cert: "JWT iss = localhost").
	resolvedISS := *iss
	if resolvedISS == "" {
		u, err := url.Parse(*redirectURI)
		if err != nil {
			return fmt.Errorf("fsvc init: invalid --redirect-uri %q: %w", *redirectURI, err)
		}
		resolvedISS = u.Hostname()
		if resolvedISS == "" {
			return errors.New("fsvc init: couldn't derive --iss from --redirect-uri (no host); pass --iss explicitly")
		}
	}

	path := g.ConfigPath
	if path == "" {
		path = config.DefaultPath()
	}
	if path == "" {
		return errors.New("fsvc init: cannot resolve config path (HOME unset?)")
	}

	if !*force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("fsvc init: config already exists at %s; rerun with -f to overwrite (this will discard any existing tokens!)", path)
		}
	}

	configDir := filepath.Dir(path)
	resolvedCertPath := *certPath
	if resolvedCertPath == "" {
		resolvedCertPath = filepath.Join(configDir, "revolut-api-cert", "revolut_public.crt")
	}
	resolvedKeyPath := *keyPath
	if resolvedKeyPath == "" {
		resolvedKeyPath = filepath.Join(configDir, "revolut-api-cert", "revolut_private.key")
	}

	// Heads-up (don't fail) if the cert/key aren't on disk yet —
	// `fsvc init` could legitimately be run before `fsvc cert` if the
	// user is wiring up a machine bootstrap from B2.
	for _, p := range []string{resolvedCertPath, resolvedKeyPath} {
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(os.Stderr, "[!] %s not found — run `fsvc cert` (or `fsvc bootstrap` once that lands)\n", p)
		}
	}

	cfg := config.New(path)
	cfg.Revolut.BaseURL = *baseURL
	cfg.Revolut.ClientID = *clientID
	cfg.Revolut.ISS = resolvedISS
	cfg.Revolut.RedirectURI = *redirectURI
	cfg.Revolut.CertPath = resolvedCertPath
	cfg.Revolut.KeyPath = resolvedKeyPath

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] Wrote %s (mode 0600)\n", path)
	fmt.Fprintf(os.Stderr, "    base_url      = %s\n", cfg.Revolut.BaseURL)
	fmt.Fprintf(os.Stderr, "    client_id     = %s\n", redactMiddle(cfg.Revolut.ClientID, 8, 4))
	fmt.Fprintf(os.Stderr, "    iss           = %s\n", cfg.Revolut.ISS)
	fmt.Fprintf(os.Stderr, "    redirect_uri  = %s\n", cfg.Revolut.RedirectURI)
	fmt.Fprintf(os.Stderr, "    cert_path     = %s\n", cfg.Revolut.CertPath)
	fmt.Fprintf(os.Stderr, "    key_path      = %s\n", cfg.Revolut.KeyPath)
	fmt.Fprintf(os.Stderr, "\nNext step:\n  fsvc revolut authorize    # opens browser, captures code, exchanges for tokens\n")
	return nil
}

// redactMiddle keeps the first `head` and last `tail` runes of s,
// replacing the middle with "…". Used for "look it's the right
// CLIENT_ID" confirmation prints without exposing the whole secret
// to whatever shell-history or screenshot tooling the user runs.
//
// Lives in init.go because RunInit is the first place we surface a
// secret-ish value back to the user; promote to command.go if a
// second caller appears.
func redactMiddle(s string, head, tail int) string {
	rs := []rune(s)
	if len(rs) <= head+tail {
		return s
	}
	return string(rs[:head]) + "…" + strings.TrimSpace(string(rs[len(rs)-tail:]))
}
