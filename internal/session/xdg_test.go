package session

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Plans, the plan-session map and stats follow $XDG_STATE_HOME on a fresh
// install, and plan reconnects recognise plan folders there.
func TestStatePathsFollowXDGStateHome(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	root := filepath.Join(stateHome, "crit")

	plan, err := planStorageDir("auth-flow")
	if err != nil || plan != filepath.Join(root, "plans", "auth-flow") {
		t.Fatalf("planStorageDir = %q, %v", plan, err)
	}
	mapping, err := planSessionsFile()
	if err != nil || mapping != filepath.Join(root, "plan-sessions.json") {
		t.Fatalf("planSessionsFile = %q, %v", mapping, err)
	}
	stats, err := statsFilePath()
	if err != nil || stats != filepath.Join(root, "stats.json") {
		t.Fatalf("statsFilePath = %q, %v", stats, err)
	}

	flags := planReconnectFlags(filepath.Join(plan, ".crit"))
	if len(flags) != 4 || flags[0] != "--plan-dir" || flags[1] != plan || flags[3] != "auth-flow" {
		t.Fatalf("planReconnectFlags = %v, want [--plan-dir %s --name auth-flow]", flags, plan)
	}
}
