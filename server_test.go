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
		Mode:        "files",
		RepoRoot:    dir,
		ReviewRound: 1,

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

	s, err := NewServer(session, frontendFS, "", "", "", "test", 0, "")
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
	s.githubAPIURL = gh.URL

	s.CheckForUpdates()

	s.versionMu.RLock()
	got := s.latestVersion
	s.versionMu.RUnlock()
	if got != "v9.9.9" {
		t.Errorf("latestVersion = %q, want v9.9.9", got)
	}

	// Verify config API reflects it
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var cfg map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["latest_version"] != "v9.9.9" {
		t.Errorf("config latest_version = %v, want v9.9.9", cfg["latest_version"])
	}
}

func TestCheckForUpdates_SkipsDevVersion(t *testing.T) {
	s, _ := newTestServer(t)
	s.currentVersion = "dev"
	s.CheckForUpdates()

	s.versionMu.RLock()
	got := s.latestVersion
	s.versionMu.RUnlock()
	if got != "" {
		t.Errorf("latestVersion should be empty for dev builds, got %q", got)
	}
}

func TestCheckForUpdates_HandlesServerError(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gh.Close()

	s, _ := newTestServer(t)
	s.currentVersion = "v1.0.0"
	s.githubAPIURL = gh.URL
	s.CheckForUpdates()

	s.versionMu.RLock()
	got := s.latestVersion
	s.versionMu.RUnlock()
	if got != "" {
		t.Errorf("latestVersion should be empty on server error, got %q", got)
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

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
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

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
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

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
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
	srv, _ := NewServer(session, frontendFS, "", "", "", "", 0, "")
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
	comments := srv.session.Load().GetReviewComments()
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

func TestHandleConfig_AuthAndIntegrationFields(t *testing.T) {
	s, _ := newTestServer(t)
	s.authToken = "test-token"
	s.cfg = Config{
		AuthUserName:  "Test User",
		AuthUserEmail: "test@example.com",
	}
	s.projectDir = t.TempDir()
	s.homeDir = t.TempDir()
	s.reviewPath = "/tmp/test-review.json"

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

	// Auth fields
	if resp["auth_logged_in"] != true {
		t.Errorf("auth_logged_in = %v, want true", resp["auth_logged_in"])
	}
	if resp["auth_user_name"] != "Test User" {
		t.Errorf("auth_user_name = %v, want Test User", resp["auth_user_name"])
	}
	if resp["auth_user_email"] != "test@example.com" {
		t.Errorf("auth_user_email = %v, want test@example.com", resp["auth_user_email"])
	}

	// Review path
	if resp["review_path"] != "/tmp/test-review.json" {
		t.Errorf("review_path = %v, want /tmp/test-review.json", resp["review_path"])
	}

	// Integration fields
	avail, ok := resp["integrations_available"].([]any)
	if !ok || len(avail) == 0 {
		t.Error("integrations_available should be a non-empty array")
	}
	if _, ok := resp["integrations"]; !ok {
		t.Error("integrations field should be present")
	}
	if _, ok := resp["any_integration_installed"]; !ok {
		t.Error("any_integration_installed field should be present")
	}

	// Config pass-throughs
	if resp["no_integration_check"] != false {
		t.Errorf("no_integration_check = %v, want false", resp["no_integration_check"])
	}
	if resp["no_update_check"] != false {
		t.Errorf("no_update_check = %v, want false", resp["no_update_check"])
	}
}

func TestHandleConfig_AuthNotLoggedIn(t *testing.T) {
	s, _ := newTestServer(t)
	// authToken is empty by default

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["auth_logged_in"] != false {
		t.Errorf("auth_logged_in = %v, want false", resp["auth_logged_in"])
	}
	if resp["auth_user_name"] != "" {
		t.Errorf("auth_user_name = %v, want empty", resp["auth_user_name"])
	}
}

func TestHandleConfig_NoIntegrationCheck(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg = Config{NoIntegrationCheck: true}

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["no_integration_check"] != true {
		t.Errorf("no_integration_check = %v, want true", resp["no_integration_check"])
	}
	integrations, ok := resp["integrations"].([]any)
	if !ok {
		t.Fatal("integrations should be an array")
	}
	if len(integrations) != 0 {
		t.Errorf("integrations should be empty when check disabled, got %d", len(integrations))
	}
	if resp["any_integration_installed"] != false {
		t.Errorf("any_integration_installed should be false when check disabled")
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
	srv, _ := NewServer(session, frontendFS, "", "", "", "dev", 0, "")

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
	s, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
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
	s, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
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
	s, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("hello\n"), 0644)

	session := &Session{
		Mode:        "files",
		RepoRoot:    dir,
		ReviewRound: 1,

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

	s.SetSession(session)

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
	s, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
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

func TestSetPRInfo_AppearsInConfig(t *testing.T) {
	s, _ := newTestServer(t)

	// Config should have no PR fields before SetPRInfo.
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var before map[string]any
	json.Unmarshal(w.Body.Bytes(), &before)
	if _, ok := before["pr_url"]; ok {
		t.Fatal("pr_url should be absent before SetPRInfo")
	}

	// Set PR info asynchronously.
	s.SetPRInfo(&PRInfo{
		URL:         "https://github.com/test/repo/pull/42",
		Number:      42,
		Title:       "fix: something",
		IsDraft:     false,
		State:       "OPEN",
		BaseRefName: "main",
		HeadRefName: "fix-branch",
		AuthorLogin: "testuser",
	})

	// Config should now include PR fields.
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var after map[string]any
	json.Unmarshal(w.Body.Bytes(), &after)
	if after["pr_url"] != "https://github.com/test/repo/pull/42" {
		t.Errorf("pr_url = %v, want https://github.com/test/repo/pull/42", after["pr_url"])
	}
	if after["pr_number"].(float64) != 42 {
		t.Errorf("pr_number = %v, want 42", after["pr_number"])
	}
	if after["pr_title"] != "fix: something" {
		t.Errorf("pr_title = %v, want 'fix: something'", after["pr_title"])
	}
	if after["pr_author"] != "testuser" {
		t.Errorf("pr_author = %v, want 'testuser'", after["pr_author"])
	}
}

func TestSetPRInfo_ConcurrentSafe(t *testing.T) {
	s, _ := newTestServer(t)

	// Simulate the async pattern: SetSession makes server ready,
	// then SetPRInfo fires from a goroutine while config requests arrive.
	done := make(chan struct{})
	go func() {
		s.SetPRInfo(&PRInfo{
			URL:    "https://github.com/test/repo/pull/1",
			Number: 1,
			Title:  "concurrent PR",
		})
		close(done)
	}()

	// Hit config concurrently — should not panic or race.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/config", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("config: got status %d, want 200", w.Code)
		}
	}
	<-done
}

// TestAgentName is in server_agent_test.go (TestAgentName_Codex covers all cases).

func TestFileCommentResolveAPI(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	// Resolve
	body := `{"resolved": true}`
	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT resolve: status = %d, body = %s", w.Code, w.Body.String())
	}
	var resolved Comment
	json.Unmarshal(w.Body.Bytes(), &resolved)
	if !resolved.Resolved {
		t.Error("expected comment to be resolved")
	}

	// Unresolve
	body = `{"resolved": false}`
	req = httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT unresolve: status = %d", w.Code)
	}
	var unresolved Comment
	json.Unmarshal(w.Body.Bytes(), &unresolved)
	if unresolved.Resolved {
		t.Error("expected comment to be unresolved")
	}

	// Not found
	req = httptest.NewRequest("PUT", "/api/comment/nonexistent/resolve?path=test.md", strings.NewReader(`{"resolved": true}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("resolve nonexistent: status = %d, want 404", w.Code)
	}
}

func TestFileCommentReplyAPI(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	// POST reply
	body := strings.NewReader(`{"body": "done, fixed", "author": "agent"}`)
	req := httptest.NewRequest("POST", "/api/comment/"+c.ID+"/replies?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("POST reply: status = %d, body = %s", w.Code, w.Body.String())
	}
	var reply Reply
	json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.Body != "done, fixed" {
		t.Errorf("reply body = %q", reply.Body)
	}
	if reply.Author != "agent" {
		t.Errorf("reply author = %q", reply.Author)
	}

	// PUT reply
	body = strings.NewReader(`{"body": "updated reply"}`)
	req = httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/replies/"+reply.ID+"?path=test.md", body)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT reply: status = %d, body = %s", w.Code, w.Body.String())
	}
	var updated Reply
	json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Body != "updated reply" {
		t.Errorf("updated body = %q", updated.Body)
	}

	// DELETE reply
	req = httptest.NewRequest("DELETE", "/api/comment/"+c.ID+"/replies/"+reply.ID+"?path=test.md", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("DELETE reply: status = %d", w.Code)
	}

	// Verify reply is gone
	comments := session.GetComments("test.md")
	if len(comments[0].Replies) != 0 {
		t.Errorf("expected 0 replies after delete, got %d", len(comments[0].Replies))
	}
}

func TestFileCommentReplyNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "reply", "author": "agent"}`)
	req := httptest.NewRequest("POST", "/api/comment/nonexistent/replies?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("POST reply to nonexistent: status = %d, want 404", w.Code)
	}
}

func TestAPIUpdateComment_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	body := `{"body": ""}`
	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"?path=test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("PUT with empty body: status = %d, want 400", w.Code)
	}
}

