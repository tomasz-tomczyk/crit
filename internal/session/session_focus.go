package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
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
	Forge             string    `json:"forge,omitempty"`
	ChangeNumber      int       `json:"change_number,omitempty"`
	PRURL             string    `json:"pr_url,omitempty"`
	MRURL             string    `json:"mr_url,omitempty"`
	Label             string    `json:"label,omitempty"`
	BaseSHA           string    `json:"base_sha,omitempty"`
	HeadSHA           string    `json:"head_sha,omitempty"`
	DefaultSHA        string    `json:"default_sha,omitempty"`
	ForkURL           string    `json:"fork_url,omitempty"`
	RemoteProject     string    `json:"remote_project,omitempty"`
	RemoteBaseProject string    `json:"remote_base_project,omitempty"`
	RemoteHost        string    `json:"remote_host,omitempty"`
	BaseRefName       string    `json:"base_ref_name,omitempty"`
	HeadRefName       string    `json:"head_ref_name,omitempty"`
	DiffScope         DiffScope `json:"diff_scope,omitempty"`
	IsStacked         bool      `json:"is_stacked,omitempty"`
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
//	pr:<project>#<num>             — URL-qualified GitHub PR
//	mr:<num>                       — range focus with MR IID (checkout-scoped)
//	mr:<project>#<num>             — URL-qualified GitLab MR
//	range:<baseSHA>..<headSHA>     — range focus without PR number
//	""                             — working-tree (and unknown)
//
// Callers must pass Focus whose BaseSHA/HeadSHA are full OIDs (SetFocus
// canonicalizeFocusSHAs enforces this). Symbolic refs in those fields would
// stamp unstable keys and hide comments after stack navigation.
func focusKeyFor(f Focus) string {
	if f.Kind != FocusRange {
		return ""
	}
	if f.ChangeNumber > 0 {
		if f.Forge == "gitlab" {
			return MRFocusKey(f.ChangeNumber, f.RemoteBaseProject, f.RemoteHost)
		}
		// github or empty forge (legacy ChangeNumber without Forge) → pr:…
		return PRFocusKey(f.ChangeNumber, f.RemoteBaseProject, f.RemoteHost)
	}
	return fmt.Sprintf("range:%s..%s", f.BaseSHA, f.HeadSHA)
}

// PRFocusKey is the GitHub PR identity used for daemon session keys and
// comment FocusKey stamping. URL-qualified reviews include owner/repo (and
// non-github.com host) so same-number PRs do not collide (#870).
// Bare numbers (empty project) keep the legacy "pr:N" form so existing
// checkout-scoped sessions continue to match.
func PRFocusKey(number int, project, host string) string {
	if project == "" {
		return fmt.Sprintf("pr:%d", number)
	}
	if host != "" && !strings.EqualFold(host, "github.com") {
		return fmt.Sprintf("pr:%s/%s#%d", host, project, number)
	}
	return fmt.Sprintf("pr:%s#%d", project, number)
}

// MRFocusKey is the GitLab MR identity used for daemon session keys and
// comment FocusKey stamping. Mirrors PRFocusKey: bare IIDs stay "mr:N";
// URL-qualified reviews include project (and non-gitlab.com host).
func MRFocusKey(number int, project, host string) string {
	if project == "" {
		return fmt.Sprintf("mr:%d", number)
	}
	if host != "" && !strings.EqualFold(host, "gitlab.com") {
		return fmt.Sprintf("mr:%s/%s#%d", host, project, number)
	}
	return fmt.Sprintf("mr:%s#%d", project, number)
}

// visibleInFocus reports whether c should be shown in the given focus.
// Comments belong to the *view* they were authored in, identified by
// FocusKey. Within a range focus, the layer/full-stack DiffScope filter
// also applies. Pure function — no I/O, no locks.
func visibleInFocus(c Comment, f Focus) bool {
	return visibleInFocusKey(c, focusKeyFor(f), f)
}

