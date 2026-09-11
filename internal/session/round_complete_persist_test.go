package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestHandleRoundCompleteGit_PersistsCarriedComments guards the interrupt
// failure mode: Round N+1 showed in the UI while review.json stayed on Round N
// with uncarried comments, so remaps never stuck and drift never appeared.
func TestHandleRoundCompleteGit_PersistsCarriedComments(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	reviewPath := filepath.Join(identity, "review.json")
	abs := filepath.Join(dir, "main.go")
	oldContent := "line one\nline two\nline three\n"
	newContent := "line one\ninserted\nline two\nline three\n"
	if err := os.WriteFile(abs, []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}

	oldComment := Comment{
		ID:          "c_old1",
		StartLine:   2,
		EndLine:     2,
		Body:        "about line two",
		Author:      "Reviewer",
		Scope:       "line",
		Anchor:      "line two",
		CreatedAt:   "2026-04-13T10:00:00Z",
		UpdatedAt:   "2026-04-13T10:00:00Z",
		ReviewRound: 1,
	}
	seed := CritJSON{
		Branch:      "feature",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {Status: "modified", Comments: []Comment{oldComment}},
		},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		branchChanges: []vcs.FileChange{{Path: "main.go", Status: "modified"}},
		diffs:         map[string][]vcs.DiffHunk{"main.go": {{OldStart: 1, NewStart: 1}}},
	}
	s := &Session{
		Mode:           "git",
		VCS:            v,
		RepoRoot:       dir,
		BaseRef:        "main",
		Branch:         "feature",
		ReviewFilePath: identity,
		ReviewRound:    1,
		subscribers:    make(map[chan SSEEvent]struct{}),
		Files: []*FileEntry{{
			Path:     "main.go",
			AbsPath:  abs,
			Status:   "modified",
			FileType: "code",
			Content:  oldContent,
			Comments: []Comment{oldComment},
		}},
	}

	s.handleRoundCompleteGit()

	if s.ReviewRound != 2 {
		t.Fatalf("ReviewRound = %d, want 2", s.ReviewRound)
	}
	got := s.GetComments("main.go")
	if len(got) != 1 {
		t.Fatalf("memory comments = %d, want 1", len(got))
	}
	if !got[0].CarriedForward {
		t.Fatal("expected carried_forward comment in memory")
	}
	if got[0].ID == "c_old1" {
		t.Fatal("carried comment kept old ID; expected a new ID")
	}
	if got[0].StartLine != 3 || got[0].EndLine != 3 {
		t.Fatalf("remapped lines = %d-%d, want 3-3", got[0].StartLine, got[0].EndLine)
	}
	if got[0].Drifted {
		t.Fatal("anchor still present; did not expect drifted")
	}

	raw, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review.json: %v", err)
	}
	var disk CritJSON
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal review.json: %v", err)
	}
	if disk.ReviewRound != 2 {
		t.Fatalf("disk ReviewRound = %d, want 2", disk.ReviewRound)
	}
	diskComments := disk.Files["main.go"].Comments
	if len(diskComments) != 1 {
		t.Fatalf("disk comments = %d, want 1", len(diskComments))
	}
	if !diskComments[0].CarriedForward {
		t.Fatal("disk comment missing carried_forward")
	}
	if diskComments[0].ID != got[0].ID {
		t.Fatalf("disk ID %q != memory ID %q", diskComments[0].ID, got[0].ID)
	}
	if diskComments[0].StartLine != 3 {
		t.Fatalf("disk StartLine = %d, want 3", diskComments[0].StartLine)
	}
}

