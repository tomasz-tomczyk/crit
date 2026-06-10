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

// JJ/sapling fallback-without-binary paths are covered by
// TestDetectVCS_JJOverrideFallsBackToGitWhenNotInstalled (jj_detect_test.go)
// and TestDetectVCS_SaplingOverride (sapling_test.go), which skip when the
// respective binary is on PATH (as in CI).
