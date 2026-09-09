package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestHandleRoundCompleteGit_RenameFollowsPersistedComments reproduces #917.
//
// Comments are keyed by path string. When the agent renames a file
// (old.go -> new.go) git reports Status="renamed" with OldPath="old.go".
// A comment that was already persisted under old.go in the review JSON
// (the debounced writer flushes on every change) must follow the rename and
// land on the new.go entry after round-complete.
//
// Today it does not: RefreshFileList only reuses an entry when
// existing[fc.Path] matches the NEW path, so new.go is created with empty
// Comments and the old.go entry is dropped. restoreOrphanedComments then reads
// the still-old.go-keyed review JSON and re-materialises the thread as an
// orphaned/removed phantom under old.go. The net effect is the review thread
// sticks to a dead path instead of the renamed file.
func TestHandleRoundCompleteGit_RenameFollowsPersistedComments(t *testing.T) {
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		branchChanges: []vcs.FileChange{
			{Path: "new.go", OldPath: "old.go", Status: "renamed"},
		},
		diffs: map[string][]vcs.DiffHunk{
			"new.go": {{OldStart: 1, NewStart: 1}},
		},
	}
	s := newWatchSession(t, v)
	s.Mode = "git"
	s.Branch = "feature"
	s.ReviewRound = 1
	s.subscribers = make(map[chan SSEEvent]struct{})

	// Post-rename, only new.go exists on disk; old.go is gone.
	if err := os.WriteFile(filepath.Join(s.RepoRoot, "new.go"), []byte("package new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The review JSON still keys the thread under the OLD path — this is what
	// the writer flushed before the rename was observed.
	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      "feature",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"old.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", Body: "this thread must follow the rename", Scope: "line", StartLine: 1, EndLine: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustMkdirAll(ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// In-memory the session still knows the file by its pre-rename path.
	s.Files = []*FileEntry{
		{Path: "old.go", AbsPath: filepath.Join(s.RepoRoot, "old.go"), Status: "modified"},
	}

	s.handleRoundCompleteGit()

	byPath := map[string]*FileEntry{}
	for _, f := range s.Files {
		byPath[f.Path] = f
	}

	// The renamed-into entry must exist and carry the persisted thread.
	newEntry := byPath["new.go"]
	if newEntry == nil {
		t.Fatalf("new.go missing from file list after rename round-complete")
	}
	if len(newEntry.Comments) == 0 {
		t.Errorf("new.go has 0 comments; the persisted thread from old.go should have followed the rename (#917)")
	}

	// And it must not be stranded as an orphaned phantom under the dead path.
	if old := byPath["old.go"]; old != nil && old.Orphaned && len(old.Comments) > 0 {
		t.Errorf("old.go was restored as an orphaned phantom holding %d comment(s); the thread should have moved to new.go instead (#917)", len(old.Comments))
	}
}
