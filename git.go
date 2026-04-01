package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// FileChange represents a single file change detected by git.
type FileChange struct {
	Path   string // relative to repo root
	Status string // "added", "modified", "deleted", "renamed", "untracked"
}

// DiffHunk represents a single hunk in a unified diff.
type DiffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string // the @@ line text
	Lines    []DiffLine
}

// DiffLine represents a single line within a diff hunk.
type DiffLine struct {
	Type    string // "context", "add", "del"
	Content string
	OldNum  int // 0 if add
	NewNum  int // 0 if del
}

// IsGitRepo returns true if the current directory is inside a git repository.
func IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// RepoRoot returns the absolute path to the git repository root.
func RepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

var (
	defaultBranchOnce     sync.Once
	defaultBranchResult   string
	defaultBranchOverride string
	defaultBranchMu       sync.RWMutex // protects defaultBranchOverride
)

// DefaultBranch returns the name of the default branch (main or master).
// The result is cached after the first call since it doesn't change during a session.
// If defaultBranchOverride is set, it is returned immediately without caching.
func DefaultBranch() string {
	defaultBranchMu.RLock()
	override := defaultBranchOverride
	defaultBranchMu.RUnlock()
	if override != "" {
		return override
	}
	defaultBranchOnce.Do(func() {
		defaultBranchResult = detectDefaultBranch()
	})
	return defaultBranchResult
}

// setDefaultBranchOverride safely updates the default branch override.
func setDefaultBranchOverride(branch string) {
	defaultBranchMu.Lock()
	defaultBranchOverride = branch
	defaultBranchMu.Unlock()
}

// getDefaultBranchOverride safely reads the default branch override.
func getDefaultBranchOverride() string {
	defaultBranchMu.RLock()
	defer defaultBranchMu.RUnlock()
	return defaultBranchOverride
}

func detectDefaultBranch() string {
	// Try remote HEAD first
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		// refs/remotes/origin/main -> main
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Fallback: check if main exists
	if err := exec.Command("git", "rev-parse", "--verify", "main").Run(); err == nil {
		return "main"
	}
	// Fallback: check if master exists
	if err := exec.Command("git", "rev-parse", "--verify", "master").Run(); err == nil {
		return "master"
	}
	return "main"
}

