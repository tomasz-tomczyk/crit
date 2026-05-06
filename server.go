package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsc.io/qr"
)

// sseHeartbeatInterval is the cadence for SSE keepalive comments. It's a var
// so tests can shrink it. 30s comfortably under the server's 60s IdleTimeout.
var sseHeartbeatInterval = 30 * time.Second

// Server handles HTTP requests for the crit review UI.
type Server struct {
	session           atomic.Pointer[Session]
	mux               *http.ServeMux
	assets            fs.FS
	shareURL          string
	authMu            sync.RWMutex // guards authToken + cfg.Auth* fields
	authToken         string
	prInfo            *PRInfo
	prInfoMu          sync.RWMutex
	author            string
	agentCmd          string
	currentVersion    string
	latestVersion     string
	versionMu         sync.RWMutex
	staleIntegrations []staleFile
	githubAPIURL      string // override for testing; defaults to "https://api.github.com"
	port              int
	status            *Status
	initErr           atomic.Pointer[error]
	projectDir        string
	homeDir           string
	cfg               Config
	reviewPath        string
	cliArgs           []string     // positional file args; flags (--pr, --range, etc.) are not preserved
	prList            *prListCache // 60s cache for picker "Other PRs"

	// shutdownCtx is the daemon's signal-handled context; child operations
	// (e.g. runAgentCmd subprocesses) derive their context from this so a
	// SIGINT/SIGTERM cancels them instead of leaking. Set via
	// SetShutdownCtx; nil in tests, in which case a background context is used.
	shutdownCtx context.Context
	// bgWG tracks long-running background goroutines (e.g. agent subprocess
	// runners) that must complete before the daemon writes the review file
	// during shutdown. The shutdown path Wait()s on this with a timeout.
	bgWG sync.WaitGroup
}

// NewServer creates a Server with the given session and configuration.
func NewServer(session *Session, frontendFS embed.FS, shareURL string, authToken string, author string, currentVersion string, port int, agentCmd string) (*Server, error) {
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return nil, fmt.Errorf("loading frontend assets: %w", err)
	}

	s := &Server{assets: assets, shareURL: shareURL, authToken: authToken, author: author, agentCmd: agentCmd, currentVersion: currentVersion, port: port, prList: &prListCache{}}
	if session != nil {
		s.session.Store(session)
	}

	mux := http.NewServeMux()

	// Endpoints that work without a ready session
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/qr", s.handleQR)

	// Session-dependent endpoints (guarded by withReady middleware)
	mux.HandleFunc("/api/review-cycle", s.withReady(s.handleReviewCycle))
	mux.HandleFunc("/api/config", s.withReady(s.handleConfig))
	mux.HandleFunc("/api/session", s.withReady(s.handleSession))
	mux.HandleFunc("/api/share", s.withReady(s.handleShare))
	mux.HandleFunc("/api/share-url", s.withReady(s.handleShareURL))
	mux.HandleFunc("/api/finish", s.withReady(s.handleFinish))
	mux.HandleFunc("/api/events", s.withReady(s.handleEvents))
	mux.HandleFunc("/api/wait-for-event", s.withReady(s.handleWaitForEvent))
	mux.HandleFunc("/api/round-complete", s.withReady(s.handleRoundComplete))
	mux.HandleFunc("/api/rounds", s.withReady(s.handleRounds))
	mux.HandleFunc("/api/focus", s.withReady(s.handleFocus))
	mux.HandleFunc("/api/picker", s.withReady(s.handlePicker))

	mux.HandleFunc("/api/agent/request", s.withReady(s.handleAgentRequest))
	mux.HandleFunc("/api/branches", s.withReady(s.handleBranches))
	mux.HandleFunc("/api/base-branch", s.withReady(s.handleBaseBranch))
	mux.HandleFunc("/api/commits", s.withReady(s.handleCommits))
	mux.HandleFunc("/api/comments", s.withReady(s.handleReviewComments))
	mux.HandleFunc("/api/review-comment/", s.withReady(s.handleReviewCommentByID))
	mux.HandleFunc("/api/files/list", s.withReady(s.handleFilesList))

	// File-scoped endpoints (use ?path= query param)
	mux.HandleFunc("/api/file", s.withReady(s.handleFile))
	mux.HandleFunc("/api/file/diff", s.withReady(s.handleFileDiff))
	mux.HandleFunc("/api/file/comments", s.withReady(s.handleFileComments))
	mux.HandleFunc("/api/comment/", s.withReady(s.handleCommentByID))

	// Static file serving (repo files need session; embedded assets do not)
	mux.HandleFunc("/files/", s.withReady(s.handleFiles))
	mux.Handle("/", http.FileServer(http.FS(assets)))

	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// requireReady returns false and writes a 503 or 500 response if the server
// is not yet initialized. Handlers that depend on session data call this first.
func (s *Server) requireReady(w http.ResponseWriter) bool {
	if s.session.Load() != nil {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	if errPtr := s.initErr.Load(); errPtr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": (*errPtr).Error(),
		})
		return false
	}
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "loading",
		"message": "Initializing...",
	})
	return false
}

// withReady wraps a handler so that requireReady is checked before the handler
// runs. Routes that depend on an initialized session use this at registration.
func (s *Server) withReady(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireReady(w) {
			return
		}
		next(w, r)
	}
}

// SetShutdownCtx wires the daemon's signal-handled context into the server so
// background goroutines can react to shutdown. Call this once during daemon
// startup, before any handler can fire. Tests that don't run a real daemon may
// leave it unset; effectiveCtx then falls back to context.Background().
func (s *Server) SetShutdownCtx(ctx context.Context) {
	s.shutdownCtx = ctx
}

