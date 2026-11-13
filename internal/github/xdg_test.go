package github

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestExportsDirFollowsXDGStateHome(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	got, err := exportsDir()
	if want := filepath.Join(stateHome, "crit", "exports"); err != nil || got != want {
		t.Fatalf("exportsDir() = %q, %v; want %q", got, err, want)
	}
}
