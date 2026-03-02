package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Comment represents a single inline review comment.
type Comment struct {
	ID              string `json:"id"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	Side            string `json:"side,omitempty"`
	Body            string `json:"body"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Resolved        bool   `json:"resolved,omitempty"`
	ResolutionNote  string `json:"resolution_note,omitempty"`
	ResolutionLines []int  `json:"resolution_lines,omitempty"`
	CarriedForward  bool   `json:"carried_forward,omitempty"`
}

// SSEEvent is sent to the browser via server-sent events.
type SSEEvent struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// FileEntry holds the state for a single file in a review session.
type FileEntry struct {
	Path     string    `json:"path"`      // relative (e.g., "auth/middleware.go")
	AbsPath  string    `json:"-"`         // absolute on disk
	Status   string    `json:"status"`    // "added", "modified", "deleted", "untracked"
	FileType string    `json:"file_type"` // "markdown" or "code"
	Content  string    `json:"-"`         // current file content
	FileHash string    `json:"-"`         // sha256 hash of content
	Comments []Comment `json:"-"`         // this file's comments
	nextID   int

	// Diff hunks for code files (from git diff)
	DiffHunks []DiffHunk `json:"-"`

	// Multi-round (markdown files only)
	PreviousContent  string    `json:"-"`
	PreviousComments []Comment `json:"-"`
}

// Session is the top-level state manager for a multi-file review.
type Session struct {
	Files       []*FileEntry
	Mode        string // "files" (explicit markdown files) or "git" (auto-detected from git)
	Branch      string
	BaseRef     string
	RepoRoot    string
	OutputDir   string // directory for .crit.json
	ReviewRound int

	mu             sync.RWMutex
	subscribers    map[chan SSEEvent]struct{}
	subMu          sync.Mutex
	writeTimer     *time.Timer
	writeGen       int
	sharedURL      string
	deleteToken    string
	status         *Status
	roundComplete  chan struct{}
	pendingEdits   int
	lastRoundEdits int
}

// CritJSON is the on-disk format for .crit.json.
type CritJSON struct {
	Branch      string                  `json:"branch"`
	BaseRef     string                  `json:"base_ref"`
	UpdatedAt   string                  `json:"updated_at"`
	ReviewRound int                     `json:"review_round"`
	ShareURL    string                  `json:"share_url,omitempty"`
	DeleteToken string                  `json:"delete_token,omitempty"`
	Files       map[string]CritJSONFile `json:"files"`
}

// CritJSONFile is the per-file section in .crit.json.
type CritJSONFile struct {
	Status   string    `json:"status"`
	FileHash string    `json:"file_hash"`
	Comments []Comment `json:"comments"`
}

// NewSessionFromGit creates a session by auto-detecting changed files via git.
func NewSessionFromGit() (*Session, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	changes, err := ChangedFiles()
	if err != nil {
		return nil, fmt.Errorf("detecting changes: %w", err)
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("no changed files detected")
	}

	branch := CurrentBranch()
	baseRef := ""
	if !IsOnDefaultBranch() {
		baseRef, _ = MergeBase(DefaultBranch())
	}

	s := &Session{
		Mode:          "git",
		Branch:        branch,
		BaseRef:       baseRef,
		RepoRoot:      root,
		OutputDir:     root,
		ReviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	for _, fc := range changes {
		absPath := filepath.Join(root, fc.Path)
		fe := &FileEntry{
			Path:    fc.Path,
			AbsPath: absPath,
			Status:  fc.Status,
			nextID:  1,
		}
		fe.FileType = detectFileType(fc.Path)

		// Read content (skip for deleted files)
		if fc.Status != "deleted" {
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue // skip files that can't be read
			}
			fe.Content = string(data)
			fe.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}

		// Load diff hunks for all files in git mode
		if fc.Status != "deleted" {
			if fc.Status == "added" || fc.Status == "untracked" {
				// Untracked/added files: show entire content as added
				fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
			} else {
				ref := baseRef
				if ref == "" {
					ref = "HEAD"
				}
				hunks, err := FileDiffUnified(fc.Path, ref)
				if err == nil {
					fe.DiffHunks = hunks
				}
			}
		}

		fe.Comments = []Comment{}
		s.Files = append(s.Files, fe)
	}

	s.loadCritJSON()
	return s, nil
}

