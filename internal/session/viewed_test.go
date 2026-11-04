package session

import (
	"encoding/json"
	"os"
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
			if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true}}); err != nil {
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
