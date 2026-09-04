package session

// InvalidatePRCache drops cached PR metadata when the user switches away from a
// PR focus. project is the URL-derived "owner/repo" (empty for bare --pr).
// Wired from cmd/crit to github.InvalidatePR at startup to avoid an import
// cycle (github imports session).
var InvalidatePRCache func(number int, project string)
