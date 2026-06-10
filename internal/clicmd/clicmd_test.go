package clicmd

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			if got := Plural(tt.n); got != tt.want {
				t.Errorf("Plural(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestExitError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	err := ExitError{Code: 2, Err: inner}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want boom", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should expose inner error")
	}
	if (ExitError{Code: 1}).Error() != "exit" {
		t.Error("nil Err should return 'exit'")
	}
}

func TestUsage(t *testing.T) {
	err := Usage("bad flags")
	var exitErr ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("Usage should return ExitError code 1, got %T %v", err, err)
	}
	if exitErr.Error() != "bad flags" {
		t.Errorf("got %q", exitErr.Error())
	}
}

func TestRequireFlagValue(t *testing.T) {
	got, err := RequireFlagValue([]string{"--output", "/tmp"}, 0, "--output")
	if err != nil || got != "/tmp" {
		t.Fatalf("got %q, %v", got, err)
	}
	_, err = RequireFlagValue([]string{"--output"}, 0, "--output")
	if err == nil {
		t.Fatal("expected error when value missing")
	}
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestMustGetwd(t *testing.T) {
	wd, err := MustGetwd()
	if err != nil {
		t.Fatal(err)
	}
	if wd == "" {
		t.Fatal("expected non-empty cwd")
	}
	if wd != os.Getenv("PWD") && wd != mustGetwdFallback() {
		// PWD may be unset in some shells; resolved path should still exist.
		if _, statErr := os.Stat(wd); statErr != nil {
			t.Fatalf("cwd %q not stat-able: %v", wd, statErr)
		}
	}
}

func mustGetwdFallback() string {
	wd, _ := os.Getwd()
	return wd
}
