package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *Session) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	session := &Session{
		Mode:          "files",
		RepoRoot:      dir,
		ReviewRound:   1,
		nextID:        1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     "test.md",
				AbsPath:  path,
				Status:   "added",
				FileType: "markdown",
				Content:  "line1\nline2\nline3\n",
				FileHash: "sha256:testhash",
				Comments: []Comment{},
			},
		},
	}

	s, err := NewServer(session, frontendFS, "", "", nil, "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, session
}

func TestGetSession(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "files" {
		t.Errorf("mode = %q, want files", resp.Mode)
	}
	if len(resp.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].Path != "test.md" {
		t.Errorf("file path = %q", resp.Files[0].Path)
	}
}

func TestGetSession_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetFile(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != "test.md" {
		t.Errorf("path = %q", resp["path"])
	}
	if !strings.Contains(resp["content"].(string), "line1") {
		t.Error("content missing")
	}
}

func TestGetFile_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=nonexistent.go", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetFile_MissingPath(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPostFileComment(t *testing.T) {
	s, session := newTestServer(t)
	body := `{"start_line":1,"end_line":2,"body":"Fix this"}`
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", strings.NewReader(body))
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
	if len(session.GetComments("test.md")) != 1 {
		t.Error("comment not persisted")
	}
}

func TestPostFileComment_EmptyBody(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"start_line":1,"end_line":1,"body":""}`
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPostFileComment_InvalidLineRange(t *testing.T) {
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
			req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
}

func TestPostFileComment_InvalidJSON(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPostFileComment_FileNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"start_line":1,"end_line":1,"body":"test"}`
	req := httptest.NewRequest("POST", "/api/file/comments?path=nonexistent.go", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetFileComments(t *testing.T) {
	s, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "one", "", "")
	session.AddComment("test.md", 2, 2, "", "two", "", "")

	req := httptest.NewRequest("GET", "/api/file/comments?path=test.md", nil)
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
	s, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	body := `{"body":"updated"}`
	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"?path=test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if session.GetComments("test.md")[0].Body != "updated" {
		t.Error("comment not updated")
	}
}

