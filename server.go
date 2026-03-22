package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
)

type Server struct {
	session        *Session
	mux            *http.ServeMux
	assets         fs.FS
	shareURL       string
	prInfo         *PRInfo
	author         string
	currentVersion    string
	latestVersion     string
	versionMu         sync.RWMutex
	staleIntegrations []staleFile
	port              int
	status            *Status
}

func NewServer(session *Session, frontendFS embed.FS, shareURL string, prInfo *PRInfo, author string, currentVersion string, port int) (*Server, error) {
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return nil, fmt.Errorf("loading frontend assets: %w", err)
	}

	s := &Server{session: session, assets: assets, shareURL: shareURL, prInfo: prInfo, author: author, currentVersion: currentVersion, port: port}

	mux := http.NewServeMux()

	// Session-scoped endpoints
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/review-cycle", s.handleReviewCycle)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/share", s.handleShare)
	mux.HandleFunc("/api/share-url", s.handleShareURL)
	mux.HandleFunc("/api/finish", s.handleFinish)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/wait-for-event", s.handleWaitForEvent)
	mux.HandleFunc("/api/round-complete", s.handleRoundComplete)

	mux.HandleFunc("/api/commits", s.handleCommits)
	mux.HandleFunc("/api/comments", s.handleReviewComments)
	mux.HandleFunc("/api/review-comment/", s.handleReviewCommentByID)
	mux.HandleFunc("/api/qr", s.handleQR)
	mux.HandleFunc("/api/files/list", s.handleFilesList)

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
	resp := map[string]interface{}{
		"share_url":      s.shareURL,
		"hosted_url":     s.session.GetSharedURL(),
		"delete_token":   s.session.GetDeleteToken(),
		"version":        s.currentVersion,
		"latest_version": latestVersion,
		"author":         s.author,
	}
	if len(s.staleIntegrations) > 0 {
		type staleInfo struct {
			Agent    string `json:"agent"`
			Location string `json:"location"`
			Hint     string `json:"hint"`
		}
		var items []staleInfo
		seen := make(map[string]bool)
		for _, sf := range s.staleIntegrations {
			hint := sf.updateHint()
			if seen[hint] {
				continue
			}
			seen[hint] = true
			items = append(items, staleInfo{Agent: sf.agent, Location: sf.location, Hint: hint})
		}
		resp["stale_integrations"] = items
	}
	if s.prInfo != nil {
		resp["pr_url"] = s.prInfo.URL
		resp["pr_number"] = s.prInfo.Number
		resp["pr_title"] = s.prInfo.Title
		resp["pr_is_draft"] = s.prInfo.IsDraft
		resp["pr_state"] = s.prInfo.State
		resp["pr_body"] = s.prInfo.Body
		resp["pr_base_ref"] = s.prInfo.BaseRefName
		resp["pr_head_ref"] = s.prInfo.HeadRefName
		resp["pr_additions"] = s.prInfo.Additions
		resp["pr_deletions"] = s.prInfo.Deletions
		resp["pr_changed_files"] = s.prInfo.ChangedFiles
		resp["pr_author"] = s.prInfo.AuthorLogin
		resp["pr_created_at"] = s.prInfo.CreatedAt
	}
	writeJSON(w, resp)
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
	scope := r.URL.Query().Get("scope")
	commit := r.URL.Query().Get("commit")
	writeJSON(w, s.session.GetSessionInfoScoped(scope, commit))
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

