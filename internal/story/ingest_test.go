package story

import (
	"errors"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// hunk is a tiny constructor for a HunkID in tests.
func hunk(path string, oldStart int) HunkID {
	return HunkID{FilePath: path, OldStart: oldStart}
}

// ref mirrors hunk but yields a StoryHunkRef for building stories.
func ref(path string, oldStart int) session.StoryHunkRef {
	return session.StoryHunkRef{FilePath: path, OldStart: oldStart}
}

func chapter(id, title string, refs ...session.StoryHunkRef) session.StoryChapter {
	return session.StoryChapter{ID: id, Title: title, HunkRefs: refs}
}

// baseScope builds an Ingest input where the story's ScopeFingerprint already
// matches the live diff, so drift never trips unless a test opts in.
func baseScope(t *testing.T, story *session.Story, indexed []HunkID) Ingest {
	t.Helper()
	if story.Prologue == nil {
		story.Prologue = validPrologue()
	}
	fp := Fingerprint(indexed)
	story.ScopeFingerprint = fp
	return Ingest{
		Story:           story,
		Indexed:         indexed,
		LiveFingerprint: fp,
	}
}

func validPrologue() *session.StoryPrologue {
	return &session.StoryPrologue{
		Title:      "Test story",
		Overview:   "A test story.",
		KeyChanges: []string{"A key change."},
		Risks:      []string{"A test risk."},
	}
}

func TestIngest_InvalidPrologueReject(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "All", ref("a.go", 1))},
	}
	fp := Fingerprint(indexed)
	story.ScopeFingerprint = fp

	res, err := Run(Ingest{
		Story:           story,
		Indexed:         indexed,
		LiveFingerprint: fp,
	})
	if !errors.Is(err, ErrInvalidPrologue) {
		t.Fatalf("expected invalid prologue rejection, got %v", err)
	}
	if res.Saved {
		t.Fatal("invalid prologue must not save")
	}
}

func TestIngest_InvalidChapterIDsReject(t *testing.T) {
	tests := []struct {
		name     string
		chapters []session.StoryChapter
	}{
		{"duplicate", []session.StoryChapter{chapter("same", "One"), chapter("same", "Two")}},
		{"reserved", []session.StoryChapter{chapter("support", "Support")}},
		{"slash", []session.StoryChapter{chapter("part/one", "Part")}},
		{"empty", []session.StoryChapter{chapter("", "Empty")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &session.Story{Prologue: validPrologue(), Chapters: tt.chapters}
			if _, err := Run(baseScope(t, st, nil)); !errors.Is(err, ErrInvalidChapterID) {
				t.Fatalf("expected ErrInvalidChapterID, got %v", err)
			}
		})
	}
}

func TestIngest_DriftReject(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1), hunk("b.go", 10)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "All", ref("a.go", 1), ref("b.go", 10))},
	}
	in := baseScope(t, story, indexed)
	// Working tree moved since prep: the live fingerprint no longer matches.
	in.LiveFingerprint = "deadbeef"

	res, err := Run(in)
	if err == nil {
		t.Fatal("expected drift rejection, got nil error")
	}
	if res.Saved {
		t.Fatal("drift must not save")
	}
	if res.Coverage == nil {
		t.Fatal("coverage report must be present even on drift reject")
	}
}

func TestIngest_DuplicateReject(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1), hunk("b.go", 10)}
	story := &session.Story{
		Version: 1,
		Chapters: []session.StoryChapter{
			chapter("ch1", "One", ref("a.go", 1)),
			// a.go:1 claimed twice -> incoherent story.
			chapter("ch2", "Two", ref("a.go", 1), ref("b.go", 10)),
		},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
	if res.Saved {
		t.Fatal("duplicate must not save")
	}
	if len(res.Coverage.Duplicated) == 0 {
		t.Fatalf("coverage.duplicated must list the offending hunk, got %+v", res.Coverage)
	}
}

func TestIngest_FloorReject(t *testing.T) {
	// 4 hunks indexed, only 1 placed (25% < 50%): reject.
	indexed := []HunkID{hunk("a.go", 1), hunk("b.go", 10), hunk("c.go", 20), hunk("d.go", 30)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "One", ref("a.go", 1))},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err == nil {
		t.Fatal("expected floor rejection")
	}
	if res.Saved {
		t.Fatal("below-floor story must not save")
	}
	if res.Coverage.Placed != 1 || res.Coverage.Indexed != 4 {
		t.Fatalf("unexpected coverage counts: %+v", res.Coverage)
	}
}

func TestIngest_AutoRepairAtFloor(t *testing.T) {
	// 4 hunks, 2 placed (exactly 50%): repair the rest into support[].
	indexed := []HunkID{hunk("a.go", 1), hunk("b.go", 10), hunk("c.go", 20), hunk("d.go", 30)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "Half", ref("a.go", 1), ref("b.go", 10))},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err != nil {
		t.Fatalf("expected save-with-repair, got error: %v", err)
	}
	if !res.Saved {
		t.Fatal("at-floor story must save")
	}
	if !res.Coverage.AutoRepaired {
		t.Fatal("coverage.auto_repaired must be true")
	}
	if res.Coverage.OK {
		t.Fatal("saved-with-repairs must be ok:false")
	}
	// The two missing hunks must land in a single support entry with the sentinel reason.
	var repaired *session.StorySupportEntry
	for i := range story.Support {
		if story.Support[i].Reason == ReasonAutoRepaired {
			repaired = &story.Support[i]
		}
	}
	if repaired == nil {
		t.Fatal("expected an auto-repaired support entry")
	} else if len(repaired.HunkRefs) != 2 {
		t.Fatalf("expected 2 back-filled hunks, got %d", len(repaired.HunkRefs))
	}
}

