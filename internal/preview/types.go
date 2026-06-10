package preview

import (
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

type (
	Server       = server.Server
	Session      = server.Session
	CritJSON     = session.CritJSON
	CritJSONFile = session.CritJSONFile
	Comment      = session.Comment
	DOMAnchor    = session.DOMAnchor
	FileEntry    = session.FileEntry
	SSEEvent     = session.SSEEvent
	shareComment = share.ShareComment
	shareFile    = share.ShareFile
)

const previewMainHTMLKey = session.PreviewMainHTMLKey

var (
	remapPreviewCommentFiles = share.RemapPreviewCommentFiles
	crawlPreview             = session.CrawlPreview
	saveCritJSON             = review.SaveCritJSON
	frontendFS               = server.FrontendFS
	liveSessionKey           = daemon.LiveSessionKey
	NewServer                = server.NewServer
	shareReviewFiles         = share.ShareReviewFiles
	previewSessionKey        = PreviewSessionKey
	shareScope               = share.ShareScope
	looksLikePreviewArgs     = LooksLikePreviewArgs
)

type serverConfig struct {
	previewFile string
	reviewPath  string
}

func createPreviewSession(sc *serverConfig) (*Session, error) {
	return session.NewPreviewSession(sc.previewFile, sc.reviewPath)
}