// handleShare uploads the current session to crit-web and returns the share URL.
// POST /api/share
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.shareURL == "" {
		http.Error(w, "share_url not configured", http.StatusBadRequest)
		return
	}

	// Idempotent: if already shared, return the existing URL without calling crit-web.
	// Uses GetShareState() to read both fields under a single lock (avoids TOCTOU race
	// where a concurrent DELETE /api/share-url could clear the token between two calls).
	if existingURL, existingToken := s.session.GetShareState(); existingURL != "" {
		writeJSON(w, map[string]any{
			"url":          existingURL,
			"delete_token": existingToken,
		})
		return
	}

	files, comments, reviewRound := buildShareFromSession(s.session)
	if len(files) == 0 {
		http.Error(w, "no files in session", http.StatusBadRequest)
		return
	}

	url, deleteToken, err := shareFilesToWeb(files, comments, s.shareURL, reviewRound)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	s.session.SetSharedURLAndToken(url, deleteToken)
	s.session.SetShareScope(shareScope(paths))
	writeJSON(w, map[string]any{"url": url, "delete_token": deleteToken})
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
		// File not in session (e.g. scoped view showing a file added after startup).
		// Try to serve it directly from disk.
		snapshot, ok = s.session.GetFileSnapshotFromDisk(path)
		if !ok {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
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
	scope := r.URL.Query().Get("scope")
	commit := r.URL.Query().Get("commit")
	snapshot, ok := s.session.GetFileDiffSnapshotScoped(path, scope, commit)
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
			Quote     string `json:"quote"`
			Author    string `json:"author"`
			Scope     string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Comment body is required", http.StatusBadRequest)
			return
		}

		// Ensure the file is registered in the session. Files that appear after
		// startup (e.g. user creates a new file while reviewing) may be visible in
		// scoped views but not yet in s.Files.
		s.session.EnsureFileEntry(path)

		if req.Scope == "file" {
			c, ok := s.session.AddFileComment(path, req.Body, req.Author)
			if !ok {
				http.Error(w, "File not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, c)
			return
		}

		if req.StartLine < 1 || req.EndLine < req.StartLine {
			http.Error(w, "Invalid line range", http.StatusBadRequest)
			return
		}

		c, ok := s.session.AddComment(path, req.StartLine, req.EndLine, req.Side, req.Body, req.Quote, req.Author)
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

// handleCommentByID handles PUT/DELETE for individual comments and CRUD for replies.
// PUT/DELETE /api/comment/{id}?path=server.go
// POST       /api/comment/{id}/replies?path=server.go
// PUT        /api/comment/{id}/replies/{rid}?path=server.go
// DELETE     /api/comment/{id}/replies/{rid}?path=server.go
func (s *Server) handleCommentByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/comment/")
	if trimmed == "" {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	// Check if this is a reply route: {id}/replies or {id}/replies/{rid}
	if parts := strings.SplitN(trimmed, "/replies", 2); len(parts) == 2 {
		commentID := parts[0]
		replyID := strings.TrimPrefix(parts[1], "/")
		s.handleReplyRoute(w, r, path, commentID, replyID)
		return
	}

	// Check if this is a resolve route: {id}/resolve
	if parts := strings.SplitN(trimmed, "/resolve", 2); len(parts) == 2 && parts[1] == "" {
		commentID := parts[0]
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Resolved bool `json:"resolved"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		c, ok := s.session.SetCommentResolved(path, commentID, req.Resolved)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)
		return
	}

	// Existing comment PUT/DELETE logic
	id := trimmed
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

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	commits := s.session.GetCommits()
	writeJSON(w, commits)
}

// replyOps abstracts the difference between file-scoped and review-scoped reply operations.
type replyOps struct {
	add    func(body, author string) (Reply, bool)
	update func(replyID, body string) (Reply, bool)
	delete func(replyID string) bool
}

// handleReplyCRUD handles POST/PUT/DELETE for reply routes using the provided operations.
func handleReplyCRUD(w http.ResponseWriter, r *http.Request, replyID string, ops replyOps) {
	switch {
	case r.Method == http.MethodPost && replyID == "":
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		var req struct {
			Body   string `json:"body"`
			Author string `json:"author"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Reply body is required", http.StatusBadRequest)
			return
		}
		reply, ok := ops.add(req.Body, req.Author)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, reply)

	case r.Method == http.MethodPut && replyID != "":
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		var req struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Reply body is required", http.StatusBadRequest)
			return
		}
		reply, ok := ops.update(replyID, req.Body)
		if !ok {
			http.Error(w, "Reply not found", http.StatusNotFound)
			return
		}
		writeJSON(w, reply)

	case r.Method == http.MethodDelete && replyID != "":
		if !ops.delete(replyID) {
			http.Error(w, "Reply not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReplyRoute(w http.ResponseWriter, r *http.Request, filePath, commentID, replyID string) {
	handleReplyCRUD(w, r, replyID, replyOps{
		add: func(body, author string) (Reply, bool) {
			return s.session.AddReply(filePath, commentID, body, author)
		},
		update: func(rid, body string) (Reply, bool) {
			return s.session.UpdateReply(filePath, commentID, rid, body)
		},
		delete: func(rid string) bool {
			return s.session.DeleteReply(filePath, commentID, rid)
		},
	})
}

func (s *Server) handleReviewCommentReplyRoute(w http.ResponseWriter, r *http.Request, commentID, replyID string) {
	handleReplyCRUD(w, r, replyID, replyOps{
		add: func(body, author string) (Reply, bool) {
			return s.session.AddReviewCommentReply(commentID, body, author)
		},
		update: func(rid, body string) (Reply, bool) {
			return s.session.UpdateReviewCommentReply(commentID, rid, body)
		},
		delete: func(rid string) bool {
			return s.session.DeleteReviewCommentReply(commentID, rid)
		},
	})
}

func (s *Server) handleReviewComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		comments := s.session.GetReviewComments()
		writeJSON(w, comments)

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		var req struct {
			Body   string `json:"body"`
			Author string `json:"author"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Body == "" {
			http.Error(w, "Comment body is required", http.StatusBadRequest)
			return
		}
		c := s.session.AddReviewComment(req.Body, req.Author)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, c)

	case http.MethodDelete:
		s.session.ClearAllComments()
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReviewCommentByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/review-comment/")
	if trimmed == "" {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
		return
	}

	// Check if this is a reply route: {id}/replies or {id}/replies/{rid}
	if parts := strings.SplitN(trimmed, "/replies", 2); len(parts) == 2 {
		commentID := parts[0]
		replyID := strings.TrimPrefix(parts[1], "/")
		s.handleReviewCommentReplyRoute(w, r, commentID, replyID)
		return
	}

	// Check if this is a resolve route: {id}/resolve
	if parts := strings.SplitN(trimmed, "/resolve", 2); len(parts) == 2 && parts[1] == "" {
		commentID := parts[0]
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Resolved bool `json:"resolved"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		c, ok := s.session.ResolveReviewComment(commentID, req.Resolved)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)
		return
	}

	id := trimmed
	switch r.Method {
	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
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
		c, ok := s.session.UpdateReviewComment(id, req.Body)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		if !s.session.DeleteReviewComment(id) {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

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
	newComments := s.session.NewCommentCount()
	unresolvedComments := s.session.UnresolvedCommentCount()
	critJSON := s.session.critJSONPath()
	prompt := ""
	if totalComments > 0 && unresolvedComments > 0 {
		prompt = fmt.Sprintf(
			"Review comments are in %s — comments are grouped per file with start_line/end_line referencing the source. "+
				"Each comment has a scope field: \"line\" for inline comments, \"file\" for file-level comments, or \"review\" for review-level comments. "+
				"Review-level comments appear in the top-level review_comments array (not tied to any file). "+
				"Read the file, address each comment in the relevant file and location. "+
				"For each comment: reply explaining what you did using `crit comment --reply-to <comment-id> --author <your-name> --resolve \"<explanation>\"`, "+
				"or edit .crit.json directly to add a reply to the comment's \"replies\" array and set \"resolved\": true. "+
				"When done run: `%s`",
			critJSON, s.session.ReinvokeCommand())
	} else if totalComments > 0 && unresolvedComments == 0 {
		prompt = "All comments are resolved — no changes needed, please proceed."
	}

	approved := unresolvedComments == 0

	writeJSON(w, map[string]any{
		"status":      "finished",
		"review_file": critJSON,
		"prompt":      prompt,
		"approved":    approved,
	})

	// Encode approved status into SSE event content as JSON so review-cycle
	// clients can extract it without string matching on the prompt.
	eventData, _ := json.Marshal(map[string]any{
		"prompt":   prompt,
		"approved": approved,
	})
	s.session.notify(SSEEvent{
		Type:    "finish",
		Content: string(eventData),
	})

	if s.status != nil {
		round := s.session.GetReviewRound()
		s.status.RoundFinished(round, newComments, unresolvedComments > 0)
		if unresolvedComments > 0 {
			s.status.WaitingForAgent()
		}
	}

}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"status":          "ok",
		"browser_clients": s.session.HasBrowserClients(),
	})
}