// TestMergeExternalCritJSON_SkipsWhenMemoryRoundAhead ensures a stale
// review.json cannot replace carried-forward comments after the in-memory
// round has already advanced.
func TestMergeExternalCritJSON_SkipsWhenMemoryRoundAhead(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	reviewPath := filepath.Join(identity, "review.json")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}

	carried := Comment{
		ID:             "c_new",
		StartLine:      3,
		EndLine:        3,
		Body:           "about line two",
		Author:         "Reviewer",
		Anchor:         "line two",
		CarriedForward: true,
		ReviewRound:    1,
		CreatedAt:      "2026-04-13T10:00:00Z",
		UpdatedAt:      "2026-04-13T10:05:00Z",
	}
	s := &Session{
		RepoRoot:       dir,
		ReviewFilePath: identity,
		ReviewRound:    2,
		Branch:         "feature",
		Files: []*FileEntry{{
			Path:     "main.go",
			Status:   "modified",
			Comments: []Comment{carried},
		}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	stale := CritJSON{
		Branch:      "feature",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{{
					ID:          "c_old1",
					StartLine:   2,
					EndLine:     2,
					Body:        "about line two",
					Author:      "Reviewer",
					Anchor:      "line two",
					ReviewRound: 1,
					CreatedAt:   "2026-04-13T10:00:00Z",
					UpdatedAt:   "2026-04-13T10:00:00Z",
				}},
			},
		},
	}
	data, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Force mtime newer than lastCritJSONMtime zero value.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(reviewPath, future, future); err != nil {
		t.Fatal(err)
	}

	if s.mergeExternalCritJSON() {
		t.Fatal("expected merge to skip when memory round is ahead of disk")
	}
	got := s.GetComments("main.go")
	if len(got) != 1 || got[0].ID != "c_new" || !got[0].CarriedForward {
		t.Fatalf("memory comments clobbered: %+v", got)
	}
}

func TestMergeExternalCritJSON_AppliesWhenDiskRoundMatches(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	reviewPath := filepath.Join(identity, "review.json")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Session{
		RepoRoot:       dir,
		ReviewFilePath: identity,
		ReviewRound:    2,
		Branch:         "feature",
		Files: []*FileEntry{{
			Path:     "main.go",
			Status:   "modified",
			Comments: []Comment{},
		}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	disk := CritJSON{
		Branch:      "feature",
		ReviewRound: 2,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{{
					ID:             "c_cli",
					StartLine:      1,
					EndLine:        1,
					Body:           "from CLI",
					Author:         "Claude",
					CarriedForward: true,
					ReviewRound:    1,
					CreatedAt:      "2026-04-13T10:00:00Z",
					UpdatedAt:      "2026-04-13T10:00:00Z",
				}},
			},
		},
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(reviewPath, future, future); err != nil {
		t.Fatal(err)
	}

	if !s.mergeExternalCritJSON() {
		t.Fatal("expected merge to apply matching-round disk comments")
	}
	got := s.GetComments("main.go")
	if len(got) != 1 || got[0].ID != "c_cli" {
		t.Fatalf("expected CLI comment merged, got %+v", got)
	}
}

// TestMergeExternalCritJSON_AppliesWhenDiskRoundUnset guards share-merge /
// legacy review.json files that omit review_round (JSON zero). Memory is
// normally at least 1; treating disk 0 as "stale" would drop web comments.
func TestMergeExternalCritJSON_AppliesWhenDiskRoundUnset(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	reviewPath := filepath.Join(identity, "review.json")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Session{
		RepoRoot:       dir,
		ReviewFilePath: identity,
		ReviewRound:    1,
		Branch:         "feature",
		Files: []*FileEntry{{
			Path:     "main.go",
			Status:   "modified",
			Comments: []Comment{},
		}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	disk := CritJSON{
		Branch: "feature",
		// ReviewRound intentionally unset (0)
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{{
					ID:        "c_web",
					StartLine: 1,
					EndLine:   1,
					Body:      "from web",
					Author:    "Web User",
					CreatedAt: "2026-04-13T10:00:00Z",
					UpdatedAt: "2026-04-13T10:00:00Z",
				}},
			},
		},
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(reviewPath, future, future); err != nil {
		t.Fatal(err)
	}

	if !s.mergeExternalCritJSON() {
		t.Fatal("expected merge to apply unset-round disk comments")
	}
	got := s.GetComments("main.go")
	if len(got) != 1 || got[0].ID != "c_web" {
		t.Fatalf("expected web comment merged, got %+v", got)
	}
}
