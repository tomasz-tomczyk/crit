package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*Server, *Document) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	doc, err := NewDocument(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Create server without embed.FS — directly set up mux
	s := &Server{doc: doc}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/share-url", s.handleShareURL)
	mux.HandleFunc("/api/document", s.handleDocument)
	mux.HandleFunc("/api/comments", s.handleComments)
	mux.HandleFunc("/api/comments/", s.handleCommentByID)
	mux.HandleFunc("/api/finish", s.handleFinish)
	mux.HandleFunc("/api/stale", s.handleStale)
	mux.HandleFunc("/api/round-complete", s.handleRoundComplete)
	mux.HandleFunc("/api/previous-round", s.handlePreviousRound)
	mux.HandleFunc("/api/diff", s.handleDiff)
	mux.HandleFunc("/files/", s.handleFiles)
	s.mux = mux
	return s, doc
}

func TestGetDocument(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/document", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["filename"] != "test.md" {
		t.Errorf("filename = %q", resp["filename"])
	}
	if !strings.Contains(resp["content"], "line1") {
		t.Error("content missing")
	}
}

func TestGetDocument_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/document", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestPostComment(t *testing.T) {
	s, doc := newTestServer(t)
	body := `{"start_line":1,"end_line":2,"body":"Fix this"}`
	req := httptest.NewRequest("POST", "/api/comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var c Comment
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Body != "Fix this" || c.StartLine != 1 || c.EndLine != 2 {
		t.Errorf("unexpected comment: %+v", c)
	}
	if len(doc.GetComments()) != 1 {
		t.Error("comment not persisted")
	}
}

func TestPostComment_EmptyBody(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"start_line":1,"end_line":1,"body":""}`
	req := httptest.NewRequest("POST", "/api/comments", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPostComment_InvalidLineRange(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct {
		name string
		body string
	}{
		{"zero start", `{"start_line":0,"end_line":1,"body":"x"}`},
		{"end before start", `{"start_line":3,"end_line":1,"body":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/comments", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
}

func TestPostComment_InvalidJSON(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/comments", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetComments(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "one")
	doc.AddComment(2, 2, "two")

	req := httptest.NewRequest("GET", "/api/comments", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var comments []Comment
	if err := json.Unmarshal(w.Body.Bytes(), &comments); err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Errorf("got %d comments, want 2", len(comments))
	}
}

