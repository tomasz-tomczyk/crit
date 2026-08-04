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
