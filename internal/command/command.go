// Package command implements the fsvc CLI subcommand surface. Each
// subcommand has its own file (cert.go, revolut.go, ...) and exports
// a single Run* entry point that takes Globals + the remaining args.
//
// This file holds the shared bits: Globals struct, the version
// metadata bridged from main, and the small helpers every subcommand
// uses (config-with-fallback-message, fail-on-validation-errors).
package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
)

// Globals is the cross-subcommand state plumbed in from cmd/fsvc/main.
// Currently just the resolved config path (might be empty = use
// default) and the version string for `fsvc --version`. Kept as a
// struct rather than package-level globals so subcommand tests can
// instantiate their own.
type Globals struct {
	// ConfigPath is the explicit config file passed via -c, or empty
	// to use config.DefaultPath().
	ConfigPath string

	// Verbose enables more chatter in subcommands. Reserved for v0.1.x.
	Verbose bool

	// Version / Commit / Date are bridged from main's ldflags so
	// `fsvc --version` and friends can echo them.
	Version string
	Commit  string
	Date    string
}

// LoadConfig is the canonical "give me the config or die nicely"
// helper. Returns a freshly-loaded *config.Config, or an error with a
// "run `fsvc init` first" hint when no config exists yet.
func (g *Globals) LoadConfig() (*config.Config, error) {
	cfg, err := config.Load(g.ConfigPath)
	if err != nil {
		if errors.Is(err, config.ErrNoConfig) {
			path := g.ConfigPath
			if path == "" {
				path = config.DefaultPath()
			}
			return nil, fmt.Errorf("%w\n\nNo config at %s. To set one up:\n  1. fsvc cert            # generate the mTLS keypair\n  2. (upload the public cert in Revolut Business → Settings → APIs)\n  3. fsvc init            # interactive setup (coming in v0.1.x)\n     # OR copy and edit the example:\n     #   cp $(fsvc-share)/config.example.toml %s", err, path, path)
		}
		return nil, err
	}
	return cfg, nil
}

// MustValidate runs cfg.Validate() and prints all problems to stderr
// before returning a single rolled-up error. Used by subcommands
// that need a fully-formed config (everything except `cert` and
// `init`).
func MustValidate(cfg *config.Config) error {
	probs := cfg.Validate()
	if len(probs) == 0 {
		return nil
	}
	fmt.Fprintln(os.Stderr, "fsvc: configuration is incomplete:")
	for _, p := range probs {
		fmt.Fprintln(os.Stderr, "  -", p)
	}
	return fmt.Errorf("invalid configuration (%d problem(s))", len(probs))
}
