package github

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func configureOutputForTest(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	configuredOutput := filepath.Join(projectDir, "configured")
	data, err := json.Marshal(map[string]string{"output": configuredOutput})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return configuredOutput
}

func TestParsePullFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    pullFlags
		wantErr bool
	}{
		{name: "empty", args: nil, want: pullFlags{}},
		{name: "pr number", args: []string{"42"}, want: pullFlags{spec: "42"}},
		{name: "output long", args: []string{"--output", "/tmp/out"}, want: pullFlags{outputDir: "/tmp/out"}},
		{name: "output short", args: []string{"-o", "/tmp/out"}, want: pullFlags{outputDir: "/tmp/out"}},
		{name: "output and pr", args: []string{"-o", "/tmp/out", "7"}, want: pullFlags{spec: "7", outputDir: "/tmp/out"}},
		{name: "output missing value", args: []string{"--output"}, wantErr: true},
		{name: "pr url", args: []string{"https://github.com/acme/widget/pull/12"}, want: pullFlags{spec: "https://github.com/acme/widget/pull/12"}},
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

func TestResolvePullFlagsOutputPrecedence(t *testing.T) {
	configuredOutput := configureOutputForTest(t)
	explicitOutput := t.TempDir()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "configured output", want: configuredOutput},
		{name: "explicit output wins", in: explicitOutput, want: explicitOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := pullFlags{outputDir: tt.in}
			if err := resolvePullFlags(&f); err != nil {
				t.Fatal(err)
			}
			got := f.configuredOutput
			if f.outputDir != "" {
				got = f.outputDir
			}
			if got != tt.want {
				t.Fatalf("resolved output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResolvedPullFlags(t *testing.T) {
	configuredOutput := configureOutputForTest(t)

	t.Run("success loads configured output", func(t *testing.T) {
		f, err := parseResolvedPullFlags([]string{"42"})
		if err != nil {
			t.Fatal(err)
		}
		if f.spec != "42" || f.configuredOutput != configuredOutput {
			t.Fatalf("flags = %+v, want PR 42 and configured output %q", f, configuredOutput)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		if _, err := parseResolvedPullFlags([]string{"--output"}); err == nil {
			t.Fatal("expected missing output value error")
		}
	})
}

func TestShouldRedirectReviewForPR(t *testing.T) {
	tests := []struct {
		name         string
		explicitSpec bool
		pinnedOutput bool
		want         bool
	}{
		{name: "explicit PR without pinned output redirects", explicitSpec: true, want: true},
		{name: "auto-detected PR does not redirect", want: false},
		{name: "explicit PR with CLI or configured output stays pinned", explicitSpec: true, pinnedOutput: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRedirectReviewForPR(tt.explicitSpec, tt.pinnedOutput); got != tt.want {
				t.Fatalf("shouldRedirectReviewForPR(%v, %v) = %v, want %v", tt.explicitSpec, tt.pinnedOutput, got, tt.want)
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

func TestParsePullFlagsSession(t *testing.T) {
	got, err := parsePullFlags([]string{"--session", "aaaaaaaaaaaa", "42"})
	if err != nil {
		t.Fatal(err)
	}
	want := pullFlags{sessionID: "aaaaaaaaaaaa", spec: "42"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	_, err = parsePullFlags([]string{"--session"})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("error = %v, want ExitError code 1", err)
	}

	got, err = parsePullFlags([]string{"--session", "bbbbbbbbbbbb", "-o", "/tmp/out", "7"})
	if err != nil {
		t.Fatal(err)
	}
	if got.sessionID != "bbbbbbbbbbbb" || got.outputDir != "/tmp/out" || got.spec != "7" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseResolvedPullFlagsSession(t *testing.T) {
	configuredOutput := configureOutputForTest(t)
	f, err := parseResolvedPullFlags([]string{"--session", "cccccccccccc", "9"})
	if err != nil {
		t.Fatal(err)
	}
	if f.sessionID != "cccccccccccc" || f.spec != "9" {
		t.Fatalf("flags = %+v", f)
	}
	if f.configuredOutput != configuredOutput {
		t.Fatalf("configuredOutput = %q, want %q", f.configuredOutput, configuredOutput)
	}
}

func TestShouldRedirectReviewForPRWithSession(t *testing.T) {
	// --session pins the review identity the same way --output does.
	if shouldRedirectReviewForPR(true, true) {
		t.Fatal("pinned session should not redirect")
	}
}
