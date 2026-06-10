package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// errReviewFileNotFoundForBranch is returned by findReviewFileByBranch when no
// review file matches the given branch. Callers (e.g. redirectReviewPathForPR)
// treat this as a silent miss — keep using the cwd-resolved path.
var errReviewFileNotFoundForBranch = errors.New("no review file found for branch")

// errReviewFileAmbiguousForBranch is returned by findReviewFileByBranch when
// multiple review files match the given branch. Callers should surface a
// stderr Note so the user knows why the cwd-resolved path was used.
var errReviewFileAmbiguousForBranch = errors.New("multiple review files match branch")

// ResolveReviewPath returns the review identity path for the current context.
// In v4 the identity is a folder; review.json and snapshots.json live inside.
// Resolution order:
//  1. If outputDir is set, return outputDir/.crit (explicit override)
//  2. Check daemon registry for running sessions matching this cwd
//  3. If one daemon matches, use its ReviewPath
//  4. If multiple daemons match, use the one matching current branch
//  5. If no daemon found, compute the centralized path: ~/.crit/reviews/<key>
func ResolveReviewPath(outputDir string) (string, error) {
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit"), nil
	}

	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return "", err
	}

	if path := ResolveReviewPathFromDaemon(cwd); path != "" {
		return path, nil
	}

	// No daemon — compute centralized path.
	branch := ""
	if vc := vcs.DetectVCS(""); vc != nil {
		branch = vc.CurrentBranch()
	}
	key := daemon.SessionKey(cwd, branch, nil)
	path, err := daemon.ReviewFilePath(key)
	if err != nil {
		return "", err
	}

	return path, nil
}

// resolveReviewPathWithArgs is like ResolveReviewPath but includes file args
// in the session key, matching the key that file-mode sessions use.
func ResolveReviewPathWithArgs(outputDir string, fileArgs []string) (string, error) {
	if len(fileArgs) == 0 {
		return ResolveReviewPath(outputDir)
	}
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit"), nil
	}

	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return "", err
	}

	if path := ResolveReviewPathFromDaemon(cwd); path != "" {
		return path, nil
	}

	key := daemon.SessionKey(cwd, "", fileArgs)
	path, err := daemon.ReviewFilePath(key)
	if err != nil {
		return "", err
	}

	return path, nil
}

// ResolveReviewPathFromDaemon checks the daemon registry for a running session
// and returns its review path. Tries exact CWD match first, then falls back to
// matching by git repo root (handles subdirectory mismatch — e.g. daemon started
// from repo/api but crit comment run from repo/).
func ResolveReviewPathFromDaemon(cwd string) string {
	sessions, _ := daemon.ListSessionsForCWD(cwd)
	if path := pickReviewPath(sessions); path != "" {
		return path
	}

	// Fallback: match by VCS repo root.
	if len(sessions) == 0 {
		vcs := vcs.DetectVCS("")
		if vcs == nil {
			return ""
		}
		if repoRoot, err := vcs.RepoRoot(); err == nil && repoRoot != cwd {
			repoSessions, _ := daemon.ListSessionsForRepoRoot(repoRoot)
			if path := pickReviewPath(repoSessions); path != "" {
				return path
			}
		}
	}
	return ""
}

// pickReviewPath selects a review path from a list of sessions.
// Returns the path if exactly one session has one, or defers to branch matching for multiple.
func pickReviewPath(sessions []daemon.SessionEntry) string {
	if len(sessions) == 1 && sessions[0].ReviewPath != "" {
		return sessions[0].ReviewPath
	}
	if len(sessions) > 1 {
		return ResolveReviewPathFromSessions(sessions)
	}
	return ""
}

