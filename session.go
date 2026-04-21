package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// lazyFileThreshold is the maximum number of files to eagerly load
// content and diffs for. Files beyond this threshold are loaded on demand
// when the user expands them in the UI. Only applies when >threshold files.
const lazyFileThreshold = 100

// fileHash returns a stable, prefixed hash string for file content tracking.
// It delegates to computeFileHash and adds a "sha256:" prefix to distinguish
// the hash algorithm used.
func fileHash(data []byte) string {
	return "sha256:" + computeFileHash(data)
}

// randomID generates a random ID with the given prefix using crypto/rand.
// Format: prefix + 6 hex characters (3 random bytes).
func randomID(prefix string) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// randomCommentID returns a random file/line comment ID (e.g. "c_a3f8b2").
func randomCommentID() string { return randomID("c_") }

// randomReviewCommentID returns a random review-level comment ID (e.g. "r_b4c9e1").
func randomReviewCommentID() string { return randomID("r_") }

// randomReplyID returns a random reply ID (e.g. "rp_d7e2a0").
func randomReplyID() string { return randomID("rp_") }

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
	Anchor         string  `json:"anchor,omitempty"`
	Drifted        bool    `json:"drifted,omitempty"`
	Author         string  `json:"author,omitempty"`
	Scope          string  `json:"scope,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Resolved       bool    `json:"resolved,omitempty"`
	Live           bool    `json:"live,omitempty"`
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

	// Lazy loading: when true, Content and DiffHunks are not yet populated.
	// Call ensureLoaded() before accessing them. Only used when >100 files.
	Lazy     bool      `json:"-"`
	loadOnce sync.Once // guards one-time loading of content + diffs
	loadErr  error     // error from loading, if any

	// Stats for lazy files (populated from git diff --numstat)
	LazyAdditions int `json:"-"`
	LazyDeletions int `json:"-"`

	// Orphaned: file has comments in the review file but is no longer in the session's
	// file list (e.g., added on branch then deleted). No content or diff available.
	Orphaned bool `json:"-"`
}

// ensureLoaded loads content and diff hunks for a lazy file on first access.
// For non-lazy files, this is an immediate no-op.
func (fe *FileEntry) ensureLoaded(repoRoot, baseRef string) error {
	if !fe.Lazy {
		return nil
	}
	fe.loadOnce.Do(func() {
		if fe.Status != "deleted" {
			data, err := os.ReadFile(fe.AbsPath)
			if err != nil {
				fe.loadErr = fmt.Errorf("reading %s: %w", fe.Path, err)
				return
			}
			fe.Content = string(data)
			fe.FileHash = fileHash(data)
		}

		if fe.Status != "deleted" {
			if fe.Status == "added" || fe.Status == "untracked" {
				fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				hunks, err := fileDiffUnifiedCtx(ctx, fe.Path, baseRef, repoRoot)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", fe.Path, err)
				} else {
					fe.DiffHunks = hunks
				}
			}
		}

		fe.Lazy = false
	})
	return fe.loadErr
}

// Session is the top-level state manager for a multi-file review.
type Session struct {
	Files          []*FileEntry
	Mode           string   // "files" (explicit markdown files) or "git" (auto-detected from git)
	CLIArgs        []string // original file arguments passed on the command line (empty for git mode)
	Branch         string
	BaseRef        string
	BaseBranchName string // display name of the base branch (e.g. "production", "master")
	RepoRoot       string
	OutputDir      string // custom output directory for the review file (empty = RepoRoot)
	ReviewFilePath string // centralized review file path (~/.crit/reviews/<key>.json)
	PlanDir        string // managed storage dir for plan mode (empty for git/files)
	ReviewRound    int
	IgnorePatterns []string

	reviewComments []Comment

	// deletedCommentIDs tracks IDs of file comments deleted in-memory but not
	// yet written to disk. Keyed by file path -> set of comment IDs. This
	// prevents mergeFileSnapshotIntoCritJSON from re-adding them from disk.
	deletedCommentIDs map[string]map[string]struct{}

	mu          sync.RWMutex
	subscribers map[chan SSEEvent]struct{}
	subMu       sync.Mutex
	writeTimer  *time.Timer
	writeGen    int
	// writeMu serializes debounced WriteFiles() calls with ClearAllComments
	// so a stale in-flight write cannot recreate the review file after it has
	// been deleted. time.Timer.Stop() does not wait for callbacks already
	// executing, so the writeGen check alone is not sufficient to prevent a
	// snapshot-taken-before-clear from resurrecting the file.
	writeMu             sync.Mutex
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
	waitingForAgent     bool      // true between finish (with unresolved comments) and round-complete
	browserClients      int32     // number of connected SSE browser clients (atomic)

}

// CritJSON is the on-disk format for review files.
type CritJSON struct {
	Branch         string                  `json:"branch"`
	BaseRef        string                  `json:"base_ref"`
	UpdatedAt      string                  `json:"updated_at"`
	ReviewRound    int                     `json:"review_round"`
	ShareURL       string                  `json:"share_url,omitempty"`
	DeleteToken    string                  `json:"delete_token,omitempty"`
	ShareScope     string                  `json:"share_scope,omitempty"`
	LastShareHash  string                  `json:"last_share_hash,omitempty"`
	ReviewComments []Comment               `json:"review_comments,omitempty"`
	Files          map[string]CritJSONFile `json:"files"`
}

// CritJSONFile is the per-file section in review files.
type CritJSONFile struct {
	Status   string    `json:"status"`
	FileHash string    `json:"file_hash"`
	Comments []Comment `json:"comments"`
}

// detectGitChanges resolves the base ref and returns the list of changed files.
func detectGitChanges(root string, ignorePatterns []string) (branch, baseRef, resolvedBase string, changes []FileChange, err error) {
	branch = CurrentBranch()
	resolvedBase = DefaultBranch()
	if branch != resolvedBase {
		baseRef, _ = MergeBase(resolvedBase)
	}

	if baseRef != "" {
		changes, err = changedFilesFromBaseInDir(baseRef, root)
	} else {
		changes, err = changedFilesOnDefaultInDir(root)
	}
	if err != nil {
		return "", "", "", nil, fmt.Errorf("detecting changes: %w", err)
	}
	changes = filterIgnored(changes, ignorePatterns)

	if len(changes) == 0 {
		return "", "", "", nil, fmt.Errorf("no changed files detected (after applying ignore patterns)")
	}
	return branch, baseRef, resolvedBase, changes, nil
}

// populateLazyFile fills stats for a file that will be loaded on demand.
func populateLazyFile(fe *FileEntry, fc FileChange, numstats map[string]NumstatEntry) {
	fe.Lazy = true
	fe.Comments = []Comment{}
	if ns, ok := numstats[fc.Path]; ok {
		fe.LazyAdditions = ns.Additions
		fe.LazyDeletions = ns.Deletions
	} else if fc.Status == "untracked" || fc.Status == "added" {
		if data, err := os.ReadFile(fe.AbsPath); err == nil {
			fe.LazyAdditions = strings.Count(string(data), "\n")
			if len(data) > 0 && data[len(data)-1] != '\n' {
				fe.LazyAdditions++
			}
		}
	}
}

// populateEagerFile reads content and computes diffs for a file loaded at startup.
func populateEagerFile(fe *FileEntry, fc FileChange, baseRef, root string) bool {
	if fc.Status != "deleted" {
		data, err := os.ReadFile(fe.AbsPath)
		if err != nil {
			return false
		}
		fe.Content = string(data)
		fe.FileHash = fileHash(data)
	}

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
	return true
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

	branch, baseRef, resolvedBase, changes, err := detectGitChanges(root, ignorePatterns)
	if err != nil {
		return nil, err
	}

	s := &Session{
		Mode:                "git",
		Branch:              branch,
		BaseRef:             baseRef,
		BaseBranchName:      resolvedBase,
		RepoRoot:            root,
		ReviewRound:         1,
		IgnorePatterns:      ignorePatterns,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		awaitingFirstReview: true,
	}

	var numstats map[string]NumstatEntry
	if len(changes) > lazyFileThreshold && baseRef != "" {
		numstats, _ = DiffNumstatDir(baseRef, root)
	}

	for i, fc := range changes {
		absPath := filepath.Join(root, fc.Path)
		fe := &FileEntry{
			Path:     fc.Path,
			AbsPath:  absPath,
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
		}

		if len(changes) > lazyFileThreshold && i >= lazyFileThreshold {
			populateLazyFile(fe, fc, numstats)
		} else if !populateEagerFile(fe, fc, baseRef, root) {
			continue
		}
		s.Files = append(s.Files, fe)
	}

	return s, nil
}

// expandAndDedupPaths expands directory paths into files and deduplicates the result.
func expandAndDedupPaths(paths []string, ignorePatterns []string) ([]string, error) {
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
			expandedPaths = append(expandedPaths, absPath)
		}
	}

	seen := make(map[string]bool, len(expandedPaths))
	unique := expandedPaths[:0]
	for _, p := range expandedPaths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	return unique, nil
}

// resolveGitContext returns git repo state for file-mode sessions.
func resolveGitContext() (root, branch, baseRef, baseBranchName string) {
	if !IsGitRepo() {
		return "", "", "", ""
	}
	root, _ = RepoRoot()
	branch = CurrentBranch()
	resolvedBase := DefaultBranch()
	baseBranchName = resolvedBase
	if branch != resolvedBase {
		baseRef, _ = MergeBase(resolvedBase)
	}
	return root, branch, baseRef, baseBranchName
}

// NewSessionFromFiles creates a session from explicitly provided file or directory paths.
// When a directory is passed, all files within it are included recursively.
// The base branch is read from DefaultBranch(), which respects defaultBranchOverride
// set by resolveServerConfig(). See NewSessionFromGit for rationale.
func NewSessionFromFiles(paths []string, ignorePatterns []string) (*Session, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files provided")
	}

	expandedPaths, err := expandAndDedupPaths(paths, ignorePatterns)
	if err != nil {
		return nil, err
	}
	if len(expandedPaths) == 0 {
		return nil, fmt.Errorf("no files found")
	}

	root, branch, baseRef, baseBranchName := resolveGitContext()
	if root == "" {
		root = filepath.Dir(expandedPaths[0])
	}

	s := &Session{
		Mode:                "files",
		Branch:              branch,
		BaseRef:             baseRef,
		BaseBranchName:      baseBranchName,
		RepoRoot:            root,
		ReviewRound:         1,
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

		if IsGitRepo() {
			hunks, diffErr := fileDiffUnified(relPath, baseRef, root)
			if diffErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", relPath, diffErr)
			} else {
				fe.DiffHunks = hunks
			}
		}

		s.Files = append(s.Files, fe)
	}

	return s, nil
}

// walkDirectory recursively walks a directory and returns all file paths,
// skipping hidden directories and common non-text directories.
func walkDirectory(dir string, ignorePatterns []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort walk: skip inaccessible entries
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

// extractAnchor returns the joined text of lines[startLine..endLine] (1-indexed)
// from the given content. Returns empty string if lines are out of range.
func extractAnchor(content string, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine || content == "" {
		return ""
	}
	lines := splitLines(content)
	if startLine > len(lines) {
		return ""
	}
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[startLine-1:end], "\n")
}

// AddComment adds a comment to a specific file.
func (s *Session) AddComment(filePath string, startLine, endLine int, side, body, quote, author string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}

	// For old-side comments, line numbers reference the base version of the file,
	// not the working tree. Extract anchor from the base ref content.
	var anchor string
	if side == "old" && s.BaseRef != "" {
		baseContent := fileContentAtRef(filePath, s.BaseRef, s.RepoRoot)
		anchor = extractAnchor(baseContent, startLine, endLine)
	} else {
		anchor = extractAnchor(f.Content, startLine, endLine)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:          randomCommentID(),
		StartLine:   startLine,
		EndLine:     endLine,
		Side:        side,
		Body:        body,
		Quote:       quote,
		Anchor:      anchor,
		Author:      author,
		Scope:       "line",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
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
		ID:          randomCommentID(),
		Body:        body,
		Author:      author,
		Scope:       "file",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
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
		ID:          randomReviewCommentID(),
		Body:        body,
		Author:      author,
		Scope:       "review",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}
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
				ID:        randomReplyID(),
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

// SetCommentLive marks a comment as live (sent to an agent).
func (s *Session) SetCommentLive(filePath, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return false
	}
	for i, c := range f.Comments {
		if c.ID == id {
			f.Comments[i].Live = true
			s.scheduleWrite()
			return true
		}
	}
	return false
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
			s.trackDeletedComment(filePath, id)
			s.scheduleWrite()
			return true
		}
	}
	return false
}

// trackDeletedComment records a file comment ID as deleted so the merge logic
// does not re-add it from disk. Caller must hold s.mu.
func (s *Session) trackDeletedComment(filePath, id string) {
	if s.deletedCommentIDs == nil {
		s.deletedCommentIDs = make(map[string]map[string]struct{})
	}
	if s.deletedCommentIDs[filePath] == nil {
		s.deletedCommentIDs[filePath] = make(map[string]struct{})
	}
	s.deletedCommentIDs[filePath][id] = struct{}{}
}

// RefreshFileContent re-reads all file content from disk.
func (s *Session) RefreshFileContent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.Files {
		if f.AbsPath == "" || f.Lazy {
			continue
		}
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		newHash := fileHash(data)
		if newHash != f.FileHash {
			f.Content = string(data)
			f.FileHash = newHash
		}
	}
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
				ID:        randomReplyID(),
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

// FindCommentByID looks up a comment by ID, optionally scoped to a file path.
// Returns the comment, the file path it belongs to, and whether it was found.
func (s *Session) FindCommentByID(id string, filePath string) (Comment, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if filePath != "" {
		for _, f := range s.Files {
			if f.Path == filePath {
				for _, c := range f.Comments {
					if c.ID == id {
						return c, f.Path, true
					}
				}
			}
		}
	}
	for _, f := range s.Files {
		for _, c := range f.Comments {
			if c.ID == id {
				return c, f.Path, true
			}
		}
	}
	return Comment{}, "", false
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

	// Determine the file's git status via a single-file diff against baseRef
	// (avoids running full ChangedFiles which diffs ALL files).
	status := fileStatusInRepo(path, repoRoot, baseRef)

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

// LoadShareFilesFromDisk reads file content from disk for all session files,
// returning share-ready file entries. Orphaned files (removed between rounds)
// are included with empty content and the orphaned flag set so crit-web can
// render them with the appropriate status badge.
func (s *Session) LoadShareFilesFromDisk() []shareFile {
	s.mu.RLock()
	type fileInfo struct {
		path                  string
		absPath               string
		status                string
		orphaned              bool
		hasUnresolvedComments bool
	}
	infos := make([]fileInfo, 0, len(s.Files))
	for _, f := range s.Files {
		hasUnresolved := false
		for _, c := range f.Comments {
			if !c.Resolved {
				hasUnresolved = true
				break
			}
		}
		infos = append(infos, fileInfo{path: f.Path, absPath: f.AbsPath, status: f.Status, orphaned: f.Orphaned, hasUnresolvedComments: hasUnresolved})
	}
	s.mu.RUnlock()

	var files []shareFile
	for _, fi := range infos {
		if fi.orphaned {
			if !fi.hasUnresolvedComments {
				continue // skip orphaned files with no unresolved comments
			}
			files = append(files, shareFile{
				Path:   fi.path,
				Status: "removed",
			})
			continue
		}
		if fi.status == "deleted" {
			continue
		}
		data, err := os.ReadFile(fi.absPath)
		if err != nil {
			continue // file may have been removed since session started
		}
		files = append(files, shareFile{Path: fi.path, Content: string(data), Status: fi.status})
	}
	return files
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

// setWaitingForAgent marks whether the session is in the "waiting for agent edits" phase.
func (s *Session) setWaitingForAgent(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waitingForAgent = v
}

// isWaitingForAgent returns true if the session is waiting for agent edits.
func (s *Session) isWaitingForAgent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.waitingForAgent
}

// SignalRoundComplete prepares the session for a new round by clearing current
// comments and sending a signal to the watcher goroutine. The ReviewRound counter
// is NOT incremented here — it is deferred to the watcher's handleRoundComplete*
// handler, which increments it only after comments have been carried forward from
// the review file. This prevents a TOCTOU race where GetSessionInfo could observe the
// new round number before carry-forward is complete, returning empty comments.
func (s *Session) SignalRoundComplete() {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.pendingWrite = false
	s.lastRoundEdits = s.pendingEdits
	s.pendingEdits = 0
	s.waitingForAgent = false
	// Clear comments on all files.
	// ReviewRound is incremented later by the watcher after carry-forward.
	for _, f := range s.Files {
		f.Comments = []Comment{}
	}
	s.mu.Unlock()
	select {
	case s.roundComplete <- struct{}{}:
	default:
	}
}

// ClearAllComments removes all comments from all files and resets comment IDs and review round.
// Used by the E2E test cleanup endpoint to return the server to a clean initial state.
// It also removes the review file entry from s.Files and deletes the review file from disk
// (centralized storage under ~/.crit/reviews/).
func (s *Session) ClearAllComments() {
	// Hold writeMu for the duration so any in-flight debounced write must
	// finish (and observe the new writeGen) before we proceed. Without this,
	// a WriteFiles() call that passed the gen check a moment ago could
	// atomicWriteFile the old snapshot back onto disk after os.Remove below.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	// Cancel any pending debounced write so it cannot recreate the review file after we delete it.
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	// Reset all file state, drop the review file entry and orphaned phantom entries.
	filtered := make([]*FileEntry, 0, len(s.Files))
	for _, f := range s.Files {
		if filepath.Base(f.Path) == ".crit.json" || f.Orphaned {
			continue
		}
		f.Comments = []Comment{}
		f.PreviousComments = nil
		f.PreviousContent = ""
		filtered = append(filtered, f)
	}
	s.Files = filtered
	s.reviewComments = nil
	s.ReviewRound = 1
	s.lastCritJSONMtime = time.Time{}
	s.pendingWrite = false
	s.waitingForAgent = false
	critPath := s.critJSONPath()
	s.mu.Unlock()
	// Delete the review file from disk (centralized or legacy path).
	os.Remove(critPath) //nolint:errcheck
}

// ChangeBaseBranch changes the diff base to the given branch, recomputes merge-base,
// rebuilds the file list with new diffs, and notifies connected browsers via SSE.
// Comments are preserved for files that still appear in the new diff.
func (s *Session) ChangeBaseBranch(branch string) error {
	s.mu.RLock()
	mode := s.Mode
	s.mu.RUnlock()
	if mode != "git" {
		return fmt.Errorf("base branch can only be changed in git mode")
	}

	// Compute merge-base with the new branch (try both local and remote ref)
	mb, err := MergeBase(branch)
	if err != nil {
		mb, err = MergeBase("origin/" + branch)
		if err != nil {
			return fmt.Errorf("cannot compute merge-base with %s: %w", branch, err)
		}
	}

	// Save old state for rollback
	oldOverride := getDefaultBranchOverride()

	// Update the global override so ChangedFiles() uses the new base
	setDefaultBranchOverride(branch)

	s.mu.Lock()
	oldBaseRef := s.BaseRef
	oldBaseBranchName := s.BaseBranchName
	s.BaseRef = mb
	s.BaseBranchName = branch
	repoRoot := s.RepoRoot
	currentBranch := s.Branch
	ignorePatterns := s.IgnorePatterns

	// Preserve existing comments keyed by file path
	commentsByPath := make(map[string][]Comment, len(s.Files))
	for _, f := range s.Files {
		if len(f.Comments) > 0 {
			commentsByPath[f.Path] = f.Comments
		}
	}
	s.mu.Unlock()

	// Re-detect changed files with new base
	var changes []FileChange
	if currentBranch != branch {
		changes, err = changedFilesFromBaseInDir(mb, repoRoot)
	} else {
		changes, err = changedFilesOnDefaultInDir(repoRoot)
	}
	if err != nil {
		// Rollback all state
		setDefaultBranchOverride(oldOverride)
		s.mu.Lock()
		s.BaseRef = oldBaseRef
		s.BaseBranchName = oldBaseBranchName
		s.mu.Unlock()
		return fmt.Errorf("detecting changes: %w", err)
	}
	changes = filterIgnored(changes, ignorePatterns)

	// Build new file entries, preserving comments
	var newFiles []*FileEntry
	for _, fc := range changes {
		absPath := filepath.Join(repoRoot, fc.Path)
		fe := &FileEntry{
			Path:     fc.Path,
			AbsPath:  absPath,
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
			Comments: commentsByPath[fc.Path],
		}
		if fe.Comments == nil {
			fe.Comments = []Comment{}
		}
		if fc.Status != "deleted" {
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				fe.Content = string(data)
				fe.FileHash = fileHash(data)
			}
		}
		if fc.Status != "added" && fc.Status != "untracked" {
			if hunks, diffErr := fileDiffUnified(fc.Path, mb, repoRoot); diffErr == nil {
				fe.DiffHunks = hunks
			}
		} else {
			fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
		}
		newFiles = append(newFiles, fe)
	}

	s.mu.Lock()
	s.Files = newFiles
	s.mu.Unlock()

	s.notify(SSEEvent{Type: "base-changed"})
	return nil
}

// scheduleWrite debounces writes to disk.
func (s *Session) scheduleWrite() {
	s.pendingWrite = true
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	gen := s.writeGen
	s.writeTimer = time.AfterFunc(200*time.Millisecond, func() {
		// Serialize debounced writes with ClearAllComments so a stale
		// in-flight write cannot recreate the review file after we've
		// deleted it. ClearAllComments bumps writeGen under writeMu, so
		// once we hold the mutex the gen check reflects the final state.
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		s.mu.RLock()
		if s.writeGen != gen {
			s.mu.RUnlock()
			return
		}
		s.mu.RUnlock()
		s.WriteFiles()
	})
}

// critJSONPath returns the path to the review file.
func (s *Session) critJSONPath() string {
	if s.OutputDir != "" {
		return filepath.Join(s.OutputDir, ".crit.json")
	}
	if s.ReviewFilePath != "" {
		return s.ReviewFilePath
	}
	// Fallback for tests and backwards compat
	return filepath.Join(s.RepoRoot, ".crit.json")
}

// writeFilesSnapshot holds all session state needed to write the review file,
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
	path       string
	status     string
	fileHash   string
	comments   []Comment
	deletedIDs map[string]struct{} // comment IDs deleted in-memory, skip during merge
}

// handleExternalDeletion checks if the review file was deleted externally and clears
// in-memory comments if so. Returns true if the file was deleted.
func (s *Session) handleExternalDeletion(critPath string) bool {
	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if lastMtime.IsZero() {
		return false
	}
	if _, statErr := os.Stat(critPath); !os.IsNotExist(statErr) {
		return false
	}

	s.clearAllCommentData()
	return true
}

// clearAllCommentData resets all in-memory comment state (file comments,
// review comments, and ID counters) and notifies if any comments existed.
// Caller must NOT hold s.mu.
func (s *Session) clearAllCommentData() {
	s.mu.Lock()
	s.lastCritJSONMtime = time.Time{}
	anyComments := false
	for _, f := range s.Files {
		if len(f.Comments) > 0 {
			f.Comments = []Comment{}
			anyComments = true
		}
	}
	if len(s.reviewComments) > 0 {
		anyComments = true
	}
	s.reviewComments = nil
	s.deletedCommentIDs = nil
	s.mu.Unlock()
	if anyComments {
		s.notify(SSEEvent{Type: "comments-changed"})
	}
}

// buildCritJSON loads the existing review file from disk, applies the snapshot metadata,
// and merges per-file comments.
func buildCritJSON(snap writeFilesSnapshot) CritJSON {
	cj := CritJSON{Files: make(map[string]CritJSONFile)}
	if data, err := os.ReadFile(snap.critPath); err == nil {
		if unmarshalErr := json.Unmarshal(data, &cj); unmarshalErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: corrupt review file, starting fresh: %v\n", unmarshalErr)
		}
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

	for _, fs := range snap.files {
		mergeFileSnapshotIntoCritJSON(&cj, fs)
	}
	return cj
}

// mergeFileSnapshotIntoCritJSON merges a single file's comments from the snapshot
// with any disk-only comments, and updates the CritJSON.
func mergeFileSnapshotIntoCritJSON(cj *CritJSON, fs writeFileSnapshot) {
	diskFile, hasDisk := cj.Files[fs.path]

	memIDs := make(map[string]struct{}, len(fs.comments))
	for _, c := range fs.comments {
		memIDs[c.ID] = struct{}{}
	}

	merged := fs.comments
	if hasDisk {
		for _, dc := range diskFile.Comments {
			if _, exists := memIDs[dc.ID]; exists {
				continue
			}
			// Skip comments that were explicitly deleted in-memory
			if _, deleted := fs.deletedIDs[dc.ID]; deleted {
				continue
			}
			merged = append(merged, dc)
		}
	}

	if len(merged) == 0 {
		delete(cj.Files, fs.path)
		return
	}

	cj.Files[fs.path] = CritJSONFile{
		Status:   fs.status,
		FileHash: fs.fileHash,
		Comments: merged,
	}
}

func critJSONIsEmpty(cj CritJSON) bool {
	return len(cj.Files) == 0 && len(cj.ReviewComments) == 0 &&
		cj.ShareURL == "" && cj.DeleteToken == "" && cj.ShareScope == ""
}

// WriteFiles writes the review file to disk.
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

	if s.handleExternalDeletion(critPath) {
		return
	}

	snap := s.snapshotForWrite(critPath)
	cj := buildCritJSON(snap)

	if critJSONIsEmpty(cj) {
		os.Remove(snap.critPath)
		s.mu.Lock()
		s.lastCritJSONMtime = time.Time{}
		s.pendingWrite = false
		s.deletedCommentIDs = nil
		s.mu.Unlock()
		return
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling review file: %v\n", err)
		return
	}
	if err := atomicWriteFile(snap.critPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing review file: %v\n", err)
		return
	}
	if info, err := os.Stat(snap.critPath); err == nil {
		s.mu.Lock()
		s.lastCritJSONMtime = info.ModTime()
		s.pendingWrite = false
		s.deletedCommentIDs = nil // written to disk, no longer needed
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
		var deleted map[string]struct{}
		if ids := s.deletedCommentIDs[f.Path]; len(ids) > 0 {
			deleted = make(map[string]struct{}, len(ids))
			for k, v := range ids {
				deleted[k] = v
			}
		}
		snap.files[i] = writeFileSnapshot{
			path:       f.Path,
			status:     f.Status,
			fileHash:   f.FileHash,
			comments:   comments,
			deletedIDs: deleted,
		}
	}
	return snap
}

// handleCritJSONDeleted clears all in-memory comment state when the review file
// has been deleted. Returns true unconditionally to signal the deletion.
func (s *Session) handleCritJSONDeleted() bool {
	s.clearAllCommentData()
	return true
}

func (s *Session) mergeFileCommentsFromDisk(f *FileEntry, diskFile CritJSONFile) bool {
	changed := false

	memIDs := make(map[string]struct{}, len(f.Comments))
	for _, c := range f.Comments {
		memIDs[c.ID] = struct{}{}
	}

	for _, dc := range diskFile.Comments {
		if _, exists := memIDs[dc.ID]; !exists {
			f.Comments = append(f.Comments, dc)
			changed = true
		} else {
			changed = s.mergeCommentRepliesAndState(f.Comments, dc) || changed
		}
	}

	// Remove comments deleted on disk.
	if len(diskFile.Comments) != len(f.Comments) {
		changed = filterDeletedComments(f, diskFile.Comments) || changed
	}

	return changed
}

func (s *Session) mergeCommentRepliesAndState(comments []Comment, dc Comment) bool {
	changed := false
	for i, mc := range comments {
		if mc.ID != dc.ID {
			continue
		}
		memReplyIDs := make(map[string]struct{}, len(mc.Replies))
		for _, r := range mc.Replies {
			memReplyIDs[r.ID] = struct{}{}
		}
		for _, dr := range dc.Replies {
			if _, exists := memReplyIDs[dr.ID]; !exists {
				comments[i].Replies = append(comments[i].Replies, dr)
				changed = true
			}
		}
		if dc.Resolved != mc.Resolved {
			comments[i].Resolved = dc.Resolved
			changed = true
		}
		break
	}
	return changed
}

func filterDeletedComments(f *FileEntry, diskComments []Comment) bool {
	diskIDs := make(map[string]struct{}, len(diskComments))
	for _, dc := range diskComments {
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
		return true
	}
	return false
}

func (s *Session) mergeReviewCommentsFromDisk(diskComments []Comment) bool {
	changed := false

	memReviewIDs := make(map[string]struct{}, len(s.reviewComments))
	for _, c := range s.reviewComments {
		memReviewIDs[c.ID] = struct{}{}
	}
	for _, dc := range diskComments {
		if _, exists := memReviewIDs[dc.ID]; !exists {
			s.reviewComments = append(s.reviewComments, dc)
			changed = true
		} else {
			changed = s.mergeReviewCommentRepliesAndState(dc) || changed
		}
	}

	// Remove review comments deleted on disk.
	changed = s.filterDeletedReviewComments(diskComments) || changed

	return changed
}

func (s *Session) mergeReviewCommentRepliesAndState(dc Comment) bool {
	changed := false
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
	return changed
}

func (s *Session) filterDeletedReviewComments(diskComments []Comment) bool {
	diskRIDs := make(map[string]struct{}, len(diskComments))
	for _, dc := range diskComments {
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
		return true
	}
	return false
}

func (s *Session) mergeExternalCritJSON() bool {
	critPath := s.critJSONPath()

	info, err := os.Stat(critPath)

	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if err != nil {
		if !lastMtime.IsZero() {
			return s.handleCritJSONDeleted()
		}
		return false
	}

	if !lastMtime.IsZero() && info.ModTime().Equal(lastMtime) {
		return false
	}

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
	// Disk is authoritative for external edits — clear deleted tracking
	s.deletedCommentIDs = nil

	changed := false

	for _, f := range s.Files {
		diskFile, hasDisk := cj.Files[f.Path]
		if !hasDisk {
			if len(f.Comments) > 0 {
				f.Comments = []Comment{}
				changed = true
			}
			continue
		}
		changed = s.mergeFileCommentsFromDisk(f, diskFile) || changed
	}

	changed = s.mergeReviewCommentsFromDisk(cj.ReviewComments) || changed
	s.mu.Unlock()

	if changed {
		s.notify(SSEEvent{Type: "comments-changed"})
	}

	return changed
}

// loadCritJSON loads comments and share state from an existing review file.
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
	} else if cj.ShareURL != "" {
		// No scope recorded — load unconditionally.
		s.sharedURL = cj.ShareURL
		s.deleteToken = cj.DeleteToken
	}

	// Restore review round so the session continues from where it left off.
	if cj.ReviewRound > s.ReviewRound {
		s.ReviewRound = cj.ReviewRound
	}

	// Restore comments for files that match by path.
	for _, f := range s.Files {
		if cf, ok := cj.Files[f.Path]; ok {
			f.Comments = cf.Comments
			for i := range f.Comments {
				if f.Comments[i].Scope == "" {
					f.Comments[i].Scope = "line"
				}
			}
		}
	}

	// Detect orphaned paths: files in the review file with comments but not in the session.
	s.appendOrphanedFiles(cj.Files)

	// Restore review-level comments.
	s.reviewComments = cj.ReviewComments

	// Record the mtime so the first ticker tick doesn't re-process our own file.
	if info, err := os.Stat(s.critJSONPath()); err == nil {
		s.lastCritJSONMtime = info.ModTime()
	}
}

// restoreOrphanedComments reads the review file and creates phantom FileEntry
// objects for any paths that have comments but aren't in s.Files.
// Safe to call multiple times — existing entries (including previous orphans) are skipped.
// Must be called with s.mu NOT held (acquires the lock internally).
func (s *Session) restoreOrphanedComments() {
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendOrphanedFiles(cj.Files)
}

// appendOrphanedFiles creates phantom FileEntry objects for paths in critFiles
// that have comments but no matching entry in s.Files. Must be called with
// s.mu held or during init (before concurrent access).
func (s *Session) appendOrphanedFiles(critFiles map[string]CritJSONFile) {
	knownPaths := make(map[string]bool, len(s.Files))
	for _, f := range s.Files {
		knownPaths[f.Path] = true
	}
	for path, cf := range critFiles {
		if knownPaths[path] || len(cf.Comments) == 0 {
			continue
		}
		fe := &FileEntry{
			Path:     path,
			Status:   "removed",
			FileType: detectFileType(path),
			Comments: cf.Comments,
			Orphaned: true,
		}
		for i := range fe.Comments {
			if fe.Comments[i].Scope == "" {
				fe.Comments[i].Scope = "line"
			}
		}
		s.Files = append(s.Files, fe)
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
	f := s.fileByPathLocked(path)
	if f == nil {
		s.mu.RUnlock()
		return nil, false
	}
	repoRoot := s.RepoRoot
	baseRef := s.BaseRef
	s.mu.RUnlock()

	// Load content on demand for lazy files
	if err := f.ensureLoaded(repoRoot, baseRef); err != nil {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"path":      f.Path,
		"status":    f.Status,
		"file_type": f.FileType,
		"content":   f.Content,
		"file_hash": f.FileHash,
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
		"file_hash": fileHash(data),
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
	repoRoot := s.RepoRoot
	baseRef := s.BaseRef
	s.mu.RUnlock()

	// Load content + diffs on demand for lazy files
	if err := f.ensureLoaded(repoRoot, baseRef); err != nil {
		return nil, false
	}

	s.mu.RLock()
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
	BaseBranchName  string            `json:"base_branch_name,omitempty"`
	ReviewRound     int               `json:"review_round"`
	AvailableScopes []string          `json:"available_scopes"`
	Files           []SessionFileInfo `json:"files"`
	ReviewComments  []Comment         `json:"review_comments"`
	Cwd             string            `json:"cwd,omitempty"`
}

// SessionFileInfo is a summary of a file for the session API response.
type SessionFileInfo struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	FileType     string `json:"file_type"`
	CommentCount int    `json:"comment_count"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Lazy         bool   `json:"lazy,omitempty"`
	Orphaned     bool   `json:"orphaned,omitempty"`
}

