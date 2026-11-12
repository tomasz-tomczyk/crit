package session

import (
	"path/filepath"
	"testing"
)

// A branch rebased onto a newer main between rounds must be diffed against the
// new merge-base in the next round, not the one captured at session start.
// Otherwise every commit main gained shows up as part of the review.
func TestHandleRoundCompleteGit_RebaseOntoNewerMainRefreshesBaseRef(t *testing.T) {
	dir := newAllScopeRepo(t)
	writeFile(t, filepath.Join(dir, "feature.go"), "package sample\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "feature work")
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)
	oldBase := sess.BaseRef
	if oldBase == "" {
		t.Fatal("expected a merge-base BaseRef on the feature branch")
	}

	// main moves on, then the feature branch is rebased onto it.
	gitT(t, dir, "checkout", "main")
	writeFile(t, filepath.Join(dir, "upstream.go"), "package sample\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "upstream work")
	gitT(t, dir, "checkout", "feature")
	gitT(t, dir, "rebase", "main")
	newBase := gitT(t, dir, "merge-base", "HEAD", "main")

	sess.handleRoundCompleteGit()

	if sess.BaseRef != newBase {
		t.Errorf("BaseRef = %s, want new merge-base %s (old %s)", sess.BaseRef, newBase, oldBase)
	}
	if sess.Focus.BaseRef != newBase {
		t.Errorf("Focus.BaseRef = %s, want %s", sess.Focus.BaseRef, newBase)
	}
	for _, f := range sess.Files {
		if f.Path == "upstream.go" {
			t.Errorf("upstream.go from main leaked into the round-2 diff: %+v", sess.Files)
		}
	}
	found := false
	for _, f := range sess.Files {
		if f.Path == "feature.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("feature.go missing from round-2 files: %+v", sess.Files)
	}
}
