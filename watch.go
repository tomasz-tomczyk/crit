package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RefreshDiffs re-computes diff hunks for all files.
func (s *Session) RefreshDiffs() {
	// Snapshot file list and baseRef under read lock
	s.mu.RLock()
	type fileSnapshot struct {
		path    string
		status  string
		content string
	}
	snapshots := make([]fileSnapshot, 0, len(s.Files))
	for _, f := range s.Files {
		if f.Status == "deleted" || f.Lazy {
			continue
		}
		snapshots = append(snapshots, fileSnapshot{
			path:    f.Path,
			status:  f.Status,
			content: f.Content,
		})
	}
	baseRef := s.BaseRef
	repoRoot := s.RepoRoot
	vcs := s.VCS
	s.mu.RUnlock()

	// Compute diffs without holding any lock
	type diffResult struct {
		path  string
		hunks []DiffHunk
	}
	results := make([]diffResult, 0, len(snapshots))
	for _, snap := range snapshots {
		var hunks []DiffHunk
		if snap.status == "added" || snap.status == "untracked" {
			hunks = FileDiffUnifiedNewFile(snap.content)
		} else if vcs != nil {
			h, err := vcs.FileDiffUnified(snap.path, baseRef, repoRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: diff failed for %s: %v\n", snap.path, err)
			} else {
				hunks = h
			}
		}
		results = append(results, diffResult{path: snap.path, hunks: hunks})
	}

	// Assign results under write lock — look up by path, not stale pointer
	s.mu.Lock()
	for _, r := range results {
		for _, f := range s.Files {
			if f.Path == r.path {
				f.DiffHunks = r.hunks
				break
			}
		}
	}
	s.mu.Unlock()
}

// RefreshFileList re-runs ChangedFiles and updates the session's file list.
// New files are added, removed files are dropped.
func (s *Session) RefreshFileList() {
	s.mu.RLock()
	vcs := s.VCS
	s.mu.RUnlock()

	if vcs == nil {
		return
	}

	// Shell out to VCS for changed files — no lock held.
	s.mu.RLock()
	baseRef := s.BaseRef
	repoRoot := s.RepoRoot
	s.mu.RUnlock()

	var changes []FileChange
	var err error
	if vcs.CurrentBranch() == vcs.DefaultBranch() {
		changes, err = vcs.ChangedFilesOnDefaultInDir(repoRoot)
	} else {
		changes, err = vcs.ChangedFilesFromBaseInDir(baseRef, repoRoot)
	}
	if err != nil {
		return
	}

	// Apply ignore patterns
	changes = filterIgnored(changes, s.IgnorePatterns)

	// Snapshot existing files under read lock
	s.mu.RLock()
	existing := make(map[string]*FileEntry, len(s.Files))
	for _, f := range s.Files {
		existing[f.Path] = f
	}
	s.mu.RUnlock()

	// Fetch numstats if we might need them for lazy files
	var numstats map[string]NumstatEntry
	if len(changes) > lazyFileThreshold {
		if baseRef != "" {
			numstats, _ = vcs.DiffNumstat(baseRef, repoRoot)
		}
	}

	// Build new file list, doing I/O (os.ReadFile, sha256) without holding the lock.
	// Status updates for existing entries are deferred to the write-lock section
	// to avoid racing with concurrent readers.
	type existingUpdate struct {
		entry  *FileEntry
		status string
	}
	var newFiles []*FileEntry
	var updates []existingUpdate
	for i, fc := range changes {
		if f, ok := existing[fc.Path]; ok {
			updates = append(updates, existingUpdate{f, fc.Status})
			newFiles = append(newFiles, f)
		} else {
			absPath := filepath.Join(repoRoot, fc.Path)
			fe := &FileEntry{
				Path:     fc.Path,
				AbsPath:  absPath,
				Status:   fc.Status,
				FileType: detectFileType(fc.Path),
				Comments: []Comment{},
			}

			// Apply lazy threshold for newly discovered files
			if len(changes) > lazyFileThreshold && i >= lazyFileThreshold {
				fe.Lazy = true
				if ns, ok := numstats[fc.Path]; ok {
					fe.LazyAdditions = ns.Additions
					fe.LazyDeletions = ns.Deletions
				}
			} else if fc.Status != "deleted" {
				if data, err := os.ReadFile(absPath); err == nil {
					fe.Content = string(data)
					fe.FileHash = fileHash(data)
				}
			}

			newFiles = append(newFiles, fe)
		}
	}

	// Assign under write lock
	s.mu.Lock()
	for _, u := range updates {
		u.entry.Status = u.status
	}
	s.Files = newFiles
	s.mu.Unlock()
}

