package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureReviewFolder migrates legacy flat review files into the v4 folder layout.
func EnsureReviewFolder(identity string) error {
	return ensureReviewFolder(identity)
}

// MIGRATION-REMOVAL: TODO remove this function and its call from LoadCritJSON
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
// This intentionally does NOT route through AtomicWriteFile. AtomicWriteFile
// writes a single byte buffer to a single target file (mkdir + tmpfile +
// fsync + rename). The migration moves an *existing* file into a freshly
// created tmp directory and atomically renames the directory into place —
// no byte writes happen, the original on-disk content is reused via
// os.Rename. The shapes only superficially overlap (both end in
// rename-into-place); the migration's atomicity guarantee is "either the
// flat file or the folder exists", not "the file is fully written or not at
// all". Reusing AtomicWriteFile would require re-reading and re-writing the
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