// effectiveCtx returns the daemon shutdown ctx if set, otherwise a background
// ctx. Used by background goroutines so test paths (which don't run a real
// daemon and never call SetShutdownCtx) keep working.
func (s *Server) effectiveCtx() context.Context {
	if s.shutdownCtx != nil {
		return s.shutdownCtx
	}
	return context.Background()
}

// WaitBackground blocks until all tracked background goroutines (currently:
// agent subprocess runners) have returned, or until timeout elapses. Returns
// true on clean drain, false on timeout. Called from the daemon shutdown path
// to give in-flight agent runs a chance to post their replies before
// session.WriteFiles() persists the final state.
func (s *Server) WaitBackground(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// SetSession attaches a fully initialized session and marks the server as ready.
// Uses atomic.Pointer to ensure the session pointer is visible to all goroutines
// immediately after store, which is critical on weakly-ordered architectures (ARM64).
//
// Also flips the session's sessionStarted flag so Session.loadCritJSON can
// enforce its pre-SetSession-only lock contract (see plan v4 §Lock discipline).
//
// Ordering matters: sessionStarted MUST be stored BEFORE the session pointer
// is published. After s.session.Store, withReady (and any goroutine that
// observes the session pointer) can call session methods immediately. If
// sessionStarted were still 0 at that moment, a code path that reaches
// loadCritJSON would falsely believe it's pre-SetSession and skip the guard.
func (s *Server) SetSession(session *Session) {
	if session != nil {
		session.sessionStarted.Store(1)
	}
	s.session.Store(session)
}

// SetPRInfo updates the PR metadata after the session is already ready.
func (s *Server) SetPRInfo(prInfo *PRInfo) {
	s.prInfoMu.Lock()
	s.prInfo = prInfo
	s.prInfoMu.Unlock()
}

// SetInitErr records a fatal initialization error. Subsequent API calls
// return 500 with the error message instead of retryable 503s.
func (s *Server) SetInitErr(err error) {
	e := err
	s.initErr.Store(&e)
}

// CheckForUpdates fetches the latest release tag from GitHub and stores
// it so the frontend can display an update notification. Safe to call
// from a goroutine — the result is written under versionMu.
func (s *Server) CheckForUpdates() {
	if s.currentVersion == "" || s.currentVersion == "dev" {
		return
	}
	base := s.githubAPIURL
	if base == "" {
		base = "https://api.github.com"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/repos/tomasz-tomczyk/crit/releases/latest")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil || release.TagName == "" {
		return
	}
	s.versionMu.Lock()
	s.latestVersion = release.TagName
	s.versionMu.Unlock()
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.versionMu.RLock()
	latestVersion := s.latestVersion
	s.versionMu.RUnlock()
	sess := s.session.Load()
	resp := map[string]interface{}{
		"share_url":         s.shareURL,
		"hosted_url":        sess.GetSharedURL(),
		"delete_token":      sess.GetDeleteToken(),
		"version":           s.currentVersion,
		"latest_version":    latestVersion,
		"author":            s.author,
		"agent_cmd_enabled": s.agentCmd != "",
		"agent_name":        agentName(s.agentCmd),
		"agent_cmd":         s.agentCmd,

		// Auth status
		"auth_logged_in":  s.authLoggedIn(),
		"auth_user_name":  s.authUserName(),
		"auth_user_email": s.authUserEmail(),

		// Review file path
		"review_path": s.reviewPath,

		// Config pass-throughs for frontend suppression
		"no_integration_check": s.cfg.NoIntegrationCheck,
		"no_update_check":      s.cfg.NoUpdateCheck,

		// Available integrations (always included)
		"integrations_available": availableIntegrations(),
	}

	// Integration detection
	s.addIntegrationStatus(resp)

	if len(s.staleIntegrations) > 0 {
		type staleInfo struct {
			Agent    string `json:"agent"`
			Location string `json:"location"`
			Hint     string `json:"hint"`
			Hash     string `json:"hash,omitempty"`
		}
		var items []staleInfo
		seen := make(map[string]bool)
		for _, sf := range s.staleIntegrations {
			hint := sf.updateHint()
			if seen[hint] {
				continue
			}
			seen[hint] = true
			items = append(items, staleInfo{Agent: sf.agent, Location: sf.location, Hint: hint, Hash: sf.hash})
		}
		resp["stale_integrations"] = items
	}
	s.prInfoMu.RLock()
	prInfo := s.prInfo
	s.prInfoMu.RUnlock()
	if prInfo != nil {
		resp["pr_url"] = prInfo.URL
		resp["pr_number"] = prInfo.Number
		resp["pr_title"] = prInfo.Title
		resp["pr_is_draft"] = prInfo.IsDraft
		resp["pr_state"] = prInfo.State
		resp["pr_body"] = prInfo.Body
		resp["pr_base_ref"] = prInfo.BaseRefName
		resp["pr_head_ref"] = prInfo.HeadRefName
		resp["pr_additions"] = prInfo.Additions
		resp["pr_deletions"] = prInfo.Deletions
		resp["pr_changed_files"] = prInfo.ChangedFiles
		resp["pr_author"] = prInfo.AuthorLogin
		resp["pr_created_at"] = prInfo.CreatedAt
	}
	writeJSON(w, resp)
}

// addIntegrationStatus populates integration detection fields in the config response.
func (s *Server) addIntegrationStatus(resp map[string]interface{}) {
	if s.cfg.NoIntegrationCheck {
		resp["integrations"] = []integrationStatus{}
		resp["any_integration_installed"] = false
		return
	}
	integrations := detectInstalledIntegrations(s.projectDir, s.homeDir)
	resp["integrations"] = integrations
	resp["any_integration_installed"] = len(integrations) > 0
}

// parseRoundParam extracts and validates the ?round=N query parameter.
//
// Returns:
//   - (0, false, true): the parameter is absent or empty — caller should not
//     apply any round filter (back-compat: pre-feature clients omit it).
//   - (N, true, true):  parsed round >= 1 — caller may apply the filter.
//   - (0, false, false): invalid — the function has already written a 400
//     response and the caller MUST return without writing further.
//
// All four round-aware endpoints (/api/session, /api/file, /api/file/diff,
// /api/file/comments, /api/comments) go through this helper so the contract
// stays uniform: a malformed value (e.g. "abc", "-1", "0") always yields 400.
func parseRoundParam(w http.ResponseWriter, r *http.Request) (round int, ok bool, valid bool) {
	roundStr := r.URL.Query().Get("round")
	if roundStr == "" {
		return 0, false, true
	}
	n, err := strconv.Atoi(roundStr)
	if err != nil || n < 1 {
		http.Error(w, "invalid round", http.StatusBadRequest)
		return 0, false, false
	}
	return n, true, true
}

// handleSession returns session metadata: mode, branch, file list with stats.
//
// GET /api/session[?scope=X&commit=Y&round=N]
//
// In files mode, ?round=N filters the file list to files that had a
// snapshot at that round (so files added in later rounds drop out when
// viewing an earlier point in the timeline). The round parameter is
// ignored in git/range mode.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	round, hasRound, valid := parseRoundParam(w, r)
	if !valid {
		return
	}
	scope := r.URL.Query().Get("scope")
	commit := r.URL.Query().Get("commit")
	session := s.session.Load()
	info := session.GetSessionInfoScoped(scope, commit)

	if hasRound && session != nil && session.Mode == "files" {
		info.Files = filterFilesAtRound(session, info.Files, round)
	}
	writeJSON(w, info)
}