// Watch dispatches to the appropriate file-watching strategy based on session mode.
func (s *Session) Watch(stop <-chan struct{}) {
	if s.Mode == "git" {
		s.watchGit(stop)
	} else {
		// Both "files" and "plan" modes use file mtime polling.
		s.watchFileMtimes(stop)
	}
}

// watchGit polls `git status --porcelain` for working tree changes.
// Used in git mode (no-args invocation).
//
// Git status polling only runs during the "waiting for agent" phase (between
// POST /api/finish and POST /api/round-complete). mergeExternalCritJSON runs
// on every tick since it only uses os.Stat.
func (s *Session) watchGit(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Read VCS once under lock — it doesn't change after session init.
	s.mu.RLock()
	vcs := s.VCS
	s.mu.RUnlock()

	var lastFP string
	wasWaiting := false

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Check for external review file changes (e.g. crit comment).
			s.mergeExternalCritJSON()

			// Only poll VCS status while waiting for the agent to make edits.
			if !s.isWaitingForAgent() {
				wasWaiting = false
				continue
			}

			var fp string
			if vcs != nil {
				fp = vcs.WorkingTreeFingerprint()
			} else {
				fp = WorkingTreeFingerprint()
			}
			if !wasWaiting {
				// Just entered waiting state — establish baseline.
				lastFP = fp
				wasWaiting = true
				continue
			}
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
			// Check for external review file changes (e.g. crit comment).
			s.mergeExternalCritJSON()

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
				hash := fileHash(data)

				s.mu.Lock()
				// Re-check hash under write lock to avoid racing with AddComment.
				// Without this, a comment added between a read-lock check and this
				// write lock would be silently discarded.
				if hash == f.FileHash {
					s.mu.Unlock()
					continue
				}
				// Snapshot on first edit of a round (markdown files)
				if f.FileType == "markdown" && s.pendingEdits == 0 {
					f.PreviousContent = f.Content
					f.PreviousComments = make([]Comment, len(f.Comments))
					copy(f.PreviousComments, f.Comments)
				}
				f.Content = string(data)
				f.FileHash = hash
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

func carryForwardComment(old Comment, newID string, now string) Comment {
	return Comment{
		ID:             newID,
		StartLine:      old.StartLine,
		EndLine:        old.EndLine,
		Side:           old.Side,
		Body:           old.Body,
		Quote:          old.Quote,
		QuoteOffset:    old.QuoteOffset,
		Anchor:         old.Anchor,
		Author:         old.Author,
		Scope:          old.Scope,
		CreatedAt:      old.CreatedAt,
		UpdatedAt:      now,
		Resolved:       old.Resolved,
		CarriedForward: true,
		Live:           old.Live,
		ReviewRound:    old.ReviewRound,
		Replies:        old.Replies,
		GitHubID:       old.GitHubID,
	}
}

// carryForwardAllComments carries forward all PreviousComments at their original positions.
// Must be called with s.mu held for writing.
func (s *Session) carryForwardAllComments() {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range s.Files {
		// Skip if comments were already carried forward (e.g. by carryForwardComments)
		if len(f.Comments) > 0 {
			continue
		}
		for _, c := range f.PreviousComments {
			carried := carryForwardComment(c, randomCommentID(), now)
			f.Comments = append(f.Comments, carried)
			// Track the old ID as deleted so mergeFileSnapshotIntoCritJSON
			// won't re-add the original from disk alongside the carried-forward copy.
			s.trackDeletedComment(f.Path, c.ID)
		}
	}
}

// rereadFileContents re-reads all non-deleted files from disk and updates Content/FileHash.
// If snapshotMarkdown is true, PreviousContent is set before overwriting (for files mode).
// Must be called with s.mu held for writing.
func (s *Session) rereadFileContents(snapshotMarkdown bool) {
	for _, f := range s.Files {
		if f.Status == "deleted" || f.Lazy {
			continue
		}
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		if snapshotMarkdown && f.FileType == "markdown" && f.PreviousContent == "" {
			f.PreviousContent = f.Content
		}
		f.Content = string(data)
		f.FileHash = fileHash(data)
	}
}

// finishRoundComplete emits terminal status and notifies SSE subscribers.
func (s *Session) finishRoundComplete(edits int) {
	s.emitRoundStatus(edits)
	s.notify(SSEEvent{
		Type:    "file-changed",
		Content: "session",
	})
}

// handleRoundCompleteGit handles round completion in git mode.
// Re-runs ChangedFiles, re-computes diffs, refreshes file list.
// Must only be called from the single watcher goroutine (watchGit).
func (s *Session) handleRoundCompleteGit() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	s.loadResolvedComments()

	// Refresh file list (agent may have created/deleted files)
	s.RefreshFileList()

	// Snapshot PreviousContent before re-reading for all files with comments.
	// LCS + anchor verification is used for all file types.
	s.mu.Lock()
	for _, f := range s.Files {
		if f.PreviousContent == "" && len(f.PreviousComments) > 0 {
			f.PreviousContent = f.Content
		}
	}
	s.rereadFileContents(false)
	s.mu.Unlock()

	// Run LCS-based carry-forward with anchor verification for all file types.
	s.carryForwardComments()

	// Carry forward remaining files (code files, or markdown files without PreviousContent).
	s.mu.Lock()
	s.carryForwardAllComments()
	s.mu.Unlock()

	// Restore phantom entries for files that disappeared but have comments in the review file.
	// Must be called outside s.mu.Lock since it acquires the lock internally.
	s.restoreOrphanedComments()

	s.mu.Lock()
	s.ReviewRound++
	s.mu.Unlock()

	// Refresh diffs for all files
	s.RefreshDiffs()

	s.finishRoundComplete(edits)
}