func TestHandleAgentRequest_NotConfigured(t *testing.T) {
	srv, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	body := strings.NewReader(`{"comment_id": "c1"}`)
	req := httptest.NewRequest("POST", "/api/agent/request", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 when agent_cmd not configured, got %d", w.Code)
	}
}

func TestHandleAgentRequest_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/agent/request", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// --- handleCommits tests ---

func TestHandleCommits_GET(t *testing.T) {
	srv, session := newTestServer(t)

	// Session with Mode "files" and no VCS — returns null/nil.
	session.mu.Lock()
	session.Mode = "files"
	session.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/commits", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("GET /api/commits: status = %d, want 200", w.Code)
	}
	// Non-git session returns null (no commits).
	body := strings.TrimSpace(w.Body.String())
	if body != "null" {
		t.Errorf("expected null for non-git session, got %s", body)
	}
}

func TestHandleCommits_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/commits", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleCommits_GitMode(t *testing.T) {
	dir := initTestRepo(t)
	// Create a feature branch with a commit.
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "new.go"), "package main")
	runGit(t, dir, "add", "new.go")
	runGit(t, dir, "commit", "-m", "add new file")

	session := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		Branch:      "feature",
		BaseRef:     "main",
		VCS:         &GitVCS{},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files:       []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/commits", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var commits []CommitInfo
	if err := json.Unmarshal(w.Body.Bytes(), &commits); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
	if len(commits) == 0 {
		t.Error("expected at least one commit")
	}
}

