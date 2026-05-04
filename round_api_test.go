package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// newRoundsTestServer returns a server with a files-mode session preloaded
// with R1 and R2 snapshots for "test.md" so handlers can be exercised with
// ?round=N. The current FileEntry.Content matches R2.
func newRoundsTestServer(t *testing.T) (*Server, *Session) {
	t.Helper()
	s, sess := newTestServer(t)
	// Set R1 (initial content) and R2 (after one edit) snapshots.
	// File current Content from newTestServer is "line1\nline2\nline3\n".
	r1 := "line1\nline2\nline3\n"
	r2 := "line1\nlineTWO\nline3\nline4\n"
	sess.Files[0].Content = r2
	sess.RoundSnapshots = map[string]map[int]RoundSnapshot{
		"test.md": {
			1: {Content: r1, Status: "modified"},
			2: {Content: r2, Status: "modified"},
		},
	}
	sess.ReviewRound = 2
	return s, sess
}

func TestHandleFileDiff_Round_FilesMode(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["hunks"]; !ok {
		t.Fatalf("hunks missing: %v", resp)
	}
	if got, _ := resp["previous_content"].(string); got == "" {
		t.Fatalf("previous_content empty for R2: %v", resp)
	}
	hunks, _ := resp["hunks"].([]any)
	if len(hunks) == 0 {
		t.Fatalf("expected diff hunks for R1->R2, got none")
	}
}

func TestHandleFileDiff_Round_InvalidParam(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=abc", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("invalid round: status=%d", w.Code)
	}
}

func TestHandleFileDiff_Round_OutOfRange(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=99", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("out-of-range round: status=%d", w.Code)
	}
}

func TestHandleFileDiff_Round_R1NoPrevious(t *testing.T) {
	// R1 is the baseline — no previous content; diff should be empty hunks.
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	hunks, _ := resp["hunks"].([]any)
	if len(hunks) != 0 {
		t.Fatalf("R1 baseline must have no diff hunks, got %v", hunks)
	}
}

func TestHandleSession_Round_FilesMode(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	// Add an extra file that exists only in R2 (not in R1).
	sess.Files = append(sess.Files, &FileEntry{
		Path:     "new.md",
		Status:   "added",
		FileType: "markdown",
		Content:  "n",
		Comments: []Comment{},
	})
	sess.RoundSnapshots["new.md"] = map[int]RoundSnapshot{
		2: {Content: "n", Status: "added"},
	}

	req := httptest.NewRequest("GET", "/api/session?round=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var info SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	for _, f := range info.Files {
		if f.Path == "new.md" {
			t.Fatalf("new.md should be hidden at R1, files=%v", info.Files)
		}
	}
}

func TestHandleRounds_LineStats(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/rounds", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var resp struct {
		CurrentRound int `json:"current_round"`
		Rounds       []struct {
			N         int `json:"n"`
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(resp.Rounds))
	}
	r1 := resp.Rounds[0]
	r2 := resp.Rounds[1]
	if r1.N != 1 || r1.Additions != 0 || r1.Deletions != 0 {
		t.Errorf("R1 must be baseline (zero stats): %+v", r1)
	}
	if r2.N != 2 {
		t.Errorf("expected R2 second, got %+v", r2)
	}
	if r2.Additions == 0 && r2.Deletions == 0 {
		t.Errorf("R2 must have non-zero line stats: %+v", r2)
	}
}
