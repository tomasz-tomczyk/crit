package share

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
