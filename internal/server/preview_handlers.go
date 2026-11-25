package server

import (
	"bytes"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// previewContentCSP sandboxes everything /preview-content serves. The
// previewed file is untrusted (an agent or a cloned repo may have written it),
// and the daemon serves it on the same host:port as /api. Without the sandbox
// its scripts would run same-origin with the API and the review page: they
// could read repo files, grant project trust, press Finish and so run project
// hooks, or reach into the parent page. `sandbox` without allow-same-origin
// gives the document an opaque origin, so the browser rejects its API writes
// (Sec-Fetch-Site is no longer same-origin), CORS hides API reads, and the
// parent page is cross-origin. A header (not only the iframe attribute) also
// covers the URL opened directly in a tab, and sibling files such as an SVG
// with a script in it. The agent scripts keep working: they talk to the parent
// with postMessage only.
const previewContentCSP = "sandbox allow-scripts allow-forms allow-modals allow-popups"

// handlePreviewContent serves the previewed HTML file and its sibling assets.
func (s *Server) handlePreviewContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", previewContentCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.session.Load()
	if sess == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	if sess.Origin == "" {
		http.Error(w, "no preview file configured", http.StatusNotFound)
		return
	}

	reqPath := strings.TrimPrefix(r.URL.Path, "/preview-content")
	baseDir := filepath.Dir(sess.Origin)

	if reqPath == "" || reqPath == "/" {
		if session.IsExcalidrawScene(sess.Origin) {
			s.servePreviewExcalidraw(w, r, sess.Origin)
			return
		}
		s.servePreviewHTML(w, sess.Origin)
		return
	}

	resolved, ok := resolvePreviewAsset(baseDir, reqPath)
	if !ok {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if isPreviewHTMLPath(resolved) {
		s.servePreviewHTML(w, resolved)
		return
	}

	http.ServeFile(w, r, resolved)
}

// resolvePreviewAsset maps a /preview-content sub-path to a file inside
// baseDir (the previewed file's folder). It refuses hidden files and folders
// (.env, .git/) and resolves symlinks before the containment check, so a link
// in the folder cannot serve a file from elsewhere (~/.ssh, say).
func resolvePreviewAsset(baseDir, reqPath string) (string, bool) {
	clean := filepath.Clean("/" + reqPath)
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(realBase, clean))
	if err != nil {
		return "", false
	}
	if !strings.HasPrefix(realPath, realBase+string(filepath.Separator)) {
		return "", false
	}
	// Check again after links resolve: a link such as cfg -> .git, or a
	// Windows 8.3 short name (GIT~1), reaches a hidden file under a plain name.
	rel, err := filepath.Rel(realBase, realPath)
	if err != nil {
		return "", false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return realPath, true
}

func isPreviewHTMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm"
}

func (s *Server) servePreviewHTML(w http.ResponseWriter, filePath string) {
	body, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "failed to read preview file", http.StatusInternalServerError)
		return
	}
	writePreviewPage(w, body)
}

// servePreviewExcalidraw serves an Excalidraw scene as a canvas page: the
// embedded excalidraw-canvas.html with the scene inlined, which
// crit-renderer-excalidraw.js draws element by element, so the agent's hover
// box and pins work on the drawing like on any previewed page.
func (s *Server) servePreviewExcalidraw(w http.ResponseWriter, r *http.Request, filePath string) {
	scene, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "failed to read preview file", http.StatusInternalServerError)
		return
	}
	page, err := fs.ReadFile(s.assets, "excalidraw-canvas.html")
	if err != nil {
		http.Error(w, "missing excalidraw-canvas.html", http.StatusInternalServerError)
		return
	}
	// The scene goes in as a JSON string. json.Marshal escapes <, > and &, so
	// the file cannot close the <script> element it sits in. Marshalling a
	// string never fails (invalid UTF-8 becomes U+FFFD).
	sceneJSON, _ := json.Marshal(string(scene))
	body := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(filepath.Base(filePath)),
		"{{THEME}}", reviewThemeFromCookie(r),
		"{{SCENE}}", string(sceneJSON),
	).Replace(string(page))
	writePreviewPage(w, []byte(body))
}

// reviewThemeFromCookie returns "light" or "dark" when the reviewer picked
// one in the crit-settings cookie, else "" (follow the system). The sandboxed
// canvas cannot read the cookie itself, but the browser still sends it.
func reviewThemeFromCookie(r *http.Request) string {
	c, err := r.Cookie("crit-settings")
	if err != nil {
		return ""
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	var settings struct {
		Theme string `json:"theme"`
	}
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return ""
	}
	if settings.Theme == "light" || settings.Theme == "dark" {
		return settings.Theme
	}
	return ""
}

// writePreviewPage writes a preview page with the agent scripts injected.
func writePreviewPage(w http.ResponseWriter, body []byte) {
	var sb strings.Builder
	for _, f := range AgentScriptFiles {
		sb.WriteString(`<script src="/`)
		sb.WriteString(f)
		sb.WriteString(`"></script>`)
	}
	agentScripts := sb.String()

	// Insert before the last </body>, or append when the file is a fragment
	// with no closing body tag (matches crit-web raw_controller).
	idx := bytes.LastIndex(bytes.ToLower(body), []byte("</body>"))
	if idx >= 0 {
		var out []byte
		out = append(out, body[:idx]...)
		out = append(out, []byte(agentScripts)...)
		out = append(out, body[idx:]...)
		body = out
	} else {
		body = append(body, []byte(agentScripts)...)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}
