package main

import (
	"encoding/json"
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
	req := httptest.NewRequest("GET", "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
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
