package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoChangedFiles is returned by detectVCSChanges when the working tree and
// branch contain no changes against the base ref (or all changes are filtered
// out by ignore patterns). Callers can detect this via errors.Is to surface a
// user-friendly message instead of a generic init failure.
var ErrNoChangedFiles = errors.New("no changed files detected (after applying ignore patterns)")

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
	UserID    string `json:"user_id,omitempty"`
	CreatedAt string `json:"created_at"`
	// ReviewRound is the review round during which this reply was authored.
	// Used by the per-round timeline to scope reply visibility independently
	// of the parent comment. Legacy replies (no field set) are treated as
	// belonging to the parent's ReviewRound — see commentsAtOrBeforeRound.
	ReviewRound int `json:"review_round,omitempty"`
	// ResolvedRound is the review round during which this reply was resolved
	// (mirrors Comment.ResolvedRound). Currently set only via the parent
	// comment's resolve transitions; reserved for future per-reply resolution.
	ResolvedRound int   `json:"resolved_round,omitempty"`
	GitHubID      int64 `json:"github_id,omitempty"`

	// LastPushedBodyHash is a short stable digest of Body at the time of
	// the most recent successful push (POST or PATCH) to GitHub. Used by
	// `crit push` to detect locally-edited replies that need a PATCH.
	// Empty means "not yet pushed" — divergence detection treats hash("")
	// as the prior value, so a record with GitHubID != 0 and empty hash
	// will PATCH on next push (the local body is canonical).
	LastPushedBodyHash string `json:"last_pushed_body_hash,omitempty"`
}

// Comment represents a single inline review comment.
type Comment struct {
	ID          string `json:"id"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Side        string `json:"side,omitempty"`
	Body        string `json:"body"`
	Quote       string `json:"quote,omitempty"`
	QuoteOffset *int   `json:"quote_offset,omitempty"`
	Anchor      string `json:"anchor,omitempty"`
	Drifted     bool   `json:"drifted,omitempty"`
	Author      string `json:"author,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Scope       string `json:"scope,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Resolved    bool   `json:"resolved,omitempty"`
	// ResolvedRound is the review round during which Resolved transitioned
	// false -> true. Cleared to 0 when Resolved transitions back to false.
	// Legacy comments lacking this field are treated as zero on read; the
	// timeline visibility filter falls back to round 1 for legacy resolved
	// comments (see commentsAtOrBeforeRound docs).
	ResolvedRound  int     `json:"resolved_round,omitempty"`
	Live           bool    `json:"live,omitempty"`
	CarriedForward bool    `json:"carried_forward,omitempty"`
	ReviewRound    int     `json:"review_round,omitempty"`
	Replies        []Reply `json:"replies,omitempty"`
	GitHubID       int64   `json:"github_id,omitempty"`

	// LastPushedBodyHash is a short stable digest of Body at the time of
	// the most recent successful push (POST or PATCH) to GitHub. Used by
	// `crit push` to detect locally-edited comments that need a PATCH.
	// Empty means "not yet pushed" — a record with GitHubID != 0 and empty
	// hash will PATCH on next push (the local body is canonical).
	LastPushedBodyHash string `json:"last_pushed_body_hash,omitempty"`

	// HeadSHA is the head SHA of the focus when this comment was authored.
	// Empty for working-tree comments and for pre-feature comments.
	HeadSHA string `json:"head_sha,omitempty"`

	// DiffScope tags which range scope this comment belongs to:
	// "layer", "full_stack", or "" (working tree / pre-feature).
	// Distinct from Comment.Scope ("line" | "file" | "review").
	DiffScope string `json:"diff_scope,omitempty"`

	// FocusKey identifies the *view* this comment was authored in.
	// Comments are visible only when the current focus's key matches.
	//   ""                            — working-tree / pre-feature
	//   "pr:<num>"                    — range focus with a known PR number
	//   "range:<base_sha>..<head_sha>"     — range focus without PR number (full 40-char SHAs)
	FocusKey string `json:"focus_key,omitempty"`
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
// The vcs parameter is used for computing diffs; pass nil to fall back to
// the git package-level functions (backward compat for tests).
func (fe *FileEntry) ensureLoaded(repoRoot, baseRef string, vcs VCS) error {
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
				fe.loadDiff(ctx, repoRoot, baseRef, vcs)
			}
		}

		fe.Lazy = false
	})
	return fe.loadErr
}

// loadDiff computes diff hunks via the VCS interface or git package-level fallback.
func (fe *FileEntry) loadDiff(ctx context.Context, repoRoot, baseRef string, vcs VCS) {
	if vcs != nil {
		hunks, err := vcs.FileDiffUnifiedCtx(ctx, fe.Path, baseRef, repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: diff failed for %s: %v\n", fe.Path, err)
		} else {
			fe.DiffHunks = hunks
		}
		return
	}
	hunks, err := fileDiffUnifiedCtx(ctx, fe.Path, baseRef, repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", fe.Path, err)
	} else {
		fe.DiffHunks = hunks
	}
}

