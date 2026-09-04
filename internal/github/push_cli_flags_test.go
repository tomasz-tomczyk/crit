package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
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
		{name: "pr number", args: []string{"99"}, want: pushFlags{spec: "99"}},
		{
			name: "all flags",
			args: []string{"--dry-run", "--event", "request-changes", "-m", "msg", "-o", "/d", "12"},
			want: pushFlags{spec: "12", dryRun: true, message: "msg", outputDir: "/d", eventFlag: "request-changes"},
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

func TestResolvePushFlagsOutputPrecedence(t *testing.T) {
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
			f := pushFlags{outputDir: tt.in}
			if err := resolvePushFlags(&f); err != nil {
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

func TestParseResolvedPushFlags(t *testing.T) {
	configuredOutput := configureOutputForTest(t)

	t.Run("success loads configured output", func(t *testing.T) {
		f, err := parseResolvedPushFlags([]string{"--dry-run", "42"})
		if err != nil {
			t.Fatal(err)
		}
		if !f.dryRun || f.spec != "42" {
			t.Fatalf("flags = %+v, want dry-run PR 42", f)
		}
		if f.configuredOutput != configuredOutput {
			t.Fatalf("configuredOutput = %q, want %q", f.configuredOutput, configuredOutput)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		if _, err := parseResolvedPushFlags([]string{"--output"}); err == nil {
			t.Fatal("expected missing output value error")
		}
	})
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
	sha, err := resolveCurrentPRHead(forge.ChangeID{Number: 5}, false, false)
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

	sha, err := resolveCurrentPRHead(forge.ChangeID{Number: 5}, true, false)
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
	if _, err := resolveCurrentPRHead(forge.ChangeID{Number: 5}, true, false); err == nil {
		t.Error("resolveCurrentPRHead live mode = nil error, want error on fetch failure")
	}

	// Dry-run mode: the failure is tolerated and surfaced via a stderr note.
	var sha string
	stderr := captureStderr(t, func() {
		var err error
		sha, err = resolveCurrentPRHead(forge.ChangeID{Number: 5}, true, true)
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

func TestParsePushFlagsSession(t *testing.T) {
	got, err := parsePushFlags([]string{"--session", "aaaaaaaaaaaa", "--dry-run", "12"})
	if err != nil {
		t.Fatal(err)
	}
	want := pushFlags{sessionID: "aaaaaaaaaaaa", dryRun: true, spec: "12"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if _, err := parsePushFlags([]string{"--session"}); err == nil {
		t.Fatal("expected missing --session value error")
	}
}

func TestLoadPushReview(t *testing.T) {
	t.Run("session and output conflict", func(t *testing.T) {
		_, _, err := loadPushReview(pushFlags{sessionID: "aaaaaaaaaaaa", outputDir: "/tmp/out"}, forge.ChangeID{Number: 1})
		if err == nil || !strings.Contains(err.Error(), "--session cannot be used with --output") {
			t.Fatalf("error = %v, want session/output conflict", err)
		}
	})

	t.Run("invalid session id", func(t *testing.T) {
		_, _, err := loadPushReview(pushFlags{sessionID: "bad"}, forge.ChangeID{Number: 1})
		if err == nil || !strings.Contains(err.Error(), "expected 12-character hex") {
			t.Fatalf("error = %v, want invalid session id", err)
		}
	})

	t.Run("missing review file", func(t *testing.T) {
		projectDir := t.TempDir()
		testutil.SetHome(t, t.TempDir())
		t.Chdir(projectDir)
		_, _, err := loadPushReview(pushFlags{}, forge.ChangeID{Number: 1})
		if err == nil || !strings.Contains(err.Error(), "no review file found") {
			t.Fatalf("error = %v, want missing review file", err)
		}
	})

	t.Run("reads existing review", func(t *testing.T) {
		projectDir := t.TempDir()
		testutil.SetHome(t, t.TempDir())
		t.Chdir(projectDir)
		cwd, err := daemon.ResolvedCWD()
		if err != nil {
			t.Fatal(err)
		}
		identity, err := daemon.ReviewFilePath(daemon.SessionKey(cwd, "", nil))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(identity, 0o755); err != nil {
			t.Fatal(err)
		}
		reviewPath := session.ReviewPathsFor(identity).Review
		payload := []byte(`{"version":4,"branch":"main","review_round":2,"files":{}}`)
		if err := os.WriteFile(reviewPath, payload, 0o644); err != nil {
			t.Fatal(err)
		}

		gotPath, cj, err := loadPushReview(pushFlags{}, forge.ChangeID{Number: 0})
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != identity {
			t.Fatalf("path = %q, want %q", gotPath, identity)
		}
		if cj.Branch != "main" || cj.ReviewRound != 2 {
			t.Fatalf("crit json = %+v", cj)
		}
	})

	t.Run("invalid review json", func(t *testing.T) {
		projectDir := t.TempDir()
		testutil.SetHome(t, t.TempDir())
		t.Chdir(projectDir)
		cwd, err := daemon.ResolvedCWD()
		if err != nil {
			t.Fatal(err)
		}
		identity, err := daemon.ReviewFilePath(daemon.SessionKey(cwd, "", nil))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(identity, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(session.ReviewPathsFor(identity).Review, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err = loadPushReview(pushFlags{}, forge.ChangeID{Number: 0})
		if err == nil || !strings.Contains(err.Error(), "invalid review file") {
			t.Fatalf("error = %v, want invalid review file", err)
		}
	})

	t.Run("live session path", func(t *testing.T) {
		projectDir := t.TempDir()
		testutil.SetHome(t, t.TempDir())
		t.Chdir(projectDir)

		identity := filepath.Join(t.TempDir(), ".crit")
		if err := os.MkdirAll(identity, 0o755); err != nil {
			t.Fatal(err)
		}
		payload := []byte(`{"version":4,"branch":"feature","review_round":3,"files":{}}`)
		if err := os.WriteFile(session.ReviewPathsFor(identity).Review, payload, 0o644); err != nil {
			t.Fatal(err)
		}

		health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok"}`)
		}))
		t.Cleanup(health.Close)
		parsed, err := url.Parse(health.URL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal(err)
		}
		const key = "ffffffffffff"
		if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
			PID: os.Getpid(), Port: port, CWD: projectDir, ReviewPath: identity,
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { daemon.RemoveSessionFile(key) })

		gotPath, cj, err := loadPushReview(pushFlags{sessionID: key}, forge.ChangeID{Number: 99})
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != identity {
			t.Fatalf("path = %q, want %q", gotPath, identity)
		}
		if cj.Branch != "feature" || cj.ReviewRound != 3 {
			t.Fatalf("crit json = %+v", cj)
		}
	})
}