// handleReviewCycle is the unified endpoint for the daemon-client pattern.
// On first call (awaitingFirstReview=true): just blocks until user finishes review.
// On subsequent calls: signals round-complete first, then blocks.
// Returns the same feedback payload as handleFinish.
func (s *Server) handleReviewCycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Subscribe BEFORE round-complete to avoid missing the finish event
	// if the user clicks "Finish Review" in the brief window between
	// SignalRoundComplete and Subscribe.
	ch := s.session.Subscribe()
	defer s.session.Unsubscribe(ch)

	if !s.session.IsAwaitingFirstReview() {
		// Agent finished changes — signal round-complete so browser refreshes
		s.session.SignalRoundComplete()
	}

	for {
		select {
		case event := <-ch:
			if event.Type == "finish" {
				s.session.SetAwaitingFirstReview(false)
				// Parse the structured finish event data
				var finishData struct {
					Prompt   string `json:"prompt"`
					Approved bool   `json:"approved"`
				}
				json.Unmarshal([]byte(event.Content), &finishData)
				writeJSON(w, map[string]any{
					"status":      "finished",
					"review_file": s.session.critJSONPath(),
					"prompt":      finishData.Prompt,
					"approved":    finishData.Approved,
				})
				return
			}
		case <-r.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
	}
}

