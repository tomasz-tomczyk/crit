package clicmd

import (
	"errors"
	"fmt"
	"os"
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
