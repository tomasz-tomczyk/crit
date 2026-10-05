package session

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestWhitespaceIgnoredHunks_ShortCircuits verifies that WhitespaceIgnoredHunks
// returns the cached hunks untouched for every case where a whitespace-ignored
// recompute can't change the result (flag off, all-added/untracked/deleted
// files, or no VCS), and only recomputes for a normal modified file.
func TestWhitespaceIgnoredHunks_ShortCircuits(t *testing.T) {
	sentinel := []vcs.DiffHunk{{Header: "@@ sentinel @@"}}

	tests := []struct {
		name           string
		status         string
		ignoreWS       bool
		vc             vcs.VCS
		wantCachedBack bool
	}{
		{name: "flag off", status: "modified", ignoreWS: false, vc: &vcs.GitVCS{}, wantCachedBack: true},
		{name: "added file", status: "added", ignoreWS: true, vc: &vcs.GitVCS{}, wantCachedBack: true},
		{name: "untracked file", status: "untracked", ignoreWS: true, vc: &vcs.GitVCS{}, wantCachedBack: true},
		{name: "deleted file", status: "deleted", ignoreWS: true, vc: &vcs.GitVCS{}, wantCachedBack: true},
		{name: "no vcs", status: "modified", ignoreWS: true, vc: nil, wantCachedBack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhitespaceIgnoredHunks(sentinel, tt.status, "", tt.ignoreWS, "code.go", "HEAD", t.TempDir(), tt.vc, Focus{})
			cachedBack := len(got) == 1 && got[0].Header == sentinel[0].Header
			if cachedBack != tt.wantCachedBack {
				t.Errorf("got %v (cachedBack=%v), want cachedBack=%v", got, cachedBack, tt.wantCachedBack)
			}
		})
	}

	t.Run("modified recomputes and collapses", func(t *testing.T) {
		dir := initTestRepo(t)
		writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn\n}\n")
		gitT(t, dir, "add", "code.go")
		gitT(t, dir, "commit", "-m", "add code.go")
		writeFile(t, filepath.Join(dir, "code.go"), "func main() {\n\treturn\n}\n")

		got := WhitespaceIgnoredHunks(sentinel, "modified", "", true, "code.go", "HEAD", dir, &vcs.GitVCS{}, Focus{})
		if len(got) != 0 {
			t.Errorf("recompute: got %d hunks, want 0 (whitespace-only change collapses)", len(got))
		}
	})
}

// TestGetFileDiffSnapshot_RangeIgnoreWhitespaceUsesRangeSHAs is a regression
// test for PR #1024: with ignoreWhitespace=true in range focus (--pr/--range),
// the diff must be recomputed between the focus SHAs, not between the working
// tree and baseRef. Before the fix, a clean working tree made every w=1
// response come back with 0 hunks while the headers still showed the raw
// counts, so PR files rendered as empty.
func TestGetFileDiffSnapshot_RangeIgnoreWhitespaceUsesRangeSHAs(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn 1\n}\n")
	writeFile(t, filepath.Join(dir, "ws.go"), "func main() {\nreturn\n}\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "base")
	base := gitT(t, dir, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn 2\n}\n")
	writeFile(t, filepath.Join(dir, "ws.go"), "func main() {\n\treturn\n}\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "head")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{Mode: "git", RepoRoot: dir, OutputDir: dir, VCS: &vcs.GitVCS{}}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	// Working tree is clean at head, so a working-tree recompute would be
	// empty for both files. The range diff must survive -w for the real
	// change and collapse for the whitespace-only change.
	raw, ok := s.GetFileDiffSnapshot("code.go", false)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(code.go, false) failed")
	}
	if len(raw["hunks"].([]vcs.DiffHunk)) == 0 {
		t.Fatal("expected raw range hunks for code.go")
	}
	ignored, ok := s.GetFileDiffSnapshot("code.go", true)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(code.go, true) failed")
	}
	if len(ignored["hunks"].([]vcs.DiffHunk)) == 0 {
		t.Error("w=1 in range focus returned 0 hunks for a non-whitespace change (used working tree instead of range SHAs)")
	}

	rawWS, ok := s.GetFileDiffSnapshot("ws.go", false)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(ws.go, false) failed")
	}
	if len(rawWS["hunks"].([]vcs.DiffHunk)) == 0 {
		t.Fatal("expected raw range hunks for ws.go")
	}
	ignoredWS, ok := s.GetFileDiffSnapshot("ws.go", true)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(ws.go, true) failed")
	}
	if len(ignoredWS["hunks"].([]vcs.DiffHunk)) != 0 {
		t.Errorf("w=1 in range focus: got %d hunks for a whitespace-only change, want 0", len(ignoredWS["hunks"].([]vcs.DiffHunk)))
	}
}