// GetSessionInfo returns a snapshot of session metadata.
func (s *Session) GetSessionInfo() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	reviewComments := make([]Comment, len(s.reviewComments))
	copy(reviewComments, s.reviewComments)

	info := SessionInfo{
		Mode:           s.Mode,
		Branch:         s.Branch,
		BaseRef:        s.BaseRef,
		BaseBranchName: s.BaseBranchName,
		ReviewRound:    s.ReviewRound,
		ReviewComments: reviewComments,
		Cwd:            s.RepoRoot,
	}

	info.AvailableScopes = cachedAvailableScopes(info.BaseRef)

	for _, f := range s.Files {
		fi := SessionFileInfo{
			Path:         f.Path,
			Status:       f.Status,
			FileType:     f.FileType,
			CommentCount: len(f.Comments),
			Lazy:         f.Lazy,
			Orphaned:     f.Orphaned,
		}
		if f.Lazy {
			// Use pre-computed stats from git diff --numstat
			fi.Additions = f.LazyAdditions
			fi.Deletions = f.LazyDeletions
		} else {
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
		}
		info.Files = append(info.Files, fi)
	}
	return info
}

// scopeCache caches the result of availableScopes to avoid running multiple
// git commands on every /api/session request. The cache has a short TTL (2s)
// so scope changes are picked up quickly.
var (
	scopeCacheMu      sync.Mutex
	scopeCacheBaseRef string
	scopeCacheResult  []string
	scopeCacheExpiry  time.Time
)

