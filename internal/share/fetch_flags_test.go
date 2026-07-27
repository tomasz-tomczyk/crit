package share

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
)

func TestParseFetchOutputDir(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args", nil, ""},
		{"empty slice", []string{}, ""},
		{"--output long flag", []string{"--output", "/tmp/x"}, "/tmp/x"},
		{"-o short flag", []string{"-o", "out"}, "out"},
		{"last value wins", []string{"--output", "first", "-o", "second"}, "second"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFetchOutputDir(c.args)
			if err != nil {
				t.Fatalf("parseFetchOutputDir(%v): %v", c.args, err)
			}
			if got != c.want {
				t.Errorf("parseFetchOutputDir(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestParseFetchOutputDir_MissingValue(t *testing.T) {
	_, err := parseFetchOutputDir([]string{"--output"})
	if err == nil {
		t.Fatal("expected error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestParseFetchOutputDir_UnknownArg(t *testing.T) {
	_, err := parseFetchOutputDir([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown arg")
	}
}

func TestResolveFetchOutputDirOutputPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(projectDir)
	configuredOutput := filepath.Join(projectDir, "configured")
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"output":"`+configuredOutput+`"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "configured output", want: configuredOutput},
		{name: "explicit output wins", args: []string{"--output", "/explicit"}, want: "/explicit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveFetchReviewPath(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(tt.want, ".crit")
			if got != want {
				t.Fatalf("review path = %q, want %q", got, want)
			}
		})
	}
}
