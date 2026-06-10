package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestExportedFindReviewFileByBranch(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	want := writeReviewFixture(t, "k1", "feature-x")

	got, err := FindReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestExportedFindReviewFileByCommentID(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{{ID: "cmt-1"}}},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "k1.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindReviewFileByCommentID("cmt-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("got %q want %q", got, path)
	}
}

func TestExportedCJContainsCommentID(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{
		"f": {Comments: []Comment{{ID: "x"}}},
	}}
	if !CJContainsCommentID(cj, "x") {
		t.Error("expected true")
	}
	if CJContainsCommentID(cj, "missing") {
		t.Error("expected false")
	}
}

func TestExportedSnapshotsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "snapshots.json")
	sf := SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{
		"a.go": {1: {Content: "hello"}},
	}}
	if err := SaveSnapshotsFile(snapPath, sf); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshotsFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoundSnapshots["a.go"][1].Content != "hello" {
		t.Errorf("got %+v", got)
	}
}

func TestStaleReviewMetaLabel(t *testing.T) {
	sr := staleReview{reviewType: "live", origin: "http://x"}
	if sr.metaLabel() != "live: http://x, " {
		t.Errorf("got %q", sr.metaLabel())
	}
	sr2 := staleReview{branch: "main"}
	if sr2.metaLabel() != "main, " {
		t.Errorf("got %q", sr2.metaLabel())
	}
}