func (s *Server) handleWaitForEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ch := s.session.Subscribe()
	defer s.session.Unsubscribe(ch)

	for {
		select {
		case event := <-ch:
			if event.Type == "finish" {
				writeJSON(w, event)
				return
			}
		case <-r.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
			return
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

	s.session.BrowserConnect()
	defer s.session.BrowserDisconnect()

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

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}
	code, err := qr.Encode(url, qr.L)
	if err != nil {
		http.Error(w, "QR generation failed", http.StatusInternalServerError)
		return
	}

	size := code.Size
	scale := 4
	imgSize := size * scale
	padding := 16

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`, imgSize+padding*2, imgSize+padding*2))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if code.Black(x, y) {
				b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d"/>`, x*scale+padding, y*scale+padding, scale, scale))
			}
		}
	}
	b.WriteString(`</svg>`)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(b.String()))
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var paths []string
	var err error

	// Try git first (works in both "git" and "files" mode when inside a repo),
	// fall back to filesystem walk for non-git directories.
	paths, err = AllTrackedFiles(s.session.RepoRoot)
	if err != nil {
		paths, err = WalkFiles(s.session.RepoRoot)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	paths = filterPathsIgnored(paths, s.session.IgnorePatterns)

	query := r.URL.Query().Get("q")
	const maxResults = 10

	var results []string
	if query == "" {
		// No query: return first N paths alphabetically
		sort.Strings(paths)
		if len(paths) > maxResults {
			results = paths[:maxResults]
		} else {
			results = paths
		}
	} else {
		results = fuzzyFilterPaths(paths, query, maxResults)
	}

	if results == nil {
		results = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// fuzzyFilterPaths scores each path against query using fuzzy matching and
// returns the top N results sorted by score (descending).
func fuzzyFilterPaths(paths []string, query string, limit int) []string {
	query = strings.ToLower(query)

	type scored struct {
		path  string
		score float64
	}
	var matches []scored

	for _, p := range paths {
		s := fuzzyScore(query, p)
		if s >= 0 {
			matches = append(matches, scored{p, s})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.path
	}
	return result
}

// fuzzyScore returns a score >= 0 if all characters in query appear in text
// in order, or -1 if not. Higher scores indicate better matches.
func fuzzyScore(query, text string) float64 {
	textLower := strings.ToLower(text)
	qi := 0
	score := 0.0
	consecutive := 0
	lastMatchPos := -1

	for ti := 0; ti < len(textLower) && qi < len(query); ti++ {
		if textLower[ti] == query[qi] {
			qi++
			if ti == lastMatchPos+1 {
				consecutive++
				score += float64(consecutive) * 2
			} else {
				consecutive = 0
				score += 1
			}
			if ti == 0 || text[ti-1] == '/' || text[ti-1] == '.' || text[ti-1] == '-' || text[ti-1] == '_' {
				score += 5
			}
			lastMatchPos = ti
		}
	}

	if qi < len(query) {
		return -1
	}
	score -= float64(len(text)) * 0.1
	return score
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
