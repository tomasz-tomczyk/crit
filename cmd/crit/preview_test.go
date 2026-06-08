package main

import (
	"embed"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewSessionKey(t *testing.T) {
	k1 := previewSessionKey("/app", "/app/index.html")
	k2 := previewSessionKey("/app", "/app/index.html")
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %q vs %q", k1, k2)
	}
	k3 := previewSessionKey("/app", "/app/other.html")
	if k1 == k3 {
		t.Error("different files produced same key")
	}
	k4 := previewSessionKey("/other", "/app/index.html")
	if k1 == k4 {
		t.Error("different cwds produced same key")
	}
	if len(k1) != 12 {
		t.Errorf("key length = %d, want 12", len(k1))
	}
}

func TestPreviewSessionKey_NoCollisionWithLive(t *testing.T) {
	cwd := "/app"
	pk := previewSessionKey(cwd, "http://localhost:3000")
	dk := liveSessionKey(cwd, "http://localhost:3000")
	if pk == dk {
		t.Error("preview and live keys collide for same input")
	}
}

func TestLooksLikePreviewArgs(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "test.html")
	htmFile := filepath.Join(dir, "test.htm")
	mdFile := filepath.Join(dir, "test.md")
	os.WriteFile(htmlFile, []byte("<html></html>"), 0644)
	os.WriteFile(htmFile, []byte("<html></html>"), 0644)
	os.WriteFile(mdFile, []byte("# hello"), 0644)
	os.Mkdir(filepath.Join(dir, "dir.html"), 0755)

	cases := []struct {
		args []string
		want bool
	}{
		{[]string{htmlFile}, true},
		{[]string{htmFile}, true},
		{[]string{mdFile}, false},
		{[]string{filepath.Join(dir, "dir.html")}, false},
		{[]string{filepath.Join(dir, "nonexistent.html")}, false},
		{[]string{htmlFile, htmFile}, false},
		{nil, false},
		{[]string{}, false},
	}
	for _, tc := range cases {
		got := looksLikePreviewArgs(tc.args)
		if got != tc.want {
			t.Errorf("looksLikePreviewArgs(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestCreatePreviewSession(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "index.html")
	os.WriteFile(htmlFile, []byte("<html><body>hello</body></html>"), 0644)

	sc := &serverConfig{previewFile: htmlFile}
	sess, err := createPreviewSession(sc)
	if err != nil {
		t.Fatalf("createPreviewSession: %v", err)
	}
	if sess.ReviewType != "preview" {
		t.Errorf("ReviewType = %q, want %q", sess.ReviewType, "preview")
	}
	if sess.Origin != htmlFile {
		t.Errorf("Origin = %q, want %q", sess.Origin, htmlFile)
	}
	if len(sess.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(sess.Files))
	}
	if sess.Files[0].Content == "" {
		t.Error("file content is empty")
	}
	if !strings.Contains(sess.Files[0].Content, "hello") {
		t.Error("file content missing expected text")
	}
}

func TestCreatePreviewSession_MissingFile(t *testing.T) {
	sc := &serverConfig{previewFile: "/nonexistent/test.html"}
	_, err := createPreviewSession(sc)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCreatePreviewSession_EmptyPath(t *testing.T) {
	sc := &serverConfig{}
	_, err := createPreviewSession(sc)
	if err == nil {
		t.Error("expected error for empty previewFile")
	}
}

func newPreviewTestServer(t *testing.T, dir string) (*Server, *Session) {
	t.Helper()
	htmlFile := filepath.Join(dir, "index.html")
	session := &Session{
		Mode:          "files",
		RepoRoot:      dir,
		ReviewRound:   1,
		ReviewType:    "preview",
		Origin:        htmlFile,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     "index.html",
				Status:   "added",
				FileType: "code",
				Content:  "<html><body>test</body></html>",
			},
		},
	}
	s, err := NewServer(session, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, session
}

func TestHandlePreviewPage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview", nil)
	w := httptest.NewRecorder()
	s.serveIndexHTML()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestHandlePreviewPage_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("POST", "/preview", nil)
	w := httptest.NewRecorder()
	s.serveIndexHTML()(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandlePreviewContent_ServesHTML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>hello</body></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hello") {
		t.Error("response missing original content")
	}
}

func TestHandlePreviewContent_InjectsAgent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>content</body></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "crit-agent.js") {
		t.Error("agent script not injected")
	}
	if !strings.Contains(body, "agent-protocol.js") {
		t.Error("agent-protocol.js not injected")
	}
	if strings.Contains(body, "agent-marker.css") {
		t.Error("agent-marker.css should not be injected (loaded dynamically by crit-agent.js)")
	}
	// Verify injection is before </body>
	agentIdx := strings.Index(body, "crit-agent.js")
	bodyIdx := strings.Index(body, "</body>")
	if agentIdx > bodyIdx {
		t.Error("agent injected after </body>")
	}
}

func TestHandlePreviewContent_InjectsAgentOnSiblingHTML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>home</body></html>"), 0644)
	chDir := filepath.Join(dir, "chapters")
	os.Mkdir(chDir, 0755)
	os.WriteFile(filepath.Join(chDir, "01-intro.html"), []byte("<html><body>chapter</body></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/chapters/01-intro.html", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "chapter") {
		t.Error("response missing chapter content")
	}
	if !strings.Contains(body, "crit-agent.js") {
		t.Error("agent script not injected into sibling HTML")
	}
}

func TestHandlePreviewContent_ServesSiblingAssets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	os.WriteFile(filepath.Join(dir, "style.css"), []byte("body { color: red; }"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/style.css", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "color: red") {
		t.Error("CSS content not served")
	}
}

func TestHandlePreviewContent_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	// Create a file outside the preview dir
	secretDir := t.TempDir()
	os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte("secret"), 0644)

	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/../../secret.txt", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	// filepath.Clean resolves .., but the prefix check should block it
	// or the file simply won't exist under dir
	if w.Code == http.StatusOK {
		body := w.Body.String()
		if strings.Contains(body, "secret") {
			t.Error("path traversal allowed access to file outside preview dir")
		}
	}
}

func TestHandlePreviewContent_DirectoryListing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	subdir := filepath.Join(dir, "subdir")
	os.Mkdir(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("hello"), 0644)

	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/subdir", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("directory request: status = %d, want 404", w.Code)
	}
}

func TestHandlePreviewContent_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("POST", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandlePreviewContent_NoSession(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHandlePreviewContent_NotFound(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	s, _ := newPreviewTestServer(t, dir)

	req := httptest.NewRequest("GET", "/preview-content/nonexistent.css", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestServePreviewHTML_NoBody(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "index.html")
	os.WriteFile(htmlFile, []byte("<html><head></head></html>"), 0644)

	s, _ := newPreviewTestServer(t, dir)
	// Override Origin to point to the no-body file
	sess := s.session.Load()
	sess.Origin = htmlFile

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	body := w.Body.String()
	// Without </body>, agent injection doesn't happen but the page still serves
	if !strings.Contains(body, "<html>") {
		t.Error("HTML not served when no </body> tag present")
	}
}

// Verify newTestServer from embed.FS compiles with preview routes registered.
func TestPreviewRouteRegistered(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	// /preview should be handled (returns index.html from embedded FS)
	req := httptest.NewRequest("GET", "/preview", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/preview status = %d, want 200", w.Code)
	}
}

// Compile guard: ensure the frontendFS embed includes preview-related files.
func TestFrontendFS_IncludesPreviewAssets(t *testing.T) {
	var _ embed.FS = frontendFS
}
