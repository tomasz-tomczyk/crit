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

// TestWhitespaceIgnoredHunks_RangeFallbacks verifies the guard rails of the
// range-focus recompute: without a VCS, without SHAs, or when the between-SHA
// diff itself fails, the cached hunks come back untouched.
func TestWhitespaceIgnoredHunks_RangeFallbacks(t *testing.T) {
	sentinel := []vcs.DiffHunk{{Header: "@@ sentinel @@"}}
	dir := initTestRepo(t)

	tests := []struct {
		name  string
		vc    vcs.VCS
		focus Focus
	}{
		{name: "nil vcs", vc: nil, focus: Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h"}},
		{name: "empty SHAs", vc: &vcs.GitVCS{}, focus: Focus{Kind: FocusRange}},
		{name: "bad SHAs", vc: &vcs.GitVCS{}, focus: Focus{Kind: FocusRange, BaseSHA: "deadbeef", HeadSHA: "beefdead"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhitespaceIgnoredHunks(sentinel, "modified", "", true, "code.go", "HEAD", dir, tt.vc, tt.focus)
			if len(got) != 1 || got[0].Header != sentinel[0].Header {
				t.Errorf("got %v, want cached sentinel back", got)
			}
		})
	}
}

// TestGetFileDiffSnapshot_RangeFullStackUsesDefaultSHA verifies the
// whitespace-ignored recompute honors the layer/full-stack scope: the change
// is real below the default SHA but whitespace-only above it, so layer w=1
// stays non-empty while full-stack w=1 collapses.
func TestGetFileDiffSnapshot_RangeFullStackUsesDefaultSHA(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn 1\n}\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "base")
	base := gitT(t, dir, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn 2\n}\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "default")
	def := gitT(t, dir, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\n\treturn 2\n}\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "head")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{Mode: "git", RepoRoot: dir, OutputDir: dir, VCS: &vcs.GitVCS{}}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	layer, ok := s.GetFileDiffSnapshot("code.go", true)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(code.go, true) failed in layer scope")
	}
	if len(layer["hunks"].([]vcs.DiffHunk)) == 0 {
		t.Error("layer w=1: got 0 hunks, want the base..head change")
	}

	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DefaultSHA: def, DiffScope: DiffScopeFullStack}); err != nil {
		t.Fatal(err)
	}
	raw, ok := s.GetFileDiffSnapshot("code.go", false)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(code.go, false) failed in full-stack scope")
	}
	if len(raw["hunks"].([]vcs.DiffHunk)) == 0 {
		t.Fatal("full-stack raw: got 0 hunks, want the whitespace-only change")
	}
	stack, ok := s.GetFileDiffSnapshot("code.go", true)
	if !ok {
		t.Fatal("GetFileDiffSnapshot(code.go, true) failed in full-stack scope")
	}
	if len(stack["hunks"].([]vcs.DiffHunk)) != 0 {
		t.Errorf("full-stack w=1: got %d hunks, want 0 (used base SHA instead of default)", len(stack["hunks"].([]vcs.DiffHunk)))
	}
}
