package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
	"github.com/j4y-w4lk3r/fsvc/internal/opvault"
)

// RunPull copies the Revolut certificate, private key, and config note
// out of 1Password onto this machine. fsvc cert is a different command:
// it generates a new keypair and would no longer match the certificate
// already uploaded to Revolut.
func RunPull(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc pull", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	vault := fs.String("vault", opvault.DefaultVault, "1Password vault that holds the Revolut items")
	force := fs.Bool("f", false, "overwrite an existing config, cert, and key")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfgPath := g.ConfigPath
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	if cfgPath == "" {
		return errors.New("fsvc pull: cannot resolve config path (HOME unset?)")
	}
	certDir := filepath.Join(filepath.Dir(cfgPath), "revolut-api-cert")
	certPath := filepath.Join(certDir, "revolut_public.crt")
	keyPath := filepath.Join(certDir, "revolut_private.key")

	if !*force {
		for _, p := range []string{cfgPath, certPath, keyPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("fsvc pull: %s already exists; rerun with -f to overwrite", p)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	mat, err := opvault.New(*vault).Fetch(ctx)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return fmt.Errorf("fsvc pull: mkdir %s: %w", certDir, err)
	}
	if err := writeFileAtomic(certPath, mat.CertPEM, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(keyPath, mat.KeyPEM, 0o600); err != nil {
		return err
	}

	cfg := config.New(cfgPath)
	cfg.Revolut.BaseURL = mat.BaseURL
	cfg.Revolut.ClientID = mat.ClientID
	cfg.Revolut.ISS = mat.ISS
	cfg.Revolut.RedirectURI = mat.RedirectURI
	cfg.Revolut.CertPath = certPath
	cfg.Revolut.KeyPath = keyPath
	cfg.Revolut.RefreshToken = mat.RefreshToken
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] Pulled Revolut material from 1Password vault %s\n", *vault)
	fmt.Fprintf(os.Stderr, "    cert          = %s\n", certPath)
	fmt.Fprintf(os.Stderr, "    key           = %s\n", keyPath)
	fmt.Fprintf(os.Stderr, "    config        = %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "    client_id     = %s\n", redactMiddle(cfg.Revolut.ClientID, 8, 4))
	fmt.Fprintf(os.Stderr, "    iss           = %s\n", cfg.Revolut.ISS)
	fmt.Fprintf(os.Stderr, "    redirect_uri  = %s\n", cfg.Revolut.RedirectURI)
	if mat.RefreshToken != "" {
		fmt.Fprintln(os.Stderr, "    refresh_token = present")
	} else {
		fmt.Fprintln(os.Stderr, "    refresh_token = absent")
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("fsvc pull: open %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsvc pull: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fsvc pull: close %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fsvc pull: chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("fsvc pull: rename %s: %w", path, err)
	}
	return nil
}
