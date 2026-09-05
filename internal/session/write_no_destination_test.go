package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A session with no RepoRoot, OutputDir or ReviewFilePath has no review
// destination. Before the critJSONPath guard it fell back to the relative
// path ".crit", so the 200ms debounced write landed in whatever directory
// the process happened to be in — which other tests change with os.Chdir.
// That is how TestNewSessionFromGitLazyThreshold intermittently saw an extra
// .crit/review.json in its fixture repo.
func TestScheduleWriteWithoutDestinationDoesNotTouchCWD(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{{
			Path:     "test.md",
			Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"}},
		}},
	}
	s.mu.Lock()
	s.scheduleWrite()
	s.mu.Unlock()

	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	time.Sleep(500 * time.Millisecond)

	if _, err := os.Stat(filepath.Join(dir, ".crit")); !os.IsNotExist(err) {
		t.Fatalf("debounced write leaked .crit into the working directory (stat err: %v)", err)
	}
}

// SetFocus persists the active diff scope on every focus change, including on
// sessions that have no review destination. Without the guard in
// persistActiveDiffScope that write created a .crit folder in the process
// working directory, which is the same leak the test above covers.
func TestPersistActiveDiffScopeWithoutDestinationDoesNotTouchCWD(t *testing.T) {
	s := &Session{ReviewRound: 1}

	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := s.persistActiveDiffScope("layer"); err != nil {
		t.Fatalf("persistActiveDiffScope: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".crit")); !os.IsNotExist(err) {
		t.Fatalf("persistActiveDiffScope leaked .crit into the working directory (stat err: %v)", err)
	}
}