// visibleInFocusKey is visibleInFocus with a precomputed focus key. Use it in
// per-comment loops: focusKeyFor allocates (Sprintf) on every call, so
// calling visibleInFocus per comment pays one allocation per comment.
// Hoisting the key out of the loop makes the scan allocation-free.
func visibleInFocusKey(c Comment, key string, f Focus) bool {
	if c.FocusKey != key {
		return false
	}
	if f.Kind == FocusRange {
		return c.DiffScope == string(f.DiffScope)
	}
	return c.DiffScope == ""
}

// AsFocus returns a synthetic Focus that produces the same stamping as this scope.
func (s InheritedScope) AsFocus() Focus {
	if s.DiffScope == "" {
		return Focus{Kind: FocusWorkingTree}
	}
	return Focus{
		Kind:         FocusRange,
		HeadSHA:      s.HeadSHA,
		BaseSHA:      s.BaseSHA,
		Forge:        s.Forge,
		ChangeNumber: s.ChangeNumber,
		DiffScope:    DiffScope(s.DiffScope),
	}
}

// stampWithFocus copies focus-derived metadata onto a freshly authored Comment.
// No-op when Focus.Kind != FocusRange, preserving working-tree behavior.
func StampWithFocus(c Comment, f Focus) Comment {
	c.FocusKey = focusKeyFor(f)
	if f.Kind == FocusRange {
		c.HeadSHA = f.HeadSHA
		c.DiffScope = string(f.DiffScope)
	}
	return c
}

// countVisibleComments returns the count of comments visible in the given focus.
func countVisibleComments(comments []Comment, f Focus) int {
	key := focusKeyFor(f)
	n := 0
	for _, c := range comments {
		if visibleInFocusKey(c, key, f) {
			n++
		}
	}
	return n
}