func TestAPIUpdateComment_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"body":"x"}`
	req := httptest.NewRequest("PUT", "/api/comment/nonexistent?path=test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPIDeleteComment(t *testing.T) {
	s, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "to delete", "", "")

	req := httptest.NewRequest("DELETE", "/api/comment/"+c.ID+"?path=test.md", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if len(session.GetComments("test.md")) != 0 {
		t.Error("comment not deleted")
	}
}

func TestAPIDeleteComment_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/comment/nonexistent?path=test.md", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestClearAllComments(t *testing.T) {
	s, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "comment 1", "", "")
	session.AddComment("test.md", 2, 2, "", "comment 2", "", "")

	if len(session.GetComments("test.md")) != 2 {
		t.Fatal("expected 2 comments before clear")
	}

	req := httptest.NewRequest("DELETE", "/api/comments", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if len(session.GetComments("test.md")) != 0 {
		t.Error("comments not cleared")
	}
}

func TestReviewComments_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("PATCH", "/api/comments", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestFinish(t *testing.T) {
	s, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "note", "", "")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "finished" {
		t.Errorf("status = %q", resp["status"])
	}
	if resp["prompt"] == "" {
		t.Error("expected prompt when comments exist")
	}
	if resp["approved"] != false {
		t.Errorf("expected approved=false with unresolved comments, got %v", resp["approved"])
	}
}

func TestFinish_NoComments(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["prompt"] != "" {
		t.Errorf("expected empty prompt, got %q", resp["prompt"])
	}
	if resp["approved"] != true {
		t.Errorf("expected approved=true with no comments, got %v", resp["approved"])
	}
}

func TestFinish_PromptIncludesFileArgs(t *testing.T) {
	s, session := newTestServer(t)
	session.CLIArgs = []string{"test.md"}
	session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "`crit test.md`") {
		t.Errorf("expected prompt to contain 'crit test.md', got: %s", prompt)
	}
}

func TestFinish_PromptBareGitMode(t *testing.T) {
	s, session := newTestServer(t)
	session.Mode = "git"
	// CLIArgs stays nil — git mode
	session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "run: `crit`") {
		t.Errorf("expected prompt to end with 'run: `crit`', got: %s", prompt)
	}
}

func TestFinish_ApproveReturnsEmptyPrompt(t *testing.T) {
	s, _ := newTestServer(t)

	// No comments = approve → approved=true and empty prompt
	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["prompt"] != "" {
		t.Errorf("expected empty prompt for approve, got: %s", resp["prompt"])
	}
	if resp["approved"] != true {
		t.Errorf("expected approved=true, got %v", resp["approved"])
	}
}

func TestFinish_UnresolvedReturnsPromptWithInstructions(t *testing.T) {
	s, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["prompt"] == "" {
		t.Error("expected non-empty prompt when there are unresolved comments")
	}
	if resp["approved"] != false {
		t.Errorf("expected approved=false, got %v", resp["approved"])
	}
}

func TestReviewCycle_ApproveReturnsEmptyPrompt(t *testing.T) {
	s, session := newTestServer(t)
	session.SetAwaitingFirstReview(true)

	// Start review-cycle in background (it blocks until finish event)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("POST", "/api/review-cycle", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		done <- w
	}()

	time.Sleep(50 * time.Millisecond)

	// Trigger finish with no comments (approve)
	finishReq := httptest.NewRequest("POST", "/api/finish", nil)
	s.ServeHTTP(httptest.NewRecorder(), finishReq)

	select {
	case w := <-done:
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp["prompt"] != "" {
			t.Errorf("expected empty prompt for approve via review-cycle, got: %s", resp["prompt"])
		}
		if resp["approved"] != true {
			t.Errorf("expected approved=true via review-cycle, got %v", resp["approved"])
		}
	case <-time.After(2 * time.Second):
		t.Error("review-cycle did not return in time")
	}
}

func TestReviewCycle_UnresolvedReturnsPrompt(t *testing.T) {
	s, session := newTestServer(t)
	session.SetAwaitingFirstReview(true)
	session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("POST", "/api/review-cycle", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		done <- w
	}()

	time.Sleep(50 * time.Millisecond)

	finishReq := httptest.NewRequest("POST", "/api/finish", nil)
	s.ServeHTTP(httptest.NewRecorder(), finishReq)

	select {
	case w := <-done:
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp["prompt"] == "" {
			t.Error("expected non-empty prompt when there are unresolved comments")
		}
		if resp["approved"] != false {
			t.Errorf("expected approved=false via review-cycle, got %v", resp["approved"])
		}
	case <-time.After(2 * time.Second):
		t.Error("review-cycle did not return in time")
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

func TestHandleFiles_SymlinkTraversal(t *testing.T) {
	s, session := newTestServer(t)

	// Create a file outside the repo root
	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside repo root pointing outside
	linkPath := filepath.Join(session.RepoRoot, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	req := httptest.NewRequest("GET", "/files/escape/secret.txt", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Errorf("symlink traversal should be blocked, got 200 with body: %s", w.Body.String())
	}
}

func TestHandleFiles_Subdirectory(t *testing.T) {
	s, session := newTestServer(t)

	subdir := filepath.Join(session.RepoRoot, "images")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(subdir, "diagram.png")
	if err := os.WriteFile(imgPath, []byte("fake png"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/files/images/diagram.png", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "fake png" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestHandleFiles_ValidFile(t *testing.T) {
	s, session := newTestServer(t)

	imgPath := filepath.Join(session.RepoRoot, "image.png")
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
	s.shareURL = "https://crit.md"
	s.currentVersion = "v1.2.3"

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["share_url"] != "https://crit.md" {
		t.Errorf("share_url = %v, want https://crit.md", resp["share_url"])
	}
	if resp["hosted_url"] != "" {
		t.Errorf("hosted_url should be empty initially, got %v", resp["hosted_url"])
	}
	if resp["version"] != "v1.2.3" {
		t.Errorf("version = %v, want v1.2.3", resp["version"])
	}
	if resp["latest_version"] != "" {
		t.Errorf("latest_version should be empty before update check, got %v", resp["latest_version"])
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

	// Test the parsing logic via our mock
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
	var cfg map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["latest_version"] != "v9.9.9" {
		t.Errorf("config latest_version = %v, want v9.9.9", cfg["latest_version"])
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
	s, session := newTestServer(t)

	body := `{"url":"https://crit.md/r/abc123"}`
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if session.GetSharedURL() != "https://crit.md/r/abc123" {
		t.Errorf("shared URL = %q, want https://crit.md/r/abc123", session.GetSharedURL())
	}

	// Verify config now reflects the stored URL
	req2 := httptest.NewRequest("GET", "/api/config", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	var resp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["hosted_url"] != "https://crit.md/r/abc123" {
		t.Errorf("hosted_url = %v, want https://crit.md/r/abc123", resp["hosted_url"])
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
	s, session := newTestServer(t)
	session.SetSharedURLAndToken("", "mydeletetoken1234567890")

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["delete_token"] != "mydeletetoken1234567890" {
		t.Errorf("delete_token = %v", resp["delete_token"])
	}
}

func TestPostShareURL_SavesDeleteToken(t *testing.T) {
	s, session := newTestServer(t)

	body := `{"url":"https://crit.md/r/abc","delete_token":"deletetoken1234567890x"}`
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if session.GetDeleteToken() != "deletetoken1234567890x" {
		t.Errorf("delete token = %q", session.GetDeleteToken())
	}
}

func TestDeleteShareURL(t *testing.T) {
	s, session := newTestServer(t)
	session.SetSharedURLAndToken("https://crit.md/r/abc", "sometoken1234567890123")

	req := httptest.NewRequest("DELETE", "/api/share-url", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if session.GetSharedURL() != "" {
		t.Errorf("hostedURL should be cleared")
	}
	if session.GetDeleteToken() != "" {
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

func TestGetFileDiff_CodeFile(t *testing.T) {
	s, session := newTestServer(t)
	// Add a code file with diff hunks
	session.mu.Lock()
	session.Files = append(session.Files, &FileEntry{
		Path:     "main.go",
		AbsPath:  "/tmp/main.go",
		Status:   "modified",
		FileType: "code",
		Content:  "package main",
		Comments: []Comment{},
		DiffHunks: []DiffHunk{
			{OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4, Header: "@@ -1,3 +1,4 @@"},
		},
	})
	session.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/file/diff?path=main.go", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Hunks []DiffHunk `json:"hunks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Hunks) != 1 {
		t.Errorf("expected 1 hunk, got %d", len(resp.Hunks))
	}
}