// NewSessionFromFiles creates a session from explicitly provided file or directory paths.
// When a directory is passed, all files within it are included recursively.
func NewSessionFromFiles(paths []string) (*Session, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files provided")
	}

	// Expand directories into individual file paths
	var expandedPaths []string
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolving path %s: %w", p, err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("file not found: %s", p)
		}
		if info.IsDir() {
			dirFiles, err := walkDirectory(absPath)
			if err != nil {
				return nil, fmt.Errorf("walking directory %s: %w", p, err)
			}
			expandedPaths = append(expandedPaths, dirFiles...)
		} else {
			expandedPaths = append(expandedPaths, absPath)
		}
	}

	// Deduplicate paths
	seen := make(map[string]bool, len(expandedPaths))
	unique := expandedPaths[:0]
	for _, p := range expandedPaths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	expandedPaths = unique

	if len(expandedPaths) == 0 {
		return nil, fmt.Errorf("no files found")
	}

	// Determine output dir and repo root
	outputDir := filepath.Dir(expandedPaths[0])

	root := ""
	branch := ""
	baseRef := ""
	if IsGitRepo() {
		root, _ = RepoRoot()
		branch = CurrentBranch()
		if !IsOnDefaultBranch() {
			baseRef, _ = MergeBase(DefaultBranch())
		}
	}
	if root == "" {
		root = outputDir
	}

	s := &Session{
		Mode:          "files",
		Branch:        branch,
		BaseRef:       baseRef,
		RepoRoot:      root,
		OutputDir:     root,
		ReviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	for _, absPath := range expandedPaths {
		relPath := absPath
		if root != "" {
			if rel, err := filepath.Rel(root, absPath); err == nil {
				relPath = rel
			}
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", absPath, err)
		}

		fe := &FileEntry{
			Path:     relPath,
			AbsPath:  absPath,
			Status:   "modified",
			FileType: detectFileType(absPath),
			Content:  string(data),
			FileHash: fmt.Sprintf("sha256:%x", sha256.Sum256(data)),
			Comments: []Comment{},
			nextID:   1,
		}

		// Load diff hunks in a git repo
		if IsGitRepo() {
			ref := baseRef
			if ref == "" {
				ref = "HEAD"
			}
			hunks, err := FileDiffUnified(relPath, ref)
			if err == nil {
				fe.DiffHunks = hunks
			}
		}

		s.Files = append(s.Files, fe)
	}

	s.loadCritJSON()
	return s, nil
}

// walkDirectory recursively walks a directory and returns all file paths,
// skipping hidden directories and common non-text directories.
func walkDirectory(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip files we can't access
		}
		name := d.Name()

		// Skip hidden directories and common non-text directories
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Skip minified files
		lowerName := strings.ToLower(name)
		if strings.HasSuffix(lowerName, ".min.js") || strings.HasSuffix(lowerName, ".min.css") {
			return nil
		}

		// Skip binary/non-reviewable files by extension
		ext := strings.ToLower(filepath.Ext(name))
		if isBinaryExtension(ext) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	return files, err
}

// isBinaryExtension returns true for file extensions that are typically binary.
func isBinaryExtension(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp", ".svg",
		".mp3", ".mp4", ".wav", ".avi", ".mov", ".mkv",
		".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".exe", ".dll", ".so", ".dylib", ".bin",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".pyc", ".class", ".o", ".a":
		return true
	}
	return false
}

// detectFileType returns "markdown" for .md files, "code" for everything else.
func detectFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".md" || ext == ".markdown" || ext == ".mdown" {
		return "markdown"
	}
	return "code"
}

