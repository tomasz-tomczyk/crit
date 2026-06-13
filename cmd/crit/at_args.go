package main

import (
	"os"
	"strings"
)

// resolveAtPrefixedArgs normalizes file references that agent file pickers
// insert with a leading "@". Claude Code's "@" autocomplete (and similar
// pickers) produce `crit @path/to/file.md`, but the literal "@path/to/file.md"
// is not a real file, so review-mode detection and daemon keying break — the
// daemon spawns, fails session init on the bogus path, and the client can't
// connect (issue #656).
//
// For each argument beginning with "@", if the "@"-prefixed literal does not
// exist on disk but the stripped path does, the stripped path is used.
// Arguments that don't start with "@" (flags, URLs, plain paths) are returned
// unchanged, and a file genuinely named "@foo" is preserved because the
// literal-exists check runs first.
func resolveAtPrefixedArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = arg
		if len(arg) < 2 || !strings.HasPrefix(arg, "@") {
			continue
		}
		if _, err := os.Stat(arg); err == nil {
			continue // a real file literally named "@..."
		}
		stripped := arg[1:]
		if _, err := os.Stat(stripped); err == nil {
			out[i] = stripped
		}
	}
	return out
}
