package focus

// ParsePRSpec resolves --pr <num|url> to a numeric PR number.
func ParsePRSpec(spec string) (int, error) {
	return parsePRSpec(spec)
}

// ParseRangeSpec parses a base..head commit range.
func ParseRangeSpec(spec string) (base, head string, err error) {
	return parseRangeSpec(spec)
}

// ParseScopeSpec normalizes a --scope flag value.
func ParseScopeSpec(s string) (DiffScope, error) {
	return parseScopeSpec(s)
}

// CommentScopeOverrideFromFlag normalizes the raw --scope string for crit comment.
func CommentScopeOverrideFromFlag(s string) (CommentFocusOverride, error) {
	return commentScopeOverrideFromFlag(s)
}

// SetProbeDaemonFocusFnForTest replaces daemon focus probing during tests.
func SetProbeDaemonFocusFnForTest(fn func() *Focus) (restore func()) {
	prev := probeDaemonFocusFn
	if fn == nil {
		probeDaemonFocusFn = func() *Focus { return nil }
	} else {
		probeDaemonFocusFn = fn
	}
	return func() { probeDaemonFocusFn = prev }
}