// RemoteBranches returns the names of all remote branches (without the "origin/" prefix).
// The result excludes HEAD. If dir is non-empty, git runs in that directory.
func RemoteBranches(dir string) ([]string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("for-each-ref failed: %w", err)
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimPrefix(line, "origin/")
		if name == "" || name == "HEAD" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// CurrentBranch returns the name of the current branch.
func CurrentBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsOnDefaultBranch returns true if HEAD is on the default branch.
func IsOnDefaultBranch() bool {
	return CurrentBranch() == DefaultBranch()
}

// MergeBase returns the merge base commit between HEAD and the given base ref.
func MergeBase(base string) (string, error) {
	cmd := exec.Command("git", "merge-base", "HEAD", base)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("merge-base failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ChangedFiles returns the list of files changed in the current working state.
// On the default branch: staged + unstaged + untracked files.
// On a feature branch: all changes since the merge base with the default branch + untracked.
func ChangedFiles() ([]FileChange, error) {
	if IsOnDefaultBranch() {
		return changedFilesOnDefault()
	}
	return changedFilesOnFeature()
}

// ChangedFilesScoped returns changed files for a specific scope.
// Supported scopes: "branch", "staged", "unstaged". Any other value falls back to ChangedFiles.
func ChangedFilesScoped(scope, baseRef string) ([]FileChange, error) {
	switch scope {
	case "branch":
		return changedFilesBranch(baseRef)
	case "staged":
		return changedFilesStaged()
	case "unstaged":
		return changedFilesUnstaged()
	default:
		return ChangedFiles()
	}
}

// changedFilesStaged returns only staged (cached) changes.
func changedFilesStaged() ([]FileChange, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --cached failed: %w", err)
	}
	return parseNameStatus(string(out)), nil
}

// changedFilesUnstaged returns unstaged modifications plus untracked files.
func changedFilesUnstaged() ([]FileChange, error) {
	cmd := exec.Command("git", "diff", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	changes := parseNameStatus(string(out))

	untracked, err := untrackedFiles()
	if err != nil {
		return nil, err
	}
	changes = append(changes, untracked...)

	return dedup(changes), nil
}

// changedFilesBranch returns files changed between baseRef and HEAD.
// Returns nil if baseRef is empty.
func changedFilesBranch(baseRef string) ([]FileChange, error) {
	if baseRef == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "diff", baseRef+"..HEAD", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s..HEAD failed: %w", baseRef, err)
	}
	return parseNameStatus(string(out)), nil
}

// FileDiffScoped returns parsed diff hunks for a file using a scope-appropriate git diff command.
// Supported scopes: "branch", "staged", "unstaged". Any other value delegates to FileDiffUnified.
// The dir parameter sets the working directory for git commands (use repo root for correct path resolution).
func FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	switch scope {
	case "branch":
		if baseRef == "" {
			return nil, nil
		}
		cmd = exec.Command("git", "diff", "--no-color", baseRef+"..HEAD", "--", path)
	case "staged":
		cmd = exec.Command("git", "diff", "--no-color", "--cached", "--", path)
	case "unstaged":
		cmd = exec.Command("git", "diff", "--no-color", "--", path)
	default:
		return fileDiffUnified(path, baseRef, dir)
	}
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means diff found changes (normal), check for actual errors
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// git diff exits 1 when there are differences
		} else {
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}

// CommitInfo represents a single commit in a log.
type CommitInfo struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"short_sha"`
	Message  string `json:"message"`
	Author   string `json:"author"`
	Date     string `json:"date"`
}

// CommitLog returns the commits between baseRef and HEAD, newest first.
// Returns nil if baseRef is empty.
// The dir parameter sets the working directory for the git command.
func CommitLog(baseRef, dir string) ([]CommitInfo, error) {
	if baseRef == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "log", "--format=%H%n%h%n%s%n%an%n%aI", baseRef+"..HEAD")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}
	lines := strings.Split(output, "\n")
	if len(lines)%5 != 0 {
		return nil, fmt.Errorf("unexpected git log output: %d lines (not a multiple of 5)", len(lines))
	}
	var commits []CommitInfo
	for i := 0; i < len(lines); i += 5 {
		commits = append(commits, CommitInfo{
			SHA:      lines[i],
			ShortSHA: lines[i+1],
			Message:  lines[i+2],
			Author:   lines[i+3],
			Date:     lines[i+4],
		})
	}
	return commits, nil
}

// ChangedFilesForCommit returns the files changed in a single commit.
// The dir parameter sets the working directory for the git command.
func ChangedFilesForCommit(sha, dir string) ([]FileChange, error) {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "-r", "--name-status", sha)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree failed: %w", err)
	}
	return parseNameStatus(string(out)), nil
}

// FileDiffForCommit returns parsed diff hunks for a file in a single commit.
// The dir parameter sets the working directory for the git command.
// For the initial (root) commit, sha^ is undefined so we diff against the empty tree.
func FileDiffForCommit(path, sha, dir string) ([]DiffHunk, error) {
	cmd := exec.Command("git", "diff", "--no-color", sha+"^.."+sha, "--", path)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		switch {
		case ok && exitErr.ExitCode() == 1:
			// git diff exits 1 when there are differences — not an error
		case ok && exitErr.ExitCode() == 128:
			// sha^ failed (root commit) — diff against the empty tree
			emptyTree := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
			cmd2 := exec.Command("git", "diff", "--no-color", emptyTree+".."+sha, "--", path)
			if dir != "" {
				cmd2.Dir = dir
			}
			out, err = cmd2.Output()
			if err != nil {
				if exitErr2, ok := err.(*exec.ExitError); ok && exitErr2.ExitCode() == 1 {
					// differences found
				} else {
					return nil, fmt.Errorf("git diff (root commit) failed: %w", err)
				}
			}
		default:
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}

func changedFilesOnDefault() ([]FileChange, error) {
	return changedFilesOnDefaultInDir("")
}

func changedFilesOnDefaultInDir(dir string) ([]FileChange, error) {
	// Staged + unstaged changes vs HEAD
	cmd := exec.Command("git", "diff", "HEAD", "--name-status")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		// If there's no HEAD (empty repo), try diff --cached + working tree
		cmd = exec.Command("git", "diff", "--name-status")
		if dir != "" {
			cmd.Dir = dir
		}
		out, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}

	changes := parseNameStatus(string(out))

	// Add untracked files
	untracked, err := untrackedFilesInDir(dir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, untracked...)

	return dedup(changes), nil
}

func changedFilesOnFeature() ([]FileChange, error) {
	defaultBranch := DefaultBranch()
	mergeBase, err := MergeBase(defaultBranch)
	if err != nil {
		// Fallback to HEAD diff if merge-base fails
		return changedFilesOnDefault()
	}

	return changedFilesFromBase(mergeBase)
}

// changedFilesFromBase returns files changed between a base ref and the working tree, plus untracked files.
func changedFilesFromBase(baseRef string) ([]FileChange, error) {
	return changedFilesFromBaseInDir(baseRef, "")
}

// changedFilesFromBaseInDir is like changedFilesFromBase but runs git from the specified directory.
func changedFilesFromBaseInDir(baseRef, dir string) ([]FileChange, error) {
	// All changes from base ref to working tree
	cmd := exec.Command("git", "diff", baseRef, "--name-status")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	changes := parseNameStatus(string(out))

	// Add untracked files
	untracked, err := untrackedFilesInDir(dir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, untracked...)

	return dedup(changes), nil
}

func untrackedFiles() ([]FileChange, error) {
	return untrackedFilesInDir("")
}

// untrackedFilesInDir returns untracked files, running from the specified directory.
// git ls-files returns paths relative to cwd, so dir should be the repo root
// to get repo-root-relative paths.
func untrackedFilesInDir(dir string) ([]FileChange, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ls-files failed: %w", err)
	}
	var changes []FileChange
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		changes = append(changes, FileChange{Path: line, Status: "untracked"})
	}
	return changes, nil
}

// AllTrackedFiles returns all tracked files plus untracked non-ignored files.
// Paths are relative to the repo root. dir should be the repo root.
func AllTrackedFiles(dir string) ([]string, error) {
	// Tracked files
	cmd := exec.Command("git", "ls-files")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !seen[line] {
			seen[line] = true
			files = append(files, line)
		}
	}

	// Untracked but not gitignored
	cmd2 := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	if dir != "" {
		cmd2.Dir = dir
	}
	out2, err := cmd2.Output()
	if err != nil {
		return files, nil // non-fatal: return tracked only
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out2)), "\n") {
		if line == "" {
			continue
		}
		if !seen[line] {
			seen[line] = true
			files = append(files, line)
		}
	}

	return files, nil
}

// skipDirs is the shared set of directory names to skip during recursive walks.
// Used by both WalkFiles (git.go) and walkDirectory (session.go) to stay in sync.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"_build":       true,
	"deps":         true,
}

// WalkFiles returns all files under root, skipping hidden directories,
// node_modules, and other common non-project directories.
// Paths are relative to root.
func WalkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

func parseNameStatus(output string) []FileChange {
	var changes []FileChange
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		// For renames (R100\told\tnew), use the new path
		if strings.HasPrefix(status, "R") && len(parts) >= 3 {
			path = parts[2]
			changes = append(changes, FileChange{Path: path, Status: "renamed"})
			continue
		}
		switch status {
		case "A":
			changes = append(changes, FileChange{Path: path, Status: "added"})
		case "M":
			changes = append(changes, FileChange{Path: path, Status: "modified"})
		case "D":
			changes = append(changes, FileChange{Path: path, Status: "deleted"})
		default:
			changes = append(changes, FileChange{Path: path, Status: "modified"})
		}
	}
	return changes
}

// dedup removes duplicate paths, keeping the first occurrence.
func dedup(changes []FileChange) []FileChange {
	seen := map[string]bool{}
	var result []FileChange
	for _, c := range changes {
		if !seen[c.Path] {
			seen[c.Path] = true
			result = append(result, c)
		}
	}
	return result
}

// FileDiffUnified returns the parsed diff hunks for a file against a base ref.
// If baseRef is empty, diffs against HEAD.
func FileDiffUnified(path, baseRef string) ([]DiffHunk, error) {
	return fileDiffUnified(path, baseRef, "")
}

// fileDiffUnified is the internal implementation that accepts an optional working directory.
func fileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	if baseRef == "" {
		cmd = exec.Command("git", "diff", "--no-color", "HEAD", "--", path)
	} else {
		cmd = exec.Command("git", "diff", "--no-color", baseRef, "--", path)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means diff found changes (normal), check for actual errors
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// git diff exits 1 when there are differences
		} else {
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}

// fileDiffUnifiedCtx is like fileDiffUnified but accepts a context for timeout control.
func fileDiffUnifiedCtx(ctx context.Context, path, baseRef, dir string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	if baseRef == "" {
		cmd = exec.CommandContext(ctx, "git", "diff", "--no-color", "HEAD", "--", path)
	} else {
		cmd = exec.CommandContext(ctx, "git", "diff", "--no-color", baseRef, "--", path)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// git diff exits 1 when there are differences
		} else {
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}

// FileDiffUnifiedNewFile returns parsed diff hunks showing the entire file as added.
// Used for untracked files that don't have a git diff.
func FileDiffUnifiedNewFile(content string) []DiffHunk {
	lines := strings.Split(content, "\n")
	// Remove trailing empty line from split
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	hunk := DiffHunk{
		OldStart: 0,
		OldCount: 0,
		NewStart: 1,
		NewCount: len(lines),
		Header:   fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)),
	}
	for i, line := range lines {
		hunk.Lines = append(hunk.Lines, DiffLine{
			Type:    "add",
			Content: line,
			OldNum:  0,
			NewNum:  i + 1,
		})
	}
	return []DiffHunk{hunk}
}

// NumstatEntry holds per-file addition/deletion counts from git diff --numstat.
type NumstatEntry struct {
	Additions int
	Deletions int
}

// DiffNumstat runs git diff --numstat against the given base ref and returns per-file stats.
func DiffNumstat(baseRef string) (map[string]NumstatEntry, error) {
	return DiffNumstatDir(baseRef, "")
}

// DiffNumstatDir is like DiffNumstat but runs in a specific directory.
func DiffNumstatDir(baseRef, dir string) (map[string]NumstatEntry, error) {
	cmd := exec.Command("git", "diff", "--numstat", baseRef)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// git diff exits 1 when there are differences — normal
		} else {
			return nil, fmt.Errorf("git diff --numstat failed: %w", err)
		}
	}

	stats := make(map[string]NumstatEntry)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		adds, err1 := strconv.Atoi(parts[0])
		dels, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			adds, dels = 0, 0
		}
		stats[path] = NumstatEntry{Additions: adds, Deletions: dels}
	}
	return stats, nil
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// ParseUnifiedDiff parses a unified diff string into hunks.
func ParseUnifiedDiff(diff string) []DiffHunk {
	var hunks []DiffHunk
	// TrimRight removes the trailing newline so strings.Split doesn't produce
	// a spurious empty element that could be confused with a blank context line.
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")

	var current *DiffHunk
	oldLine, newLine := 0, 0

	for _, line := range lines {
		if m := hunkHeaderRe.FindStringSubmatch(line); m != nil {
			if current != nil {
				hunks = append(hunks, *current)
			}
			oldStart, _ := strconv.Atoi(m[1])
			oldCount := 1
			if m[2] != "" {
				oldCount, _ = strconv.Atoi(m[2])
			}
			newStart, _ := strconv.Atoi(m[3])
			newCount := 1
			if m[4] != "" {
				newCount, _ = strconv.Atoi(m[4])
			}
			current = &DiffHunk{
				OldStart: oldStart,
				OldCount: oldCount,
				NewStart: newStart,
				NewCount: newCount,
				Header:   line,
			}
			oldLine = oldStart
			newLine = newStart
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "+") {
			current.Lines = append(current.Lines, DiffLine{
				Type:    "add",
				Content: strings.TrimPrefix(line, "+"),
				NewNum:  newLine,
			})
			newLine++
		} else if strings.HasPrefix(line, "-") {
			current.Lines = append(current.Lines, DiffLine{
				Type:    "del",
				Content: strings.TrimPrefix(line, "-"),
				OldNum:  oldLine,
			})
			oldLine++
		} else if strings.HasPrefix(line, " ") {
			current.Lines = append(current.Lines, DiffLine{
				Type:    "context",
				Content: strings.TrimPrefix(line, " "),
				OldNum:  oldLine,
				NewNum:  newLine,
			})
			oldLine++
			newLine++
		} else if line == "" && oldLine < current.OldStart+current.OldCount {
			// Bare empty line within expected hunk bounds — treat as blank context line.
			// Git outputs these when diff.suppressBlankEmpty is set, stripping the
			// leading space from blank context lines. We check bounds to avoid treating
			// the trailing empty string from strings.Split as a spurious context line.
			current.Lines = append(current.Lines, DiffLine{
				Type:    "context",
				Content: "",
				OldNum:  oldLine,
				NewNum:  newLine,
			})
			oldLine++
			newLine++
		} else if line == `\ No newline at end of file` {
			// Skip this marker
			continue
		}
	}

	if current != nil {
		hunks = append(hunks, *current)
	}
	return hunks
}

// WorkingTreeFingerprint returns a string representing the current working tree state.
// Compare consecutive calls to detect changes.
func WorkingTreeFingerprint() string {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
