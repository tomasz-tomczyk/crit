package session_test

import (
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func init() {
	session.EnsureReviewFolderFn = review.EnsureReviewFolder
}
