package server

import (
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type (
	Session         = session.Session
	RoundSnapshot   = session.RoundSnapshot
	Config          = config.Config
	Focus           = session.Focus
	Comment         = session.Comment
	Reply           = session.Reply
	CritJSON        = session.CritJSON
	CritJSONFile    = session.CritJSONFile
	FileEntry       = session.FileEntry
	SSEEvent        = session.SSEEvent
	SessionInfo     = session.SessionInfo
	SessionFileInfo = session.SessionFileInfo
	PRInfo          = github.PRInfo
	Status          = session.Status
	ShareFile       = session.ShareFile
	DiffHunk        = session.DiffHunk
	DOMAnchor       = session.DOMAnchor
	PRListCache     = github.PRListCache
	GitVCS          = vcs.GitVCS
	CommitInfo      = vcs.CommitInfo
)

const (
	FocusRange         = session.FocusRange
	DiffScopeLayer     = session.DiffScopeLayer
	DiffScopeFullStack = session.DiffScopeFullStack
)

type PRSummary = github.PRSummary

const MaxAttachmentBytes = session.MaxAttachmentBytes

const (
	DeleteResultNotFound  = session.DeleteResultNotFound
	DeleteResultForbidden = session.DeleteResultForbidden
)

// StaleIntegration describes an out-of-date agent integration file.
type StaleIntegration struct {
	Agent    string
	Location string
	Hint     string
	Hash     string
}
