// Command fsvc is the financial-services CLI for the j4y suite — a focused
// client for personal banking APIs (Revolut Business today, ING tomorrow)
// with the same secrets-management story as the rest of the suite:
// encrypted at rest via `ykw encrypt`, decrypted on demand with a
// YubiKey, never written to disk in plaintext.
//
// Status (v0.0.x): scaffold only. The Revolut endpoints, OAuth/JWT flow,
// and ykw-decrypt integration land in v0.1.x. See README.md for the
// roadmap.
//
// Subcommand layout (target shape, NOT yet implemented):
//
//	fsvc init                         interactive setup (config + cred bootstrap)
//	fsvc cert                         generate Revolut mTLS keypair
//	fsvc revolut auth                 refresh access_token via JWT client_assertion
//	fsvc revolut accounts             list accounts
//	fsvc revolut cards                list cards
//	fsvc revolut txs [--account ID] [--from DATE] [--to DATE]
//	                                  list transactions
//	fsvc bootstrap                    pull encrypted creds from b2://j4y-bu/fsvc/
//
// Global flags (BEFORE the subcommand name):
//
//	-c PATH    config file (overrides $XDG_CONFIG_HOME/fsvc/config.toml)
//	-v         print version and exit
//	-h         show help
//
// Per-subcommand flags follow the subcommand name.
package main

import (
	"flag"
	"fmt"
	"os"
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
	_ = gfs.String("c", "", "config file path (overrides $XDG_CONFIG_HOME/fsvc/config.toml)")
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

	sub := rest[0]
	switch sub {
	case "help":
		printUsage(os.Stdout)
		return
	case "init", "cert", "revolut", "bootstrap":
		// Recognized but not yet implemented. Better UX than a generic
		// "unknown command" so users (and the README) can tell us
		// apart from typos.
		fmt.Fprintf(os.Stderr, "fsvc %s: not yet implemented (target: v0.1.0)\n", sub)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "fsvc is currently scaffold-only. Track progress at:")
		fmt.Fprintln(os.Stderr, "  https://github.com/j4y-w4lk3r/fsvc")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "fsvc: unknown command %q\n\n", sub)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "fsvc — financial-services CLI for the j4y suite")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Status: scaffold only. v0.1.0 will ship the Revolut Business client.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  fsvc [GLOBAL FLAGS] <subcommand> [SUBCOMMAND FLAGS] [ARGS...]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Planned subcommands:")
	fmt.Fprintln(w, "  init                  interactive setup (config + cred bootstrap)")
	fmt.Fprintln(w, "  cert                  generate Revolut mTLS keypair")
	fmt.Fprintln(w, "  revolut auth          refresh access_token via JWT client_assertion")
	fmt.Fprintln(w, "  revolut accounts      list accounts")
	fmt.Fprintln(w, "  revolut cards         list cards")
	fmt.Fprintln(w, "  revolut txs           list transactions")
	fmt.Fprintln(w, "  bootstrap             pull encrypted creds from b2://j4y-bu/fsvc/")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  -c PATH               config file (default: $XDG_CONFIG_HOME/fsvc/config.toml)")
	fmt.Fprintln(w, "  -v / --version        print version and exit")
	fmt.Fprintln(w, "  -h / --help           show this help")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "While scaffold-only, every recognized subcommand exits 2 with a")
	fmt.Fprintln(w, "'not yet implemented' note. Track progress + roadmap at:")
	fmt.Fprintln(w, "  https://github.com/j4y-w4lk3r/fsvc")
}