// FileByPath returns the FileEntry for a given relative path, or nil.
func (s *Session) FileByPath(path string) *FileEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, f := range s.Files {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// AddComment adds a comment to a specific file.
func (s *Session) AddComment(filePath string, startLine, endLine int, side, body string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:        fmt.Sprintf("c%d", f.nextID),
		StartLine: startLine,
		EndLine:   endLine,
		Side:      side,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.nextID++
	f.Comments = append(f.Comments, c)
	s.scheduleWrite()
	return c, true
}

// UpdateComment updates a comment in a specific file.
func (s *Session) UpdateComment(filePath, id, body string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	for i, c := range f.Comments {
		if c.ID == id {
			f.Comments[i].Body = body
			f.Comments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			s.scheduleWrite()
			return f.Comments[i], true
		}
	}
	return Comment{}, false
}

// DeleteComment deletes a comment from a specific file.
func (s *Session) DeleteComment(filePath, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return false
	}
	for i, c := range f.Comments {
		if c.ID == id {
			f.Comments = append(f.Comments[:i], f.Comments[i+1:]...)
			s.scheduleWrite()
			return true
		}
	}
	return false
}

// GetComments returns comments for a specific file.
func (s *Session) GetComments(filePath string) []Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return []Comment{}
	}
	result := make([]Comment, len(f.Comments))
	copy(result, f.Comments)
	return result
}

// GetAllComments returns all comments grouped by file path.
func (s *Session) GetAllComments() map[string][]Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string][]Comment)
	for _, f := range s.Files {
		if len(f.Comments) > 0 {
			comments := make([]Comment, len(f.Comments))
			copy(comments, f.Comments)
			result[f.Path] = comments
		}
	}
	return result
}

// TotalCommentCount returns the total number of comments across all files.
func (s *Session) TotalCommentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, f := range s.Files {
		total += len(f.Comments)
	}
	return total
}

func (s *Session) fileByPathLocked(path string) *FileEntry {
	for _, f := range s.Files {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// GetSharedURL returns the stored share URL.
func (s *Session) GetSharedURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sharedURL
}

// SetSharedURLAndToken atomically updates both the shared URL and delete token.
func (s *Session) SetSharedURLAndToken(url, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sharedURL = url
	s.deleteToken = token
	s.scheduleWrite()
}

// GetDeleteToken returns the stored delete token.
func (s *Session) GetDeleteToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deleteToken
}

// GetReviewRound returns the current review round.
func (s *Session) GetReviewRound() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ReviewRound
}

// IncrementEdits increments the pending edit counter.
func (s *Session) IncrementEdits() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingEdits++
}

// GetPendingEdits returns the pending edit count.
func (s *Session) GetPendingEdits() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingEdits
}

// GetLastRoundEdits returns the edit count from the last round.
func (s *Session) GetLastRoundEdits() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRoundEdits
}

// SignalRoundComplete transitions to a new review round.
func (s *Session) SignalRoundComplete() {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.lastRoundEdits = s.pendingEdits
	s.pendingEdits = 0
	s.ReviewRound++
	// Clear comments on all files, reset IDs
	for _, f := range s.Files {
		f.Comments = []Comment{}
		f.nextID = 1
	}
	s.mu.Unlock()
	select {
	case s.roundComplete <- struct{}{}:
	default:
	}
}

// RoundCompleteChan returns the channel signaled on round completion.
func (s *Session) RoundCompleteChan() <-chan struct{} {
	return s.roundComplete
}

// scheduleWrite debounces writes to disk.
func (s *Session) scheduleWrite() {
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	gen := s.writeGen
	s.writeTimer = time.AfterFunc(200*time.Millisecond, func() {
		s.mu.RLock()
		if s.writeGen != gen {
			s.mu.RUnlock()
			return
		}
		s.mu.RUnlock()
		s.WriteFiles()
	})
}

// critJSONPath returns the path to the .crit.json file.
func (s *Session) critJSONPath() string {
	return filepath.Join(s.OutputDir, ".crit.json")
}