const scopeCacheTTL = 2 * time.Second

// cachedAvailableScopes returns availableScopes results, using a 2-second cache
// to avoid running git commands on every /api/session poll.
func cachedAvailableScopes(baseRef string) []string {
	scopeCacheMu.Lock()
	defer scopeCacheMu.Unlock()

	now := time.Now()
	if now.Before(scopeCacheExpiry) && scopeCacheBaseRef == baseRef {
		result := make([]string, len(scopeCacheResult))
		copy(result, scopeCacheResult)
		return result
	}

	scopes := availableScopes(baseRef)
	scopeCacheBaseRef = baseRef
	scopeCacheResult = scopes
	scopeCacheExpiry = now.Add(scopeCacheTTL)

	result := make([]string, len(scopes))
	copy(result, scopes)
	return result
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

// scopedSessionSnapshot holds session state read under lock for scoped queries.
type scopedSessionSnapshot struct {
	baseRef        string
	baseBranchName string
	repoRoot       string
	mode           string
	branch         string
	reviewRound    int
	ignorePatterns []string
	commentCounts  map[string]int
	lazyFiles      map[string]*FileEntry
	reviewComments []Comment
}

func (s *Session) snapshotForScoped() scopedSessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commentCounts := make(map[string]int, len(s.Files))
	lazyFiles := make(map[string]*FileEntry, len(s.Files))
	for _, f := range s.Files {
		commentCounts[f.Path] = len(f.Comments)
		if f.Lazy {
			lazyFiles[f.Path] = f
		}
	}
	rc := make([]Comment, len(s.reviewComments))
	copy(rc, s.reviewComments)

	return scopedSessionSnapshot{
		baseRef:        s.BaseRef,
		baseBranchName: s.BaseBranchName,
		repoRoot:       s.RepoRoot,
		mode:           s.Mode,
		branch:         s.Branch,
		reviewRound:    s.ReviewRound,
		ignorePatterns: s.IgnorePatterns,
		commentCounts:  commentCounts,
		lazyFiles:      lazyFiles,
		reviewComments: rc,
	}
}

