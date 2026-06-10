package session

import "github.com/tomasz-tomczyk/crit/internal/vcs"

// Hooks wired from cmd/crit at startup to avoid import cycles with integrations
// detection (which lives in the main package today).
var (
	PrintVersionFn             func()
	PrintHelpFn                func()
	InstalledAgentsFn          func(cwd, home string) map[string]bool
	CheckMissingIntegrationsFn func(cwd, home string) []string
	PrintMissingHintsFn        func(missing []string) int
	ResolveFocusFn             func(prSpec, rangeSpec, scopeSpec string, remoteFiles bool, v vcs.VCS, repoRoot string) (*Focus, error)
	ResolveReviewPathFn        func(outputDir string) (string, error)
	LoadCritJSONFromPathFn     func(critPath string) (CritJSON, error)
)
