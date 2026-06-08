package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewPathsFor(t *testing.T) {
	// Build expected paths via filepath.Join so the test runs on both POSIX
	// (forward slashes) and Windows (backslashes). reviewPathsFor uses
	// filepath.Join internally; the test must mirror that, not hardcode `/`.
	cases := []struct {
		identity string
	}{
		{identity: filepath.Join("/home/u/.crit/reviews", "abc")},
		// In-repo identity is the .crit folder (no .json extension).
		{identity: filepath.Join("/tmp/proj", ".crit")},
	}
	for _, tc := range cases {
		got := reviewPathsFor(tc.identity)
		wantFolder := tc.identity
		wantReview := filepath.Join(tc.identity, "review.json")
		wantSnaps := filepath.Join(tc.identity, "snapshots.json")
		if got.Folder != wantFolder || got.Review != wantReview || got.Snapshots != wantSnaps {
			t.Errorf("reviewPathsFor(%q) = %+v; want folder=%q review=%q snaps=%q",
				tc.identity, got, wantFolder, wantReview, wantSnaps)
		}
	}
}

func TestSnapshotsFile_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	original := SnapshotsFile{
		RoundSnapshots: map[string]map[int]RoundSnapshot{
			"plan.md": {
				1: {Content: "v1", Status: "modified", CapturedAt: ts},
				2: {Content: "v2", Status: "modified", CapturedAt: ts},
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var got SnapshotsFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.RoundSnapshots["plan.md"][1].Content != "v1" {
		t.Fatal("round 1 content lost")
	}
	if !got.RoundSnapshots["plan.md"][2].CapturedAt.Equal(ts) {
		t.Fatal("captured_at lost")
	}
}

func TestSnapshotsFile_AbsentReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "abc")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	sf, err := loadSnapshotsFile(reviewPathsFor(folder).Snapshots)
	if err != nil {
		t.Fatalf("missing sidecar must not error: %v", err)
	}
	if len(sf.RoundSnapshots) != 0 {
		t.Fatalf("want empty, got %+v", sf.RoundSnapshots)
	}
}

func TestSaveAndLoadSnapshotsFile(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "abc")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := reviewPathsFor(folder)
	sf := SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{
		"a.md": {1: {Content: "x", CapturedAt: time.Now().UTC()}},
	}}
	if err := saveSnapshotsFile(paths.Snapshots, sf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Snapshots); err != nil {
		t.Fatalf("not written: %v", err)
	}
	got, err := loadSnapshotsFile(paths.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoundSnapshots["a.md"][1].Content != "x" {
		t.Fatalf("round-trip lost content: %+v", got)
	}
}

// --- Task 2: ensureReviewFolder migration tests ---

