package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// SaplingVCS implements VCS for Sapling SCM repositories.
type SaplingVCS struct {
	defaultBranchOnce sync.Once
	defaultBranch     string
	defaultBranchMu   sync.RWMutex // protects defaultBranch when set via override
}

func (s *SaplingVCS) Name() string { return "sl" }

// RepoRoot returns the absolute path to the Sapling repository root.
func (s *SaplingVCS) RepoRoot() (string, error) {
	out, err := exec.Command("sl", "root").Output()
	if err != nil {
		return "", fmt.Errorf("sl root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the active bookmark or short node hash.
func (s *SaplingVCS) CurrentBranch() string {
	out, err := exec.Command("sl", "log", "-r", ".", "-T", "{activebookmark}").Output()
	if err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return b
		}
	}
	out, err = exec.Command("sl", "log", "-r", ".", "-T", "{node|short}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DefaultBranch returns the cached default branch, detecting it on first call.
// If an override is set, it is returned immediately without caching.
func (s *SaplingVCS) DefaultBranch() string {
	s.defaultBranchMu.RLock()
	override := s.defaultBranch
	s.defaultBranchMu.RUnlock()
	if override != "" {
		return override
	}
	s.defaultBranchOnce.Do(func() {
		s.defaultBranchMu.Lock()
		// Double-check: an override may have been set between RUnlock and Do.
		if s.defaultBranch == "" {
			s.defaultBranch = detectSaplingDefaultBranch()
		}
		s.defaultBranchMu.Unlock()
	})
	s.defaultBranchMu.RLock()
	defer s.defaultBranchMu.RUnlock()
	return s.defaultBranch
}

// SetDefaultBranchOverride overrides the default branch detection.
func (s *SaplingVCS) SetDefaultBranchOverride(branch string) {
	s.defaultBranchMu.Lock()
	s.defaultBranch = branch
	s.defaultBranchMu.Unlock()
}

// GetDefaultBranchOverride returns the current default branch override, if any.
func (s *SaplingVCS) GetDefaultBranchOverride() string {
	s.defaultBranchMu.RLock()
	defer s.defaultBranchMu.RUnlock()
	return s.defaultBranch
}

// MergeBase returns the common ancestor between the working copy and ref.
func (s *SaplingVCS) MergeBase(ref string) (string, error) {
	revset := fmt.Sprintf("ancestor(., %s)", ref)
	out, err := exec.Command("sl", "log", "-r", revset, "-T", "{node}").Output()
	if err != nil {
		return "", fmt.Errorf("sl ancestor: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ChangedFilesOnDefaultInDir returns changed files when on the default branch.
func (s *SaplingVCS) ChangedFilesOnDefaultInDir(dir string) ([]FileChange, error) {
	out, err := slCommandInDir(dir, "status")
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(out), nil
}

// ChangedFilesFromBaseInDir returns files changed between baseRef and the working copy.
func (s *SaplingVCS) ChangedFilesFromBaseInDir(baseRef, dir string) ([]FileChange, error) {
	out, err := slCommandInDir(dir, "status", "--rev", baseRef)
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(out), nil
}

// ChangedFilesScoped returns changed files for a scope. Sapling has no staging area,
// so "staged" and "unstaged" return nil.
func (s *SaplingVCS) ChangedFilesScoped(scope, baseRef string) ([]FileChange, error) {
	if scope == "branch" {
		return s.ChangedFilesFromBaseInDir(baseRef, "")
	}
	// Sapling has no staging area.
	return nil, nil
}

// ChangedFilesForCommit returns the files changed in a single commit.
func (s *SaplingVCS) ChangedFilesForCommit(sha, dir string) ([]FileChange, error) {
	out, err := slCommandInDir(dir, "status", "--change", sha)
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(out), nil
}

// FileDiffUnified returns parsed diff hunks for a file against a base ref.
func (s *SaplingVCS) FileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error) {
	return s.FileDiffUnifiedCtx(context.Background(), path, baseRef, dir)
}

// FileDiffUnifiedCtx is like FileDiffUnified but accepts a context for cancellation.
func (s *SaplingVCS) FileDiffUnifiedCtx(ctx context.Context, path, baseRef, dir string) ([]DiffHunk, error) {
	args := buildDiffArgs(baseRef, path)
	cmd := exec.CommandContext(ctx, "sl", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		// sl diff exits 1 when there is a diff; check for actual errors.
		if len(out) > 0 {
			return ParseUnifiedDiff(string(out)), nil
		}
		return nil, nil
	}
	return ParseUnifiedDiff(string(out)), nil
}

// FileDiffScoped returns diff hunks for a file using a scope-appropriate diff.
// Sapling has no staging area, so "staged" and "unstaged" return nil.
func (s *SaplingVCS) FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error) {
	if scope == "branch" {
		return s.FileDiffUnified(path, baseRef, dir)
	}
	return nil, nil
}

// FileDiffForCommit returns diff hunks for a file in a single commit.
// Uses --change which handles initial commits (no parent) correctly.
func (s *SaplingVCS) FileDiffForCommit(path, sha, dir string) ([]DiffHunk, error) {
	cmd := exec.Command("sl", "diff", "--change", sha, path)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		if len(out) > 0 {
			return ParseUnifiedDiff(string(out)), nil
		}
		return nil, nil
	}
	return ParseUnifiedDiff(string(out)), nil
}

// FileDiffUnifiedNewFile returns diff hunks showing an entire file as added.
func (s *SaplingVCS) FileDiffUnifiedNewFile(path string) ([]DiffHunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FileDiffUnifiedNewFile(string(data)), nil
}

// CommitLog returns the commits between baseRef and HEAD.
func (s *SaplingVCS) CommitLog(baseRef, dir string) ([]CommitInfo, error) {
	if baseRef == "" {
		return nil, nil
	}
	revset := fmt.Sprintf("%s::. - %s", baseRef, baseRef)
	// The \\n in Go string literals produce literal \n characters, which Sapling's
	// template engine interprets as newlines (field separators in the output).
	tpl := "{node}\\n{node|short}\\n{desc|firstline}\\n{author|user}\\n{date|isodate}\\n---\\n"
	args := []string{"log", "-r", revset, "-T", tpl}
	out, err := slCommandInDir(dir, args...)
	if err != nil {
		return nil, err
	}
	return parseSaplingCommitLog(out), nil
}

// WorkingTreeFingerprint returns a string representing the current working tree state.
// Note: `sl status` output may vary with locale settings on some systems.
// This is acceptable for change-detection (comparing consecutive calls) but
// should not be used as a stable hash key.
func (s *SaplingVCS) WorkingTreeFingerprint() string {
	out, err := exec.Command("sl", "status").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// UntrackedFiles returns untracked files in the given directory.
func (s *SaplingVCS) UntrackedFiles(dir string) ([]FileChange, error) {
	out, err := slCommandInDir(dir, "status", "-u")
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(out), nil
}

// AllTrackedFiles returns all tracked files plus untracked non-ignored files.
func (s *SaplingVCS) AllTrackedFiles(dir string) ([]string, error) {
	trackedOut, err := slCommandInDir(dir, "files")
	if err != nil {
		return nil, err
	}
	files := splitNonEmpty(trackedOut)

	untrackedOut, err := slCommandInDir(dir, "status", "-u")
	if err != nil {
		return files, nil //nolint:nilerr // graceful: return tracked files even if untracked listing fails
	}
	for _, fc := range parseSaplingStatus(untrackedOut) {
		files = append(files, fc.Path)
	}
	return files, nil
}

// RemoteBranches returns remote bookmark names. Returns nil on error
// since remote bookmarks may not be available.
func (s *SaplingVCS) RemoteBranches(dir string) ([]string, error) {
	out, err := slCommandInDir(dir, "bookmark", "--list", "--remote")
	if err != nil {
		return nil, nil //nolint:nilerr // graceful: remote bookmarks may not be available
	}
	return parseRemoteBookmarks(out), nil
}

// DiffNumstat returns per-file addition/deletion counts.
func (s *SaplingVCS) DiffNumstat(baseRef, dir string) (map[string]NumstatEntry, error) {
	if baseRef == "" {
		return nil, nil
	}
	out, err := slCommandInDir(dir, "diff", "--stat", "-r", baseRef)
	if err != nil {
		return nil, err
	}
	return parseSaplingDiffStat(out), nil
}

// UserName returns the Sapling-configured user name.
func (s *SaplingVCS) UserName() string {
	out, err := exec.Command("sl", "config", "ui.username").Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	// Strip email suffix: "Name <email>" -> "Name"
	if idx := strings.Index(name, " <"); idx >= 0 {
		name = name[:idx]
	}
	return name
}

// FileContentAtRef returns the content of a file at the given Sapling revision.
func (s *SaplingVCS) FileContentAtRef(path, ref, dir string) (string, error) {
	if ref == "" {
		return "", nil
	}
	cmd := exec.Command("sl", "cat", "-r", ref, path)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sl cat -r %s %s: %w", ref, path, err)
	}
	return string(out), nil
}

// FileStatusInRepo returns the status of a single file relative to baseRef.
func (s *SaplingVCS) FileStatusInRepo(path, baseRef, dir string) string {
	if baseRef == "" {
		return "modified"
	}
	out, err := slCommandInDir(dir, "status", "--rev", baseRef, path)
	if err != nil {
		return ""
	}
	changes := parseSaplingStatus(out)
	if len(changes) == 0 {
		return ""
	}
	return changes[0].Status
}

// HasStagingArea returns false because Sapling has no staging area.
func (s *SaplingVCS) HasStagingArea() bool { return false }

// SkipDirNames returns directory names to skip during walks.
// Includes .git because Sapling often operates on git-backed repos.
func (s *SaplingVCS) SkipDirNames() []string { return []string{".sl", ".git"} }

// detectSaplingDefaultBranch probes for "main" then "master" bookmarks.
func detectSaplingDefaultBranch() string {
	for _, branch := range []string{"main", "master"} {
		err := exec.Command("sl", "log", "-r", branch, "-T", "{node|short}").Run()
		if err == nil {
			return branch
		}
	}
	return "main"
}

// slCommandInDir runs an sl subcommand in the given directory and returns stdout.
func slCommandInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("sl", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sl %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// buildDiffArgs constructs arguments for sl diff.
func buildDiffArgs(baseRef, path string) []string {
	args := []string{"diff"}
	if baseRef != "" {
		args = append(args, "-r", baseRef)
	}
	args = append(args, path)
	return args
}

// splitNonEmpty splits output by newline and returns non-empty lines.
func splitNonEmpty(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	var result []string
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// parseRemoteBookmarks extracts bookmark names from `sl bookmark --list --remote` output.
func parseRemoteBookmarks(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// parseSaplingCommitLog parses the templated output from sl log into CommitInfo slices.
// The template produces blocks separated by "---\n", each containing:
// node, short_node, first_line_of_description, author, isodate.
func parseSaplingCommitLog(output string) []CommitInfo {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	blocks := strings.Split(trimmed, "---")
	var commits []CommitInfo
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.SplitN(block, "\n", 5)
		if len(lines) < 5 {
			continue
		}
		commits = append(commits, CommitInfo{
			SHA:      strings.TrimSpace(lines[0]),
			ShortSHA: strings.TrimSpace(lines[1]),
			Message:  strings.TrimSpace(lines[2]),
			Author:   strings.TrimSpace(lines[3]),
			Date:     strings.TrimSpace(lines[4]),
		})
	}
	return commits
}
