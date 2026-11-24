package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestPrintUserDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the legacy layout without XDG variables")
	}
	t.Run("one layout", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetFreshHome(t, home)
		var buf bytes.Buffer
		printUserDirs(&buf, home)
		out := buf.String()
		for _, want := range []string{
			"config: " + filepath.Join(home, ".config", "crit", "config.json"),
			"state:  " + filepath.Join(home, ".local", "state", "crit"),
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "also has crit data") {
			t.Errorf("no other state folder exists, got:\n%s", out)
		}
	})
	t.Run("legacy data left behind", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home) // seeds ~/.crit
		if err := os.MkdirAll(filepath.Join(home, ".local", "state", "crit"), 0o755); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		printUserDirs(&buf, home)
		out := buf.String()
		for _, want := range []string{
			"state:  " + filepath.Join(home, ".local", "state", "crit"),
			"also has crit data: " + filepath.Join(home, ".crit"),
			"same XDG_STATE_HOME",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})
}
