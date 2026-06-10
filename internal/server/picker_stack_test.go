package server

import (
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestDetectStack_ExcludesStaleBranchesBeforeMergeBase(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	m1 := vcs.CommitAtForTest(t, dir, "m1.txt", "1", "m1")
	vcs.GitRun(t, dir, "branch", "old", m1)
	vcs.CommitAtForTest(t, dir, "m2.txt", "2", "m2")
	vcs.GitRun(t, dir, "checkout", "-b", "feat")
	f1 := vcs.CommitAtForTest(t, dir, "f1.txt", "f1", "f1")
	f2 := vcs.CommitAtForTest(t, dir, "f2.txt", "f2", "f2")
	vcs.GitRun(t, dir, "branch", "feat-f1", f1)

	stack, err := detectStack(&vcs.GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range stack {
		if e.Label == "old" {
			t.Errorf("stale branch \"old\" leaked into stack: %+v", stack)
		}
		if e.HeadSHA == m1 {
			t.Errorf("entry at pre-merge-base SHA %s leaked: %+v", m1, e)
		}
	}
	wantHeads := map[string]bool{f1: false, f2: false}
	for _, e := range stack {
		if _, ok := wantHeads[e.HeadSHA]; ok {
			wantHeads[e.HeadSHA] = true
		}
	}
	for sha, found := range wantHeads {
		if !found {
			t.Errorf("expected stack to include sha %s, got %+v", sha, stack)
		}
	}
}

func TestDetectStack_IncludesPostMergeBaseBranchTips(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.CommitAtForTest(t, dir, "m.txt", "m", "m")
	vcs.GitRun(t, dir, "checkout", "-b", "feat")
	a := vcs.CommitAtForTest(t, dir, "a.txt", "a", "a")
	vcs.GitRun(t, dir, "branch", "feat-a", a)
	b := vcs.CommitAtForTest(t, dir, "b.txt", "b", "b")
	vcs.GitRun(t, dir, "branch", "feat-b", b)

	stack, err := detectStack(&vcs.GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"feat-a": false, "feat-b": false}
	for _, e := range stack {
		if _, ok := want[e.Label]; ok {
			want[e.Label] = true
		}
	}
	for label, found := range want {
		if !found {
			t.Errorf("expected branch %q in stack, got %+v", label, stack)
		}
	}
}

func TestDetectStack_DropsNakedCommitsBehindBranch(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.GitRun(t, dir, "checkout", "-b", "staging")
	vcs.CommitAtForTest(t, dir, "n1.txt", "n1", "[ABC-100] noise one")
	vcs.CommitAtForTest(t, dir, "n2.txt", "n2", "[ABC-101] noise two")
	vcs.CommitAtForTest(t, dir, "n3.txt", "n3", "[ABC-102] noise three")
	vcs.GitRun(t, dir, "checkout", "-b", "feat-parent")
	vcs.CommitAtForTest(t, dir, "p.txt", "p", "feat-parent commit")
	vcs.GitRun(t, dir, "checkout", "-b", "feat-current")
	vcs.CommitAtForTest(t, dir, "c.txt", "c", "feat-current commit")

	stack, err := detectStack(&vcs.GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := map[string]bool{
		"feat-current": false,
		"feat-parent":  false,
		"staging":      false,
	}
	forbiddenSubstrings := []string{"ABC-100", "ABC-101", "ABC-102"}
	for _, e := range stack {
		if _, ok := wantLabels[e.Label]; ok {
			wantLabels[e.Label] = true
			continue
		}
		for _, sub := range forbiddenSubstrings {
			if strings.Contains(e.Label, sub) {
				t.Errorf("naked commit subject leaked into stack: %q (full entry %+v)", e.Label, e)
			}
		}
	}
	for label, found := range wantLabels {
		if !found {
			t.Errorf("expected %q in stack, got %+v", label, stack)
		}
	}
}

func TestDetectStack_KeepsNakedCommitsAheadOfNearestBranch(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.CommitAtForTest(t, dir, "m.txt", "m", "main")
	vcs.GitRun(t, dir, "checkout", "-b", "feat")
	vcs.CommitAtForTest(t, dir, "f.txt", "f", "feat tip")
	featTipSHA := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.GitRun(t, dir, "checkout", "--detach", featTipSHA)
	vcs.CommitAtForTest(t, dir, "w1.txt", "w1", "wip one")
	vcs.CommitAtForTest(t, dir, "w2.txt", "w2", "wip two")

	stack, err := detectStack(&vcs.GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sawFeat, sawWip bool
	for _, e := range stack {
		if e.Label == "feat" {
			sawFeat = true
		}
		if strings.Contains(e.Label, "wip") {
			sawWip = true
		}
	}
	if !sawFeat {
		t.Errorf("expected feat in stack, got %+v", stack)
	}
	if !sawWip {
		t.Errorf("expected at least one wip naked-commit entry in stack, got %+v", stack)
	}
}

func TestDetectStack_DefaultBranchAsRoot(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.CommitAtForTest(t, dir, "m1.txt", "1", "m1")
	mergeBase := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.GitRun(t, dir, "branch", "stale-at-base", mergeBase)
	vcs.GitRun(t, dir, "checkout", "-b", "feat")
	vcs.CommitAtForTest(t, dir, "f.txt", "f", "f")

	stack, err := detectStack(&vcs.GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range stack {
		if e.HeadSHA == mergeBase {
			t.Errorf("merge-base SHA leaked into entries: %+v", e)
		}
		if e.Label == "stale-at-base" {
			t.Errorf("branch parked at merge-base leaked into entries: %+v", e)
		}
	}
}
