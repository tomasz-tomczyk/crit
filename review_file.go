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

// resolveReviewPath returns the review identity path for the current context.
// In v4 the identity is a folder; review.json and snapshots.json live inside.
// Resolution order:
//  1. If outputDir is set, return outputDir/.crit (explicit override)
//  2. Check daemon registry for running sessions matching this cwd
//  3. If one daemon matches, use its ReviewPath
//  4. If multiple daemons match, use the one matching current branch
//  5. If no daemon found, compute the centralized path: ~/.crit/reviews/<key>
func resolveReviewPath(outputDir string) (string, error) {
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit"), nil
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

// SnapshotsFile is the per-round-content sidecar inside a review folder. Lives
// at <folder>/snapshots.json. The crit server reads/writes it; agents do not.
type SnapshotsFile struct {
	RoundSnapshots map[string]map[int]RoundSnapshot `json:"round_snapshots"`
}

// reviewPaths derives the v4 folder layout from a review identity path.
// The identity may have been a flat .json file in v3; v4 treats it uniformly
// as a folder. Migration is handled by ensureReviewFolder.
type reviewPaths struct {
	Folder    string
	Review    string
	Snapshots string
}

// reviewPathsFor returns the v4 folder-form paths for a review identity.
// reviewPathsFor does not touch disk; migration is handled separately.
func reviewPathsFor(identity string) reviewPaths {
	return reviewPaths{
		Folder:    identity,
		Review:    filepath.Join(identity, "review.json"),
		Snapshots: filepath.Join(identity, "snapshots.json"),
	}
}

// loadSnapshotsFile reads <folder>/snapshots.json. Missing file = empty map +
// nil error (first-boot, legacy review with no snapshots yet, or the folder is
// in the orphan-snapshots state which is benign on read).
func loadSnapshotsFile(snapshotsPath string) (SnapshotsFile, error) {
	sf := SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{}}
	data, err := os.ReadFile(snapshotsPath)
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
func saveSnapshotsFile(snapshotsPath string, sf SnapshotsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshots file: %w", err)
	}
	// atomicWriteFile MkdirAlls the parent, so the review folder is created
	// implicitly on first sidecar write.
	return atomicWriteFile(snapshotsPath, append(data, '\n'), 0o644)
}

// writeCritJSON resolves the review path and writes a CritJSON via saveCritJSON.
func writeCritJSON(cj CritJSON, outputDir string) error {
	path, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return saveCritJSON(path, cj)
}

// MIGRATION-REMOVAL: TODO remove this function and its call from loadCritJSON
// in the release after v4-folder-format ships. Tracked in follow-up issue (see
// plan v4 §"Follow-up issue draft").
//
// ensureReviewFolder migrates legacy review-file shapes into the v4 folder
// layout at <identity>/. It handles three pre-v4 states, all idempotent and
// crash-tolerant:
//
//  1. <identity> is already a folder: no-op (steady state).
//  2. <identity>.json/ exists as a folder (early-v4 wrong-shape mid-state with
//     `.json` accidentally kept on the folder name): rename to <identity>/.
//  3. <identity>.json exists as a flat file (v3 layout): move into
//     <identity>/review.json, plus any sibling <identity>.json.snapshots.json
//     sidecar into <identity>/snapshots.json.
//
// No-op when there's nothing to migrate.
func ensureReviewFolder(identity string) error {
	if info, err := os.Stat(identity); err == nil {
		if info.IsDir() {
			return nil
		}
		// Defense-in-depth: a flat file showed up at the v4 identity path
		// (e.g. an external tool, or a downgrade). Treat as v3-shaped at
		// this same path and migrate in place.
		return migrateFlatToFolder(identity, identity)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat review identity: %w", err)
	}

	// Legacy <identity>.json path (folder or flat). The v4 identity does not
	// carry the .json extension; both shapes are pre-v4 and need renaming.
	legacy := identity + ".json"
	info, err := os.Stat(legacy)
	switch {
	case err == nil && info.IsDir():
		return renameLegacyJSONFolder(legacy, identity)
	case err == nil && !info.IsDir():
		return migrateFlatToFolder(legacy, identity)
	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("stat legacy review identity: %w", err)
	}
}

