package session

import (
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// StoryScopeHunk is one diff hunk in a story scope. It mirrors vcs.DiffHunk but
// is a stable, package-neutral shape so the story package can consume it
// without importing session internals (and without a session->story cycle).
type StoryScopeHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string
	RawLines []StoryScopeLine
}

// StoryScopeLine is one line within a StoryScopeHunk.
type StoryScopeLine struct {
	Type    string // "context", "add", "del"
	Content string
	OldNum  int
	NewNum  int
}

// StoryScopeFile is a changed file plus its hunks in the current diff scope.
type StoryScopeFile struct {
	Path    string
	Status  string
	Hunks   []StoryScopeHunk
	Ignored bool // matched an ignore_patterns entry
}

// StoryScope is the neutral snapshot of the current diff scope that the story
// package needs to build prep text and run coverage: base/head SHAs, commit
// messages, and the changed files (indexed) plus the ignored files (pre-placed
// into support[] by ingest).
type StoryScope struct {
	BaseSHA        string
	HeadSHA        string
	CommitMessages []string
	Files          []StoryScopeFile // Ignored=false: indexed; Ignored=true: pre-placed
}

// StoryScope builds a StoryScope for the session's current focus. It forces
// lazy files to load so every hunk is present, and separately re-derives the
// ignored files (which the session already filtered out) so ingest can
// pre-place them into support[]. ignorePatterns is the resolved union used to
// build the session.
func (s *Session) StoryScope(ignorePatterns []string) StoryScope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scope := StoryScope{BaseSHA: s.BaseRef}

	// HeadSHA is meaningful only for fixed-range scopes (--pr/--range). For a
	// working-tree scope it stays empty (the tree is the head).
	if s.Focus.Kind == FocusRange {
		scope.BaseSHA = s.Focus.BaseSHA
		scope.HeadSHA = s.Focus.HeadSHA
	}

	if s.VCS != nil {
		headRef := scope.HeadSHA // "" => working-tree HEAD
		if commits, err := s.VCS.CommitLog(scope.BaseSHA, headRef, s.RepoRoot); err == nil {
			for _, c := range commits {
				scope.CommitMessages = append(scope.CommitMessages, c.Message)
			}
		}
	}

	for _, fe := range s.Files {
		_ = fe.ensureLoaded(s.RepoRoot, s.BaseRef, s.VCS)
		scope.Files = append(scope.Files, StoryScopeFile{
			Path:   fe.Path,
			Status: fe.Status,
			Hunks:  convertHunks(fe.DiffHunks),
		})
	}

	// Re-derive ignored files: the session filtered them out, but ingest needs
	// to pre-place their hunks into support[]. Only meaningful in a git scope.
	if s.VCS != nil && len(ignorePatterns) > 0 && s.Focus.Kind != FocusRange {
		if all, err := s.VCS.ChangedFilesFromBaseInDir(s.BaseRef, s.RepoRoot); err == nil {
			for _, fc := range all {
				if !matchesAny(fc.Path, ignorePatterns) {
					continue
				}
				hunks, _ := diffHunksForFile(fc.Path, fc.OldPath, fc.Status, s.BaseRef, s.RepoRoot, false, s.VCS)
				scope.Files = append(scope.Files, StoryScopeFile{
					Path:    fc.Path,
					Status:  fc.Status,
					Hunks:   convertHunks(hunks),
					Ignored: true,
				})
			}
		}
	}

	return scope
}

func matchesAny(path string, patterns []string) bool {
	for _, p := range patterns {
		if config.MatchPattern(p, path) {
			return true
		}
	}
	return false
}

func convertHunks(hunks []vcs.DiffHunk) []StoryScopeHunk {
	out := make([]StoryScopeHunk, 0, len(hunks))
	for _, h := range hunks {
		lines := make([]StoryScopeLine, 0, len(h.Lines))
		for _, l := range h.Lines {
			lines = append(lines, StoryScopeLine{
				Type:    l.Type,
				Content: l.Content,
				OldNum:  l.OldNum,
				NewNum:  l.NewNum,
			})
		}
		out = append(out, StoryScopeHunk{
			OldStart: h.OldStart,
			OldCount: h.OldCount,
			NewStart: h.NewStart,
			NewCount: h.NewCount,
			Header:   h.Header,
			RawLines: lines,
		})
	}
	return out
}
