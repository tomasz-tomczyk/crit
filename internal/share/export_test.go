package share

import "github.com/tomasz-tomczyk/crit/internal/testutil"

var (
	tokenFromHostedURL   = TokenFromHostedURL
	mustMkdirAll         = testutil.MustMkdirAll
	buildSharePayload    = BuildSharePayload
	loadCommentsForShare = LoadCommentsForShare
	unpublishFromWeb     = UnpublishFromWeb
	shareScope           = ShareScope
)
