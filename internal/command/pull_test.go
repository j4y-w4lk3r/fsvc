package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
)

func TestRunPullRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := &Globals{ConfigPath: cfgPath}
	err := RunPull(g, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v", err)
	}
}

func TestWriteFileAtomicModes(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "revolut_private.key")
	if err := writeFileAtomic(key, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	cfg := config.New(filepath.Join(dir, "config.toml"))
	cfg.Revolut.ClientID = "abc"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}
