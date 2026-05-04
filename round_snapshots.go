package main

import "time"

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
