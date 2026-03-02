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
				nextID:   1,
			},
		},
	}

	s, err := NewServer(session, frontendFS, "", "test", 0)
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
	session.AddComment("test.md", 1, 1, "", "one")
	session.AddComment("test.md", 2, 2, "", "two")

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
	c, _ := session.AddComment("test.md", 1, 1, "", "original")

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
	c, _ := session.AddComment("test.md", 1, 1, "", "to delete")

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

func TestFinish(t *testing.T) {
	s, session := newTestServer(t)
	session.AddComment("test.md", 1, 1, "", "note")

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
	s, session := newTestServer(t)

	body := `{"url":"https://crit.live/r/abc123"}`
	req := httptest.NewRequest("POST", "/api/share-url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if session.GetSharedURL() != "https://crit.live/r/abc123" {
		t.Errorf("shared URL = %q, want https://crit.live/r/abc123", session.GetSharedURL())
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
	s, session := newTestServer(t)
	session.SetSharedURLAndToken("", "mydeletetoken1234567890")

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
	s, session := newTestServer(t)

	body := `{"url":"https://crit.live/r/abc","delete_token":"deletetoken1234567890x"}`
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
	session.SetSharedURLAndToken("https://crit.live/r/abc", "sometoken1234567890123")

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
		nextID:   1,
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
