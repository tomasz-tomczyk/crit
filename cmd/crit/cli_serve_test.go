package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestPreflightCheck_CleanRepo verifies that running crit in a clean
// repo with no changes returns the user-facing message instead of letting
// the daemon spawn and crash with a misleading "could not reach daemon" error.
func TestPreflightCheck_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)
	defaultBranchOnce = sync.Once{}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &serverConfig{}
	msg := preflightCheck(sc)
	if msg == "" {
		t.Fatal("expected a no-changes message, got empty string")
	}
	if !strings.Contains(msg, "No changed files found.") {
		t.Errorf("missing headline; got:\n%s", msg)
	}
	if !strings.Contains(msg, "crit <file") {
		t.Errorf("missing file-args hint; got:\n%s", msg)
	}
	if !strings.Contains(msg, "review changed files") {
		t.Errorf("missing default-mode explanation; got:\n%s", msg)
	}
	if strings.Contains(msg, "plan") {
		t.Errorf("message should not mention plan mode (internal subcommand); got:\n%s", msg)
	}
	// Must not mention daemons, ports, or networking.
	for _, banned := range []string{"daemon", "port", "connection", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("message mentions %q; should be free of networking/daemon noise:\n%s", banned, msg)
		}
	}
}

// TestPreflightCheck_WithChanges verifies the preflight is silent
// (returns "") when the repo has changes, so the daemon proceeds normally.
func TestPreflightCheck_WithChanges(t *testing.T) {
	dir := initTestRepo(t)
	defaultBranchOnce = sync.Once{}

	gitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/README.md", "# Modified\n")

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &serverConfig{}
	if msg := preflightCheck(sc); msg != "" {
		t.Errorf("expected empty message when repo has changes, got:\n%s", msg)
	}
}

// TestPreflightCheck_NotARepo verifies that preflightCheck returns a
// user-facing error when run outside a VCS repo. Issue #593.
func TestPreflightCheck_NotARepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &serverConfig{}
	msg := preflightCheck(sc)
	if msg == "" {
		t.Fatal("expected a not-in-repo message, got empty string")
	}
	if !strings.Contains(msg, "Not in a version-controlled repository") {
		t.Errorf("missing headline; got:\n%s", msg)
	}
	if !strings.Contains(msg, "crit <file") {
		t.Errorf("missing file-args hint; got:\n%s", msg)
	}
	for _, banned := range []string{"daemon", "port", "connection", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("message mentions %q; should be free of networking/daemon noise:\n%s", banned, msg)
		}
	}
}

// TestResolveServeReviewPath_PlanModeColocatesWithReviewFile verifies that
// plan-mode daemons compute a review path under the plan dir, so attachment
// upload and share-payload inlining target the same folder. Pre-fix, plan
// mode fell through to the centralized ~/.crit/reviews/<key> path while
// session.critJSONPath() returned <planDir>/.crit — the split caused pasted
// images to render as [image: <alt>] placeholders on crit-web.
func TestResolveServeReviewPath(t *testing.T) {
	t.Run("outputDir wins", func(t *testing.T) {
		dir := t.TempDir()
		got := resolveServeReviewPath(dir, "/some/plan/dir", "deadbeef")
		want := filepath.Join(dir, ".crit")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("planDir used when outputDir empty", func(t *testing.T) {
		planDir := t.TempDir()
		got := resolveServeReviewPath("", planDir, "deadbeef")
		want := filepath.Join(planDir, ".crit")
		if got != want {
			t.Errorf("plan-mode review path: got %q, want %q (must co-locate with review.json so attachments/ inlining can find them)", got, want)
		}
	})

	t.Run("centralized path when neither outputDir nor planDir set", func(t *testing.T) {
		got := resolveServeReviewPath("", "", "deadbeef123")
		if !strings.Contains(got, "deadbeef123") {
			t.Errorf("centralized path should embed session key; got %q", got)
		}
	})
}