// Session is the top-level state manager for a multi-file review.
type Session struct {
	VCS            VCS // nil for non-VCS sessions (e.g. files mode without a repo)
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

	// RoundSnapshots is in-memory state populated from <folder>/snapshots.json
	// at boot. Persisted via saveSnapshotsFile; never written into review.json.
	//
	// Lock contract: read/write under s.mu, EXCEPT during construction
	// (NewSessionFromFiles, before SetSession) where the caller is the only
	// goroutine that could observe it.
	RoundSnapshots map[string]map[int]RoundSnapshot

	// sessionStarted is set (atomically) by Server.SetSession to mark the
	// transition from constructor-time (single-goroutine) to runtime
	// (multi-goroutine). loadCritJSON checks this flag to enforce its
	// pre-SetSession-only contract. 0 = pre-SetSession, 1 = post-SetSession.
	sessionStarted atomic.Uint32

	// deletedCommentIDs tracks IDs of file comments deleted in-memory but not
	// yet written to disk. Keyed by file path -> set of comment IDs. This
	// prevents mergeFileSnapshotIntoCritJSON from re-adding them from disk.
	deletedCommentIDs map[string]map[string]struct{}

	// pendingGitHubDeletes holds GitHub comment IDs (root or reply) that the
	// user has deleted locally and that the next `crit push` must DELETE
	// upstream. Persisted as CritJSON.PendingGitHubDeletes; the next push
	// drains entries as each DELETE succeeds (or returns 404 / 403).
	pendingGitHubDeletes []int64

	// lastLoadedPendingGHDeletes is the set of GitHub IDs that were on disk
	// in PendingGitHubDeletes the last time the daemon read or wrote
	// review.json. Used by buildCritJSON to reconcile the in-memory snapshot
	// against the on-disk queue: an ID present in the snapshot but missing
	// from disk AND present in this set means a separate `crit push` process
	// drained it — it must NOT be resurrected. See BLOCKER #1.
	lastLoadedPendingGHDeletes map[int64]struct{}

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

	// Focus is the discriminator that selects what the session is showing.
	// Defaults to FocusWorkingTree (today's behavior). Range-mode sessions
	// (--pr / --range) populate this with FocusRange and the SHAs.
	// Read/write under s.mu — see GetSessionInfoScoped.
	Focus Focus

	// LastRangeFocus is the most recent FocusRange the session was on, set
	// every time SetFocus transitions OUT of range mode (e.g. user clicks
	// "Working tree"). Surfaced via /api/session so the frontend can render
	// a "Resume PR #N" pill in working-tree mode.
	LastRangeFocus *Focus

	// RemoteFiles, when true, routes file content reads in range focus
	// through the GitHub API (gh api repos/.../contents/?ref=<sha>) instead
	// of `git show <sha>:<path>`. Diffs and changed-file lists still use
	// local git. Set from --remote at startup; not mutated afterwards.
	RemoteFiles bool

	// remoteFileCache memoizes (sha,path) -> []byte to avoid repeat API
	// calls within a single session. Keyed by sha + "\x00" + path.
	// Bounded LRU; see remoteFileCacheCap.
	remoteFileCache *bytesLRU
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
	CliArgs        []string                `json:"cli_args,omitempty"`
	Files          map[string]CritJSONFile `json:"files"`

	// ActiveDiffScope is the most recent focus diff_scope from this session.
	// Read by `crit push` to gate full-stack pushes; "" indicates working-tree mode.
	ActiveDiffScope string `json:"active_diff_scope,omitempty"`

	// PendingGitHubDeletes holds GitHub comment IDs (root or reply — same
	// /pulls/comments/{id} endpoint) that the user has deleted locally and
	// that need a DELETE upstream on the next `crit push`. Drained as each
	// DELETE succeeds (or returns 404 / 403). Survives intermediate pulls so
	// the user's intent is not lost.
	PendingGitHubDeletes []int64 `json:"pending_github_deletes,omitempty"`
}

// CritJSONFile is the per-file section in review files.
type CritJSONFile struct {
	Status   string    `json:"status"`
	FileHash string    `json:"file_hash"`
	Comments []Comment `json:"comments"`
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
// The vcs parameter is used for computing diffs; pass nil to fall back to
// the git package-level functions.
func populateEagerFile(fe *FileEntry, fc FileChange, baseRef, root string, vcs VCS) bool {
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
			populateEagerFileDiff(fe, fc, baseRef, root, vcs)
		}
	}

	fe.Comments = []Comment{}
	return true
}

