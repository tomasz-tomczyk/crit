package main

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// TestPreflightNoChangedFiles_CleanRepo verifies that running crit in a clean
// git repo with no changes returns the user-facing message instead of letting
// the daemon spawn and crash with a misleading "could not reach daemon" error.
// Reproduces issue #438.
func TestPreflightNoChangedFiles_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)
	defaultBranchOnce = sync.Once{}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &serverConfig{}
	msg := preflightNoChangedFiles(sc)
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

// TestPreflightNoChangedFiles_WithChanges verifies the preflight is silent
// (returns "") when the repo has changes, so the daemon proceeds normally.
func TestPreflightNoChangedFiles_WithChanges(t *testing.T) {
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
	if msg := preflightNoChangedFiles(sc); msg != "" {
		t.Errorf("expected empty message when repo has changes, got:\n%s", msg)
	}
}

// TestPreflightNoChangedFiles_NotARepo verifies the preflight is silent (so
// the existing not-a-repo error path in createSession surfaces normally).
func TestPreflightNoChangedFiles_NotARepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &serverConfig{}
	if msg := preflightNoChangedFiles(sc); msg != "" {
		t.Errorf("expected empty message outside a repo, got:\n%s", msg)
	}
}
