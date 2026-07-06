package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildCritJSONPreservesStory is the back-compat property from spec §3.3 /
// §3.4: an externally-set story on review.json survives the daemon's
// read-merge-modify write cycle, because Story is a field on CritJSON and
// buildCritJSON only overwrites known daemon-managed fields.
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

	// A daemon write cycle rebuilds the CritJSON from a snapshot.
	snap := writeFilesSnapshot{
		critPath:    critPath,
		branch:      "feature",
		baseRef:     "abc",
		reviewRound: 2, // daemon advances the round; story must still survive
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

// TestStorySchemaJSONRoundTrip verifies the schema marshals and unmarshals
// losslessly, including omitempty behavior on the optional field.
func TestStorySchemaJSONRoundTrip(t *testing.T) {
	// No story -> "story" key absent (omitempty), so old readers see no change.
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
			Prologue: &StoryPrologue{Summary: "s", Complexity: "low", FocusAreas: []StoryFocus{{Area: "auth", Severity: "high"}}},
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
	if back.Story == nil || back.Story.Prologue == nil || back.Story.Prologue.FocusAreas[0].Area != "auth" {
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
