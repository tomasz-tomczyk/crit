package session

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// twoStateRoots gives a home whose XDG state folder is in use while the
// legacy ~/.crit still holds data, as after a partial move. It returns the
// home, the XDG root and the legacy root.
func twoStateRoots(t *testing.T) (home, xdg, legacy string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the legacy layout without XDG variables")
	}
	home = t.TempDir()
	testutil.SetHome(t, home) // seeds ~/.crit
	xdg = filepath.Join(home, ".local", "state", "crit")
	mustMkdir(t, xdg)
	return home, xdg, filepath.Join(home, ".crit")
}

func TestListResumableReviews_ReadsEveryStateRoot(t *testing.T) {
	_, xdg, legacy := twoStateRoots(t)
	if err := SaveCritJSON(filepath.Join(xdg, "reviews", "aaaaaaaaaaaa"), CritJSON{Branch: "xdg", UpdatedAt: "2026-03-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCritJSON(filepath.Join(legacy, "reviews", "bbbbbbbbbbbb"), CritJSON{Branch: "legacy", UpdatedAt: "2026-02-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	// The same key in both: listed once, from the root reconnect reads.
	if err := SaveCritJSON(filepath.Join(xdg, "reviews", "cccccccccccc"), CritJSON{Branch: "both-xdg", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCritJSON(filepath.Join(legacy, "reviews", "cccccccccccc"), CritJSON{Branch: "both-legacy", UpdatedAt: "2026-04-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	reviews, err := listResumableReviews()
	if err != nil {
		t.Fatal(err)
	}
	var branches []string
	for _, r := range reviews {
		branches = append(branches, r.branch)
	}
	want := []string{"xdg", "legacy", "both-xdg"}
	if !slices.Equal(branches, want) {
		t.Fatalf("branches = %v, want %v", branches, want)
	}
}

// A review that lives only in the other root resumes from there: the daemon
// is pointed at that root with --output.
func TestReconnect_ReviewInOtherStateRoot(t *testing.T) {
	_, _, legacy := twoStateRoots(t)
	const key = "839f3b4cd5d6"
	revDir := filepath.Join(legacy, "reviews", key)
	if err := SaveCritJSON(revDir, CritJSON{}); err != nil {
		t.Fatal(err)
	}

	got, err := resolveReconnectReviewDir(key, daemon.SessionEntry{})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(revDir) {
		t.Fatalf("review dir = %q, want %q", got, revDir)
	}
	args := daemonArgsForReconnect(key, nil, daemon.SessionEntry{}, got, true)
	i := slices.Index(args, "--output")
	if i < 0 || i+1 >= len(args) || filepath.Clean(args[i+1]) != filepath.Clean(legacy) {
		t.Fatalf("args = %q, want --output %s", args, legacy)
	}
}

// After moving ~/.crit to the XDG folder, a session file that still records
// the old review_path finds the review where it went.
func TestReconnect_StaleReviewPathAfterMove(t *testing.T) {
	_, xdg, legacy := twoStateRoots(t)
	const key = "839f3b4cd5d6"
	moved := filepath.Join(xdg, "reviews", key)
	if err := SaveCritJSON(moved, CritJSON{}); err != nil {
		t.Fatal(err)
	}
	stale := daemon.SessionEntry{ReviewPath: filepath.Join(legacy, "reviews", key)}

	got, err := resolveReconnectReviewDir(key, stale)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(moved) {
		t.Fatalf("review dir = %q, want %q", got, moved)
	}
	if args := daemonArgsForReconnect(key, nil, stale, got, true); slices.Contains(args, "--output") {
		t.Fatalf("args = %q, want no --output for the default root", args)
	}
}
