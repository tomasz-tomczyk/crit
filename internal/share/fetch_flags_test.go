package share

import (
	"errors"
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
