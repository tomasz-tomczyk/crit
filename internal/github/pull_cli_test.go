package github

import (
	"errors"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
)

func TestParsePullFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    pullFlags
		wantErr bool
	}{
		{name: "empty", args: nil, want: pullFlags{}},
		{name: "pr number", args: []string{"42"}, want: pullFlags{prFlag: 42}},
		{name: "output long", args: []string{"--output", "/tmp/out"}, want: pullFlags{outputDir: "/tmp/out"}},
		{name: "output short", args: []string{"-o", "/tmp/out"}, want: pullFlags{outputDir: "/tmp/out"}},
		{name: "output and pr", args: []string{"-o", "/tmp/out", "7"}, want: pullFlags{prFlag: 7, outputDir: "/tmp/out"}},
		{name: "output missing value", args: []string{"--output"}, wantErr: true},
		{name: "non-numeric positional", args: []string{"notanumber"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePullFlags(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePullFlags(%v) = nil error, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePullFlags(%v) unexpected error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("parsePullFlags(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

// withEmptyPATH points PATH at an empty temp dir so exec.LookPath("gh") fails
// deterministically, exercising the RequireGH guard without a real gh binary.
func withEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestRunPull_GHMissing(t *testing.T) {
	withEmptyPATH(t)
	err := RunPull(nil)
	if err == nil {
		t.Fatal("RunPull with gh missing = nil, want error")
	}
}

func TestRunPull_FlagParseError(t *testing.T) {
	// gh present is irrelevant: the bad flag value surfaces a usage exit before
	// any network call. We still need gh to pass the RequireGH gate, so this
	// asserts the parse-error exit code only when gh happens to be installed;
	// otherwise it folds into the gh-missing error. Either way it must error.
	err := RunPull([]string{"--output"})
	if err == nil {
		t.Fatal("RunPull with dangling --output = nil, want error")
	}
	var exitErr clicmd.ExitError
	if errors.As(err, &exitErr) && exitErr.Code != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.Code)
	}
}

func TestParsePullFlags_ErrorIsExitCode1(t *testing.T) {
	_, err := parsePullFlags([]string{"--output"})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T, want clicmd.ExitError", err)
	}
	if exitErr.Code != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.Code)
	}
}
