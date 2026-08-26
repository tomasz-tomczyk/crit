package share

import (
	"encoding/json"
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
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestParseFetchOutputDir(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantOut  string
		wantSess string
	}{
		{"no args", nil, "", ""},
		{"empty slice", []string{}, "", ""},
		{"--output long flag", []string{"--output", "/tmp/x"}, "/tmp/x", ""},
		{"-o short flag", []string{"-o", "out"}, "out", ""},
		{"last value wins", []string{"--output", "first", "-o", "second"}, "second", ""},
		{"--session", []string{"--session", "aaaaaaaaaaaa"}, "", "aaaaaaaaaaaa"},
		{"session and output", []string{"--session", "bbbbbbbbbbbb", "--output", "out"}, "out", "bbbbbbbbbbbb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOut, gotSess, err := parseFetchOutputDir(c.args)
			if err != nil {
				t.Fatalf("parseFetchOutputDir(%v): %v", c.args, err)
			}
			if gotOut != c.wantOut {
				t.Errorf("output = %q, want %q", gotOut, c.wantOut)
			}
			if gotSess != c.wantSess {
				t.Errorf("session = %q, want %q", gotSess, c.wantSess)
			}
		})
	}
}

func TestParseFetchOutputDir_MissingValue(t *testing.T) {
	_, _, err := parseFetchOutputDir([]string{"--output"})
	if err == nil {
		t.Fatal("expected error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestParseFetchOutputDir_UnknownArg(t *testing.T) {
	_, _, err := parseFetchOutputDir([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown arg")
	}
}

func TestResolveFetchOutputDirOutputPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	configuredOutput := filepath.Join(projectDir, "configured")
	configData, err := json.Marshal(map[string]string{"output": configuredOutput})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		configData,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	explicitOutput := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "configured output", want: configuredOutput},
		{name: "explicit output wins", args: []string{"--output", explicitOutput}, want: explicitOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveFetchReviewPath(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			cwd, err := daemon.ResolvedCWD()
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(tt.want, "reviews", daemon.SessionKey(cwd, "", nil))
			if got != want {
				t.Fatalf("review path = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveFetchReviewPathParseError(t *testing.T) {
	if _, err := resolveFetchReviewPath([]string{"--output"}); err == nil {
		t.Fatal("expected missing output value error")
	}
}

func TestParseFetchOutputDir_MissingSessionValue(t *testing.T) {
	_, _, err := parseFetchOutputDir([]string{"--session"})
	if err == nil {
		t.Fatal("expected error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestShareFlagDest(t *testing.T) {
	var sf shareFlags
	cases := []struct {
		arg  string
		ok   bool
		set  func(*shareFlags) *string
		want string
	}{
		{"--output", true, func(s *shareFlags) *string { return &s.outputDir }, "out"},
		{"-o", true, func(s *shareFlags) *string { return &s.outputDir }, "short"},
		{"--session", true, func(s *shareFlags) *string { return &s.sessionID }, "aaaaaaaaaaaa"},
		{"--share-url", true, func(s *shareFlags) *string { return &s.svcURL }, "https://x"},
		{"--preview", true, func(s *shareFlags) *string { return &s.preview }, "p.html"},
		{"--org", true, func(s *shareFlags) *string { return &s.org }, "acme"},
		{"--visibility", true, func(s *shareFlags) *string { return &s.visibility }, "public"},
		{"--bogus", false, nil, ""},
		{"file.md", false, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			dest, ok := shareFlagDest(&sf, c.arg)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !c.ok {
				if dest != nil {
					t.Fatalf("dest = %v, want nil", dest)
				}
				return
			}
			*dest = c.want
			if *c.set(&sf) != c.want {
				t.Fatalf("flag field = %q, want %q", *c.set(&sf), c.want)
			}
		})
	}
}

func TestParseShareFlagsSession(t *testing.T) {
	sf, err := parseShareFlags([]string{"--session", "aaaaaaaaaaaa", "plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if sf.sessionID != "aaaaaaaaaaaa" {
		t.Fatalf("sessionID = %q", sf.sessionID)
	}
	if len(sf.files) != 1 || sf.files[0] != "plan.md" {
		t.Fatalf("files = %v", sf.files)
	}
}

func TestParseUnpublishFlagsSession(t *testing.T) {
	got, err := parseUnpublishFlags([]string{"--session", "aaaaaaaaaaaa", "plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got.sessionID != "aaaaaaaaaaaa" {
		t.Fatalf("sessionID = %q", got.sessionID)
	}
	if len(got.files) != 1 || got.files[0] != "plan.md" {
		t.Fatalf("files = %v", got.files)
	}

	_, err = parseUnpublishFlags([]string{"--session"})
	if err == nil {
		t.Fatal("expected missing --session value error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T, want ExitError", err)
	}
}

func TestResolveFetchReviewPathSession(t *testing.T) {
	projectDir := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	reviewPath := filepath.Join(t.TempDir(), ".crit")
	if err := os.MkdirAll(reviewPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reviewPath, "review.json"), []byte(`{"version":4,"files":{}}`), 0o644); err != nil {
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
	const key = "eeeeeeeeeeee"
	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
		PID: os.Getpid(), Port: port, CWD: projectDir, ReviewPath: reviewPath,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.RemoveSessionFile(key) })

	got, err := resolveFetchReviewPath([]string{"--session", key})
	if err != nil {
		t.Fatal(err)
	}
	if got != reviewPath {
		t.Fatalf("path = %q, want %q", got, reviewPath)
	}

	_, err = resolveFetchReviewPath([]string{"--session", key, "--output", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "--session cannot be used with --output") {
		t.Fatalf("error = %v, want session/output conflict", err)
	}
}

func TestParseShareFlagsMissingSessionValue(t *testing.T) {
	_, err := parseShareFlags([]string{"--session"})
	if err == nil {
		t.Fatal("expected error")
	}
}
