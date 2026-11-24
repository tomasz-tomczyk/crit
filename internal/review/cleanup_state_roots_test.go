package review

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// crit cleanup deletes stale reviews from every state root, not only the one
// in use: a partial move to the XDG folder leaves old reviews under ~/.crit.
func TestRunCleanup_EveryStateRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the legacy layout without XDG variables")
	}
	home := t.TempDir()
	testutil.SetHome(t, home) // seeds ~/.crit
	xdg := filepath.Join(home, ".local", "state", "crit", "reviews", "aaaaaaaaaaaa")
	legacy := filepath.Join(home, ".crit", "reviews", "bbbbbbbbbbbb")
	for _, dir := range []string{xdg, legacy} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(`{"updated_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunCleanup([]string{"--days", "7", "--force"}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{xdg, legacy} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v), want it deleted", dir, err)
		}
	}
}
