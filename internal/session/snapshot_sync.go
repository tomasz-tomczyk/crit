package session

import (
	"os"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/diff"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// liveChangesTTL is how long a fresh base-vs-working-tree file list is reused.
// One page load asks for the session and then for up to lazyFileThreshold
// file diffs at once; they share one git scan and see the same file list.
const liveChangesTTL = 2 * time.Second

// liveAllChanges returns the scope "all" file list (base vs working tree),
// filtered the same way as the session file list. Results are reused for
// liveChangesTTL.
func (s *Session) liveAllChanges(v vcs.VCS, baseRef, repoRoot string, ignorePatterns []string) ([]vcs.FileChange, error) {
	s.liveChangesMu.Lock()
	defer s.liveChangesMu.Unlock()
	key := baseRef + "\x00" + repoRoot
	if s.liveChangesKey == key && time.Now().Before(s.liveChangesExpiry) {
		return s.liveChanges, nil
	}
	changes, err := changedFilesForSession(v, baseRef, repoRoot)
	if err != nil {
		return nil, err
	}
	changes = filterBinary(config.FilterIgnored(changes, ignorePatterns))
	s.liveChanges = changes
	s.liveChangesKey = key
	s.liveChangesExpiry = time.Now().Add(liveChangesTTL)
	return changes, nil
}

// finishRoundPending ends the round-complete window that began with signal
// number seq: syncing resumes unless a newer signal arrived meanwhile.
func (s *Session) finishRoundPending(seq uint64) {
	s.mu.Lock()
	if s.roundSignalSeq == seq {
		s.roundCompletePending = false
	}
	s.mu.Unlock()
	s.clearLiveChanges()
}

// clearLiveChanges drops the cached scope "all" file list, so the fetch after
// a round complete sees the new working tree.
func (s *Session) clearLiveChanges() {
	s.liveChangesMu.Lock()
	s.liveChangesExpiry = time.Time{}
	s.liveChangesMu.Unlock()
}

// SyncWorkingTree brings the cached content of git-mode files up to date with
// the working tree, and moves each file's comments to the matching lines of
// the new content (the LCS mapping and anchor check that round complete uses).
// With no paths it checks every loaded file.
//
// The live scopes show working-tree content. Syncing when that content is
// served (a live session fetch, a scope "all" file diff) keeps the session's
// content the same as the text the browser shows: new comments take their
// anchor from it, and round complete carries comments forward from it. It is
// not synced when a comment is added, because the browser's line numbers
// refer to the text it loaded, not to what is on disk now.
//
// No-op outside git mode, under range focus (content is pinned to a SHA), and
// while a round complete is pending: in that window the comments have been
// cleared from memory and are reloaded from the review file with positions
// for the old content.
func (s *Session) SyncWorkingTree(paths ...string) {
	candidates, baseRef, repoRoot, vc := s.syncCandidates(paths)
	var updates []syncUpdate
	for _, c := range candidates {
		if u, ok := readSyncUpdate(c, baseRef, repoRoot, vc); ok {
			updates = append(updates, u)
		}
	}
	if len(updates) == 0 {
		return
	}
	// Hold writeMu from the update through the write: round complete reloads
	// comments from the review file and pairs them with Content, so moved
	// positions must be on disk before SignalRoundComplete (which takes
	// writeMu) can run, and a debounced write it cancels would lose them.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.applySyncUpdates(updates) {
		_ = s.syncWriteFilesLocked()
	}
}

type syncCandidate struct {
	f          *FileEntry
	oldHash    string
	oldContent string
	status     string
	oldPath    string
}

type syncUpdate struct {
	syncCandidate
	content string
	hash    string
	hunks   []vcs.DiffHunk
	hunksOK bool
}

// syncCandidates snapshots the loaded files (or only paths, when given) that
// SyncWorkingTree may update. Returns none when syncing is off.
func (s *Session) syncCandidates(paths []string) (candidates []syncCandidate, baseRef, repoRoot string, vc vcs.VCS) {
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Mode != "git" || s.Focus.Kind == FocusRange || s.roundCompletePending {
		return nil, "", "", nil
	}
	for _, f := range s.Files {
		if f.Lazy || f.Orphaned || f.AbsPath == "" || f.Status == "deleted" {
			continue
		}
		if len(want) > 0 && !want[f.Path] {
			continue
		}
		candidates = append(candidates, syncCandidate{f, f.FileHash, f.Content, f.Status, f.OldPath})
	}
	return candidates, s.BaseRef, s.RepoRoot, s.VCS
}

// readSyncUpdate reads c from disk and, when it changed, diffs it against
// base. Runs without the session lock.
func readSyncUpdate(c syncCandidate, baseRef, repoRoot string, vc vcs.VCS) (syncUpdate, bool) {
	data, err := os.ReadFile(c.f.AbsPath)
	if err != nil {
		return syncUpdate{}, false
	}
	hash := fileHash(data)
	if hash == c.oldHash {
		return syncUpdate{}, false
	}
	u := syncUpdate{syncCandidate: c, content: string(data), hash: hash}
	if c.status == "untracked" || c.status == "added" {
		u.hunks, u.hunksOK = vcs.FileDiffUnifiedNewFile(u.content), true
	} else if h, err := diffHunksForFile(c.f.Path, c.oldPath, c.status, baseRef, repoRoot, false, vc); err == nil {
		u.hunks, u.hunksOK = h, true
	}
	return u, true
}

// applySyncUpdates stores the new content and moves comments with it, and
// reports whether any comment moved. A file whose hash changed since it was
// read (round complete, lazy load) is skipped.
func (s *Session) applySyncUpdates(updates []syncUpdate) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roundCompletePending {
		return false
	}
	moved := false
	for _, u := range updates {
		if u.f.FileHash != u.oldHash {
			continue
		}
		var changed bool
		u.f.Comments, changed = remapCommentsToContent(u.f.Comments, u.oldContent, u.content)
		moved = moved || changed
		u.f.Content = u.content
		u.f.FileHash = u.hash
		if u.hunksOK {
			u.f.DiffHunks = u.hunks
		}
	}
	return moved
}

// remapCommentsToContent moves new-side line comments from oldContent to
// newContent. File-level comments, old-side comments (they point at the base
// ref) and live pins (DOM anchors) keep their positions. Returns a new slice
// and whether any comment moved.
func remapCommentsToContent(comments []Comment, oldContent, newContent string) ([]Comment, bool) {
	if len(comments) == 0 {
		return comments, false
	}
	lineMap := diff.MapOldLineToNew(diff.ComputeLineDiff(oldContent, newContent))
	newLines := diff.SplitLines(newContent)
	newLineCount := len(newLines)
	if newLineCount == 0 {
		newLineCount = 1
	}
	out := make([]Comment, len(comments))
	moved := false
	for i, c := range comments {
		out[i] = c
		if c.DOMAnchor != nil || c.Scope == "file" || c.Side == "old" || c.StartLine == 0 {
			continue
		}
		start, end, drifted := remapCommentPosition(c, lineMap, newLines, newLineCount)
		if c.Anchor == "" {
			drifted = c.Drifted
		}
		if start != c.StartLine || end != c.EndLine || drifted != c.Drifted {
			out[i].StartLine, out[i].EndLine, out[i].Drifted = start, end, drifted
			moved = true
		}
	}
	return out, moved
}
