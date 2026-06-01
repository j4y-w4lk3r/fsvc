// Command fsvc is the financial-services CLI for the j4y suite — a focused
// client for personal banking APIs (Revolut Business today, ING tomorrow)
// with the same secrets-management story as the rest of the suite:
// encrypted at rest via `ykw encrypt`, decrypted on demand with a
// YubiKey, never written to disk in plaintext.
//
// Subcommand layout:
//
//	fsvc cert                         generate Revolut mTLS keypair
//	fsvc revolut exchange-code <CODE> trade an OAuth auth code for refresh+access tokens
//	fsvc revolut auth                 refresh access_token via JWT client_assertion
//	fsvc revolut accounts             list accounts
//	fsvc revolut cards                list cards
//	fsvc revolut txs [flags]          list transactions
//
// Planned (v0.1.x):
//
//	fsvc init                         interactive setup
//	fsvc bootstrap                    pull encrypted creds from b2://j4y-bu/fsvc/
//
// Global flags (BEFORE the subcommand name):
//
//	-c PATH    config file (overrides $XDG_CONFIG_HOME/fsvc/config.toml)
//	-V         verbose output
//	-v         print version and exit
//	-h         show help
//
// Per-subcommand flags follow the subcommand name.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/j4y-w4lk3r/fsvc/internal/command"
)

// Stamped at link time by goreleaser via -ldflags
// "-X main.version=... -X main.commit=... -X main.date=...". Defaults
// fire on `go run` / `go build` without ldflags so we still print
// something useful in dev — same pattern as bbm/rui.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	gfs := flag.NewFlagSet("fsvc", flag.ContinueOnError)
	gfs.SetOutput(os.Stderr)
	configPath := gfs.String("c", "", "config file path (overrides $XDG_CONFIG_HOME/fsvc/config.toml)")
	verbose := gfs.Bool("V", false, "verbose output")
	showVer := gfs.Bool("v", false, "print version and exit")
	showVerLong := gfs.Bool("version", false, "print version and exit")
	showHelp := gfs.Bool("h", false, "show help")
	showHelpLong := gfs.Bool("help", false, "show help")

	if err := gfs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if *showVer || *showVerLong {
		fmt.Printf("fsvc %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	if *showHelp || *showHelpLong {
		printUsage(os.Stdout)
		return
	}

	rest := gfs.Args()
	if len(rest) == 0 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	g := &command.Globals{
		ConfigPath: *configPath,
		Verbose:    *verbose,
		Version:    version,
		Commit:     commit,
		Date:       date,
	}

	sub := rest[0]
	subArgs := rest[1:]

	var err error
	switch sub {
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return
	case "cert":
		err = command.RunCert(g, subArgs)
	case "init":
		err = command.RunInit(g, subArgs)
	case "revolut":
		err = command.RunRevolut(g, subArgs)
	case "bootstrap":
		// Recognized but not yet implemented. Distinguishes "planned"
		// from "typo" so users (and the README) can tell apart.
		fmt.Fprintf(os.Stderr, "fsvc bootstrap: not yet implemented (target: v0.1.x)\n\n")
		fmt.Fprintln(os.Stderr, "Workaround for now — pull the encrypted bundle manually:")
		fmt.Fprintln(os.Stderr, "  bbm pull fsvc/config.toml.gpg /tmp/fsvc-config.toml.gpg")
		fmt.Fprintln(os.Stderr, "  ykw decrypt /tmp/fsvc-config.toml.gpg ~/.config/fsvc/config.toml")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "fsvc: unknown command %q\n\n", sub)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	if err != nil {
		// Subcommands return errors instead of os.Exiting directly so we
		// can render a single "fsvc: <err>" line in one place.
		fmt.Fprintf(os.Stderr, "fsvc: %v\n", err)
		os.Exit(1)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "fsvc — financial-services CLI for the j4y suite")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  fsvc [GLOBAL FLAGS] <subcommand> [SUBCOMMAND FLAGS] [ARGS...]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  cert                  generate Revolut mTLS keypair")
	fmt.Fprintln(w, "  init                  scaffold ~/.config/fsvc/config.toml from CLI flags")
	fmt.Fprintln(w, "  revolut authorize     one-shot OAuth flow (browser + local server + token swap)")
	fmt.Fprintln(w, "  revolut exchange-code <CODE>")
	fmt.Fprintln(w, "                        manual: trade an OAuth auth code for tokens")
	fmt.Fprintln(w, "  revolut auth          refresh access_token via JWT client_assertion")
	fmt.Fprintln(w, "  revolut accounts      list Revolut Business accounts (with totals)")
	fmt.Fprintln(w, "  revolut cards         list cards")
	fmt.Fprintln(w, "  revolut txs           list transactions (--days, --account, --json, ...)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Planned (v0.1.x):")
	fmt.Fprintln(w, "  bootstrap             pull encrypted creds from b2://j4y-bu/fsvc/")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  -c PATH               config file (default: $XDG_CONFIG_HOME/fsvc/config.toml)")
	fmt.Fprintln(w, "  -V                    verbose")
	fmt.Fprintln(w, "  -v / --version        print version and exit")
	fmt.Fprintln(w, "  -h / --help           show this help")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "First-run walkthrough:")
	fmt.Fprintln(w, "  1. fsvc cert                              # generate the mTLS keypair")
	fmt.Fprintln(w, "  2. (upload .crt to Revolut Business → Settings → APIs)")
	fmt.Fprintln(w, "  3. fsvc init --client-id <ID> \\           # scaffold config.toml")
	fmt.Fprintln(w, "                --redirect-uri http://localhost:8080/callback")
	fmt.Fprintln(w, "  4. fsvc revolut authorize                 # browser opens, code captured automatically")
	fmt.Fprintln(w, "  5. fsvc revolut accounts                  # smoke test")
}