// WriteFiles writes the .crit.json file to disk.
func (s *Session) WriteFiles() {
	s.mu.RLock()
	cj := CritJSON{
		Branch:      s.Branch,
		BaseRef:     s.BaseRef,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReviewRound: s.ReviewRound,
		ShareURL:    s.sharedURL,
		DeleteToken: s.deleteToken,
		Files:       make(map[string]CritJSONFile),
	}
	for _, f := range s.Files {
		if len(f.Comments) == 0 {
			continue
		}
		comments := make([]Comment, len(f.Comments))
		copy(comments, f.Comments)
		cj.Files[f.Path] = CritJSONFile{
			Status:   f.Status,
			FileHash: f.FileHash,
			Comments: comments,
		}
	}
	s.mu.RUnlock()

	// Only write if there's meaningful content; remove stale file otherwise
	if len(cj.Files) == 0 && cj.ShareURL == "" && cj.DeleteToken == "" {
		os.Remove(s.critJSONPath())
		return
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling .crit.json: %v\n", err)
		return
	}
	if err := os.WriteFile(s.critJSONPath(), data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .crit.json: %v\n", err)
	}
}

// loadCritJSON loads comments and share state from an existing .crit.json.
func (s *Session) loadCritJSON() {
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}

	s.sharedURL = cj.ShareURL
	s.deleteToken = cj.DeleteToken

	// Restore comments for files that match by path and hash
	for _, f := range s.Files {
		if cf, ok := cj.Files[f.Path]; ok {
			if cf.FileHash == f.FileHash {
				f.Comments = cf.Comments
				for _, c := range f.Comments {
					id := 0
					_, _ = fmt.Sscanf(c.ID, "c%d", &id)
					if id >= f.nextID {
						f.nextID = id + 1
					}
				}
			}
		}
	}
}

// SSE subscriber management

// Subscribe registers a new SSE subscriber.
func (s *Session) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 4)
	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()
	return ch
}

// Unsubscribe removes an SSE subscriber.
func (s *Session) Unsubscribe(ch chan SSEEvent) {
	s.subMu.Lock()
	delete(s.subscribers, ch)
	s.subMu.Unlock()
	close(ch)
}

func (s *Session) notify(event SSEEvent) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// Shutdown sends a server-shutdown event to all SSE subscribers.
func (s *Session) Shutdown() {
	s.notify(SSEEvent{Type: "server-shutdown"})
}

// RefreshDiffs re-computes diff hunks for all files.
func (s *Session) RefreshDiffs() {
	// Snapshot file list and baseRef under read lock
	s.mu.RLock()
	type fileSnapshot struct {
		entry   *FileEntry
		path    string
		status  string
		content string
	}
	snapshots := make([]fileSnapshot, 0, len(s.Files))
	for _, f := range s.Files {
		if f.Status == "deleted" {
			continue
		}
		snapshots = append(snapshots, fileSnapshot{
			entry:   f,
			path:    f.Path,
			status:  f.Status,
			content: f.Content,
		})
	}
	baseRef := s.BaseRef
	s.mu.RUnlock()

	// Compute diffs without holding any lock
	type diffResult struct {
		entry *FileEntry
		hunks []DiffHunk
	}
	results := make([]diffResult, 0, len(snapshots))
	for _, snap := range snapshots {
		var hunks []DiffHunk
		if snap.status == "added" || snap.status == "untracked" {
			hunks = FileDiffUnifiedNewFile(snap.content)
		} else {
			ref := baseRef
			if ref == "" {
				ref = "HEAD"
			}
			h, err := FileDiffUnified(snap.path, ref)
			if err == nil {
				hunks = h
			}
		}
		results = append(results, diffResult{entry: snap.entry, hunks: hunks})
	}

	// Assign results under write lock
	s.mu.Lock()
	for _, r := range results {
		r.entry.DiffHunks = r.hunks
	}
	s.mu.Unlock()
}

