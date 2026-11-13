package session

import (
	"encoding/json"
	"fmt"
	"os"
)

// ViewedFile is the reviewer's viewed mark for one path. Viewed false records
// an explicit untick, which is different from no entry at all: only an
// explicit untick may clear the mark on the forge.
type ViewedFile struct {
	Viewed bool `json:"viewed"`
	// Auto marks a file ticked by auto_viewed_patterns rather than by the
	// reviewer. Such files are hidden from view, not reviewed, so they are
	// never pushed.
	Auto bool `json:"auto,omitempty"`
	// FileHash is the content the mark applies to, when the file was loaded.
	FileHash string `json:"file_hash,omitempty"`
	// HeadSHA and ChangeNumber pin the mark to the PR revision it was made
	// on. Both are empty outside a PR's own diff, and such marks stay local.
	HeadSHA      string `json:"head_sha,omitempty"`
	ChangeNumber int    `json:"change_number,omitempty"`
}

// ViewedUpdate is one mark sent by the review page. HeadSHA is the revision
// the page was showing, which can trail the focus for a moment after it moves.
type ViewedUpdate struct {
	Path     string `json:"path"`
	Viewed   bool   `json:"viewed"`
	Auto     bool   `json:"auto"`
	FileHash string `json:"file_hash"`
	HeadSHA  string `json:"head_sha"`
}

// viewedFocusRef returns the PR revision a mark made in f applies to. A
// full-stack diff spans more than the PR, so a mark made there says nothing
// about the PR's own changes.
func viewedFocusRef(f Focus) (headSHA string, changeNumber int) {
	if f.Kind != FocusRange || f.ChangeNumber == 0 || f.DiffScope == DiffScopeFullStack {
		return "", 0
	}
	return f.HeadSHA, f.ChangeNumber
}

// viewedChangeKey names the ChangeViewedFiles bucket for marks made in f, or
// "" for views whose marks live in ViewedFiles. It is the same change
// identity comments are stamped with, so marks follow the layer and the
// repository, not just the number.
func viewedChangeKey(f Focus) string {
	if _, n := viewedFocusRef(f); n == 0 {
		return ""
	}
	return focusKeyFor(f)
}

// MigrateLegacyViewed unpins PR marks written before marks were keyed by
// change identity. Such a mark records only a PR number, so it cannot say
// which repository it belongs to, and a later mark on another layer may have
// overwritten it. It stays as a local mark, which is never pushed; the
// reviewer re-ticks the file to push it.
func MigrateLegacyViewed(cj *CritJSON) {
	for path, mark := range cj.ViewedFiles {
		if mark.HeadSHA != "" || mark.ChangeNumber != 0 {
			mark.HeadSHA, mark.ChangeNumber = "", 0
			cj.ViewedFiles[path] = mark
		}
	}
}

// viewedBucket returns the marks for key, creating the map when create is
// set.
func viewedBucket(cj *CritJSON, key string, create bool) map[string]ViewedFile {
	if key == "" {
		if cj.ViewedFiles == nil && create {
			cj.ViewedFiles = make(map[string]ViewedFile)
		}
		return cj.ViewedFiles
	}
	if cj.ChangeViewedFiles[key] == nil && create {
		if cj.ChangeViewedFiles == nil {
			cj.ChangeViewedFiles = make(map[string]map[string]ViewedFile)
		}
		cj.ChangeViewedFiles[key] = make(map[string]ViewedFile)
	}
	return cj.ChangeViewedFiles[key]
}

// ViewedFiles returns the marks saved in the review file for the current
// focus. They live only on disk: buildCritJSON starts from the disk copy, so
// a daemon write keeps what `crit pull --viewed` saved without merging it
// into memory.
func (s *Session) ViewedFiles() (map[string]ViewedFile, error) {
	critPath := s.critJSONPath()
	if critPath == "" {
		return nil, nil
	}
	data, err := ReadFileShared(ReviewPathsFor(critPath).Review)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil, fmt.Errorf("invalid review file: %w", err)
	}
	MigrateLegacyViewed(&cj)
	s.mu.RLock()
	key := viewedChangeKey(s.Focus)
	s.mu.RUnlock()
	return viewedBucket(&cj, key, false), nil
}

// SetViewedFiles saves marks in the same transaction as the session's pending
// state, under the current focus's change and pinned to its head.
func (s *Session) SetViewedFiles(updates []ViewedUpdate) error {
	critPath := s.critJSONPath()
	if critPath == "" || len(updates) == 0 {
		return nil
	}
	s.mu.RLock()
	headSHA, changeNumber := viewedFocusRef(s.Focus)
	key := viewedChangeKey(s.Focus)
	s.mu.RUnlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	// Acknowledge a deleted review before the lock recreates its folder, or
	// writeReviewFile would see the deletion and drop this mark.
	s.handleExternalDeletion(critPath)
	return WithReviewWriteLock(critPath, func() error {
		return s.writeReviewFile(critPath, func(cj *CritJSON) {
			MigrateLegacyViewed(cj)
			marks := viewedBucket(cj, key, true)
			for _, u := range updates {
				mark := ViewedFile{Viewed: u.Viewed, Auto: u.Auto && u.Viewed, FileHash: u.FileHash}
				// A mark made on any other revision stays unpinned: pinning
				// it to this head would claim a version nobody saw.
				if key != "" && u.HeadSHA == headSHA {
					mark.HeadSHA, mark.ChangeNumber = headSHA, changeNumber
				}
				marks[u.Path] = mark
			}
		})
	})
}

// pushableViewedReview returns a review holding only the marks `crit push
// --viewed` could still send, or nil if there are none. Approval removes the
// review, and finishing is the natural moment before that push, so these
// marks outlive it. Comments and rounds do not.
func pushableViewedReview(reviewPath string) *CritJSON {
	data, err := ReadFileShared(ReviewPathsFor(reviewPath).Review)
	if err != nil {
		return nil
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return nil
	}
	changes := make(map[string]map[string]ViewedFile)
	for key, bucket := range cj.ChangeViewedFiles {
		for path, mark := range bucket {
			if mark.Auto || mark.HeadSHA == "" {
				continue
			}
			if changes[key] == nil {
				changes[key] = make(map[string]ViewedFile)
			}
			changes[key][path] = mark
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return &CritJSON{
		Branch:            cj.Branch,
		BaseRef:           cj.BaseRef,
		UpdatedAt:         cj.UpdatedAt,
		ReviewRound:       1,
		CliArgs:           cj.CliArgs,
		CWD:               cj.CWD,
		ActiveDiffScope:   cj.ActiveDiffScope,
		Files:             map[string]CritJSONFile{},
		ChangeViewedFiles: changes,
	}
}
