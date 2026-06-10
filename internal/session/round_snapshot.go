package session

import "time"

// RoundSnapshot captures the content of a single file at a specific review
// round. Files-mode only. R1 = baseline at session construction;
// R(N+1) = content the agent produced during round N.
//
// Snapshots are persisted in <folder>/snapshots.json — never in the review
// file. Agent tooling that reads review.json must remain insulated from the
// (potentially large) per-round bodies.
type RoundSnapshot struct {
	Content string `json:"content"`
	Status  string `json:"status,omitempty"`
	// Position is the file's index in Session.Files at capture time. It pins
	// display order at this round so the timeline can render rounds in their
	// original layout even if the session-level file list reorders later.
	// Snapshots persisted before this field landed read back as 0.
	Position   int       `json:"position"`
	CapturedAt time.Time `json:"captured_at"`
}

// CloneRoundSnapshots returns a deep copy of src so the caller can release the
// session lock before persisting to disk. RoundSnapshot values are treated as
// immutable post-capture, so the inner struct copy is value-only.
func CloneRoundSnapshots(src map[string]map[int]RoundSnapshot) map[string]map[int]RoundSnapshot {
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