// filterFilesAtRound returns the subset of files that have a snapshot recorded
// at the given round. Caller must not hold session.mu.
func filterFilesAtRound(session *Session, files []SessionFileInfo, round int) []SessionFileInfo {
	session.mu.RLock()
	defer session.mu.RUnlock()
	out := make([]SessionFileInfo, 0, len(files))
	for _, f := range files {
		byRound := session.RoundSnapshots[f.Path]
		if byRound == nil {
			continue
		}
		if _, ok := byRound[round]; !ok {
			continue
		}
		out = append(out, f)
	}
	return out
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
		s.session.Load().SetSharedURLAndToken(body.URL, body.DeleteToken)
		writeJSON(w, map[string]string{"ok": "true"})

	case http.MethodDelete:
		s.session.Load().SetSharedURLAndToken("", "")
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
	if existingURL, existingToken := s.session.Load().GetShareState(); existingURL != "" {
		writeJSON(w, map[string]any{
			"url":          existingURL,
			"delete_token": existingToken,
		})
		return
	}

	// Read file content from disk and comments from the review file.
	// This uses the same disk-based path as `crit share` (CLI), ensuring
	// a single source of truth for the share payload. The review file is
	// kept current by saveCritJSON (200ms debounce on every comment change).
	files := s.session.Load().LoadShareFilesFromDisk()
	if len(files) == 0 {
		http.Error(w, "no files in session", http.StatusBadRequest)
		return
	}

	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.Path
	}

	critPath := s.session.Load().critJSONPath()
	res, err := shareReviewFiles(critPath, files, filePaths, s.shareURL, s.authTokenSnapshot(), s.author)
	if err != nil {
		if errors.Is(err, errShareUnauthorized) {
			clearAuthIdentity()
			s.clearAuthState()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.session.Load().SetSharedURLAndToken(res.URL, res.DeleteToken)
	s.session.Load().SetShareScope(shareScope(filePaths))
	writeJSON(w, map[string]any{"url": res.URL, "delete_token": res.DeleteToken})
}

// handleRounds returns the per-round timeline for files-mode sessions. In
// git/range mode it returns 200 with an empty rounds list (the wire shape
// stays stable so the frontend doesn't need mode-specific code paths).
//
// GET /api/rounds
func (s *Server) handleRounds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session := s.session.Load()
	if session == nil {
		writeJSON(w, map[string]any{"current_round": 0, "rounds": []any{}})
		return
	}

	type roundEntry struct {
		N            int    `json:"n"`
		Additions    int    `json:"additions"`
		Deletions    int    `json:"deletions"`
		CommentCount int    `json:"comment_count"`
		CapturedAt   string `json:"captured_at"`
	}

	// Span current_round + rounds slice under a single RLock so a
	// round-complete that lands between the two reads can't yield an
	// internally inconsistent response (e.g. current=N with rounds ending at N-1).
	session.mu.RLock()
	defer session.mu.RUnlock()

	resp := map[string]any{
		"current_round": session.ReviewRound,
		"rounds":        []roundEntry{},
	}

	if session.Mode != "files" {
		writeJSON(w, resp)
		return
	}

	rounds := session.availableRounds()
	if len(rounds) == 0 {
		writeJSON(w, resp)
		return
	}

	// Per-round comment counts (comments where review_round == n).
	counts := make(map[int]int, len(rounds))
	for _, f := range session.Files {
		for _, c := range f.Comments {
			counts[c.ReviewRound]++
		}
	}
	for _, c := range session.reviewComments {
		counts[c.ReviewRound]++
	}

	out := make([]roundEntry, 0, len(rounds))
	for _, r := range rounds {
		var capturedAt string
		// Pick the EARLIEST CapturedAt across every file that snapshotted at
		// this round — Go map iteration order is randomized, so picking "the
		// first" produced a non-deterministic captured_at across requests.
		// The earliest is a meaningful representative (the moment the round
		// began capturing) and stable for any given snapshot map.
		var earliest time.Time
		for _, byRound := range session.RoundSnapshots {
			rs, ok := byRound[r]
			if !ok {
				continue
			}
			if earliest.IsZero() || rs.CapturedAt.Before(earliest) {
				earliest = rs.CapturedAt
			}
		}
		if !earliest.IsZero() {
			capturedAt = earliest.Format(time.RFC3339)
		}
		adds, dels := lineStatsForRound(session, r)
		out = append(out, roundEntry{
			N:            r,
			Additions:    adds,
			Deletions:    dels,
			CommentCount: counts[r],
			CapturedAt:   capturedAt,
		})
	}
	resp["rounds"] = out
	writeJSON(w, resp)
}

