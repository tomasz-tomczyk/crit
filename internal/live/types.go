package live

import (
	"github.com/tomasz-tomczyk/crit/internal/comment"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

var writeFile = testutil.WriteFile

type (
	Config       = config.Config
	CritJSON     = session.CritJSON
	CritJSONFile = session.CritJSONFile
	Session      = session.Session
	FileEntry    = session.FileEntry
	Comment      = session.Comment
	DOMAnchor    = session.DOMAnchor
	SSEEvent     = session.SSEEvent
)

var (
	looksLikeLiveArgs      = LooksLikeLiveArgs
	saveCritJSON           = review.SaveCritJSON
	loadCritJSON           = review.LoadCritJSON
	appendReply            = comment.AppendReply
	checkShareAllowed      = share.CheckShareAllowed
	checkGitHubSyncAllowed = share.CheckGitHubSyncAllowed
	checkCommentCLIAllowed = comment.CheckCommentCLIAllowed
	carryForwardComment    = session.CarryForwardComment
	NewServer              = server.NewServer
	frontendFS             = server.FrontendFS
)

type GhComment = github.GhComment

var mergeGHComments = github.MergeGHComments
