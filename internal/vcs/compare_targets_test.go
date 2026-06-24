package vcs

import "testing"

func TestCompareTargetsFor_NilVCS(t *testing.T) {
	got, err := CompareTargetsFor(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.VCS != "" || got.Detected != "" || len(got.Local) != 0 || len(got.Remote) != 0 {
		t.Errorf("CompareTargetsFor(nil) = %+v, want zero", got)
	}
}

func TestLocalBranches_GitRepo(t *testing.T) {
	dir := initTestRepo(t)
	branches, err := LocalBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) == 0 {
		t.Fatal("expected at least main branch")
	}
}

func TestJJLocalBookmarks_LocalRepo(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	names, err := JJLocalBookmarks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("expected main bookmark")
	}
}
