package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// On a fresh install the global config is created under $XDG_CONFIG_HOME/crit,
// and nothing is written to the legacy ~/.crit.config.json or ~/.crit.
func TestSaveGlobalConfig_FreshInstallWritesXDGConfig(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	want := filepath.Join(cfgHome, "crit", "config.json")
	if got := GlobalConfigPath(); got != want {
		t.Fatalf("GlobalConfigPath = %q, want %q", got, want)
	}
	if err := SaveGlobalConfig(func(m map[string]json.RawMessage) error {
		m["author"] = json.RawMessage(`"xdg"`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600", info.Mode().Perm())
	}
	cfg, _, err := LoadConfigFile(GlobalConfigPath())
	if err != nil || cfg.Author != "xdg" {
		t.Fatalf("reloaded config author = %q, err %v", cfg.Author, err)
	}
	for _, legacy := range []string{".crit.config.json", ".crit"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); !os.IsNotExist(err) {
			t.Errorf("fresh install created legacy %s (err %v)", legacy, err)
		}
	}
}

// An existing ~/.crit.config.json stays the global config even when
// XDG_CONFIG_HOME is set.
func TestGlobalConfigPath_LegacyFileStaysAuthoritative(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	legacy := filepath.Join(home, ".crit.config.json")
	if err := os.WriteFile(legacy, []byte(`{"author":"legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := GlobalConfigPath(); got != legacy {
		t.Fatalf("GlobalConfigPath = %q, want %q", got, legacy)
	}
}

func TestCustomCSSPath_FreshInstallUsesXDGConfig(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	if got, want := CustomCSSPath(), filepath.Join(cfgHome, "crit", "custom.css"); got != want {
		t.Fatalf("CustomCSSPath = %q, want %q", got, want)
	}
}
