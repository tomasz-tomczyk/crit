package main

import (
	"crypto/sha256"
	"fmt"
	"io"
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
func connectToPreviewDaemon(key string) bool {
	entry, alive := findAliveSession(key)
	if !alive {
		return false
	}
	fmt.Fprintf(os.Stderr, "[crit] connected to preview daemon at http://localhost:%d\n", entry.Port)
	fmt.Fprintf(os.Stderr, "[crit] open http://localhost:%d/preview\n", entry.Port)
	if !daemonHasBrowser(entry) {
		go openBrowser(fmt.Sprintf("http://localhost:%d/preview", entry.Port))
	}
	runReviewClient(entry)
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
		CLIArgs:             []string{sc.previewFile},
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

// handlePreviewPage serves index.html for the /preview path.
func (s *Server) handlePreviewPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f, err := s.assets.Open("index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.Copy(w, f)
}

// handlePreviewContent serves the previewed HTML file and its sibling assets
// (CSS, JS, images) so the iframe can load them. Paths under /preview-content/
// are resolved relative to the previewed file's directory.
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
	if reqPath == "" || reqPath == "/" {
		// Serve the main preview file
		http.ServeFile(w, r, sess.Origin)
		return
	}

	// Serve sibling assets relative to the preview file's directory
	baseDir := filepath.Dir(sess.Origin)
	resolved := filepath.Join(baseDir, filepath.Clean(reqPath))

	// Path traversal check
	if !strings.HasPrefix(resolved, baseDir) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, resolved)
}

// runPreview is the entry point for `crit preview <file.html>`.
func runPreview(args []string) {
	rawPath := ""
	for _, a := range args {
		if len(a) > 0 && a[0] != '-' {
			rawPath = a
			break
		}
	}
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

	cwd, err := resolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	key := previewSessionKey(cwd, absPath)
	if connectToPreviewDaemon(key) {
		return
	}

	daemonArgs := []string{"--preview-file", absPath}
	entry, err := startDaemon(key, daemonArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start preview daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[crit] preview mode: %s\n", filepath.Base(absPath))
	fmt.Fprintf(os.Stderr, "[crit] open http://localhost:%d/preview\n", entry.Port)

	installDaemonSignalHandler(entry.PID)

	go openBrowser(fmt.Sprintf("http://localhost:%d/preview", entry.Port))

	runReviewClient(entry)
}
