package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasGitDirAt(t *testing.T) {
	dir := InitTestRepo(t)
	if !hasGitDirAtForTest(dir) {
		t.Error("git repo should have .git directory")
	}
	if hasGitDirAtForTest(t.TempDir()) {
		t.Error("empty dir should not have .git")
	}
}

func TestTryGitFetch_FetchesMissingCommit(t *testing.T) {
	// Bare remote with two commits; clone only has the first.
	bare := t.TempDir()
	GitRun(t, bare, "init", "--bare")
	origin := InitTestRepo(t)
	firstSHA := GitRun(t, origin, "rev-parse", "HEAD")
	GitRun(t, origin, "remote", "add", "bare", bare)
	GitRun(t, origin, "push", "bare", "main")

	cloneDir := t.TempDir()
	GitRun(t, cloneDir, "clone", bare, ".")
	GitRun(t, cloneDir, "checkout", firstSHA)

	secondSHA := CommitAtForTest(t, origin, "second.txt", "2", "second")
	GitRun(t, origin, "push", "bare", "main")

	if HasObject(secondSHA, cloneDir) {
		t.Fatal("setup: clone should not have second commit yet")
	}
	if err := tryGitFetchForTest(cloneDir, "origin", secondSHA); err != nil {
		t.Fatalf("tryGitFetch: %v", err)
	}
	if !HasObject(secondSHA, cloneDir) {
		t.Fatal("second commit should be present after fetch")
	}
}

func TestEnsureSHAFetched_Git_FetchesFromOrigin(t *testing.T) {
	bare := t.TempDir()
	GitRun(t, bare, "init", "--bare")
	origin := InitTestRepo(t)
	firstSHA := GitRun(t, origin, "rev-parse", "HEAD")
	GitRun(t, origin, "remote", "add", "bare", bare)
	GitRun(t, origin, "push", "bare", "main")

	cloneDir := t.TempDir()
	GitRun(t, cloneDir, "clone", bare, ".")
	GitRun(t, cloneDir, "checkout", firstSHA)

	secondSHA := CommitAtForTest(t, origin, "fetch.txt", "x", "fetch me")
	GitRun(t, origin, "push", "bare", "main")

	g := &GitVCS{}
	if err := ensureSHAFetched(g, secondSHA, cloneDir, ""); err != nil {
		t.Fatalf("EnsureSHAFetched: %v", err)
	}
	if !g.HasObject(secondSHA, cloneDir) {
		t.Fatal("commit should be reachable after EnsureSHAFetched")
	}
}

func TestEnsureSHAFetched_Git_ForkURLStillMissing(t *testing.T) {
	dir := InitTestRepo(t)
	g := &GitVCS{}
	err := ensureSHAFetched(g, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", dir, "https://github.com/example/fork.git")
	if err == nil {
		t.Fatal("expected error when commit still missing after fetch attempts")
	}
	if !strings.Contains(err.Error(), "fork") {
		t.Errorf("error should mention fork URL, got: %v", err)
	}
}

func TestEnsureSHAFetchedJJ_ColocatedGitRepo(t *testing.T) {
	dir := InitTestRepo(t)
	// Simulate colocated JJ by only checking git fetch path — JJVCS name triggers JJ path.
	j := &JJVCS{}
	err := ensureSHAFetchedJJ(j, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", dir, "")
	if err == nil {
		t.Fatal("expected error for missing commit")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("expected fetch guidance in error, got: %v", err)
	}
}

func TestEnsureSHAFetchedJJ_PureJJWithFork(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &JJVCS{}
	err := ensureSHAFetchedJJ(j, "abc123", dir, "https://github.com/fork/repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fork") {
		t.Errorf("expected fork mention, got: %v", err)
	}
}

func TestEnsureSHAFetchedSapling_MissingCommit(t *testing.T) {
	dir := InitTestRepo(t) // has .git for fallback path
	sl := &SaplingVCS{}
	err := ensureSHAFetchedSapling(sl, "deadbeef", dir, "")
	if err == nil {
		t.Fatal("expected error when commit missing")
	}
	if !strings.Contains(err.Error(), "sl pull") && !strings.Contains(err.Error(), "fetch") {
		t.Errorf("expected sl pull or fetch in error, got: %v", err)
	}
}

func TestEnsureSHAFetchedSapling_ForkURL(t *testing.T) {
	dir := t.TempDir()
	sl := &SaplingVCS{}
	err := ensureSHAFetchedSapling(sl, "abc", dir, "https://github.com/fork/repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fork") {
		t.Errorf("expected fork mention, got: %v", err)
	}
}
