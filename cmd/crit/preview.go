package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// previewSessionKey returns the session/review key for a preview-mode session.
// Formula: sha256(cwd + "\0preview\0" + absPath)[:12].
func previewSessionKey(cwd, absPath string) string {
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte("\x00preview\x00"))
	h.Write([]byte(absPath))
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// looksLikePreviewArgs returns true when args is exactly one element
// that refers to an existing .html file on disk.
func looksLikePreviewArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	ext := filepath.Ext(args[0])
	if ext != ".html" && ext != ".htm" {
		return false
	}
	info, err := os.Stat(args[0])
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// connectToPreviewDaemon attaches the current CLI to an already-running preview
// daemon for key, blocking on its review session.
func connectToPreviewDaemon(key string, noOpen bool) bool {
	entry, alive := findAliveSession(key)
	if !alive {
		return false
	}
	fmt.Fprintf(os.Stderr, "[crit] connected to preview daemon at %s\n", entry.baseURL())
	fmt.Fprintf(os.Stderr, "[crit] open %s/preview\n", entry.baseURL())
	if !noOpen && !daemonHasBrowser(entry) {
		go openBrowser(entry.baseURL() + "/preview")
	}
	runReviewClient(entry, key)
	return true
}

// createPreviewSession builds a session for preview mode. The previewed file
// becomes a single FileEntry so the existing comment infrastructure works.
func createPreviewSession(sc *serverConfig) (*Session, error) {
	if sc.previewFile == "" {
		return nil, fmt.Errorf("createPreviewSession: previewFile is empty")
	}
	cwd, _ := resolvedCWD()
	relPath, err := filepath.Rel(cwd, sc.previewFile)
	if err != nil {
		relPath = sc.previewFile
	}
	content, err := os.ReadFile(sc.previewFile)
	if err != nil {
		return nil, fmt.Errorf("reading preview file: %w", err)
	}
	s := &Session{
		Mode:                "files",
		RepoRoot:            cwd,
		ReviewRound:         1,
		ReviewType:          "preview",
		Origin:              sc.previewFile,
		CLIArgs:             []string{"preview", sc.previewFile},
		awaitingFirstReview: true,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     relPath,
				Status:   "added",
				FileType: "code",
				Content:  string(content),
			},
		},
	}
	if sc.reviewPath != "" {
		s.ReviewFilePath = sc.reviewPath
		s.loadCritJSON()
	}
	return s, nil
}

// handlePreviewContent serves the previewed HTML file and its sibling assets
// (CSS, JS, images) so the iframe can load them. Paths under /preview-content/
// are resolved relative to the previewed file's directory.
// The main HTML file gets crit-agent.js injected before </body> so pin
// commenting works (same approach as the live-mode proxy).
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
		// Serve the main HTML with agent injection
		s.servePreviewHTML(w, sess.Origin)
		return
	}

	// Serve sibling assets relative to the preview file's directory
	resolved := filepath.Join(baseDir, filepath.Clean(reqPath))

	// Path traversal check (trailing separator prevents prefix collisions)
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

	// Multi-page previews (e.g. static sites with chapter/*.html) must get
	// the same crit-agent injection as the root page — otherwise in-iframe
	// navigation leaves Pin mode enabled in the chrome but the new document
	// has no agent listening for clicks.
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

// servePreviewHTML reads the HTML file and injects agent scripts before </body>.
// Same-origin injection so no absolute URLs needed — just relative paths to
// the embedded agent JS served at the root.
func (s *Server) servePreviewHTML(w http.ResponseWriter, filePath string) {
	body, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "failed to read preview file", http.StatusInternalServerError)
		return
	}

	var sb strings.Builder
	for _, f := range agentScriptFiles {
		sb.WriteString(`<script src="/`)
		sb.WriteString(f)
		sb.WriteString(`"></script>`)
	}
	agentScripts := sb.String()

	// Inject before last </body>
	idx := bytes.LastIndex(bytes.ToLower(body), []byte("</body>"))
	if idx >= 0 {
		var out []byte
		out = append(out, body[:idx]...)
		out = append(out, []byte(agentScripts)...)
		out = append(out, body[idx:]...)
		body = out
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

// runPreview is the entry point for `crit preview <file.html>`.
func runPreview(args []string) {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	host := fs.String("host", "", "Host to listen on")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status (shorthand)")
	shareURL := fs.String("share-url", "", "Share service URL")
	fs.Parse(args)

	remaining := fs.Args()
	rawPath := ""
	for _, a := range remaining {
		if len(a) > 0 && a[0] != '-' {
			rawPath = a
			break
		}
	}

	// Also respect config file setting.
	cwd, err := resolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg := LoadConfig(cwd)
	noOpenResolved := *noOpen || cfg.NoOpen

	if rawPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit preview <file.html>")
		os.Exit(1)
	}

	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit preview: cannot resolve path %q: %v\n", rawPath, err)
		os.Exit(1)
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		fmt.Fprintf(os.Stderr, "crit preview: %q is not a file\n", rawPath)
		os.Exit(1)
	}

	key := previewSessionKey(cwd, absPath)
	if connectToPreviewDaemon(key, noOpenResolved) {
		return
	}

	daemonArgs := []string{"--preview-file", absPath}
	daemonArgs = appendCommonDaemonFlags(daemonArgs, commonDaemonFlags{
		port:   resolvePort(*port, cfg.Port),
		host:   resolveHost(*host, cfg.Host),
		noOpen: noOpenResolved,
		quiet:  *quiet || cfg.Quiet,
		// Preview is shareable to crit-web (CRI-78), so default the share URL
		// the same way code review does — otherwise the Share button never
		// appears in preview mode. Design/live mode keeps the empty default and
		// is gated off client-side; it is intentionally not shareable.
		shareURL: resolveShareURL(*shareURL, cfg, defaultShareURL),
	})
	entry, err := startDaemon(key, daemonArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start preview daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[crit] preview mode: %s\n", filepath.Base(absPath))
	fmt.Fprintf(os.Stderr, "[crit] open %s/preview\n", entry.baseURL())

	installDaemonSignalHandler(entry.PID)

	if !noOpenResolved {
		go openBrowser(entry.baseURL() + "/preview")
	}

	runReviewClient(entry, key)
}
