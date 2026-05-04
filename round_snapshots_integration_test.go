package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundSnapshots_Integration exercises the full files-mode round
// timeline: R1 baseline at session construction, an agent edit, R2 capture
// via handleRoundCompleteFiles, and HTTP queries that surface both rounds.
//
// This is the only integration-style test for round snapshots. It deliberately
// avoids the //go:build integration tag so it runs in the default test suite
// and catches regressions in CI without an opt-in flag.
func TestRoundSnapshots_Integration(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	r1Body := "# Plan\n\nstep one\n"
	if err := os.WriteFile(planPath, []byte(r1Body), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSessionFromFiles([]string{planPath}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}
	identity := filepath.Join(dir, ".crit.json")
	s.ReviewFilePath = identity

	// captureBaselineAndPersist already ran in NewSessionFromFiles, but with
	// an empty ReviewFilePath. Re-run now that the identity is pinned so the
	// sidecar lands in the test tempdir instead of the dev repo root.
	s.captureBaselineAndPersist()

	// File path is whatever NewSessionFromFiles assigned (relative to the
	// repo root if a VCS was detected, absolute otherwise on a bare tempdir).
	relPath := s.Files[0].Path
	if _, ok := s.RoundSnapshots[relPath][1]; !ok {
		t.Fatalf("R1 baseline missing after constructor: %+v", s.RoundSnapshots)
	}

	// Simulate an agent edit: bump the in-memory file content.
	r2Body := "# Plan\n\nstep one\nstep two\nstep three\n"
	s.mu.Lock()
	s.Files[0].Content = r2Body
	// status flag is what captureRoundSnapshot looks at to skip "deleted".
	s.Files[0].Status = "modified"
	s.mu.Unlock()

	// Round-complete capture: must take R2 BEFORE rereadFileContents.
	s.handleRoundCompleteFiles()

	s.mu.RLock()
	r1, hasR1 := s.RoundSnapshots[relPath][1]
	r2, hasR2 := s.RoundSnapshots[relPath][2]
	currentRound := s.ReviewRound
	s.mu.RUnlock()
	if !hasR1 || !hasR2 {
		t.Fatalf("expected R1 and R2 snapshots, got %+v", s.RoundSnapshots)
	}
	if r1.Content != r1Body {
		t.Errorf("R1 content lost: %q", r1.Content)
	}
	if r2.Content != r2Body {
		t.Errorf("R2 content not captured: %q", r2.Content)
	}
	if currentRound != 2 {
		t.Errorf("ReviewRound = %d, want 2", currentRound)
	}

	// Sidecar should be on disk in the v4 folder layout.
	paths := reviewPathsFor(identity)
	sidecar, err := loadSnapshotsFile(paths.Snapshots)
	if err != nil {
		t.Fatalf("loadSnapshotsFile: %v", err)
	}
	if _, ok := sidecar.RoundSnapshots[relPath][2]; !ok {
		t.Fatalf("sidecar missing R2 snapshot: %+v", sidecar.RoundSnapshots)
	}

	// HTTP wiring.
	srv, err := NewServer(s, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSession(s)
	srv.SetShutdownCtx(context.Background())

	// /api/rounds — both rounds present, R2 has non-zero stats.
	req := httptest.NewRequest("GET", "/api/rounds", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("/api/rounds status=%d body=%s", w.Code, w.Body.String())
	}
	var roundsResp struct {
		CurrentRound int `json:"current_round"`
		Rounds       []struct {
			N         int `json:"n"`
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &roundsResp); err != nil {
		t.Fatal(err)
	}
	if roundsResp.CurrentRound != 2 {
		t.Errorf("current_round = %d, want 2", roundsResp.CurrentRound)
	}
	if len(roundsResp.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d (%+v)", len(roundsResp.Rounds), roundsResp.Rounds)
	}
	if roundsResp.Rounds[1].Additions == 0 {
		t.Errorf("R2 additions should be non-zero: %+v", roundsResp.Rounds[1])
	}

	// /api/file?round=1 returns R1 content.
	req = httptest.NewRequest("GET", "/api/file?path="+relPath+"&round=1", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("/api/file?round=1 status=%d body=%s", w.Code, w.Body.String())
	}
	var fileResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fileResp); err != nil {
		t.Fatal(err)
	}
	if got, _ := fileResp["content"].(string); got != r1Body {
		t.Errorf("R1 content via API = %q, want %q", got, r1Body)
	}

	// /api/file/diff?round=2 reflects R1 -> R2 changes.
	req = httptest.NewRequest("GET", "/api/file/diff?path="+relPath+"&round=2", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("/api/file/diff?round=2 status=%d body=%s", w.Code, w.Body.String())
	}
	var diffResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &diffResp); err != nil {
		t.Fatal(err)
	}
	prev, _ := diffResp["previous_content"].(string)
	if prev != r1Body {
		t.Errorf("diff previous_content = %q, want R1 body", prev)
	}
	hunks, _ := diffResp["hunks"].([]any)
	if len(hunks) == 0 {
		t.Errorf("expected non-empty hunks for R1 -> R2 diff: %v", diffResp)
	}

	// Folder-layout sanity: review.json + snapshots.json inside the folder,
	// no flat .json file outside it.
	if _, err := os.Stat(paths.Folder); err != nil {
		t.Fatalf("review folder missing: %v", err)
	}
	if _, err := os.Stat(paths.Snapshots); err != nil {
		t.Fatalf("snapshots.json missing inside folder: %v", err)
	}
	flat := strings.TrimSuffix(identity, "/") + ".json"
	if _, err := os.Stat(flat); err == nil {
		t.Errorf("unexpected legacy flat file at %s", flat)
	}
}
