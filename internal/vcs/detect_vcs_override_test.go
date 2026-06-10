package vcs

import "testing"

func TestDetectVCS_ExplicitGitOverride(t *testing.T) {
	v := DetectVCS("git")
	if v == nil || v.Name() != "git" {
		t.Fatalf("DetectVCS(git) = %v", v)
	}
}

func TestDetectVCS_AutoDetectInTempRepo(t *testing.T) {
	dir := InitTestRepo(t)
	t.Chdir(dir)
	v := DetectVCS("")
	if v == nil || v.Name() != "git" {
		t.Fatalf("auto-detect in git repo = %v", v)
	}
}

func TestDetectVCS_JJOverrideWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	t.Chdir(dir)
	v := DetectVCS("jj")
	if v == nil {
		t.Fatal("expected git fallback when jj missing")
	}
	if v.Name() != "git" {
		t.Errorf("got vcs %q, want git fallback", v.Name())
	}
}

func TestDetectVCS_SaplingOverrideWithoutBinary(t *testing.T) {
	dir := InitTestRepo(t)
	t.Chdir(dir)
	v := DetectVCS("sapling")
	if v == nil || v.Name() != "git" {
		t.Fatalf("expected git fallback, got %v", v)
	}
}
