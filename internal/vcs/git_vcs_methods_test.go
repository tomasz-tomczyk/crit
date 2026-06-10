package vcs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestGitVCS_Methods exercises GitVCS interface methods so coverage counts
// toward this package (cross-package calls from session tests do not).
func TestGitVCS_Methods(t *testing.T) {
	dir := InitTestRepo(t)
	g := &GitVCS{}

	if root, err := g.RepoRoot(); err != nil || root == "" {
		t.Errorf("RepoRoot() = %q, %v", root, err)
	}
	if g.CurrentBranch() == "" {
		t.Error("CurrentBranch() empty")
	}
	if g.DefaultBranch() == "" {
		t.Error("DefaultBranch() empty")
	}
	g.SetDefaultBranchOverride("main")
	if g.GetDefaultBranchOverride() != "main" {
		t.Errorf("override = %q", g.GetDefaultBranchOverride())
	}
	g.SetDefaultBranchOverride("")

	base := GitRun(t, dir, "rev-parse", "HEAD")
	head := CommitAtForTest(t, dir, "a.txt", "hi", "add a")

	if mb, err := g.MergeBase("HEAD"); err != nil || mb == "" {
		t.Errorf("MergeBase: %q, %v", mb, err)
	}
	if g.DefaultBaseRef() == "" {
		t.Error("DefaultBaseRef() empty")
	}

	changed, err := g.ChangedFilesOnDefaultInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = changed

	changed, err = g.ChangedFilesFromBaseInDir("HEAD", dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = changed

	changed, err = g.ChangedFilesScoped("layer", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	_ = changed

	changed, err = g.ChangedFilesForCommit(head, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].Path != "a.txt" {
		t.Errorf("ChangedFilesForCommit = %+v", changed)
	}

	path := filepath.Join(dir, "a.txt")
	hunks, err := g.FileDiffUnified("a.txt", base, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = hunks

	hunks, err = g.FileDiffScoped("a.txt", "layer", base, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = hunks

	hunks, err = g.FileDiffForCommit("a.txt", head, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = hunks

	hunks, err = g.FileDiffUnifiedNewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = hunks

	log, err := g.CommitLog(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) == 0 {
		t.Error("CommitLog empty")
	}

	untracked, err := g.UntrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = untracked

	tracked, err := g.AllTrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) == 0 {
		t.Error("AllTrackedFiles empty")
	}

	remotes, err := g.RemoteBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = remotes

	ns, err := g.DiffNumstat(base, dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = ns

	content, err := g.FileContentAtRef("README.md", "HEAD", dir)
	if err != nil || content == "" {
		t.Errorf("FileContentAtRef: %q, %v", content, err)
	}

	between, err := g.ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "a.txt", Status: "added"}}
	if !reflect.DeepEqual(between, want) {
		t.Errorf("ChangedFilesBetweenSHAs = %+v, want %+v", between, want)
	}

	diffHunks, err := g.FileDiffBetweenSHAs("a.txt", "", base, head, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = diffHunks

	data, err := g.ReadFileAtSHA(head, "a.txt", dir)
	if err != nil || len(data) == 0 {
		t.Errorf("ReadFileAtSHA: %v", err)
	}

	if !g.HasObject(head, dir) {
		t.Error("HasObject(head) false")
	}

	status := g.FileStatusInRepo("a.txt", base, dir)
	if status == "" {
		t.Error("FileStatusInRepo empty")
	}

	if g.UserName() == "" {
		t.Log("git user.name unset in test env (non-fatal)")
	}

	ctxHunks, err := g.FileDiffUnifiedCtx(t.Context(), "a.txt", base, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = ctxHunks

	tipSHA := GitRun(t, dir, "rev-parse", "HEAD")
	GitRun(t, dir, "update-ref", "refs/remotes/origin/feat-x", tipSHA)
	branchTips, err := RemoteBranchTips(g, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	_ = branchTips
}

func TestResolveDefaultBranchSHA_Git(t *testing.T) {
	dir := InitTestRepo(t)
	want := GitRun(t, dir, "rev-parse", "HEAD")
	got, err := ResolveDefaultBranchSHA(&GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestResolveDefaultBranchSHA_NilVCS(t *testing.T) {
	if _, err := ResolveDefaultBranchSHA(nil, "", "main"); err == nil {
		t.Fatal("expected error for nil vcs")
	}
}

func TestIsCommitish(t *testing.T) {
	if !IsCommitish("abc1234") {
		t.Error("expected true for hex sha")
	}
	if IsCommitish("not-a-sha") {
		t.Error("expected false for non-hex")
	}
	if IsCommitish("-bad") {
		t.Error("expected false for option injection")
	}
}

func TestSplitCommitRange_Valid(t *testing.T) {
	base, head, ok := SplitCommitRange("abc1234..def5678")
	if !ok || base != "abc1234" || head != "def5678" {
		t.Errorf("got %q, %q, %v", base, head, ok)
	}
}

func TestWalkAncestors_Git(t *testing.T) {
	dir := InitTestRepo(t)
	CommitAtForTest(t, dir, "a.txt", "1", "a")
	shas, err := WalkAncestors(&GitVCS{}, dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) < 2 {
		t.Errorf("expected >= 2 ancestors, got %d", len(shas))
	}
}

func TestLocalBranchTips_Git(t *testing.T) {
	dir := InitTestRepo(t)
	CommitAtForTest(t, dir, "a.txt", "x", "a")
	GitRun(t, dir, "checkout", "-b", "feat-x")
	CommitAtForTest(t, dir, "b.txt", "y", "b")

	got, err := LocalBranchTips(&GitVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range got {
		if name == "feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("feat-x not in tips: %+v", got)
	}
}

// WorkingTreeFingerprint runs `git status --porcelain` in the process CWD and
// returns "" for a clean tree, so asserting it from the ambient checkout is
// nondeterministic (dev machines are dirty, CI runners are clean). Chdir into
// a repo with an untracked file to make the assertion meaningful.
func TestGitVCS_WorkingTreeFingerprint(t *testing.T) {
	dir := InitTestRepo(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if (&GitVCS{}).WorkingTreeFingerprint() == "" {
		t.Error("WorkingTreeFingerprint empty for dirty tree")
	}
}