// --- handleBranches tests ---

func TestHandleBranches_NoVCS(t *testing.T) {
	srv, session := newTestServer(t)
	// Ensure VCS is nil (files mode).
	session.mu.Lock()
	session.VCS = nil
	session.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/branches", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var branches []string
	if err := json.Unmarshal(w.Body.Bytes(), &branches); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
	if len(branches) != 0 {
		t.Errorf("expected empty branches for no VCS, got %v", branches)
	}
}

func TestHandleBranches_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/branches", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleBranches_WithGitVCS(t *testing.T) {
	dir := initTestRepo(t)

	session := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		VCS:         &GitVCS{},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files:       []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/branches", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// No remotes in test repo, so empty list is expected.
	var branches []string
	if err := json.Unmarshal(w.Body.Bytes(), &branches); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
}

// --- handleBaseBranch tests ---

func TestHandleBaseBranch_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/base-branch", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleBaseBranch_EmptyBranch(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"branch": ""}`)
	req := httptest.NewRequest("POST", "/api/base-branch", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty branch", w.Code)
	}
}

func TestHandleBaseBranch_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("POST", "/api/base-branch", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for invalid JSON", w.Code)
	}
}

// --- handleQR tests ---

func TestHandleQR_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	// Note: /api/qr is NOT guarded by withReady, so it works even without a session.
	noSessionSrv, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = srv // keep reference for potential use

	req := httptest.NewRequest("GET", "/api/qr?url=https://crit.md/r/test123", nil)
	w := httptest.NewRecorder()
	noSessionSrv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", contentType)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<svg") {
		t.Error("response should contain SVG markup")
	}
	if !strings.Contains(body, "<rect") {
		t.Error("response should contain rect elements for QR modules")
	}
}

func TestHandleQR_MissingURL(t *testing.T) {
	srv, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/qr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for missing url param", w.Code)
	}
}

func TestHandleQR_MethodNotAllowed(t *testing.T) {
	srv, err := NewServer(nil, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/qr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleEvents (SSE) tests ---

func TestHandleEvents_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/events", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleEvents_SSEHeaders(t *testing.T) {
	srv, session := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(w, req)
		close(done)
	}()

	// Send an event then cancel.
	time.Sleep(50 * time.Millisecond)
	session.notify(SSEEvent{Type: "comments-changed"})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Verify SSE headers.
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	conn := w.Header().Get("Connection")
	if conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}

	// Verify event data was written.
	body := w.Body.String()
	if !strings.Contains(body, "event: comments-changed") {
		t.Errorf("expected SSE event in body, got: %s", body)
	}
}