// RefreshFileList re-runs ChangedFiles and updates the session's file list.
// New files are added, removed files are dropped.
func (s *Session) RefreshFileList() {
	// ChangedFiles shells out to git — no lock needed
	changes, err := ChangedFiles()
	if err != nil {
		return
	}

	// Snapshot existing files under read lock
	s.mu.RLock()
	existing := make(map[string]*FileEntry, len(s.Files))
	for _, f := range s.Files {
		existing[f.Path] = f
	}
	repoRoot := s.RepoRoot
	s.mu.RUnlock()

	// Build new file list, doing I/O (os.ReadFile, sha256) without holding the lock
	var newFiles []*FileEntry
	for _, fc := range changes {
		if f, ok := existing[fc.Path]; ok {
			f.Status = fc.Status
			newFiles = append(newFiles, f)
		} else {
			absPath := filepath.Join(repoRoot, fc.Path)
			fe := &FileEntry{
				Path:     fc.Path,
				AbsPath:  absPath,
				Status:   fc.Status,
				FileType: detectFileType(fc.Path),
				Comments: []Comment{},
				nextID:   1,
			}
			if fc.Status != "deleted" {
				if data, err := os.ReadFile(absPath); err == nil {
					fe.Content = string(data)
					fe.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
				}
			}
			newFiles = append(newFiles, fe)
		}
	}

	// Assign under write lock
	s.mu.Lock()
	s.Files = newFiles
	s.mu.Unlock()
}

// Watch dispatches to the appropriate file-watching strategy based on session mode.
func (s *Session) Watch(stop <-chan struct{}) {
	if s.Mode == "git" {
		s.watchGit(stop)
	} else {
		s.watchFileMtimes(stop)
	}
}

// watchGit polls `git status --porcelain` for working tree changes.
// Used in git mode (no-args invocation).
func (s *Session) watchGit(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastFP := WorkingTreeFingerprint()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			fp := WorkingTreeFingerprint()
			if fp == lastFP {
				continue
			}
			lastFP = fp

			s.IncrementEdits()
			s.notify(SSEEvent{
				Type:    "edit-detected",
				Content: fmt.Sprintf("%d", s.GetPendingEdits()),
			})
		case <-s.roundComplete:
			s.handleRoundCompleteGit()
		}
	}
}

// watchFileMtimes polls individual file mtimes for changes.
// Used in files mode (explicit file args).
func (s *Session) watchFileMtimes(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track last mod times per file
	lastMod := make(map[string]time.Time)

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.mu.RLock()
			files := make([]*FileEntry, len(s.Files))
			copy(files, s.Files)
			s.mu.RUnlock()

			changed := false
			for _, f := range files {
				info, err := os.Stat(f.AbsPath)
				if err != nil {
					continue
				}
				modTime := info.ModTime()
				if modTime.Equal(lastMod[f.Path]) {
					continue
				}
				lastMod[f.Path] = modTime

				data, err := os.ReadFile(f.AbsPath)
				if err != nil {
					continue
				}
				hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))

				s.mu.RLock()
				same := hash == f.FileHash
				s.mu.RUnlock()

				if same {
					continue
				}

				s.mu.Lock()
				// Snapshot on first edit of a round (markdown files)
				if f.FileType == "markdown" && s.pendingEdits == 0 {
					f.PreviousContent = f.Content
					f.PreviousComments = make([]Comment, len(f.Comments))
					copy(f.PreviousComments, f.Comments)
				}
				f.Content = string(data)
				f.FileHash = hash
				f.Comments = []Comment{}
				f.nextID = 1
				s.mu.Unlock()
				changed = true
			}

			if changed {
				s.IncrementEdits()
				s.notify(SSEEvent{
					Type:    "edit-detected",
					Content: fmt.Sprintf("%d", s.GetPendingEdits()),
				})
			}
		case <-s.roundComplete:
			s.handleRoundCompleteFiles()
		}
	}
}

