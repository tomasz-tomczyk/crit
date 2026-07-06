package story

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestFromScope_SplitsIndexedAndIgnored(t *testing.T) {
	scope := session.StoryScope{
		BaseSHA:        "base",
		HeadSHA:        "head",
		CommitMessages: []string{"do a thing"},
		Files: []session.StoryScopeFile{
			{
				Path:   "a.go",
				Status: "modified",
				Hunks: []session.StoryScopeHunk{
					{OldStart: 3, Header: "@@ -3 +3 @@", RawLines: []session.StoryScopeLine{
						{Type: "add", Content: "x := 1"},
						{Type: "context", Content: "return x"},
					}},
				},
			},
			{
				Path:    "go.sum",
				Status:  "modified",
				Ignored: true,
				Hunks:   []session.StoryScopeHunk{{OldStart: 10, Header: "@@ -10 +10 @@"}},
			},
		},
	}

	in, indexed, ignored := FromScope(scope)

	if len(indexed) != 1 || indexed[0] != (HunkID{FilePath: "a.go", OldStart: 3}) {
		t.Fatalf("indexed = %+v", indexed)
	}
	if len(ignored) != 1 || ignored[0] != (HunkID{FilePath: "go.sum", OldStart: 10}) {
		t.Fatalf("ignored = %+v", ignored)
	}
	// Ignored files must not appear in the prep text input.
	if len(in.Files) != 1 || in.Files[0].Path != "a.go" {
		t.Fatalf("prep input should carry only indexed files, got %+v", in.Files)
	}
	// The hunk body is reconstructed with +/space prefixes.
	prep := BuildPrep(in)
	if !containsAll(prep.Text, "+x := 1", " return x", "do a thing") {
		t.Errorf("prep text missing reconstructed body:\n%s", prep.Text)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