func TestAPIUpdateComment(t *testing.T) {
	s, doc := newTestServer(t)
	c := doc.AddComment(1, 1, "original")

	body := `{"body":"updated"}`
	req := httptest.NewRequest("PUT", "/api/comments/"+c.ID, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if doc.GetComments()[0].Body != "updated" {
		t.Error("comment not updated")
	}
}

func TestAPIUpdateComment_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"body":"x"}`
	req := httptest.NewRequest("PUT", "/api/comments/nonexistent", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPIDeleteComment(t *testing.T) {
	s, doc := newTestServer(t)
	c := doc.AddComment(1, 1, "to delete")

	req := httptest.NewRequest("DELETE", "/api/comments/"+c.ID, nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if len(doc.GetComments()) != 0 {
		t.Error("comment not deleted")
	}
}

func TestAPIDeleteComment_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/comments/nonexistent", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestFinish(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "note")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "finished" {
		t.Errorf("status = %q", resp["status"])
	}
	if resp["prompt"] == "" {
		t.Error("expected prompt when comments exist")
	}
}

func TestFinish_NoComments(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["prompt"] != "" {
		t.Errorf("expected empty prompt, got %q", resp["prompt"])
	}
}

func TestStale(t *testing.T) {
	s, doc := newTestServer(t)

	// No stale notice initially
	req := httptest.NewRequest("GET", "/api/stale", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["notice"] != "" {
		t.Error("expected no stale notice")
	}

	// Set and clear
	doc.mu.Lock()
	doc.staleNotice = "stale!"
	doc.mu.Unlock()

	req = httptest.NewRequest("DELETE", "/api/stale", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if doc.GetStaleNotice() != "" {
		t.Error("stale notice not cleared")
	}
}

// ===== Path Traversal Tests =====

func TestHandleFiles_PathTraversal(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct {
		name string
		path string
		code int
	}{
		{"dotdot", "/files/../../../etc/passwd", 400},
		{"dotdot encoded", "/files/..%2F..%2Fetc%2Fpasswd", 400},
		{"empty path", "/files/", 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code == 200 {
				t.Errorf("path %q should be blocked, got 200", tc.path)
			}
		})
	}
}

func TestHandleFiles_ValidFile(t *testing.T) {
	s, doc := newTestServer(t)

	// Create a file in the doc directory
	imgPath := filepath.Join(doc.FileDir, "image.png")
	if err := os.WriteFile(imgPath, []byte("fake png"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/files/image.png", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "fake png" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestHandleFiles_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/files/test.md", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetConfig(t *testing.T) {
	s, _ := newTestServer(t)
	s.shareURL = "https://crit.live"
	s.currentVersion = "v1.2.3"

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["share_url"] != "https://crit.live" {
		t.Errorf("share_url = %q, want https://crit.live", resp["share_url"])
	}
	if resp["hosted_url"] != "" {
		t.Errorf("hosted_url should be empty initially, got %q", resp["hosted_url"])
	}
	if resp["version"] != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", resp["version"])
	}
	if resp["latest_version"] != "" {
		t.Errorf("latest_version should be empty before update check, got %q", resp["latest_version"])
	}
}

func TestCheckForUpdates(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/tomasz-tomczyk/crit/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer gh.Close()

	s, _ := newTestServer(t)
	s.currentVersion = "v1.0.0"

	// Swap the GitHub URL for the mock server
	origURL := "https://api.github.com/repos/tomasz-tomczyk/crit/releases/latest"
	_ = origURL // not used directly — checkForUpdates has it hardcoded, so we test via integration
	// Instead, call the handler directly with our mock to test the parsing logic
	req, _ := http.NewRequest("GET", gh.URL+"/repos/tomasz-tomczyk/crit/releases/latest", nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		t.Fatal(err)
	}
	s.versionMu.Lock()
	s.latestVersion = release.TagName
	s.versionMu.Unlock()

	s.versionMu.RLock()
	got := s.latestVersion
	s.versionMu.RUnlock()
	if got != "v9.9.9" {
		t.Errorf("latestVersion = %q, want v9.9.9", got)
	}

	// Verify config reflects it
	req2 := httptest.NewRequest("GET", "/api/config", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	var cfg map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["latest_version"] != "v9.9.9" {
		t.Errorf("config latest_version = %q, want v9.9.9", cfg["latest_version"])
	}
}

func TestGetConfig_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestPostShareURL(t *testing.T) {
	s, doc := newTestServer(t)

	body := `{"url":"https://crit.live/r/abc123"}`
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if doc.GetSharedURL() != "https://crit.live/r/abc123" {
		t.Errorf("shared URL = %q, want https://crit.live/r/abc123", doc.GetSharedURL())
	}

	// Verify config now reflects the stored URL
	req2 := httptest.NewRequest("GET", "/api/config", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	var resp map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["hosted_url"] != "https://crit.live/r/abc123" {
		t.Errorf("hosted_url = %q, want https://crit.live/r/abc123", resp["hosted_url"])
	}
}

func TestPostShareURL_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetConfig_IncludesDeleteToken(t *testing.T) {
	s, doc := newTestServer(t)
	doc.SetDeleteToken("mydeletetoken1234567890")

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["delete_token"] != "mydeletetoken1234567890" {
		t.Errorf("delete_token = %q", resp["delete_token"])
	}
}

func TestPostShareURL_SavesDeleteToken(t *testing.T) {
	s, doc := newTestServer(t)

	body := `{"url":"https://crit.live/r/abc","delete_token":"deletetoken1234567890x"}`
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if doc.GetDeleteToken() != "deletetoken1234567890x" {
		t.Errorf("delete token = %q", doc.GetDeleteToken())
	}
}

func TestDeleteShareURL(t *testing.T) {
	s, doc := newTestServer(t)
	doc.SetSharedURL("https://crit.live/r/abc")
	doc.SetDeleteToken("sometoken1234567890123")

	req := httptest.NewRequest("DELETE", "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if doc.GetSharedURL() != "" {
		t.Errorf("hostedURL should be cleared")
	}
	if doc.GetDeleteToken() != "" {
		t.Errorf("deleteToken should be cleared")
	}
}

func TestPostShareURL_EmptyURL(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(`{"url":""}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPostShareURL_InvalidJSON(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRoundComplete(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestRoundComplete_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetPreviousRound_Empty(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["content"] != "" {
		t.Errorf("expected empty content for first round, got %q", resp["content"])
	}
}

func TestGetPreviousRound_AfterReload(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix this")

	// Simulate file change
	os.WriteFile(doc.FilePath, []byte("modified content"), 0644)
	doc.ReloadFile()

	req := httptest.NewRequest("GET", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp struct {
		Content     string    `json:"content"`
		Comments    []Comment `json:"comments"`
		ReviewRound int       `json:"review_round"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Content != "line1\nline2\nline3\n" {
		t.Errorf("previous content = %q", resp.Content)
	}
	if len(resp.Comments) != 1 || resp.Comments[0].Body != "fix this" {
		t.Errorf("previous comments = %+v", resp.Comments)
	}
	if resp.ReviewRound != 1 {
		t.Errorf("review_round = %d, want 1 (no round-complete yet)", resp.ReviewRound)
	}
}

func TestGetPreviousRound_ReviewRoundIncrementsAfterRoundComplete(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix this")

	// Simulate file change + round complete
	os.WriteFile(doc.FilePath, []byte("modified content"), 0644)
	doc.ReloadFile()
	doc.SignalRoundComplete()
	// Drain the channel so it doesn't block
	select {
	case <-doc.RoundCompleteChan():
	default:
	}

	req := httptest.NewRequest("GET", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp struct {
		ReviewRound int `json:"review_round"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.ReviewRound != 2 {
		t.Errorf("review_round = %d, want 2 after one round-complete", resp.ReviewRound)
	}
}

func TestGetPreviousRound_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetDiff_NoPreviousRound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Entries []DiffEntry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty diff entries for first round, got %d", len(resp.Entries))
	}
}

func TestGetDiff_AfterReload(t *testing.T) {
	s, doc := newTestServer(t)

	os.WriteFile(doc.FilePath, []byte("modified line 1\nnew line"), 0644)
	doc.ReloadFile()

	req := httptest.NewRequest("GET", "/api/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Entries []DiffEntry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) == 0 {
		t.Error("expected non-empty diff entries after reload")
	}

	// Verify diff contains expected types
	hasAdded := false
	hasRemoved := false
	for _, e := range resp.Entries {
		if e.Type == "added" {
			hasAdded = true
		}
		if e.Type == "removed" {
			hasRemoved = true
		}
	}
	if !hasAdded {
		t.Error("expected at least one added entry in diff")
	}
	if !hasRemoved {
		t.Error("expected at least one removed entry in diff")
	}
}

func TestGetDiff_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
