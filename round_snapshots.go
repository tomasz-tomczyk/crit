package main

import (
	"time"
)

// RoundSnapshot captures the content of a single file at a specific review
// round. Files-mode only. R1 = baseline at session construction;
// R(N+1) = content the agent produced during round N.
//
// Snapshots are persisted in <folder>/snapshots.json — never in the review
// file. Agent tooling that reads review.json must remain insulated from the
// (potentially large) per-round bodies.
type RoundSnapshot struct {
	Content    string    `json:"content"`
	Status     string    `json:"status,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
}

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
	for _, f := range s.Files {
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
			CapturedAt: now,
		}
	}
}

// cloneRoundSnapshots returns a deep copy of src so the caller can release the
// session lock before persisting to disk. RoundSnapshot values are treated as
// immutable post-capture, so the inner struct copy is value-only.
func cloneRoundSnapshots(src map[string]map[int]RoundSnapshot) map[string]map[int]RoundSnapshot {
	if len(src) == 0 {
		return map[string]map[int]RoundSnapshot{}
	}
	dst := make(map[string]map[int]RoundSnapshot, len(src))
	for path, byRound := range src {
		inner := make(map[int]RoundSnapshot, len(byRound))
		for r, rs := range byRound {
			inner[r] = rs
		}
		dst[path] = inner
	}
	return dst
}
