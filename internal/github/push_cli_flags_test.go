package github

import (
	"errors"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
)

func TestParsePushFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    pushFlags
		wantErr bool
	}{
		{name: "empty", args: nil, want: pushFlags{}},
		{name: "dry-run", args: []string{"--dry-run"}, want: pushFlags{dryRun: true}},
		{name: "message long", args: []string{"--message", "hello"}, want: pushFlags{message: "hello"}},
		{name: "message short", args: []string{"-m", "hi"}, want: pushFlags{message: "hi"}},
		{name: "output", args: []string{"-o", "/tmp/x"}, want: pushFlags{outputDir: "/tmp/x"}},
		{name: "event", args: []string{"-e", "approve"}, want: pushFlags{eventFlag: "approve"}},
		{name: "pr number", args: []string{"99"}, want: pushFlags{prFlag: 99}},
		{
			name: "all flags",
			args: []string{"--dry-run", "--event", "request-changes", "-m", "msg", "-o", "/d", "12"},
			want: pushFlags{prFlag: 12, dryRun: true, message: "msg", outputDir: "/d", eventFlag: "request-changes"},
		},
		{name: "message missing value", args: []string{"--message"}, wantErr: true},
		{name: "output missing value", args: []string{"--output"}, wantErr: true},
		{name: "event missing value", args: []string{"--event"}, wantErr: true},
		{name: "non-numeric positional", args: []string{"bogus"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePushFlags(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePushFlags(%v) = nil error, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePushFlags(%v) unexpected error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("parsePushFlags(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParsePushFlags_NonNumericExitCode(t *testing.T) {
	_, err := parsePushFlags([]string{"bogus"})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T, want clicmd.ExitError", err)
	}
	if exitErr.Code != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.Code)
	}
}

func TestResolveCurrentPRHead_NotInRange(t *testing.T) {
	sha, err := resolveCurrentPRHead(5, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "" {
		t.Errorf("sha = %q, want empty when not in range mode", sha)
	}
}

func TestResolveCurrentPRHead_InRange(t *testing.T) {
	restore := SwapFetchPRByNumberForTest(func(n int) (*PRInfo, error) {
		return &PRInfo{Number: n, HeadRefOid: "abc123"}, nil
	})
	defer restore()

	sha, err := resolveCurrentPRHead(5, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want abc123", sha)
	}
}

func TestResolveCurrentPRHead_FetchError(t *testing.T) {
	restore := SwapFetchPRByNumberForTest(func(int) (*PRInfo, error) {
		return nil, errors.New("network down")
	})
	defer restore()

	// Live mode: a fetch failure is fatal because the stale-head check must run.
	if _, err := resolveCurrentPRHead(5, true, false); err == nil {
		t.Error("resolveCurrentPRHead live mode = nil error, want error on fetch failure")
	}

	// Dry-run mode: the failure is tolerated and surfaced via a stderr note.
	var sha string
	stderr := captureStderr(t, func() {
		var err error
		sha, err = resolveCurrentPRHead(5, true, true)
		if err != nil {
			t.Errorf("dry-run should tolerate fetch error, got %v", err)
		}
	})
	if sha != "" {
		t.Errorf("dry-run sha = %q, want empty", sha)
	}
	if !strings.Contains(stderr, "stale-head check not enforced") {
		t.Errorf("expected dry-run stderr note, got: %q", stderr)
	}
}

func TestPushShouldExitFailure(t *testing.T) {
	tests := []struct {
		name                              string
		posted, patched, deleted          int
		exportPath                        string
		postFailed, deleteFailed, wantOut bool
	}{
		{name: "all zero no failures", wantOut: false},
		{name: "post failed nothing landed", postFailed: true, wantOut: true},
		{name: "post failed but some posted", posted: 1, postFailed: true, wantOut: false},
		{name: "delete failed but export written", exportPath: "/tmp/x.md", deleteFailed: true, wantOut: false},
		{name: "patched only no failure", patched: 2, wantOut: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pushShouldExitFailure(tt.posted, tt.patched, tt.deleted, tt.exportPath, tt.postFailed, tt.deleteFailed)
			if got != tt.wantOut {
				t.Errorf("pushShouldExitFailure = %v, want %v", got, tt.wantOut)
			}
		})
	}
}

func TestRunPush_GHMissing(t *testing.T) {
	withEmptyPATH(t)
	if err := RunPush(nil); err == nil {
		t.Fatal("RunPush with gh missing = nil, want error")
	}
}