// populateEagerFileDiff computes diff hunks for an eager-loaded file.
func populateEagerFileDiff(fe *FileEntry, fc FileChange, baseRef, root string, vcs VCS) {
	if vcs != nil {
		hunks, err := vcs.FileDiffUnified(fc.Path, baseRef, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: diff failed for %s: %v\n", fc.Path, err)
		} else {
			fe.DiffHunks = hunks
		}
		return
	}
	hunks, err := fileDiffUnified(fc.Path, baseRef, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", fc.Path, err)
	} else {
		fe.DiffHunks = hunks
	}
}

// NewSessionFromGit creates a session by auto-detecting changed files via git.
// It delegates to NewSessionFromVCS with a GitVCS backend.
// Retained for test compatibility; production code uses NewSessionFromVCS directly.
func NewSessionFromGit(ignorePatterns []string) (*Session, error) {
	return NewSessionFromVCS(&GitVCS{}, ignorePatterns)
}

// detectVCSChanges resolves the base ref and returns the list of changed files using the VCS interface.
func detectVCSChanges(vcs VCS, root string, ignorePatterns []string) (branch, baseRef, resolvedBase string, changes []FileChange, err error) {
	branch = vcs.CurrentBranch()
	resolvedBase = vcs.DefaultBranch()
	if branch != resolvedBase {
		baseRef, _ = vcs.MergeBase(vcs.DefaultBaseRef())
	}

	if baseRef != "" {
		changes, err = vcs.ChangedFilesFromBaseInDir(baseRef, root)
	} else {
		changes, err = vcs.ChangedFilesOnDefaultInDir(root)
	}
	if err != nil {
		return "", "", "", nil, fmt.Errorf("detecting changes: %w", err)
	}
	changes = filterIgnored(changes, ignorePatterns)

	if len(changes) == 0 {
		return "", "", "", nil, ErrNoChangedFiles
	}
	return branch, baseRef, resolvedBase, changes, nil
}

// NewSessionFromVCS creates a session by auto-detecting changed files using the given VCS backend.
// This is the VCS-agnostic equivalent of NewSessionFromGit.
func NewSessionFromVCS(vcs VCS, ignorePatterns []string) (*Session, error) {
	return newGitSession(vcs, ignorePatterns, true)
}

