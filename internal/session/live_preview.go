package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// NewLiveSession builds a minimal session for live mode (no files, no VCS).
func NewLiveSession(liveOrigin, reviewPath string) (*Session, error) {
	if liveOrigin == "" {
		return nil, fmt.Errorf("NewLiveSession: liveOrigin is empty")
	}
	cwd, _ := os.Getwd()
	s := &Session{
		Mode:                "files",
		RepoRoot:            cwd,
		ReviewRound:         1,
		ReviewType:          "live",
		Origin:              liveOrigin,
		CLIArgs:             []string{"live", liveOrigin},
		awaitingFirstReview: true,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		Files:               []*FileEntry{},
	}
	if reviewPath != "" {
		s.ReviewFilePath = reviewPath
		if cj, err := readCritJSONFromDisk(reviewPath); err == nil {
			hasComments := len(cj.ReviewComments) > 0
			if !hasComments {
				for _, fe := range cj.Files {
					if len(fe.Comments) > 0 {
						hasComments = true
						break
					}
				}
			}
			if hasComments && cj.ReviewRound > 0 {
				s.ReviewRound = cj.ReviewRound
			}
			for path, fe := range cj.Files {
				s.Files = append(s.Files, &FileEntry{
					Path:     path,
					FileType: "live-route",
					Comments: fe.Comments,
					Status:   fe.Status,
				})
			}
		}
	}
	return s, nil
}

// NewPreviewSession builds a session for preview mode.
func NewPreviewSession(previewFile, reviewPath string) (*Session, error) {
	if previewFile == "" {
		return nil, fmt.Errorf("NewPreviewSession: previewFile is empty")
	}
	cwd, _ := os.Getwd()
	relPath, err := filepath.Rel(cwd, previewFile)
	if err != nil {
		relPath = previewFile
	}
	content, err := os.ReadFile(previewFile)
	if err != nil {
		return nil, fmt.Errorf("reading preview file: %w", err)
	}
	s := &Session{
		Mode:                "files",
		RepoRoot:            cwd,
		ReviewRound:         1,
		ReviewType:          "preview",
		Origin:              previewFile,
		CLIArgs:             []string{"preview", previewFile},
		awaitingFirstReview: true,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     relPath,
				Status:   "added",
				FileType: "code",
				Content:  string(content),
			},
		},
	}
	if reviewPath != "" {
		s.ReviewFilePath = reviewPath
		s.loadCritJSON()
	}
	return s, nil
}