// SetFocus atomically swaps the session's Focus and rebuilds the file list.
// On any failure during rebuild or persistence, the previous Focus + Files
// are restored in memory; disk state remains consistent because the only
// disk write between snapshot and rollback is persistActiveDiffScope, and
// rollback runs only when that write fails (saveCritJSONToDisk uses atomic rename,
// so it's all-or-nothing — no torn ActiveDiffScope on disk).
//
// Caller is responsible for validating the request shape upstream;
// SetFocus owns SHA validation (via ensureSHAFetched), OID canonicalization
// of BaseSHA/HeadSHA/DefaultSHA, and persistence.
func (s *Session) SetFocus(f Focus) error {
	if f.Kind == FocusRange &&
		f.DiffScope == DiffScopeFullStack &&
		f.DefaultSHA == "" {
		return fmt.Errorf("full-stack scope requires a resolvable default branch tip")
	}

	s.mu.RLock()
	repoRoot := s.RepoRoot
	vc := s.VCS
	remoteFiles := s.RemoteFiles
	s.mu.RUnlock()

	if err := validateFocusSHAs(f, vc, repoRoot, remoteFiles); err != nil {
		return err
	}
	if err := canonicalizeFocusSHAs(&f, vc, repoRoot, remoteFiles); err != nil {
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

	newFiles, newBaseRef, err := s.buildFilesForFocus(f, vc, repoRoot)
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
func validateFocusSHAs(f Focus, v vcs.VCS, repoRoot string, remoteFiles bool) error {
	if f.Kind != FocusRange || remoteFiles {
		return nil
	}
	if err := vcs.EnsureSHAFetched(v, f.BaseSHA, repoRoot, ""); err != nil {
		return err
	}
	if err := vcs.EnsureSHAFetched(v, f.HeadSHA, repoRoot, f.ForkURL); err != nil {
		return err
	}
	if f.DiffScope == DiffScopeFullStack && f.DefaultSHA != "" {
		if err := vcs.EnsureSHAFetched(v, f.DefaultSHA, repoRoot, ""); err != nil {
			return err
		}
	}
	return nil
}

// canonicalizeFocusSHAs rewrites BaseSHA/HeadSHA/DefaultSHA to full commit
// OIDs. Branch names and abbreviated SHAs pass validateFocusSHAs
// (EnsureSHAFetched checks presence via HasObject); this function then
// resolves them to full OIDs so focusKeyFor stays stable across stack
// navigation that re-enters with resolved OIDs.
// No-op for working-tree focus, remote mode, or missing VCS/repo.
func canonicalizeFocusSHAs(f *Focus, v vcs.VCS, repoRoot string, remoteFiles bool) error {
	if f == nil || f.Kind != FocusRange || remoteFiles || v == nil || repoRoot == "" {
		return nil
	}
	base, err := vcs.ResolveCommitOID(v, f.BaseSHA, repoRoot)
	if err != nil {
		return fmt.Errorf("canonicalizing base %q: %w", f.BaseSHA, err)
	}
	head, err := vcs.ResolveCommitOID(v, f.HeadSHA, repoRoot)
	if err != nil {
		return fmt.Errorf("canonicalizing head %q: %w", f.HeadSHA, err)
	}
	f.BaseSHA = base
	f.HeadSHA = head
	if f.DefaultSHA != "" {
		def, err := vcs.ResolveCommitOID(v, f.DefaultSHA, repoRoot)
		if err != nil {
			return fmt.Errorf("canonicalizing default %q: %w", f.DefaultSHA, err)
		}
		f.DefaultSHA = def
	}
	return nil
}

// dropStaleCacheOnPRSwitch invalidates the previous PR's cached metadata
// whenever SetFocus moves between two distinct, non-zero PR numbers. The next
// time the user comes back to the old GitHub change we want fresh state in case
// the PR was retitled, force-pushed, or the description changed.
func dropStaleCacheOnPRSwitch(oldFocus, newFocus Focus) {
	if oldFocus.Forge != "github" || newFocus.Forge != "github" || oldFocus.ChangeNumber == 0 || newFocus.ChangeNumber == 0 {
		return
	}
	if oldFocus.ChangeNumber == newFocus.ChangeNumber &&
		oldFocus.RemoteBaseProject == newFocus.RemoteBaseProject &&
		oldFocus.RemoteHost == newFocus.RemoteHost {
		return
	}
	if InvalidatePRCache != nil {
		InvalidatePRCache(oldFocus.ChangeNumber, oldFocus.RemoteBaseProject, oldFocus.RemoteHost)
	}
}

// persistActiveDiffScope updates CritJSON.ActiveDiffScope on disk via
// saveCritJSONToDisk. Always called on focus change — including when scope is empty
// (working-tree), so a stale "layer" doesn't linger from a previous range
// session and confuse the push gate.
func (s *Session) persistActiveDiffScope(scope string) error {
	critPath := s.critJSONPath()
	if critPath == "" {
		return nil
	}
	cj, err := readCritJSONFromDisk(critPath)
	if err != nil {
		// File may not exist yet — fall through and create one with just the scope.
		cj = CritJSON{Files: map[string]CritJSONFile{}}
	}
	cj.ActiveDiffScope = scope
	return saveCritJSONToDisk(critPath, cj)
}

// readFileAtSHA returns file content at the given SHA. When RemoteFiles is
// set and we're in a range focus with a parseable PR URL, it goes through
// the GitHub API (gh api repos/.../contents/?ref=<sha>); otherwise it falls
// through to local git. Result is memoized in s.remoteFileCache for the
// remote path; the local path is fast enough already.
func (s *Session) readFileAtSHA(sha, path string) ([]byte, error) { //nolint:unparam // production callers use readFileAtSHAForFocus; retained as the current-focus convenience and test seam
	return s.readFileAtSHAForFocus(s.Focus, sha, path)
}

func (s *Session) readFileAtSHAForFocus(focus Focus, sha, path string) ([]byte, error) {
	if s.RemoteFiles && focus.Kind == FocusRange && (focus.PRURL != "" || focus.MRURL != "") {
		return s.readFileAtSHARemote(focus, sha, path)
	}
	return s.VCS.ReadFileAtSHA(sha, path, s.RepoRoot)
}

// readFileAtSHARemote fetches file content via `gh api`. Falls back to the
// local VCS read when the PR URL is unparseable — the caller still gets a
// best-effort result rather than a hard failure.
func (s *Session) readFileAtSHARemote(focus Focus, sha, path string) ([]byte, error) {
	cacheKey := sha + "\x00" + path
	cache := s.ensureRemoteFileCache()
	if v, ok := cache.Get(cacheKey); ok {
		return v, nil
	}
	if focus.MRURL != "" && FetchMRFileContent != nil {
		data, err := FetchMRFileContent(focus, sha, path)
		if err != nil {
			return nil, err
		}
		cache.Put(cacheKey, data)
		return data, nil
	}
	owner, name, ok := parseRepoFromPRURL(focus.PRURL)
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
// s.readFileAtSHAForFocus. GitLab layer reviews can also load file lists and
// hunks from the MR Diffs API, allowing cross-project reviews without fetching
// either side into the local checkout.
func (s *Session) buildFilesForFocus(f Focus, v vcs.VCS, repoRoot string) ([]*FileEntry, string, error) { //nolint:gocyclo // remote/local change sources converge in one file-entry construction loop
	if f.Kind != FocusRange {
		return s.buildFilesForWorkingTree(v, repoRoot)
	}
	if v == nil {
		return nil, "", fmt.Errorf("range focus requires a VCS")
	}
	var changes []vcs.FileChange
	remoteHunks := make(map[string][]vcs.DiffHunk)
	if s.RemoteFiles && f.MRURL != "" && f.DiffScope == DiffScopeLayer && FetchMRDiffs != nil {
		remoteChanges, err := FetchMRDiffs(f)
		if err != nil {
			return nil, "", err
		}
		changes = make([]vcs.FileChange, 0, len(remoteChanges))
		for _, change := range remoteChanges {
			changes = append(changes, change.FileChange)
			remoteHunks[change.Path] = change.Hunks
		}
	} else {
		var err error
		changes, err = v.ChangedFilesBetweenSHAs(f.DiffBaseSHA(), f.HeadSHA, repoRoot)
		if err != nil {
			return nil, "", err
		}
	}
	var numstats map[string]vcs.NumstatEntry
	if len(changes) > lazyFileThreshold {
		// Between-SHA only — never fall back to working-tree numstat, which
		// contaminates sidebar +/- when the tree is dirty or --remote.
		var nsErr error
		numstats, nsErr = v.DiffNumstatBetweenSHAs(f.DiffBaseSHA(), f.HeadSHA, repoRoot)
		if nsErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: numstat failed: %v\n", nsErr)
		}
	}
	out := make([]*FileEntry, 0, len(changes))
	for i, fc := range changes {
		fe := &FileEntry{
			Path:     fc.Path,
			OldPath:  fc.OldPath,
			AbsPath:  filepath.Join(repoRoot, fc.Path),
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
			Comments: []Comment{},
		}
		// Same eager/lazy split as NewGitSession: range focus is how --pr
		// rebuilds the file list, so large doc PRs must not load every file.
		if len(changes) > lazyFileThreshold && i >= lazyFileThreshold {
			populateLazyFile(fe, fc, numstats, false)
			out = append(out, fe)
			continue
		}
		if fc.Status != "deleted" {
			data, readErr := s.readFileAtSHAForFocus(f, f.HeadSHA, fc.Path)
			if readErr != nil {
				return nil, "", fmt.Errorf("read %s at %s: %w", fc.Path, f.HeadSHA, readErr)
			}
			fe.Content = string(data)
			fe.FileHash = fileHash(data)
		}
		if hunks, ok := remoteHunks[fc.Path]; ok {
			fe.DiffHunks = hunks
		} else if fc.Status != "added" && fc.Status != "untracked" {
			hunks, _ := v.FileDiffBetweenSHAs(fc.Path, fc.OldPath, f.DiffBaseSHA(), f.HeadSHA, repoRoot, false)
			fe.DiffHunks = hunks
		} else {
			fe.DiffHunks = vcs.FileDiffUnifiedNewFile(fe.Content)
		}
		out = append(out, fe)
	}
	return out, f.DiffBaseSHA(), nil
}

// buildFilesForWorkingTree rebuilds the file list from the VCS for the
// working-tree focus. Mirrors the eager-load loop in NewGitSession but
// does not mutate session state directly.
func (s *Session) buildFilesForWorkingTree(v vcs.VCS, repoRoot string) ([]*FileEntry, string, error) {
	if v == nil {
		// No VCS — keep current file list (file mode).
		s.mu.RLock()
		files := s.Files
		baseRef := s.BaseRef
		s.mu.RUnlock()
		return files, baseRef, nil
	}
	s.mu.RLock()
	ignorePatterns := s.IgnorePatterns
	s.mu.RUnlock()
	baseRef, err := s.baseRefForWorkingTreeDiscovery(v)
	if err != nil {
		return nil, "", err
	}
	changes, err := changedFilesForSession(v, baseRef, repoRoot)
	if err != nil {
		return nil, "", err
	}
	changes = config.FilterIgnored(changes, ignorePatterns)
	changes = filterBinary(changes)
	var numstats map[string]vcs.NumstatEntry
	if len(changes) > lazyFileThreshold && baseRef != "" {
		var nsErr error
		numstats, nsErr = v.DiffNumstat(baseRef, repoRoot)
		if nsErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: numstat failed: %v\n", nsErr)
		}
	}
	out := make([]*FileEntry, 0, len(changes))
	for i, fc := range changes {
		fe := &FileEntry{
			Path:     fc.Path,
			OldPath:  fc.OldPath,
			AbsPath:  filepath.Join(repoRoot, fc.Path),
			Status:   fc.Status,
			FileType: detectFileType(fc.Path),
			Comments: []Comment{},
		}
		if len(changes) > lazyFileThreshold && i >= lazyFileThreshold {
			populateLazyFile(fe, fc, numstats, true)
			out = append(out, fe)
			continue
		}
		if !populateEagerFile(fe, fc, baseRef, repoRoot, v) {
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
func cachedAvailableScopes(baseRef string, v vcs.VCS) []string {
	scopeCacheMu.Lock()
	defer scopeCacheMu.Unlock()

	now := time.Now()
	if now.Before(scopeCacheExpiry) && scopeCacheBaseRef == baseRef {
		result := make([]string, len(scopeCacheResult))
		copy(result, scopeCacheResult)
		return result
	}

	scopes := availableScopes(baseRef, v)
	scopeCacheBaseRef = baseRef
	scopeCacheResult = scopes
	scopeCacheExpiry = now.Add(scopeCacheTTL)

	result := make([]string, len(scopes))
	copy(result, scopes)
	return result
}

// availableScopes returns the list of scopes that have files.
// Only includes a scope if the VCS reports changes for it.
func availableScopes(baseRef string, v vcs.VCS) []string {
	scopes := []string{"all"}
	if v == nil {
		return scopes
	}
	if baseRef != "" {
		if files, err := v.ChangedFilesScoped("branch", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "branch")
		}
	}
	if v.HasStagingArea() {
		if files, err := v.ChangedFilesScoped("staged", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "staged")
		}
		if files, err := v.ChangedFilesScoped("unstaged", baseRef); err == nil && len(files) > 0 {
			scopes = append(scopes, "unstaged")
		}
	}
	return scopes
}

const virtualWorkingTreeCommitSHA = "__crit_virtual_working_tree__"

// hasWorkingTreeChanges reports whether there are uncommitted local changes.
// Uses the repo-root-aware in-dir helper so tests and multi-repo daemon usage
// do not accidentally inspect the process CWD.
func hasWorkingTreeChanges(v vcs.VCS, repoRoot string) bool {
	if v == nil {
		return false
	}
	files, err := v.ChangedFilesOnDefaultInDir(repoRoot)
	return err == nil && len(files) > 0
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
	baseRef, repoRoot, vc := s.BaseRef, s.RepoRoot, s.VCS
	focus := s.Focus
	headRef := ""
	if focus.Kind == FocusRange && focus.HeadSHA != "" {
		headRef = focus.HeadSHA
	}
	s.mu.RUnlock()
	commits, err := vc.CommitLog(baseRef, headRef, repoRoot)
	if err != nil {
		return nil
	}
	if focus.Kind == FocusWorkingTree && hasWorkingTreeChanges(vc, repoRoot) {
		commits = append([]CommitInfo{{
			SHA:      virtualWorkingTreeCommitSHA,
			ShortSHA: "WT",
			Message:  "Working changes",
			Virtual:  true,
		}}, commits...)
	}
	return commits
}

// scopedSessionSnapshot holds session state read under lock for scoped queries.
type scopedSessionSnapshot struct {
	vc               vcs.VCS
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
	focusKey := focusKeyFor(s.Focus)
	for _, c := range s.reviewComments {
		if !c.Resolved {
			totalUnresolved++
		}
		if !visibleInFocusKey(c, focusKey, s.Focus) {
			continue
		}
		rc = append(rc, c)
	}

	return scopedSessionSnapshot{
		vc:               s.VCS,
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

// addedFileRendersWhole reports whether a file that is "added" relative to the
// merge-base (committed on the branch, new vs base) should render as an entire
// new file for the given scope. A branch-added file already exists in HEAD, so
// the "staged"/"unstaged" scopes must show the real index/working-tree delta —
// not the whole file. Every other scope (branch/all/"") renders it whole.
func addedFileRendersWhole(scope string) bool {
	return scope != "staged" && scope != "unstaged"
}

func scopedHunks(fc vcs.FileChange, scope, commit, baseRef, repoRoot string, v vcs.VCS, ignoreWhitespace bool) []vcs.DiffHunk {
	if v == nil {
		return nil
	}
	if commit == virtualWorkingTreeCommitSHA {
		h, err := v.FileDiffUnified(fc.Path, "HEAD", repoRoot, ignoreWhitespace)
		if err == nil {
			return h
		}
		return nil
	}
	if base, head, ok := vcs.SplitCommitRange(commit); ok {
		h, err := v.FileDiffBetweenSHAs(fc.Path, fc.OldPath, base, head, repoRoot, ignoreWhitespace)
		if err == nil {
			return h
		}
		return nil
	}
	if commit != "" {
		h, err := v.FileDiffForCommit(fc.Path, commit, repoRoot, ignoreWhitespace)
		if err == nil {
			return h
		}
		return nil
	}
	showWholeFile := fc.Status == "untracked"
	if fc.Status == "added" {
		showWholeFile = addedFileRendersWhole(scope)
	}
	if showWholeFile {
		absPath := filepath.Join(repoRoot, fc.Path)
		if data, err := os.ReadFile(absPath); err == nil {
			return vcs.FileDiffUnifiedNewFile(string(data))
		}
		return nil
	}
	if fc.Status == "renamed" && fc.OldPath != "" {
		h, err := diffHunksForFile(fc.Path, fc.OldPath, fc.Status, baseRef, repoRoot, ignoreWhitespace, v)
		if err == nil {
			return h
		}
		return nil
	}
	h, err := v.FileDiffScoped(fc.Path, scope, baseRef, repoRoot, ignoreWhitespace)
	if err == nil {
		return h
	}
	return nil
}

func changesForScopeSelection(v vcs.VCS, repoRoot, baseRef, scope, commit string) ([]vcs.FileChange, error) {
	if commit == virtualWorkingTreeCommitSHA {
		return v.ChangedFilesOnDefaultInDir(repoRoot)
	}
	if base, head, ok := vcs.SplitCommitRange(commit); ok {
		return v.ChangedFilesBetweenSHAs(base, head, repoRoot)
	}
	if commit != "" {
		return v.ChangedFilesForCommit(commit, repoRoot)
	}
	return v.ChangedFilesScoped(scope, baseRef)
}

func countHunkStats(hunks []vcs.DiffHunk) (additions, deletions int) {
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
		AvailableScopes: availableScopes(snap.baseRef, snap.vc),
		ReviewComments:  snap.reviewComments,
		Focus:           snap.focus,
		LastRangeFocus:  snap.lastRangeFocus,
	}

	if snap.vc == nil {
		return info
	}

	changes, err := changesForScopeSelection(snap.vc, snap.repoRoot, snap.baseRef, scope, commit)
	if err != nil || len(changes) == 0 {
		return info
	}

	changes = config.FilterIgnored(changes, snap.ignorePatterns)
	changes = filterBinary(changes)

	for _, fc := range changes {
		fi := SessionFileInfo{
			Path:         fc.Path,
			OldPath:      fc.OldPath,
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

		hunks := scopedHunks(fc, scope, commit, snap.baseRef, snap.repoRoot, snap.vc, false)
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
func (s *Session) loadScopedFileState(path, scope, commit string) (status, content, oldPath, baseRef, repoRoot string) {
	s.mu.RLock()
	f := s.fileByPathLocked(path)
	baseRef = s.BaseRef
	repoRoot = s.RepoRoot
	vc := s.VCS
	if f != nil {
		status = f.Status
		oldPath = f.OldPath
	}
	s.mu.RUnlock()

	if f != nil {
		if err := s.ensureFileLoaded(f); err == nil {
			s.mu.RLock()
			content = f.Content
			s.mu.RUnlock()
		}
		if commit == "" {
			return status, content, oldPath, baseRef, repoRoot
		}
	}

	if repoRoot == "" {
		return status, content, oldPath, baseRef, repoRoot
	}

	lookupScopedStatus := func() {
		if vc == nil {
			return
		}
		changes, err := changesForScopeSelection(vc, repoRoot, baseRef, scope, commit)
		if err == nil {
			for _, fc := range changes {
				if fc.Path == path {
					status = fc.Status
					oldPath = fc.OldPath
					break
				}
			}
		}
	}

	if commit != "" {
		lookupScopedStatus()
		return status, content, oldPath, baseRef, repoRoot
	}

	if f != nil {
		return status, content, oldPath, baseRef, repoRoot
	}

	absPath := filepath.Join(repoRoot, path)
	if data, err := os.ReadFile(absPath); err == nil {
		content = string(data)
		lookupScopedStatus()
	}
	return status, content, oldPath, baseRef, repoRoot
}

func computeScopedDiffHunks(path, scope, commit, status, oldPath, content, baseRef, repoRoot string, v vcs.VCS, ignoreWhitespace bool) []vcs.DiffHunk {
	// Pure content-based diffs don't need VCS.
	if status == "untracked" && (scope == "unstaged" || scope == "all" || scope == "") {
		return vcs.FileDiffUnifiedNewFile(content)
	}
	if status == "added" && addedFileRendersWhole(scope) {
		return vcs.FileDiffUnifiedNewFile(content)
	}
	return scopedHunks(vcs.FileChange{Path: path, OldPath: oldPath, Status: status}, scope, commit, baseRef, repoRoot, v, ignoreWhitespace)
}

// GetFileDiffSnapshotScoped returns diff data for a file filtered by scope.
// When scope is "" or in file mode (scopes only apply to git), delegates to GetFileDiffSnapshot.
// When commit is non-empty, returns the diff for that single commit.
// When ignoreWhitespace is true, whitespace-only changes collapse to context (code diffs only).
func (s *Session) GetFileDiffSnapshotScoped(path, scope, commit string, ignoreWhitespace bool) (map[string]any, bool) {
	s.mu.RLock()
	inRange := s.Focus.Kind == FocusRange
	s.mu.RUnlock()
	if commit == "" && (scope == "" || scope == "all" || s.Mode == "files" || s.Mode == "plan" || inRange) {
		return s.GetFileDiffSnapshot(path, ignoreWhitespace)
	}

	status, content, oldPath, baseRef, repoRoot := s.loadScopedFileState(path, scope, commit)

	s.mu.RLock()
	vc := s.VCS
	s.mu.RUnlock()

	hunks := computeScopedDiffHunks(path, scope, commit, status, oldPath, content, baseRef, repoRoot, vc, ignoreWhitespace)
	if hunks == nil {
		hunks = []vcs.DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}
