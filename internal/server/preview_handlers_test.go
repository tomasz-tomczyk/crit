package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPreviewContentTestServer(t *testing.T, htmlFile string) *Server {
	t.Helper()
	sess := &Session{
		Mode:        "files",
		RepoRoot:    filepath.Dir(htmlFile),
		ReviewRound: 1,
		ReviewType:  "preview",
		Origin:      htmlFile,
		Files: []*FileEntry{
			{
				Path:     filepath.Base(htmlFile),
				Status:   "added",
				FileType: "code",
				Content:  "<html><body>test</body></html>",
			},
		},
	}
	sess.InitTestChannels()
	s, err := NewServer(sess, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServePreviewHTML_AppendsAgentWhenNoBodyTag(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlFile, []byte("<div>fragment with no body tag</div>"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, htmlFile)

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "fragment with no body tag") {
		t.Error("response missing original content")
	}
	if !strings.Contains(body, "crit-agent.js") {
		t.Error("agent script not appended when </body> is absent")
	}
	fragIdx := strings.Index(body, "fragment with no body tag")
	agentIdx := strings.Index(body, "crit-agent.js")
	if fragIdx < 0 || agentIdx < 0 || agentIdx < fragIdx {
		t.Error("expected agent scripts appended after fragment content")
	}
}

func TestServePreviewExcalidraw_ServesCanvasPageWithSceneAndAgent(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "arch & flow.excalidraw")
	// A text element whose words would close the script element if inlined raw.
	content := `{"type":"excalidraw","elements":[{"id":"t","type":"text","text":"</script><script>alert(1)</script>"}]}`
	if err := os.WriteFile(scene, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, scene)

	req := httptest.NewRequest("GET", "/preview-content/", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<title>arch &amp; flow.excalidraw</title>") {
		t.Error("title is not the escaped file name")
	}
	if strings.Contains(body, "{{SCENE}}") || strings.Contains(body, "{{TITLE}}") {
		t.Error("template placeholders left in the page")
	}
	if strings.Contains(body, "</script><script>alert(1)") {
		t.Error("scene text was inlined unescaped")
	}
	if !strings.Contains(body, `\u003c/script\u003e\u003cscript\u003ealert(1)`) {
		t.Error("scene not inlined as an escaped JSON string")
	}
	for _, script := range []string{"/crit-renderer-excalidraw.js", "/crit-excalidraw-canvas.js", "/rough.min.js", "crit-agent.js"} {
		if !strings.Contains(body, script) {
			t.Errorf("page does not load %s", script)
		}
	}
	if strings.Index(body, "crit-excalidraw-canvas.js") > strings.Index(body, "crit-agent.js") {
		t.Error("the canvas must be drawn before the agent scripts run")
	}
}

func TestServePreviewExcalidraw_SiblingFilesServedAsIs(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "arch.excalidraw")
	if err := os.WriteFile(scene, []byte(`{"elements":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, scene)

	req := httptest.NewRequest("GET", "/preview-content/arch.excalidraw", nil)
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)

	if w.Code != http.StatusOK || w.Body.String() != `{"elements":[]}` {
		t.Fatalf("got %d %q, want the raw scene", w.Code, w.Body.String())
	}
}
