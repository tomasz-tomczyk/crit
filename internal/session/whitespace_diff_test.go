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
			got := WhitespaceIgnoredHunks(sentinel, tt.status, "", tt.ignoreWS, "code.go", "HEAD", t.TempDir(), tt.vc)
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

		got := WhitespaceIgnoredHunks(sentinel, "modified", "", true, "code.go", "HEAD", dir, &vcs.GitVCS{})
		if len(got) != 0 {
			t.Errorf("recompute: got %d hunks, want 0 (whitespace-only change collapses)", len(got))
		}
	})
}
