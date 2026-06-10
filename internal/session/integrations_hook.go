package session

// Hooks wired from cmd/crit at startup to avoid import cycles with integrations
// detection (which lives in the main package today).
var (
	PrintVersionFn             func()
	PrintHelpFn                func()
	InstalledAgentsFn          func(cwd, home string) map[string]bool
	CheckMissingIntegrationsFn func(cwd, home string) []string
	PrintMissingHintsFn        func(missing []string) int
)