// handleRoundCompleteGit handles round completion in git mode.
// Re-runs ChangedFiles, re-computes diffs, refreshes file list.
func (s *Session) handleRoundCompleteGit() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	// Load resolved comments from .crit.json
	s.loadResolvedComments()

	// Refresh file list (agent may have created/deleted files)
	s.RefreshFileList()

	// Re-read all file contents
	s.mu.Lock()
	for _, f := range s.Files {
		if f.Status == "deleted" {
			continue
		}
		if data, err := os.ReadFile(f.AbsPath); err == nil {
			f.Content = string(data)
			f.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}
	}
	// Carry forward all comments at original positions
	for _, f := range s.Files {
		now := time.Now().UTC().Format(time.RFC3339)
		for _, c := range f.PreviousComments {
			carried := Comment{
				ID:              fmt.Sprintf("c%d", f.nextID),
				StartLine:       c.StartLine,
				EndLine:         c.EndLine,
				Side:            c.Side,
				Body:            c.Body,
				CreatedAt:       c.CreatedAt,
				UpdatedAt:       now,
				Resolved:        c.Resolved,
				ResolutionNote:  c.ResolutionNote,
				ResolutionLines: c.ResolutionLines,
				CarriedForward:  true,
			}
			f.nextID++
			f.Comments = append(f.Comments, carried)
		}
	}
	s.mu.Unlock()

	// Refresh diffs for all files
	s.RefreshDiffs()

	s.emitRoundStatus(edits)
	s.notify(SSEEvent{
		Type:    "file-changed",
		Content: "session",
	})
}

// handleRoundCompleteFiles handles round completion in files mode.
// Re-reads files, carries forward unresolved comments.
func (s *Session) handleRoundCompleteFiles() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	// Load resolved comments from .crit.json
	s.loadResolvedComments()
	s.carryForwardComments()

	// Re-read all file contents and update hashes
	s.mu.Lock()
	for _, f := range s.Files {
		if data, err := os.ReadFile(f.AbsPath); err == nil {
			f.Content = string(data)
			f.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}
	}
	s.mu.Unlock()

	s.emitRoundStatus(edits)
	s.notify(SSEEvent{
		Type:    "file-changed",
		Content: "session",
	})
}

// emitRoundStatus prints terminal status for a completed round.
func (s *Session) emitRoundStatus(edits int) {
	if s.status == nil {
		return
	}
	s.mu.RLock()
	round := s.ReviewRound
	resolved, open := 0, 0
	for _, f := range s.Files {
		for _, c := range f.PreviousComments {
			if c.Resolved {
				resolved++
			} else {
				open++
			}
		}
	}
	s.mu.RUnlock()
	s.status.FileUpdated(edits)
	s.status.RoundReady(round, resolved, open)
}

// loadResolvedComments reads .crit.json to pick up resolved fields the agent wrote.
func (s *Session) loadResolvedComments() {
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		// No .crit.json — clear all PreviousComments
		s.mu.Lock()
		for _, f := range s.Files {
			f.PreviousComments = nil
		}
		s.mu.Unlock()
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.Files {
		if cf, ok := cj.Files[f.Path]; ok {
			f.PreviousComments = cf.Comments
		} else {
			f.PreviousComments = nil
		}
	}
}

