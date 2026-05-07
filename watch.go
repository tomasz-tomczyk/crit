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
		ResolvedRound:  old.ResolvedRound,
		CarriedForward: true,
		Live:           old.Live,
		ReviewRound:    old.ReviewRound,
		Replies:        old.Replies,
		GitHubID:       old.GitHubID,
		// Preserve focus-scope tags from the original. Carrying forward must
		// preserve the comment's authored scope; restamping with the current
		// focus would silently strip scope tags across rounds.
		HeadSHA:   old.HeadSHA,
		DiffScope: old.DiffScope,
		FocusKey:  old.FocusKey,
	}
}

// carryForwardAllComments carries forward all PreviousComments at their original positions.
// Must be called with s.mu held for writing.
func (s *Session) carryForwardAllComments() {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range s.Files {
		if len(f.PreviousComments) == 0 {
			continue
		}
		// Skip if PreviousComments are already carried forward (e.g. by the
		// LCS path in carryForwardComments). Detect via CarriedForward marker
		// rather than `len(f.Comments) > 0`, because the latter spuriously
		// skips files that only contain comments added between
		// SignalRoundComplete and this handler — those are brand-new for the
		// new round and don't satisfy carry-forward, so PreviousComments
		// would be silently dropped.
		alreadyCarried := false
		for _, mc := range f.Comments {
			if mc.CarriedForward {
				alreadyCarried = true
				break
			}
		}
		if alreadyCarried {
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

	// Re-read all file contents and update hashes.
	// (snapshot markdown PreviousContent in case watcher hasn't polled yet)
	//
	// Capture the round we are about to commit BEFORE rereadFileContents and
	// BEFORE incrementing ReviewRound. On the first round-complete after boot,
	// ReviewRound == 1 (R1 baseline already captured at construction), so this
	// captures R2.
	s.mu.Lock()
	nextRound := s.ReviewRound + 1
	// INVARIANT: captureRoundSnapshot MUST run before rereadFileContents(true); reordering would snapshot the new on-disk content as the previous round and silently corrupt the timeline.
	s.captureRoundSnapshot(nextRound)
	sidecarPath := reviewPathsFor(s.critJSONPath()).Snapshots
	sf := SnapshotsFile{RoundSnapshots: cloneRoundSnapshots(s.RoundSnapshots)}
	s.rereadFileContents(true)
	s.ReviewRound++
	s.mu.Unlock()

	// File I/O off the hot path. Drift between review.json and snapshots.json
	// is benign (degrades to "no timeline available").
	//
	// ORDERING ASSUMPTION: sidecar writes from concurrent round-completes are
	// serialized by the debounced round-complete handler upstream — only one
	// round-complete is in-flight at a time, so the (clone-under-lock,
	// release-lock, write-off-lock) sequence cannot interleave with a second
	// captureRoundSnapshot/cloneRoundSnapshots cycle. If that upstream
	// debounce ever changes (e.g. round-completes become parallel), move the
	// saveSnapshotsFile call inside the s.mu.Lock() block above.
	if err := saveSnapshotsFile(sidecarPath, sf); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: write snapshots sidecar: %v\n", err)
	}

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
	info, statErr := os.Stat(reviewPathsFor(critPath).Review)
	data, err := readFileShared(reviewPathsFor(critPath).Review)
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
		// Edited-but-recognizable: if LCS predicts the same row and the line
		// is still close enough to the original, treat as anchored. Avoids
		// false drift when text was appended/trimmed/tweaked in place.
		if anchorSimilar(candidate, anchor) {
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

// anchorSimilar reports whether candidate and anchor are close enough to
// treat the comment as still anchored. Catches in-place edits (appended,
// trimmed, or lightly reworded text) that exact match would flag as drifted.
func anchorSimilar(candidate, anchor string) bool {
	a := strings.TrimSpace(candidate)
	b := strings.TrimSpace(anchor)
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	// Common case: text was appended to or trimmed from the anchor line.
	// Gate on a minimum length so trivial anchors (`}`, `return nil`) don't
	// match any longer line that happens to contain them.
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	if minLen >= 8 && (strings.Contains(a, b) || strings.Contains(b, a)) {
		return true
	}
	return levenshteinRatio(a, b) >= 0.7
}

// levenshteinRatio returns 1 - (distance / maxLen), clamped to [0, 1].
func levenshteinRatio(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 && lb == 0 {
		return 1
	}
	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	d := levenshtein(ar, br)
	return 1 - float64(d)/float64(maxLen)
}

// levenshtein computes edit distance between two rune slices using a
// rolling two-row buffer. O(la*lb) time, O(min(la,lb)) space.
func levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
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
	// Preserve any comments added between SignalRoundComplete (which clears
	// f.Comments) and the watcher actually processing the signal. Such
	// comments have IDs not present in PreviousComments (they're brand-new
	// for this round). On Windows, slow file I/O widens that race window;
	// without preservation, AddComment's append into the empty slice would
	// be wiped out by the f.Comments = nil assignment below.
	prevIDs := make(map[string]struct{}, len(prevComments))
	for _, c := range prevComments {
		prevIDs[c.ID] = struct{}{}
	}
	preserved := make([]Comment, 0, len(f.Comments))
	for _, c := range f.Comments {
		if _, isPrev := prevIDs[c.ID]; isPrev {
			continue
		}
		preserved = append(preserved, c)
	}
	f.Comments = preserved
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
