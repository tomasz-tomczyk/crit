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
	cj.ViewedFiles["plan.md"] = ViewedFile{Viewed: true, FileHash: "sha256:p"}
	cj.ChangeViewedFiles = map[string]map[string]ViewedFile{"pr:7": {"x.go": {Viewed: true, HeadSHA: "abc", ChangeNumber: 7}}}
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
	if got["plan.md"].FileHash != "sha256:p" || !got["main.go"].Viewed {
		t.Fatalf("external mark lost: %+v", got)
	}
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "abc"}
	if got, _ := s.ViewedFiles(); got["x.go"].HeadSHA != "abc" {
		t.Fatalf("external PR mark lost: %+v", got)
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

func TestSetViewedFiles_SamePathInDifferentLayers(t *testing.T) {
	s := viewedTestSession(t)
	layer7 := Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "h7", DiffScope: DiffScopeLayer, IsStacked: true}
	layer8 := Focus{Kind: FocusRange, ChangeNumber: 8, HeadSHA: "h8", DiffScope: DiffScopeLayer, IsStacked: true}

	s.Focus = layer7
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, HeadSHA: "h7"}}); err != nil {
		t.Fatal(err)
	}
	s.Focus = layer8
	if got, _ := s.ViewedFiles(); len(got) != 0 {
		t.Fatalf("layer 8 shows layer 7's marks: %+v", got)
	}
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: false, HeadSHA: "h8"}}); err != nil {
		t.Fatal(err)
	}

	s.Focus = layer7
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if e := got["main.go"]; !e.Viewed || e.HeadSHA != "h7" || e.ChangeNumber != 7 {
		t.Fatalf("layer 8's untick overwrote layer 7's mark: %+v", e)
	}
	s.Focus = layer8
	if e := mustViewed(t, s)["main.go"]; e.Viewed || e.HeadSHA != "h8" || e.ChangeNumber != 8 {
		t.Fatalf("layer 8 mark: %+v", e)
	}
	s.Focus = Focus{}
	if got := mustViewed(t, s); len(got) != 0 {
		t.Fatalf("working tree shows PR marks: %+v", got)
	}
}

func TestSetViewedFiles_SameNumberInDifferentRepositories(t *testing.T) {
	s := viewedTestSession(t)
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "a", RemoteBaseProject: "acme/widget"}
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "main.go", Viewed: true, HeadSHA: "a"}}); err != nil {
		t.Fatal(err)
	}
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "b", RemoteBaseProject: "other/thing"}
	if got := mustViewed(t, s); len(got) != 0 {
		t.Fatalf("another repository's PR 7 shows these marks: %+v", got)
	}
	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if !cj.ChangeViewedFiles["pr:acme/widget#7"]["main.go"].Viewed {
		t.Fatalf("mark not keyed by repository: %+v", cj.ChangeViewedFiles)
	}
}

func TestViewedFiles_LegacyPinnedMarks(t *testing.T) {
	s := viewedTestSession(t)
	// A review written before marks were keyed by change: each PR mark knows
	// its PR number and head commit, but not its repository.
	legacy := CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{}, ViewedFiles: map[string]ViewedFile{
		"main.go":  {Viewed: true, FileHash: "sha256:a", HeadSHA: "h7", ChangeNumber: 7},
		"other.go": {Viewed: true, HeadSHA: "h8", ChangeNumber: 8},
		"old.go":   {Viewed: true, HeadSHA: "stale", ChangeNumber: 7},
		"local.go": {Viewed: true},
	}}
	if err := SaveCritJSON(s.critJSONPath(), legacy); err != nil {
		t.Fatal(err)
	}

	// Outside a PR only the local mark shows.
	if got := mustViewed(t, s); len(got) != 1 || !got["local.go"].Viewed {
		t.Fatalf("working tree: %+v", got)
	}

	// PR 7 at h7 vouches for the mark made on exactly that revision.
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 7, HeadSHA: "h7"}
	got := mustViewed(t, s)
	if e := got["main.go"]; !e.Viewed || e.HeadSHA != "h7" || e.FileHash != "sha256:a" || len(got) != 1 {
		t.Fatalf("PR 7 view: %+v", got)
	}
	if pushableViewedReview(s.critJSONPath()) != nil {
		t.Fatal("reading must not write the adoption")
	}

	// Writing in that focus saves the adoption, which makes the mark pushable.
	if err := s.SetViewedFiles([]ViewedUpdate{{Path: "new.go", Viewed: true, HeadSHA: "h7"}}); err != nil {
		t.Fatal(err)
	}
	pushable := pushableViewedReview(s.critJSONPath())
	if pushable == nil || !pushable.ChangeViewedFiles["pr:7"]["main.go"].Viewed {
		t.Fatalf("adopted mark not pushable: %+v", pushable)
	}
	if _, kept := pushable.ChangeViewedFiles["pr:7"]["old.go"]; kept {
		t.Fatal("a legacy mark on another revision must not be adopted")
	}

	// PR 8 still finds its own mark later.
	s.Focus = Focus{Kind: FocusRange, ChangeNumber: 8, HeadSHA: "h8"}
	if !mustViewed(t, s)["other.go"].Viewed {
		t.Fatal("PR 8's legacy mark was lost")
	}
}

func mustViewed(t *testing.T, s *Session) map[string]ViewedFile {
	t.Helper()
	got, err := s.ViewedFiles()
	if err != nil {
		t.Fatal(err)
	}
	return got
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
		ViewedFiles: map[string]ViewedFile{"local.go": {Viewed: true}},
		ChangeViewedFiles: map[string]map[string]ViewedFile{
			"pr:7": {
				"pinned.go":   {Viewed: true, HeadSHA: "head", ChangeNumber: 7},
				"untick.go":   {Viewed: false, HeadSHA: "head", ChangeNumber: 7},
				"auto.go":     {Viewed: true, Auto: true, HeadSHA: "head", ChangeNumber: 7},
				"unpinned.go": {Viewed: true},
			},
			"pr:8": {"auto.go": {Viewed: true, Auto: true, HeadSHA: "head", ChangeNumber: 8}},
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
	marks := got.ChangeViewedFiles["pr:7"]
	if len(got.ChangeViewedFiles) != 1 || len(marks) != 2 || !marks["pinned.go"].Viewed || len(got.ViewedFiles) != 0 {
		t.Fatalf("marks: %+v / %+v", got.ChangeViewedFiles, got.ViewedFiles)
	}
	if _, kept := marks["untick.go"]; !kept {
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
	cj := CritJSON{Files: map[string]CritJSONFile{},
		ViewedFiles: map[string]ViewedFile{
			"local.go":  {Viewed: true},
			"legacy.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7},
		},
		ChangeViewedFiles: map[string]map[string]ViewedFile{
			"pr:7": {"auto.go": {Viewed: true, Auto: true, HeadSHA: "head", ChangeNumber: 7}},
		},
	}
	if err := SaveCritJSON(identity, cj); err != nil {
		t.Fatal(err)
	}
	cleanupOnApproval(true, identity, true)
	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Fatalf("review should be gone: %v", err)
	}
}