// --- buildPlanFeedback tests ---

func TestBuildPlanFeedback(t *testing.T) {
	session := &Session{
		Mode:        "plan",
		PlanDir:     "/tmp/plans/my-feature",
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	srv, err := NewServer(session, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	result := srv.buildPlanFeedback("/home/user/.crit/reviews/abc123.json")

	if !strings.Contains(result, "my-feature") {
		t.Errorf("expected slug 'my-feature' in feedback, got: %s", result)
	}
	if !strings.Contains(result, "/home/user/.crit/reviews/abc123.json") {
		t.Errorf("expected critJSON path in feedback, got: %s", result)
	}
	if !strings.Contains(result, "crit comment --plan") {
		t.Errorf("expected crit comment hint in feedback, got: %s", result)
	}
}

// --- handleFileCommentResolve tests ---

func TestHandleFileCommentResolve(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this bug", "", "")

	tests := []struct {
		name       string
		resolved   bool
		wantStatus int
	}{
		{"resolve", true, 200},
		{"unresolve", false, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"resolved": %v}`, tt.resolved)
			req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader(body))
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
			var result Comment
			json.Unmarshal(w.Body.Bytes(), &result)
			if result.Resolved != tt.resolved {
				t.Errorf("resolved = %v, want %v", result.Resolved, tt.resolved)
			}
		})
	}
}

func TestHandleFileCommentResolve_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"resolved": true}`)
	req := httptest.NewRequest("PUT", "/api/comment/nonexistent/resolve?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleFileCommentResolve_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/comment/c1/resolve?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleFileCommentResolve_InvalidBody(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "")

	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for invalid JSON", w.Code)
	}
}

// --- handleReviewCommentResolve tests ---

func TestHandleReviewCommentResolve(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("general note", "")

	tests := []struct {
		name     string
		resolved bool
	}{
		{"resolve", true},
		{"unresolve", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"resolved": %v}`, tt.resolved)
			req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", strings.NewReader(body))
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("[%s] status = %d, want 200, body = %s", tt.name, w.Code, w.Body.String())
			}
			var result Comment
			json.Unmarshal(w.Body.Bytes(), &result)
			if result.Resolved != tt.resolved {
				t.Errorf("resolved = %v, want %v", result.Resolved, tt.resolved)
			}
		})
	}
}

func TestHandleReviewCommentResolve_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"resolved": true}`)
	req := httptest.NewRequest("PUT", "/api/review-comment/nonexistent/resolve", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleReviewCommentResolve_InvalidBody(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")

	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleReviewCommentResolve_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/review-comment/c1/resolve", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleReviewCommentUpdate tests ---

func TestHandleReviewCommentUpdate_DELETE(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("delete me", "")

	req := httptest.NewRequest("DELETE", "/api/review-comment/"+c.ID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Errorf("DELETE: status = %d, want 204", w.Code)
	}

	// Verify deleted.
	comments := session.GetReviewComments()
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after delete, got %d", len(comments))
	}
}

func TestHandleReviewCommentUpdate_DELETE_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/review-comment/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleReviewCommentUpdate_PUT_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "updated"}`)
	req := httptest.NewRequest("PUT", "/api/review-comment/nonexistent", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleReviewCommentUpdate_PUT_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("original", "")

	body := strings.NewReader(`{"body": ""}`)
	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID, body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty body", w.Code)
	}
}

func TestHandleReviewCommentUpdate_PUT_InvalidJSON(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("original", "")

	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID, strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleReviewCommentUpdate_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/review-comment/c1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleReplyCRUD additional tests ---

func TestHandleReplyCRUD_PUT_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")
	reply, _ := session.AddReply("test.md", c.ID, "first reply", "agent")

	body := strings.NewReader(`{"body": ""}`)
	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/replies/"+reply.ID+"?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty reply body", w.Code)
	}
}

func TestHandleReplyCRUD_PUT_InvalidJSON(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")
	reply, _ := session.AddReply("test.md", c.ID, "first reply", "agent")

	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/replies/"+reply.ID+"?path=test.md", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for invalid JSON", w.Code)
	}
}

func TestHandleReplyCRUD_PUT_NotFound(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	body := strings.NewReader(`{"body": "updated"}`)
	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/replies/nonexistent?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404 for nonexistent reply", w.Code)
	}
}

func TestHandleReplyCRUD_DELETE_NotFound(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	req := httptest.NewRequest("DELETE", "/api/comment/"+c.ID+"/replies/nonexistent?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleReplyCRUD_POST_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	body := strings.NewReader(`{"body": "", "author": "agent"}`)
	req := httptest.NewRequest("POST", "/api/comment/"+c.ID+"/replies?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty reply body", w.Code)
	}
}

func TestHandleReplyCRUD_POST_InvalidJSON(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	req := httptest.NewRequest("POST", "/api/comment/"+c.ID+"/replies?path=test.md", strings.NewReader("bad json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for invalid JSON", w.Code)
	}
}

func TestHandleReplyCRUD_MethodNotAllowed(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	req := httptest.NewRequest("PATCH", "/api/comment/"+c.ID+"/replies?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- Review comment reply CRUD additional tests ---

func TestReviewCommentReplyCRUD_PUT_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")
	reply, _ := session.AddReviewCommentReply(c.ID, "reply text", "agent")

	body := strings.NewReader(`{"body": ""}`)
	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/replies/"+reply.ID, body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty reply body", w.Code)
	}
}

func TestReviewCommentReplyCRUD_PUT_NotFound(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")

	body := strings.NewReader(`{"body": "updated"}`)
	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/replies/nonexistent", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestReviewCommentReplyCRUD_DELETE_NotFound(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")

	req := httptest.NewRequest("DELETE", "/api/review-comment/"+c.ID+"/replies/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestReviewCommentReplyCRUD_POST_EmptyBody(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")

	body := strings.NewReader(`{"body": "", "author": "agent"}`)
	req := httptest.NewRequest("POST", "/api/review-comment/"+c.ID+"/replies", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestReviewCommentReplyCRUD_POST_InvalidJSON(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("note", "")

	req := httptest.NewRequest("POST", "/api/review-comment/"+c.ID+"/replies", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- handleFileCommentUpdate additional tests ---

func TestHandleFileCommentUpdate_DELETE(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "delete me", "", "")

	req := httptest.NewRequest("DELETE", "/api/comment/"+c.ID+"?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("DELETE: status = %d, want 200", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "deleted" {
		t.Errorf("expected status 'deleted', got %v", resp)
	}
}

func TestHandleFileCommentUpdate_DELETE_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/comment/nonexistent?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleFileCommentUpdate_PUT_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "updated"}`)
	req := httptest.NewRequest("PUT", "/api/comment/nonexistent?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleFileCommentUpdate_PUT_InvalidJSON(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "original", "", "")

	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"?path=test.md", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleFileCommentUpdate_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PATCH", "/api/comment/c1?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleHealth additional tests ---

func TestHandleHealth_WithBrowserClients(t *testing.T) {
	srv, session := newTestServer(t)
	session.BrowserConnect()
	defer session.BrowserDisconnect()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["browser_clients"] != true {
		t.Errorf("browser_clients = %v, want true", resp["browser_clients"])
	}
}

func TestHandleHealth_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleFinish additional tests ---

func TestHandleFinish_AllResolved(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "")
	session.SetCommentResolved("test.md", c.ID, true)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "resolved") {
		t.Errorf("expected 'resolved' in prompt, got: %s", prompt)
	}
	if resp["approved"] != true {
		t.Errorf("approved = %v, want true", resp["approved"])
	}
}

func TestHandleFinish_PlanMode(t *testing.T) {
	session := &Session{
		Mode:        "plan",
		PlanDir:     "/tmp/plans/test-plan",
		RepoRoot:    t.TempDir(),
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  "/tmp/plan.md",
				Status:   "added",
				FileType: "markdown",
				Content:  "# Plan",
				Comments: []Comment{{ID: "c1", Body: "needs work", StartLine: 1, EndLine: 1}},
			},
		},
	}
	srv, err := NewServer(session, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "crit comment --plan") {
		t.Errorf("plan mode finish should mention crit comment --plan, got: %s", prompt)
	}
}

