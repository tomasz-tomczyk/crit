package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// readCritJSONFromDisk reads review.json for a review identity folder.
// Missing file returns zero CritJSON and nil error.
func readCritJSONFromDisk(critPath string) (CritJSON, error) {
	var cj CritJSON
	data, err := ReadFileShared(ReviewPathsFor(critPath).Review)
	if err != nil {
		if os.IsNotExist(err) {
			return cj, nil
		}
		return cj, err
	}
	if err := json.Unmarshal(data, &cj); err != nil {
		return cj, err
	}
	return cj, nil
}

// SaveCritJSON writes review.json for a review identity folder. It is the
// single write path for review.json: review.SaveCritJSON delegates here, so
// every writer (share, comment, github, live, preview, cmd/crit) gets the
// same folder-normalization guard and atomic write.
func SaveCritJSON(critPath string, cj CritJSON) error {
	return saveCritJSONToDisk(critPath, cj)
}

// saveCritJSONToDisk writes review.json for a review identity folder.
func saveCritJSONToDisk(critPath string, cj CritJSON) error {
	// Defense-in-depth: if a code path stat-tests <identity> and finds a flat
	// file (e.g. an external tool dropped one in, or a v3 downgrade
	// reintroduced the layout), normalize to the folder form before writing.
	// ensureReviewFolder is a no-op when <identity> is already a directory
	// (the steady-state v4 case), so this guard adds only a single os.Stat per
	// save in production. See plan v4 §Folder-format invariants.
	if err := ensureReviewFolder(critPath); err != nil {
		return fmt.Errorf("ensuring review folder: %w", err)
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling review file: %w", err)
	}
	return AtomicWriteFile(ReviewPathsFor(critPath).Review, append(data, '\n'), 0o644)
}

// SnapshotsFile is the on-disk shape of snapshots.json.
type SnapshotsFile struct {
	RoundSnapshots map[string]map[int]RoundSnapshot `json:"round_snapshots"`
}

// loadSnapshotsFromDisk reads snapshots.json. Missing file is not an error.
func loadSnapshotsFromDisk(snapshotsPath string) (SnapshotsFile, error) {
	sf := SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{}}
	data, err := ReadFileShared(snapshotsPath)
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

// ClearReviewFolder removes the entire review identity folder under the
// review write lock, so a writer that already read review.json saves before
// the removal instead of restoring it after. Writers waiting for the lock
// notice their lock file was removed and lock a new one. Windows cannot
// delete the open lock file, so there the empty folder and lock file go once
// the lock is released. Idempotent: a missing folder is fine.
func ClearReviewFolder(identity string) error {
	info, err := os.Stat(identity)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return os.Remove(identity)
	}
	lockPath := ReviewPathsFor(identity).ReviewLock
	lockGone := false
	err = WithReviewWriteLock(identity, func() error {
		entries, err := os.ReadDir(identity)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(identity, e.Name())
			if path == lockPath {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
		// Unix allows this while the lock is held; Windows does not.
		if lockGone = os.Remove(lockPath) == nil; lockGone {
			_ = os.Remove(identity)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !lockGone {
		// Windows: the open lock file could not go while we held it.
		_ = os.Remove(lockPath)
	}
	// Not recursive: a writer that locked a new lock file after us may
	// already have saved a new review here, and that one stays.
	_ = os.Remove(identity)
	return nil
}

// SaveSnapshotsFile writes snapshots.json atomically.
func SaveSnapshotsFile(snapshotsPath string, sf SnapshotsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshots file: %w", err)
	}
	return AtomicWriteFile(snapshotsPath, append(data, '\n'), 0o644)
}
