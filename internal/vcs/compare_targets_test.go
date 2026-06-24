package vcs

import (
	"os/exec"
	"testing"
)

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

func TestCompareTargetsFor_GitRepo(t *testing.T) {
	dir := initTestRepo(t)
	got, err := CompareTargetsFor(&GitVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.VCS != "git" {
		t.Errorf("VCS = %q, want git", got.VCS)
	}
	if len(got.Local) == 0 {
		t.Error("expected at least one local branch")
	}
}

func TestCompareTargetsFor_JJRepo(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	dir := initTestJJRepoWithLocalMain(t)
	got, err := CompareTargetsFor(&JJVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.VCS != "jj" {
		t.Errorf("VCS = %q, want jj", got.VCS)
	}
	if len(got.Local) == 0 {
		t.Error("expected at least one local bookmark")
	}
}
