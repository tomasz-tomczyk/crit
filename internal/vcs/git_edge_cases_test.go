package vcs

import (
	"testing"
)

func TestSplitCommitRange_Invalid(t *testing.T) {
	_, _, ok := SplitCommitRange("not-a-range")
	if ok {
		t.Error("expected false for invalid range")
	}
	_, _, ok = SplitCommitRange("abc..")
	if ok {
		t.Error("expected false for empty head")
	}
}

func TestWalkAncestors_JJWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	shas, err := WalkAncestors(&JJVCS{}, dir, 3)
	if err != nil {
		// jj not installed — command fails; that's fine.
		return
	}
	_ = shas
}

func TestWalkAncestors_SaplingWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	shas, err := WalkAncestors(&SaplingVCS{}, dir, 3)
	if err != nil {
		return
	}
	_ = shas
}

func TestLocalBranchTips_JJWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	got, err := LocalBranchTips(&JJVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Without jj, localBranchTipsJJ returns empty map (commands fail silently).
	_ = got
}

func TestRemoteBranchTips_JJWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	got, err := RemoteBranchTips(&JJVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	_ = got
}

func TestChangedFilesOnDefaultInDir_Git(t *testing.T) {
	dir := InitTestRepo(t)
	writeFileForTest(t, dir+"/dirty.txt", "uncommitted")
	files, err := (&GitVCS{}).ChangedFilesOnDefaultInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("expected changed files on default branch with dirty working tree")
	}
}

func TestFileStatusInRepo_Git(t *testing.T) {
	dir := InitTestRepo(t)
	writeFileForTest(t, dir+"/new.txt", "x")
	base := GitRun(t, dir, "rev-parse", "HEAD")
	status := (&GitVCS{}).FileStatusInRepo("new.txt", base, dir)
	if status != "untracked" && status != "?" {
		t.Errorf("untracked file status = %q", status)
	}
}