// ResolveReviewPathFromSessions picks the best ReviewPath from multiple daemon sessions.
// Tries current branch first, then falls back to the first session with a ReviewPath.
func ResolveReviewPathFromSessions(sessions []daemon.SessionEntry) string {
	branch := vcs.CurrentBranch()
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

// SnapshotsFile is the per-round-content sidecar inside a review folder. Lives
// at <folder>/snapshots.json. The crit server reads/writes it; agents do not.
type SnapshotsFile struct {
	RoundSnapshots map[string]map[int]RoundSnapshot `json:"round_snapshots"`
}

type reviewPaths = session.ReviewPaths

var ReviewPathsFor = session.ReviewPathsFor

// loadSnapshotsFile reads <folder>/snapshots.json. Missing file = empty map +
// nil error (first-boot, legacy review with no snapshots yet, or the folder is
// in the orphan-snapshots state which is benign on read).
func loadSnapshotsFile(snapshotsPath string) (SnapshotsFile, error) {
	sf := SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{}}
	data, err := session.ReadFileShared(snapshotsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sf, nil
		}
		return sf, fmt.Errorf("reading snapshots file: %w", err)
	}
	if err := json.Unmarshal(data, &sf); err != nil {
		return sf, fmt.Errorf("invalid snapshots file: %w", err)
	}
	if sf.RoundSnapshots == nil {
		sf.RoundSnapshots = map[string]map[int]RoundSnapshot{}
	}
	return sf, nil
}

// saveSnapshotsFile writes the sidecar atomically. Mirrors saveCritJSON.
func SaveSnapshotsFile(snapshotsPath string, sf SnapshotsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshots file: %w", err)
	}
	// config.AtomicWriteFile MkdirAlls the parent, so the review folder is created
	// implicitly on first sidecar write.
	return config.AtomicWriteFile(snapshotsPath, append(data, '\n'), 0o644)
}

// writeCritJSON resolves the review path and writes a CritJSON via saveCritJSON.
func writeCritJSON(cj CritJSON, outputDir string) error {
	path, err := ResolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return SaveCritJSON(path, cj)
}

// LoadCritJSON reads the review file from disk (folder layout
// <identity>/review.json), or returns a fresh CritJSON if it doesn't exist.
// Triggers v3->v4 migration on read for any pre-existing flat-file review.
func LoadCritJSON(critPath string) (CritJSON, error) {
	var cj CritJSON

	// MIGRATION-REMOVAL: trigger v3->v4 folder migration on read.
	if err := session.EnsureReviewFolder(critPath); err != nil {
		return cj, fmt.Errorf("review folder migration: %w", err)
	}

	reviewPath := ReviewPathsFor(critPath).Review
	if data, err := session.ReadFileShared(reviewPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return cj, fmt.Errorf("invalid existing review file: %w", err)
		}
	} else if os.IsNotExist(err) {
		branch := vcs.CurrentBranch()
		cfg := config.LoadConfig(filepath.Dir(critPath))
		base := cfg.BaseBranch
		if base == "" {
			base = vcs.DefaultBaseRef()
		}
		baseRef, _ := vcs.MergeBase(base)
		cj = CritJSON{
			Branch:      branch,
			BaseRef:     baseRef,
			ReviewRound: 1,
			Files:       make(map[string]session.CritJSONFile),
		}
	} else {
		return cj, fmt.Errorf("reading review file: %w", err)
	}
	return cj, nil
}

// SaveCritJSON writes the CritJSON struct to disk with pretty-printed JSON
// and a trailing newline. Uses atomic writes to prevent corruption.
// In v4 the review identity is treated as a folder; the review JSON is
// written to <identity>/review.json and config.AtomicWriteFile MkdirAlls the parent.
//
// This delegates to session.SaveCritJSON, the single review.json write path.
// session.SaveCritJSON applies the folder-normalization guard
// (session.EnsureReviewFolder) so every writer gets the same
// defense-in-depth, regardless of which package symbol it calls.
func SaveCritJSON(critPath string, cj CritJSON) error {
	return session.SaveCritJSON(critPath, cj)
}

// clearCritJSON removes the review folder (review.json, snapshots.json, and
// any attachments) for the resolved path or outputDir.
func clearCritJSON(outputDir string) error {
	critPath, err := ResolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return clearReviewFolder(critPath)
}

