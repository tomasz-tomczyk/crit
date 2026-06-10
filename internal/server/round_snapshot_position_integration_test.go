//go:build integration

package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestServeFileAtRound_IncludesPosition confirms the per-round file API
// surfaces Position in its JSON response.
func TestServeFileAtRound_IncludesPosition(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	rs := sess.RoundSnapshots["test.md"][2]
	rs.Position = 7
	sess.RoundSnapshots["test.md"][2] = rs

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
