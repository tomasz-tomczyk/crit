package review

import (
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type (
	CritJSON      = session.CritJSON
	CritJSONFile  = session.CritJSONFile
	Comment       = session.Comment
	RoundSnapshot = session.RoundSnapshot
	SessionEntry  = daemon.SessionEntry
)

var (
	ResolvedCWD = daemon.ResolvedCWD
	SessionKey  = daemon.SessionKey
	DetectVCS   = vcs.DetectVCS
)
