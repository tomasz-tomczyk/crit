package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
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

func TestHandleShare_FreshPreviewPreservesMetadataWithoutReviewFile(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir := t.TempDir()
	originalPath := filepath.Join("artifacts", "reports", "checkout.html")
	origin := filepath.Join(dir, originalPath)
	if err := os.MkdirAll(filepath.Dir(origin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(origin, []byte("<!doctype html><h1>Checkout</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var captured map[string]any
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/preview123",
			"delete_token": "tok_preview",
		})
	}))
	defer critWeb.Close()

	sess := &Session{
		Mode:        "files",
		ReviewType:  "preview",
		RepoRoot:    dir,
		OutputDir:   dir,
		Origin:      origin,
		ReviewRound: 1,
		Files: []*FileEntry{{
			Path: originalPath, AbsPath: origin, FileType: "code", Content: "<!doctype html><h1>Checkout</h1>",
		}},
	}
	sess.InitTestChannels()
	s, err := NewServer(sess, frontendFS, critWeb.URL, false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	s.authMu.Lock()
	s.cfg.ShareConsented = true
	s.authMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cliArgs, _ := captured["cli_args"].([]any)
	if len(cliArgs) != 2 || cliArgs[0] != "preview" || cliArgs[1] != origin {
		t.Errorf("cli_args = %v, want [preview %s]", cliArgs, origin)
	}
	files, _ := captured["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["path"] != session.PreviewMainHTMLKey {
		t.Errorf("files = %v, want entry HTML at %q", files, session.PreviewMainHTMLKey)
	}

	// A stale review file must not override the live preview's canonical path.
	stale, err := json.Marshal(CritJSON{CliArgs: []string{"files", "stale.html"}, Files: map[string]CritJSONFile{}})
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := session.ReviewPathsFor(sess.CritJSONPath()).Review
	if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.shareCLIArgsForSession(sess); len(got) != 2 || got[0] != "preview" || got[1] != origin {
		t.Errorf("cli args with stale persisted metadata = %v, want [preview %s]", got, origin)
	}
}

func TestShareCLIArgsForSession_PreviewFallbacks(t *testing.T) {
	s := &Server{}

	if got := s.shareCLIArgsForSession(nil); got != nil {
		t.Fatalf("nil session cli args = %v, want nil", got)
	}

	live := &Session{ReviewType: "preview", CLIArgs: []string{"preview", "live.html", "ignored"}}
	if got := s.shareCLIArgsForSession(live); len(got) != 2 || got[0] != "preview" || got[1] != "live.html" {
		t.Fatalf("live session cli args = %v, want [preview live.html]", got)
	}

	empty := &Session{ReviewType: "preview"}
	if got := s.shareCLIArgsForSession(empty); got != nil {
		t.Fatalf("empty preview cli args = %v, want nil", got)
	}
}

