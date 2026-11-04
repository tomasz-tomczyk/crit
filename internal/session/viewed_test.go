package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func viewedTestSession(t *testing.T) *Session {
	t.Helper()
	s := newTestSession(t)
	s.OutputDir = t.TempDir()
	return s
}

func TestSetViewedFiles_CreatesReviewWithoutComments(t *testing.T) {
	s := viewedTestSession(t)
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, FileHash: "sha256:a"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !got["main.go"].Viewed || got["main.go"].FileHash != "sha256:a" {
		t.Fatalf("mark not saved: %+v", got)
	}
}

func TestViewedFiles_SurviveDaemonWrites(t *testing.T) {
	s := viewedTestSession(t)
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true}}); err != nil {
		t.Fatal(err)
	}
	// An ordinary write with no comments must neither drop the mark nor
	// remove the review as empty.
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !got["main.go"].Viewed {
		t.Fatalf("mark lost by a daemon write: %+v", got)
	}
}

func TestViewedFiles_KeepExternalWrite(t *testing.T) {
	s := viewedTestSession(t)
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true}}); err != nil {
		t.Fatal(err)
	}
	// What `crit pull --viewed` does from another process.
	review := ReviewPathsFor(s.critJSONPath()).Review
	data, err := os.ReadFile(review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	cj.ViewedFiles["plan.md"] = ViewedFile{Viewed: true, HeadSHA: "abc", ChangeNumber: 7}
	if err := SaveCritJSON(s.critJSONPath(), cj); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if got["plan.md"].HeadSHA != "abc" || !got["main.go"].Viewed {
		t.Fatalf("external mark lost: %+v", got)
	}
}

func TestViewedFiles_NoReviewFile(t *testing.T) {
	s := viewedTestSession(t)
	got, err := s.ViewedFiles()
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, statErr := os.Stat(ReviewPathsFor(s.critJSONPath()).Review); !os.IsNotExist(statErr) {
		t.Fatalf("reading must not create a review: %v", statErr)
	}
}

func TestSetViewedFiles_PinsToFocus(t *testing.T) {
	tests := []struct {
		name       string
		focus      Focus
		wantHead   string
		wantChange int
	}{
		{"pr layer", Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "abc", DiffScope: DiffScopeLayer}, "abc", 7},
		{"pr without scope", Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "abc"}, "abc", 7},
		{"full stack", Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "abc", DiffScope: DiffScopeFullStack}, "", 0},
		{"plain range", Focus{Kind: FocusRange, HeadSHA: "abc"}, "", 0},
		{"working tree", Focus{}, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := viewedTestSession(t)
			s.Focus = tt.focus
			if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, HeadSHA: tt.wantHead}}); err != nil {
				t.Fatal(err)
			}
			got, err := s.ViewedFiles()
			if err != nil {
				t.Fatal(err)
			}
			if e := got["main.go"]; e.HeadSHA != tt.wantHead || e.ChangeNumber != tt.wantChange {
				t.Fatalf("got %+v", e)
			}
		})
	}
}

func TestSetViewedFiles_UntickIsRecordedAndClearsAuto(t *testing.T) {
	s := viewedTestSession(t)
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, Auto: true}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: false, Auto: true}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["main.go"]
	if !ok || e.Viewed || e.Auto {
		t.Fatalf("got %+v, present=%v", e, ok)
	}
}

func TestMergeCritJSON3_KeepsViewedFilesFromOtherWriter(t *testing.T) {
	base := CritJSON{Files: map[string]CritJSONFile{}}
	mine := CloneCritJSON(base)
	theirs := CloneCritJSON(base)
	theirs.ViewedFiles = map[string]ViewedFile{"main.go": {Viewed: true}}
	merged, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.ViewedFiles["main.go"].Viewed {
		t.Fatalf("a forge push must not drop marks saved meanwhile: %+v", merged.ViewedFiles)
	}
}

func TestSetViewedFiles_OtherRevisionStaysLocal(t *testing.T) {
	s := viewedTestSession(t)
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "new"}
	// The page still shows the old head: the focus moved before its reload.
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, HeadSHA: "old"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if e := got["main.go"]; !e.Viewed || e.HeadSHA != "" || e.ChangeNumber != 0 {
		t.Fatalf("a mark on another revision must not be pinned to the focus: %+v", e)
	}
}

func TestSetViewedFiles_AfterExternalDeletion(t *testing.T) {
	s := viewedTestSession(t)
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ReviewPathsFor(s.critJSONPath()).Review); err != nil {
		t.Fatal(err)
	}
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "plan.md", Viewed: true}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !got["plan.md"].Viewed {
		t.Fatalf("mark dropped after the review was deleted: %+v", got)
	}
}

func TestCleanupOnApproval_KeepsPushableMarksOnly(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "review")
	cj := CritJSON{
		Branch:      "feature",
		ReviewRound: 3,
		ShareURL:    "https://crit.example/r/abc",
		Files: map[string]CritJSONFile{"a.go": {Status: "modified", Comments: []Comment{{
			ID: "c1", Body: "done", Resolved: true,
		}}}},
		ViewedFiles: map[string]ViewedFile{
			"pinned.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7},
			"untick.go": {Viewed: false, HeadSHA: "head", ChangeNumber: 7},
			"auto.go":   {Viewed: true, Auto: true, HeadSHA: "head", ChangeNumber: 7},
			"local.go":  {Viewed: true},
		},
	}
	if err := SaveCritJSON(identity, cj); err != nil {
		t.Fatal(err)
	}
	snapshots := ReviewPathsFor(identity).Snapshots
	if err := os.WriteFile(snapshots, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupOnApproval(true, identity, true)

	data, err := os.ReadFile(ReviewPathsFor(identity).Review)
	if err != nil {
		t.Fatalf("pushable marks must outlive approval: %v", err)
	}
	var got CritJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.ViewedFiles) != 2 || !got.ViewedFiles["pinned.go"].Viewed {
		t.Fatalf("marks: %+v", got.ViewedFiles)
	}
	if _, kept := got.ViewedFiles["untick.go"]; !kept {
		t.Fatal("an untick is pushable too")
	}
	if len(got.Files) != 0 || got.ShareURL != "" || got.ReviewRound != 1 || got.Branch != "feature" {
		t.Fatalf("approval must still clear the review: %+v", got)
	}
	if _, err := os.Stat(snapshots); !os.IsNotExist(err) {
		t.Fatalf("approval must still clear round snapshots: %v", err)
	}
}

func TestCleanupOnApproval_RemovesReviewWithoutPushableMarks(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "review")
	cj := CritJSON{Files: map[string]CritJSONFile{}, ViewedFiles: map[string]ViewedFile{
		"auto.go":  {Viewed: true, Auto: true, HeadSHA: "head", ChangeNumber: 7},
		"local.go": {Viewed: true},
	}}
	if err := SaveCritJSON(identity, cj); err != nil {
		t.Fatal(err)
	}
	cleanupOnApproval(true, identity, true)
	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Fatalf("review should be gone: %v", err)
	}
}