func scopedHunks(fc FileChange, scope, commit, baseRef, repoRoot string) []DiffHunk {
	if commit != "" {
		h, err := FileDiffForCommit(fc.Path, commit, repoRoot)
		if err == nil {
			return h
		}
		return nil
	}
	if fc.Status == "added" || fc.Status == "untracked" {
		absPath := filepath.Join(repoRoot, fc.Path)
		if data, err := os.ReadFile(absPath); err == nil {
			return FileDiffUnifiedNewFile(string(data))
		}
		return nil
	}
	h, err := FileDiffScoped(fc.Path, scope, baseRef, repoRoot)
	if err == nil {
		return h
	}
	return nil
}

func countHunkStats(hunks []DiffHunk) (additions, deletions int) {
	for _, h := range hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case "add":
				additions++
			case "del":
				deletions++
			}
		}
	}
	return additions, deletions
}

// GetSessionInfoScoped returns session metadata filtered to a specific diff scope.
// When scope is "" or in file mode (scopes only apply to git), delegates to GetSessionInfo.
// All other scopes (including "all") run fresh git queries to pick up files added after startup.
// When commit is non-empty, files and diffs are scoped to that single commit.
func (s *Session) GetSessionInfoScoped(scope, commit string) SessionInfo {
	if commit == "" && (scope == "" || scope == "all" || s.Mode == "files" || s.Mode == "plan") {
		return s.GetSessionInfo()
	}

	snap := s.snapshotForScoped()

	info := SessionInfo{
		Mode:            snap.mode,
		Branch:          snap.branch,
		BaseRef:         snap.baseRef,
		BaseBranchName:  snap.baseBranchName,
		ReviewRound:     snap.reviewRound,
		AvailableScopes: availableScopes(snap.baseRef),
		ReviewComments:  snap.reviewComments,
	}

	var changes []FileChange
	var err error
	if commit != "" {
		changes, err = ChangedFilesForCommit(commit, snap.repoRoot)
	} else {
		changes, err = ChangedFilesScoped(scope, snap.baseRef)
	}
	if err != nil || len(changes) == 0 {
		return info
	}

	changes = filterIgnored(changes, snap.ignorePatterns)

	for _, fc := range changes {
		fi := SessionFileInfo{
			Path:         fc.Path,
			Status:       fc.Status,
			FileType:     detectFileType(fc.Path),
			CommentCount: snap.commentCounts[fc.Path],
		}

		if lf, ok := snap.lazyFiles[fc.Path]; ok {
			fi.Lazy = true
			fi.Additions = lf.LazyAdditions
			fi.Deletions = lf.LazyDeletions
			info.Files = append(info.Files, fi)
			continue
		}

		hunks := scopedHunks(fc, scope, commit, snap.baseRef, snap.repoRoot)
		fi.Additions, fi.Deletions = countHunkStats(hunks)
		info.Files = append(info.Files, fi)
	}

	return info
}

