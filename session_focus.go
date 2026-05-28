package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FocusKind tags which arm of Focus is populated.
type FocusKind string

const (
	// FocusWorkingTree is the default — diff working tree against base ref.
	FocusWorkingTree FocusKind = "working_tree"
	// FocusRange diffs a fixed (BaseSHA, HeadSHA) range, used by --pr / --range.
	FocusRange FocusKind = "range"
)

// DiffScope selects which range to diff in FocusRange mode.
//
//	layer      — BaseSHA..HeadSHA  (what GitHub shows for the PR)
//	full_stack — DefaultSHA..HeadSHA (cumulative from default branch)
//
// Empty string is the implicit "no scope" used by FocusWorkingTree comments
// authored before this feature shipped.
type DiffScope string

const (
	// DiffScopeLayer is the per-PR layer (BaseSHA..HeadSHA).
	DiffScopeLayer DiffScope = "layer"
	// DiffScopeFullStack is the cumulative diff from the default branch.
	DiffScopeFullStack DiffScope = "full_stack"
)

// Focus is what the session is currently showing. Exactly one arm is meaningful
// per Kind; the other fields are zero. No interface — keep it serializable for
// /api/session, comparable, and trivially copyable.
type Focus struct {
	Kind FocusKind `json:"kind"`

	// FocusWorkingTree fields.
	BaseRef        string `json:"base_ref,omitempty"`
	BaseBranchName string `json:"base_branch_name,omitempty"`

	// FocusRange fields. All optional except BaseSHA + HeadSHA.
	PRNumber    int       `json:"pr_number,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	Label       string    `json:"label,omitempty"`
	BaseSHA     string    `json:"base_sha,omitempty"`
	HeadSHA     string    `json:"head_sha,omitempty"`
	DefaultSHA  string    `json:"default_sha,omitempty"`
	ForkURL     string    `json:"fork_url,omitempty"`
	BaseRefName string    `json:"base_ref_name,omitempty"`
	HeadRefName string    `json:"head_ref_name,omitempty"`
	DiffScope   DiffScope `json:"diff_scope,omitempty"`
	IsStacked   bool      `json:"is_stacked,omitempty"`
}

// ReadOnly reports whether comments may be added/edited in this focus.
// v1: always false. Range mode is fully writable so users can annotate;
// pushes to GitHub are gated separately (see runPush).
func (f Focus) ReadOnly() bool { return false }

// DiffBaseSHA returns the SHA to use as the diff base for the current scope.
// In full-stack scope without a resolved DefaultSHA, falls back to BaseSHA;
// callers that explicitly require full-stack must validate DefaultSHA upstream.
func (f Focus) DiffBaseSHA() string {
	if f.Kind != FocusRange {
		return f.BaseRef
	}
	if f.DiffScope == DiffScopeFullStack && f.DefaultSHA != "" {
		return f.DefaultSHA
	}
	return f.BaseSHA
}

// FullStackAvailable reports whether the full-stack scope can be selected.
// False when DefaultSHA could not be resolved (detached HEAD, no remote, etc.).
func (f Focus) FullStackAvailable() bool {
	return f.Kind == FocusRange && f.DefaultSHA != ""
}

// PickerVisible reports whether the layer/full-stack picker should render.
// Hide when the PR is not stacked (base IS the default branch), because layer
// and full-stack would produce identical diffs.
func (f Focus) PickerVisible() bool {
	return f.Kind == FocusRange && f.IsStacked
}

// focusKeyFor returns the per-view key used to scope comment visibility.
//
//	pr:<num>                       — range focus with PR number
//	range:<baseSHA>..<headSHA>     — range focus without PR number (full 40-char SHAs)
//	""                             — working-tree (and unknown)
func focusKeyFor(f Focus) string {
	if f.Kind != FocusRange {
		return ""
	}
	if f.PRNumber > 0 {
		return fmt.Sprintf("pr:%d", f.PRNumber)
	}
	return fmt.Sprintf("range:%s..%s", f.BaseSHA, f.HeadSHA)
}

// visibleInFocus reports whether c should be shown in the given focus.
// Comments belong to the *view* they were authored in, identified by
// FocusKey. Within a range focus, the layer/full-stack DiffScope filter
// also applies. Pure function — no I/O, no locks.
func visibleInFocus(c Comment, f Focus) bool {
	if c.FocusKey != focusKeyFor(f) {
		return false
	}
	if f.Kind == FocusRange {
		return c.DiffScope == string(f.DiffScope)
	}
	return c.DiffScope == ""
}

// stampWithFocus copies focus-derived metadata onto a freshly authored Comment.
// No-op when Focus.Kind != FocusRange, preserving working-tree behavior.
func stampWithFocus(c Comment, f Focus) Comment {
	c.FocusKey = focusKeyFor(f)
	if f.Kind == FocusRange {
		c.HeadSHA = f.HeadSHA
		c.DiffScope = string(f.DiffScope)
	}
	return c
}

// countVisibleComments returns the count of comments visible in the given focus.
func countVisibleComments(comments []Comment, f Focus) int {
	n := 0
	for _, c := range comments {
		if visibleInFocus(c, f) {
			n++
		}
	}
	return n
}

// SetFocus atomically swaps the session's Focus and rebuilds the file list.
// On any failure during rebuild or persistence, the previous Focus + Files
// are restored in memory; disk state remains consistent because the only
// disk write between snapshot and rollback is persistActiveDiffScope, and
// rollback runs only when that write fails (saveCritJSON uses atomic rename,
// so it's all-or-nothing — no torn ActiveDiffScope on disk).
//
// Caller is responsible for validating the request shape upstream;
// SetFocus owns SHA validation (via ensureSHAFetched) and persistence.
func (s *Session) SetFocus(f Focus) error {
	if f.Kind == FocusRange &&
		f.DiffScope == DiffScopeFullStack &&
		f.DefaultSHA == "" {
		return fmt.Errorf("full-stack scope requires a resolvable default branch tip")
	}

	s.mu.RLock()
	repoRoot := s.RepoRoot
	vcs := s.VCS
	remoteFiles := s.RemoteFiles
	s.mu.RUnlock()

	if err := validateFocusSHAs(f, vcs, repoRoot, remoteFiles); err != nil {
		return err
	}

	// Hold writeMu across the rest of SetFocus to serialize with the
	// debounce-timer callback in scheduleWrite. Without this, a timer that
	// fires after our WriteFiles() flush below — but before
	// persistActiveDiffScope — would race the swap: it would snapshot the
	// new Focus's (empty) Files alongside the OLD ActiveDiffScope on disk,
	// producing a torn intermediate state where comments authored under
	// the new view appear with the old scope label. The timer callback
	// also takes writeMu, so blocking it here is sufficient.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Cancel any pending debounce timer outright. WriteFiles below flushes
	// in-memory state synchronously, so a deferred fire would write nothing
	// new — but stopping it removes the chance that it sneaks in between
	// our two locked critical sections (the swap and persistActiveDiffScope).
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()

	// Flush any pending debounced WriteFiles BEFORE we replace s.Files.
	// Without this, recent in-memory comments (authored within the last
	// 200ms) live only in s.Files and would be lost when buildFilesForFocus
	// returns a fresh slice with empty Comments. WriteFiles is idempotent
	// when there's nothing pending.
	s.WriteFiles()

	// Snapshot for rollback.
	s.mu.Lock()
	oldFocus := s.Focus
	oldFiles := s.Files
	oldBaseRef := s.BaseRef
	s.mu.Unlock()

	newFiles, newBaseRef, err := s.buildFilesForFocus(f, vcs, repoRoot)
	if err != nil {
		return fmt.Errorf("rebuilding file list for focus: %w", err)
	}

	s.mu.Lock()
	s.Focus = f
	s.Files = newFiles
	s.BaseRef = newBaseRef
	// Stash the previous range focus when transitioning OUT of range mode so
	// the working-tree view can offer a "Resume PR #N" affordance.
	if oldFocus.Kind == FocusRange && f.Kind != FocusRange {
		stash := oldFocus
		s.LastRangeFocus = &stash
	}
	s.mu.Unlock()

	// Switching to a *different* PR drops the old PR's cached metadata.
	dropStaleCacheOnPRSwitch(oldFocus, f)

	// Re-load on-disk comments into the freshly built in-memory file list.
	// buildFilesForFocus / buildFilesForWorkingTree both produce empty
	// Comments slices, so without this step the next scheduleWrite would
	// silently overwrite the disk state (including any re-anchor work
	// done above). loadCritJSONLocked walks s.Files and restores matching
	// paths' comments. We use the *Locked variant because we hold s.mu —
	// the public loadCritJSON enforces a pre-SetSession-only guard that
	// would silently no-op this runtime reload.
	s.mu.Lock()
	s.loadCritJSONLocked()
	s.mu.Unlock()

	if err := s.persistActiveDiffScope(string(f.DiffScope)); err != nil {
		// Roll back in-memory state. The WriteFiles() flush at the top of
		// SetFocus persisted the pre-swap state to disk; loadCritJSON only
		// reads, and persistActiveDiffScope failed before mutating the file —
		// so disk still reflects the old focus and rollback is complete.
		// (If persistActiveDiffScope ever grows a partial-write failure mode,
		// disk could lag in-memory by exactly one ActiveDiffScope field. That
		// remains acceptable: the next successful focus change rewrites it,
		// and ActiveDiffScope is metadata, not user content.)
		s.mu.Lock()
		s.Focus = oldFocus
		s.Files = oldFiles
		s.BaseRef = oldBaseRef
		s.mu.Unlock()
		return fmt.Errorf("persisting active diff scope: %w", err)
	}

	s.mu.Lock()
	s.scheduleWrite()
	s.mu.Unlock()

	// Snapshot under the lock so the SSE payload reflects the same state the
	// frontend would see on a fresh /api/session fetch — without this, the
	// Resume PR pill never appears after a range -> working_tree switch
	// because session.last_range_focus on the client stays at its initial
	// (typically undefined) value.
	s.mu.RLock()
	lastRange := s.LastRangeFocus
	s.mu.RUnlock()
	payload, _ := json.Marshal(map[string]any{
		"focus":            f,
		"last_range_focus": lastRange,
	})
	s.notify(SSEEvent{Type: "focus-changed", Content: string(payload)})
	return nil
}

// validateFocusSHAs runs ensureSHAFetched for each SHA needed by the focus.
// No-op for working-tree focus, and also when remoteFiles is set: in --remote
// mode file content reads go through the GitHub API, so the local-fetch step
// is unnecessary side effects (reflog churn, fork creds).
func validateFocusSHAs(f Focus, vcs VCS, repoRoot string, remoteFiles bool) error {
	if f.Kind != FocusRange || remoteFiles {
		return nil
	}
	if err := ensureSHAFetched(vcs, f.BaseSHA, repoRoot, ""); err != nil {
		return err
	}
	if err := ensureSHAFetched(vcs, f.HeadSHA, repoRoot, f.ForkURL); err != nil {
		return err
	}
	if f.DiffScope == DiffScopeFullStack && f.DefaultSHA != "" {
		if err := ensureSHAFetched(vcs, f.DefaultSHA, repoRoot, ""); err != nil {
			return err
		}
	}
	return nil
}

// dropStaleCacheOnPRSwitch invalidates the previous PR's cached metadata
// whenever SetFocus moves between two distinct, non-zero PR numbers. The next
// time the user comes back to oldFocus.PRNumber we want fresh state in case
// the PR was retitled, force-pushed, or the description changed.
func dropStaleCacheOnPRSwitch(oldFocus, newFocus Focus) {
	if oldFocus.PRNumber == 0 || newFocus.PRNumber == 0 {
		return
	}
	if oldFocus.PRNumber == newFocus.PRNumber {
		return
	}
	invalidatePRCache(oldFocus.PRNumber)
}

// persistActiveDiffScope updates CritJSON.ActiveDiffScope on disk via
// saveCritJSON. Always called on focus change — including when scope is empty
// (working-tree), so a stale "layer" doesn't linger from a previous range
// session and confuse the push gate.
func (s *Session) persistActiveDiffScope(scope string) error {
	critPath := s.critJSONPath()
	cj, err := loadCritJSON(critPath)
	if err != nil {
		// File may not exist yet — fall through and create one with just the scope.
		cj = CritJSON{Files: map[string]CritJSONFile{}}
	}
	cj.ActiveDiffScope = scope
	return saveCritJSON(critPath, cj)
}

// readFileAtSHA returns file content at the given SHA. When RemoteFiles is
// set and we're in a range focus with a parseable PR URL, it goes through
// the GitHub API (gh api repos/.../contents/?ref=<sha>); otherwise it falls
// through to local git. Result is memoized in s.remoteFileCache for the
// remote path; the local path is fast enough already.
func (s *Session) readFileAtSHA(sha, path string) ([]byte, error) {
	if s.RemoteFiles && s.Focus.Kind == FocusRange && s.Focus.PRURL != "" {
		return s.readFileAtSHARemote(sha, path)
	}
	return s.VCS.ReadFileAtSHA(sha, path, s.RepoRoot)
}

// readFileAtSHARemote fetches file content via `gh api`. Falls back to the
// local VCS read when the PR URL is unparseable — the caller still gets a
// best-effort result rather than a hard failure.
func (s *Session) readFileAtSHARemote(sha, path string) ([]byte, error) {
	cacheKey := sha + "\x00" + path
	cache := s.ensureRemoteFileCache()
	if v, ok := cache.Get(cacheKey); ok {
		return v, nil
	}
	owner, name, ok := parseRepoFromPRURL(s.Focus.PRURL)
	if !ok {
		// Unparseable PRURL is rare (we built the Focus from a gh API call)
		// but keep going — local git is still a valid path.
		return s.VCS.ReadFileAtSHA(sha, path, s.RepoRoot)
	}
	data, err := fetchPRFileContent(owner, name, sha, path)
	if err != nil {
		return nil, err
	}
	cache.Put(cacheKey, data)
	return data, nil
}

// ensureRemoteFileCache returns s.remoteFileCache, lazy-initialising it under
// s.mu so concurrent readers (e.g. parallel buildFilesForFocus paths) don't
// race on first allocation. Subsequent calls take a single RLock.
func (s *Session) ensureRemoteFileCache() *bytesLRU {
	s.mu.RLock()
	c := s.remoteFileCache
	s.mu.RUnlock()
	if c != nil {
		return c
	}
	s.mu.Lock()
	if s.remoteFileCache == nil {
		s.remoteFileCache = newBytesLRU(remoteFileCacheCap)
	}
	c = s.remoteFileCache
	s.mu.Unlock()
	return c
}

// buildFilesForFocus returns a fresh []*FileEntry and BaseRef value for the
// given focus. Working-tree focus rebuilds from the VCS so toggling between
// modes shows the right file list. Range focus reads files via
// s.readFileAtSHA (which routes to gh api when --remote is set) and computes
// diffs via vcs.FileDiffBetweenSHAs.
//
// Note: when s.RemoteFiles is true, only file content reads are remote.
// FileDiffBetweenSHAs and ChangedFilesBetweenSHAs still go through local git
// — the GitHub API has no clean equivalent for those operations.
func (s *Session) buildFilesForFocus(f Focus, vcs VCS, repoRoot string) ([]*FileEntry, string, error) {
	if f.Kind != FocusRange {
		return s.buildFilesForWorkingTree(vcs, repoRoot)
	}
	if vcs == nil {
		return nil, "", fmt.Errorf("range focus requires a VCS")
	}
	changes, err := vcs.ChangedFilesBetweenSHAs(f.DiffBaseSHA(), f.HeadSHA, repoRoot)
	if err != nil {
		return nil, "", err
	}
	out := make([]*FileEntry, 0, len(changes))
	for _, fc := range changes {
		fe := &FileEntry{
			Path:     fc.Path,
			AbsPath:  filepath.Join(repoRoot, fc.Path),
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
			Comments: []Comment{},
		}
		if fc.Status != "deleted" {
			data, readErr := s.readFileAtSHA(f.HeadSHA, fc.Path)
			if readErr != nil {
				return nil, "", fmt.Errorf("read %s at %s: %w", fc.Path, f.HeadSHA, readErr)
			}
			fe.Content = string(data)
			fe.FileHash = fileHash(data)
		}
		if fc.Status != "added" && fc.Status != "untracked" {
			hunks, _ := vcs.FileDiffBetweenSHAs(fc.Path, f.DiffBaseSHA(), f.HeadSHA, repoRoot)
			fe.DiffHunks = hunks
		} else {
			fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)
		}
		out = append(out, fe)
	}
	return out, f.DiffBaseSHA(), nil
}

// buildFilesForWorkingTree rebuilds the file list from the VCS for the
// working-tree focus. Mirrors the eager-load loop in NewSessionFromVCS but
// does not mutate session state directly.
func (s *Session) buildFilesForWorkingTree(vcs VCS, repoRoot string) ([]*FileEntry, string, error) {
	if vcs == nil {
		// No VCS — keep current file list (file mode).
		s.mu.RLock()
		files := s.Files
		baseRef := s.BaseRef
		s.mu.RUnlock()
		return files, baseRef, nil
	}
	s.mu.RLock()
	ignorePatterns := s.IgnorePatterns
	branch := s.Branch
	s.mu.RUnlock()
	defaultBranch := vcs.DefaultBranch()
	baseRef := ""
	if branch != defaultBranch {
		baseRef, _ = vcs.MergeBase(defaultBranch)
	}
	var changes []FileChange
	var err error
	if branch == defaultBranch {
		changes, err = vcs.ChangedFilesOnDefaultInDir(repoRoot)
	} else {
		changes, err = vcs.ChangedFilesFromBaseInDir(baseRef, repoRoot)
	}
	if err != nil {
		return nil, "", err
	}
	changes = filterIgnored(changes, ignorePatterns)
	changes = filterBinary(changes)
	out := make([]*FileEntry, 0, len(changes))
	for _, fc := range changes {
		fe := &FileEntry{
			Path:     fc.Path,
			AbsPath:  filepath.Join(repoRoot, fc.Path),
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
			Comments: []Comment{},
		}
		if !populateEagerFile(fe, fc, baseRef, repoRoot, vcs) {
			continue
		}
		out = append(out, fe)
	}
	return out, baseRef, nil
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
// to avoid running VCS commands on every /api/session poll.
func cachedAvailableScopes(baseRef string, vcs VCS) []string {
	scopeCacheMu.Lock()
	defer scopeCacheMu.Unlock()

	now := time.Now()
	if now.Before(scopeCacheExpiry) && scopeCacheBaseRef == baseRef {
		result := make([]string, len(scopeCacheResult))
		copy(result, scopeCacheResult)
		return result
	}

	scopes := availableScopes(baseRef, vcs)
	scopeCacheBaseRef = baseRef
	scopeCacheResult = scopes
	scopeCacheExpiry = now.Add(scopeCacheTTL)

	result := make([]string, len(scopes))
	copy(result, scopes)
	return result
}

// availableScopes returns the list of scopes that have files.
// Only includes a scope if the VCS reports changes for it.
func availableScopes(baseRef string, vcs VCS) []string {
	scopes := []string{"all"}
	if vcs == nil {
		return scopes
	}
	if baseRef != "" {
		if files, err := vcs.ChangedFilesScoped("branch", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "branch")
		}
	}
	if vcs.HasStagingArea() {
		if files, err := vcs.ChangedFilesScoped("staged", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "staged")
		}
		if files, err := vcs.ChangedFilesScoped("unstaged", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "unstaged")
		}
	}
	return scopes
}

// GetCommits returns the list of commits between the base ref and the focus's
// upper bound. In working-tree mode the upper bound is the VCS's HEAD; in range
// mode it's Focus.HeadSHA so the dropdown doesn't list commits past the focus.
// Returns nil for non-VCS sessions or when no base ref is set.
func (s *Session) GetCommits() []CommitInfo {
	s.mu.RLock()
	if s.Mode != "git" || s.BaseRef == "" || s.VCS == nil {
		s.mu.RUnlock()
		return nil
	}
	baseRef, repoRoot, vcs := s.BaseRef, s.RepoRoot, s.VCS
	headRef := ""
	if s.Focus.Kind == FocusRange && s.Focus.HeadSHA != "" {
		headRef = s.Focus.HeadSHA
	}
	s.mu.RUnlock()
	commits, err := vcs.CommitLog(baseRef, headRef, repoRoot)
	if err != nil {
		return nil
	}
	return commits
}

// scopedSessionSnapshot holds session state read under lock for scoped queries.
type scopedSessionSnapshot struct {
	vcs              VCS
	baseRef          string
	baseBranchName   string
	repoRoot         string
	mode             string
	branch           string
	reviewRound      int
	ignorePatterns   []string
	commentCounts    map[string]int
	unresolvedCounts map[string]int
	totalUnresolved  int
	lazyFiles        map[string]*FileEntry
	reviewComments   []Comment
	focus            Focus
	lastRangeFocus   *Focus
}

func (s *Session) snapshotForScoped() scopedSessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commentCounts := make(map[string]int, len(s.Files))
	unresolvedCounts := make(map[string]int, len(s.Files))
	lazyFiles := make(map[string]*FileEntry, len(s.Files))
	totalUnresolved := 0
	for _, f := range s.Files {
		commentCounts[f.Path] = countVisibleComments(f.Comments, s.Focus)
		for _, c := range f.Comments {
			if !c.Resolved {
				unresolvedCounts[f.Path]++
				totalUnresolved++
			}
		}
		if f.Lazy {
			lazyFiles[f.Path] = f
		}
	}
	rc := make([]Comment, 0, len(s.reviewComments))
	for _, c := range s.reviewComments {
		if !c.Resolved {
			totalUnresolved++
		}
		if !visibleInFocus(c, s.Focus) {
			continue
		}
		rc = append(rc, c)
	}

	return scopedSessionSnapshot{
		vcs:              s.VCS,
		baseRef:          s.BaseRef,
		baseBranchName:   s.BaseBranchName,
		repoRoot:         s.RepoRoot,
		mode:             s.Mode,
		branch:           s.Branch,
		reviewRound:      s.ReviewRound,
		ignorePatterns:   s.IgnorePatterns,
		commentCounts:    commentCounts,
		unresolvedCounts: unresolvedCounts,
		totalUnresolved:  totalUnresolved,
		lazyFiles:        lazyFiles,
		reviewComments:   rc,
		focus:            s.Focus,
		lastRangeFocus:   s.LastRangeFocus,
	}
}

func scopedHunks(fc FileChange, scope, commit, baseRef, repoRoot string, vcs VCS) []DiffHunk {
	if vcs == nil {
		return nil
	}
	if commit != "" {
		h, err := vcs.FileDiffForCommit(fc.Path, commit, repoRoot)
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
	h, err := vcs.FileDiffScoped(fc.Path, scope, baseRef, repoRoot)
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

	// Range focus already pins the file list to BaseSHA..HeadSHA via
	// buildFilesForFocus. Working-tree scopes (branch, staged, unstaged) are
	// meaningless in this mode — they would run git diff against HEAD instead
	// of the range's head SHA, leaking files outside the range. Delegate to
	// GetSessionInfo which returns the pre-built range-scoped file list.
	s.mu.RLock()
	inRange := s.Focus.Kind == FocusRange
	s.mu.RUnlock()
	if inRange && commit == "" {
		return s.GetSessionInfo()
	}

	snap := s.snapshotForScoped()

	info := SessionInfo{
		Mode:            snap.mode,
		Branch:          snap.branch,
		BaseRef:         snap.baseRef,
		BaseBranchName:  snap.baseBranchName,
		ReviewRound:     snap.reviewRound,
		AvailableScopes: availableScopes(snap.baseRef, snap.vcs),
		ReviewComments:  snap.reviewComments,
		Focus:           snap.focus,
		LastRangeFocus:  snap.lastRangeFocus,
	}

	if snap.vcs == nil {
		return info
	}

	var changes []FileChange
	var err error
	if commit != "" {
		changes, err = snap.vcs.ChangedFilesForCommit(commit, snap.repoRoot)
	} else {
		changes, err = snap.vcs.ChangedFilesScoped(scope, snap.baseRef)
	}
	if err != nil || len(changes) == 0 {
		return info
	}

	changes = filterIgnored(changes, snap.ignorePatterns)
	changes = filterBinary(changes)

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

		hunks := scopedHunks(fc, scope, commit, snap.baseRef, snap.repoRoot, snap.vcs)
		fi.Additions, fi.Deletions = countHunkStats(hunks)
		info.Files = append(info.Files, fi)
	}

	info.HiddenUnresolved = snap.hiddenUnresolved(info.Files)
	return info
}

// hiddenUnresolved returns the count of unresolved comments that exist outside
// the given file list (and outside the snapshot's review comments), so the
// client can correctly label the finish button when out-of-scope comments are
// not loaded. Computed from data captured under the same lock as the snapshot.
func (snap *scopedSessionSnapshot) hiddenUnresolved(scopeFiles []SessionFileInfo) int {
	scopeUnresolved := 0
	for _, c := range snap.reviewComments {
		if !c.Resolved {
			scopeUnresolved++
		}
	}
	for _, fi := range scopeFiles {
		scopeUnresolved += snap.unresolvedCounts[fi.Path]
	}
	if hidden := snap.totalUnresolved - scopeUnresolved; hidden > 0 {
		return hidden
	}
	return 0
}

// loadScopedFileState reads file state from the session or disk for scoped diff queries.
func (s *Session) loadScopedFileState(path, scope string) (status, content, baseRef, repoRoot string) {
	s.mu.RLock()
	f := s.fileByPathLocked(path)
	baseRef = s.BaseRef
	repoRoot = s.RepoRoot
	vcs := s.VCS
	if f != nil {
		status = f.Status
	}
	s.mu.RUnlock()

	if f != nil {
		if err := f.ensureLoaded(repoRoot, baseRef, vcs); err == nil {
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
		if vcs != nil {
			if changes, err := vcs.ChangedFilesScoped(scope, baseRef); err == nil {
				for _, fc := range changes {
					if fc.Path == path {
						status = fc.Status
						break
					}
				}
			}
		}
	}
	return status, content, baseRef, repoRoot
}

func computeScopedDiffHunks(path, scope, commit, status, content, baseRef, repoRoot string, vcs VCS) []DiffHunk {
	// Pure content-based diffs don't need VCS.
	if status == "untracked" && (scope == "unstaged" || scope == "all" || scope == "") {
		return FileDiffUnifiedNewFile(content)
	}
	if status == "added" && scope != "unstaged" {
		return FileDiffUnifiedNewFile(content)
	}
	if vcs == nil {
		return nil
	}
	if commit != "" {
		h, err := vcs.FileDiffForCommit(path, commit, repoRoot)
		if err == nil {
			return h
		}
		return nil
	}
	h, err := vcs.FileDiffScoped(path, scope, baseRef, repoRoot)
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

	s.mu.RLock()
	vcs := s.VCS
	s.mu.RUnlock()

	hunks := computeScopedDiffHunks(path, scope, commit, status, content, baseRef, repoRoot, vcs)
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}
