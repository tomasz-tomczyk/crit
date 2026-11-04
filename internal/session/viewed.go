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

// ViewedUpdate is one mark sent by the review page.
type ViewedUpdate struct {
	Path     string `json:"path"`
	Viewed   bool   `json:"viewed"`
	Auto     bool   `json:"auto"`
	FileHash string `json:"file_hash"`
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

// ViewedFiles returns the marks saved in the review file. They live only on
// disk: buildCritJSON starts from the disk copy, so a daemon write keeps what
// `crit pull --viewed` saved without merging it into memory.
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
	return cj.ViewedFiles, nil
}

// SetViewedFiles saves marks in the same transaction as the session's pending
// state, pinned to the current focus.
func (s *Session) SetViewedFiles(updates []ViewedUpdate) error {
	critPath := s.critJSONPath()
	if critPath == "" || len(updates) == 0 {
		return nil
	}
	s.mu.RLock()
	headSHA, changeNumber := viewedFocusRef(s.Focus)
	s.mu.RUnlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return WithReviewWriteLock(critPath, func() error {
		return s.writeReviewFile(critPath, func(cj *CritJSON) {
			if cj.ViewedFiles == nil {
				cj.ViewedFiles = make(map[string]ViewedFile, len(updates))
			}
			for _, u := range updates {
				cj.ViewedFiles[u.Path] = ViewedFile{
					Viewed:       u.Viewed,
					Auto:         u.Auto && u.Viewed,
					FileHash:     u.FileHash,
					HeadSHA:      headSHA,
					ChangeNumber: changeNumber,
				}
			}
		})
	})
}