func TestMigrate_FlatFileToFolder(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	flatSidecar := identity + ".snapshots.json"
	if err := os.WriteFile(identity, []byte(`{"branch":"main","review_round":1,"files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flatSidecar, []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatalf("identity not a folder after migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(identity, "review.json")); err != nil {
		t.Fatalf("review.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(identity, "snapshots.json")); err != nil {
		t.Fatalf("snapshots.json missing: %v", err)
	}
	if _, err := os.Stat(flatSidecar); !os.IsNotExist(err) {
		t.Fatalf("flat sidecar not removed: err=%v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity, "review.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatal(err)
	}
	if err := ensureReviewFolder(identity); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatal("folder lost")
	}
}

func TestMigrate_NoFlatNoFolder_NoOp(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("ensureReviewFolder on non-existent identity must not error: %v", err)
	}
	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Fatal("must not create the folder if there's nothing to migrate")
	}
}

func TestMigrate_OrphanFlatSidecarOnly(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.WriteFile(identity+".snapshots.json", []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureReviewFolder(identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Fatal("must not create folder when only orphan sidecar exists")
	}
}

func TestMigrate_BothFolderAndOrphanSidecar_FolderWins(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity, "review.json"), []byte(`{"branch":"new"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identity+".snapshots.json", []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(identity, "review.json"))
	if !strings.Contains(string(data), "new") {
		t.Fatal("folder review.json was overwritten")
	}
}

// --- Task 3: loadCritJSON / saveCritJSON / clearCritJSON folder layout ---

func TestSaveAndLoadCritJSON_FolderLayout(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")

	cj := CritJSON{Branch: "main", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(identity, cj); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(identity, "review.json")); err != nil {
		t.Fatalf("review.json not in folder: %v", err)
	}
	got, err := loadCritJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "main" {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

func TestClearReviewFolder_RemovesFolder(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	if err := saveCritJSON(identity, CritJSON{Branch: "main", Files: map[string]CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshotsFile(reviewPathsFor(identity).Snapshots, SnapshotsFile{RoundSnapshots: map[string]map[int]RoundSnapshot{}}); err != nil {
		t.Fatal(err)
	}

	if err := clearReviewFolder(identity); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Fatalf("folder not removed: err=%v", err)
	}
}

// --- Tasks 4-7: in-memory snapshots, capture, R1 baseline, sidecar restore ---

func TestSession_CaptureRoundSnapshot_FilesMode(t *testing.T) {
	s := &Session{
		Mode: "files",
		Files: []*FileEntry{
			{Path: "a.md", Status: "modified", Content: "hello"},
		},
	}
	s.captureRoundSnapshot(1)
	got, ok := s.RoundSnapshots["a.md"][1]
	if !ok {
		t.Fatal("R1 snapshot not captured")
	}
	if got.Content != "hello" {
		t.Errorf("Content = %q", got.Content)
	}
}

func TestSession_CaptureRoundSnapshot_GitModeNoOp(t *testing.T) {
	s := &Session{
		Mode:  "git",
		Files: []*FileEntry{{Path: "a.go", Content: "x"}},
	}
	s.captureRoundSnapshot(1)
	if len(s.RoundSnapshots) != 0 {
		t.Fatalf("git mode must not capture snapshots, got %+v", s.RoundSnapshots)
	}
}

func TestSession_CaptureRoundSnapshot_SkipsLazyAndDeleted(t *testing.T) {
	s := &Session{
		Mode: "files",
		Files: []*FileEntry{
			{Path: "lazy.md", Lazy: true, Content: "x"},
			{Path: "gone.md", Status: "deleted", Content: "x"},
			{Path: "ok.md", Status: "modified", Content: "x"},
		},
	}
	s.captureRoundSnapshot(1)
	if _, ok := s.RoundSnapshots["lazy.md"]; ok {
		t.Error("lazy file should be skipped")
	}
	if _, ok := s.RoundSnapshots["gone.md"]; ok {
		t.Error("deleted file should be skipped")
	}
	if _, ok := s.RoundSnapshots["ok.md"][1]; !ok {
		t.Error("ok.md R1 missing")
	}
}

func TestSession_CaptureRoundSnapshot_Idempotent(t *testing.T) {
	s := &Session{
		Mode: "files",
		Files: []*FileEntry{
			{Path: "a.md", Status: "modified", Content: "v1"},
		},
	}
	s.captureRoundSnapshot(1)
	// Mutate file content; second capture for the same round must NOT overwrite.
	s.Files[0].Content = "v2"
	s.captureRoundSnapshot(1)
	if got := s.RoundSnapshots["a.md"][1].Content; got != "v1" {
		t.Errorf("idempotency violated: round 1 content = %q", got)
	}
}

func TestCloneRoundSnapshots_DeepCopy(t *testing.T) {
	src := map[string]map[int]RoundSnapshot{
		"a.md": {1: {Content: "x"}},
	}
	dst := cloneRoundSnapshots(src)
	dst["a.md"][1] = RoundSnapshot{Content: "MUT"}
	if src["a.md"][1].Content != "x" {
		t.Fatal("clone is shallow")
	}
}

func TestLoadCritJSON_TriggersMigration(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.WriteFile(identity, []byte(`{"branch":"main","review_round":1,"files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadCritJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "main" {
		t.Fatalf("round-trip lost: %+v", got)
	}
	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatal("loadCritJSON should have triggered migration")
	}
}