func TestGetFileDiff_MarkdownFilesMode(t *testing.T) {
	s, session := newTestServer(t)
	// Set previous content for the markdown file
	session.mu.Lock()
	session.Files[0].PreviousContent = "old content"
	session.Files[0].Content = "new content"
	session.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Hunks []DiffHunk `json:"hunks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Hunks) == 0 {
		t.Error("expected non-empty diff hunks")
	}
}

func TestGetFileDiff_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=nonexistent.go", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetFileDiff_MissingPath(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCommentByID_MissingPath(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/comment/c1", strings.NewReader(`{"body":"x"}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ===== Scope Query Parameter Tests =====

func TestGetSession_IncludesAvailableScopes(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	scopes, ok := resp["available_scopes"]
	if !ok {
		t.Fatal("response missing available_scopes field")
	}
	scopeList, ok := scopes.([]any)
	if !ok {
		t.Fatalf("available_scopes is not an array: %T", scopes)
	}
	// Test server is not in a real git repo, so only "all" is available
	// (git commands to detect staged/unstaged will fail)
	if len(scopeList) < 1 {
		t.Errorf("expected at least 1 scope, got %d: %v", len(scopeList), scopeList)
	}
	if scopeList[0] != "all" {
		t.Errorf("first scope = %q, want all", scopeList[0])
	}
}

func TestGetSession_ScopeAll_SameAsNoScope(t *testing.T) {
	s, _ := newTestServer(t)

	// No scope
	req1 := httptest.NewRequest("GET", "/api/session", nil)
	w1 := httptest.NewRecorder()
	s.ServeHTTP(w1, req1)

	// scope=all
	req2 := httptest.NewRequest("GET", "/api/session?scope=all", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)

	if w1.Code != 200 || w2.Code != 200 {
		t.Fatalf("status codes: %d, %d", w1.Code, w2.Code)
	}

	var resp1, resp2 SessionInfo
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp1.Mode != resp2.Mode {
		t.Errorf("mode mismatch: %q vs %q", resp1.Mode, resp2.Mode)
	}
	if len(resp1.Files) != len(resp2.Files) {
		t.Errorf("file count mismatch: %d vs %d", len(resp1.Files), len(resp2.Files))
	}
}

func TestGetFileDiff_ScopeAll_SameAsNoScope(t *testing.T) {
	s, session := newTestServer(t)
	// Add a code file with diff hunks
	session.mu.Lock()
	session.Files = append(session.Files, &FileEntry{
		Path:     "main.go",
		AbsPath:  "/tmp/main.go",
		Status:   "modified",
		FileType: "code",
		Content:  "package main",
		Comments: []Comment{},
		DiffHunks: []DiffHunk{
			{OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4, Header: "@@ -1,3 +1,4 @@"},
		},
	})
	session.mu.Unlock()

	// No scope
	req1 := httptest.NewRequest("GET", "/api/file/diff?path=main.go", nil)
	w1 := httptest.NewRecorder()
	s.ServeHTTP(w1, req1)

	// scope=all
	req2 := httptest.NewRequest("GET", "/api/file/diff?path=main.go&scope=all", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)

	if w1.Code != 200 || w2.Code != 200 {
		t.Fatalf("status codes: %d, %d", w1.Code, w2.Code)
	}
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("scope=all response differs from no-scope response")
	}
}

