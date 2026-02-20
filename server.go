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
	doc            *Document
	mux            *http.ServeMux
	assets         fs.FS
	shareURL       string
	currentVersion string
	latestVersion  string
	versionMu      sync.RWMutex
	port           int
}

func NewServer(doc *Document, frontendFS embed.FS, shareURL string, currentVersion string, port int) *Server {
	s := &Server{doc: doc, shareURL: shareURL, currentVersion: currentVersion, port: port}

	assets, _ := fs.Sub(frontendFS, "frontend")
	s.assets = assets

	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/share-url", s.handleShareURL)
	mux.HandleFunc("/api/document", s.handleDocument)
	mux.HandleFunc("/api/comments", s.handleComments)
	mux.HandleFunc("/api/comments/", s.handleCommentByID)
	mux.HandleFunc("/api/finish", s.handleFinish)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/stale", s.handleStale)
	mux.HandleFunc("/api/round-complete", s.handleRoundComplete)
	mux.HandleFunc("/api/previous-round", s.handlePreviousRound)
	mux.HandleFunc("/api/diff", s.handleDiff)
	mux.HandleFunc("/files/", s.handleFiles)
	mux.Handle("/", http.FileServer(http.FS(assets)))

	s.mux = mux
	return s
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
		"hosted_url":     s.doc.GetSharedURL(),
		"delete_token":   s.doc.GetDeleteToken(),
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

func (s *Server) handleShareURL(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			URL         string `json:"url"`
			DeleteToken string `json:"delete_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		s.doc.SetSharedURLAndToken(body.URL, body.DeleteToken)
		writeJSON(w, map[string]string{"ok": "true"})

	case http.MethodDelete:
		s.doc.SetSharedURL("")
		s.doc.SetDeleteToken("")
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]string{
		"filename": s.doc.FileName,
		"content":  s.doc.Content,
	}
	writeJSON(w, resp)
}

func (s *Server) handleStale(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		notice := s.doc.GetStaleNotice()
		writeJSON(w, map[string]string{"notice": notice})
	case http.MethodDelete:
		s.doc.ClearStaleNotice()
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRoundComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.SignalRoundComplete()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePreviousRound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.mu.RLock()
	resp := map[string]interface{}{
		"content":      s.doc.PreviousContent,
		"comments":     s.doc.PreviousComments,
		"review_round": s.doc.reviewRound,
	}
	s.doc.mu.RUnlock()
	writeJSON(w, resp)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.mu.RLock()
	prev := s.doc.PreviousContent
	curr := s.doc.Content
	s.doc.mu.RUnlock()

	var entries []DiffEntry
	if prev != "" {
		entries = ComputeLineDiff(prev, curr)
	}
	if entries == nil {
		entries = []DiffEntry{}
	}
	writeJSON(w, map[string]interface{}{
		"entries": entries,
	})
}

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		comments := s.doc.GetComments()
		writeJSON(w, comments)

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
		var req struct {
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
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

		c := s.doc.AddComment(req.StartLine, req.EndLine, req.Body)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, c)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCommentByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/comments/")
	if id == "" {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
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
		c, ok := s.doc.UpdateComment(id, req.Body)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		if !s.doc.DeleteComment(id) {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.doc.WriteFiles()

	reviewFile := s.doc.reviewFilePath()
	prompt := ""
	if len(s.doc.GetComments()) > 0 {
		prompt = fmt.Sprintf(
			"Address review comments in %s. "+
				"Mark resolved in %s (set \"resolved\": true, optionally \"resolution_note\" and \"resolution_lines\"). "+
				"When done run: `crit go %d`",
			reviewFile, s.doc.commentsFilePath(), s.port)
	}

	writeJSON(w, map[string]string{
		"status":      "finished",
		"review_file": reviewFile,
		"prompt":      prompt,
	})

	fmt.Printf("\nReview finished. Waiting for %s to be updated...\n", s.doc.FileName)
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

	ch := s.doc.Subscribe()
	defer s.doc.Unsubscribe(ch)

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

	fullPath := filepath.Join(s.doc.FileDir, reqPath)
	cleanPath, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(cleanPath, s.doc.FileDir+string(filepath.Separator)) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, cleanPath)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
