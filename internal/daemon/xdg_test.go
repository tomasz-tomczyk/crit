package daemon

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// On a fresh install sessions and reviews live under $XDG_STATE_HOME/crit.
func TestStateDirsFollowXDGStateHome(t *testing.T) {
	testutil.SetFreshHome(t, t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	root := filepath.Join(stateHome, "crit")

	sessions, err := SessionsDir()
	if err != nil || sessions != filepath.Join(root, "sessions") {
		t.Fatalf("SessionsDir = %q, %v; want %q", sessions, err, filepath.Join(root, "sessions"))
	}
	review, err := ReviewFilePath("mykey123456")
	if err != nil || review != filepath.Join(root, "reviews", "mykey123456") {
		t.Fatalf("ReviewFilePath = %q, %v", review, err)
	}
}

// An existing ~/.crit keeps holding sessions and reviews when XDG_STATE_HOME
// is set but the XDG state directory does not exist yet.
func TestStateDirsStayInLegacyDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	review, err := ReviewFilePath("mykey123456")
	if want := filepath.Join(home, ".crit", "reviews", "mykey123456"); err != nil || review != want {
		t.Fatalf("ReviewFilePath = %q, %v; want %q", review, err, want)
	}
}