func TestGetFileDiff_ScopeStaged_ValidResponse(t *testing.T) {
	s, session := newTestServer(t)
	// Add a code file with diff hunks to the session
	session.mu.Lock()
	session.Files = append(session.Files, &FileEntry{
		Path:     "app.go",
		AbsPath:  "/tmp/app.go",
		Status:   "modified",
		FileType: "code",
		Content:  "package main",
		Comments: []Comment{},
		DiffHunks: []DiffHunk{
			{OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4, Header: "@@ -1,3 +1,4 @@"},
		},
	})
	session.mu.Unlock()

	// scope=staged — even though this is not a real git repo,
	// the handler should return a valid response (empty hunks from failed git call)
	req := httptest.NewRequest("GET", "/api/file/diff?path=app.go&scope=staged", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hunks []DiffHunk `json:"hunks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// Should parse as valid JSON with a hunks field (may be empty without real git)
	if resp.Hunks == nil {
		t.Error("hunks should not be nil (should be empty array)")
	}
}

func TestGetFileDiff_ScopeNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=nonexistent.go&scope=staged", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestGetFile_NotInSession_FallbackToDisk tests the scenario where switching
// scopes (e.g. from "all" to "unstaged") returns files that weren't in the
// session's original file list. The /api/file endpoint should fall back to
// reading from disk instead of returning 404 (which caused the frontend to hang).
func TestGetFile_NotInSession_FallbackToDisk(t *testing.T) {
	s, session := newTestServer(t)

	// Create a file on disk that is NOT in session.Files
	extraPath := filepath.Join(session.RepoRoot, "extra.go")
	if err := os.WriteFile(extraPath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify it's not in the session's file list
	session.mu.RLock()
	found := session.fileByPathLocked("extra.go")
	session.mu.RUnlock()
	if found != nil {
		t.Fatal("extra.go should NOT be in session files for this test")
	}

	// Request it via /api/file — before the fix this returned 404
	req := httptest.NewRequest("GET", "/api/file?path=extra.go", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (file exists on disk but not in session)", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != "extra.go" {
		t.Errorf("path = %q, want extra.go", resp["path"])
	}
	if resp["content"] != "package main\n" {
		t.Errorf("content = %q, want file content from disk", resp["content"])
	}
	if resp["file_type"] != "code" {
		t.Errorf("file_type = %q, want code", resp["file_type"])
	}
}

// TestGetFile_NotInSession_PathTraversal verifies the disk fallback
// still blocks path traversal attempts.
func TestGetFile_NotInSession_PathTraversal(t *testing.T) {
	s, _ := newTestServer(t)

	for _, path := range []string{"../etc/passwd", "foo/../../etc/passwd"} {
		req := httptest.NewRequest("GET", "/api/file?path="+path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Errorf("path %q: status = %d, want 404", path, w.Code)
		}
	}
}

func TestHandleFinish_PromptIncludesAuthor(t *testing.T) {
	srv, session := newTestServer(t)
	session.AddComment(session.Files[0].Path, 1, 1, "", "fix this", "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "--author") {
		t.Errorf("expected prompt to mention --author, got: %s", prompt)
	}
}

func TestHandleFinishEmitsSSEEvent(t *testing.T) {
	srv, session := newTestServer(t)
	session.AddComment(session.Files[0].Path, 1, 1, "", "test", "", "")

	// Subscribe before triggering finish
	ch := session.Subscribe()
	defer session.Unsubscribe(ch)

	req := httptest.NewRequest(http.MethodPost, "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	select {
	case event := <-ch:
		if event.Type != "finish" {
			t.Errorf("expected finish event, got %s", event.Type)
		}
		if event.Content == "" {
			t.Error("expected non-empty content in finish event")
		}
		// Verify the event content is structured JSON with prompt and approved fields
		var data map[string]any
		if err := json.Unmarshal([]byte(event.Content), &data); err != nil {
			t.Errorf("expected JSON content in finish event, got: %s", event.Content)
		}
		if data["prompt"] == "" {
			t.Error("expected non-empty prompt in finish event data")
		}
		if data["approved"] != false {
			t.Errorf("expected approved=false with unresolved comments, got %v", data["approved"])
		}
	case <-time.After(time.Second):
		t.Fatal("no finish event received")
	}
}

func TestWaitForEventReturnsOnFinish(t *testing.T) {
	srv, session := newTestServer(t)
	session.AddComment(session.Files[0].Path, 1, 1, "", "test", "", "")

	var resp *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/wait-for-event", nil)
		resp = httptest.NewRecorder()
		srv.ServeHTTP(resp, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	finishReq := httptest.NewRequest(http.MethodPost, "/api/finish", nil)
	finishW := httptest.NewRecorder()
	srv.ServeHTTP(finishW, finishReq)

	select {
	case <-done:
		if resp.Code != 200 {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
		var event map[string]string
		json.NewDecoder(resp.Body).Decode(&event)
		if event["type"] != "finish" {
			t.Errorf("expected finish event, got %s", event["type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long-poll did not return after finish")
	}
}

func TestWaitForEventIgnoresOtherEvents(t *testing.T) {
	srv, session := newTestServer(t)
	session.AddComment(session.Files[0].Path, 1, 1, "", "test", "", "")

	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/wait-for-event", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	session.notify(SSEEvent{Type: "comments-changed"})

	select {
	case <-done:
		t.Fatal("long-poll should not return on comments-changed event")
	case <-time.After(200 * time.Millisecond):
		// Good — still blocking
	}
}

func TestWaitForEventRespectsCancel(t *testing.T) {
	srv, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/wait-for-event", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 504, got %d", w.Code)
	}
}

func TestWaitForEvent_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/wait-for-event", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestGetFile_NotInSession_NotOnDisk verifies that files not in session
// AND not on disk still return 404.
func TestGetFile_NotInSession_NotOnDisk(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/file?path=doesnotexist.go", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ===== File List Endpoint Tests =====

func TestGetFilesList(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "src/main.go"), "package main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add file")

	session := &Session{
		Mode:          "git",
		RepoRoot:      dir,
		ReviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files:         []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", nil, "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("no query returns capped results", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/list", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var files []string
		if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
			t.Fatal(err)
		}

		if len(files) == 0 {
			t.Fatal("expected at least 1 file")
		}
		if len(files) > 10 {
			t.Fatalf("expected at most 10 files, got %d", len(files))
		}
	})

	t.Run("query filters by fuzzy match", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/list?q=main", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		var files []string
		json.NewDecoder(w.Body).Decode(&files)

		found := false
		for _, f := range files {
			if f == "src/main.go" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected src/main.go in filtered results, got: %v", files)
		}
	})
}

func TestGetFilesList_RespectsIgnorePatterns(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "debug.log"), "log data")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add files")

	session := &Session{
		Mode:           "git",
		RepoRoot:       dir,
		ReviewRound:    1,
		IgnorePatterns: []string{"*.log"},
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
		Files:          []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", nil, "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/files/list", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var files []string
	json.NewDecoder(w.Body).Decode(&files)

	for _, f := range files {
		if f == "debug.log" {
			t.Error("ignored file debug.log should not appear")
		}
	}
}

func TestGetFilesList_FilesMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.js"), "console.log('hi')")
	writeFile(t, filepath.Join(dir, "lib/util.js"), "module.exports = {}")
	// node_modules should be excluded by WalkFiles
	writeFile(t, filepath.Join(dir, "node_modules/pkg/index.js"), "module")

	session := &Session{
		Mode:          "files",
		RepoRoot:      dir,
		ReviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files:         []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", nil, "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/files/list", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var files []string
	json.NewDecoder(w.Body).Decode(&files)

	found := false
	for _, f := range files {
		if f == "app.js" {
			found = true
		}
		if strings.HasPrefix(f, "node_modules/") {
			t.Errorf("node_modules file should not appear: %s", f)
		}
	}
	if !found {
		t.Errorf("expected app.js in file list, got: %v", files)
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/health: got %d, want 200", w.Code)
	}
}

func TestReviewCycleFirstRound(t *testing.T) {
	srv, session := newTestServer(t)

	done := make(chan int)
	go func() {
		req := httptest.NewRequest("POST", "/api/review-cycle", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		done <- w.Code
	}()

	// Give the handler time to start blocking
	time.Sleep(50 * time.Millisecond)

	// Simulate user clicking "Finish Review"
	session.WriteFiles()
	session.notify(SSEEvent{Type: "finish", Content: "test feedback"})

	code := <-done
	if code != http.StatusOK {
		t.Errorf("POST /api/review-cycle: got %d, want 200", code)
	}
}

func TestGetFilesList_MethodNotAllowed(t *testing.T) {
	session := &Session{
		Files:         []*FileEntry{},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}
	srv, _ := NewServer(session, frontendFS, "", "", nil, "", "", 0, "")
	req := httptest.NewRequest("POST", "/api/files/list", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestSessionIncludesReviewComments(t *testing.T) {
	srv, sess := newTestServer(t)
	sess.AddReviewComment("general note", "")
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	rc, ok := result["review_comments"].([]any)
	if !ok {
		t.Fatal("expected review_comments array in session response")
	}
	if len(rc) != 1 {
		t.Errorf("expected 1 review comment, got %d", len(rc))
	}
}

func TestFinishPromptMentionsScopes(t *testing.T) {
	srv, sess := newTestServer(t)
	sess.AddReviewComment("address all issues", "")
	if _, ok := sess.AddFileComment("test.md", "restructure this file", ""); !ok {
		t.Fatal("AddFileComment failed")
	}
	if _, ok := sess.AddComment("test.md", 1, 1, "", "bug here", "", ""); !ok {
		t.Fatal("AddComment failed")
	}
	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	prompt := result["prompt"]
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "review_comments") {
		t.Error("prompt should mention review_comments array")
	}
	if !strings.Contains(prompt, "scope") {
		t.Error("prompt should mention scope field")
	}
}

func TestReviewCommentsAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	// POST — create review comment
	body := strings.NewReader(`{"body": "general note"}`)
	req := httptest.NewRequest("POST", "/api/comments", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var c Comment
	json.Unmarshal(w.Body.Bytes(), &c)
	if c.Scope != "review" {
		t.Errorf("expected scope 'review', got %q", c.Scope)
	}

	// GET — list review comments
	req = httptest.NewRequest("GET", "/api/comments", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", w.Code)
	}
	var comments []Comment
	json.Unmarshal(w.Body.Bytes(), &comments)
	if len(comments) != 1 {
		t.Fatalf("expected 1, got %d", len(comments))
	}

	// PUT — update review comment
	body = strings.NewReader(`{"body": "updated note"}`)
	req = httptest.NewRequest("PUT", "/api/review-comment/"+c.ID, body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE — single review comment
	req = httptest.NewRequest("DELETE", "/api/review-comment/"+c.ID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE expected 204, got %d", w.Code)
	}

	// GET — verify empty
	req = httptest.NewRequest("GET", "/api/comments", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &comments)
	if len(comments) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(comments))
	}
}

func TestReviewCommentRepliesAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a review comment first
	body := strings.NewReader(`{"body": "general note", "author": "reviewer"}`)
	req := httptest.NewRequest("POST", "/api/comments", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST comment expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var c Comment
	json.Unmarshal(w.Body.Bytes(), &c)

	// POST reply
	body = strings.NewReader(`{"body": "I will fix this", "author": "agent"}`)
	req = httptest.NewRequest("POST", "/api/review-comment/"+c.ID+"/replies", body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST reply expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var reply Reply
	json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.Body != "I will fix this" {
		t.Errorf("expected reply body 'I will fix this', got %q", reply.Body)
	}
	if reply.Author != "agent" {
		t.Errorf("expected reply author 'agent', got %q", reply.Author)
	}

	// PUT reply — update
	body = strings.NewReader(`{"body": "updated reply"}`)
	req = httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/replies/"+reply.ID, body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT reply expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updatedReply Reply
	json.Unmarshal(w.Body.Bytes(), &updatedReply)
	if updatedReply.Body != "updated reply" {
		t.Errorf("expected updated body 'updated reply', got %q", updatedReply.Body)
	}

	// DELETE reply
	req = httptest.NewRequest("DELETE", "/api/review-comment/"+c.ID+"/replies/"+reply.ID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE reply expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify reply is gone by checking the comment
	comments := srv.session.GetReviewComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(comments[0].Replies) != 0 {
		t.Errorf("expected 0 replies after delete, got %d", len(comments[0].Replies))
	}
}

func TestReviewCommentReplyNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	// POST reply to nonexistent comment
	body := strings.NewReader(`{"body": "reply", "author": "agent"}`)
	req := httptest.NewRequest("POST", "/api/review-comment/nonexistent/replies", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestPostFileScopedComment(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "this file needs restructuring", "scope": "file"}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var c Comment
	json.Unmarshal(w.Body.Bytes(), &c)
	if c.Scope != "file" {
		t.Errorf("expected scope 'file', got %q", c.Scope)
	}
	if c.StartLine != 0 || c.EndLine != 0 {
		t.Errorf("expected zero lines, got %d-%d", c.StartLine, c.EndLine)
	}
}

func TestPostFileScopedCommentRequiresBody(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"scope": "file"}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResolveReviewCommentAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a review comment
	body := strings.NewReader(`{"body": "needs fixing"}`)
	req := httptest.NewRequest("POST", "/api/comments", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var c Comment
	json.Unmarshal(w.Body.Bytes(), &c)

	// Resolve it
	body = strings.NewReader(`{"resolved": true}`)
	req = httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT resolve expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resolved Comment
	json.Unmarshal(w.Body.Bytes(), &resolved)
	if !resolved.Resolved {
		t.Error("expected comment to be resolved")
	}

	// Unresolve it
	body = strings.NewReader(`{"resolved": false}`)
	req = httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT unresolve expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var unresolved Comment
	json.Unmarshal(w.Body.Bytes(), &unresolved)
	if unresolved.Resolved {
		t.Error("expected comment to be unresolved")
	}

	// Not found
	body = strings.NewReader(`{"resolved": true}`)
	req = httptest.NewRequest("PUT", "/api/review-comment/nonexistent/resolve", body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleConfig_AgentCmdEnabled(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	if data["agent_cmd_enabled"] != false {
		t.Fatal("expected agent_cmd_enabled=false when not configured")
	}
	s.agentCmd = "claude -p"
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, httptest.NewRequest("GET", "/api/config", nil))
	json.Unmarshal(w2.Body.Bytes(), &data)
	if data["agent_cmd_enabled"] != true {
		t.Fatal("expected agent_cmd_enabled=true when configured")
	}
}

func TestFuzzyScore(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		text    string
		wantHit bool // true if score >= 0 (match), false if -1 (no match)
	}{
		{name: "empty query on empty text", query: "", text: "", wantHit: true},
		{name: "empty query penalized by length", query: "", text: "anything.go", wantHit: false},
		{name: "exact match", query: "main.go", text: "main.go", wantHit: true},
		{name: "substring match", query: "main", text: "main.go", wantHit: true},
		{name: "fuzzy match scattered", query: "mgo", text: "main.go", wantHit: true},
		{name: "no match missing char", query: "xyz", text: "main.go", wantHit: false},
		{name: "case insensitive", query: "main", text: "MAIN.GO", wantHit: true},
		{name: "query longer than text", query: "toolongquery", text: "short", wantHit: false},
		{name: "path separator bonus", query: "sg", text: "src/git.go", wantHit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fuzzyScore(tt.query, tt.text)
			gotHit := score >= 0
			if gotHit != tt.wantHit {
				t.Errorf("fuzzyScore(%q, %q) = %v, wantHit=%v", tt.query, tt.text, score, tt.wantHit)
			}
		})
	}
}

func TestFuzzyScore_Ranking(t *testing.T) {
	// Exact prefix match should score higher than scattered match
	exactScore := fuzzyScore("main", "main.go")
	scatteredScore := fuzzyScore("main", "middleware/auth_interceptor.go")
	if exactScore <= scatteredScore {
		t.Errorf("exact prefix score (%v) should beat scattered score (%v)", exactScore, scatteredScore)
	}

	// Shorter path with same match should score higher (length penalty)
	shortScore := fuzzyScore("srv", "server.go")
	longScore := fuzzyScore("srv", "internal/services/server_runner.go")
	if shortScore <= longScore {
		t.Errorf("short path score (%v) should beat long path score (%v)", shortScore, longScore)
	}
}

func TestFuzzyFilterPaths(t *testing.T) {
	paths := []string{
		"main.go",
		"server.go",
		"session.go",
		"internal/middleware.go",
		"README.md",
		"config.go",
	}

	t.Run("empty query returns nothing because length penalty", func(t *testing.T) {
		results := fuzzyFilterPaths(paths, "", 3)
		if len(results) != 0 {
			t.Errorf("got %d results, want 0 (length penalty makes score < 0)", len(results))
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		results := fuzzyFilterPaths(paths, "xyz", 10)
		if len(results) != 0 {
			t.Errorf("got %d results, want 0", len(results))
		}
	})

	t.Run("exact match appears first", func(t *testing.T) {
		results := fuzzyFilterPaths(paths, "server.go", 5)
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		if results[0] != "server.go" {
			t.Errorf("first result = %q, want %q", results[0], "server.go")
		}
	})

	t.Run("substring match works", func(t *testing.T) {
		results := fuzzyFilterPaths(paths, "sess", 5)
		found := false
		for _, r := range results {
			if r == "session.go" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected session.go in results: %v", results)
		}
	})

	t.Run("limit caps results", func(t *testing.T) {
		results := fuzzyFilterPaths(paths, "go", 2)
		if len(results) > 2 {
			t.Errorf("got %d results, want at most 2", len(results))
		}
	})

	t.Run("nil paths returns empty", func(t *testing.T) {
		results := fuzzyFilterPaths(nil, "test", 10)
		if len(results) != 0 {
			t.Errorf("got %d results, want 0", len(results))
		}
	})
}

func TestHandleSession_PlanMode(t *testing.T) {
	session := &Session{
		Mode:    "plan",
		PlanDir: "/tmp/test-plan",
		Files: []*FileEntry{
			{Path: "auth-flow.md", FileType: "markdown", Content: "# Plan", Comments: []Comment{}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	srv, _ := NewServer(session, frontendFS, "", "", nil, "", "dev", 0, "")

	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["mode"] != "plan" {
		t.Errorf("mode = %v, want 'plan'", resp["mode"])
	}
}

func TestReadinessGate_Returns503WhenNotReady(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", "", nil, "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	endpoints := []string{
		"/api/session",
		"/api/config",
		"/api/comments",
	}
	for _, ep := range endpoints {
		req := httptest.NewRequest("GET", ep, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: got status %d, want 503", ep, w.Code)
		}
		var body map[string]string
		json.Unmarshal(w.Body.Bytes(), &body)
		if body["status"] != "loading" {
			t.Errorf("%s: got status=%q, want 'loading'", ep, body["status"])
		}
	}
}

func TestReadinessGate_HealthAlwaysOK(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", "", nil, "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health: got status %d, want 200", w.Code)
	}
}

func TestReadinessGate_Returns200AfterSetSession(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", "", nil, "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("hello\n"), 0644)

	session := &Session{
		Mode:          "files",
		RepoRoot:      dir,
		ReviewRound:   1,
		nextID:        1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     "test.md",
				AbsPath:  path,
				Status:   "added",
				FileType: "markdown",
				Content:  "hello\n",
				FileHash: "sha256:testhash",
				Comments: []Comment{},
			},
		},
	}

	s.SetSession(session, nil)

	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("session after SetSession: got status %d, want 200", w.Code)
	}
}

func TestRouteCommentByID(t *testing.T) {
	tests := []struct {
		name    string
		trimmed string
		want    commentRoute
		ok      bool
	}{
		{"empty", "", commentRoute{}, false},
		{"plain ID", "c5", commentRoute{kind: "comment", id: "c5"}, true},
		{"replies", "c5/replies", commentRoute{kind: "reply", id: "c5", sub: ""}, true},
		{"reply ID", "c5/replies/r2", commentRoute{kind: "reply", id: "c5", sub: "r2"}, true},
		{"resolve", "c5/resolve", commentRoute{kind: "resolve", id: "c5"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := routeCommentByID(tt.trimmed)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("route = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadinessGate_Returns500OnInitError(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", "", nil, "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	s.SetInitErr(fmt.Errorf("no changed files detected"))

	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "error" {
		t.Errorf("got status=%q, want 'error'", body["status"])
	}
	if !strings.Contains(body["message"], "no changed files") {
		t.Errorf("got message=%q, want it to contain 'no changed files'", body["message"])
	}
}