// lineStatsForRound aggregates added/removed line counts across every file
// with a snapshot at round n, comparing against round n-1. R1 (or any round
// where no n-1 snapshots exist) returns 0/0. Caller must hold session.mu
// (RLock is sufficient).
func lineStatsForRound(session *Session, n int) (int, int) {
	if n <= 1 {
		return 0, 0
	}
	var adds, dels int
	for _, byRound := range session.RoundSnapshots {
		curr, ok := byRound[n]
		if !ok {
			continue
		}
		prev, hasPrev := byRound[n-1]
		if !hasPrev {
			// New file at round n: every line counts as an addition.
			adds += len(splitLines(curr.Content))
			continue
		}
		entries := ComputeLineDiff(prev.Content, curr.Content)
		for _, e := range entries {
			switch e.Type {
			case "added":
				adds++
			case "removed":
				dels++
			}
		}
	}
	return adds, dels
}

// handleFile returns file content + metadata for a single file.
// GET /api/file?path=server.go[&round=N]
//
// In files mode, ?round=N returns the snapshot recorded for that round. In
// git/range mode, the round parameter is ignored.
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

	if served := serveFileAtRound(w, r, s.session.Load(), path); served {
		return
	}

	snapshot, ok := s.session.Load().GetFileSnapshot(path)
	if !ok {
		// File not in session (e.g. scoped view showing a file added after startup).
		// Try to serve it directly from disk.
		snapshot, ok = s.session.Load().GetFileSnapshotFromDisk(path)
		if !ok {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
	}
	writeJSON(w, snapshot)
}

// serveFileAtRound writes a per-round snapshot response if the request
// includes a valid ?round=N for a files-mode session that has a snapshot for
// (path, round). Returns served=true when it has fully written the response
// (including 400/404). Returns served=false when the caller should fall
// through to the working-tree code path (no round param, git/range mode, or
// no snapshot recorded for this round).
func serveFileAtRound(w http.ResponseWriter, r *http.Request, session *Session, path string) bool {
	round, hasRound, valid := parseRoundParam(w, r)
	if !valid {
		// parseRoundParam wrote 400; signal handled.
		return true
	}
	if !hasRound {
		return false
	}
	if session == nil || session.Mode != "files" {
		return false
	}
	session.mu.RLock()
	rs, ok := session.roundSnapshotForFile(path, round)
	prev, hasPrev := session.roundSnapshotForFile(path, round-1)
	session.mu.RUnlock()
	if !ok {
		http.Error(w, "file_not_in_round", http.StatusNotFound)
		return true
	}
	resp := map[string]any{
		"path":     path,
		"round":    round,
		"content":  rs.Content,
		"status":   rs.Status,
		"position": rs.Position,
	}
	if hasPrev {
		resp["previous_content"] = prev.Content
	}
	writeJSON(w, resp)
	return true
}

// handleFileDiff returns diff hunks for a file.
// For code files: git diff hunks. For markdown files: inter-round LCS diff.
// GET /api/file/diff?path=server.go[&round=N]
//
// In files mode, ?round=N returns the diff between round N's snapshot and
// round (N-1)'s snapshot. R1 is the baseline and has no previous content,
// so the response carries empty hunks. In git/range mode, the round
// parameter is ignored.
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

	if served := serveFileDiffAtRound(w, r, s.session.Load(), path); served {
		return
	}

	scope := r.URL.Query().Get("scope")
	commit := r.URL.Query().Get("commit")
	snapshot, ok := s.session.Load().GetFileDiffSnapshotScoped(path, scope, commit)
	if !ok {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snapshot)
}

