package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// fileHash returns a stable hash string for file content.
func fileHash(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

// Reply represents a single reply in a comment thread.
type Reply struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"created_at"`
	GitHubID  int64  `json:"github_id,omitempty"`
}

// Comment represents a single inline review comment.
type Comment struct {
	ID             string  `json:"id"`
	StartLine      int     `json:"start_line"`
	EndLine        int     `json:"end_line"`
	Side           string  `json:"side,omitempty"`
	Body           string  `json:"body"`
	Quote          string  `json:"quote,omitempty"`
	QuoteOffset    *int    `json:"quote_offset,omitempty"`
	Author         string  `json:"author,omitempty"`
	Scope          string  `json:"scope,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Resolved       bool    `json:"resolved,omitempty"`
	CarriedForward bool    `json:"carried_forward,omitempty"`
	ReviewRound    int     `json:"review_round,omitempty"`
	Replies        []Reply `json:"replies,omitempty"`
	GitHubID       int64   `json:"github_id,omitempty"`
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

	// Diff hunks for code files (from git diff)
	DiffHunks []DiffHunk `json:"-"`

	// Multi-round (markdown files only)
	PreviousContent  string    `json:"-"`
	PreviousComments []Comment `json:"-"`
}

// Session is the top-level state manager for a multi-file review.
type Session struct {
	Files          []*FileEntry
	Mode           string   // "files" (explicit markdown files) or "git" (auto-detected from git)
	CLIArgs        []string // original file arguments passed on the command line (empty for git mode)
	Branch         string
	BaseRef        string
	RepoRoot       string
	OutputDir      string // custom output directory for .crit.json (empty = RepoRoot)
	ReviewRound    int
	IgnorePatterns []string

	reviewComments []Comment
	reviewNextID   int

	mu                  sync.RWMutex
	nextID              int // session-global comment ID counter (c1, c2, c3... across ALL files)
	subscribers         map[chan SSEEvent]struct{}
	subMu               sync.Mutex
	writeTimer          *time.Timer
	writeGen            int
	pendingWrite        bool
	sharedURL           string
	deleteToken         string
	shareScope          string
	status              *Status
	roundComplete       chan struct{}
	pendingEdits        int
	lastRoundEdits      int
	lastCritJSONMtime   time.Time // mtime after our last WriteFiles(); used to detect external changes
	awaitingFirstReview bool      // true until first review-cycle completes
	browserClients      int32     // number of connected SSE browser clients (atomic)
}

// CritJSON is the on-disk format for .crit.json.
type CritJSON struct {
	Branch         string                  `json:"branch"`
	BaseRef        string                  `json:"base_ref"`
	UpdatedAt      string                  `json:"updated_at"`
	ReviewRound    int                     `json:"review_round"`
	ShareURL       string                  `json:"share_url,omitempty"`
	DeleteToken    string                  `json:"delete_token,omitempty"`
	ShareScope     string                  `json:"share_scope,omitempty"`
	ReviewComments []Comment               `json:"review_comments,omitempty"`
	Files          map[string]CritJSONFile `json:"files"`
}

// CritJSONFile is the per-file section in .crit.json.
type CritJSONFile struct {
	Status   string    `json:"status"`
	FileHash string    `json:"file_hash"`
	Comments []Comment `json:"comments"`
}

