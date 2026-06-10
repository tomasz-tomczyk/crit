package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// writeStaleReview writes a review file aged `days` days into a reviews dir so
// RunCleanup classifies it as stale. Returns the file path.
func writeStaleReview(t *testing.T, revDir, name string, daysOld int) string {
	t.Helper()
	if err := os.MkdirAll(revDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cj := session.CritJSON{
		Branch:      "stale-branch",
		UpdatedAt:   time.Now().Add(-time.Duration(daysOld) * 24 * time.Hour).UTC().Format(time.RFC3339),
		ReviewRound: 1,
		Files: map[string]session.CritJSONFile{
			"main.go": {Comments: []session.Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	path := filepath.Join(revDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCleanup_InvalidDays(t *testing.T) {
	err := RunCleanup([]string{"--days", "notanumber"})
	if err == nil {
		t.Fatal("RunCleanup with bad --days = nil, want error")
	}
	var exitErr clicmd.ExitError
	if errors.As(err, &exitErr) && exitErr.Code != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.Code)
	}
}

func TestRunCleanup_NegativeDays(t *testing.T) {
	if err := RunCleanup([]string{"--days", "-3"}); err == nil {
		t.Fatal("RunCleanup with negative --days = nil, want error")
	}
}

func TestRunCleanup_UnknownFlag(t *testing.T) {
	if err := RunCleanup([]string{"--bogus"}); err == nil {
		t.Fatal("RunCleanup with unknown flag = nil, want error")
	}
}

func TestRunCleanup_ForceDeletesStale(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	revDir := filepath.Join(home, ".crit", "reviews")
	staleFile := writeStaleReview(t, revDir, "stale123.json", 30)

	if err := RunCleanup([]string{"--days", "7", "--force"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale review not deleted: err=%v", err)
	}
}

func TestRunCleanup_AbortKeepsFiles(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	revDir := filepath.Join(home, ".crit", "reviews")
	staleFile := writeStaleReview(t, revDir, "stale123.json", 30)

	// Without --force, RunCleanup reads a confirmation from stdin. Feed "n" so
	// it aborts and leaves the file intact.
	withStdin(t, "n\n")
	if err := RunCleanup([]string{"--days", "7"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(staleFile); err != nil {
		t.Errorf("stale review should survive abort: %v", err)
	}
}

// withStdin replaces os.Stdin with a pipe carrying `input` for the test.
func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}
