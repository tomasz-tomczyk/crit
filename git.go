package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// FileChange represents a single file change detected by git.
type FileChange struct {
	Path   string // relative to repo root
	Status string // "added", "modified", "deleted", "renamed"
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
	defaultBranchOnce   sync.Once
	defaultBranchResult string
)

// DefaultBranch returns the name of the default branch (main or master).
// The result is cached after the first call since it doesn't change during a session.
func DefaultBranch() string {
	defaultBranchOnce.Do(func() {
		defaultBranchResult = detectDefaultBranch()
	})
	return defaultBranchResult
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

func changedFilesOnDefault() ([]FileChange, error) {
	// Staged + unstaged changes vs HEAD
	cmd := exec.Command("git", "diff", "HEAD", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		// If there's no HEAD (empty repo), try diff --cached + working tree
		cmd = exec.Command("git", "diff", "--name-status")
		out, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git diff failed: %w", err)
		}
	}

	changes := parseNameStatus(string(out))

	// Add untracked files
	untracked, err := untrackedFiles()
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

	// All changes from merge base to working tree
	cmd := exec.Command("git", "diff", mergeBase, "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	changes := parseNameStatus(string(out))

	// Add untracked files
	untracked, err := untrackedFiles()
	if err != nil {
		return nil, err
	}
	changes = append(changes, untracked...)

	return dedup(changes), nil
}

func untrackedFiles() ([]FileChange, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ls-files failed: %w", err)
	}
	var changes []FileChange
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		changes = append(changes, FileChange{Path: line, Status: "added"})
	}
	return changes, nil
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
	var cmd *exec.Cmd
	if baseRef == "" {
		cmd = exec.Command("git", "diff", "HEAD", "--", path)
	} else {
		cmd = exec.Command("git", "diff", baseRef, "--", path)
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

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// ParseUnifiedDiff parses a unified diff string into hunks.
func ParseUnifiedDiff(diff string) []DiffHunk {
	var hunks []DiffHunk
	lines := strings.Split(diff, "\n")

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