func TestHandleFinish_WithStatus(t *testing.T) {
	var buf strings.Builder
	session := &Session{
		Mode:        "files",
		RepoRoot:    t.TempDir(),
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files: []*FileEntry{
			{
				Path:     "test.md",
				AbsPath:  "/tmp/test.md",
				Status:   "added",
				FileType: "markdown",
				Content:  "hello",
				Comments: []Comment{{ID: "c1", Body: "fix", StartLine: 1, EndLine: 1}},
			},
		},
	}
	srv, err := NewServer(session, frontendFS, "", "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// Set status on the Server, not the Session.
	srv.status = &Status{w: &buf, color: false}

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	output := buf.String()
	if output == "" {
		t.Error("expected status output")
	}
}

func TestHandleFinish_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/finish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleFileComments additional tests ---

func TestHandleFileComments_POST_FileScope(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "file-level note", "scope": "file"}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("status = %d, want 201, body = %s", w.Code, w.Body.String())
	}
	var c Comment
	json.Unmarshal(w.Body.Bytes(), &c)
	if c.Scope != "file" {
		t.Errorf("scope = %q, want file", c.Scope)
	}
}

func TestHandleFileComments_POST_EmptyBody(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "", "start_line": 1, "end_line": 1}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleFileComments_POST_InvalidLineRange(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "test", "start_line": 0, "end_line": 1}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for invalid line range", w.Code)
	}
}

