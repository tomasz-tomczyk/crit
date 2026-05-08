package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jjTopicChainRevset format depends on whether a default base resolves. We test
// the literal output for both shapes (with-base / fallback) without driving jj.
func TestJJTopicChainRevset_FallbackToRoot(t *testing.T) {
	dir := t.TempDir() // not a jj repo — base resolution will fail
	got := jjTopicChainRevset(dir, 0)
	if got != "ancestors(@) ~ root()" {
		t.Errorf("jjTopicChainRevset(no base, depth=0) = %q, want fallback to root()", got)
	}

	got = jjTopicChainRevset(dir, 5)
	if got != "ancestors(@, 5) ~ root()" {
		t.Errorf("jjTopicChainRevset(no base, depth=5) = %q, want bounded fallback", got)
	}
}

func TestJJTopicChainRevset_WithBase(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	got := jjTopicChainRevset(dir, 0)
	// Sanity: must reference `ancestors(@)` and exclude ancestors of the base
	// commit_id, not fall back to `~ root()`.
	if !strings.HasPrefix(got, "ancestors(@) ~ ancestors(commit_id(") {
		t.Errorf("jjTopicChainRevset with resolvable base should reference commit_id(); got %q", got)
	}
	if strings.Contains(got, "~ root()") {
		t.Errorf("expected base-anchored revset, got fallback shape: %q", got)
	}

	got = jjTopicChainRevset(dir, 7)
	if !strings.HasPrefix(got, "ancestors(@, 7) ~ ancestors(commit_id(") {
		t.Errorf("jjTopicChainRevset(depth=7) shape unexpected: %q", got)
	}
}

func TestJJCommitSubject(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	// Add a known subject to look up.
	if err := os.WriteFile(filepath.Join(dir, "subj.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "subj.txt")
	runJJWithUser(t, dir, "commit", "-m", "subject under test")
	sha := runJJ(t, dir, "log", "-r", "@-", "--no-graph", "-T", "commit_id")

	got := jjCommitSubject(dir, sha)
	if got != "subject under test" {
		t.Errorf("jjCommitSubject = %q, want %q", got, "subject under test")
	}

	if got := jjCommitSubject(dir, "deadbeefdeadbeef"); got != "" {
		t.Errorf("jjCommitSubject(unknown) = %q, want empty", got)
	}
}

func TestCommitSubjectFor_JJ(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	if err := os.WriteFile(filepath.Join(dir, "p.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "p.txt")
	runJJWithUser(t, dir, "commit", "-m", "picker subject")
	sha := runJJ(t, dir, "log", "-r", "@-", "--no-graph", "-T", "commit_id")

	got := commitSubjectFor(&JJVCS{}, dir, sha)
	if got != "picker subject" {
		t.Errorf("commitSubjectFor(jj) = %q, want %q", got, "picker subject")
	}
}

func TestTopicChainSHAs_JJ_OnlyTopicCommits(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	mainSHA := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	if err := os.WriteFile(filepath.Join(dir, "topic.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "topic.txt")
	runJJWithUser(t, dir, "commit", "-m", "topic commit")
	topicSHA := runJJ(t, dir, "log", "-r", "@-", "--no-graph", "-T", "commit_id")

	out := topicChainSHAs(&JJVCS{}, dir)
	if out[mainSHA] {
		t.Errorf("topicChainSHAs should exclude default-branch tip %q; got map %v", mainSHA, out)
	}
	if !out[topicSHA] {
		t.Errorf("topicChainSHAs should include topic commit %q; got map %v", topicSHA, out)
	}
}
