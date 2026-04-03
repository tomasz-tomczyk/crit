package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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
		if f.Status == "deleted" || f.Lazy {
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
	repoRoot := s.RepoRoot
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
			h, err := fileDiffUnified(snap.path, baseRef, repoRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", snap.path, err)
			} else {
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

	// Apply ignore patterns
	changes = filterIgnored(changes, s.IgnorePatterns)

	// Snapshot existing files under read lock
	s.mu.RLock()
	existing := make(map[string]*FileEntry, len(s.Files))
	for _, f := range s.Files {
		existing[f.Path] = f
	}
	repoRoot := s.RepoRoot
	baseRef := s.BaseRef
	s.mu.RUnlock()

	// Fetch numstats if we might need them for lazy files
	var numstats map[string]NumstatEntry
	if len(changes) > lazyFileThreshold {
		if baseRef != "" {
			numstats, _ = DiffNumstatDir(baseRef, repoRoot)
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
func (s *Session) watchGit(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastFP := WorkingTreeFingerprint()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Check for external .crit.json changes (e.g. crit comment).
			s.mergeExternalCritJSON()

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
			// Check for external .crit.json changes (e.g. crit comment).
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
		Author:         old.Author,
		Scope:          old.Scope,
		CreatedAt:      old.CreatedAt,
		UpdatedAt:      now,
		Resolved:       old.Resolved,
		CarriedForward: true,
		ReviewRound:    old.ReviewRound,
		Replies:        old.Replies,
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
			carried := carryForwardComment(c, fmt.Sprintf("c%d", s.nextID), now)
			s.nextID++
			f.Comments = append(f.Comments, carried)
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

	s.mu.Lock()
	s.rereadFileContents(false)
	s.carryForwardAllComments()
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

// loadResolvedComments reads .crit.json to pick up resolved fields the agent wrote.
func (s *Session) loadResolvedComments() {
	critPath := s.critJSONPath()
	info, statErr := os.Stat(critPath)
	data, err := os.ReadFile(critPath)
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
	// Restore review-level comments so they survive round-complete.
	// Always overwrite (even when disk has 0) to clear stale in-memory state.
	s.reviewComments = cj.ReviewComments
	s.reviewNextID = 0
	for _, c := range s.reviewComments {
		id := 0
		fmt.Sscanf(c.ID, "r%d", &id)
		if id >= s.reviewNextID {
			s.reviewNextID = id + 1
		}
	}
	// Record the current mtime so mergeExternalCritJSON does not re-process
	// this same file. Without this, the file watcher could detect the
	// externally-written .crit.json (e.g. from a test or crit comment) as a
	// new change and wipe comments that were added via the API after the
	// round completed.
	if statErr == nil {
		s.lastCritJSONMtime = info.ModTime()
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
			// File-level comments have no line references — carry forward as-is.
			if c.Scope == "file" {
				carried := carryForwardComment(c, fmt.Sprintf("c%d", s.nextID), now)
				s.nextID++
				f.Comments = append(f.Comments, carried)
				continue
			}
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
			carried := carryForwardComment(c, fmt.Sprintf("c%d", s.nextID), now)
			carried.StartLine = newStart
			carried.EndLine = newEnd
			s.nextID++
			f.Comments = append(f.Comments, carried)
		}
		s.mu.Unlock()
	}
}