// serveFileDiffAtRound writes a per-round diff response when the request
// includes a valid ?round=N for a files-mode session. Returns served=true
// when it has fully written the response (success or 400/404). Returns
// served=false when the caller should fall through to the working-tree code
// path (no round param, or git/range mode).
func serveFileDiffAtRound(w http.ResponseWriter, r *http.Request, session *Session, path string) bool {
	round, hasRound, valid := parseRoundParam(w, r)
	if !valid {
		return true
	}
	if !hasRound {
		return false
	}
	if session == nil || session.Mode != "files" {
		return false
	}
	session.mu.RLock()
	rs, ok := session.roundSnapshotForFile(path, round)
	prev, hasPrev := session.roundSnapshotForFile(path, round-1)
	session.mu.RUnlock()
	if !ok {
		http.Error(w, "file_not_in_round", http.StatusNotFound)
		return true
	}

	resp := map[string]any{
		"hunks":            []DiffHunk{},
		"previous_content": prev.Content,
	}
	if hasPrev && prev.Content != rs.Content {
		entries := ComputeLineDiff(prev.Content, rs.Content)
		hunks := DiffEntriesToHunks(entries)
		if hunks == nil {
			hunks = []DiffHunk{}
		}
		resp["hunks"] = hunks
	}
	writeJSON(w, resp)
	return true
}

// handleFileComments handles GET (list) and POST (create) for file-scoped comments.
// GET/POST /api/file/comments?path=server.go
//
// In files mode, ?round=N filters the GET response via commentsAtOrBeforeRound:
// only comments authored at or before round N (and replies authored at or
// before N) are returned. Note that the Resolved / ResolvedRound fields on
// each returned comment reflect *current* state, not state-at-round-N — the
// frontend uses ResolvedRound to compute round-faithful resolution itself.
// See commentsAtOrBeforeRound for the full Stage 1 vs Stage 2 contract.
func (s *Server) handleFileComments(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		round, hasRound, valid := parseRoundParam(w, r)
		if !valid {
			return
		}
		comments := s.session.Load().GetComments(path)
		if hasRound && s.session.Load().Mode == "files" {
			comments = commentsAtOrBeforeRound(comments, round)
		}
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
		s.session.Load().EnsureFileEntry(path)

		if req.Scope == "file" {
			c, ok := s.session.Load().AddFileComment(path, req.Body, req.Author, s.authUserID())
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

		c, ok := s.session.Load().AddComment(path, req.StartLine, req.EndLine, normalizeCommentSide(req.Side), req.Body, req.Quote, req.Author, s.authUserID())
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

// normalizeCommentSide canonicalizes the wire `side` field to crit's internal
// representation: "" for the new (right) side and "old" for the deletion (left)
// side. Callers may pass GitHub-style "RIGHT"/"LEFT" (e.g. seeded payloads,
// pulled PR comments, third-party scripts); without normalization, those values
// flow into Comment.Side unchanged and cause the frontend's diff renderer to
// miss them when keying by `lineNumber + ':' + side`, falsely flagging fresh
// comments as "outdated" on first load.
func normalizeCommentSide(s string) string {
	switch strings.ToUpper(s) {
	case "LEFT", "OLD":
		return "old"
	case "RIGHT", "NEW", "":
		return ""
	default:
		return s
	}
}

// handleCommentByID handles PUT/DELETE for individual comments and CRUD for replies.
// PUT/DELETE /api/comment/{id}?path=server.go
// POST       /api/comment/{id}/replies?path=server.go
// PUT        /api/comment/{id}/replies/{rid}?path=server.go
// DELETE     /api/comment/{id}/replies/{rid}?path=server.go
// commentRoute holds the parsed components of a comment-by-ID URL path.
type commentRoute struct {
	kind string // "reply", "resolve", or "comment"
	id   string // the comment ID
	sub  string // for replies: the reply ID (may be empty for POST)
}

// routeCommentByID parses a URL suffix like "c5", "c5/replies", "c5/replies/r2",
// or "c5/resolve" and returns the route components. Returns false if the suffix is empty.
func routeCommentByID(trimmed string) (commentRoute, bool) {
	if trimmed == "" {
		return commentRoute{}, false
	}
	if parts := strings.SplitN(trimmed, "/replies", 2); len(parts) == 2 {
		return commentRoute{
			kind: "reply",
			id:   parts[0],
			sub:  strings.TrimPrefix(parts[1], "/"),
		}, true
	}
	if parts := strings.SplitN(trimmed, "/resolve", 2); len(parts) == 2 && parts[1] == "" {
		return commentRoute{kind: "resolve", id: parts[0]}, true
	}
	return commentRoute{kind: "comment", id: trimmed}, true
}

func (s *Server) handleCommentByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/comment/")
	route, ok := routeCommentByID(trimmed)
	if !ok {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	switch route.kind {
	case "reply":
		s.handleReplyRoute(w, r, path, route.id, route.sub)
	case "resolve":
		s.handleFileCommentResolve(w, r, path, route.id)
	case "comment":
		s.handleFileCommentUpdate(w, r, path, route.id)
	}
}

// handleFileCommentResolve handles PUT /api/comment/{id}/resolve?path=X.
func (s *Server) handleFileCommentResolve(w http.ResponseWriter, r *http.Request, path, commentID string) {
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
	c, ok := s.session.Load().SetCommentResolved(path, commentID, req.Resolved)
	if !ok {
		http.Error(w, "Comment not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

// handleFileCommentUpdate handles PUT and DELETE on /api/comment/{id}?path=X.
func (s *Server) handleFileCommentUpdate(w http.ResponseWriter, r *http.Request, path, id string) {
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
		c, ok := s.session.Load().UpdateComment(path, id, req.Body)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		if !s.session.Load().DeleteComment(path, id) {
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
	commits := s.session.Load().GetCommits()
	writeJSON(w, commits)
}

// handleBranches returns remote branch names for the base-branch picker.
func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.session.Load()
	sess.mu.RLock()
	repoRoot := sess.RepoRoot
	vcs := sess.VCS
	sess.mu.RUnlock()
	if vcs == nil {
		writeJSON(w, []string{})
		return
	}
	branches, err := vcs.RemoteBranches(repoRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, branches)
}

// handleBaseBranch changes the diff base branch for the current session.
func (s *Server) handleBaseBranch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Branch == "" {
		http.Error(w, "Bad request: branch is required", http.StatusBadRequest)
		return
	}
	if err := s.session.Load().ChangeBaseBranch(body.Branch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
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
			return s.session.Load().AddReply(filePath, commentID, body, author, s.authUserID())
		},
		update: func(rid, body string) (Reply, bool) {
			return s.session.Load().UpdateReply(filePath, commentID, rid, body)
		},
		delete: func(rid string) bool {
			return s.session.Load().DeleteReply(filePath, commentID, rid)
		},
	})
}

func (s *Server) handleReviewCommentReplyRoute(w http.ResponseWriter, r *http.Request, commentID, replyID string) {
	handleReplyCRUD(w, r, replyID, replyOps{
		add: func(body, author string) (Reply, bool) {
			return s.session.Load().AddReviewCommentReply(commentID, body, author, s.authUserID())
		},
		update: func(rid, body string) (Reply, bool) {
			return s.session.Load().UpdateReviewCommentReply(commentID, rid, body)
		},
		delete: func(rid string) bool {
			return s.session.Load().DeleteReviewCommentReply(commentID, rid)
		},
	})
}

func (s *Server) handleReviewComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		round, hasRound, valid := parseRoundParam(w, r)
		if !valid {
			return
		}
		comments := s.session.Load().GetReviewComments()
		if hasRound && s.session.Load().Mode == "files" {
			comments = commentsAtOrBeforeRound(comments, round)
		}
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
		c := s.session.Load().AddReviewComment(req.Body, req.Author, s.authUserID())
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, c)

	case http.MethodDelete:
		s.session.Load().ClearAllComments()
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReviewCommentByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/review-comment/")
	route, ok := routeCommentByID(trimmed)
	if !ok {
		http.Error(w, "Comment ID required", http.StatusBadRequest)
		return
	}

	switch route.kind {
	case "reply":
		s.handleReviewCommentReplyRoute(w, r, route.id, route.sub)
	case "resolve":
		s.handleReviewCommentResolve(w, r, route.id)
	case "comment":
		s.handleReviewCommentUpdate(w, r, route.id)
	}
}