// newGitSession is the implementation behind NewSessionFromVCS. When
// requireChanges is false, an empty working-tree diff produces a session with
// no files instead of returning ErrNoChangedFiles — callers that apply a
// --pr/--range focus rebuild the file list via SetFocus, so the working-tree
// diff is irrelevant. See issue #471.
func newGitSession(vcs VCS, ignorePatterns []string, requireChanges bool) (*Session, error) {
	root, err := vcs.RepoRoot()
	if err != nil {
		return nil, fmt.Errorf("not a %s repository: %w", vcs.Name(), err)
	}

	branch, baseRef, resolvedBase, changes, err := detectVCSChanges(vcs, root, ignorePatterns)
	if errors.Is(err, ErrNoChangedFiles) && !requireChanges {
		// detectVCSChanges zeroes its return values on the empty path; recover
		// the metadata so the session reports the correct branch/base.
		branch = vcs.CurrentBranch()
		resolvedBase = vcs.DefaultBranch()
		if branch != resolvedBase {
			baseRef, _ = vcs.MergeBase(vcs.DefaultBaseRef())
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}

	s := &Session{
		VCS:                 vcs,
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
		Focus:               Focus{Kind: FocusWorkingTree, BaseRef: baseRef, BaseBranchName: resolvedBase},
	}

	var numstats map[string]NumstatEntry
	if len(changes) > lazyFileThreshold && baseRef != "" {
		numstats, _ = vcs.DiffNumstat(baseRef, root)
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
		} else if !populateEagerFile(fe, fc, baseRef, root, vcs) {
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
			expandedPaths = append(expandedPaths, walkDirectory(absPath, ignorePatterns)...)
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

// resolveGitContext returns VCS repo state for file-mode sessions.
func resolveGitContext() (root, branch, baseRef, baseBranchName string, vcs VCS) {
	vcs = DetectVCS("")
	if vcs == nil {
		return "", "", "", "", nil
	}
	root, _ = vcs.RepoRoot()
	branch = vcs.CurrentBranch()
	resolvedBase := vcs.DefaultBranch()
	baseBranchName = resolvedBase
	if branch != resolvedBase {
		baseRef, _ = vcs.MergeBase(vcs.DefaultBaseRef())
	}
	return root, branch, baseRef, baseBranchName, vcs
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

	root, branch, baseRef, baseBranchName, vcs := resolveGitContext()
	if root == "" {
		root = filepath.Dir(expandedPaths[0])
	}

	s := &Session{
		VCS:                 vcs,
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
		Focus:               Focus{Kind: FocusWorkingTree, BaseRef: baseRef, BaseBranchName: baseBranchName},
	}

	for _, absPath := range expandedPaths {
		relPath := absPath
		if root != "" {
			if rel, err := filepath.Rel(root, absPath); err == nil {
				slash := filepath.ToSlash(rel)
				// On Windows filepath.Rel can succeed across drives /
				// short-name boundaries with a useless ../../../... result;
				// fall back to the absolute path in that case so downstream
				// (git diff, picker labels) at least sees a stable string.
				if strings.HasPrefix(slash, "../") || slash == ".." {
					relPath = filepath.ToSlash(absPath)
				} else {
					// Stored in review JSON (cross-platform artefact) and used as
					// the file key — keep separators POSIX on every host OS.
					relPath = slash
				}
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

		if vcs != nil {
			hunks, diffErr := vcs.FileDiffUnified(relPath, baseRef, root)
			if diffErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: diff failed for %s: %v\n", relPath, diffErr)
			} else {
				fe.DiffHunks = hunks
			}
		}

		s.Files = append(s.Files, fe)
	}

	s.captureBaselineAndPersist()

	return s, nil
}

// captureBaselineAndPersist captures the R1 baseline (idempotent so resumed
// sessions are unaffected) and best-effort writes the sidecar to disk.
//
// Skips the sidecar write when the identity would fall back to RepoRoot —
// this path runs before applySessionOverrides has a chance to assign
// ReviewFilePath / OutputDir. The first WriteFiles or round-complete will
// re-emit the sidecar at the canonical centralized path.
//
// Pre-SetSession only — caller is the constructor and no concurrent readers
// exist. See plan v4 §Lock discipline.
func (s *Session) captureBaselineAndPersist() {
	s.captureRoundSnapshot(s.ReviewRound)
	if s.ReviewFilePath == "" && s.OutputDir == "" {
		return
	}
	if len(s.RoundSnapshots) == 0 {
		return
	}
	identity := s.critJSONPath()
	// MIGRATION-REMOVAL: ensure folder layout up front so a stale flat
	// review file doesn't fail the sidecar write below.
	if err := ensureReviewFolder(identity); err != nil {
		return
	}
	sidecar := reviewPathsFor(identity).Snapshots
	if err := saveSnapshotsFile(sidecar, SnapshotsFile{
		RoundSnapshots: cloneRoundSnapshots(s.RoundSnapshots),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: write snapshots sidecar at construction: %v\n", err)
	}
}

// walkDirectory recursively walks a directory and returns all file paths,
// skipping hidden directories and common non-text directories.
//
// Ordering: at each depth, recurse into subdirectories (alphabetical) before
// listing files (alphabetical). This matches the "directories before files at
// each depth" grouping users expect when passing a directory like `crit .` —
// and the frontend now preserves backend order in files mode, so this is the
// single source of truth for display order.
func walkDirectory(dir string, ignorePatterns []string) []string {
	var files []string
	walkDirSubsFirst(dir, dir, ignorePatterns, &files)
	return files
}

// walkDirSubsFirst is the recursive helper for walkDirectory. It reads dir
// (os.ReadDir returns entries sorted by name), recurses into subdirectories
// first, then appends files. ignorePatterns are matched relative to root
// (the original argument), not the current dir. Best-effort: inaccessible
// directories are silently skipped.
func walkDirSubsFirst(dir, root string, ignorePatterns []string, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // best-effort: skip inaccessible dirs
	}
	var subdirs, fileEntries []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			if skipDirs[name] {
				continue
			}
			subdirs = append(subdirs, e)
			continue
		}
		lowerName := strings.ToLower(name)
		if strings.HasSuffix(lowerName, ".min.js") || strings.HasSuffix(lowerName, ".min.css") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if isBinaryExtension(ext) {
			continue
		}
		full := filepath.Join(dir, name)
		if relPath, relErr := filepath.Rel(root, full); relErr == nil {
			skipped := false
			for _, pat := range ignorePatterns {
				if matchPattern(pat, relPath) {
					skipped = true
					break
				}
			}
			if skipped {
				continue
			}
		}
		fileEntries = append(fileEntries, e)
	}
	for _, d := range subdirs {
		walkDirSubsFirst(filepath.Join(dir, d.Name()), root, ignorePatterns, out)
	}
	for _, f := range fileEntries {
		*out = append(*out, filepath.Join(dir, f.Name()))
	}
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
func (s *Session) AddComment(filePath string, startLine, endLine int, side, body, quote, author, userID string) (Comment, bool) {
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
		var baseContent string
		if s.VCS != nil {
			baseContent, _ = s.VCS.FileContentAtRef(filePath, s.BaseRef, s.RepoRoot)
		} else {
			baseContent = fileContentAtRef(filePath, s.BaseRef, s.RepoRoot)
		}
		anchor = extractAnchor(baseContent, startLine, endLine)
	} else {
		anchor = extractAnchor(f.Content, startLine, endLine)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	c := stampWithFocus(Comment{
		ID:          randomCommentID(),
		StartLine:   startLine,
		EndLine:     endLine,
		Side:        side,
		Body:        body,
		Quote:       quote,
		Anchor:      anchor,
		Author:      author,
		UserID:      userID,
		Scope:       "line",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}, s.Focus)
	f.Comments = append(f.Comments, c)
	s.scheduleWrite()
	return c, true
}

// AddFileComment adds a file-level comment (not tied to specific lines).
func (s *Session) AddFileComment(filePath, body, author, userID string) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return Comment{}, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c := stampWithFocus(Comment{
		ID:          randomCommentID(),
		Body:        body,
		Author:      author,
		UserID:      userID,
		Scope:       "file",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}, s.Focus)
	f.Comments = append(f.Comments, c)
	s.scheduleWrite()
	return c, true
}

// AddReviewComment adds a review-level comment (not tied to any file).
func (s *Session) AddReviewComment(body, author, userID string) Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	c := stampWithFocus(Comment{
		ID:          randomReviewCommentID(),
		Body:        body,
		Author:      author,
		UserID:      userID,
		Scope:       "review",
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: s.ReviewRound,
	}, s.Focus)
	s.reviewComments = append(s.reviewComments, c)
	s.scheduleWrite()
	return c
}

// GetReviewComments returns a copy of all review-level comments visible
// in the current focus, with staleness annotated.
func (s *Session) GetReviewComments() []Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Comment, 0, len(s.reviewComments))
	for _, c := range s.reviewComments {
		if !visibleInFocus(c, s.Focus) {
			continue
		}
		out = append(out, c)
	}
	return out
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

