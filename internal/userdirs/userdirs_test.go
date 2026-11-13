package userdirs

import (
	"os"
	"path/filepath"
	"testing"
)

// freshHome returns an empty home with the XDG variables unset, as on a
// machine where crit has never run.
func freshHome(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	return t.TempDir()
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setGOOS(t *testing.T, value string) {
	t.Helper()
	orig := goos
	goos = value
	t.Cleanup(func() { goos = orig })
}

func assertPaths(t *testing.T, home string, want map[string]string) {
	t.Helper()
	got := map[string]string{
		"config":  ConfigFile(home),
		"prompts": PromptsDir(home),
		"hooks":   HooksDir(home),
		"css":     CustomCSS(home),
		"state":   StateDir(home),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestFreshInstallUsesXDGDefaults(t *testing.T) {
	setGOOS(t, "darwin")
	home := freshHome(t)
	cfg := filepath.Join(home, ".config", "crit")
	assertPaths(t, home, map[string]string{
		"config":  filepath.Join(cfg, "config.json"),
		"prompts": filepath.Join(cfg, "prompts"),
		"hooks":   filepath.Join(cfg, "hooks"),
		"css":     filepath.Join(cfg, "custom.css"),
		"state":   filepath.Join(home, ".local", "state", "crit"),
	})
	if IsLegacyInstall(home) {
		t.Error("fresh home reported as a legacy install")
	}
}

func TestFreshInstallHonoursXDGVariables(t *testing.T) {
	home := freshHome(t)
	cfgHome := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	assertPaths(t, home, map[string]string{
		"config":  filepath.Join(cfgHome, "crit", "config.json"),
		"prompts": filepath.Join(cfgHome, "crit", "prompts"),
		"hooks":   filepath.Join(cfgHome, "crit", "hooks"),
		"css":     filepath.Join(cfgHome, "crit", "custom.css"),
		"state":   filepath.Join(stateHome, "crit"),
	})
}

func TestRelativeXDGVariablesAreIgnored(t *testing.T) {
	setGOOS(t, "linux")
	home := freshHome(t)
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	t.Setenv("XDG_STATE_HOME", "relative/state")
	assertPaths(t, home, map[string]string{
		"config": filepath.Join(home, ".config", "crit", "config.json"),
		"state":  filepath.Join(home, ".local", "state", "crit"),
	})
}

// An existing install keeps every path on the legacy layout, including files
// that do not exist yet, even when the XDG variables are set.
func TestLegacyInstallKeepsLegacyLayout(t *testing.T) {
	home := freshHome(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mkdir(t, filepath.Join(home, ".crit", "reviews"))
	legacy := filepath.Join(home, ".crit")
	assertPaths(t, home, map[string]string{
		"config":  filepath.Join(home, ".crit.config.json"),
		"prompts": filepath.Join(legacy, "prompts"),
		"hooks":   filepath.Join(legacy, "hooks"),
		"css":     filepath.Join(legacy, "custom.css"),
		"state":   legacy,
	})
}

// A legacy global config file alone also marks an existing install.
func TestLegacyConfigFileAloneIsALegacyInstall(t *testing.T) {
	home := freshHome(t)
	touch(t, filepath.Join(home, ".crit.config.json"))
	assertPaths(t, home, map[string]string{
		"config": filepath.Join(home, ".crit.config.json"),
		"state":  filepath.Join(home, ".crit"),
	})
}

// Moving one item to its XDG location switches just that item; the rest of a
// legacy install stays where it is. This is how a user moves prompts and hooks
// into a dotfiles repo while reviews stay in ~/.crit.
func TestXDGItemWinsOverLegacyItem(t *testing.T) {
	home := freshHome(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	mkdir(t, filepath.Join(home, ".crit", "prompts"))
	mkdir(t, filepath.Join(home, ".crit", "hooks"))
	touch(t, filepath.Join(home, ".crit.config.json"))

	mkdir(t, filepath.Join(cfgHome, "crit", "prompts"))
	touch(t, filepath.Join(cfgHome, "crit", "config.json"))

	assertPaths(t, home, map[string]string{
		"config":  filepath.Join(cfgHome, "crit", "config.json"),
		"prompts": filepath.Join(cfgHome, "crit", "prompts"),
		"hooks":   filepath.Join(home, ".crit", "hooks"),
		"state":   filepath.Join(home, ".crit"),
	})
}

// Once the XDG state directory exists it is used, so a stray ~/.crit created
// later does not hide a fresh install's reviews.
func TestExistingXDGStateDirWinsOverLegacyDir(t *testing.T) {
	setGOOS(t, "linux")
	home := freshHome(t)
	state := filepath.Join(home, ".local", "state", "crit")
	mkdir(t, state)
	mkdir(t, filepath.Join(home, ".crit"))
	if got := StateDir(home); got != state {
		t.Fatalf("StateDir = %q, want %q", got, state)
	}
}

func TestWindowsWithoutXDGVariablesKeepsLegacyLayout(t *testing.T) {
	setGOOS(t, "windows")
	home := freshHome(t)
	assertPaths(t, home, map[string]string{
		"config":  filepath.Join(home, ".crit.config.json"),
		"prompts": filepath.Join(home, ".crit", "prompts"),
		"state":   filepath.Join(home, ".crit"),
	})

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	if got, want := ConfigFile(home), filepath.Join(cfgHome, "crit", "config.json"); got != want {
		t.Fatalf("ConfigFile with XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}