func TestHandleFileComments_POST_EndBeforeStart(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": "test", "start_line": 5, "end_line": 2}`)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for end < start", w.Code)
	}
}

func TestHandleFileComments_POST_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/file/comments?path=test.md", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleFileComments_MissingPath(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/comments", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for missing path", w.Code)
	}
}

func TestHandleFileComments_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/file/comments?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleConfig additional tests ---

func TestHandleConfig_WithAuthToken(t *testing.T) {
	session := &Session{
		Mode:        "files",
		RepoRoot:    t.TempDir(),
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files:       []*FileEntry{},
	}
	srv, err := NewServer(session, frontendFS, "https://crit.md", "test-token", "tester", "v2.0.0", 3000, "claude -p")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["agent_cmd_enabled"] != true {
		t.Error("expected agent_cmd_enabled=true")
	}
	if resp["auth_logged_in"] != true {
		t.Error("expected auth_logged_in=true")
	}
}

// --- handleSession scope tests ---

func TestHandleSession_WithScope(t *testing.T) {
	dir := initTestRepo(t)
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "new.go"), "package main\n")
	runGit(t, dir, "add", "new.go")
	runGit(t, dir, "commit", "-m", "add new file")

	session := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		Branch:      "feature",
		BaseRef:     "main",
		VCS:         &GitVCS{},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files:       []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/session?scope=branch", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["mode"] != "git" {
		t.Errorf("mode = %v, want git", resp["mode"])
	}
}

// --- handleReviewComments additional tests ---

func TestHandleReviewComments_POST_EmptyBody(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"body": ""}`)
	req := httptest.NewRequest("POST", "/api/comments", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty body", w.Code)
	}
}

func TestHandleReviewComments_POST_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/comments", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleReviewComments_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/comments", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleCommentByID additional tests ---

func TestHandleCommentByID_EmptyID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/comment/", strings.NewReader(`{"body": "test"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty ID", w.Code)
	}
}

// --- handleReviewCommentByID additional tests ---

func TestHandleReviewCommentByID_EmptyID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/review-comment/", strings.NewReader(`{"body": "test"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty ID", w.Code)
	}
}

// --- handleFileDiff additional tests ---

func TestHandleFileDiff_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/file/diff?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleFile additional tests ---

func TestHandleFile_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/file?path=test.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleSession with commit parameter ---

func TestHandleSession_WithCommit(t *testing.T) {
	dir := initTestRepo(t)
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "new.go"), "package main\n")
	runGit(t, dir, "add", "new.go")
	runGit(t, dir, "commit", "-m", "add new file")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	session := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		Branch:      "feature",
		BaseRef:     "main",
		VCS:         &GitVCS{},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files:       []*FileEntry{},
	}

	srv, err := NewServer(session, frontendFS, "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/session?commit="+sha, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	files, ok := resp["files"].([]any)
	if !ok {
		t.Fatal("expected files array in response")
	}
	if len(files) == 0 {
		t.Error("expected files when scoped to specific commit")
	}
}

func TestHandleSession_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/session", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handleFiles additional tests ---

func TestHandleFiles_EmptyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/files/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400 for empty path", w.Code)
	}
}

func TestHandleFiles_PathTraversal_DotDot(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/files/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	// Should return 400 or 403.
	if w.Code == 200 {
		t.Error("path traversal should not return 200")
	}
}
