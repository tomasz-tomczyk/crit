package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestFindStaleReviews_FlatFile(t *testing.T) {
	dir := t.TempDir()
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	cj := session.CritJSON{
		Branch:      "old-branch",
		UpdatedAt:   oldTime,
		ReviewRound: 1,
		Files: map[string]session.CritJSONFile{
			"main.go": {Comments: []session.Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "stale123.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	recentCJ := session.CritJSON{
		Branch:      "recent-branch",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReviewRound: 1,
		Files:       map[string]session.CritJSONFile{},
	}
	recentData, _ := json.MarshalIndent(recentCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "recent456.json"), recentData, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviewsForTest(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale review, got %d", len(stale))
	}
	if stale[0].branch != "old-branch" {
		t.Errorf("branch = %q, want %q", stale[0].branch, "old-branch")
	}
}

func TestFindStaleReviews_FolderForm(t *testing.T) {
	dir := t.TempDir()
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	staleFolder := filepath.Join(dir, "stalekey1")
	if err := os.MkdirAll(staleFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	cj := session.CritJSON{
		Branch:      "old-branch",
		UpdatedAt:   oldTime,
		ReviewRound: 1,
		Files: map[string]session.CritJSONFile{
			"main.go": {Comments: []session.Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(staleFolder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviewsForTest(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale review, got %d", len(stale))
	}
	if stale[0].path != staleFolder {
		t.Errorf("path = %q, want %q", stale[0].path, staleFolder)
	}
}

func TestDeleteStaleReviews_FolderForm(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "key1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "review.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted := deleteStaleReviewsForTest([]staleReview{{key: "key1", path: folder}})
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("folder not removed: err=%v", err)
	}
}

func TestRunCleanup_NoStale(t *testing.T) {
	dir := t.TempDir()
	testutil.SetHome(t, dir)
	revDir := filepath.Join(dir, ".crit", "reviews")
	if err := os.MkdirAll(revDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RunCleanup([]string{"--days", "7", "--force"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveStaleReviewPath_FlatFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !removeStaleReviewPathForTest(path) {
		t.Error("expected true")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}
}

func TestExportedReviewAPIs(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	cj := CritJSON{Branch: "main", Files: map[string]CritJSONFile{}}
	if err := EnsureReviewFolder(critPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCritJSON(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q", got.Branch)
	}
	if err := ClearCritJSON(dir); err != nil {
		t.Fatal(err)
	}
}
