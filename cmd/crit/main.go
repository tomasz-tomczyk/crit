package main

import (
	"os"

	integrationassets "github.com/tomasz-tomczyk/crit/integrations"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
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
	if handled, err := dispatchCLI(os.Args[1:]); handled {
		clicmd.Exit(err)
		return
	}
	args := resolveAtPrefixedArgs(os.Args[1:])
	runPositionalCLI(args)
}