// NewSessionFromGit creates a session by auto-detecting changed files via git.
// The base branch is read from DefaultBranch(), which respects the package-level
// defaultBranchOverride set by resolveServerConfig() when --base-branch is given.
// We use the global rather than a parameter so that RefreshFileList() during
// multi-round reviews picks up the same override automatically.
func NewSessionFromGit(ignorePatterns []string) (*Session, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	// Compute baseRef FIRST so we use the same value for both file detection and diffs.
	// Previously these were computed independently which could lead to inconsistencies.
	branch := CurrentBranch()
	resolvedBase := DefaultBranch()
	baseRef := ""
	if branch != resolvedBase {
		baseRef, _ = MergeBase(resolvedBase)
	}

	var changes []FileChange
	if baseRef != "" {
		// Feature branch with valid merge base: diff against it
		changes, err = changedFilesFromBaseInDir(baseRef, root)
	} else {
		// Default branch or merge-base unavailable: diff against HEAD
		changes, err = changedFilesOnDefaultInDir(root)
	}
	if err != nil {
		return nil, fmt.Errorf("detecting changes: %w", err)
	}
	// Apply ignore patterns
	changes = filterIgnored(changes, ignorePatterns)

	if len(changes) == 0 {
		return nil, fmt.Errorf("no changed files detected (after applying ignore patterns)")
	}

	s := &Session{
		Mode:                "git",
		Branch:              branch,
		BaseRef:             baseRef,
		RepoRoot:            root,
		ReviewRound:         1,
		nextID:              1,
		IgnorePatterns:      ignorePatterns,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		awaitingFirstReview: true,
	}

	for _, fc := range changes {
		absPath := filepath.Join(root, fc.Path)
		fe := &FileEntry{
			Path:    fc.Path,
			AbsPath: absPath,
			Status:  fc.Status,
		}
		fe.FileType = detectFileType(fc.Path)

		// Read content (skip for deleted files)
		if fc.Status != "deleted" {
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue // skip files that can't be read
			}
			fe.Content = string(data)
			fe.FileHash = fileHash(data)
		}

		// Load diff hunks for all files in git mode.
		// Run git diff from the repo root to ensure path consistency.
		if fc.Status != "deleted" {
			if fc.Status == "added" || fc.Status == "untracked" {
				fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
			} else {
				hunks, err := fileDiffUnified(fc.Path, baseRef, root)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", fc.Path, err)
				} else {
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
// The base branch is read from DefaultBranch(), which respects defaultBranchOverride
// set by resolveServerConfig(). See NewSessionFromGit for rationale.
func NewSessionFromFiles(paths []string, ignorePatterns []string) (*Session, error) {
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
			dirFiles, err := walkDirectory(absPath, ignorePatterns)
			if err != nil {
				return nil, fmt.Errorf("walking directory %s: %w", p, err)
			}
			expandedPaths = append(expandedPaths, dirFiles...)
		} else {
			// Explicit files are never filtered by ignore patterns
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
		resolvedBase := DefaultBranch()
		if branch != resolvedBase {
			baseRef, _ = MergeBase(resolvedBase)
		}
	}
	if root == "" {
		root = outputDir
	}

	s := &Session{
		Mode:                "files",
		Branch:              branch,
		BaseRef:             baseRef,
		RepoRoot:            root,
		ReviewRound:         1,
		nextID:              1,
		IgnorePatterns:      ignorePatterns,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		awaitingFirstReview: true,
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
			FileHash: fileHash(data),
			Comments: []Comment{},
		}

		// Load diff hunks in a git repo
		if IsGitRepo() {
			hunks, err := fileDiffUnified(relPath, baseRef, root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", relPath, err)
			} else {
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
func walkDirectory(dir string, ignorePatterns []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip files we can't access
		}
		name := d.Name()

		// Skip hidden directories and common non-text directories
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || skipDirs[name] {
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

		// Apply ignore patterns (use path relative to dir)
		if relPath, relErr := filepath.Rel(dir, path); relErr == nil {
			for _, pat := range ignorePatterns {
				if matchPattern(pat, relPath) {
					return nil
				}
			}
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
func (s *Session) AddComment(filePath string, startLine, endLine int, side, body, quote, author string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:          fmt.Sprintf("c%d", s.nextID),
		StartLine:   startLine,
		EndLine:     endLine,
		Side:        side,
		Body:        body,
		Quote:       quote,
		Author:      author,
		Scope:       "line",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
	s.nextID++
	f.Comments = append(f.Comments, c)
	s.scheduleWrite()
	return c, true
}

// AddFileComment adds a file-level comment (not tied to specific lines).
func (s *Session) AddFileComment(filePath, body, author string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:          fmt.Sprintf("c%d", s.nextID),
		Body:        body,
		Author:      author,
		Scope:       "file",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
	s.nextID++
	f.Comments = append(f.Comments, c)
	s.scheduleWrite()
	return c, true
}

// AddReviewComment adds a review-level comment (not tied to any file).
func (s *Session) AddReviewComment(body, author string) Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:          fmt.Sprintf("r%d", s.reviewNextID),
		Body:        body,
		Author:      author,
		Scope:       "review",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
	s.reviewNextID++
	s.reviewComments = append(s.reviewComments, c)
	s.scheduleWrite()
	return c
}

// GetReviewComments returns a copy of all review-level comments.
func (s *Session) GetReviewComments() []Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	comments := make([]Comment, len(s.reviewComments))
	copy(comments, s.reviewComments)
	return comments
}

// UpdateReviewComment updates a review-level comment by ID.
func (s *Session) UpdateReviewComment(id, body string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == id {
			s.reviewComments[i].Body = body
			s.reviewComments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			s.scheduleWrite()
			return s.reviewComments[i], true
		}
	}
	return Comment{}, false
}

// DeleteReviewComment deletes a review-level comment by ID.
func (s *Session) DeleteReviewComment(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == id {
			s.reviewComments = append(s.reviewComments[:i], s.reviewComments[i+1:]...)
			s.scheduleWrite()
			return true
		}
	}
	return false
}

// ResolveReviewComment sets or clears the resolved flag on a review-level comment.
func (s *Session) ResolveReviewComment(id string, resolved bool) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == id {
			s.reviewComments[i].Resolved = resolved
			s.reviewComments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			s.scheduleWrite()
			return s.reviewComments[i], true
		}
	}
	return Comment{}, false
}

// AddReviewCommentReply adds a reply to a review-level comment.
func (s *Session) AddReviewCommentReply(commentID, body, author string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == commentID {
			now := time.Now().UTC().Format(time.RFC3339)
			r := Reply{
				ID:        nextReplyID(commentID, c.Replies),
				Body:      body,
				Author:    author,
				CreatedAt: now,
			}
			s.reviewComments[i].Replies = append(s.reviewComments[i].Replies, r)
			s.reviewComments[i].Resolved = false
			s.reviewComments[i].UpdatedAt = now
			s.scheduleWrite()
			return r, true
		}
	}
	return Reply{}, false
}