// MIGRATION-REMOVAL: TODO delete with ensureReviewFolder.
//
// renameLegacyJSONFolder fixes the early-v4 mid-state where the review folder
// was created with a stray `.json` extension (`<identity>.json/`). It renames
// it to the correct `<identity>/`.
func renameLegacyJSONFolder(legacyFolder, identity string) error {
	if err := os.Rename(legacyFolder, identity); err != nil {
		return fmt.Errorf("renaming legacy .json folder: %w", err)
	}
	fmt.Fprintf(os.Stderr, "crit: renamed legacy .json review folder to %s\n", identity)
	return nil
}

// MIGRATION-REMOVAL: TODO delete with ensureReviewFolder.
//
// migrateFlatToFolder converts a v3 flat-file review at flatPath into a v4
// folder at identity, moving any sibling .snapshots.json sidecar inside.
// flatPath and identity may be equal (legacy in-place migration) or differ
// (legacy <identity>.json -> v4 <identity>).
//
// This intentionally does NOT route through atomicWriteFile. atomicWriteFile
// writes a single byte buffer to a single target file (mkdir + tmpfile +
// fsync + rename). The migration moves an *existing* file into a freshly
// created tmp directory and atomically renames the directory into place —
// no byte writes happen, the original on-disk content is reused via
// os.Rename. The shapes only superficially overlap (both end in
// rename-into-place); the migration's atomicity guarantee is "either the
// flat file or the folder exists", not "the file is fully written or not at
// all". Reusing atomicWriteFile would require re-reading and re-writing the
// review JSON for no benefit. (review W6)
func migrateFlatToFolder(flatPath, identity string) error {
	tmp := identity + ".crit-migrate.tmp"
	// A previous crash may have left tmp/ behind. Wipe.
	_ = os.RemoveAll(tmp)

	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("creating migration tmp dir: %w", err)
	}

	if err := os.Rename(flatPath, filepath.Join(tmp, "review.json")); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("moving review file into folder: %w", err)
	}

	flatSidecar := flatPath + ".snapshots.json"
	if _, err := os.Stat(flatSidecar); err == nil {
		if err := os.Rename(flatSidecar, filepath.Join(tmp, "snapshots.json")); err != nil {
			fmt.Fprintf(os.Stderr, "crit: warning: could not move snapshot sidecar during migration: %v\n", err)
		}
	}

	if err := os.Rename(tmp, identity); err != nil {
		return fmt.Errorf("finalizing folder migration: %w", err)
	}

	fmt.Fprintf(os.Stderr, "crit: migrated review storage to folder format: %s\n", identity)
	return nil
}

// loadCritJSON reads the review file from disk (folder layout
// <identity>/review.json), or returns a fresh CritJSON if it doesn't exist.
// Triggers v3->v4 migration on read for any pre-existing flat-file review.
func loadCritJSON(critPath string) (CritJSON, error) {
	var cj CritJSON

	// MIGRATION-REMOVAL: trigger v3->v4 folder migration on read.
	if err := ensureReviewFolder(critPath); err != nil {
		return cj, fmt.Errorf("review folder migration: %w", err)
	}

	reviewPath := reviewPathsFor(critPath).Review
	if data, err := readFileShared(reviewPath); err == nil {
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
// In v4 the review identity is treated as a folder; the review JSON is
// written to <identity>/review.json and atomicWriteFile MkdirAlls the parent.
func saveCritJSON(critPath string, cj CritJSON) error {
	// Defense-in-depth: if a future code path stat-tests <identity> and
	// finds a flat file (e.g. an external tool dropped one in, or a v3
	// downgrade reintroduced the layout), normalize to the folder form
	// before writing. ensureReviewFolder is a no-op when <identity> is
	// already a directory (the steady-state v4 case), so this guard adds
	// only a single os.Stat per save in production. See plan v4
	// §Folder-format invariants.
	if err := ensureReviewFolder(critPath); err != nil {
		return fmt.Errorf("ensuring review folder: %w", err)
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling review file: %w", err)
	}
	return atomicWriteFile(reviewPathsFor(critPath).Review, append(data, '\n'), 0o644)
}

// clearCritJSON removes the review folder for the resolved path or outputDir.
func clearCritJSON(outputDir string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return clearReviewFolder(critPath)
}

// clearReviewFolder removes the entire review folder (review.json,
// snapshots.json, future attachments). Idempotent: a missing folder is fine.
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
	dir, err := reviewsDir()
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
			data, readErr := os.ReadFile(filepath.Join(folder, "review.json"))
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
		data, readErr := readFileShared(path)
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
