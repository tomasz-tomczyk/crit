package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReviewFile_DoesNotContainRoundSnapshots is a regression guard against
// round snapshots accidentally being persisted into review.json. Snapshots
// must live exclusively in <folder>/snapshots.json so that AI agents reading
// review.json never load megabytes of historical file content.
func TestReviewFile_DoesNotContainRoundSnapshots(t *testing.T) {
	s := newTestSession(t)
	s.RoundSnapshots = map[string]map[int]RoundSnapshot{
		"plan.md": {
			1: {Content: "v1", Status: "modified", CapturedAt: time.Now()},
			2: {Content: "v2", Status: "modified", CapturedAt: time.Now()},
		},
	}
	s.AddComment("plan.md", 1, 1, "", "fix", "", "", "")

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(reviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatalf("review.json not written: %v", err)
	}
	if strings.Contains(string(data), "round_snapshots") {
		t.Fatalf("review.json must not contain round_snapshots key:\n%s", data)
	}
	// Decoding into a generic map verifies no unexpected top-level keys.
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["round_snapshots"]; ok {
		t.Fatal("round_snapshots leaked into review.json")
	}
}

// TestSharePayload_DoesNotIncludeRoundSnapshots is a regression guard that
// inspects the share payload directly. The share API must never carry per-
// round content bodies — they would balloon payload size and reveal
// agent-only review history to the share recipient.
func TestSharePayload_DoesNotIncludeRoundSnapshots(t *testing.T) {
	files := []shareFile{{Path: "plan.md", Content: "current", Status: "modified"}}
	comments := []shareComment{{File: "plan.md", Body: "x"}}

	payload := buildSharePayload(files, comments, 2, []string{"plan.md"}, "", "", "")

	if _, ok := payload["round_snapshots"]; ok {
		t.Fatal("share payload contains round_snapshots key")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "round_snapshots") {
		t.Fatalf("share payload JSON contains round_snapshots:\n%s", data)
	}
}

// TestWriteFiles_EmptyDoesNotDeleteSidecar is the v3-review-flagged B1
// regression: when WriteFiles finds the CritJSON empty and removes
// review.json, the snapshots.json sidecar must remain in place. Otherwise
// a user clearing all comments mid-session would silently lose every
// captured round.
func TestWriteFiles_EmptyDoesNotDeleteSidecar(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSessionFromFiles([]string{planPath}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}
	// Pin the review identity to the test tempdir so this test does not
	// touch the dev-machine's repo root via the critJSONPath fallback.
	identity := filepath.Join(dir, ".crit")
	s.ReviewFilePath = identity
	paths := reviewPathsFor(identity)

	// Seed the sidecar with two rounds of snapshots in memory and on disk.
	s.RoundSnapshots = map[string]map[int]RoundSnapshot{
		"plan.md": {
			1: {Content: "v1", CapturedAt: time.Now()},
			2: {Content: "v2", CapturedAt: time.Now()},
		},
	}
	if err := saveSnapshotsFile(paths.Snapshots, SnapshotsFile{
		RoundSnapshots: cloneRoundSnapshots(s.RoundSnapshots),
	}); err != nil {
		t.Fatalf("saveSnapshotsFile: %v", err)
	}

	// Pre-create review.json so WriteFiles' empty-removal branch has
	// something to delete.
	if err := saveCritJSON(identity, CritJSON{
		Branch: s.Branch,
		Files:  map[string]CritJSONFile{},
	}); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	// Sanity: both files exist before the call.
	if _, err := os.Stat(paths.Review); err != nil {
		t.Fatalf("review.json missing pre-call: %v", err)
	}
	if _, err := os.Stat(paths.Snapshots); err != nil {
		t.Fatalf("snapshots.json missing pre-call: %v", err)
	}

	// WriteFiles with no comments and no share state -> empty CritJSON ->
	// removes review.json.
	flushWrites(s)
	s.WriteFiles()

	if _, err := os.Stat(paths.Review); !os.IsNotExist(err) {
		t.Fatalf("review.json should be removed by empty-write branch, err=%v", err)
	}
	if _, err := os.Stat(paths.Snapshots); err != nil {
		t.Fatalf("snapshots.json must be preserved across empty WriteFiles, err=%v", err)
	}
	if _, err := os.Stat(paths.Folder); err != nil {
		t.Fatalf("review folder must remain, err=%v", err)
	}
}