// carryForwardComments maps comments from the previous round
// to the new document positions for markdown files.
func (s *Session) carryForwardComments() {
	s.mu.RLock()
	var toProcess []*FileEntry
	for _, f := range s.Files {
		if f.FileType == "markdown" && f.PreviousContent != "" {
			toProcess = append(toProcess, f)
		}
	}
	s.mu.RUnlock()

	for _, f := range toProcess {
		s.mu.RLock()
		prevContent := f.PreviousContent
		currContent := f.Content
		prevComments := make([]Comment, len(f.PreviousComments))
		copy(prevComments, f.PreviousComments)
		s.mu.RUnlock()

		if len(prevComments) == 0 {
			continue
		}

		entries := ComputeLineDiff(prevContent, currContent)
		lineMap := MapOldLineToNew(entries)

		newLineCount := len(splitLines(currContent))
		if newLineCount == 0 {
			newLineCount = 1
		}

		s.mu.Lock()
		now := time.Now().UTC().Format(time.RFC3339)
		for _, c := range prevComments {
			newStart := lineMap[c.StartLine]
			newEnd := lineMap[c.EndLine]
			if newStart == 0 {
				newStart = c.StartLine
			}
			if newEnd == 0 {
				newEnd = c.EndLine
			}
			if newStart > newLineCount {
				newStart = newLineCount
			}
			if newEnd > newLineCount {
				newEnd = newLineCount
			}
			if newStart < 1 {
				newStart = 1
			}
			if newEnd < newStart {
				newEnd = newStart
			}
			carried := Comment{
				ID:              fmt.Sprintf("c%d", f.nextID),
				StartLine:       newStart,
				EndLine:         newEnd,
				Body:            c.Body,
				CreatedAt:       c.CreatedAt,
				UpdatedAt:       now,
				Resolved:        c.Resolved,
				ResolutionNote:  c.ResolutionNote,
				ResolutionLines: c.ResolutionLines,
				CarriedForward:  true,
			}
			f.nextID++
			f.Comments = append(f.Comments, carried)
		}
		s.mu.Unlock()
	}
}

// GetFileSnapshot returns a JSON-ready map for the /api/file endpoint.
func (s *Session) GetFileSnapshot(path string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(path)
	if f == nil {
		return nil, false
	}
	return map[string]any{
		"path":      f.Path,
		"status":    f.Status,
		"file_type": f.FileType,
		"content":   f.Content,
	}, true
}

// GetFileDiffSnapshot returns diff data for the /api/file/diff endpoint.
func (s *Session) GetFileDiffSnapshot(path string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(path)
	if f == nil {
		return nil, false
	}

	if f.FileType == "code" || s.Mode == "git" {
		hunks := f.DiffHunks
		if hunks == nil {
			hunks = []DiffHunk{}
		}
		return map[string]any{"hunks": hunks}, true
	}

	// Markdown in files mode: inter-round LCS diff
	var hunks []DiffHunk
	if f.PreviousContent != "" {
		entries := ComputeLineDiff(f.PreviousContent, f.Content)
		hunks = DiffEntriesToHunks(entries)
	}
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}

// GetFileContent returns the content for a specific file path.
func (s *Session) GetFileContent(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(path)
	if f == nil {
		return "", false
	}
	return f.Content, true
}

// GetFileDiffHunks returns the diff hunks for a specific file.
func (s *Session) GetFileDiffHunks(path string) ([]DiffHunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(path)
	if f == nil {
		return nil, false
	}
	return f.DiffHunks, true
}

// SessionInfo returns metadata about the session for the API.
type SessionInfo struct {
	Mode        string            `json:"mode"` // "files" or "git"
	Branch      string            `json:"branch"`
	BaseRef     string            `json:"base_ref"`
	ReviewRound int               `json:"review_round"`
	Files       []SessionFileInfo `json:"files"`
}

// SessionFileInfo is a summary of a file for the session API response.
type SessionFileInfo struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	FileType     string `json:"file_type"`
	CommentCount int    `json:"comment_count"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
}

// GetSessionInfo returns a snapshot of session metadata.
func (s *Session) GetSessionInfo() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info := SessionInfo{
		Mode:        s.Mode,
		Branch:      s.Branch,
		BaseRef:     s.BaseRef,
		ReviewRound: s.ReviewRound,
	}

	for _, f := range s.Files {
		fi := SessionFileInfo{
			Path:         f.Path,
			Status:       f.Status,
			FileType:     f.FileType,
			CommentCount: len(f.Comments),
		}
		// Count additions/deletions from diff hunks
		for _, h := range f.DiffHunks {
			for _, l := range h.Lines {
				switch l.Type {
				case "add":
					fi.Additions++
				case "del":
					fi.Deletions++
				}
			}
		}
		info.Files = append(info.Files, fi)
	}
	return info
}
