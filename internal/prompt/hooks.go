package prompt

import "strings"

const (
	HookFinishUnresolved = "on_finish_unresolved"
	HookFinishApproved   = "on_finish_approved"
)

// PromptMode maps a session to the user-facing hook mode suffix.
func PromptMode(reviewType, sessionMode string) string {
	switch reviewType {
	case "live":
		return "live"
	case "preview":
		return "preview"
	}
	if sessionMode == "git" {
		return "diff"
	}
	// files, plan, and legacy empty review_type → files
	return "files"
}

// HookForFinish picks the hook id for the current finish state.
func HookForFinish(approved bool) string {
	if approved {
		return HookFinishApproved
	}
	return HookFinishUnresolved
}

// ResolveHookKey returns the config key and hook id for resolution.
// e.g. ("on_finish_unresolved:diff", "on_finish_unresolved")
func ResolveHookKey(hook, mode string) (specific, generic string) {
	if mode == "" {
		return hook, hook
	}
	return hook + ":" + mode, hook
}

// LookupPrompt returns the template value for hook+mode from the prompts map.
// Mode-specific key wins; falls back to generic hook key.
func LookupPrompt(prompts map[string]string, hook, mode string) (value, resolvedKey string) {
	if prompts == nil {
		return "", ""
	}
	specific, generic := ResolveHookKey(hook, mode)
	if v, ok := prompts[specific]; ok && strings.TrimSpace(v) != "" {
		return v, specific
	}
	if v, ok := prompts[generic]; ok && strings.TrimSpace(v) != "" {
		return v, generic
	}
	return "", ""
}
