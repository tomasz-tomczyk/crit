package session

import (
	"encoding/json"
	"fmt"
	"os"
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

// SaveCritJSON writes review.json for a review identity folder.
func SaveCritJSON(critPath string, cj CritJSON) error {
	return saveCritJSONToDisk(critPath, cj)
}

// saveCritJSONToDisk writes review.json for a review identity folder.
func saveCritJSONToDisk(critPath string, cj CritJSON) error {
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

// ensureReviewFolder delegates to EnsureReviewFolderFn when wired from cmd/crit.
func ensureReviewFolder(identity string) error {
	if EnsureReviewFolderFn != nil {
		return EnsureReviewFolderFn(identity)
	}
	if info, err := os.Stat(identity); err == nil && info.IsDir() {
		return nil
	}
	return nil
}

// ClearReviewFolder removes the entire review identity folder.
func ClearReviewFolder(identity string) error {
	if err := os.RemoveAll(identity); err != nil && !os.IsNotExist(err) {
		return err
	}
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