// handleReviewCommentResolve handles PUT /api/review-comment/{id}/resolve.
func (s *Server) handleReviewCommentResolve(w http.ResponseWriter, r *http.Request, commentID string) {
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
	c, ok := s.session.Load().ResolveReviewComment(commentID, req.Resolved)
	if !ok {
		http.Error(w, "Comment not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

// handleReviewCommentUpdate handles PUT and DELETE on /api/review-comment/{id}.
func (s *Server) handleReviewCommentUpdate(w http.ResponseWriter, r *http.Request, id string) {
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
		c, ok := s.session.Load().UpdateReviewComment(id, req.Body)
		if !ok {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		if !s.session.Load().DeleteReviewComment(id) {
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
	sess := s.session.Load()
	sess.mu.RLock()
	isRange := sess.Focus.Kind == FocusRange
	sess.mu.RUnlock()
	if isRange {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "round-complete is not meaningful in range mode",
		})
		return
	}
	sess.SignalRoundComplete()
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleFocus accepts POST /api/focus to switch the session's focus.
// Body is a JSON Focus payload. Rejects full-stack scope without DefaultSHA
// with HTTP 400. SetFocus emits SSE focus-changed on success.
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req Focus
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.session.Load().SetFocus(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sess := s.session.Load()
	// Synchronous, serialized flush. The response includes review_file —
	// CLI clients and the e2e suite read that path verbatim, so the file
	// must exist on disk before we hand the path back. Bare WriteFiles
	// races with the debounce timer (both call atomicWriteFile concurrently
	// on the same target, which has manifested as ENOENT failures on
	// Windows where the read-after-write window is wider).
	if err := sess.SyncWriteFiles(); err != nil {
		http.Error(w, fmt.Sprintf("writing review file: %v", err), http.StatusInternalServerError)
		return
	}

	totalComments := sess.TotalCommentCount()
	newComments := sess.NewCommentCount()
	unresolvedComments := sess.UnresolvedCommentCount()
	// In v4 the session identity is a folder (.../<key>/ or .../.crit/); the
	// agent-facing review payload lives at <identity>/review.json. Surface the
	// file path, not the folder, so `cat $review_file` works.
	reviewFile := reviewPathsFor(sess.critJSONPath()).Review
	prompt := ""
	if totalComments > 0 && unresolvedComments > 0 {
		if sess.Mode == "plan" {
			// Plan mode: concise feedback for the hook workflow.
			// Claude revises the plan text directly — no need for crit comment or review file instructions.
			prompt = s.buildPlanFeedback(reviewFile)
		} else {
			prompt = fmt.Sprintf(
				"Review comments are in %s — comments are grouped per file with start_line/end_line referencing the source. "+
					"Each comment has a scope field: \"line\" for inline comments, \"file\" for file-level comments, or \"review\" for review-level comments. "+
					"Review-level comments appear in the top-level review_comments array (not tied to any file). "+
					"Read the file, address each unresolved comment in the relevant file and location. "+
					"Before acting, check each comment's replies array — if you have already replied, the reviewer may be following up conversationally rather than requesting a new code change. "+
					"For each comment, reply explaining what you did using `crit comment --reply-to <comment-id> --author <your-name> \"<explanation>\"`. "+
					"When done run: `%s`",
				reviewFile, sess.ReinvokeCommand())
		}
	} else if totalComments > 0 && unresolvedComments == 0 {
		prompt = "All comments are resolved — no changes needed, please proceed."
	}

	approved := unresolvedComments == 0
	if !approved {
		sess.setWaitingForAgent(true)
	}

	writeJSON(w, map[string]any{
		"status":      "finished",
		"review_file": reviewFile,
		"prompt":      prompt,
		"approved":    approved,
	})

	// Encode approved status into SSE event content as JSON so review-cycle
	// clients can extract it without string matching on the prompt.
	eventData, _ := json.Marshal(map[string]any{
		"prompt":   prompt,
		"approved": approved,
	})
	sess.notify(SSEEvent{
		Type:    "finish",
		Content: string(eventData),
	})

	if s.status != nil {
		round := sess.GetReviewRound()
		s.status.RoundFinished(round, newComments, unresolvedComments > 0)
		if unresolvedComments > 0 {
			s.status.WaitingForAgent()
		}
	}

}

// buildPlanFeedback formats review feedback for plan mode.
// Points to the review file and hints at crit-cli skill, without inlining every comment.
func (s *Server) buildPlanFeedback(reviewFile string) string {
	// Extract slug from PlanDir (last path component)
	slug := filepath.Base(s.session.Load().PlanDir)
	return fmt.Sprintf(
		"Plan review feedback — revise the plan to address the review comments. "+
			"Comments are in %s — grouped per file with start_line/end_line referencing the source. "+
			"Each comment has a scope field: \"line\" for inline comments, \"file\" for file-level, or \"review\" for review-level comments. "+
			"Read the file, revise the plan to address each comment. "+
			"To reply to comments, use `crit comment --plan %s --reply-to <id> --author <your-name> \"<explanation>\"`.",
		reviewFile, slug)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	browserClients := false
	if sess := s.session.Load(); sess != nil {
		browserClients = sess.HasBrowserClients()
	}
	writeJSON(w, map[string]any{
		"status":          "ok",
		"browser_clients": browserClients,
	})
}

// buildNextCommand renders the command the agent should run to start the
// next review round, given the args the daemon was launched with.
func buildNextCommand(args []string) string {
	if len(args) == 0 {
		return "crit"
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "crit")
	for _, a := range args {
		parts = append(parts, shellQuoteArg(a))
	}
	return strings.Join(parts, " ")
}

// shellQuoteArg quotes a single CLI arg using POSIX single-quote syntax when
// it contains whitespace or shell metacharacters; returns it unchanged
// otherwise. Single quotes inside the arg are escaped as '\”.
func shellQuoteArg(a string) string {
	if a == "" {
		return `''`
	}
	if !strings.ContainsAny(a, " \t\n\"'\\$`*?[]{}();&|<>#~!") {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
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

	sess := s.session.Load()

	// Subscribe BEFORE round-complete to avoid missing the finish event
	// if the user clicks "Finish Review" in the brief window between
	// SignalRoundComplete and Subscribe.
	ch := sess.Subscribe()
	defer sess.Unsubscribe(ch)

	if !sess.IsAwaitingFirstReview() {
		// Agent finished changes — signal round-complete so browser refreshes
		sess.SignalRoundComplete()
	}

	for {
		select {
		case event := <-ch:
			if event.Type == "finish" {
				sess.SetAwaitingFirstReview(false)
				// Parse the structured finish event data
				var finishData struct {
					Prompt   string `json:"prompt"`
					Approved bool   `json:"approved"`
				}
				json.Unmarshal([]byte(event.Content), &finishData)
				writeJSON(w, map[string]any{
					"status":       "finished",
					"review_file":  reviewPathsFor(sess.critJSONPath()).Review,
					"prompt":       finishData.Prompt,
					"approved":     finishData.Approved,
					"next_command": buildNextCommand(s.cliArgs),
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

	sess := s.session.Load()
	ch := sess.Subscribe()
	defer sess.Unsubscribe(ch)

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

	// Safari won't fire EventSource.onopen until it sees a body byte.
	fmt.Fprint(w, ":\n\n")
	flusher.Flush()

	sess := s.session.Load()
	sess.BrowserConnect()
	defer sess.BrowserDisconnect()

	ch := sess.Subscribe()
	defer sess.Unsubscribe(ch)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ":\n\n")
			flusher.Flush()
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

	baseDir := s.session.Load().RepoRoot
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

	sess := s.session.Load()
	var paths []string
	var err error

	// Try VCS first (works in both "git" and "files" mode when inside a repo),
	// fall back to filesystem walk for non-VCS directories.
	if sess.VCS != nil {
		paths, err = sess.VCS.AllTrackedFiles(sess.RepoRoot)
	} else {
		err = fmt.Errorf("no VCS")
	}
	if err != nil {
		paths, err = WalkFiles(sess.RepoRoot)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	paths = filterPathsIgnored(paths, sess.IgnorePatterns)

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

// agentRequestBody is the JSON body for POST /api/agent/request.
type agentRequestBody struct {
	CommentID string `json:"comment_id"`
	FilePath  string `json:"file_path"`
}

// agentName extracts the binary name from the agent command string.
func agentName(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "agent"
	}
	return filepath.Base(parts[0])
}

// handleAgentRequest dispatches a comment to the configured agent command.
// POST /api/agent/request
func (s *Server) handleAgentRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.agentCmd == "" {
		http.Error(w, "agent_cmd not configured", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body agentRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CommentID == "" {
		http.Error(w, "Bad request: comment_id required", http.StatusBadRequest)
		return
	}

	comment, filePath, found := s.session.Load().FindCommentByID(body.CommentID, body.FilePath)
	if !found {
		http.Error(w, "Comment not found", http.StatusNotFound)
		return
	}

	s.session.Load().SetCommentLive(filePath, comment.ID)

	prompt := buildAgentPrompt(comment, filePath)

	// Run agent command asynchronously. Tracked via bgWG so the daemon
	// shutdown path can wait for the reply to be posted before WriteFiles
	// persists the final review state.
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		s.runAgentCmd(prompt, comment.ID, filePath)
	}()

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{
		"status":     "accepted",
		"comment_id": body.CommentID,
		"file_path":  filePath,
	})
}

// buildAgentPrompt constructs a prompt string from a comment for the agent.
func buildAgentPrompt(c Comment, filePath string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("A reviewer left a comment on %s", filePath))
	if c.StartLine > 0 {
		if c.EndLine > c.StartLine {
			b.WriteString(fmt.Sprintf(" (lines %d-%d)", c.StartLine, c.EndLine))
		} else {
			b.WriteString(fmt.Sprintf(" (line %d)", c.StartLine))
		}
	}
	b.WriteString(":\n\n")
	if c.Quote != "" {
		b.WriteString("Code:\n```\n")
		b.WriteString(c.Quote)
		b.WriteString("\n```\n\n")
	}
	b.WriteString("Comment:\n> ")
	b.WriteString(c.Body)
	b.WriteString("\n\n")
	for _, reply := range c.Replies {
		b.WriteString(fmt.Sprintf("Reply from %s:\n> %s\n\n", reply.Author, reply.Body))
	}
	b.WriteString("Address this comment. If it requires a code change, make the edit.\n\n" +
		"IMPORTANT: Do NOT run `crit comment` or `crit` commands. " +
		"Just print your response to stdout — it will be posted as a reply automatically.\n")
	return b.String()
}

// runAgentCmd executes the configured agent command with the given prompt.
// If agent_cmd contains {prompt}, the placeholder is replaced with the prompt
// as a single argument. Otherwise, the prompt is piped via stdin.
func (s *Server) runAgentCmd(prompt string, commentID string, filePath string) {
	// Parent on the daemon shutdown ctx so SIGINT/SIGTERM kills
	// the agent subprocess instead of orphaning it (the subprocess has its
	// own session id via daemonSysProcAttr). Tests that don't wire a shutdown
	// ctx fall back to context.Background() — same behavior as before.
	ctx, cancel := context.WithTimeout(s.effectiveCtx(), 10*time.Minute)
	defer cancel()

	parts := strings.Fields(s.agentCmd)
	if len(parts) == 0 {
		return
	}
	log.Printf("agent-request %s: running %q", commentID, s.agentCmd)

	// Replace {prompt} placeholder with the actual prompt as a single argument.
	hasPlaceholder := false
	for i, p := range parts {
		if p == "{prompt}" {
			parts[i] = prompt
			hasPlaceholder = true
		}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	if !hasPlaceholder {
		cmd.Stdin = strings.NewReader(prompt)
	}
	sess := s.session.Load()
	cmd.Dir = sess.RepoRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		log.Printf("agent-request %s: error: %v\nStderr: %s", commentID, err, stderr.String())
		return
	}

	response := strings.TrimSpace(stdout.String())
	if response == "" {
		log.Printf("agent-request %s: completed (no output)", commentID)
		return
	}

	author := agentName(s.agentCmd)
	log.Printf("agent-request %s: completed, posting reply (%d bytes)\nResponse: %s\nStderr: %s", commentID, len(response), response, stderr.String())
	// Try original path first, then search all files (path may have changed during agent run)
	_, ok := sess.AddReply(filePath, commentID, response, author, "")
	if !ok {
		if _, actualPath, found := sess.FindCommentByID(commentID, ""); found {
			_, ok = sess.AddReply(actualPath, commentID, response, author, "")
			if ok {
				filePath = actualPath
			}
		}
	}
	if !ok {
		log.Printf("agent-request %s: failed to add reply (comment not found in file %q)", commentID, filePath)
	} else {
		// On shutdown, skip the refresh fan-out: the SSE subscribers are gone
		// and we're about to WriteFiles. The reply is already in the session
		// (added by AddReply above) and will be persisted.
		select {
		case <-s.effectiveCtx().Done():
			return
		default:
		}
		// Re-read content (and file list/diffs in git mode) so next fetch returns updated data
		sess.RefreshFileContent()
		if sess.Mode == "git" {
			sess.RefreshFileList()
			sess.RefreshDiffs()
		}
		sess.notify(SSEEvent{Type: "comments-changed"})
	}
}

func (s *Server) authTokenSnapshot() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.authToken
}

func (s *Server) authLoggedIn() bool {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.authToken != ""
}

func (s *Server) authUserID() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.cfg.AuthUserID
}

func (s *Server) authUserName() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.cfg.AuthUserName
}

func (s *Server) authUserEmail() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.cfg.AuthUserEmail
}

func (s *Server) clearAuthState() {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	s.authToken = ""
	s.cfg.AuthToken = ""
	s.cfg.AuthUserID = ""
	s.cfg.AuthUserName = ""
	s.cfg.AuthUserEmail = ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}
