package main

import (
	"bytes"
	"context"
	"errors"
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

// stripExternalDiffEnv returns a copy of the current process environment with
// any external-diff variables removed. Used together with `-c diff.external=`
// to guarantee diff output is the standard unified format regardless of how
// the user has configured external diff tools (e.g. difftastic). Issue #380
// surfaced cases where `--no-ext-diff` alone wasn't enough.
func stripExternalDiffEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_EXTERNAL_DIFF=") || strings.HasPrefix(e, "GIT_DIFF_OPTS=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// runGit runs `git args...` in dir with hardened env (GIT_TERMINAL_PROMPT=0
// so a misconfigured credential helper can't hang the daemon waiting for tty
// input) and an optional context for cancellation. Returns stdout bytes; on
// error, the returned error includes captured stderr.
//
// This is a partial migration target: most call sites in this file still use
// exec.Command directly. New code, and any site that benefits from
// cancellation, should use runGit. Convert opportunistically.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Append rather than replace os.Environ so existing GIT_* config (auth,
	// SSH agent, etc.) is preserved. GIT_TERMINAL_PROMPT=0 prevents git from
	// blocking on a credential prompt when run from a daemon with no tty.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrTrim := strings.TrimSpace(stderr.String())
		if stderrTrim != "" {
			return stdout.Bytes(), fmt.Errorf("git %s: %w: %s", args[0], err, stderrTrim)
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

// IsGitRepo returns true if the current directory is inside a git repository.
func IsGitRepo() bool {
	out, err := runGit(context.Background(), "", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// RepoRoot returns the absolute path to the git repository root.
func RepoRoot() (string, error) {
	out, err := runGit(context.Background(), "", "rev-parse", "--show-toplevel")
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

// defaultBaseRef returns the best ref to use as the diff base for the
// auto-detected default branch. Prefers `origin/<defaultBranch>` when that
// remote-tracking ref exists locally, otherwise falls back to the bare local
// branch name. The remote ref is preferred because the local default branch
// is often stale relative to upstream — leading to misleading diffs that
// include commits already merged on the remote.
//
// If the user has set a base-branch override (via config or CLI), this
// function honors it as-is, since the user has explicitly chosen a ref.
func defaultBaseRef() string {
	branch := DefaultBranch()
	if getDefaultBranchOverride() != "" {
		return branch
	}
	remote := "origin/" + branch
	if exec.Command("git", "rev-parse", "--verify", "refs/remotes/"+remote).Run() == nil {
		return remote
	}
	return branch
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

// isOnDefaultBranch returns true if HEAD is on the default branch.
func isOnDefaultBranch() bool {
	return CurrentBranch() == DefaultBranch()
}

// MergeBase returns the merge base commit between HEAD and the given base ref.
// Falls back to origin/<base> when the local ref is missing — common in
// worktrees off a bare repo where only the remote-tracking ref exists.
func MergeBase(base string) (string, error) {
	out, err := exec.Command("git", "merge-base", "HEAD", base).Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if !strings.HasPrefix(base, "origin/") {
		fallback, fbErr := exec.Command("git", "merge-base", "HEAD", "origin/"+base).Output()
		if fbErr == nil {
			return strings.TrimSpace(string(fallback)), nil
		}
	}
	return "", fmt.Errorf("merge-base failed: %w", err)
}

// fileContentAtRef returns the content of a file at the given git ref.
// Returns empty string on any error (file doesn't exist at ref, not a git repo, etc.).
func fileContentAtRef(path, ref, dir string) string {
	if ref == "" {
		return ""
	}
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// ChangedFiles returns the list of files changed in the current working state.
// On the default branch: staged + unstaged + untracked files.
// On a feature branch: all changes since the merge base with the default branch + untracked.
func ChangedFiles() ([]FileChange, error) {
	if isOnDefaultBranch() {
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
	out, err := runGit(context.Background(), "", "diff", "--cached", "--name-status")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached failed: %w", err)
	}
	return parseNameStatus(string(out)), nil
}

// changedFilesUnstaged returns unstaged modifications plus untracked files.
func changedFilesUnstaged() ([]FileChange, error) {
	out, err := runGit(context.Background(), "", "diff", "--name-status")
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
	out, err := runGit(context.Background(), "", "diff", baseRef+"..HEAD", "--name-status")
	if err != nil {
		return nil, fmt.Errorf("git diff %s..HEAD failed: %w", baseRef, err)
	}
	return parseNameStatus(string(out)), nil
}

// FileDiffScoped returns parsed diff hunks for a file using a scope-appropriate git diff command.
// Supported scopes: "branch", "staged", "unstaged". Any other value delegates to fileDiffUnified.
// The dir parameter sets the working directory for git commands (use repo root for correct path resolution).
func FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	switch scope {
	case "branch":
		if baseRef == "" {
			return nil, nil
		}
		cmd = exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", baseRef+"..HEAD", "--", path)
	case "staged":
		cmd = exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", "--cached", "--", path)
	case "unstaged":
		cmd = exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", "--", path)
	default:
		return fileDiffUnified(path, baseRef, dir)
	}
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means diff found changes (normal), check for actual errors
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
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

// CommitLog returns the commits between baseRef and headRef, newest first.
// Returns nil if baseRef is empty. When headRef is empty, git's HEAD ref is used
// (working-tree behavior). Pass an explicit SHA to scope the upper bound — e.g.
// in range mode where HEAD may point past the focus.
// The dir parameter sets the working directory for the git command.
func CommitLog(baseRef, headRef, dir string) ([]CommitInfo, error) {
	if baseRef == "" {
		return nil, nil
	}
	if headRef == "" {
		headRef = "HEAD"
	}
	cmd := exec.Command("git", "log", "--format=%H%n%h%n%s%n%an%n%aI", baseRef+".."+headRef)
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
	cmd := exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", sha+"^.."+sha, "--", path)
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
			// git diff exits 1 when there are differences — not an error
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 128:
			// sha^ failed (root commit) — diff against the empty tree
			emptyTree := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
			cmd2 := exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", emptyTree+".."+sha, "--", path)
			cmd2.Env = stripExternalDiffEnv()
			if dir != "" {
				cmd2.Dir = dir
			}
			out, err = cmd2.Output()
			if err != nil {
				var exitErr2 *exec.ExitError
				if errors.As(err, &exitErr2) && exitErr2.ExitCode() == 1 {
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
	mergeBase, err := MergeBase(defaultBaseRef())
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

// runGitInDir runs `git <args...>` in dir and returns the stdout. Mirrors the
// existing inline exec.Command pattern used elsewhere in git.go.
func runGitInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	return string(out), err
}

// ResolveDefaultBranchSHA returns the tip SHA of the repo's default branch,
// preferring the remote (origin) and falling back to local. Returns
// ("", err) on failure — callers treat that as "full-stack unavailable"
// rather than fatal.
func ResolveDefaultBranchSHA(vcs VCS, repoRoot, defaultBranch string) (string, error) {
	if vcs == nil || defaultBranch == "" {
		return "", fmt.Errorf("default branch unknown")
	}
	if vcs.Name() == "git" {
		if out, err := runGitInDir(repoRoot, "rev-parse", "--verify", "origin/"+defaultBranch); err == nil {
			return strings.TrimSpace(out), nil
		}
		if out, err := runGitInDir(repoRoot, "rev-parse", "--verify", defaultBranch); err == nil {
			return strings.TrimSpace(out), nil
		}
		return "", fmt.Errorf("could not resolve %s tip", defaultBranch)
	}
	// Sapling: try remote bookmark, then local.
	if out, err := slCommandInDir(repoRoot, "log", "-r", "remote/"+defaultBranch, "-T", "{node}"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	if out, err := slCommandInDir(repoRoot, "log", "-r", defaultBranch, "-T", "{node}"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	return "", fmt.Errorf("could not resolve %s tip", defaultBranch)
}

// walkAncestors enumerates HEAD-first the recent ancestor SHAs that are
// candidates for stack stops. Capped at maxDepth.
func walkAncestors(vcs VCS, repoRoot string, maxDepth int) ([]string, error) {
	if vcs == nil {
		return nil, nil
	}
	if vcs.Name() == "git" {
		out, err := runGitInDir(repoRoot, "rev-list", "--first-parent", "-n", strconv.Itoa(maxDepth), "HEAD")
		if err != nil {
			return nil, err
		}
		return splitNonEmpty(out), nil
	}
	// Sapling: ancestors of `.` that are still draft.
	out, err := slCommandInDir(repoRoot, "log", "-r",
		fmt.Sprintf("ancestors(., %d) & draft()", maxDepth),
		"-T", "{node}\n")
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(out), nil
}

// localBranchTips returns SHAs that have a useful local label, mapped to that
// label. For git: refs/heads/ entries. For sapling: bookmarks (when present)
// plus draft commit first-line descriptions for stack labels.
func localBranchTips(vcs VCS, repoRoot string) (map[string]string, error) {
	if vcs == nil {
		return nil, nil
	}
	if vcs.Name() == "git" {
		out, err := runGitInDir(repoRoot, "for-each-ref", "--format=%(objectname) %(refname:short)", "refs/heads/")
		if err != nil {
			return nil, err
		}
		result := make(map[string]string)
		for _, line := range splitNonEmpty(out) {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			}
		}
		return result, nil
	}
	result := make(map[string]string)
	if bookmarks, err := slCommandInDir(repoRoot, "bookmarks", "-T", "{node} {bookmark}\n"); err == nil {
		for _, line := range splitNonEmpty(bookmarks) {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			}
		}
	}
	if drafts, err := slCommandInDir(repoRoot, "log", "-r", "draft()", "-T", "{node} {desc|firstline}\n"); err == nil {
		for _, line := range splitNonEmpty(drafts) {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				if _, ok := result[parts[0]]; !ok {
					result[parts[0]] = parts[1]
				}
			}
		}
	}
	return result, nil
}

// remoteBranchTips returns up to 20 remote branches sorted by recency,
// excluding the default branch and any HEAD pointers. Caller subtracts SHAs
// already covered by other picker sections.
func remoteBranchTips(vcs VCS, repoRoot, defaultBranch string) ([]BranchEntry, error) {
	if vcs == nil {
		return nil, nil
	}
	if vcs.Name() == "git" {
		return remoteBranchTipsGit(repoRoot, defaultBranch)
	}
	return remoteBranchTipsSapling(repoRoot, defaultBranch)
}

func remoteBranchTipsGit(repoRoot, defaultBranch string) ([]BranchEntry, error) {
	out, err := runGitInDir(repoRoot, "for-each-ref",
		"--sort=-committerdate", "--count=40",
		"--format=%(objectname) %(refname:short)",
		"refs/remotes/")
	if err != nil {
		return nil, err
	}
	var entries []BranchEntry
	for _, line := range splitNonEmpty(out) {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[1]
		if strings.HasSuffix(name, "/HEAD") {
			continue
		}
		if name == "origin/"+defaultBranch || name == defaultBranch {
			continue
		}
		entries = append(entries, BranchEntry{Name: name, HeadSHA: parts[0]})
		if len(entries) >= 20 {
			break
		}
	}
	return entries, nil
}

func remoteBranchTipsSapling(repoRoot, defaultBranch string) ([]BranchEntry, error) {
	out, err := slCommandInDir(repoRoot, "log", "-r",
		"sort(remote(), -date)", "--limit", "40",
		"-T", "{node} {remotebookmarks}\n")
	if err != nil {
		return nil, nil //nolint:nilerr // best-effort; missing is fine
	}
	var entries []BranchEntry
	for _, line := range splitNonEmpty(out) {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		bookmark := strings.SplitN(parts[1], " ", 2)[0]
		if bookmark == defaultBranch || strings.HasSuffix(bookmark, "/"+defaultBranch) {
			continue
		}
		entries = append(entries, BranchEntry{Name: bookmark, HeadSHA: parts[0]})
		if len(entries) >= 20 {
			break
		}
	}
	return entries, nil
}

// ChangedFilesBetweenSHAs returns the files changed in the range baseSHA..headSHA.
// Renames are reported with status "renamed" and the new path; the old path is
// not surfaced (matches existing parseNameStatus behavior).
// Untracked working-tree files are NOT included — this is a pure git-history range.
func ChangedFilesBetweenSHAs(baseSHA, headSHA, dir string) ([]FileChange, error) {
	cmd := exec.Command("git", "diff", "--name-status", "-M", baseSHA+".."+headSHA)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s..%s --name-status: %w", baseSHA, headSHA, err)
	}
	return parseNameStatus(string(out)), nil
}

// FileDiffBetweenSHAs returns parsed diff hunks for path in the range
// baseSHA..headSHA. Returns nil hunks when there is no diff.
func FileDiffBetweenSHAs(path, baseSHA, headSHA, dir string) ([]DiffHunk, error) {
	cmd := exec.Command("git", "-c", "diff.external=", "diff",
		"--no-color", "--no-ext-diff",
		baseSHA+".."+headSHA, "--", path)
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		// git diff exits 1 when there are differences — that's normal.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("git diff: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}

// ReadFileAtSHA returns the bytes of path at the given SHA.
// Returns (nil, nil) when the file does not exist at that SHA (deleted/added cases).
// Errors are reserved for "git command failed" (e.g. SHA not present locally).
func ReadFileAtSHA(sha, path, dir string) ([]byte, error) {
	cmd := exec.Command("git", "show", sha+":"+path)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			// Distinguish "path missing at sha" (not an error) from
			// "sha missing entirely" (error). git show prints "fatal:
			// path 'foo' exists on disk, but not in 'sha'" or similar
			// for missing path; "fatal: bad object sha" for missing sha.
			msg := strings.TrimSpace(stderr.String())
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "path") || strings.Contains(lower, "does not exist") {
				return nil, nil
			}
			return nil, fmt.Errorf("git show %s:%s: %s", sha, path, msg)
		}
		return nil, fmt.Errorf("git show %s:%s: %w", sha, path, err)
	}
	return out, nil
}

// HasObject reports whether sha is reachable as a commit object in the local store.
// Cheap (no walk).
func HasObject(sha, dir string) bool {
	cmd := exec.Command("git", "cat-file", "-e", sha+"^{commit}")
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run() == nil
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
	out, err := runGit(context.Background(), dir, "ls-files")
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
	out2, err := runGit(context.Background(), dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return files, nil //nolint:nilerr // non-fatal: return tracked files only
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
	".sl":          true,
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
			return nil //nolint:nilerr // best-effort walk: skip inaccessible entries
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
			return nil //nolint:nilerr // skip files with unresolvable relative paths
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

// fileStatusInRepo returns the git status of a single file relative to baseRef
// by running `git diff --name-status <baseRef> -- <path>` from the repo root.
// This mirrors the same diff approach that ChangedFiles uses (merge-base diff),
// so files committed on a branch are correctly reported as added/modified.
// Falls back to checking whether the file is tracked when baseRef is empty.
func fileStatusInRepo(path, repoRoot, baseRef string) string {
	if baseRef == "" {
		// No base ref — check if the file is tracked at all.
		cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
		cmd.Dir = repoRoot
		if err := cmd.Run(); err != nil {
			return "untracked"
		}
		return "modified"
	}
	cmd := exec.Command("git", "diff", "--name-status", baseRef, "--", path)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "untracked"
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		// File exists but is unchanged relative to baseRef.
		// Check if it's tracked; if not, it's untracked.
		chk := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
		chk.Dir = repoRoot
		if err := chk.Run(); err != nil {
			return "untracked"
		}
		return "modified"
	}
	changes := parseNameStatus(line)
	if len(changes) > 0 {
		return changes[0].Status
	}
	return "modified"
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

// fileDiffUnified returns the parsed diff hunks for a file against a base ref.
// If baseRef is empty, diffs against HEAD. The dir parameter sets the working directory.
func fileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	if baseRef == "" {
		cmd = exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", "HEAD", "--", path)
	} else {
		cmd = exec.Command("git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", baseRef, "--", path)
	}
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means diff found changes (normal), check for actual errors
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
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
		cmd = exec.CommandContext(ctx, "git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", "HEAD", "--", path)
	} else {
		cmd = exec.CommandContext(ctx, "git", "-c", "diff.external=", "diff", "--no-color", "--no-ext-diff", baseRef, "--", path)
	}
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
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

// DiffNumstatDir runs git diff --numstat against the given base ref and returns per-file stats.
// If dir is non-empty, git runs in that directory.
func DiffNumstatDir(baseRef, dir string) (map[string]NumstatEntry, error) {
	cmd := exec.Command("git", "-c", "diff.external=", "diff", "--no-ext-diff", "--numstat", baseRef)
	cmd.Env = stripExternalDiffEnv()
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
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

		switch {
		case strings.HasPrefix(line, "+"):
			current.Lines = append(current.Lines, DiffLine{
				Type:    "add",
				Content: strings.TrimPrefix(line, "+"),
				NewNum:  newLine,
			})
			newLine++
		case strings.HasPrefix(line, "-"):
			current.Lines = append(current.Lines, DiffLine{
				Type:    "del",
				Content: strings.TrimPrefix(line, "-"),
				OldNum:  oldLine,
			})
			oldLine++
		case strings.HasPrefix(line, " "):
			current.Lines = append(current.Lines, DiffLine{
				Type:    "context",
				Content: strings.TrimPrefix(line, " "),
				OldNum:  oldLine,
				NewNum:  newLine,
			})
			oldLine++
			newLine++
		case line == "" && oldLine < current.OldStart+current.OldCount:
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
		case line == `\ No newline at end of file`:
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
	cmd := exec.Command("git", "--no-optional-locks", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
