package command

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// RunCert handles `fsvc cert` — generate a fresh mTLS keypair for the
// Revolut Business API, OR (with --print / --copy) surface an existing
// keypair without regenerating.
//
// Modes:
//
//	fsvc cert            generate keypair (refuses to overwrite without -f)
//	fsvc cert --print    print the existing public cert PEM to stdout
//	                     (handy for `fsvc cert --print | pbcopy`)
//	fsvc cert --copy     copy the existing public cert PEM to the OS
//	                     clipboard via the platform's native tool
//	                     (pbcopy on macOS, wl-copy/xclip/xsel on Linux,
//	                     clip on Windows). Falls back to printing if no
//	                     clipboard tool is available.
//
// Output (default $XDG_CONFIG_HOME/fsvc/revolut-api-cert/):
//
//	revolut_private.key — PKCS#1 RSA 2048-bit, mode 0600
//	revolut_public.crt  — self-signed X.509, valid 5 years, mode 0644
//
// You then upload the .crt to Revolut Business → Settings → APIs and
// they hand back a CLIENT_ID. The .key never leaves the machine and
// is what `fsvc revolut auth` uses to sign client_assertion JWTs +
// terminate the mTLS handshake.
//
// 5-year validity matches what revolut_cert_setup.sh used (`-days
// 1825`). Self-signed is fine here — Revolut pins the public cert by
// fingerprint, not by chain.
func RunCert(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc cert", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	defaultDir := filepath.Join(defaultConfigDir(), "revolut-api-cert")
	outDir := fs.String("dir", defaultDir, "directory to write revolut_private.key + revolut_public.crt")
	cn := fs.String("cn", "fsvc-revolut-business-api", "Common Name for the self-signed cert (cosmetic)")
	bits := fs.Int("bits", 2048, "RSA key size in bits (Revolut requires >= 2048)")
	years := fs.Int("years", 5, "cert validity in years")
	force := fs.Bool("f", false, "overwrite existing keypair")
	doPrint := fs.Bool("print", false, "print existing public cert PEM to stdout (skip generation)")
	doCopy := fs.Bool("copy", false, "copy existing public cert PEM to the OS clipboard (skip generation)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	crtPath := filepath.Join(*outDir, "revolut_public.crt")

	// --print / --copy short-circuit before any keygen. Mutually
	// compatible with each other (both can be passed; behaves like
	// --copy plus a tee to stdout). Mutually exclusive with -f
	// because "force overwrite" implies generation.
	if *doPrint || *doCopy {
		if *force {
			return errors.New("fsvc cert: -f doesn't make sense with --print/--copy (those modes don't generate)")
		}
		raw, err := os.ReadFile(crtPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("fsvc cert: %s does not exist; run `fsvc cert` first to generate the keypair", crtPath)
			}
			return fmt.Errorf("fsvc cert: read %s: %w", crtPath, err)
		}
		if *doPrint {
			if _, err := os.Stdout.Write(raw); err != nil {
				return fmt.Errorf("fsvc cert: write to stdout: %w", err)
			}
		}
		if *doCopy {
			if err := copyToClipboard(raw); err != nil {
				// Don't fail hard — fall back to printing so the user
				// at least has the bytes available to paste manually.
				fmt.Fprintf(os.Stderr, "[!] Couldn't copy to clipboard (%v). Falling back to stdout.\n", err)
				if _, err := os.Stdout.Write(raw); err != nil {
					return fmt.Errorf("fsvc cert: write to stdout: %w", err)
				}
			} else {
				fmt.Fprintf(os.Stderr, "[✓] Public cert PEM copied to clipboard (%d bytes from %s)\n", len(raw), crtPath)
				fmt.Fprintf(os.Stderr, "    Paste it into Revolut Business → Settings → APIs → Add certificate → X509 public key\n")
			}
		}
		return nil
	}

	if *bits < 2048 {
		return fmt.Errorf("fsvc cert: -bits=%d too small (Revolut requires >= 2048)", *bits)
	}

	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return fmt.Errorf("fsvc cert: mkdir %s: %w", *outDir, err)
	}

	keyPath := filepath.Join(*outDir, "revolut_private.key")

	if !*force {
		if _, err := os.Stat(keyPath); err == nil {
			return fmt.Errorf("fsvc cert: %s already exists; rerun with -f to overwrite (this will invalidate the cert uploaded to Revolut!), or use --copy/--print to surface the existing one", keyPath)
		}
		if _, err := os.Stat(crtPath); err == nil {
			return fmt.Errorf("fsvc cert: %s already exists; rerun with -f to overwrite, or use --copy/--print to surface the existing one", crtPath)
		}
	}

	fmt.Fprintf(os.Stderr, "[*] Generating RSA-%d keypair...\n", *bits)
	priv, err := rsa.GenerateKey(rand.Reader, *bits)
	if err != nil {
		return fmt.Errorf("fsvc cert: generate key: %w", err)
	}

	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.AddDate(*years, 0, 0)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("fsvc cert: generate serial: %w", err)
	}

	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: *cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	derCert, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("fsvc cert: self-sign: %w", err)
	}

	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv), 0o600); err != nil {
		return err
	}
	if err := writePEM(crtPath, "CERTIFICATE", derCert, 0o644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] Wrote %s (mode 0600)\n", keyPath)
	fmt.Fprintf(os.Stderr, "[✓] Wrote %s (mode 0644)\n", crtPath)
	fmt.Fprintf(os.Stderr, "\nNext steps:\n")
	fmt.Fprintf(os.Stderr, "  1. fsvc cert --copy                     # copy the public cert to clipboard\n")
	fmt.Fprintf(os.Stderr, "  2. (paste into Revolut Business → Settings → APIs → Add certificate)\n")
	fmt.Fprintf(os.Stderr, "  3. (enable access — make sure the cert listing says 'Access enabled')\n")
	fmt.Fprintf(os.Stderr, "  4. fsvc init --client-id <ID> --redirect-uri http://localhost:8080/callback\n")
	fmt.Fprintf(os.Stderr, "  5. fsvc revolut authorize\n")
	fmt.Fprintf(os.Stderr, "  6. fsvc revolut accounts\n")

	return nil
}

