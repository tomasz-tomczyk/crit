package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errReviewFileNotFoundForBranch is returned by findReviewFileByBranch when no
// review file matches the given branch. Callers (e.g. redirectReviewPathForPR)
// treat this as a silent miss — keep using the cwd-resolved path.
var errReviewFileNotFoundForBranch = errors.New("no review file found for branch")

// errReviewFileAmbiguousForBranch is returned by findReviewFileByBranch when
// multiple review files match the given branch. Callers should surface a
// stderr Note so the user knows why the cwd-resolved path was used.
var errReviewFileAmbiguousForBranch = errors.New("multiple review files match branch")

// resolveReviewPath returns the full path to the review file for the current context.
// Resolution order:
//  1. If outputDir is set, return outputDir/.crit.json (explicit override)
//  2. Check daemon registry for running sessions matching this cwd
//  3. If one daemon matches, use its ReviewPath
//  4. If multiple daemons match, use the one matching current branch
//  5. If no daemon found, compute the centralized path: ~/.crit/reviews/<key>.json
func resolveReviewPath(outputDir string) (string, error) {
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit.json"), nil
	}

	cwd, err := resolvedCWD()
	if err != nil {
		return "", err
	}

	if path := resolveReviewPathFromDaemon(cwd); path != "" {
		return path, nil
	}

	// No daemon — compute centralized path.
	branch := ""
	if vcs := DetectVCS(""); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	key := sessionKey(cwd, branch, nil)
	path, err := reviewFilePath(key)
	if err != nil {
		return "", err
	}

	return path, nil
}

// resolveReviewPathFromDaemon checks the daemon registry for a running session
// and returns its review path. Tries exact CWD match first, then falls back to
// matching by git repo root (handles subdirectory mismatch — e.g. daemon started
// from repo/api but crit comment run from repo/).
func resolveReviewPathFromDaemon(cwd string) string {
	sessions, _ := listSessionsForCWD(cwd)
	if path := pickReviewPath(sessions); path != "" {
		return path
	}

	// Fallback: match by VCS repo root.
	if len(sessions) == 0 {
		vcs := DetectVCS("")
		if vcs == nil {
			return ""
		}
		if repoRoot, err := vcs.RepoRoot(); err == nil && repoRoot != cwd {
			repoSessions, _ := listSessionsForRepoRoot(repoRoot)
			if path := pickReviewPath(repoSessions); path != "" {
				return path
			}
		}
	}
	return ""
}

// pickReviewPath selects a review path from a list of sessions.
// Returns the path if exactly one session has one, or defers to branch matching for multiple.
func pickReviewPath(sessions []sessionEntry) string {
	if len(sessions) == 1 && sessions[0].ReviewPath != "" {
		return sessions[0].ReviewPath
	}
	if len(sessions) > 1 {
		return resolveReviewPathFromSessions(sessions)
	}
	return ""
}

// resolveReviewPathFromSessions picks the best ReviewPath from multiple daemon sessions.
// Tries current branch first, then falls back to the first session with a ReviewPath.
func resolveReviewPathFromSessions(sessions []sessionEntry) string {
	branch := CurrentBranch()
	for _, s := range sessions {
		if s.Branch == branch && s.ReviewPath != "" {
			return s.ReviewPath
		}
	}
	for _, s := range sessions {
		if s.ReviewPath != "" {
			return s.ReviewPath
		}
	}
	return ""
}

// writeCritJSON resolves the review path and writes a CritJSON via saveCritJSON.
func writeCritJSON(cj CritJSON, outputDir string) error {
	path, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return saveCritJSON(path, cj)
}

// loadCritJSON reads the review file from disk, or returns a fresh CritJSON if the file doesn't exist.
func loadCritJSON(critPath string) (CritJSON, error) {
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return cj, fmt.Errorf("invalid existing review file: %w", err)
		}
	} else if os.IsNotExist(err) {
		branch := CurrentBranch()
		cfg := LoadConfig(filepath.Dir(critPath))
		base := cfg.BaseBranch
		if base == "" {
			base = defaultBaseRef()
		}
		baseRef, _ := MergeBase(base)
		cj = CritJSON{
			Branch:      branch,
			BaseRef:     baseRef,
			ReviewRound: 1,
			Files:       make(map[string]CritJSONFile),
		}
	} else {
		return cj, fmt.Errorf("reading review file: %w", err)
	}
	return cj, nil
}

