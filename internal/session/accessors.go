package session

import "github.com/tomasz-tomczyk/crit/internal/vcs"

// RLock, RUnlock, Lock, and Unlock expose the session mutex for internal/server
// HTTP handlers that need read-consistent snapshots of session state.
func (s *Session) RLock()   { s.mu.RLock() }
func (s *Session) RUnlock() { s.mu.RUnlock() }
func (s *Session) Lock()    { s.mu.Lock() }
func (s *Session) Unlock()  { s.mu.Unlock() }

// MarkSessionStarted marks the session as post-SetSession runtime. Must be
// called before publishing the session pointer to other goroutines.
func (s *Session) MarkSessionStarted() { s.sessionStarted.Store(1) }

// PickerContext returns VCS handle, repo root, and focus under read lock.
func (s *Session) PickerContext() (vcs.VCS, string, Focus) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VCS, s.RepoRoot, s.Focus
}

// FilterFilesAtRound returns files that have a snapshot at the given round.
func (s *Session) FilterFilesAtRound(files []SessionFileInfo, round int) []SessionFileInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SessionFileInfo, 0, len(files))
	for _, f := range files {
		byRound := s.RoundSnapshots[f.Path]
		if byRound == nil {
			continue
		}
		if _, ok := byRound[round]; !ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

// AvailableRounds returns sorted round numbers present in RoundSnapshots.
// Caller must hold RLock when concurrent mutation is possible.
func (s *Session) AvailableRounds() []int { return s.availableRounds() }

// RoundSnapshotForFile returns the snapshot for path at round.
// Caller must hold RLock when concurrent mutation is possible.
func (s *Session) RoundSnapshotForFile(path string, round int) (RoundSnapshot, bool) {
	return s.roundSnapshotForFile(path, round)
}

// CritJSONPath returns the path to the session's review.json file.
func (s *Session) CritJSONPath() string { return s.critJSONPath() }

// Notify broadcasts an SSE event to subscribers.
func (s *Session) Notify(event SSEEvent) { s.notify(event) }

// LoadCritJSON reloads review state from disk into the session.
func (s *Session) LoadCritJSON() { s.loadCritJSON() }

// ClearReviewComments removes all review-level comments from the session.
func (s *Session) ClearReviewComments() { s.reviewComments = nil }

// SetLiveRoundStart installs a callback invoked when a live/preview round advances.
func (s *Session) SetLiveRoundStart(fn func(prevRound, newRound int)) {
	s.liveRoundStart = fn
}

// SetWaitingForAgent marks the session as waiting for an agent round to finish.
func (s *Session) SetWaitingForAgent(v bool) { s.setWaitingForAgent(v) }

// ReviewComments returns review-level comments. Caller must hold RLock when
// concurrent mutation is possible.
func (s *Session) ReviewComments() []Comment { return s.reviewComments }

// ExtractAnchor reads lines startLine..endLine from content as anchor text.
func ExtractAnchor(content string, startLine, endLine int) string {
	return extractAnchor(content, startLine, endLine)
}

// RandomReviewCommentID returns a unique review-level comment ID.
func RandomReviewCommentID() string { return randomReviewCommentID() }
