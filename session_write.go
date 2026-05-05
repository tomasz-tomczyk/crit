package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// scheduleWrite debounces writes to disk.
// scheduleWrite must be called with s.mu held.
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

// critJSONPath returns the review identity path. In v4 the identity is a
// folder containing review.json and snapshots.json — see reviewPathsFor.
func (s *Session) critJSONPath() string {
	if s.OutputDir != "" {
		return filepath.Join(s.OutputDir, ".crit")
	}
	if s.ReviewFilePath != "" {
		return s.ReviewFilePath
	}
	// Fallback for tests and backwards compat
	return filepath.Join(s.RepoRoot, ".crit")
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
	cliArgs        []string
	// pendingGHDeletes is the snapshot of session.pendingGitHubDeletes; carried
	// into CritJSON so the next push can drain DELETE intents that were never
	// flushed (e.g. user deleted, then quit before pushing).
	pendingGHDeletes []int64
	// lastLoadedGHDeletes is the set of GitHub IDs the daemon has previously
	// observed on disk in PendingGitHubDeletes. Used to distinguish "drained
	// by a concurrent `crit push`" (in lastLoaded but not in disk now) from
	// "freshly added since last load" (in snap but not in lastLoaded).
	lastLoadedGHDeletes map[int64]struct{}
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
	if _, statErr := os.Stat(reviewPathsFor(critPath).Review); !os.IsNotExist(statErr) {
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
	if data, err := os.ReadFile(reviewPathsFor(snap.critPath).Review); err == nil {
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
	cj.CliArgs = snap.cliArgs
	cj.PendingGitHubDeletes = reconcilePendingGHDeletes(
		snap.pendingGHDeletes, cj.PendingGitHubDeletes, snap.lastLoadedGHDeletes,
	)

	for _, fs := range snap.files {
		mergeFileSnapshotIntoCritJSON(&cj, fs)
	}
	return cj
}

// reconcilePendingGHDeletes merges the daemon's in-memory snapshot of pending
// GitHub-delete IDs against what is currently on disk. The Session and
// `crit push` are separate processes: push drains IDs by writing them out of
// disk's PendingGitHubDeletes. If we naively wrote snap back, a concurrent
// daemon write would resurrect drained IDs.
//
// Semantics:
//   - Keep an ID if it is still on disk (push has not drained it).
//   - Keep an ID if it is freshly added in-memory (not in lastLoaded), since
//     it was queued after our last disk read.
//   - Drop an ID that is in lastLoaded but no longer on disk — push drained
//     it concurrently; we must not resurrect it.
//
// Order is preserved from snap so callers see a stable queue.
func reconcilePendingGHDeletes(snap, disk []int64, lastLoaded map[int64]struct{}) []int64 {
	if len(snap) == 0 {
		return nil
	}
	diskSet := make(map[int64]struct{}, len(disk))
	for _, id := range disk {
		diskSet[id] = struct{}{}
	}
	out := make([]int64, 0, len(snap))
	seen := make(map[int64]struct{}, len(snap))
	for _, id := range snap {
		if _, dup := seen[id]; dup {
			continue
		}
		_, onDisk := diskSet[id]
		_, wasLoaded := lastLoaded[id]
		// Drop only when push has demonstrably drained it: previously seen
		// on disk AND no longer there. Fresh entries (not in lastLoaded)
		// are kept regardless of disk state.
		if !onDisk && wasLoaded {
			continue
		}
		out = append(out, id)
		seen[id] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

	paths := reviewPathsFor(snap.critPath)
	if critJSONIsEmpty(cj) {
		// B1: remove ONLY review.json. Snapshots are server-only state and
		// may still be valid for the timeline; full-folder cleanup belongs to
		// explicit cleanup paths (clearCritJSON, ClearAllComments,
		// deleteStaleReviews, cleanupOnApproval).
		os.Remove(paths.Review)
		s.mu.Lock()
		s.lastCritJSONMtime = time.Time{}
		s.pendingWrite = false
		s.deletedCommentIDs = nil
		s.pendingGitHubDeletes = nil
		s.lastLoadedPendingGHDeletes = nil
		s.mu.Unlock()
		return
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling review file: %v\n", err)
		return
	}
	if err := atomicWriteFile(paths.Review, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing review file: %v\n", err)
		return
	}
	if info, err := os.Stat(paths.Review); err == nil {
		s.mu.Lock()
		s.lastCritJSONMtime = info.ModTime()
		s.pendingWrite = false
		s.deletedCommentIDs = nil // written to disk, no longer needed
		// Sync in-memory pending-delete state to what we just wrote, so the
		// next snapshot does not re-include IDs that a concurrent
		// `crit push` already drained. See reconcilePendingGHDeletes.
		s.pendingGitHubDeletes = append(s.pendingGitHubDeletes[:0:0], cj.PendingGitHubDeletes...)
		s.lastLoadedPendingGHDeletes = make(map[int64]struct{}, len(cj.PendingGitHubDeletes))
		for _, id := range cj.PendingGitHubDeletes {
			s.lastLoadedPendingGHDeletes[id] = struct{}{}
		}
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
	pendDeletes := make([]int64, len(s.pendingGitHubDeletes))
	copy(pendDeletes, s.pendingGitHubDeletes)
	lastLoaded := make(map[int64]struct{}, len(s.lastLoadedPendingGHDeletes))
	for id := range s.lastLoadedPendingGHDeletes {
		lastLoaded[id] = struct{}{}
	}
	snap := writeFilesSnapshot{
		critPath:            critPath,
		lastMtime:           s.lastCritJSONMtime,
		branch:              s.Branch,
		baseRef:             s.BaseRef,
		reviewRound:         s.ReviewRound,
		sharedURL:           s.sharedURL,
		deleteToken:         s.deleteToken,
		shareScope:          s.shareScope,
		reviewComments:      rc,
		cliArgs:             s.CLIArgs,
		pendingGHDeletes:    pendDeletes,
		lastLoadedGHDeletes: lastLoaded,
		files:               make([]writeFileSnapshot, len(s.Files)),
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

func (s *Session) mergeExternalCritJSON() bool {
	critPath := s.critJSONPath()

	info, err := os.Stat(reviewPathsFor(critPath).Review)

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

	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
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
