package focus

import "github.com/tomasz-tomczyk/crit/internal/vcs"

// ChangeResolveInfo carries forge-neutral change metadata needed to build a
// Focus without importing either provider adapter.
type ChangeResolveInfo struct {
	URL               string
	Number            int
	Title             string
	BaseRefOid        string
	HeadRefOid        string
	BaseRefName       string
	HeadRefName       string
	HeadRepoURL       string
	BaseRepoProject   string
	HeadRepoProject   string
	HeadRepoHost      string
	IsCrossRepository bool
}

var (
	// FetchPRHook resolves a --pr <num|url> spec. The raw CLI value is passed
	// through so URL-derived owner/repo survives (mirrors FetchMRHook).
	FetchPRHook     func(spec string) (ChangeResolveInfo, error)
	IsStackedPRHook func(info ChangeResolveInfo, v vcs.VCS) bool
	FetchMRHook     func(spec string) (ChangeResolveInfo, error)
	IsStackedMRHook func(info ChangeResolveInfo, v vcs.VCS) bool
)

// SetPRResolveHooks wires PR resolution from cmd/crit to break focus↔github cycles.
func SetPRResolveHooks(
	fetch func(spec string) (ChangeResolveInfo, error),
	stacked func(info ChangeResolveInfo, v vcs.VCS) bool,
) {
	FetchPRHook = fetch
	IsStackedPRHook = stacked
}

// SetMRResolveHooks wires GitLab MR resolution through the same neutral
// metadata shape used by PR focus.
func SetMRResolveHooks(
	fetch func(spec string) (ChangeResolveInfo, error),
	stacked func(info ChangeResolveInfo, v vcs.VCS) bool,
) {
	FetchMRHook = fetch
	IsStackedMRHook = stacked
}
