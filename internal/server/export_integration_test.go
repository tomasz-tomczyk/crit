//go:build integration

package server

import "testing"

// LineStatsForRound aggregates line add/del counts for round n.
func LineStatsForRound(sess *Session, n int) (int, int) {
	return lineStatsForRound(sess, n)
}

// NewRoundsTestServer returns a server with R1/R2 snapshots for integration tests.
func NewRoundsTestServer(t *testing.T) (*Server, *Session) {
	return newRoundsTestServer(t)
}
