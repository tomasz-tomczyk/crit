package session

import (
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// VCS type aliases for cross-package use within session.
type (
	FileChange  = vcs.FileChange
	DiffHunk    = vcs.DiffHunk
	CommitInfo  = vcs.CommitInfo
	BranchEntry = vcs.BranchEntry
	RemoteRef   = forge.RemoteRef
)

// InheritedScope is focus metadata stamped on comments authored via CLI tools.
// It must carry every Focus field focusKeyFor reads, or CLI comments get a
// different focus key than the daemon and the page hides them. Build it with
// ScopeFromFocus.
type InheritedScope struct {
	HeadSHA      string
	BaseSHA      string
	Forge        string
	ChangeNumber int
	DiffScope    string // "layer" | "full_stack" | ""
	// RemoteBaseProject and RemoteHost qualify the change number
	// (pr:<project>#N, mr:<host>/<project>#N); see PRFocusKey and MRFocusKey.
	RemoteBaseProject string
	RemoteHost        string
}