// DeleteReviewComment deletes a review-level comment by ID. Review-level
// comments are local-only (not synced to GitHub PR review comments), so a
// straight removal is safe — there is no upstream record to tombstone.
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
// On a false -> true transition, ResolvedRound is stamped from s.ReviewRound.
// On a true -> false transition, ResolvedRound is cleared to 0.
func (s *Session) ResolveReviewComment(id string, resolved bool) (Comment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == id {
			s.reviewComments[i].Resolved = resolved
			if resolved {
				s.reviewComments[i].ResolvedRound = s.ReviewRound
			} else {
				s.reviewComments[i].ResolvedRound = 0
			}
			s.reviewComments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			s.scheduleWrite()
			return s.reviewComments[i], true
		}
	}
	return Comment{}, false
}

// AddReviewCommentReply adds a reply to a review-level comment.
func (s *Session) AddReviewCommentReply(commentID, body, author, userID string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.reviewComments {
		if c.ID == commentID {
			now := time.Now().UTC().Format(time.RFC3339)
			r := Reply{
				ID:          randomReplyID(),
				Body:        body,
				Author:      author,
				UserID:      userID,
				CreatedAt:   now,
				ReviewRound: s.ReviewRound,
			}
			s.reviewComments[i].Replies = append(s.reviewComments[i].Replies, r)
			s.reviewComments[i].Resolved = false
			s.reviewComments[i].ResolvedRound = 0
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
// Review-level threads are local-only, so a straight removal is safe.
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
// On a false -> true transition, ResolvedRound is stamped from s.ReviewRound.
// On a true -> false transition, ResolvedRound is cleared to 0.
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
			if resolved {
				f.Comments[i].ResolvedRound = s.ReviewRound
			} else {
				f.Comments[i].ResolvedRound = 0
			}
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

// DeleteComment deletes a comment from a specific file. The record is always
// spliced out locally. If it has been pushed to GitHub (GitHubID != 0) the
// GitHub ID is appended to pendingGitHubDeletes so the next `crit push` can
// issue DELETE upstream — without that, deleting a pushed comment would leave
// a ghost on the PR. trackDeletedComment guards against a concurrent reload
// from disk resurrecting the just-removed entry before the next save.
func (s *Session) DeleteComment(filePath, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return false
	}
	for i, c := range f.Comments {
		if c.ID == id {
			if c.GitHubID != 0 {
				s.appendPendingGHDelete(c.GitHubID)
			}
			f.Comments = append(f.Comments[:i], f.Comments[i+1:]...)
			s.trackDeletedComment(filePath, id)
			s.scheduleWrite()
			return true
		}
	}
	return false
}

// appendPendingGHDelete adds a GitHub comment ID to the pending-deletes list
// if it isn't already present. Caller must hold s.mu.
func (s *Session) appendPendingGHDelete(ghID int64) {
	if ghID == 0 {
		return
	}
	for _, existing := range s.pendingGitHubDeletes {
		if existing == ghID {
			return
		}
	}
	s.pendingGitHubDeletes = append(s.pendingGitHubDeletes, ghID)
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
func (s *Session) AddReply(filePath, commentID, body, author, userID string) (Reply, bool) {
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
				ID:          randomReplyID(),
				Body:        body,
				Author:      author,
				UserID:      userID,
				CreatedAt:   now,
				ReviewRound: s.ReviewRound,
			}
			f.Comments[i].Replies = append(f.Comments[i].Replies, r)
			f.Comments[i].Resolved = false
			f.Comments[i].ResolvedRound = 0
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

// DeleteReply removes a reply from a specific comment. Always spliced out
// locally; replies that have been pushed (GitHubID != 0) get their GitHub ID
// queued in pendingGitHubDeletes so the next `crit push` can DELETE them
// upstream (replies share /pulls/comments/{id} with root comments).
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
					if r.GitHubID != 0 {
						s.appendPendingGHDelete(r.GitHubID)
					}
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

// GetComments returns comments for a specific file that are visible in the
// current focus, with staleness annotated.
func (s *Session) GetComments(filePath string) []Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.fileByPathLocked(filePath)
	if f == nil {
		return []Comment{}
	}
	result := make([]Comment, 0, len(f.Comments))
	for _, c := range f.Comments {
		if !visibleInFocus(c, s.Focus) {
			continue
		}
		if len(c.Replies) > 0 {
			rs := make([]Reply, len(c.Replies))
			copy(rs, c.Replies)
			c.Replies = rs
		}
		result = append(result, c)
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
	vcs := s.VCS
	s.mu.RUnlock()

	if repoRoot == "" {
		return false
	}

	absPath := filepath.Join(repoRoot, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}

	// Determine the file's VCS status via a single-file diff against baseRef
	// (avoids running full ChangedFiles which diffs ALL files).
	status := "modified"
	if vcs != nil {
		status = vcs.FileStatusInRepo(path, baseRef, repoRoot)
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
	} else if status != "deleted" && vcs != nil {
		if hunks, err := vcs.FileDiffUnified(path, baseRef, repoRoot); err == nil {
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
		// Drop the v4 review folder (`.crit`) and the legacy v3 flat file
		// (`.crit.json`) so they never appear in the file list.
		if base := filepath.Base(f.Path); base == ".crit" || base == ".crit.json" || f.Orphaned {
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
	s.RoundSnapshots = nil // v4 lock-discipline: reset in-lock alongside ReviewRound
	s.lastCritJSONMtime = time.Time{}
	s.pendingWrite = false
	s.waitingForAgent = false
	critPath := s.critJSONPath()
	s.mu.Unlock()
	// Full-folder cleanup; idempotent on missing folder.
	if err := os.RemoveAll(critPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: removing review folder: %v\n", err)
	}
}

// ChangeBaseBranch changes the diff base to the given branch, recomputes merge-base,
// rebuilds the file list with new diffs, and notifies connected browsers via SSE.
// Comments are preserved for files that still appear in the new diff.
func (s *Session) ChangeBaseBranch(branch string) error { //nolint:gocyclo // inherent complexity: rollback, recompute diffs, preserve comments
	s.mu.RLock()
	mode := s.Mode
	vcs := s.VCS
	s.mu.RUnlock()
	if mode != "git" {
		return fmt.Errorf("base branch can only be changed in git mode")
	}
	if vcs == nil {
		return fmt.Errorf("no VCS available")
	}

	// Compute merge-base with the new branch (try both local and remote ref)
	mb, err := vcs.MergeBase(branch)
	if err != nil {
		mb, err = vcs.MergeBase("origin/" + branch)
		if err != nil {
			return fmt.Errorf("cannot compute merge-base with %s: %w", branch, err)
		}
	}

	// Save old state for rollback
	oldOverride := vcs.GetDefaultBranchOverride()

	// Update the override so ChangedFiles() uses the new base
	vcs.SetDefaultBranchOverride(branch)

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
		changes, err = vcs.ChangedFilesFromBaseInDir(mb, repoRoot)
	} else {
		changes, err = vcs.ChangedFilesOnDefaultInDir(repoRoot)
	}
	if err != nil {
		// Rollback all state
		vcs.SetDefaultBranchOverride(oldOverride)
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
			if hunks, diffErr := vcs.FileDiffUnified(fc.Path, mb, repoRoot); diffErr == nil {
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

// reportLoadCritJSONLockViolation surfaces a post-SetSession loadCritJSON
// call. In production it logs to stderr and returns so the daemon stays up;
// in dev/CI (CRIT_DEBUG set) it panics so the regression fails loudly. See
// plan v4 §Lock discipline.
func reportLoadCritJSONLockViolation() {
	const msg = "BUG: Session.loadCritJSON called post-SetSession; ignoring (see plan v4 §Lock discipline)"
	if os.Getenv("CRIT_DEBUG") != "" {
		panic(msg)
	}
	fmt.Fprintln(os.Stderr, msg)
}

// loadCritJSON loads comments and share state from an existing review file.
//
// Lock contract: PRE-SETSESSION ONLY. Safe to call only from the constructor
// path (NewSessionFromFiles, applySessionOverrides, etc.) before any goroutine
// reads s.RoundSnapshots / s.reviewComments / etc. Runtime callers that hold
// s.mu.Lock() must use loadCritJSONLocked instead. See plan v4 §Lock discipline.
//
// Acquires s.mu.Lock() defensively: even pre-SetSession, a prior mutation
// (e.g. AddReviewComment in a test) may have armed scheduleWrite's debounced
// AfterFunc, which reads s.lastCritJSONMtime under RLock from a separate
// goroutine. Stopping the timer first short-circuits the common case; the
// lock covers the in-flight case.
func (s *Session) loadCritJSON() {
	if s.sessionStarted.Load() != 0 {
		reportLoadCritJSONLockViolation()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.loadCritJSONLocked()
}

// restoreShareStateLocked copies share-related fields from cj into the
// session, gated by share-scope match so we don't carry over a share
// pointer when the file set has changed since the share was created.
// Caller must hold s.mu.Lock() (or be the constructor pre-SetSession).
func (s *Session) restoreShareStateLocked(cj *CritJSON) {
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
		return
	}
	if cj.ShareURL != "" {
		// No scope recorded — load unconditionally.
		s.sharedURL = cj.ShareURL
		s.deleteToken = cj.DeleteToken
	}
}

// restoreFileCommentsLocked copies per-file comments from cj into matching
// FileEntry slots, defaulting empty Scope to "line" for legacy comments.
// Caller must hold s.mu.Lock() (or be the constructor pre-SetSession).
func (s *Session) restoreFileCommentsLocked(cj *CritJSON) {
	for _, f := range s.Files {
		cf, ok := cj.Files[f.Path]
		if !ok {
			continue
		}
		f.Comments = cf.Comments
		for i := range f.Comments {
			if f.Comments[i].Scope == "" {
				f.Comments[i].Scope = "line"
			}
		}
	}
}

// loadCritJSONLocked is the runtime variant of loadCritJSON. It performs the
// same disk read + in-memory restore but skips the pre-SetSession guard so
// runtime code paths can reload comments after a state change (e.g. SetFocus
// rebuilds s.Files and needs to repopulate per-file Comments from disk).
//
// Lock contract: caller MUST hold s.mu.Lock() (writer lock). The function
// mutates s.Files[*].Comments, s.reviewComments, s.ReviewRound,
// s.sharedURL/deleteToken/shareScope, and s.lastCritJSONMtime, all of which
// race with concurrent readers under s.mu.RLock(). The pre-SetSession path
// (loadCritJSON) gets away without the lock because no other goroutine has
// observed the session yet.
func (s *Session) loadCritJSONLocked() {
	identity := s.critJSONPath()

	// Capture identity-on-entry. If ReviewFilePath / OutputDir were set
	// BEFORE this call (the canonical resumed-session path in cli_serve),
	// the on-disk sidecar is already authoritative and we don't need to
	// rewrite it from in-memory state. Used downstream to skip a
	// redundant O(N*M) clone+marshal+rename on every cold boot.
	identityOnEntry := s.ReviewFilePath != "" || s.OutputDir != ""

	// MIGRATION-REMOVAL: trigger v3->v4 folder migration on read.
	if err := ensureReviewFolder(identity); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: review folder migration: %v\n", err)
	}

	data, err := os.ReadFile(reviewPathsFor(identity).Review)
	if err != nil {
		// Fall through to the sidecar load — a folder may exist with only
		// snapshots.json (orphan-snapshots) and we still want to surface that.
		sidecarHadData := s.loadSnapshotsFromSidecar(identity)
		// Persist the in-memory R1 baseline that NewSessionFromFiles captured
		// in case ReviewFilePath / OutputDir was assigned just before this
		// call (the canonical constructor-time path).
		//
		// Resumed-session optimization (review W5): when the sidecar already
		// carried snapshots and identity was set on entry, the on-disk data
		// is authoritative — skip the redundant rewrite.
		if identityOnEntry && sidecarHadData {
			s.captureRoundSnapshot(s.ReviewRound)
			return
		}
		s.captureBaselineAndPersist()
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}

	s.restoreShareStateLocked(&cj)

	// Restore review round so the session continues from where it left off.
	if cj.ReviewRound > s.ReviewRound {
		s.ReviewRound = cj.ReviewRound
	}

	s.restoreFileCommentsLocked(&cj)

	// Detect orphaned paths: files in the review file with comments but not in the session.
	s.appendOrphanedFiles(cj.Files)

	// Restore review-level comments.
	s.reviewComments = cj.ReviewComments

	// Restore pending DELETE intents so they survive across daemon restarts.
	s.pendingGitHubDeletes = cj.PendingGitHubDeletes
	s.lastLoadedPendingGHDeletes = make(map[int64]struct{}, len(cj.PendingGitHubDeletes))
	for _, id := range cj.PendingGitHubDeletes {
		s.lastLoadedPendingGHDeletes[id] = struct{}{}
	}

	// Record the mtime so the first ticker tick doesn't re-process our own file.
	if info, err := os.Stat(reviewPathsFor(s.critJSONPath()).Review); err == nil {
		s.lastCritJSONMtime = info.ModTime()
	}

	// Restore round snapshots from the folder sidecar.
	sidecarHadData := s.loadSnapshotsFromSidecar(s.critJSONPath())

	// If ReviewFilePath / OutputDir was assigned just before this call (the
	// canonical constructor-time path in cli_serve), the in-memory R1 baseline
	// captured by NewSessionFromFiles hasn't been persisted yet. Re-run the
	// best-effort persist now that the identity is known.
	//
	// Optimization (review W5): for a resumed session — identity already
	// known on entry AND the sidecar carried a non-empty snapshot map — the
	// on-disk sidecar is authoritative and rewriting it is redundant
	// (O(N*M) clone+marshal+rename on every cold boot). The capture itself
	// remains idempotent so we still call captureRoundSnapshot to keep R1
	// well-defined in memory; we just skip the disk write.
	if identityOnEntry && sidecarHadData {
		s.captureRoundSnapshot(s.ReviewRound)
		return
	}
	s.captureBaselineAndPersist()
}

// loadSnapshotsFromSidecar restores Session.RoundSnapshots from
// <identity>/snapshots.json. Missing file = silent empty map. Malformed = log
// + fall through (next round-complete rewrites it). Returns true when the
// sidecar carried at least one snapshot (i.e. this is a resumed session).
//
// Lock contract: pre-SetSession or under s.mu.Lock(). Mutates s.RoundSnapshots.
func (s *Session) loadSnapshotsFromSidecar(identity string) bool {
	sidecarPath := reviewPathsFor(identity).Snapshots
	sf, err := loadSnapshotsFile(sidecarPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: snapshots sidecar unreadable, ignoring: %v\n", err)
		return false
	}
	if len(sf.RoundSnapshots) == 0 {
		return false
	}
	s.RoundSnapshots = cloneRoundSnapshots(sf.RoundSnapshots)
	return true
}

// restoreOrphanedComments reads the review file and creates phantom FileEntry
// objects for any paths that have comments but aren't in s.Files.
// Safe to call multiple times — existing entries (including previous orphans) are skipped.
// Must be called with s.mu NOT held (acquires the lock internally).
func (s *Session) restoreOrphanedComments() {
	data, err := os.ReadFile(reviewPathsFor(s.critJSONPath()).Review)
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
	vcs := s.VCS
	s.mu.RUnlock()

	// Load content on demand for lazy files
	if err := f.ensureLoaded(repoRoot, baseRef, vcs); err != nil {
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
	vcs := s.VCS
	s.mu.RUnlock()

	// Load content + diffs on demand for lazy files
	if err := f.ensureLoaded(repoRoot, baseRef, vcs); err != nil {
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
	VCSName         string            `json:"vcs_name,omitempty"`
	Branch          string            `json:"branch"`
	BaseRef         string            `json:"base_ref"`
	BaseBranchName  string            `json:"base_branch_name,omitempty"`
	ReviewRound     int               `json:"review_round"`
	AvailableScopes []string          `json:"available_scopes"`
	Files           []SessionFileInfo `json:"files"`
	ReviewComments  []Comment         `json:"review_comments"`
	Cwd             string            `json:"cwd,omitempty"`
	Focus           Focus             `json:"focus"`
	LastRangeFocus  *Focus            `json:"last_range_focus,omitempty"`
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

	reviewComments := make([]Comment, 0, len(s.reviewComments))
	for _, c := range s.reviewComments {
		if !visibleInFocus(c, s.Focus) {
			continue
		}
		reviewComments = append(reviewComments, c)
	}

	var vcsName string
	if s.VCS != nil {
		vcsName = s.VCS.Name()
	}

	vcs := s.VCS

	info := SessionInfo{
		Mode:           s.Mode,
		VCSName:        vcsName,
		Branch:         s.Branch,
		BaseRef:        s.BaseRef,
		BaseBranchName: s.BaseBranchName,
		ReviewRound:    s.ReviewRound,
		ReviewComments: reviewComments,
		Cwd:            s.RepoRoot,
		Focus:          s.Focus,
		LastRangeFocus: s.LastRangeFocus,
	}

	info.AvailableScopes = cachedAvailableScopes(info.BaseRef, vcs)

	for _, f := range s.Files {
		visibleCount := countVisibleComments(f.Comments, s.Focus)
		// Orphaned files are surfaced solely to preserve user comments
		// across focus changes. When *no* comment on the orphan is
		// visible in the current focus, the entry is just noise — drop
		// it so navigating between layers/scopes doesn't litter the
		// file list with phantom rows from unrelated focuses.
		if f.Orphaned && visibleCount == 0 {
			continue
		}
		fi := SessionFileInfo{
			Path:         f.Path,
			Status:       f.Status,
			FileType:     f.FileType,
			CommentCount: visibleCount,
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
