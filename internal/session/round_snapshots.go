package session

import "time"

// captureRoundSnapshot records the current Content/Status of every loaded,
// non-deleted file under the given round number. Files-mode only; in git mode
// the function is a no-op (snapshots are not part of the git-mode contract).
//
// Lock contract: caller MUST hold s.mu for writing OR be the only goroutine
// that could observe s.RoundSnapshots (constructor pre-SetSession).
func (s *Session) captureRoundSnapshot(round int) {
	if s.Mode != "files" {
		return
	}
	if round < 1 {
		return
	}
	if s.RoundSnapshots == nil {
		s.RoundSnapshots = make(map[string]map[int]RoundSnapshot)
	}
	now := time.Now().UTC()
	for i, f := range s.Files {
		if f == nil || f.Lazy || f.Status == "deleted" {
			continue
		}
		byRound := s.RoundSnapshots[f.Path]
		if byRound == nil {
			byRound = make(map[int]RoundSnapshot)
			s.RoundSnapshots[f.Path] = byRound
		}
		if _, ok := byRound[round]; ok {
			// Idempotent: do not overwrite an existing capture for this round.
			continue
		}
		byRound[round] = RoundSnapshot{
			Content:    f.Content,
			Status:     f.Status,
			Position:   i,
			CapturedAt: now,
		}
	}
}

// roundSnapshotForFile returns the snapshot recorded for (path, round), or
// false if no snapshot exists. Caller must hold s.mu (RLock is sufficient).
func (s *Session) roundSnapshotForFile(path string, round int) (RoundSnapshot, bool) {
	if s.RoundSnapshots == nil {
		return RoundSnapshot{}, false
	}
	byRound := s.RoundSnapshots[path]
	if byRound == nil {
		return RoundSnapshot{}, false
	}
	rs, ok := byRound[round]
	return rs, ok
}

// commentsAtOrBeforeRound returns the comments whose ReviewRound <= round,
// with each surviving comment's Replies filtered to drop entries authored in
// later rounds. Legacy replies with no ReviewRound set (zero value) inherit
// their parent's ReviewRound — visible from the parent's round onward.
// Caller must hold s.mu (RLock is sufficient).
//
// Resolution-state semantics (Stage 1, intentional): the returned Comments
// carry the *current* Resolved / ResolvedRound values, NOT the state as of
// `round`. A comment that was open at round 1 and resolved at round 3 will
// surface here with Resolved=true, ResolvedRound=3 even when the caller asked
// for round=1. This is by design: Stage 1 exposes ResolvedRound on the wire
// so the frontend can compute round-faithful resolution state itself; Stage
// 2 will fold that decision in. Until then, callers MUST inspect both fields
// rather than trust Resolved alone.
func commentsAtOrBeforeRound(comments []Comment, round int) []Comment {
	if round <= 0 {
		return nil
	}
	// comments[:0:0] forces a fresh backing array on the first append so we
	// never alias the caller's slice header — the function returns a filtered
	// view, not an in-place mutation of the input.
	out := comments[:0:0]
	for _, c := range comments {
		if c.ReviewRound > round {
			continue
		}
		if len(c.Replies) > 0 {
			c.Replies = repliesAtOrBeforeRound(c.Replies, round, c.ReviewRound)
		}
		out = append(out, c)
	}
	return out
}

// repliesAtOrBeforeRound returns the replies visible at or before round.
// A reply with ReviewRound == 0 (legacy / pre-feature data) inherits parentRound,
// matching the parent comment's existing visibility window.
func repliesAtOrBeforeRound(replies []Reply, round, parentRound int) []Reply {
	out := make([]Reply, 0, len(replies))
	for _, r := range replies {
		effective := r.ReviewRound
		if effective == 0 {
			effective = parentRound
		}
		if effective <= round {
			out = append(out, r)
		}
	}
	return out
}

// availableRounds returns the sorted set of round numbers across all files in
// s.RoundSnapshots. Caller must hold s.mu (RLock is sufficient).
func (s *Session) availableRounds() []int {
	if len(s.RoundSnapshots) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	for _, byRound := range s.RoundSnapshots {
		for r := range byRound {
			seen[r] = struct{}{}
		}
	}
	out := make([]int, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	// Tiny in-place insertion sort; rounds rarely exceed ~10.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