func TestShareCLIArgsForSession_NormalizesPersistedPreviewArgs(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ReviewType: "preview", OutputDir: dir, RepoRoot: dir}
	persisted, err := json.Marshal(CritJSON{
		CliArgs: []string{"preview", "persisted.html", "ignored"},
		Files:   map[string]CritJSONFile{},
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := session.ReviewPathsFor(sess.CritJSONPath()).Review
	if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, persisted, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := (&Server{}).shareCLIArgsForSession(sess); len(got) != 2 || got[0] != "preview" || got[1] != "persisted.html" {
		t.Fatalf("persisted cli args = %v, want [preview persisted.html]", got)
	}
}

func newPreviewReshareTestServer(t *testing.T) (*Server, *Session, string) {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "original-preview.html")
	if err := os.WriteFile(origin, []byte("<!doctype html><h1>Preview</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		Mode:        "files",
		ReviewType:  "preview",
		RepoRoot:    dir,
		OutputDir:   dir,
		Origin:      origin,
		CLIArgs:     []string{"preview", origin},
		ReviewRound: 1,
		Files: []*FileEntry{{
			Path: "original-preview.html", AbsPath: origin, Content: "<!doctype html><h1>Preview</h1>",
		}},
	}
	sess.InitTestChannels()
	stale, err := json.Marshal(CritJSON{
		CliArgs: []string{"files", "stale.md"},
		Files:   map[string]CritJSONFile{},
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := session.ReviewPathsFor(sess.CritJSONPath()).Review
	if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(sess, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, sess, origin
}

func TestHandleUpsertPayload_PreviewPreservesOriginalCLIPath(t *testing.T) {
	s, _, origin := newPreviewReshareTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/share/upsert-payload", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var payload struct {
		CLIArgs []string `json:"cli_args"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload.CLIArgs, []string{"preview", origin}) {
		t.Fatalf("cli_args = %v, want [preview %s]", payload.CLIArgs, origin)
	}
}

func TestReshareUpsertInputs_PreviewPreservesOriginalCLIPath(t *testing.T) {
	s, sess, origin := newPreviewReshareTestServer(t)
	_, _, cfg, err := s.reshareUpsertInputs(sess, "https://crit.md/r/preview", "delete-token")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.CliArgs, []string{"preview", origin}) {
		t.Fatalf("cli args = %v, want [preview %s]", cfg.CliArgs, origin)
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
	var getCount int
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/reviews/existing/comments" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		getCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/existing", "tok_existing")
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["url"] != critWeb.URL+"/r/existing" {
		t.Errorf("url = %v", result["url"])
	}
	if getCount != 1 {
		t.Errorf("GET comments count = %d, want 1", getCount)
	}
}

func TestWriteExistingShareIfPresent_NoExistingShare(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	w := httptest.NewRecorder()

	handled, err := s.writeExistingShareIfPresent(w)
	if err != nil {
		t.Fatalf("writeExistingShareIfPresent: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

func TestHandleShare_ExistingShareServiceError(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/reviews/existing/comments" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/existing", "tok_existing")

	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["error"] == "" {
		t.Fatal("expected error in response body")
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

func TestHandleSharePull_MergesRemoteComments(t *testing.T) {
	var gotAuth string
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/comments") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "web-1", "body": "from web", "file_path": "plan.md",
				"start_line": 1, "end_line": 1, "external_id": "web-ext-1",
			},
		})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	s.authMu.Lock()
	s.authToken = "tok_test_bearer"
	s.authMu.Unlock()
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer tok_test_bearer" {
		t.Errorf("Authorization = %q, want Bearer tok_test_bearer", gotAuth)
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["merged"].(float64) != 1 {
		t.Errorf("merged = %v, want 1", result["merged"])
	}
}

func TestHandleSharePull_NoSharedReview(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleSharePull_MethodNotAllowed(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	req := httptest.NewRequest(http.MethodGet, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleShareReshare_PullsAndUpserts(t *testing.T) {
	var putAuth string
	var putHits int
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments"):
			json.NewEncoder(w).Encode([]any{})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/reviews/"):
			putHits++
			putAuth = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(map[string]any{
				"url":          "https://crit.md/r/abc123",
				"review_round": 2,
				"changed":      true,
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	s.authMu.Lock()
	s.authToken = "tok_reshare"
	s.authMu.Unlock()
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if putHits != 1 {
		t.Errorf("PUT hits = %d, want 1", putHits)
	}
	if putAuth != "Bearer tok_reshare" {
		t.Errorf("Authorization = %q, want Bearer tok_reshare", putAuth)
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["changed"] != true {
		t.Errorf("changed = %v, want true", result["changed"])
	}
}

func TestHandleShareURL_LocalOnlySkipsRemoteDelete(t *testing.T) {
	remoteHits := 0
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteHits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc", "delete-tok")

	req := httptest.NewRequest(http.MethodDelete, "/api/share-url?local_only=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if remoteHits != 0 {
		t.Errorf("remote hits = %d, want 0 (local_only must skip UnpublishFromWeb)", remoteHits)
	}
	if sess.GetSharedURL() != "" {
		t.Errorf("hosted URL should be cleared")
	}
}

func TestHandleSharePull_Unauthorized(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	s.authMu.Lock()
	s.authToken = "bad"
	s.authMu.Unlock()
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleSharePull_NotFound(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/gone", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleSharePull_NoNewComments(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["merged"].(float64) != 0 {
		t.Errorf("merged = %v, want 0", result["merged"])
	}
}

func TestHandleShareReshare_SoftFailsPullThenUpserts(t *testing.T) {
	putHits := 0
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments"):
			// Non-auth transient failure — reshare should soft-fail pull and still PUT.
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "upstream down"})
		case r.Method == http.MethodPut:
			putHits++
			json.NewEncoder(w).Encode(map[string]any{
				"url": "https://crit.md/r/abc123", "review_round": 2, "changed": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if putHits != 1 {
		t.Errorf("PUT hits = %d, want 1 after soft-failed pull", putHits)
	}
}

func TestHandleShareReshare_FatalUnauthorizedPull(t *testing.T) {
	putHits := 0
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putHits++
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
	if putHits != 0 {
		t.Errorf("PUT must not run after fatal pull auth error")
	}
}

func TestHandleShareReshare_NoSharedReview(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	// softPull fatals with no shared review before upsert.
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleShareReshare_MethodNotAllowed(t *testing.T) {
	s, _ := newShareTestServer(t, "https://example.com", true)
	req := httptest.NewRequest(http.MethodGet, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleShareURL_DeleteUnpublishesRemotely(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	s.authMu.Lock()
	s.authToken = "tok_del"
	s.authMu.Unlock()
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc", "delete-tok")

	req := httptest.NewRequest(http.MethodDelete, "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/reviews" {
		t.Errorf("remote = %s %s, want DELETE /api/reviews", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok_del" {
		t.Errorf("Authorization = %q, want Bearer tok_del", gotAuth)
	}
	if sess.GetSharedURL() != "" {
		t.Errorf("hosted URL should be cleared")
	}
}

func TestHandleShareReshare_UpsertUnauthorized(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments"):
			json.NewEncoder(w).Encode([]any{})
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleShareReshare_NotFoundOnPull(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/gone", "delete-tok")

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleShareURL_DeleteRemoteFailure(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc", "delete-tok")

	req := httptest.NewRequest(http.MethodDelete, "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", w.Code, w.Body.String())
	}
	// Remote failed — local share state must remain so the user can retry.
	if sess.GetSharedURL() == "" {
		t.Errorf("hosted URL cleared despite remote unpublish failure")
	}
}

func TestHandleSharePull_MissingReviewFile(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")
	// Remove the seeded review file so pullAndMergeRemoteComments hits the
	// missing-file branch.
	reviewPath := filepath.Join(sess.OutputDir, ".crit", "review.json")
	if err := os.Remove(reviewPath); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/share/pull", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleShareReshare_EmptyFiles(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		http.NotFound(w, r)
	}))
	defer critWeb.Close()

	s, sess := newShareTestServer(t, critWeb.URL, true)
	sess.SetSharedURLAndToken(critWeb.URL+"/r/abc123", "delete-tok")
	// Clear session files so reshareUpsertInputs returns errNoFilesInSession.
	sess.Files = nil

	req := httptest.NewRequest(http.MethodPost, "/api/share/reshare", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}
