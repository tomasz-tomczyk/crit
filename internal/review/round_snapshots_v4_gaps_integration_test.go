//go:build integration

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// TestFindReviewFileByCommentID_AmbiguousAcrossFolderAndFlat: if the same
// comment ID lives in both a folder and a leftover flat file (test-pollution
// scenario or an aborted migration in production), the function must return
// an error rather than silently picking one.
func TestFindReviewFileByCommentID_AmbiguousAcrossFolderAndFlat(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	commentID := "c_dup_xyz"
	cj := CritJSON{
		Branch: "main",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{{ID: commentID, Body: "x"}}},
		},
	}
	data, _ := json.Marshal(cj)

	folder := filepath.Join(dir, "k1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	flat := filepath.Join(dir, "k2.json")
	if err := os.WriteFile(flat, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = FindReviewFileByCommentID(commentID, "")
	if err == nil {
		t.Fatal("expected ambiguity error when comment exists in both folder and flat review")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error should mention multiple matches, got: %v", err)
	}
}

// TestWalkReviewIdentities_SkipsOrphanFolderSilently: a folder with only
// snapshots.json (no review.json) is benignly skipped.
func TestWalkReviewIdentities_SkipsOrphanFolderSilently(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir, _ := daemon.ReviewsDir()
	if err := os.MkdirAll(filepath.Join(dir, "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orphan", "snapshots.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "review.json"), []byte(`{"branch":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var visited []string
	err := WalkReviewIdentities(func(identity string, _ []byte) error {
		visited = append(visited, filepath.Base(identity))
		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	for _, v := range visited {
		if v == "orphan" {
			t.Errorf("orphan folder should not be visited: %v", visited)
		}
	}
	found := false
	for _, v := range visited {
		if v == "good" {
			found = true
		}
	}
	if !found {
		t.Errorf("good folder should be visited, got: %v", visited)
	}
}

// TestFindReviewFileByBranch_MalformedJSONSkipped: corrupt review.json must
// not abort the scan.
func TestFindReviewFileByBranch_MalformedJSONSkipped(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir, _ := daemon.ReviewsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "review.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := WriteFolderReviewFixture(t, "good", "feature-x")

	got, err := FindReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatalf("scan should succeed despite corrupt sibling: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
