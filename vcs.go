package main

import (
	"context"
	"os"
	"path/filepath"
)

// VCS abstracts version control operations so crit can support multiple backends
// (git, Sapling, etc.). Each method corresponds to an existing package-level
// function in git.go; the interface lets callers work with any VCS uniformly.
type VCS interface {
	// Name returns the VCS identifier ("git", "sl", etc.).
	Name() string

	// RepoRoot returns the absolute path to the repository root.
	RepoRoot() (string, error)

	// CurrentBranch returns the name of the current branch.
	CurrentBranch() string

	// DefaultBranch returns the name of the default branch (e.g. "main", "master").
	DefaultBranch() string

	// SetDefaultBranchOverride overrides the default branch detection.
	SetDefaultBranchOverride(branch string)

	// GetDefaultBranchOverride returns the current default branch override, if any.
	GetDefaultBranchOverride() string

	// MergeBase returns the merge-base commit between HEAD and the given ref.
	MergeBase(ref string) (string, error)

	// ChangedFilesOnDefaultInDir returns changed files when on the default branch.
	ChangedFilesOnDefaultInDir(dir string) ([]FileChange, error)

	// ChangedFilesFromBaseInDir returns files changed between baseRef and the working tree.
	ChangedFilesFromBaseInDir(baseRef, dir string) ([]FileChange, error)

	// ChangedFilesScoped returns changed files for a specific scope (branch, staged, unstaged).
	ChangedFilesScoped(scope, baseRef string) ([]FileChange, error)

	// ChangedFilesForCommit returns the files changed in a single commit.
	ChangedFilesForCommit(sha, dir string) ([]FileChange, error)

	// FileDiffUnified returns parsed diff hunks for a file against a base ref.
	FileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error)

	// FileDiffUnifiedCtx is like FileDiffUnified but accepts a context for timeout control.
	FileDiffUnifiedCtx(ctx context.Context, path, baseRef, dir string) ([]DiffHunk, error)

	// FileDiffScoped returns diff hunks for a file using a scope-appropriate diff command.
	FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error)

	// FileDiffForCommit returns diff hunks for a file in a single commit.
	FileDiffForCommit(path, sha, dir string) ([]DiffHunk, error)

	// FileDiffUnifiedNewFile returns diff hunks showing an entire file as added.
	FileDiffUnifiedNewFile(path string) ([]DiffHunk, error)

	// CommitLog returns the commits between baseRef and HEAD.
	CommitLog(baseRef, dir string) ([]CommitInfo, error)

	// WorkingTreeFingerprint returns a string representing the current working tree state.
	WorkingTreeFingerprint() string

	// UntrackedFiles returns untracked files, running from the specified directory.
	UntrackedFiles(dir string) ([]FileChange, error)

	// AllTrackedFiles returns all tracked files plus untracked non-ignored files.
	AllTrackedFiles(dir string) ([]string, error)

	// RemoteBranches returns the names of all remote branches.
	RemoteBranches(dir string) ([]string, error)

	// DiffNumstat returns per-file addition/deletion counts.
	DiffNumstat(baseRef, dir string) (map[string]NumstatEntry, error)

	// UserName returns the VCS-configured user name.
	UserName() string

	// FileStatusInRepo returns the status of a single file relative to baseRef.
	FileStatusInRepo(path, baseRef, dir string) string

	// HasStagingArea returns true if the VCS has a staging area (e.g. git index).
	HasStagingArea() bool

	// SkipDirNames returns directory names that should be skipped during walks
	// (e.g. ".git", ".sl").
	SkipDirNames() []string
}

// DetectVCS returns the appropriate VCS backend for the current directory.
// If vcsOverride is set ("git", "sl", or "sapling"), that backend is used directly.
// Otherwise, auto-detection checks for .sl/ first (Sapling on git repos has both),
// then falls back to git. Returns nil if no VCS is detected.
func DetectVCS(vcsOverride string) VCS {
	switch vcsOverride {
	case "git":
		return &GitVCS{}
	case "sl", "sapling":
		// TODO: return &SaplingVCS{} once implemented
		return nil
	}

	// Auto-detect: check for .sl/ first since Sapling repos on top of git have both.
	if hasSLDir() {
		// TODO: return &SaplingVCS{} once implemented
		return nil
	}

	if IsGitRepo() {
		return &GitVCS{}
	}

	return nil
}

// hasSLDir checks whether a .sl/ directory exists at or above the current directory.
func hasSLDir() bool {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".sl")); err == nil && info.IsDir() {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}