// UpdateReviewCommentReply updates a reply's body on a review-level comment.
func (s *Session) UpdateReviewCommentReply(commentID, replyID, body string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == commentID {
			for j, r := range c.Replies {
				if r.ID == replyID {
					s.reviewComments[i].Replies[j].Body = body
					s.reviewComments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					s.scheduleWrite()
					return s.reviewComments[i].Replies[j], true
				}
			}
			return Reply{}, false
		}
	}
	return Reply{}, false
}

// DeleteReviewCommentReply removes a reply from a review-level comment.
func (s *Session) DeleteReviewCommentReply(commentID, replyID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == commentID {
			for j, r := range c.Replies {
				if r.ID == replyID {
					s.reviewComments[i].Replies = append(s.reviewComments[i].Replies[:j], s.reviewComments[i].Replies[j+1:]...)
					s.reviewComments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					s.scheduleWrite()
					return true
				}
			}
			return false
		}
	}
	return false
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

// SetCommentResolved sets or clears the resolved flag on a comment.
func (s *Session) SetCommentResolved(filePath, id string, resolved bool) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	for i, c := range f.Comments {
		if c.ID == id {
			f.Comments[i].Resolved = resolved
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

// nextReplyID generates the next reply ID for a comment, e.g. "c1-r1", "c1-r2".
// It finds the max existing reply number with the prefix and increments.
func nextReplyID(commentID string, existing []Reply) string {
	prefix := commentID + "-r"
	max := 0
	for _, r := range existing {
		if strings.HasPrefix(r.ID, prefix) {
			numStr := r.ID[len(prefix):]
			if n, err := strconv.Atoi(numStr); err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%s-r%d", commentID, max+1)
}

// AddReply adds a reply to a specific comment on a file.
func (s *Session) AddReply(filePath, commentID, body, author string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Reply{}, false
	}
	for i, c := range f.Comments {
		if c.ID == commentID {
			now := time.Now().UTC().Format(time.RFC3339)
			r := Reply{
				ID:        nextReplyID(commentID, c.Replies),
				Body:      body,
				Author:    author,
				CreatedAt: now,
			}
			f.Comments[i].Replies = append(f.Comments[i].Replies, r)
			f.Comments[i].Resolved = false
			f.Comments[i].UpdatedAt = now
			s.scheduleWrite()
			return r, true
		}
	}
	return Reply{}, false
}

// UpdateReply updates a reply's body on a specific comment.
func (s *Session) UpdateReply(filePath, commentID, replyID, body string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Reply{}, false
	}
	for i, c := range f.Comments {
		if c.ID == commentID {
			for j, r := range c.Replies {
				if r.ID == replyID {
					f.Comments[i].Replies[j].Body = body
					f.Comments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					s.scheduleWrite()
					return f.Comments[i].Replies[j], true
				}
			}
			return Reply{}, false
		}
	}
	return Reply{}, false
}

// DeleteReply removes a reply from a specific comment.
func (s *Session) DeleteReply(filePath, commentID, replyID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return false
	}
	for i, c := range f.Comments {
		if c.ID == commentID {
			for j, r := range c.Replies {
				if r.ID == replyID {
					f.Comments[i].Replies = append(f.Comments[i].Replies[:j], f.Comments[i].Replies[j+1:]...)
					f.Comments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					s.scheduleWrite()
					return true
				}
			}
			return false
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
	for i, c := range result {
		if len(c.Replies) > 0 {
			result[i].Replies = make([]Reply, len(c.Replies))
			copy(result[i].Replies, c.Replies)
		}
	}
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
			for i, c := range comments {
				if len(c.Replies) > 0 {
					comments[i].Replies = make([]Reply, len(c.Replies))
					copy(comments[i].Replies, c.Replies)
				}
			}
			result[f.Path] = comments
		}
	}
	return result
}

// TotalCommentCount returns the total number of comments across all files and review comments.
func (s *Session) TotalCommentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.reviewComments)
	for _, f := range s.Files {
		total += len(f.Comments)
	}
	return total
}

// NewCommentCount returns the number of new (non-carried-forward) comments across all files.
// Review comments are always counted as new (not carried forward).
func (s *Session) NewCommentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.reviewComments)
	for _, f := range s.Files {
		for _, c := range f.Comments {
			if !c.CarriedForward {
				total++
			}
		}
	}
	return total
}

