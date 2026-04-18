package main

import "testing"

// Compile-time interface compliance check.
var _ VCS = &GitVCS{}

func TestGitVCS_Name(t *testing.T) {
	g := &GitVCS{}
	if got := g.Name(); got != "git" {
		t.Errorf("Name() = %q, want %q", got, "git")
	}
}

func TestGitVCS_HasStagingArea(t *testing.T) {
	g := &GitVCS{}
	if !g.HasStagingArea() {
		t.Error("HasStagingArea() = false, want true")
	}
}

func TestGitVCS_SkipDirNames(t *testing.T) {
	g := &GitVCS{}
	dirs := g.SkipDirNames()
	if len(dirs) != 1 || dirs[0] != ".git" {
		t.Errorf("SkipDirNames() = %v, want [.git]", dirs)
	}
}

func TestDetectVCS_GitOverride(t *testing.T) {
	vcs := DetectVCS("git")
	if vcs == nil || vcs.Name() != "git" {
		t.Errorf("DetectVCS(\"git\") should return GitVCS, got %v", vcs)
	}
}
