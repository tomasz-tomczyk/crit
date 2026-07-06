package story

import (
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// FromScope converts a session's neutral StoryScope snapshot into the inputs
// the story package works with: prep-text input, and the indexed vs ignored
// hunk-ID sets for coverage.
func FromScope(scope session.StoryScope) (PrepInput, []HunkID, []HunkID) {
	in := PrepInput{
		BaseSHA:        scope.BaseSHA,
		HeadSHA:        scope.HeadSHA,
		CommitMessages: scope.CommitMessages,
	}
	var indexed, ignored []HunkID

	for _, f := range scope.Files {
		if f.Ignored {
			for _, h := range f.Hunks {
				ignored = append(ignored, HunkID{FilePath: f.Path, OldStart: h.OldStart})
			}
			continue
		}
		pf := PrepFile{Path: f.Path, Status: f.Status}
		for _, h := range f.Hunks {
			indexed = append(indexed, HunkID{FilePath: f.Path, OldStart: h.OldStart})
			pf.Hunks = append(pf.Hunks, PrepHunk{
				OldStart: h.OldStart,
				OldCount: h.OldCount,
				NewStart: h.NewStart,
				NewCount: h.NewCount,
				Header:   h.Header,
				Body:     renderHunkBody(h),
			})
		}
		in.Files = append(in.Files, pf)
	}

	return in, indexed, ignored
}

// renderHunkBody reconstructs the unified-diff line body (prefixed +/-/space)
// from the parsed hunk lines, so the prep text carries the actual change.
func renderHunkBody(h session.StoryScopeHunk) string {
	var b strings.Builder
	for _, l := range h.RawLines {
		switch l.Type {
		case "add":
			b.WriteString("+")
		case "del":
			b.WriteString("-")
		default:
			b.WriteString(" ")
		}
		b.WriteString(l.Content)
		b.WriteString("\n")
	}
	return b.String()
}
