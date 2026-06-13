package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildDriftedGitRepo creates a repo where the base branch has advanced past the
// point the PR branch forked from:
//
//	main:    C0 ── C1(forkpoint) ── M1(drift, base tip)
//	                  └── F1(PR head)
//
// It returns (repoDir, forkpoint=C1, baseTip=M1, prHead=F1).
func buildDriftedGitRepo(t *testing.T) (dir, forkpoint, baseTip, prHead string) {
	t.Helper()
	dir = InitTestRepo(t) // C0 on main
	forkpoint = CommitAtForTest(t, dir, "base.txt", "base\n", "forkpoint")
	GitRun(t, dir, "checkout", "-b", "feature")
	prHead = CommitAtForTest(t, dir, "feature.txt", "feat\n", "pr work")
	GitRun(t, dir, "checkout", "main")
	baseTip = CommitAtForTest(t, dir, "drift.txt", "drift\n", "base branch drift")
	return dir, forkpoint, baseTip, prHead
}

// TestMergeBaseOf_Git pins the two-arg merge-base: neither side is HEAD, and it
// returns the fork point, not the (drifted) base tip. This is the git path of
// the `crit --pr` fix.
func TestMergeBaseOf_Git(t *testing.T) {
	ClearGitEnvForTest(t) // robust under git hooks that export GIT_DIR
	dir, forkpoint, baseTip, prHead := buildDriftedGitRepo(t)

	mb, err := MergeBaseOf(baseTip, prHead, dir)
	if err != nil {
		t.Fatalf("MergeBaseOf: %v", err)
	}
	if mb != forkpoint {
		t.Errorf("MergeBaseOf(baseTip, prHead) = %s, want forkpoint %s (got base tip %s?)",
			mb, forkpoint, baseTip)
	}

	// Symmetric: argument order must not matter.
	mbSwapped, err := MergeBaseOf(prHead, baseTip, dir)
	if err != nil {
		t.Fatalf("MergeBaseOf (swapped): %v", err)
	}
	if mbSwapped != forkpoint {
		t.Errorf("MergeBaseOf(prHead, baseTip) = %s, want forkpoint %s", mbSwapped, forkpoint)
	}
}

// TestJJVCS_MergeBaseOf covers the two-arg merge-base where neither side is @:
// two siblings off main must resolve to their common ancestor (the fork point),
// not the divergent base-branch tip. This is the jj path of the `crit --pr` fix.
func TestJJVCS_MergeBaseOf(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}

	forkpoint := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	// PR head: a child of main.
	runJJ(t, dir, "new", "main", "-m", "pr work")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prHead := runJJ(t, dir, "log", "-r", "@", "--no-graph", "-T", "commit_id")

	// Base drift: a second child of main, diverging from the PR.
	runJJ(t, dir, "new", "main", "-m", "base drift")
	if err := os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseTip := runJJ(t, dir, "log", "-r", "@", "--no-graph", "-T", "commit_id")

	mb, err := j.MergeBaseOf(baseTip, prHead, dir)
	if err != nil {
		t.Fatalf("MergeBaseOf: %v", err)
	}
	if mb != forkpoint {
		t.Errorf("MergeBaseOf(baseTip, prHead) = %q, want forkpoint %q (got base tip %q?)",
			mb, forkpoint, baseTip)
	}
}

// TestSaplingVCS_MergeBaseOf is the sapling path of the `crit --pr` fix: two
// siblings off the seed must resolve to their common ancestor, not the drifted
// base tip. ancestor() is symmetric, so order does not matter.
func TestSaplingVCS_MergeBaseOf(t *testing.T) {
	dir := initTestSaplingRepo(t)
	s := &SaplingVCS{}

	forkpoint := strings.TrimSpace(runSL(t, dir, "log", "-r", ".", "-T", "{node}"))

	// PR head: a child of the seed.
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSL(t, dir, "commit", "-A", "-m", "pr work")
	prHead := strings.TrimSpace(runSL(t, dir, "log", "-r", ".", "-T", "{node}"))

	// Base drift: move the working parent back to the seed and branch off again.
	runSL(t, dir, "goto", forkpoint)
	if err := os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSL(t, dir, "commit", "-A", "-m", "base drift")
	baseTip := strings.TrimSpace(runSL(t, dir, "log", "-r", ".", "-T", "{node}"))

	mb, err := s.MergeBaseOf(baseTip, prHead, dir)
	if err != nil {
		t.Fatalf("MergeBaseOf: %v", err)
	}
	if strings.TrimSpace(mb) != forkpoint {
		t.Errorf("MergeBaseOf(baseTip, prHead) = %q, want forkpoint %q (got base tip %q?)",
			strings.TrimSpace(mb), forkpoint, baseTip)
	}
}
