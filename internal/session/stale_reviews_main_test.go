package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCountComments(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Resolved: false},
				{ID: "c2", Resolved: true},
			}},
			"b.go": {Comments: []Comment{
				{ID: "c3", Resolved: false},
			}},
		},
		ReviewComments: []Comment{
			{ID: "r1", Resolved: true},
		},
	}
	unresolved, resolved := countComments(cj)
	if unresolved != 2 {
		t.Errorf("unresolved = %d, want 2", unresolved)
	}
	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}
}

func TestFindStaleReviews(t *testing.T) {
	dir := t.TempDir()

	// Create a review file with an old updated_at.
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	cj := CritJSON{
		Branch:      "old-branch",
		UpdatedAt:   oldTime,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {Comments: []Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "stale123.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Create a recent review file.
	recentCJ := CritJSON{
		Branch:      "recent-branch",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	}
	recentData, _ := json.MarshalIndent(recentCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "recent456.json"), recentData, 0644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale review, got %d", len(stale))
	}
	if stale[0].branch != "old-branch" {
		t.Errorf("branch = %q, want %q", stale[0].branch, "old-branch")
	}
	if stale[0].comments != 1 {
		t.Errorf("comments = %d, want 1", stale[0].comments)
	}
}

func TestFindStaleReviews_FolderForm(t *testing.T) {
	dir := t.TempDir()

	// Folder-form review with stale updated_at.
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	staleFolder := filepath.Join(dir, "stalekey1")
	if err := os.MkdirAll(staleFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		Branch:      "old-branch",
		UpdatedAt:   oldTime,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {Comments: []Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(staleFolder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Sidecar present too.
	if err := os.WriteFile(filepath.Join(staleFolder, "snapshots.json"), []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Recent folder-form review.
	recentFolder := filepath.Join(dir, "recentkey")
	if err := os.MkdirAll(recentFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	recentCJ := CritJSON{
		Branch:      "recent-branch",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	}
	recentData, _ := json.MarshalIndent(recentCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(recentFolder, "review.json"), recentData, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale review, got %d (%+v)", len(stale), stale)
	}
	if stale[0].branch != "old-branch" {
		t.Errorf("branch = %q", stale[0].branch)
	}
	if stale[0].comments != 1 {
		t.Errorf("comments = %d", stale[0].comments)
	}
	if stale[0].path != staleFolder {
		t.Errorf("path = %q, want folder identity %q", stale[0].path, staleFolder)
	}
}

func TestFindStaleReviews_OrphanSnapshotsFolder(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, "orphan1")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	// snapshots.json without a sibling review.json.
	if err := os.WriteFile(filepath.Join(orphan, "snapshots.json"), []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Age the folder mtime past the cutoff.
	old := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected orphan folder collected, got %d entries: %+v", len(stale), stale)
	}
	if stale[0].path != orphan {
		t.Errorf("path = %q, want orphan folder %q", stale[0].path, orphan)
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
	if err := os.WriteFile(filepath.Join(folder, "snapshots.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted := deleteStaleReviews([]staleReview{{key: "key1", path: folder}})
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("folder not removed: err=%v", err)
	}
}

func TestCleanupOnApproval_RemovesFolderForm(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	if err := saveCritJSON(identity, CritJSON{Branch: "main", Files: map[string]CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshotsFile(ReviewPathsFor(identity).Snapshots, SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{}}); err != nil {
		t.Fatal(err)
	}

	cleanupOnApproval(true, identity, true)

	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Errorf("folder not removed: err=%v", err)
	}
}

func TestDeleteStaleReviews(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	stale := []staleReview{{key: "test", path: path}}
	deleted := deleteStaleReviews(stale)
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale review file should be deleted")
	}
}

func TestCleanupOnApproval_DeletesReviewFile(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	// approved=true with cleanup enabled should delete the file.
	cleanupOnApproval(true, reviewPath, true)

	if _, err := os.Stat(reviewPath); !os.IsNotExist(err) {
		t.Error("expected review file to be deleted after approval")
	}
}

func TestCleanupOnApproval_KeepsFileWhenNotApproved(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	cleanupOnApproval(false, reviewPath, true)

	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		t.Error("expected review file to still exist when not approved")
	}
}

func TestCleanupOnApproval_KeepsFileWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	// approved=true but cleanup disabled — file should stay.
	cleanupOnApproval(true, reviewPath, false)

	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		t.Error("expected review file to still exist when cleanup is disabled")
	}
}

func TestCleanupOnApproval_EmptyPath(t *testing.T) {
	// Should be a no-op when reviewPath is empty
	cleanupOnApproval(true, "", true)
}