// copyToClipboard pipes `data` into the OS-native clipboard tool.
// Per platform:
//
//	macOS:    pbcopy            (always present)
//	Windows:  clip              (always present on modern Windows)
//	Linux:    wl-copy → xclip → xsel  (try in order; missing all = err)
//
// Why the try-list on Linux: Wayland is the modern default but
// xclip/xsel cover X11 setups. We don't pull in any clipboard
// libraries (golang.design/x/clipboard, atotto/clipboard, etc.) —
// each adds CGO/X11/Cocoa deps that aren't worth it for one feature.
// Shelling out to platform binaries keeps the build static and the
// dep graph tiny.
//
// Caller is responsible for handling the (rare) "no clipboard tool
// available" failure case — typically by falling back to stdout.
func copyToClipboard(data []byte) error {
	cmd, err := clipboardCmd()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("clipboard: stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("clipboard: start %v: %w", cmd.Args, err)
	}
	if _, err := io.Copy(stdin, bytesReader(data)); err != nil {
		stdin.Close()
		_ = cmd.Wait()
		return fmt.Errorf("clipboard: write: %w", err)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clipboard: %v exited %w", cmd.Args, err)
	}
	return nil
}

// clipboardCmd picks the right clipboard helper for the current
// platform and returns it pre-constructed (not yet started). Returns
// an error if no usable tool is available, so the caller can fall
// back to printing.
func clipboardCmd() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pbcopy"), nil
	case "windows":
		return exec.Command("clip"), nil
	case "linux":
		// Wayland-first, X11-fallback.
		for _, candidate := range [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		} {
			if _, err := exec.LookPath(candidate[0]); err == nil {
				return exec.Command(candidate[0], candidate[1:]...), nil
			}
		}
		return nil, errors.New("no clipboard tool found (install one of: wl-copy, xclip, xsel)")
	default:
		return nil, fmt.Errorf("clipboard: no support for OS %q", runtime.GOOS)
	}
}

// bytesReader is a tiny io.Reader wrapper for a byte slice. We don't
// use bytes.NewReader so this file doesn't have to import "bytes"
// for one call.
func bytesReader(b []byte) io.Reader { return &byteSliceReader{b: b} }

type byteSliceReader struct {
	b []byte
	i int
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

// writePEM writes a single PEM block to disk with the requested mode.
// Uses the atomic pattern (write to tmp, fsync, rename) so we never
// leave a half-written private key in place.
func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("fsvc cert: open %s: %w", tmp, err)
	}
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsvc cert: encode %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsvc cert: sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fsvc cert: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("fsvc cert: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// defaultConfigDir resolves $XDG_CONFIG_HOME/fsvc or $HOME/.config/fsvc.
// Mirrors config.DefaultPath() but returns the directory, not the
// config.toml path. Duplicating one-liners here rather than exporting
// from the config package keeps the package surface small.
func defaultConfigDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "fsvc")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".config", "fsvc")
	}
	return "."
}