// clearReviewFolder removes the entire review folder (review.json,
// snapshots.json, attachments). Idempotent: a missing folder is fine.
func clearReviewFolder(identity string) error {
	if err := os.RemoveAll(identity); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// walkReviewIdentities iterates over every review identity in
// ~/.crit/reviews/ — folder-form (v4) and MIGRATION-REMOVAL flat-file (v3) —
// and invokes visit with the identity path and the bytes of its review.json.
// Stopping the walk: visit returns a sentinel via the bool/error pair; a
// non-nil error from visit aborts the walk and is returned.
func walkReviewIdentities(visit func(identity string, data []byte) error) error {
	dir, err := daemon.ReviewsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, de := range entries {
		name := de.Name()
		if de.IsDir() {
			folder := filepath.Join(dir, name)
			data, readErr := session.ReadFileShared(filepath.Join(folder, "review.json"))
			if readErr != nil {
				if !os.IsNotExist(readErr) {
					fmt.Fprintf(os.Stderr, "crit: warning: could not read %s/review.json: %v\n", folder, readErr)
				}
				continue
			}
			if err := visit(folder, data); err != nil {
				return err
			}
			continue
		}
		// MIGRATION-REMOVAL: legacy v3 flat-file scan.
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, readErr := session.ReadFileShared(path)
		if readErr != nil {
			continue
		}
		if err := visit(path, data); err != nil {
			return err
		}
	}
	return nil
}

// findReviewFileByCommentID scans all review identities in ~/.crit/reviews/
// for the given comment ID, skipping excludePath. Returns the identity path
// (folder for v4, flat .json for unmigrated v3) if found in exactly one
// place, or an error if missing or ambiguous.
func findReviewFileByCommentID(commentID string, excludePath string) (string, error) {
	var matchPath string
	walkErr := walkReviewIdentities(func(identity string, data []byte) error {
		if identity == excludePath {
			return nil
		}
		// Fast path: skip files that don't contain the ID at all.
		if !bytes.Contains(data, []byte(commentID)) {
			return nil
		}
		var cj CritJSON
		if err := json.Unmarshal(data, &cj); err != nil {
			// Malformed review file; warn and skip so a single corrupt
			// file doesn't abort the scan or silently hide regressions.
			fmt.Fprintf(os.Stderr, "crit: warning: malformed review JSON at %s: %v\n", identity, err)
			return nil //nolint:nilerr // intentional skip on parse failure
		}
		if !cjContainsCommentID(&cj, commentID) {
			return nil
		}
		if matchPath != "" {
			return fmt.Errorf("comment %q found in multiple review files", commentID)
		}
		matchPath = identity
		return nil
	})
	if walkErr != nil {
		return "", walkErr
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
	var matchPath string
	walkErr := walkReviewIdentities(func(identity string, data []byte) error {
		if identity == excludePath {
			return nil
		}
		var cj CritJSON
		if err := json.Unmarshal(data, &cj); err != nil {
			// Malformed review file; skip rather than aborting. Warn once
			// per malformed file so corruption isn't silently invisible.
			fmt.Fprintf(os.Stderr, "crit: warning: malformed review JSON at %s: %v\n", identity, err)
			return nil //nolint:nilerr // intentional skip on parse failure
		}
		if cj.Branch != branch {
			return nil
		}
		if matchPath != "" {
			return fmt.Errorf("%w: %q", errReviewFileAmbiguousForBranch, branch)
		}
		matchPath = identity
		return nil
	})
	if walkErr != nil {
		// walkErr already wraps errReviewFileAmbiguousForBranch when the
		// scan stopped because two reviews matched the same branch; for any
		// other error (e.g. unreadable reviewsDir) we surface it verbatim.
		return "", walkErr
	}
	if matchPath == "" {
		return "", fmt.Errorf("%w: %q", errReviewFileNotFoundForBranch, branch)
	}
	return matchPath, nil
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
