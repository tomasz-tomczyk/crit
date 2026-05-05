package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestRoundSnapshotPosition asserts that captureRoundSnapshot records each
// file's index in s.Files as Position.
func TestRoundSnapshotPosition(t *testing.T) {
	s := &Session{
		Mode:        "files",
		ReviewRound: 1,
		Files: []*FileEntry{
			{Path: "a.md", Status: "modified", Content: "a"},
			{Path: "b.md", Status: "modified", Content: "b"},
			{Path: "c.md", Status: "modified", Content: "c"},
		},
	}
	s.captureRoundSnapshot(1)

	cases := []struct {
		path string
		want int
	}{
		{"a.md", 0},
		{"b.md", 1},
		{"c.md", 2},
	}
	for _, tc := range cases {
		got, ok := s.roundSnapshotForFile(tc.path, 1)
		if !ok {
			t.Fatalf("snapshot missing for %s", tc.path)
		}
		if got.Position != tc.want {
			t.Errorf("%s: Position = %d, want %d", tc.path, got.Position, tc.want)
		}
	}
}

// TestServeFileAtRound_IncludesPosition confirms the per-round file API
// surfaces Position in its JSON response.
func TestServeFileAtRound_IncludesPosition(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	// Inject Position into the existing snapshot fixture (the test helper
	// builds snapshots inline rather than going through captureRoundSnapshot).
	sess.mu.Lock()
	rs := sess.RoundSnapshots["test.md"][2]
	rs.Position = 7
	sess.RoundSnapshots["test.md"][2] = rs
	sess.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	pos, ok := resp["position"]
	if !ok {
		t.Fatalf("position missing from response: %v", resp)
	}
	if got, _ := pos.(float64); int(got) != 7 {
		t.Errorf("position = %v, want 7", pos)
	}
}
