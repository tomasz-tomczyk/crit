package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// GitVCS implements VCS for git repositories. Each method delegates to the
// existing package-level function in git.go.
type GitVCS struct{}

func (g *GitVCS) Name() string { return "git" }

func (g *GitVCS) RepoRoot() (string, error) { return RepoRoot() }

func (g *GitVCS) CurrentBranch() string { return CurrentBranch() }

func (g *GitVCS) DefaultBranch() string { return DefaultBranch() }

func (g *GitVCS) SetDefaultBranchOverride(branch string) { setDefaultBranchOverride(branch) }

func (g *GitVCS) GetDefaultBranchOverride() string { return getDefaultBranchOverride() }

func (g *GitVCS) MergeBase(ref string) (string, error) { return MergeBase(ref) }

func (g *GitVCS) ChangedFilesOnDefaultInDir(dir string) ([]FileChange, error) {
	return changedFilesOnDefaultInDir(dir)
}

func (g *GitVCS) ChangedFilesFromBaseInDir(baseRef, dir string) ([]FileChange, error) {
	return changedFilesFromBaseInDir(baseRef, dir)
}

func (g *GitVCS) ChangedFilesScoped(scope, baseRef string) ([]FileChange, error) {
	return ChangedFilesScoped(scope, baseRef)
}

func (g *GitVCS) ChangedFilesForCommit(sha, dir string) ([]FileChange, error) {
	return ChangedFilesForCommit(sha, dir)
}

func (g *GitVCS) FileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error) {
	return fileDiffUnified(path, baseRef, dir)
}

func (g *GitVCS) FileDiffUnifiedCtx(ctx context.Context, path, baseRef, dir string) ([]DiffHunk, error) {
	return fileDiffUnifiedCtx(ctx, path, baseRef, dir)
}

func (g *GitVCS) FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error) {
	return FileDiffScoped(path, scope, baseRef, dir)
}

func (g *GitVCS) FileDiffForCommit(path, sha, dir string) ([]DiffHunk, error) {
	return FileDiffForCommit(path, sha, dir)
}

func (g *GitVCS) FileDiffUnifiedNewFile(path string) ([]DiffHunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FileDiffUnifiedNewFile(string(data)), nil
}

func (g *GitVCS) CommitLog(baseRef, dir string) ([]CommitInfo, error) {
	return CommitLog(baseRef, dir)
}

func (g *GitVCS) WorkingTreeFingerprint() string { return WorkingTreeFingerprint() }

func (g *GitVCS) UntrackedFiles(dir string) ([]FileChange, error) {
	return untrackedFilesInDir(dir)
}

func (g *GitVCS) AllTrackedFiles(dir string) ([]string, error) {
	return AllTrackedFiles(dir)
}

func (g *GitVCS) RemoteBranches(dir string) ([]string, error) {
	return RemoteBranches(dir)
}

func (g *GitVCS) DiffNumstat(baseRef, dir string) (map[string]NumstatEntry, error) {
	return DiffNumstatDir(baseRef, dir)
}

func (g *GitVCS) UserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FileContentAtRef returns the content of a file at the given git ref.
func (g *GitVCS) FileContentAtRef(path, ref, dir string) (string, error) {
	content := fileContentAtRef(path, ref, dir)
	return content, nil
}

// FileStatusInRepo returns the status of a single file relative to baseRef.
// Note: the VCS interface uses (path, baseRef, dir) order while the underlying
// fileStatusInRepo uses (path, repoRoot, baseRef) — arguments are reordered here.
func (g *GitVCS) FileStatusInRepo(path, baseRef, repoRoot string) string {
	return fileStatusInRepo(path, repoRoot, baseRef)
}

func (g *GitVCS) HasStagingArea() bool { return true }

func (g *GitVCS) SkipDirNames() []string { return []string{".git"} }