// loadScopedFileState reads file state from the session or disk for scoped diff queries.
func (s *Session) loadScopedFileState(path, scope string) (status, content, baseRef, repoRoot string) {
	s.mu.RLock()
	f := s.fileByPathLocked(path)
	baseRef = s.BaseRef
	repoRoot = s.RepoRoot
	if f != nil {
		status = f.Status
	}
	s.mu.RUnlock()

	if f != nil {
		if err := f.ensureLoaded(repoRoot, baseRef); err == nil {
			s.mu.RLock()
			content = f.Content
			s.mu.RUnlock()
		}
		return status, content, baseRef, repoRoot
	}

	if repoRoot == "" {
		return status, content, baseRef, repoRoot
	}
	absPath := filepath.Join(repoRoot, path)
	if data, err := os.ReadFile(absPath); err == nil {
		content = string(data)
		if changes, err := ChangedFilesScoped(scope, baseRef); err == nil {
			for _, fc := range changes {
				if fc.Path == path {
					status = fc.Status
					break
				}
			}
		}
	}
	return status, content, baseRef, repoRoot
}

func computeScopedDiffHunks(path, scope, commit, status, content, baseRef, repoRoot string) []DiffHunk {
	if commit != "" {
		h, err := FileDiffForCommit(path, commit, repoRoot)
		if err == nil {
			return h
		}
		return nil
	}
	if status == "untracked" && (scope == "unstaged" || scope == "all" || scope == "") {
		return FileDiffUnifiedNewFile(content)
	}
	if status == "added" && scope != "unstaged" {
		return FileDiffUnifiedNewFile(content)
	}
	h, err := FileDiffScoped(path, scope, baseRef, repoRoot)
	if err == nil {
		return h
	}
	return nil
}

// GetFileDiffSnapshotScoped returns diff data for a file filtered by scope.
// When scope is "" or in file mode (scopes only apply to git), delegates to GetFileDiffSnapshot.
// When commit is non-empty, returns the diff for that single commit.
func (s *Session) GetFileDiffSnapshotScoped(path, scope, commit string) (map[string]any, bool) {
	if commit == "" && (scope == "" || scope == "all" || s.Mode == "files" || s.Mode == "plan") {
		return s.GetFileDiffSnapshot(path)
	}

	status, content, baseRef, repoRoot := s.loadScopedFileState(path, scope)

	hunks := computeScopedDiffHunks(path, scope, commit, status, content, baseRef, repoRoot)
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}