// UnresolvedCommentCount returns the number of unresolved comments across all files and review comments.
func (s *Session) UnresolvedCommentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, c := range s.reviewComments {
		if !c.Resolved {
			total++
		}
	}
	for _, f := range s.Files {
		for _, c := range f.Comments {
			if !c.Resolved {
				total++
			}
		}
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

// EnsureFileEntry registers a file into the session if it doesn't already exist.
// This handles files that appear after startup (e.g. created by the user while
// reviewing). The file is read from disk and added with appropriate status and
// diff hunks so that comments and diff rendering work correctly.
// Returns true if the file was found (either already existed or was added).
func (s *Session) EnsureFileEntry(path string) bool {
	s.mu.RLock()
	if s.fileByPathLocked(path) != nil {
		s.mu.RUnlock()
		return true
	}
	repoRoot := s.RepoRoot
	baseRef := s.BaseRef
	s.mu.RUnlock()

	if repoRoot == "" {
		return false
	}

	absPath := filepath.Join(repoRoot, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}

	// Determine the file's git status
	status := "untracked"
	if changes, err := ChangedFiles(); err == nil {
		for _, fc := range changes {
			if fc.Path == path {
				status = fc.Status
				break
			}
		}
	}

	fe := &FileEntry{
		Path:     path,
		AbsPath:  absPath,
		Status:   status,
		FileType: detectFileType(path),
		Content:  string(data),
		FileHash: fileHash(data),
		Comments: []Comment{},
	}

	// Generate diff hunks
	if status == "added" || status == "untracked" {
		fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
	} else if status != "deleted" {
		if hunks, err := fileDiffUnified(path, baseRef, repoRoot); err == nil {
			fe.DiffHunks = hunks
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Double-check under write lock (another goroutine may have added it)
	if s.fileByPathLocked(path) != nil {
		return true
	}
	s.Files = append(s.Files, fe)
	return true
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

// SetShareScope stores the scope hash for the current share.
func (s *Session) SetShareScope(scope string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shareScope = scope
}

// GetShareScope returns the stored share scope hash.
func (s *Session) GetShareScope() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shareScope
}

// GetShareState returns the shared URL and delete token atomically.
func (s *Session) GetShareState() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sharedURL, s.deleteToken
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

// IsAwaitingFirstReview returns true if no review cycle has completed yet.
func (s *Session) IsAwaitingFirstReview() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.awaitingFirstReview
}

// SetAwaitingFirstReview sets the awaitingFirstReview flag.
func (s *Session) SetAwaitingFirstReview(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awaitingFirstReview = v
}

// SignalRoundComplete transitions to a new review round.
func (s *Session) SignalRoundComplete() {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.pendingWrite = false
	s.lastRoundEdits = s.pendingEdits
	s.pendingEdits = 0
	s.ReviewRound++
	// Clear comments on all files, reset session-global ID counter
	for _, f := range s.Files {
		f.Comments = []Comment{}
	}
	s.nextID = 1
	s.mu.Unlock()
	select {
	case s.roundComplete <- struct{}{}:
	default:
	}
}

// ClearAllComments removes all comments from all files and resets comment IDs and review round.
// Used by the E2E test cleanup endpoint to return the server to a clean initial state.
// It also removes .crit.json from s.Files and deletes it from disk so it does not appear
// as an untracked git file in subsequent requests.
func (s *Session) ClearAllComments() {
	s.mu.Lock()
	// Cancel any pending debounced write so it cannot recreate .crit.json after we delete it.
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	// Reset all file state and drop the .crit.json entry from the file list.
	filtered := make([]*FileEntry, 0, len(s.Files))
	for _, f := range s.Files {
		if filepath.Base(f.Path) == ".crit.json" {
			continue
		}
		f.Comments = []Comment{}
		f.PreviousComments = nil
		f.PreviousContent = ""
		filtered = append(filtered, f)
	}
	s.Files = filtered
	s.nextID = 1
	s.reviewComments = nil
	s.reviewNextID = 0
	s.ReviewRound = 1
	s.lastCritJSONMtime = time.Time{}
	s.pendingWrite = false
	critPath := s.critJSONPath()
	s.mu.Unlock()
	// Delete .crit.json from disk so it is no longer listed as an untracked git file.
	os.Remove(critPath) //nolint:errcheck
}

// RoundCompleteChan returns the channel signaled on round completion.
func (s *Session) RoundCompleteChan() <-chan struct{} {
	return s.roundComplete
}

// scheduleWrite debounces writes to disk.
func (s *Session) scheduleWrite() {
	s.pendingWrite = true
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
	dir := s.RepoRoot
	if s.OutputDir != "" {
		dir = s.OutputDir
	}
	return filepath.Join(dir, ".crit.json")
}

// writeFilesSnapshot holds all session state needed to write .crit.json,
// captured under lock so that disk I/O can happen without holding the lock.
type writeFilesSnapshot struct {
	critPath       string
	lastMtime      time.Time
	branch         string
	baseRef        string
	reviewRound    int
	sharedURL      string
	deleteToken    string
	shareScope     string
	reviewComments []Comment
	// Per-file data needed for the merge. We copy comments so the snapshot
	// is independent of later in-memory mutations.
	files []writeFileSnapshot
}

type writeFileSnapshot struct {
	path     string
	status   string
	fileHash string
	comments []Comment
}

// WriteFiles writes the .crit.json file to disk.
//
// The implementation snapshots all needed session state under RLock, then
// releases the lock before doing any disk I/O (ReadFile, Stat, WriteFile).
// This prevents a slow filesystem from blocking comment operations.
//
// Concurrency note: the debounce timer in scheduleWrite ensures that only one
// WriteFiles call is in-flight at a time for a given generation. Between the
// snapshot and the final WriteFile, no concurrent WriteFiles should be running
// because scheduleWrite cancels the previous timer before arming a new one.
func (s *Session) WriteFiles() {
	critPath := s.critJSONPath()

	// --- Phase 1: check for external deletion (needs brief lock) ---
	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if !lastMtime.IsZero() {
		if _, statErr := os.Stat(critPath); os.IsNotExist(statErr) {
			s.mu.Lock()
			s.lastCritJSONMtime = time.Time{}
			anyComments := false
			for _, f := range s.Files {
				if len(f.Comments) > 0 {
					f.Comments = []Comment{}
					anyComments = true
				}
			}
			s.nextID = 1
			s.mu.Unlock()
			if anyComments {
				s.notify(SSEEvent{Type: "comments-changed"})
			}
			return
		}
	}

	// --- Phase 2: snapshot session state under RLock ---
	snap := s.snapshotForWrite(critPath)

	// --- Phase 3: all disk I/O happens here, no lock held ---

	// Start from existing .crit.json to preserve comments for files not in this session
	// (e.g. comments added via `crit comment` on files outside the current diff).
	cj := CritJSON{Files: make(map[string]CritJSONFile)}
	if data, err := os.ReadFile(snap.critPath); err == nil {
		_ = json.Unmarshal(data, &cj)
		if cj.Files == nil {
			cj.Files = make(map[string]CritJSONFile)
		}
	}
	cj.Branch = snap.branch
	cj.BaseRef = snap.baseRef
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	cj.ReviewRound = snap.reviewRound
	cj.ShareURL = snap.sharedURL
	cj.DeleteToken = snap.deleteToken
	cj.ShareScope = snap.shareScope
	cj.ReviewComments = snap.reviewComments

	// Overlay session files: merge with disk comments, remove entries with no comments.
	for _, fs := range snap.files {
		diskFile, hasDisk := cj.Files[fs.path]

		// Build set of in-memory comment IDs
		memIDs := make(map[string]struct{}, len(fs.comments))
		for _, c := range fs.comments {
			memIDs[c.ID] = struct{}{}
		}

		// Start with in-memory comments (already a copy from the snapshot)
		merged := fs.comments

		// Merge in any disk-only comments (added externally via crit comment, crit pull, etc.)
		if hasDisk {
			for _, dc := range diskFile.Comments {
				if _, exists := memIDs[dc.ID]; !exists {
					merged = append(merged, dc)
				}
			}
		}

		if len(merged) == 0 {
			delete(cj.Files, fs.path)
			continue
		}

		cj.Files[fs.path] = CritJSONFile{
			Status:   fs.status,
			FileHash: fs.fileHash,
			Comments: merged,
		}
	}

	// Only remove if nothing meaningful remains
	if len(cj.Files) == 0 && len(cj.ReviewComments) == 0 && cj.ShareURL == "" && cj.DeleteToken == "" && cj.ShareScope == "" {
		os.Remove(snap.critPath)
		s.mu.Lock()
		s.lastCritJSONMtime = time.Time{}
		s.pendingWrite = false
		s.mu.Unlock()
		return
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling .crit.json: %v\n", err)
		return
	}
	if err := os.WriteFile(snap.critPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .crit.json: %v\n", err)
		return
	}
	// Record mtime so mergeExternalCritJSON can distinguish our writes from external ones.
	if info, err := os.Stat(snap.critPath); err == nil {
		s.mu.Lock()
		s.lastCritJSONMtime = info.ModTime()
		s.pendingWrite = false
		s.mu.Unlock()
	}
}

// snapshotForWrite captures all session state needed by WriteFiles under RLock.
// The returned snapshot owns its own copies of comment slices, so it is safe
// to use after the lock is released.
func (s *Session) snapshotForWrite(critPath string) writeFilesSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rc := make([]Comment, len(s.reviewComments))
	copy(rc, s.reviewComments)
	snap := writeFilesSnapshot{
		critPath:       critPath,
		lastMtime:      s.lastCritJSONMtime,
		branch:         s.Branch,
		baseRef:        s.BaseRef,
		reviewRound:    s.ReviewRound,
		sharedURL:      s.sharedURL,
		deleteToken:    s.deleteToken,
		shareScope:     s.shareScope,
		reviewComments: rc,
		files:          make([]writeFileSnapshot, len(s.Files)),
	}
	for i, f := range s.Files {
		comments := make([]Comment, len(f.Comments))
		copy(comments, f.Comments)
		snap.files[i] = writeFileSnapshot{
			path:     f.Path,
			status:   f.Status,
			fileHash: f.FileHash,
			comments: comments,
		}
	}
	return snap
}

// mergeExternalCritJSON checks if .crit.json was modified externally (not by us)
// and merges any new comments into the in-memory session.
// Returns true if changes were detected and merged.
func (s *Session) mergeExternalCritJSON() bool {
	critPath := s.critJSONPath()

	info, err := os.Stat(critPath)

	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if err != nil {
		// File doesn't exist. If we previously tracked it, it was externally deleted.
		if !lastMtime.IsZero() {
			s.mu.Lock()
			s.lastCritJSONMtime = time.Time{}
			anyComments := false
			for _, f := range s.Files {
				if len(f.Comments) > 0 {
					f.Comments = []Comment{}
					anyComments = true
				}
			}
			s.nextID = 1
			s.mu.Unlock()
			if anyComments {
				s.notify(SSEEvent{Type: "comments-changed"})
			}
		}
		return !lastMtime.IsZero()
	}

	// If mtime matches our last write, this is our own change — skip.
	if !lastMtime.IsZero() && info.ModTime().Equal(lastMtime) {
		return false
	}

	// If a debounced write is pending, skip the merge to avoid re-adding
	// comments that the user just deleted (race between delete + debounce).
	s.mu.RLock()
	pending := s.pendingWrite
	s.mu.RUnlock()
	if pending {
		return false
	}

	data, err := os.ReadFile(critPath)
	if err != nil {
		return false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return false
	}

	s.mu.Lock()
	s.lastCritJSONMtime = info.ModTime()

	changed := false

	for _, f := range s.Files {
		diskFile, hasDisk := cj.Files[f.Path]

		if !hasDisk {
			// File not in .crit.json — if we have comments, they were cleared externally.
			if len(f.Comments) > 0 {
				f.Comments = []Comment{}
				changed = true
			}
			continue
		}

		// Build set of in-memory comment IDs.
		memIDs := make(map[string]struct{}, len(f.Comments))
		for _, c := range f.Comments {
			memIDs[c.ID] = struct{}{}
		}

		// Merge in new comments from disk.
		for _, dc := range diskFile.Comments {
			if _, exists := memIDs[dc.ID]; !exists {
				f.Comments = append(f.Comments, dc)
				id := 0
				fmt.Sscanf(dc.ID, "c%d", &id)
				if id >= s.nextID {
					s.nextID = id + 1
				}
				changed = true
			} else {
				// Comment exists in memory — merge replies and resolved state from disk.
				for i, mc := range f.Comments {
					if mc.ID != dc.ID {
						continue
					}
					// Merge new replies from disk
					memReplyIDs := make(map[string]struct{}, len(mc.Replies))
					for _, r := range mc.Replies {
						memReplyIDs[r.ID] = struct{}{}
					}
					for _, dr := range dc.Replies {
						if _, exists := memReplyIDs[dr.ID]; !exists {
							f.Comments[i].Replies = append(f.Comments[i].Replies, dr)
							changed = true
						}
					}
					// Sync resolved state bidirectionally from disk
					if dc.Resolved != mc.Resolved {
						f.Comments[i].Resolved = dc.Resolved
						changed = true
					}
					break
				}
			}
		}

		// Check for comments removed on disk (e.g. crit comment --clear).
		// After the merge above, len(f.Comments) >= len(diskFile.Comments), so != means memory has disk-absent IDs.
		if len(diskFile.Comments) != len(f.Comments) {
			diskIDs := make(map[string]struct{}, len(diskFile.Comments))
			for _, dc := range diskFile.Comments {
				diskIDs[dc.ID] = struct{}{}
			}
			filtered := f.Comments[:0]
			for _, c := range f.Comments {
				if _, exists := diskIDs[c.ID]; exists {
					filtered = append(filtered, c)
				}
			}
			if len(filtered) != len(f.Comments) {
				f.Comments = filtered
				changed = true
			}
		}
	}

	// Merge review-level comments from disk
	memReviewIDs := make(map[string]struct{}, len(s.reviewComments))
	for _, c := range s.reviewComments {
		memReviewIDs[c.ID] = struct{}{}
	}
	for _, dc := range cj.ReviewComments {
		if _, exists := memReviewIDs[dc.ID]; !exists {
			s.reviewComments = append(s.reviewComments, dc)
			id := 0
			fmt.Sscanf(dc.ID, "r%d", &id)
			if id >= s.reviewNextID {
				s.reviewNextID = id + 1
			}
			changed = true
		} else {
			// Merge resolved state and replies from disk
			for i, mc := range s.reviewComments {
				if mc.ID != dc.ID {
					continue
				}
				if dc.Resolved != mc.Resolved {
					s.reviewComments[i].Resolved = dc.Resolved
					changed = true
				}
				memRIDs := make(map[string]struct{}, len(mc.Replies))
				for _, r := range mc.Replies {
					memRIDs[r.ID] = struct{}{}
				}
				for _, dr := range dc.Replies {
					if _, exists := memRIDs[dr.ID]; !exists {
						s.reviewComments[i].Replies = append(s.reviewComments[i].Replies, dr)
						changed = true
					}
				}
				break
			}
		}
	}
	// Remove review comments deleted on disk (always run filter — length check is unreliable
	// when adds and deletes happen in the same external edit)
	{
		diskRIDs := make(map[string]struct{}, len(cj.ReviewComments))
		for _, dc := range cj.ReviewComments {
			diskRIDs[dc.ID] = struct{}{}
		}
		filtered := s.reviewComments[:0]
		for _, c := range s.reviewComments {
			if _, exists := diskRIDs[c.ID]; exists {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) != len(s.reviewComments) {
			s.reviewComments = filtered
			changed = true
		}
	}
	s.mu.Unlock()

	if changed {
		s.notify(SSEEvent{Type: "comments-changed"})
	}

	return changed
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

	// Only restore share state if the file set matches what was shared.
	if cj.ShareScope != "" {
		paths := make([]string, 0, len(s.Files))
		for _, f := range s.Files {
			paths = append(paths, f.Path)
		}
		if shareScope(paths) == cj.ShareScope {
			s.sharedURL = cj.ShareURL
			s.deleteToken = cj.DeleteToken
			s.shareScope = cj.ShareScope
		}
	} else {
		// Legacy .crit.json without scope — load unconditionally for backwards compat.
		s.sharedURL = cj.ShareURL
		s.deleteToken = cj.DeleteToken
	}

	// Restore review round so the session continues from where it left off.
	if cj.ReviewRound > s.ReviewRound {
		s.ReviewRound = cj.ReviewRound
	}

	// Restore comments for files that match by path.
	// Scan ALL files' comments to find the global max ID for session-level nextID.
	for _, f := range s.Files {
		if cf, ok := cj.Files[f.Path]; ok {
			f.Comments = cf.Comments
			for i := range f.Comments {
				if f.Comments[i].Scope == "" {
					f.Comments[i].Scope = "line"
				}
				id := 0
				_, _ = fmt.Sscanf(f.Comments[i].ID, "c%d", &id)
				if id >= s.nextID {
					s.nextID = id + 1
				}
			}
		}
	}

	// Restore review-level comments and rebuild reviewNextID.
	s.reviewComments = cj.ReviewComments
	for _, c := range s.reviewComments {
		id := 0
		_, _ = fmt.Sscanf(c.ID, "r%d", &id)
		if id >= s.reviewNextID {
			s.reviewNextID = id + 1
		}
	}

	// Record the mtime so the first ticker tick doesn't re-process our own file.
	if info, err := os.Stat(s.critJSONPath()); err == nil {
		s.lastCritJSONMtime = info.ModTime()
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

// BrowserConnect increments the browser client count.
func (s *Session) BrowserConnect() {
	atomic.AddInt32(&s.browserClients, 1)
}

// BrowserDisconnect decrements the browser client count, clamping at zero.
func (s *Session) BrowserDisconnect() {
	if atomic.AddInt32(&s.browserClients, -1) < 0 {
		atomic.StoreInt32(&s.browserClients, 0)
	}
}

// HasBrowserClients returns true if any browser SSE clients are connected.
func (s *Session) HasBrowserClients() bool {
	return atomic.LoadInt32(&s.browserClients) > 0
}

// ReinvokeCommand returns the crit command the agent should run to trigger the next round.
// For file-mode sessions it includes the original file arguments; for git-mode it's bare "crit".
func (s *Session) ReinvokeCommand() string {
	if len(s.CLIArgs) == 0 {
		return "crit"
	}
	return "crit " + strings.Join(s.CLIArgs, " ")
}

// Shutdown sends a server-shutdown event to all SSE subscribers.
func (s *Session) Shutdown() {
	s.notify(SSEEvent{Type: "server-shutdown"})
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

// GetFileSnapshotFromDisk reads a file directly from the repo root.
// Used as a fallback when a scoped view references a file not in the session's file list
// (e.g. a file changed after crit started).
func (s *Session) GetFileSnapshotFromDisk(path string) (map[string]any, bool) {
	s.mu.RLock()
	repoRoot := s.RepoRoot
	s.mu.RUnlock()

	if repoRoot == "" {
		return nil, false
	}
	// Prevent path traversal
	absPath := filepath.Join(repoRoot, path)
	if !strings.HasPrefix(absPath, repoRoot+string(filepath.Separator)) && absPath != repoRoot {
		return nil, false
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, false
	}
	return map[string]any{
		"path":      path,
		"status":    "modified",
		"file_type": detectFileType(path),
		"content":   string(data),
	}, true
}

// GetFileDiffSnapshot returns diff data for the /api/file/diff endpoint.
func (s *Session) GetFileDiffSnapshot(path string) (map[string]any, bool) {
	s.mu.RLock()
	f := s.fileByPathLocked(path)
	if f == nil {
		s.mu.RUnlock()
		return nil, false
	}

	if f.FileType == "code" || s.Mode == "git" {
		hunks := f.DiffHunks
		s.mu.RUnlock()
		if hunks == nil {
			hunks = []DiffHunk{}
		}
		return map[string]any{"hunks": hunks}, true
	}

	// Markdown in files mode: snapshot content, then compute LCS diff outside the lock
	prevContent := f.PreviousContent
	currContent := f.Content
	s.mu.RUnlock()

	var hunks []DiffHunk
	if prevContent != "" {
		entries := ComputeLineDiff(prevContent, currContent)
		hunks = DiffEntriesToHunks(entries)
	}
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks, "previous_content": prevContent}, true
}

// SessionInfo returns metadata about the session for the API.
type SessionInfo struct {
	Mode            string            `json:"mode"` // "files" or "git"
	Branch          string            `json:"branch"`
	BaseRef         string            `json:"base_ref"`
	ReviewRound     int               `json:"review_round"`
	AvailableScopes []string          `json:"available_scopes"`
	Files           []SessionFileInfo `json:"files"`
	ReviewComments  []Comment         `json:"review_comments"`
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

	reviewComments := make([]Comment, len(s.reviewComments))
	copy(reviewComments, s.reviewComments)

	info := SessionInfo{
		Mode:            s.Mode,
		Branch:          s.Branch,
		BaseRef:         s.BaseRef,
		ReviewRound:     s.ReviewRound,
		AvailableScopes: availableScopes(s.BaseRef),
		ReviewComments:  reviewComments,
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

// availableScopes returns the list of scopes that have files.
// Only includes a scope if git reports changes for it.
func availableScopes(baseRef string) []string {
	scopes := []string{"all"}
	if baseRef != "" {
		if files, err := changedFilesBranch(baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "branch")
		}
	}
	if files, err := changedFilesStaged(); err == nil && len(files) > 0 {
		scopes = append(scopes, "staged")
	}
	if files, err := changedFilesUnstaged(); err == nil && len(files) > 0 {
		scopes = append(scopes, "unstaged")
	}
	return scopes
}

// GetCommits returns the list of commits between the base ref and HEAD.
// Returns nil for non-git sessions or when no base ref is set.
func (s *Session) GetCommits() []CommitInfo {
	s.mu.RLock()
	if s.Mode != "git" || s.BaseRef == "" {
		s.mu.RUnlock()
		return nil
	}
	baseRef, repoRoot := s.BaseRef, s.RepoRoot
	s.mu.RUnlock()
	commits, err := CommitLog(baseRef, repoRoot)
	if err != nil {
		return nil
	}
	return commits
}

// GetSessionInfoScoped returns session metadata filtered to a specific diff scope.
// When scope is "" or in file mode (scopes only apply to git), delegates to GetSessionInfo.
// All other scopes (including "all") run fresh git queries to pick up files added after startup.
// When commit is non-empty, files and diffs are scoped to that single commit.
func (s *Session) GetSessionInfoScoped(scope, commit string) SessionInfo {
	if commit == "" && (scope == "" || s.Mode == "files") {
		return s.GetSessionInfo()
	}

	// Read session fields under lock, then release before shelling out to git.
	s.mu.RLock()
	baseRef := s.BaseRef
	repoRoot := s.RepoRoot
	mode := s.Mode
	branch := s.Branch
	reviewRound := s.ReviewRound
	ignorePatterns := s.IgnorePatterns
	// Build a map of comment counts (comments are scope-independent)
	commentCounts := make(map[string]int, len(s.Files))
	for _, f := range s.Files {
		commentCounts[f.Path] = len(f.Comments)
	}
	reviewComments := make([]Comment, len(s.reviewComments))
	copy(reviewComments, s.reviewComments)
	s.mu.RUnlock()

	info := SessionInfo{
		Mode:            mode,
		Branch:          branch,
		BaseRef:         baseRef,
		ReviewRound:     reviewRound,
		AvailableScopes: availableScopes(baseRef),
		ReviewComments:  reviewComments,
	}

	var changes []FileChange
	var err error
	if commit != "" {
		changes, err = ChangedFilesForCommit(commit, repoRoot)
	} else {
		changes, err = ChangedFilesScoped(scope, baseRef)
	}
	if err != nil || len(changes) == 0 {
		return info
	}

	// Apply the same ignore patterns used during session creation so that
	// files like .crit/sessions/*.json don't leak into scoped views.
	changes = filterIgnored(changes, ignorePatterns)

	for _, fc := range changes {
		fi := SessionFileInfo{
			Path:         fc.Path,
			Status:       fc.Status,
			FileType:     detectFileType(fc.Path),
			CommentCount: commentCounts[fc.Path],
		}

		// Compute diff stats for the scoped view
		var hunks []DiffHunk
		if commit != "" {
			h, diffErr := FileDiffForCommit(fc.Path, commit, repoRoot)
			if diffErr == nil {
				hunks = h
			}
		} else if fc.Status == "added" || fc.Status == "untracked" {
			absPath := filepath.Join(repoRoot, fc.Path)
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				hunks = FileDiffUnifiedNewFile(string(data))
			}
		} else {
			h, diffErr := FileDiffScoped(fc.Path, scope, baseRef, repoRoot)
			if diffErr == nil {
				hunks = h
			}
		}
		for _, h := range hunks {
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

// GetFileDiffSnapshotScoped returns diff data for a file filtered by scope.
// When scope is "" or in file mode (scopes only apply to git), delegates to GetFileDiffSnapshot.
// When commit is non-empty, returns the diff for that single commit.
func (s *Session) GetFileDiffSnapshotScoped(path, scope, commit string) (map[string]any, bool) {
	if commit == "" && (scope == "" || s.Mode == "files") {
		return s.GetFileDiffSnapshot(path)
	}

	s.mu.RLock()
	f := s.fileByPathLocked(path)
	var baseRef, repoRoot, status, content string
	if f != nil {
		baseRef = s.BaseRef
		status = f.Status
		content = f.Content
	} else {
		baseRef = s.BaseRef
	}
	repoRoot = s.RepoRoot
	s.mu.RUnlock()

	// If the file is not in the session (e.g. created after startup), read it
	// from disk and determine its status so we can generate the correct diff.
	if f == nil && repoRoot != "" {
		absPath := filepath.Join(repoRoot, path)
		if data, err := os.ReadFile(absPath); err == nil {
			content = string(data)
			// Determine file status from git for the requested scope
			if changes, err := ChangedFilesScoped(scope, baseRef); err == nil {
				for _, fc := range changes {
					if fc.Path == path {
						status = fc.Status
						break
					}
				}
			}
		}
	}

	var hunks []DiffHunk
	if commit != "" {
		h, err := FileDiffForCommit(path, commit, repoRoot)
		if err == nil {
			hunks = h
		}
	} else if status == "untracked" && (scope == "unstaged" || scope == "all" || scope == "") {
		hunks = FileDiffUnifiedNewFile(content)
	} else if status == "added" && scope != "unstaged" {
		hunks = FileDiffUnifiedNewFile(content)
	} else {
		h, err := FileDiffScoped(path, scope, baseRef, repoRoot)
		if err == nil {
			hunks = h
		}
	}
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}
