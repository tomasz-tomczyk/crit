package session

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// scheduleWrite debounces writes to disk.
// scheduleWrite must be called with s.mu held.
func (s *Session) scheduleWrite() {
	s.pendingWrite = true
	s.writeSeq++
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
		_ = s.writeFilesErr()
	})
}

// critJSONPath returns the review identity path. In v4 the identity is a
// folder containing review.json and snapshots.json — see ReviewPathsFor.
func (s *Session) critJSONPath() string {
	if s.OutputDir != "" {
		return filepath.Join(s.OutputDir, ".crit")
	}
	if s.ReviewFilePath != "" {
		return s.ReviewFilePath
	}
	if s.RepoRoot == "" {
		// No review destination configured. Returning "" keeps writes out of
		// the process working directory, which tests mutate with os.Chdir.
		return ""
	}
	// Fallback for tests and backwards compat
	return filepath.Join(s.RepoRoot, ".crit")
}

// writeFilesSnapshot holds all session state needed to write the review file,
// captured under lock so that disk I/O can happen without holding the lock.
type writeFilesSnapshot struct {
	critPath string
	// writeSeq is Session.writeSeq at snapshot time. A write clears
	// pendingWrite only if no change was scheduled after its snapshot.
	writeSeq        uint64
	lastMtime       time.Time
	branch          string
	baseRef         string
	reviewRound     int
	sharedURL       string
	shareBaseURL    string
	deleteToken     string
	shareScope      string
	shareOrg        string
	shareOrgName    string
	shareVisibility string
	reviewComments  []Comment
	cliArgs         []string
	cwd             string
	// story is the session's in-memory narrative (nil if none). Carried into
	// CritJSON like the other daemon-managed fields above so `crit story`'s
	// daemon-side mutators (set via s.SetStory) are actually persisted —
	// buildCritJSON otherwise only preserves whatever was already on disk.
	story                   *Story
	pendingRemoteDeletes    []RemoteRef
	lastLoadedRemoteDeletes map[RemoteRef]struct{}
	deletedCommentIDs       map[string]map[string]struct{}
	resolvedDirtyIDs        map[string]uint64
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

// handleExternalDeletion checks if the review file was deleted externally and clears
// in-memory comments if so. Returns true if the file was deleted.
func (s *Session) handleExternalDeletion(critPath string) bool {
	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if lastMtime.IsZero() {
		return false
	}
	if _, statErr := os.Stat(ReviewPathsFor(critPath).Review); !os.IsNotExist(statErr) {
		return false
	}

	s.clearAllCommentData()
	return true
}

// clearAllCommentData resets all in-memory comment state (file comments,
// review comments, and ID counters) and notifies if any comments existed.
// Caller must NOT hold s.mu.
//
// ReviewRound is also reset to 1: this function runs when the review file
// is deleted out from under the daemon (`crit cleanup`, manual `rm`,
// hosted-side unpublish). Without the reset, a long-lived daemon (idle
// timeout 1h) keeps an in-memory ReviewRound from a prior life, and the
// next pin authored after the disk wipe ships against that stale round —
// surfaced in the UI as "Round #2 (or higher) on a brand-new review".
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
	s.resolvedDirtyIDs = nil
	s.ReviewRound = 1
	s.RoundSnapshots = nil
	s.mu.Unlock()
	if anyComments {
		s.notify(SSEEvent{Type: "comments-changed"})
	}
}

