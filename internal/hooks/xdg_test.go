package hooks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/hooks"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Global hook scripts are discovered under $XDG_CONFIG_HOME/crit/hooks on a
// fresh install.
func TestResolveFinishCommand_GlobalDiscoveredFileUnderXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dir := filepath.Join(cfgHome, "crit", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "on_finish_approved.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ec, err := hooks.ResolveFinishCommand(nil, nil, t.TempDir(), home, prompt.HookFinishApproved, "files", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Kind != "file" || ec.Arg != script {
		t.Fatalf("ec = %+v, want file %s", ec, script)
	}
	if want := "global:" + filepath.ToSlash(script); ec.Source != want {
		t.Fatalf("source = %q, want %q", ec.Source, want)
	}
}

// A project's own .crit/hooks is not affected by the XDG variables.
func TestResolveFinishCommand_ProjectDiscoveryIgnoresXDG(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := t.TempDir()
	dir := filepath.Join(project, ".crit", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "on_finish_approved.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ec, err := hooks.ResolveFinishCommand(nil, nil, project, t.TempDir(), prompt.HookFinishApproved, "files", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Arg != script || ec.Source != "project:.crit/hooks/on_finish_approved.sh" {
		t.Fatalf("ec = %+v", ec)
	}
}
