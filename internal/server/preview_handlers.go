package server

import (
	"bytes"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handlePreviewContent serves the previewed HTML file and its sibling assets.
func (s *Server) handlePreviewContent(w http.ResponseWriter, r *http.Request) {
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
		if isPreviewExcalidrawPath(sess.Origin) {
			s.servePreviewExcalidraw(w, sess.Origin)
			return
		}
		s.servePreviewHTML(w, sess.Origin)
		return
	}

	resolved := filepath.Join(baseDir, filepath.Clean(reqPath))
	if !strings.HasPrefix(resolved, baseDir+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
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

func isPreviewHTMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm"
}

// isPreviewExcalidrawPath reports whether the previewed file is an Excalidraw
// scene (same rule as preview.isExcalidrawPath).
func isPreviewExcalidrawPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".excalidraw") || strings.HasSuffix(lower, ".excalidraw.json")
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
func (s *Server) servePreviewExcalidraw(w http.ResponseWriter, filePath string) {
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
	// the file cannot close the <script> element it sits in.
	sceneJSON, err := json.Marshal(string(scene))
	if err != nil {
		http.Error(w, "failed to encode scene", http.StatusInternalServerError)
		return
	}
	body := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(filepath.Base(filePath)),
		"{{SCENE}}", string(sceneJSON),
	).Replace(string(page))
	writePreviewPage(w, []byte(body))
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