// buildCritJSON loads the existing review file from disk, applies the snapshot
// metadata, and merges file and review-level comments. unloaded reports that
// disk held comments the snapshot lacks (written by another
// process), so memory should reload them.
func buildCritJSON(snap writeFilesSnapshot) (cj CritJSON, unloaded bool) {
	cj = CritJSON{Files: make(map[string]CritJSONFile)}
	if data, err := ReadFileShared(ReviewPathsFor(snap.critPath).Review); err == nil {
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
	cj.ShareBaseURL = snap.shareBaseURL
	cj.DeleteToken = snap.deleteToken
	cj.ShareScope = snap.shareScope
	cj.ShareOrg = snap.shareOrg
	cj.ShareOrgName = snap.shareOrgName
	cj.ShareVisibility = snap.shareVisibility
	cj.ReviewComments, unloaded = mergeDiskComments(snap.reviewComments, cj.ReviewComments,
		snap.deletedCommentIDs[reviewCommentTombstones])
	cj.CliArgs = snap.cliArgs
	// Only daemon-built snapshots carry a cwd; an empty one must leave the
	// recorded directory alone rather than erase a resumable review's only
	// pointer back to its working tree.
	if snap.cwd != "" {
		cj.CWD = snap.cwd
	}
	cj.Story = snap.story
	cj.PendingRemoteDeletes = reconcilePendingRemoteDeletes(
		snap.pendingRemoteDeletes, cj.PendingRemoteDeletes, snap.lastLoadedRemoteDeletes,
	)

	for _, fs := range snap.files {
		if mergeFileSnapshotIntoCritJSON(&cj, fs, snap.deletedCommentIDs[fs.path]) {
			unloaded = true
		}
	}
	applyDeletedCommentIDs(&cj, snap.deletedCommentIDs)
	return cj, unloaded
}

// mergeDiskComments returns mem plus the comments only disk has, skipping
// tombstoned ones. Memory wins for a comment in both: another writer's new
// comments survive a daemon write, but its edits to a comment memory also
// holds do not (mergeRepliesWrittenSinceLastWrite has already taken in new
// replies). added reports whether disk contributed.
func mergeDiskComments(mem, disk []Comment, deleted map[string]struct{}) (merged []Comment, added bool) {
	if len(disk) == 0 {
		return mem, false
	}
	merged = append(make([]Comment, 0, len(mem)+len(disk)), mem...)
	memIDs := make(map[string]struct{}, len(mem))
	for _, mc := range mem {
		memIDs[mc.ID] = struct{}{}
	}
	for _, dc := range disk {
		if _, inMem := memIDs[dc.ID]; inMem {
			continue
		}
		if _, gone := deleted[dc.ID]; gone {
			continue
		}
		merged = append(merged, dc)
		added = true
	}
	return merged, added
}

// applyDeletedCommentIDs removes explicitly deleted comments even when their
// paths no longer have a FileEntry (for example, after a base change or rename).
// Tombstones are authoritative over both disk-only and in-memory copies.
func applyDeletedCommentIDs(cj *CritJSON, deletedByPath map[string]map[string]struct{}) {
	for path, deletedIDs := range deletedByPath {
		file, ok := cj.Files[path]
		if !ok || len(deletedIDs) == 0 {
			continue
		}
		comments := file.Comments[:0]
		for _, c := range file.Comments {
			if _, deleted := deletedIDs[c.ID]; !deleted {
				comments = append(comments, c)
			}
		}
		if len(comments) == 0 {
			delete(cj.Files, path)
			continue
		}
		file.Comments = comments
		cj.Files[path] = file
	}
}

func reconcilePendingRemoteDeletes(snap, disk []RemoteRef, lastLoaded map[RemoteRef]struct{}) []RemoteRef {
	if len(snap) == 0 {
		return nil
	}
	diskSet := make(map[RemoteRef]struct{}, len(disk))
	for _, ref := range disk {
		diskSet[ref] = struct{}{}
	}
	out := make([]RemoteRef, 0, len(snap))
	seen := make(map[RemoteRef]struct{}, len(snap))
	for _, ref := range snap {
		if _, duplicate := seen[ref]; duplicate {
			continue
		}
		_, onDisk := diskSet[ref]
		_, wasLoaded := lastLoaded[ref]
		if !onDisk && wasLoaded {
			continue
		}
		out = append(out, ref)
		seen[ref] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeFileSnapshotIntoCritJSON merges a single file's comments from the
// snapshot with disk-only comments, and updates the CritJSON. It
// reports whether disk contributed comments the snapshot lacks.
func mergeFileSnapshotIntoCritJSON(cj *CritJSON, fs writeFileSnapshot, deleted map[string]struct{}) bool {
	merged, added := mergeDiskComments(fs.comments, cj.Files[fs.path].Comments, deleted)

	if len(merged) == 0 {
		delete(cj.Files, fs.path)
		return added
	}

	cj.Files[fs.path] = CritJSONFile{
		Status:   fs.status,
		FileHash: fs.fileHash,
		Comments: merged,
	}
	return added
}

func critJSONIsEmpty(cj CritJSON) bool {
	return len(cj.Files) == 0 && len(cj.ReviewComments) == 0 &&
		cj.ShareURL == "" && cj.ShareBaseURL == "" && cj.DeleteToken == "" && cj.ShareScope == "" &&
		cj.Story == nil && len(cj.PendingRemoteDeletes) == 0
}

// WriteFiles writes the review file to disk.
//
// The implementation snapshots all needed session state under RLock, then
// releases the lock before doing any disk I/O (ReadFile, Stat, WriteFile).
// This prevents a slow filesystem from blocking comment operations.
//
// Concurrency note: callers that need to guarantee the write is observable
// before returning a path to a client (e.g. POST /api/finish) MUST use
// SyncWriteFiles, which surfaces write errors. WriteFiles uses the same
// serialization but preserves the best-effort API for existing callers.
func (s *Session) WriteFiles() {
	_ = s.SyncWriteFiles()
}

// SyncWriteFiles flushes pending state to disk synchronously, serialized
// against the debounce timer and ClearAllComments. It cancels any pending
// debounced write so the writeFilesErr call below is the authoritative
// flush. Returns an error if the on-disk write failed; callers translate
// that into a 5xx response rather than handing back a path to a missing
// file.
func (s *Session) SyncWriteFiles() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.syncWriteFilesLocked()
}

// syncWriteFilesLocked is SyncWriteFiles for a caller that already holds
// writeMu.
func (s *Session) syncWriteFilesLocked() error {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	// Bump writeGen so any timer callback that already passed Stop() but
	// has not yet acquired writeMu observes the gen change and bails.
	s.writeGen++
	s.mu.Unlock()

	return s.writeFilesErr()
}

// writeFilesErr is the actual write implementation. Returns an error so
// SyncWriteFiles can surface failures to HTTP callers; the timer callback
// and best-effort callers via WriteFiles ignore the error (preserving the
// pre-existing log-and-continue behaviour).
func (s *Session) writeFilesErr() error {
	critPath := s.critJSONPath()
	if critPath == "" {
		return nil
	}

	// Check before taking the lock: the lock recreates the review folder.
	if s.handleExternalDeletion(critPath) {
		return nil
	}
	// Preserve the no-op for an empty session without creating a review folder.
	if _, err := os.Stat(critPath); os.IsNotExist(err) {
		snap := s.snapshotForWrite(critPath)
		if cj, _ := buildCritJSON(snap); critJSONIsEmpty(cj) {
			s.markEmptyWritten(snap)
			return nil
		}
	}
	return WithReviewWriteLock(critPath, func() error {
		return s.writeReviewFile(critPath)
	})
}

// Caller holds writeMu and the review-file transaction lock.
func (s *Session) writeReviewFile(critPath string) error {
	if s.handleExternalDeletion(critPath) {
		return nil
	}
	s.mergeRepliesWrittenSinceLastWrite(critPath)

	snap := s.snapshotForWrite(critPath)
	cj, unloaded := buildCritJSON(snap)

	paths := ReviewPathsFor(snap.critPath)
	if critJSONIsEmpty(cj) {
		// B1: remove ONLY review.json. Snapshots are server-only state and
		// may still be valid for the timeline; full-folder cleanup belongs to
		// explicit cleanup paths (clearCritJSON, ClearAllComments,
		// deleteStaleReviews, cleanupOnApproval).
		if err := os.Remove(paths.Review); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing empty review file: %w", err)
		}
		s.markEmptyWritten(snap)
		return nil
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling review file: %v\n", err)
		return fmt.Errorf("marshaling review file: %w", err)
	}
	if err := AtomicWriteFile(paths.Review, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing review file: %v\n", err)
		return fmt.Errorf("writing review file: %w", err)
	}
	if info, err := os.Stat(paths.Review); err == nil {
		s.mu.Lock()
		s.lastCritJSONMtime = info.ModTime()
		// The watcher skips a file whose mtime matches our own write, so
		// flag comments another process added for it to load.
		s.diskHasUnloadedComments = unloaded
		s.clearPendingWrite(snap)
		s.clearWrittenDeletedCommentIDs(snap.deletedCommentIDs)
		s.clearWrittenResolvedDirtyIDs(snap.resolvedDirtyIDs)
		// Sync in-memory pending-delete state to what we just wrote, so the
		// next snapshot does not re-include IDs that a concurrent
		// `crit push` already drained. See reconcilePendingRemoteDeletes.
		s.pendingRemoteDeletes = append(s.pendingRemoteDeletes[:0:0], cj.PendingRemoteDeletes...)
		s.lastLoadedPendingDeletes = make(map[RemoteRef]struct{}, len(cj.PendingRemoteDeletes))
		for _, ref := range cj.PendingRemoteDeletes {
			s.lastLoadedPendingDeletes[ref] = struct{}{}
		}
		s.mu.Unlock()
	}
	return nil
}

// clearPendingWrite clears pendingWrite after snap was written, unless a
// change was scheduled after the snapshot: that change is not on disk yet,
// and the watcher must not reload over it. Caller must hold s.mu.
func (s *Session) clearPendingWrite(snap writeFilesSnapshot) {
	if s.writeSeq == snap.writeSeq {
		s.pendingWrite = false
	}
}

// markEmptyWritten records that an empty snapshot is on disk (no review.json),
// so the watcher stops treating the session as having a pending write.
func (s *Session) markEmptyWritten(snap writeFilesSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCritJSONMtime = time.Time{}
	s.clearPendingWrite(snap)
	s.clearWrittenDeletedCommentIDs(snap.deletedCommentIDs)
	s.clearWrittenResolvedDirtyIDs(snap.resolvedDirtyIDs)
	s.pendingRemoteDeletes = nil
	s.lastLoadedPendingDeletes = nil
}

// clearWrittenDeletedCommentIDs acknowledges only tombstones included in a
// completed write snapshot. Deletions recorded while that write was doing disk
// I/O belong to a later write and must remain pending. Caller must hold s.mu.
func (s *Session) clearWrittenDeletedCommentIDs(written map[string]map[string]struct{}) {
	for path, ids := range written {
		current := s.deletedCommentIDs[path]
		for id := range ids {
			delete(current, id)
		}
		if len(current) == 0 {
			delete(s.deletedCommentIDs, path)
		}
	}
	if len(s.deletedCommentIDs) == 0 {
		s.deletedCommentIDs = nil
	}
}

// markResolvedDirtyLocked records that the reviewer changed id's resolved
// state. Caller must hold s.mu.
func (s *Session) markResolvedDirtyLocked(id string) {
	if s.resolvedDirtyIDs == nil {
		s.resolvedDirtyIDs = make(map[string]uint64)
	}
	s.resolvedDirtySeq++
	s.resolvedDirtyIDs[id] = s.resolvedDirtySeq
}

// clearWrittenResolvedDirtyIDs drops the resolved changes a completed write
// included. A change made again after the write's snapshot has a newer seq
// and stays. Caller must hold s.mu.
func (s *Session) clearWrittenResolvedDirtyIDs(written map[string]uint64) {
	for id, seq := range written {
		if s.resolvedDirtyIDs[id] == seq {
			delete(s.resolvedDirtyIDs, id)
		}
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
	pendDeletes := append([]RemoteRef(nil), s.pendingRemoteDeletes...)
	lastLoaded := make(map[RemoteRef]struct{}, len(s.lastLoadedPendingDeletes))
	for ref := range s.lastLoadedPendingDeletes {
		lastLoaded[ref] = struct{}{}
	}
	deletedByPath := make(map[string]map[string]struct{}, len(s.deletedCommentIDs))
	for path, ids := range s.deletedCommentIDs {
		deletedByPath[path] = make(map[string]struct{}, len(ids))
		for id := range ids {
			deletedByPath[path][id] = struct{}{}
		}
	}
	snap := writeFilesSnapshot{
		writeSeq:                s.writeSeq,
		critPath:                critPath,
		lastMtime:               s.lastCritJSONMtime,
		branch:                  s.Branch,
		baseRef:                 s.BaseRef,
		reviewRound:             s.ReviewRound,
		sharedURL:               s.sharedURL,
		shareBaseURL:            s.shareBaseURL,
		deleteToken:             s.deleteToken,
		shareScope:              s.shareScope,
		shareOrg:                s.shareOrg,
		shareOrgName:            s.shareOrgName,
		shareVisibility:         s.shareVisibility,
		reviewComments:          rc,
		cliArgs:                 s.CLIArgs,
		cwd:                     s.CWD,
		story:                   s.story,
		pendingRemoteDeletes:    pendDeletes,
		lastLoadedRemoteDeletes: lastLoaded,
		deletedCommentIDs:       deletedByPath,
		resolvedDirtyIDs:        maps.Clone(s.resolvedDirtyIDs),
		files:                   make([]writeFileSnapshot, len(s.Files)),
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
		if dc.ResolvedRound != mc.ResolvedRound {
			comments[i].ResolvedRound = dc.ResolvedRound
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
			memReviewIDs[dc.ID] = struct{}{}
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
		if dc.ResolvedRound != mc.ResolvedRound {
			s.reviewComments[i].ResolvedRound = dc.ResolvedRound
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

// SyncCommentsFromDisk saves pending in-memory changes, then reloads file-
// and review-level comments from review.json. Call after another writer
// (e.g. share.MergeWebComments) updated review.json. When this process makes
// that update, use ApplyReviewFileUpdate so no save can land in between.
func (s *Session) SyncCommentsFromDisk() bool {
	changed, _ := s.ApplyReviewFileUpdate(nil)
	return changed
}

// ApplyReviewFileUpdate saves pending in-memory changes, runs update (a
// read-modify-write of review.json by other code, such as a hosted comment
// merge), then reloads comments from disk. writeMu is held throughout, so a
// debounced save cannot overwrite the update, and the save first means the
// reload cannot drop comments that were only in memory. update may be nil.
// It is not run when the save fails. changed reports whether the reload
// changed in-memory comments.
func (s *Session) ApplyReviewFileUpdate(update func() error) (changed bool, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.syncWriteFilesLocked(); err != nil {
		return false, err
	}
	if update != nil {
		err = update()
	}

	s.mu.Lock()
	s.lastCritJSONMtime = time.Time{}
	s.mu.Unlock()
	return s.mergeExternalCritJSON(), err
}

// mergeRepliesWrittenSinceLastWrite takes into memory the replies that
// `crit comment --reply-to` wrote to the review file since the daemon's last
// write. Every write calls it first: the watcher skips its merge while a write
// is pending, and the write would otherwise overwrite those replies. Replies
// the reviewer deleted stay out, and it adds or removes no comments.
func (s *Session) mergeRepliesWrittenSinceLastWrite(critPath string) {
	path := ReviewPathsFor(critPath).Review
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	s.mu.RLock()
	lastWrite := s.lastCritJSONMtime
	s.mu.RUnlock()
	if info.ModTime().Equal(lastWrite) {
		return
	}
	data, err := ReadFileShared(path)
	if err != nil {
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}
	changed := false
	s.mu.Lock()
	for _, f := range s.Files {
		if diskFile, ok := cj.Files[f.Path]; ok {
			for _, dc := range diskFile.Comments {
				changed = s.addNewRepliesLocked(f.Comments, dc) || changed
			}
		}
	}
	for _, dc := range cj.ReviewComments {
		changed = s.addNewRepliesLocked(s.reviewComments, dc) || changed
	}
	s.mu.Unlock()
	if changed {
		s.notify(SSEEvent{Type: "comments-changed"})
	}
}

// addNewRepliesLocked appends disk replies of dc that memory lacks and the
// reviewer did not delete. When it adopts a reply it also takes dc's
// Resolved, ResolvedRound and UpdatedAt: `crit comment --reply-to` sets them
// along with the reply (--resolve resolves, a plain reply unresolves), and
// the next write would otherwise put memory's old values back on disk. It
// does not when the reviewer changed the resolved state since the last
// write: that change is newer than the one on disk. Caller must hold s.mu.
func (s *Session) addNewRepliesLocked(comments []Comment, dc Comment) bool {
	for i := range comments {
		if comments[i].ID != dc.ID {
			continue
		}
		known := make(map[string]struct{}, len(comments[i].Replies))
		for _, r := range comments[i].Replies {
			known[r.ID] = struct{}{}
		}
		changed := false
		for _, r := range dc.Replies {
			_, inMemory := known[r.ID]
			_, deleted := s.deletedReplyIDs[r.ID]
			if inMemory || deleted {
				continue
			}
			comments[i].Replies = append(comments[i].Replies, r)
			changed = true
		}
		if _, dirty := s.resolvedDirtyIDs[dc.ID]; changed && !dirty {
			comments[i].Resolved = dc.Resolved
			comments[i].ResolvedRound = dc.ResolvedRound
			comments[i].UpdatedAt = dc.UpdatedAt
		}
		return changed
	}
	return false
}

// markReplyDeletedLocked records a reply the reviewer deleted. Caller must
// hold s.mu.
func (s *Session) markReplyDeletedLocked(replyID string) {
	if s.deletedReplyIDs == nil {
		s.deletedReplyIDs = make(map[string]struct{})
	}
	s.deletedReplyIDs[replyID] = struct{}{}
}

func (s *Session) mergeExternalCritJSON() bool {
	critPath := s.critJSONPath()

	info, err := os.Stat(ReviewPathsFor(critPath).Review)

	s.mu.RLock()
	lastMtime := s.lastCritJSONMtime
	s.mu.RUnlock()

	if err != nil {
		if !lastMtime.IsZero() {
			return s.handleCritJSONDeleted()
		}
		return false
	}

	s.mu.RLock()
	unloaded := s.diskHasUnloadedComments
	s.mu.RUnlock()
	if !lastMtime.IsZero() && info.ModTime().Equal(lastMtime) && !unloaded {
		return false
	}

	s.mu.RLock()
	pending := s.pendingWrite
	s.mu.RUnlock()
	if pending {
		return false
	}

	data, err := ReadFileShared(ReviewPathsFor(critPath).Review)
	if err != nil {
		return false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return false
	}

	s.mu.RLock()
	memRound := s.ReviewRound
	s.mu.RUnlock()
	// Memory is ahead of an explicit on-disk round (round-complete remapped
	// comments, write pending/failed, or a stale review.json raced in).
	// Applying that disk file would replace carried-forward IDs with the
	// prior round's comments and wipe drift bits — refuse until disk catches
	// up. Disk review_round 0 means unset/legacy (tests and older files);
	// never treat that as "stale prior round" or share merges stop applying.
	if cj.ReviewRound > 0 && memRound > cj.ReviewRound {
		s.mu.Lock()
		s.lastCritJSONMtime = info.ModTime()
		s.diskHasUnloadedComments = false
		s.mu.Unlock()
		return false
	}

	s.mu.Lock()
	s.lastCritJSONMtime = info.ModTime()
	s.diskHasUnloadedComments = false
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
	changed = s.adoptExternalPinRoutes(&cj) || changed

	changed = s.mergeReviewCommentsFromDisk(cj.ReviewComments) || changed
	s.mu.Unlock()

	if changed {
		s.notify(SSEEvent{Type: "comments-changed"})
	}

	return changed
}

// adoptExternalPinRoutes adds live-route file entries for DOM pins written
// by `crit comment` while this session is running. The merge above only
// updates paths the session already has, and the first pin on a page is
// what creates that path. Must be called with s.mu held.
func (s *Session) adoptExternalPinRoutes(cj *CritJSON) bool {
	if s.ReviewType != "live" && s.ReviewType != "preview" {
		return false
	}
	known := make(map[string]struct{}, len(s.Files))
	for _, f := range s.Files {
		known[f.Path] = struct{}{}
	}
	changed := false
	for path, diskFile := range cj.Files {
		if _, ok := known[path]; ok {
			continue
		}
		if !diskFileHasDOMPin(diskFile) {
			continue
		}
		comments := make([]Comment, len(diskFile.Comments))
		copy(comments, diskFile.Comments)
		status := diskFile.Status
		if status == "" {
			status = "added"
		}
		s.Files = append(s.Files, &FileEntry{
			Path:     path,
			Status:   status,
			FileType: "live-route",
			Comments: comments,
		})
		changed = true
	}
	return changed
}

func diskFileHasDOMPin(f CritJSONFile) bool {
	for _, c := range f.Comments {
		if c.DOMAnchor != nil {
			return true
		}
	}
	return false
}
