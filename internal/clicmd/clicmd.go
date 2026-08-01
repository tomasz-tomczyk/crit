package clicmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ExitError signals a CLI handler failure with a specific exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return "exit"
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error { return e.Err }

// Exit prints err to stderr and exits. ExitError carries an explicit code.
func Exit(err error) {
	if err == nil {
		return
	}
	code := 1
	var exitErr ExitError
	if errors.As(err, &exitErr) && exitErr.Code != 0 {
		code = exitErr.Code
	}
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(code)
}

// Exitf formats a message and exits with code 1.
func Exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	if len(args) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprintln(os.Stderr)
	}
	os.Exit(1)
}

// Plural returns "" for n==1, otherwise "s".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// MustGetwd returns the current working directory or an ExitError.
func MustGetwd() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", ExitError{Code: 1, Err: fmt.Errorf("crit: unable to determine current working directory: %w", err)}
	}
	return wd, nil
}

// Usage returns an ExitError for invalid CLI usage.
func Usage(msg string) error {
	return ExitError{Code: 1, Err: errors.New(msg)}
}

// RequireFlagValue extracts the value following a flag at position i.
func RequireFlagValue(args []string, i int, flag string) (string, error) {
	if i+1 >= len(args) {
		return "", ExitError{Code: 1, Err: fmt.Errorf("%s requires a value", flag)}
	}
	return args[i+1], nil
}

// ReorderFlagsFirst rewrites args so all flags (and their values) precede
// positional arguments, without changing the relative order within either
// group. flag.Parse stops parsing at the first non-flag argument, so
// `crit <file> --no-open` silently treats "--no-open" as a second file
// argument instead of a flag. Passing the reordered slice through
// flag.Parse lets users place flags anywhere on the command line.
//
// boolFlags must name every flag (without leading dashes) that takes no
// value, so the argument that follows it isn't mistaken for its value.
// "-h"/"--help"/"-help" are always treated as valueless, matching the
// stdlib flag package's built-in handling. A "--" argument ends
// reordering: everything after it is treated as positional, and "--" is
// re-emitted before the positional group so flag.Parse still treats
// dash-looking positionals as non-flags (same terminator semantics).
// Combined short flags like -pq are not expanded (same as stdlib flag).
func ReorderFlagsFirst(args []string, boolFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	sawTerminator := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			sawTerminator = true
			positional = append(positional, args[i+1:]...)
			break
		}
		if a == "-" || len(a) == 0 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		if name == "h" || name == "help" || boolFlags[name] {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if sawTerminator {
		return append(append(flags, "--"), positional...)
	}
	return append(flags, positional...)
}
