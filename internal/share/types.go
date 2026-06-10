package share

import (
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

type (
	Config       = config.Config
	CritJSON     = session.CritJSON
	CritJSONFile = session.CritJSONFile
	Comment      = session.Comment
	Reply        = session.Reply
	Session      = session.Session
	FileEntry    = session.FileEntry
	SSEEvent     = session.SSEEvent
	shareComment = ShareComment
)

var ReviewPathsFor = review.ReviewPathsFor
