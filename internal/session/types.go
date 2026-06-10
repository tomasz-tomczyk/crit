package session

import (
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// VCS type aliases for cross-package use within session.
type (
	FileChange  = vcs.FileChange
	DiffHunk    = vcs.DiffHunk
	CommitInfo  = vcs.CommitInfo
	BranchEntry = vcs.BranchEntry
)

// InheritedScope is focus metadata stamped on comments authored via CLI tools.
type InheritedScope struct {
	HeadSHA   string
	BaseSHA   string
	PRNumber  int
	DiffScope string // "layer" | "full_stack" | ""
}
