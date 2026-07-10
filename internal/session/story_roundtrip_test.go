package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildCritJSONPreservesStory verifies that an externally-set story on
// review.json survives the daemon's read-merge-modify write cycle, because
// Story is a field on CritJSON and buildCritJSON only overwrites known
// daemon-managed fields.
func TestBuildCritJSONPreservesStory(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	reviewPath := ReviewPathsFor(critPath).Review
	if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed a review.json that already carries a story (as `crit story` would).
	seeded := CritJSON{
		Branch:      "feature",
		BaseRef:     "abc",
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
		Story: &Story{
			Version:          1,
			ScopeFingerprint: "fp",
			Chapters: []StoryChapter{
				{ID: "ch1", Title: "Auth", HunkRefs: []StoryHunkRef{{FilePath: "a.go", OldStart: 3}}},
			},
			Coverage: &StoryCoverage{OK: true, Indexed: 1, Placed: 1},
		},
	}
	data, err := json.Marshal(seeded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// A daemon write cycle rebuilds the CritJSON from a snapshot. story is set
	// to the seeded value here to simulate a real session: loadCritJSONLocked
	// restores s.story from disk on load, and snapshotForWrite carries s.story
	// into every subsequent snapshot — a snapshot with a nil story represents
	// a session that never loaded one (or explicitly cleared it), not "leave
	// whatever is on disk alone".
	snap := writeFilesSnapshot{
		critPath:    critPath,
		branch:      "feature",
		baseRef:     "abc",
		reviewRound: 2, // daemon advances the round; story must still survive
		story:       seeded.Story,
	}
	cj := buildCritJSON(snap)

	if cj.Story == nil {
		t.Fatal("story dropped during buildCritJSON read-merge-modify")
	}
	if cj.Story.ScopeFingerprint != "fp" {
		t.Errorf("story fingerprint mutated: %q", cj.Story.ScopeFingerprint)
	}
	if len(cj.Story.Chapters) != 1 || cj.Story.Chapters[0].Title != "Auth" {
		t.Errorf("story chapters not preserved: %+v", cj.Story.Chapters)
	}
	// Daemon-managed fields still updated.
	if cj.ReviewRound != 2 {
		t.Errorf("review round not updated: got %d", cj.ReviewRound)
	}
}

// TestSyncWriteFilesPersistsNewStory verifies that setting a story via
// SetStory and flushing with SyncWriteFiles actually lands the new story on
// disk. buildCritJSON only preserves the story already on disk unless the
// write snapshot also carries the session's in-memory story — this is the
// daemon-side mutation path the /api/story handlers use (§6), distinct from
// TestBuildCritJSONPreservesStory above, which only exercises the passive
// preserve-across-unrelated-writes case.
func TestSyncWriteFilesPersistsNewStory(t *testing.T) {
	s := newTestSession(t)

	st := &Story{
		Version: 1,
		Chapters: []StoryChapter{
			{ID: "ch1", Title: "New", HunkRefs: []StoryHunkRef{{FilePath: "main.go", OldStart: 1}}},
		},
		Coverage: &StoryCoverage{OK: true, Indexed: 1, Placed: 1},
	}
	s.SetStory(st)
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles: %v", err)
	}

	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatalf("reading review file: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if cj.Story == nil || len(cj.Story.Chapters) != 1 || cj.Story.Chapters[0].ID != "ch1" {
		t.Fatalf("new story not persisted to disk: %+v", cj.Story)
	}
}

// TestSyncWriteFilesPersistsClearedStory verifies ClearStory + SyncWriteFiles
// removes a previously-saved story from disk (backs DELETE /api/story). With
// no comments/share state either, an all-empty CritJSON is represented by
// deleting review.json entirely (existing B1 empty-file behavior) rather than
// writing an empty object — so "cleared" means either absent or story-less.
func TestSyncWriteFilesPersistsClearedStory(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "test", "", "", "") // keep the file non-empty so this exercises Story specifically, not the B1 delete-on-empty path
	s.SetStory(&Story{Version: 1, Chapters: []StoryChapter{{ID: "ch1", Title: "T"}}})
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles (seed): %v", err)
	}

	s.ClearStory()
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles (clear): %v", err)
	}

	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatalf("reading review file: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if cj.Story != nil {
		t.Fatalf("expected story cleared on disk, got %+v", cj.Story)
	}
}

// TestStorySchemaJSONRoundTrip verifies the schema marshals and unmarshals
// losslessly, including omitempty behavior on the optional field.
func TestStorySchemaJSONRoundTrip(t *testing.T) {
	// No story -> "story" key absent (omitempty).
	empty := CritJSON{Files: map[string]CritJSONFile{}}
	b, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); containsKey(got, "story") {
		t.Errorf("empty CritJSON must not emit a story key: %s", got)
	}

	full := CritJSON{
		Files: map[string]CritJSONFile{},
		Story: &Story{
			Version:  1,
			Prologue: &StoryPrologue{Title: "T", Overview: "s", KeyChanges: []string{"k"}, Risks: []string{"r"}},
			Chapters: []StoryChapter{{ID: "ch1", Title: "T", HunkRefs: []StoryHunkRef{{FilePath: "f", OldStart: 0}}}},
			Support:  []StorySupportEntry{{HunkRefs: []StoryHunkRef{{FilePath: "g", OldStart: 9}}, Reason: "ignored"}},
			Coverage: &StoryCoverage{OK: false, Indexed: 2, Placed: 1, AutoRepaired: true},
		},
	}
	fb, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	var back CritJSON
	if err := json.Unmarshal(fb, &back); err != nil {
		t.Fatal(err)
	}
	if back.Story == nil || back.Story.Prologue == nil || back.Story.Prologue.Overview != "s" || len(back.Story.Prologue.KeyChanges) != 1 || len(back.Story.Prologue.Risks) != 1 {
		t.Fatalf("story round-trip lost data: %+v", back.Story)
	}
	if back.Story.Support[0].Reason != "ignored" {
		t.Errorf("support reason lost: %+v", back.Story.Support)
	}
}

func containsKey(jsonStr, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
