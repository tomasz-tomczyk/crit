package session

// InvalidatePRCache drops cached PR metadata when the user switches away from a
// PR focus. Wired from cmd/crit to github.InvalidatePRCache at startup to avoid
// an import cycle (github imports session).
var InvalidatePRCache func(int)
