package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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

func TestServePreviewExcalidraw_SceneGoneIsAServerError(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "arch.excalidraw")
	if err := os.WriteFile(scene, []byte(`{"elements":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, scene)
	if err := os.Remove(scene); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.handlePreviewContent(w, httptest.NewRequest("GET", "/preview-content/", nil))

	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "failed to read preview file") {
		t.Fatalf("got %d %q, want 500 failed to read preview file", w.Code, w.Body.String())
	}
}

func TestServePreviewExcalidraw_MissingCanvasPageIsAServerError(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "arch.excalidraw")
	if err := os.WriteFile(scene, []byte(`{"elements":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, scene)
	s.assets = fstest.MapFS{}

	w := httptest.NewRecorder()
	s.handlePreviewContent(w, httptest.NewRequest("GET", "/preview-content/", nil))

	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "missing excalidraw-canvas.html") {
		t.Fatalf("got %d %q, want 500 missing excalidraw-canvas.html", w.Code, w.Body.String())
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

// Every /preview-content response is sandboxed (opaque origin), so the
// previewed file's scripts cannot act as the daemon's origin: not the page,
// not a sibling HTML page, not a sibling SVG opened directly.
func TestPreviewContent_SandboxedCSPOnEveryResponse(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "index.html")
	files := map[string]string{
		"index.html": "<html><body>main</body></html>",
		"other.html": "<html><body>other</body></html>",
		"logo.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := newPreviewContentTestServer(t, htmlFile)

	for _, path := range []string{"/preview-content", "/preview-content/", "/preview-content/other.html", "/preview-content/logo.svg", "/preview-content/missing.png"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "127.0.0.1:4567"
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			policy := w.Header().Get("Content-Security-Policy")
			sandbox := cspDirective(policy, "sandbox")
			if !strings.HasPrefix(sandbox, "sandbox ") || !strings.Contains(sandbox, "allow-scripts") {
				t.Fatalf("CSP = %q, want a sandbox directive that allows scripts", policy)
			}
			if strings.Contains(sandbox, "allow-same-origin") {
				t.Fatalf("sandbox = %q must not allow-same-origin", sandbox)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q", got)
			}
		})
	}
}

func TestPreviewContent_RefusesHiddenFilesAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "site")
	outside := filepath.Join(root, "secret.txt")
	mustWrite := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	htmlFile := filepath.Join(dir, "index.html")
	mustWrite(htmlFile, "<html><body>main</body></html>")
	mustWrite(filepath.Join(dir, "assets", "app.css"), "body{}")
	mustWrite(filepath.Join(dir, ".env"), "TOKEN=x")
	mustWrite(filepath.Join(dir, ".git", "config"), "[core]")
	mustWrite(outside, "secret")
	symlinks := os.Symlink(outside, filepath.Join(dir, "assets", "leak.txt")) == nil &&
		os.Symlink(root, filepath.Join(dir, "up")) == nil &&
		os.Symlink(filepath.Join(dir, "assets", "app.css"), filepath.Join(dir, "inside.css")) == nil
	s := newPreviewContentTestServer(t, htmlFile)

	cases := []struct {
		path   string
		want   int
		needLn bool
	}{
		{"/preview-content/assets/app.css", http.StatusOK, false},
		{"/preview-content/.env", http.StatusNotFound, false},
		{"/preview-content/.git/config", http.StatusNotFound, false},
		{"/preview-content/assets/../.env", http.StatusNotFound, false},
		{"/preview-content/../secret.txt", http.StatusNotFound, false},
		{"/preview-content/assets/leak.txt", http.StatusNotFound, true},
		{"/preview-content/up/secret.txt", http.StatusNotFound, true},
		{"/preview-content/inside.css", http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if tc.needLn && !symlinks {
				t.Skip("symlinks unavailable")
			}
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			s.handlePreviewContent(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "TOKEN") {
				t.Fatalf("leaked a file outside the preview folder: %q", w.Body.String())
			}
		})
	}
}

func TestEmbeddedFontsAllowOpaqueOriginFontLoads(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fonts/excalidraw/LICENSES.txt", nil)
	req.Host = "127.0.0.1:4567"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want * for the sandboxed canvas's FontFace loads", got)
	}
}

func TestReviewThemeFromCookie(t *testing.T) {
	cases := []struct {
		name, cookie, want string
	}{
		{"no cookie", "", ""},
		{"dark", url.QueryEscape(`{"theme":"dark","width":900}`), "dark"},
		{"light", url.QueryEscape(`{"theme":"light"}`), "light"},
		{"system", url.QueryEscape(`{"theme":"system"}`), ""},
		{"unknown value", url.QueryEscape(`{"theme":"\"><script>"}`), ""},
		{"malformed", "%zz", ""},
		{"not json", "dark", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/preview-content/", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "crit-settings", Value: tc.cookie})
			}
			if got := reviewThemeFromCookie(req); got != tc.want {
				t.Errorf("reviewThemeFromCookie = %q, want %q", got, tc.want)
			}
		})
	}
}

// The sandboxed canvas cannot read the cookie, so the page carries the theme.
func TestServePreviewExcalidraw_CarriesReviewTheme(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "arch.excalidraw")
	if err := os.WriteFile(scene, []byte(`{"elements":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPreviewContentTestServer(t, scene)
	req := httptest.NewRequest(http.MethodGet, "/preview-content/", nil)
	req.AddCookie(&http.Cookie{Name: "crit-settings", Value: url.QueryEscape(`{"theme":"dark"}`)})
	w := httptest.NewRecorder()
	s.handlePreviewContent(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `data-crit-review-theme="dark"`) {
		t.Fatalf("canvas page lacks the review theme: %.200q", body)
	}
	if strings.Contains(body, "{{THEME}}") {
		t.Fatal("unfilled {{THEME}} placeholder")
	}
}
