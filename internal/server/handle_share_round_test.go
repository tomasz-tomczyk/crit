package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func newShareTestServer(t *testing.T, critWebURL string, consent bool) (*Server, *Session) {
	t.Helper()
	testutil.SetHome(t, t.TempDir())
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	os.WriteFile(planPath, []byte("# Plan"), 0o644)

	cj := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"},
				{ID: "c2", StartLine: 2, EndLine: 2, Body: "Done", Resolved: true},
			}},
		},
	}
	data, _ := json.Marshal(cj)
	os.MkdirAll(filepath.Join(dir, ".crit"), 0o755)
	os.WriteFile(filepath.Join(dir, ".crit", "review.json"), data, 0o644)

	sess := &Session{
		Mode:        "files",
		OutputDir:   dir,
		RepoRoot:    dir,
		ReviewRound: 1,
		Files: []*FileEntry{{
			Path:    "plan.md",
			AbsPath: planPath,
			Status:  "added",
			Content: "# Plan",
		}},
	}
	sess.InitTestChannels()

	s, err := NewServer(sess, frontendFS, critWebURL, false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if consent {
		s.authMu.Lock()
		s.cfg.ShareConsented = true
		s.authMu.Unlock()
	}
	return s, sess
}

func TestHandleShare_Success(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		comments := payload["comments"].([]any)
		if len(comments) != 1 {
			t.Errorf("expected 1 unresolved comment, got %d", len(comments))
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/test123",
			"delete_token": "tok_test",
		})
	}))
	defer critWeb.Close()

	s, _ := newShareTestServer(t, critWeb.URL, true)
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["url"] != "https://crit.md/r/test123" {
		t.Errorf("url = %v", result["url"])
	}
}

func TestHandleShare_WrongMethod(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	req := httptest.NewRequest(http.MethodGet, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleShare_NoShareURL(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleShare_AlreadyShared(t *testing.T) {
	s, sess := newShareTestServer(t, "https://example.com", true)
	sess.SetSharedURLAndToken("https://crit.md/r/existing", "tok_existing")
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["url"] != "https://crit.md/r/existing" {
		t.Errorf("url = %v", result["url"])
	}
}

func TestHandleShare_ShareServiceError(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer critWeb.Close()

	s, _ := newShareTestServer(t, critWeb.URL, true)
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
}

func TestHandleFile_RoundZero(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=0", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("?round=0 should be 400, got %d", w.Code)
	}
}

func TestHandleFile_RoundCurrentMatchesSnapshot(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	wantContent := sess.RoundSnapshots["test.md"][2].Content
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["content"].(string); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
}

func TestHandleFile_EmptyRoundParamFallsThrough(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("empty round should fall through, got %d", w.Code)
	}
}

func TestHandleFile_DuplicateRoundParam_FirstWins(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=1&round=99", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("first round=1 should win, got %d", w.Code)
	}
}

func TestHandleRounds_MethodNotAllowed(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		req := httptest.NewRequest(m, "/api/rounds", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 405 {
			t.Errorf("%s /api/rounds: status=%d, want 405", m, w.Code)
		}
	}
}

func TestHandleFileComments_RoundFiltersByReviewRound(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	sess.Files[0].Comments = []Comment{
		{ID: "c1", ReviewRound: 1, Body: "first round"},
		{ID: "c2", ReviewRound: 2, Body: "second round"},
		{ID: "c3", ReviewRound: 3, Body: "future"},
	}
	req := httptest.NewRequest("GET", "/api/file/comments?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []Comment
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(got))
	}
}
