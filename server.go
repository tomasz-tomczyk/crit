package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	session        *Session
	mux            *http.ServeMux
	assets         fs.FS
	shareURL       string
	currentVersion string
	latestVersion  string
	versionMu      sync.RWMutex
	port           int
	status         *Status
}

func NewServer(session *Session, frontendFS embed.FS, shareURL string, currentVersion string, port int) (*Server, error) {
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return nil, fmt.Errorf("loading frontend assets: %w", err)
	}

	s := &Server{session: session, assets: assets, shareURL: shareURL, currentVersion: currentVersion, port: port}

	mux := http.NewServeMux()

	// Session-scoped endpoints
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/share-url", s.handleShareURL)
	mux.HandleFunc("/api/finish", s.handleFinish)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/round-complete", s.handleRoundComplete)

	// File-scoped endpoints (use ?path= query param)
	mux.HandleFunc("/api/file", s.handleFile)
	mux.HandleFunc("/api/file/diff", s.handleFileDiff)
	mux.HandleFunc("/api/file/comments", s.handleFileComments)
	mux.HandleFunc("/api/comment/", s.handleCommentByID)

	// Static file serving
	mux.HandleFunc("/files/", s.handleFiles)
	mux.Handle("/", http.FileServer(http.FS(assets)))

	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.versionMu.RLock()
	latestVersion := s.latestVersion
	s.versionMu.RUnlock()
	writeJSON(w, map[string]string{
		"share_url":      s.shareURL,
		"hosted_url":     s.session.GetSharedURL(),
		"delete_token":   s.session.GetDeleteToken(),
		"version":        s.currentVersion,
		"latest_version": latestVersion,
	})
}

func (s *Server) checkForUpdates() {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/tomasz-tomczyk/crit/releases/latest", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "crit/"+s.currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}
	s.versionMu.Lock()
	s.latestVersion = release.TagName
	s.versionMu.Unlock()
}

// handleSession returns session metadata: mode, branch, file list with stats.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.session.GetSessionInfo())
}

func (s *Server) handleShareURL(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		var body struct {
			URL         string `json:"url"`
			DeleteToken string `json:"delete_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		s.session.SetSharedURLAndToken(body.URL, body.DeleteToken)
		writeJSON(w, map[string]string{"ok": "true"})

	case http.MethodDelete:
		s.session.SetSharedURLAndToken("", "")
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFile returns file content + metadata for a single file.
// GET /api/file?path=server.go
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}
	snapshot, ok := s.session.GetFileSnapshot(path)
	if !ok {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snapshot)
}

// handleFileDiff returns diff hunks for a file.
// For code files: git diff hunks. For markdown files: inter-round LCS diff.
// GET /api/file/diff?path=server.go
func (s *Server) handleFileDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}
	snapshot, ok := s.session.GetFileDiffSnapshot(path)
	if !ok {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snapshot)
}

// handleFileComments handles GET (list) and POST (create) for file-scoped comments.
// GET/POST /api/file/comments?path=server.go
func (s *Server) handleFileComments(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}
	if s.session.FileByPath(path) == nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		comments := s.session.GetComments(path)
		writeJSON(w, comments)

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
		var req struct {
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			Side      string `json:"side"`
			Body      string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Comment body is required", http.StatusBadRequest)
			return
		}
		if req.StartLine < 1 || req.EndLine < req.StartLine {
			http.Error(w, "Invalid line range", http.StatusBadRequest)
			return
		}

		c, ok := s.session.AddComment(path, req.StartLine, req.EndLine, req.Side, req.Body)
		if !ok {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, c)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCommentByID handles PUT and DELETE for individual comments.
// PUT/DELETE /api/comment/{id}?path=server.go
func (s *Server) handleCommentByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/comment/")
	if id == "" {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
		var req struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Comment body is required", http.StatusBadRequest)
			return
		}
		c, ok := s.session.UpdateComment(path, id, req.Body)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		if !s.session.DeleteComment(path, id) {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRoundComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.session.SignalRoundComplete()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.session.WriteFiles()

	totalComments := s.session.TotalCommentCount()
	critJSON := s.session.critJSONPath()
	prompt := ""
	if totalComments > 0 {
		prompt = fmt.Sprintf(
			"Review comments are in %s — comments are grouped per file with start_line/end_line referencing the source. "+
				"Read the file, address each comment in the relevant file and location, "+
				"then mark it resolved (set \"resolved\": true, optionally \"resolution_note\" and \"resolution_lines\"). "+
				"When done run: `crit go %d`",
			critJSON, s.port)
	}

	writeJSON(w, map[string]string{
		"status":      "finished",
		"review_file": critJSON,
		"prompt":      prompt,
	})

	if s.status != nil {
		round := s.session.GetReviewRound()
		s.status.RoundFinished(round, totalComments, totalComments > 0)
		if totalComments > 0 {
			s.status.WaitingForAgent()
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch := s.session.Subscribe()
	defer s.session.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reqPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if reqPath == "" || strings.Contains(reqPath, "..") {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	baseDir := s.session.RepoRoot
	fullPath := filepath.Join(baseDir, reqPath)
	cleanPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(cleanPath, resolvedBase+string(filepath.Separator)) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, cleanPath)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