// saveCritJSON writes the CritJSON struct to disk with pretty-printed JSON
// and a trailing newline. Uses atomic writes to prevent corruption.
func saveCritJSON(critPath string, cj CritJSON) error {
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling review file: %w", err)
	}
	// atomicWriteFile (atomic_write.go) handles MkdirAll internally.
	return atomicWriteFile(critPath, append(data, '\n'), 0644)
}

// clearCritJSON removes the review file from the resolved path or outputDir.
func clearCritJSON(outputDir string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	if err := os.Remove(critPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// findReviewFileByCommentID scans all review files in ~/.crit/reviews/ for the given
// comment ID, skipping excludePath. Returns the path if found in exactly one file,
// or an error wrapping commentID if it's missing or appears in multiple files.
func findReviewFileByCommentID(commentID string, excludePath string) (string, error) {
	dir, err := reviewsDir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("comment %q not found in any review file", commentID)
		}
		return "", err
	}

	var matchPath string
	for _, de := range entries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		if path == excludePath {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		if reviewFileContainsComment(data, commentID) {
			if matchPath != "" {
				return "", fmt.Errorf("comment %q found in multiple review files", commentID)
			}
			matchPath = path
		}
	}
	if matchPath == "" {
		return "", fmt.Errorf("comment %q not found in any review file", commentID)
	}
	return matchPath, nil
}

// findReviewFileByBranch scans all review files in ~/.crit/reviews/ for one
// whose top-level "branch" field equals branch, skipping excludePath. Returns
// the path if exactly one match is found. Used by `crit pull`/`crit push` to
// route explicit-PR operations to the review file that owns the PR's branch
// when the cwd-resolved review file is for a different branch — same class
// of cwd-vs-intent mismatch that PR #424 fixed for `crit comment`.
//
// Cross-repo safety: matching is purely on the "branch" field, so two repos
// with reviews on the same branch name could theoretically collide. In
// practice the caller has already constrained the PR number to cwd's repo
// via `gh pr view` (which uses the cwd's git remote), so a single-match
// across all reviews is the right one. If multiple repos do share both the
// branch name and an active review file, the ambiguous error fires and the
// caller falls back to the cwd-resolved path.
func findReviewFileByBranch(branch, excludePath string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("branch is required")
	}
	dir, err := reviewsDir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %q", errReviewFileNotFoundForBranch, branch)
		}
		return "", err
	}

	var matchPath string
	for _, de := range entries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		if path == excludePath {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var cj CritJSON
		if err := json.Unmarshal(data, &cj); err != nil {
			continue
		}
		if cj.Branch != branch {
			continue
		}
		if matchPath != "" {
			return "", fmt.Errorf("%w: %q", errReviewFileAmbiguousForBranch, branch)
		}
		matchPath = path
	}
	if matchPath == "" {
		return "", fmt.Errorf("%w: %q", errReviewFileNotFoundForBranch, branch)
	}
	return matchPath, nil
}

// reviewFileContainsComment does a quick check if a review JSON file contains
// a comment with the given ID. Uses string search first as a fast path to
// avoid parsing files that definitely don't contain the ID.
func reviewFileContainsComment(data []byte, commentID string) bool {
	// Fast path: if the ID string doesn't appear at all, skip JSON parsing.
	if !bytes.Contains(data, []byte(commentID)) {
		return false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return false
	}
	for _, c := range cj.ReviewComments {
		if c.ID == commentID {
			return true
		}
	}
	for _, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.ID == commentID {
				return true
			}
		}
	}
	return false
}

// cjContainsCommentID reports whether the given comment ID exists in the
// in-memory CritJSON, across review-level and per-file comments.
func cjContainsCommentID(cj *CritJSON, id string) bool {
	for _, c := range cj.ReviewComments {
		if c.ID == id {
			return true
		}
	}
	for _, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}
