package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Reviews default to $XDG_STATE_HOME/crit on a fresh install, and the output
// setting still overrides that data root.
func TestResolveServeReviewPathFollowsXDGStateHome(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	got, err := resolveServeReviewPath("", "", "deadbeef1234")
	if want := filepath.Join(stateHome, "crit", "reviews", "deadbeef1234"); err != nil || got != want {
		t.Fatalf("default review path = %q, %v; want %q", got, err, want)
	}

	output := t.TempDir()
	got, err = resolveServeReviewPath(output, "", "deadbeef1234")
	if want := filepath.Join(output, "reviews", "deadbeef1234"); err != nil || got != want {
		t.Fatalf("output review path = %q, %v; want %q", got, err, want)
	}
}

// `crit install prompts` run from the home directory installs into the user's
// prompts directory, which is $XDG_CONFIG_HOME/crit/prompts on a fresh install.
func TestInstallPromptsGlobalUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Chdir(home)

	if err := installPrompts(false); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cfgHome, "crit", "prompts"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no prompts installed under XDG_CONFIG_HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".crit")); !os.IsNotExist(err) {
		t.Fatalf("fresh install created ~/.crit (err %v)", err)
	}
}