func TestIngest_IgnoredPrePlacement(t *testing.T) {
	// Ignored files are pre-placed into support[] and counted as placed, so a
	// story that only covers the non-ignored hunk still passes cleanly.
	indexed := []HunkID{hunk("a.go", 1)}
	ignored := []HunkID{hunk("pkg.lock", 5), hunk("pkg.lock", 40)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "Real", ref("a.go", 1))},
	}
	in := baseScope(t, story, indexed)
	in.Ignored = ignored

	res, err := Run(in)
	if err != nil {
		t.Fatalf("expected clean save, got error: %v", err)
	}
	if !res.Saved {
		t.Fatal("must save")
	}
	if !res.Coverage.OK {
		t.Fatalf("ignored pre-placement should not count as a repair; coverage=%+v", res.Coverage)
	}
	var ign *session.StorySupportEntry
	for i := range story.Support {
		if story.Support[i].Reason == ReasonIgnored {
			ign = &story.Support[i]
		}
	}
	if ign == nil || len(ign.HunkRefs) != 2 {
		t.Fatalf("expected ignored support entry with 2 hunks, got %+v", story.Support)
	}
}

func TestIngest_CleanPass(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1), hunk("b.go", 10)}
	story := &session.Story{
		Version: 1,
		Chapters: []session.StoryChapter{
			chapter("ch1", "A", ref("a.go", 1)),
			chapter("ch2", "B", ref("b.go", 10)),
		},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err != nil {
		t.Fatalf("expected clean save, got error: %v", err)
	}
	if !res.Saved {
		t.Fatal("clean story must save")
	}
	if !res.Coverage.OK {
		t.Fatalf("zero-repair story must be ok:true, got %+v", res.Coverage)
	}
	if res.Coverage.AutoRepaired {
		t.Fatal("clean story must not report auto_repaired")
	}
	if res.Coverage.Placed != 2 || res.Coverage.Indexed != 2 {
		t.Fatalf("unexpected counts: %+v", res.Coverage)
	}
}

func TestIngest_CapsStoryTitles(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1)}
	longTitle := "This is a deliberately long chapter title that should not fit in the story rail"
	story := &session.Story{
		Version: 1,
		Prologue: &session.StoryPrologue{
			Title:      longTitle,
			Overview:   "A test story.",
			KeyChanges: []string{"A key change."},
			Risks:      []string{"A test risk."},
		},
		Chapters: []session.StoryChapter{chapter("ch1", longTitle, ref("a.go", 1))},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err != nil {
		t.Fatalf("expected clean save, got error: %v", err)
	}
	if !res.Saved {
		t.Fatal("story must save")
	}
	if got := len([]rune(story.Chapters[0].Title)); got != MaxChapterTitleRunes {
		t.Fatalf("chapter title length = %d, want %d; title=%q", got, MaxChapterTitleRunes, story.Chapters[0].Title)
	}
	if got := len([]rune(story.Prologue.Title)); got != MaxChapterTitleRunes {
		t.Fatalf("prologue title length = %d, want %d; title=%q", got, MaxChapterTitleRunes, story.Prologue.Title)
	}
	if story.Chapters[0].Title == longTitle {
		t.Fatal("long chapter title was not capped")
	}
	if story.Prologue.Title == longTitle {
		t.Fatal("long prologue title was not capped")
	}
}

func TestIngest_SupportPlacementCountsAsPlaced(t *testing.T) {
	// A hunk the author intentionally files under support[] (e.g. mechanical
	// churn) counts as placed and must not be auto-repaired.
	indexed := []HunkID{hunk("a.go", 1), hunk("gen.go", 100)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "A", ref("a.go", 1))},
		Support: []session.StorySupportEntry{
			{HunkRefs: []session.StoryHunkRef{ref("gen.go", 100)}, Reason: "Generated code."},
		},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err != nil {
		t.Fatalf("expected clean save, got error: %v", err)
	}
	if !res.Coverage.OK {
		t.Fatalf("author-placed support must be a clean pass, got %+v", res.Coverage)
	}
}

func TestIngest_DuplicateAcrossChapterAndSupport(t *testing.T) {
	indexed := []HunkID{hunk("a.go", 1)}
	story := &session.Story{
		Version:  1,
		Chapters: []session.StoryChapter{chapter("ch1", "A", ref("a.go", 1))},
		Support: []session.StorySupportEntry{
			{HunkRefs: []session.StoryHunkRef{ref("a.go", 1)}, Reason: "dup"},
		},
	}
	in := baseScope(t, story, indexed)

	res, err := Run(in)
	if err == nil {
		t.Fatal("a hunk in both a chapter and support is a duplicate: reject")
	}
	if res.Saved {
		t.Fatal("must not save on duplicate")
	}
}