// handleRoundCompleteFiles handles round completion in files mode.
// Re-reads files, carries forward unresolved comments.
// Must only be called from the single watcher goroutine (watchFileMtimes).
func (s *Session) handleRoundCompleteFiles() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	s.loadResolvedComments()
	s.carryForwardComments()

	s.mu.Lock()
	s.carryForwardAllComments()
	s.mu.Unlock()

	// Restore phantom entries for files that disappeared but have comments in the review file.
	s.restoreOrphanedComments()

	// Re-read all file contents and update hashes
	// (snapshot markdown PreviousContent in case watcher hasn't polled yet)
	s.mu.Lock()
	s.rereadFileContents(true)
	s.ReviewRound++
	s.mu.Unlock()

	s.finishRoundComplete(edits)
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

// loadResolvedComments reads the review file to pick up resolved fields the agent wrote.
func (s *Session) loadResolvedComments() {
	critPath := s.critJSONPath()
	info, statErr := os.Stat(critPath)
	data, err := os.ReadFile(critPath)
	if err != nil {
		// No review file — clear all PreviousComments
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
	// Restore review-level comments so they survive round-complete.
	// Always overwrite (even when disk has 0) to clear stale in-memory state.
	s.reviewComments = cj.ReviewComments
	// Record the current mtime so mergeExternalCritJSON does not re-process
	// this same file. Without this, the file watcher could detect the
	// externally-written review file (e.g. from a test or crit comment) as a
	// new change and wipe comments that were added via the API after the
	// round completed.
	if statErr == nil {
		s.lastCritJSONMtime = info.ModTime()
	}
}

// findAnchorInLines searches for the anchor text in the given lines (joined with newline).
// Returns the 1-indexed start line of the best match, or 0 if not found.
// If multiple matches exist, returns the one closest to preferredStart.
func findAnchorInLines(lines []string, anchor string, preferredStart int) int {
	anchorLines := strings.Split(anchor, "\n")
	anchorLen := len(anchorLines)
	if anchorLen == 0 || len(lines) < anchorLen {
		return 0
	}

	var matches []int
	for i := 0; i <= len(lines)-anchorLen; i++ {
		candidate := strings.Join(lines[i:i+anchorLen], "\n")
		if candidate == anchor {
			matches = append(matches, i+1) // 1-indexed
		}
	}

	if len(matches) == 0 {
		return 0
	}
	if len(matches) == 1 {
		return matches[0]
	}

	// Multiple matches: pick closest to the LCS-suggested position.
	best := matches[0]
	bestDist := abs(best - preferredStart)
	for _, m := range matches[1:] {
		d := abs(m - preferredStart)
		if d < bestDist {
			best = m
			bestDist = d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// verifyAndCorrectPosition checks whether the LCS-remapped position still
// points at the anchor text. If not, it searches the new content for the anchor.
// Returns the corrected (start, end) and whether the comment has drifted.
func verifyAndCorrectPosition(newLines []string, anchor string, lcsStart, lcsEnd int) (start, end, drifted int) {
	anchorLines := strings.Split(anchor, "\n")
	anchorLen := len(anchorLines)

	// Check if the LCS position still matches.
	if lcsStart >= 1 && lcsStart+anchorLen-1 <= len(newLines) {
		candidate := strings.Join(newLines[lcsStart-1:lcsStart+anchorLen-1], "\n")
		if candidate == anchor {
			return lcsStart, lcsStart + anchorLen - 1, 0
		}
	}

	// LCS position doesn't match — search the entire file.
	found := findAnchorInLines(newLines, anchor, lcsStart)
	if found > 0 {
		return found, found + anchorLen - 1, 0
	}

	// Anchor not found anywhere — mark drifted, keep LCS position.
	return lcsStart, lcsEnd, 1
}

// remapLines translates old start/end line numbers through the LCS line map,
// falling back to the original positions and clamping to [1, maxLine].
func remapLines(lineMap map[int]int, oldStart, oldEnd, maxLine int) (int, int) {
	s := lineMap[oldStart]
	e := lineMap[oldEnd]
	if s == 0 {
		s = oldStart
	}
	if e == 0 {
		e = oldEnd
	}
	if s > maxLine {
		s = maxLine
	}
	if e > maxLine {
		e = maxLine
	}
	if s < 1 {
		s = 1
	}
	if e < s {
		e = s
	}
	return s, e
}

// carryForwardComments maps comments from the previous round to new document
// positions using LCS line mapping + anchor verification. Works for all file
// types (markdown and code) that have PreviousContent and PreviousComments.
// Files without PreviousContent are left for carryForwardAllComments.
func (s *Session) carryForwardComments() {
	s.mu.RLock()
	var toProcess []*FileEntry
	for _, f := range s.Files {
		if f.PreviousContent != "" && len(f.PreviousComments) > 0 {
			toProcess = append(toProcess, f)
		}
	}
	s.mu.RUnlock()

	for _, f := range toProcess {
		s.carryForwardFileComments(f)
	}
}

// carryForwardFileComments remaps comments for a single file using LCS line
// mapping with anchor-based verification and correction.
//
// Old-side comments (c.Side == "old") reference the base ref, not the working
// tree. Their line numbers and anchor text are stable across rounds (the base
// ref doesn't change), so they are carried forward at their original positions
// without LCS remapping or anchor search.
func (s *Session) carryForwardFileComments(f *FileEntry) {
	s.mu.RLock()
	prevContent := f.PreviousContent
	currContent := f.Content
	prevComments := make([]Comment, len(f.PreviousComments))
	copy(prevComments, f.PreviousComments)
	s.mu.RUnlock()

	if len(prevComments) == 0 {
		return
	}

	entries := ComputeLineDiff(prevContent, currContent)
	lineMap := MapOldLineToNew(entries)

	newLines := splitLines(currContent)
	newLineCount := len(newLines)
	if newLineCount == 0 {
		newLineCount = 1
	}

	s.mu.Lock()
	f.Comments = nil // Clear before carry-forward to prevent duplicates
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range prevComments {
		s.trackDeletedComment(f.Path, c.ID)

		// File-level and old-side comments keep their original positions.
		// File-level comments have no line references. Old-side comments
		// reference the base ref which doesn't change between rounds.
		if c.Scope == "file" || c.Side == "old" {
			f.Comments = append(f.Comments, carryForwardComment(c, randomCommentID(), now))
			continue
		}
		newStart, newEnd := remapLines(lineMap, c.StartLine, c.EndLine, newLineCount)
		carried := carryForwardComment(c, randomCommentID(), now)
		carried.StartLine = newStart
		carried.EndLine = newEnd

		if c.Anchor != "" {
			corrStart, corrEnd, drift := verifyAndCorrectPosition(newLines, c.Anchor, newStart, newEnd)
			carried.StartLine = corrStart
			carried.EndLine = corrEnd
			carried.Drifted = drift != 0
		}

		f.Comments = append(f.Comments, carried)
	}
	s.mu.Unlock()
}
