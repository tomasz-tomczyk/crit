package session

import (
	"strings"

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
//
// BaseSHA/HeadSHA are resolved to stable commit SHAs (not branch refs) so they
// can be persisted on the Story and interpolated into the on_story_generate
// prompt. HeadSHA stays empty for working-tree scopes (the tree is the head).
type StoryScope struct {
	BaseSHA        string
	HeadSHA        string
	MergeBaseSHA   string
	CommitMessages []string
	PRNumber       int              // >0 only for --pr scopes
	PRURL          string           // populated only for --pr scopes
	PRTitle        string           // populated when the branch/scope has a corresponding PR
	PRBody         string           // populated when the branch/scope has a corresponding PR
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
		scope.PRNumber = s.Focus.PRNumber
		scope.PRURL = s.Focus.PRURL
	}

	// CommitLog runs against the (possibly symbolic) base ref before we resolve
	// it below, so the range is unchanged whether or not rev-parse succeeds.
	if s.VCS != nil {
		headRef := scope.HeadSHA // "" => working-tree HEAD
		if commits, err := s.VCS.CommitLog(scope.BaseSHA, headRef, s.RepoRoot); err == nil {
			for _, c := range commits {
				scope.CommitMessages = append(scope.CommitMessages, c.Message)
			}
		}
	}

	// Resolve base (and, for ranges, head) to stable commit SHAs. In git mode
	// BaseRef is often a branch ref like "main"; the persisted Story and the
	// prompt variables want a pinned SHA. rev-parse failures leave the ref
	// as-is rather than blocking the scope.
	scope.BaseSHA = resolveSHA(s.RepoRoot, scope.BaseSHA)
	if scope.HeadSHA != "" {
		scope.HeadSHA = resolveSHA(s.RepoRoot, scope.HeadSHA)
	}
	if scope.HeadSHA != "" {
		if mb, err := vcs.MergeBaseOf(scope.BaseSHA, scope.HeadSHA, s.RepoRoot); err == nil {
			scope.MergeBaseSHA = mb
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

// resolveSHA turns a ref (branch, tag, "HEAD~2") into a full commit SHA. On
// any failure (empty ref, not a git repo, unknown ref) it returns ref
// unchanged — the caller treats the raw ref as a best-effort scope marker.
func resolveSHA(repoRoot, ref string) string {
	if ref == "" {
		return ref
	}
	out, err := vcs.RunGitInDir(repoRoot, "rev-parse", ref)
	if err != nil {
		return ref
	}
	if sha := strings.TrimSpace(out); sha != "" {
		return sha
	}
	return ref
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
