package main

import (
	"os"

	integrationassets "github.com/tomasz-tomczyk/crit/integrations"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/preview"
	"github.com/tomasz-tomczyk/crit/internal/session"
	webassets "github.com/tomasz-tomczyk/crit/web"
)

var frontendFS = webassets.FS
var integrationsFS = integrationassets.FS

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		clicmd.Exit(session.RunReview(nil))
		return
	}
	if handler, ok := commandDispatch[os.Args[1]]; ok {
		handler(os.Args[2:])
		return
	}
	args := resolveAtPrefixedArgs(os.Args[1:])
	if live.LooksLikeLiveArgs(args) {
		runLive(args)
		return
	}
	if preview.LooksLikePreviewArgs(args) {
		runPreview(args)
		return
	}
	clicmd.Exit(session.RunReview(args))
}
