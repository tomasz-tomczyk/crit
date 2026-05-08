package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectVCS_JJWinsOverGitInColocatedRepo covers auto-detect ordering:
// a colocated jj+git repo has both .jj and .git directories, and the jj
// backend must take precedence. The contributor's TestDetectVCS_JJOverride
// only covers the explicit `--vcs jj` flag.
func TestDetectVCS_JJWinsOverGitInColocatedRepo(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	withCwd(t, dir)

	v := DetectVCS("")
	if _, ok := v.(*JJVCS); !ok {
		t.Errorf("DetectVCS auto-detect with both .jj and .git = %T, want *JJVCS", v)
	}
}

// TestDetectVCS_JJOverrideFallsBackToGitWhenNotInstalled confirms the
// documented warning path: when --vcs jj is set but jj is not in PATH,
// DetectVCS falls back to git in a real git repo.
func TestDetectVCS_JJOverrideFallsBackToGitWhenNotInstalled(t *testing.T) {
	if _, err := exec.LookPath("jj"); err == nil {
		t.Skip("jj is installed; cannot test fallback")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	withCwd(t, dir)

	v := DetectVCS("jj")
	if _, ok := v.(*GitVCS); !ok {
		t.Errorf("DetectVCS(jj) without jj in PATH = %T, want *GitVCS fallback", v)
	}
}

// TestLoadConfigFile_VCSJJ exercises the config.go parse path for "vcs": "jj".
func TestLoadConfigFile_VCSJJ(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(configPath, []byte(`{"vcs": "jj"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	if cfg.VCS != "jj" {
		t.Errorf("vcs = %q, want jj", cfg.VCS)
	}
}

// TestMergeConfigs_AuthorFallback_JJ exercises the jj branch in the
// VCS-aware author fallback inside mergeConfigs. When the global config
// already supplies an Author, mergeConfigs must keep it (the fallback
// only fires when Author is empty), regardless of VCS.
func TestMergeConfigs_AuthorFallback_JJ_PreservesExplicitAuthor(t *testing.T) {
	global := Config{VCS: "jj", Author: "Explicit Author"}
	project := Config{}
	merged := mergeConfigs(global, project, configPresence{})
	if merged.VCS != "jj" {
		t.Errorf("merged.VCS = %q, want jj", merged.VCS)
	}
	if merged.Author != "Explicit Author" {
		t.Errorf("merged.Author = %q, want %q (fallback should not overwrite explicit value)", merged.Author, "Explicit Author")
	}
}

func TestHasJJDirAt(t *testing.T) {
	dir := t.TempDir()
	if hasJJDirAt(dir) {
		t.Error("hasJJDirAt on empty temp dir = true")
	}
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasJJDirAt(dir) {
		t.Error("hasJJDirAt with .jj present = false")
	}
}

// TestEnsureSHAFetchedJJ_PureJJErrorMessages exercises the pure-JJ error
// message paths in github.go (no .git directory present) without needing a
// real fetchable remote. The stub reports the jj name and a permanently-
// missing object so the function takes the no-fetch branches.
func TestEnsureSHAFetchedJJ_PureJJErrorMessages(t *testing.T) {
	repoRoot := t.TempDir() // no .git/, no .jj/ — pure-JJ behavior path

	stub := &fakeJJVCSForFetch{}
	err := ensureSHAFetchedJJ(stub, "abc123", repoRoot, "")
	if err == nil {
		t.Fatal("expected error for pure-JJ + missing object")
	}
	if !strings.Contains(err.Error(), "pure JJ auto-fetch is not supported") {
		t.Errorf("pure-JJ error missing expected fragment: %v", err)
	}

	err = ensureSHAFetchedJJ(stub, "abc123", repoRoot, "https://example.com/fork.git")
	if err == nil {
		t.Fatal("expected error for pure-JJ + fork + missing object")
	}
	if !strings.Contains(err.Error(), "PR head is on fork") {
		t.Errorf("pure-JJ-with-fork error missing expected fragment: %v", err)
	}
}

// fakeJJVCSForFetch is the minimal VCS implementation needed by
// ensureSHAFetchedJJ. Only Name() and HasObject() are consulted; the rest
// returns zero values to satisfy the interface without doing any real work.
type fakeJJVCSForFetch struct{}

func (*fakeJJVCSForFetch) Name() string                     { return "jj" }
func (*fakeJJVCSForFetch) HasObject(string, string) bool    { return false }
func (*fakeJJVCSForFetch) RepoRoot() (string, error)        { return "", nil }
func (*fakeJJVCSForFetch) CurrentBranch() string            { return "" }
func (*fakeJJVCSForFetch) DefaultBranch() string            { return "" }
func (*fakeJJVCSForFetch) SetDefaultBranchOverride(string)  {}
func (*fakeJJVCSForFetch) GetDefaultBranchOverride() string { return "" }
func (*fakeJJVCSForFetch) DefaultBaseRef() string           { return "" }
func (*fakeJJVCSForFetch) MergeBase(string) (string, error) { return "", nil }
func (*fakeJJVCSForFetch) ChangedFilesOnDefaultInDir(string) ([]FileChange, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) ChangedFilesFromBaseInDir(string, string) ([]FileChange, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) ChangedFilesScoped(string, string) ([]FileChange, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) ChangedFilesForCommit(string, string) ([]FileChange, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffUnified(string, string, string) ([]DiffHunk, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffUnifiedCtx(context.Context, string, string, string) ([]DiffHunk, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffScoped(string, string, string, string) ([]DiffHunk, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffForCommit(string, string, string) ([]DiffHunk, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffUnifiedNewFile(string) ([]DiffHunk, error) { return nil, nil }
func (*fakeJJVCSForFetch) CommitLog(string, string, string) ([]CommitInfo, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) WorkingTreeFingerprint() string              { return "" }
func (*fakeJJVCSForFetch) UntrackedFiles(string) ([]FileChange, error) { return nil, nil }
func (*fakeJJVCSForFetch) AllTrackedFiles(string) ([]string, error)    { return nil, nil }
func (*fakeJJVCSForFetch) RemoteBranches(string) ([]string, error)     { return nil, nil }
func (*fakeJJVCSForFetch) DiffNumstat(string, string) (map[string]NumstatEntry, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) UserName() string { return "" }
func (*fakeJJVCSForFetch) FileContentAtRef(string, string, string) (string, error) {
	return "", nil
}
func (*fakeJJVCSForFetch) ChangedFilesBetweenSHAs(string, string, string) ([]FileChange, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileDiffBetweenSHAs(string, string, string, string) ([]DiffHunk, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) ReadFileAtSHA(string, string, string) ([]byte, error) {
	return nil, nil
}
func (*fakeJJVCSForFetch) FileStatusInRepo(string, string, string) string { return "" }
func (*fakeJJVCSForFetch) HasStagingArea() bool                           { return false }
func (*fakeJJVCSForFetch) SkipDirNames() []string                         { return nil }
