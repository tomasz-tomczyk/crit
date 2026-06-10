package focus

import "github.com/tomasz-tomczyk/crit/internal/vcs"

// PRResolveInfo carries PR metadata needed to build a Focus without importing github.
type PRResolveInfo struct {
	URL               string
	Number            int
	Title             string
	BaseRefOid        string
	HeadRefOid        string
	BaseRefName       string
	HeadRefName       string
	HeadRepoURL       string
	IsCrossRepository bool
}

var (
	FetchPRByNumberHook func(prNum int) (PRResolveInfo, error)
	IsStackedPRHook     func(info PRResolveInfo, v vcs.VCS) bool
)

// SetPRResolveHooks wires PR resolution from cmd/crit to break focus↔github cycles.
func SetPRResolveHooks(
	fetch func(prNum int) (PRResolveInfo, error),
	stacked func(info PRResolveInfo, v vcs.VCS) bool,
) {
	FetchPRByNumberHook = fetch
	IsStackedPRHook = stacked
}
