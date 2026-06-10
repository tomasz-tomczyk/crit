package session

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestResolveDefaultBranchSHA_Git(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	want := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	got, err := vcs.ResolveDefaultBranchSHA(&vcs.GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestResolveDefaultBranchSHA_NilVCS(t *testing.T) {
	if _, err := vcs.ResolveDefaultBranchSHA(nil, "", "main"); err == nil {
		t.Fatal("expected error for nil vcs")
	}
}

func TestWalkAncestors_Git(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.CommitAtForTest(t, dir, "a.txt", "1", "a")
	vcs.CommitAtForTest(t, dir, "b.txt", "2", "b")

	shas, err := vcs.WalkAncestors(&vcs.GitVCS{}, dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) < 3 {
		t.Errorf("expected >= 3 ancestors (seed + a + b), got %d", len(shas))
	}
}

func TestLocalBranchTips_Git(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.CommitAtForTest(t, dir, "a.txt", "x", "a")
	headBeforeBranch := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.GitRun(t, dir, "checkout", "-b", "feat-x")
	vcs.CommitAtForTest(t, dir, "b.txt", "y", "b")

	got, err := vcs.LocalBranchTips(&vcs.GitVCS{}, dir)
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
		t.Errorf("feat-x not in tips: %+v (head before branch: %s)", got, headBeforeBranch)
	}
}

func TestRemoteBranchTips_Git_ExcludesDefault(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	headSHA := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.GitRun(t, dir, "update-ref", "refs/remotes/origin/feat-x", headSHA)
	vcs.GitRun(t, dir, "update-ref", "refs/remotes/origin/main", headSHA)

	branches, err := vcs.RemoteBranchTips(&vcs.GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "origin/main" || b.Name == "main" {
			t.Errorf("default branch leaked: %+v", b)
		}
	}
	found := false
	for _, b := range branches {
		if b.Name == "origin/feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("origin/feat-x not in tips: %+v", branches)
	}
}
