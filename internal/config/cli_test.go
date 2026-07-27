package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConfig_Help(t *testing.T) {
	err := RunConfig([]string{"--help"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunConfig_Generate(t *testing.T) {
	out := captureStdout(t, func() {
		if err := RunConfig([]string{"--generate"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"port"`) {
		t.Errorf("generate output should contain port key, got: %s", out[:min(200, len(out))])
	}
}

func TestRunConfig_ShowResolved(t *testing.T) {
	out := captureStdout(t, func() {
		if err := RunConfig(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "{") {
		t.Errorf("expected JSON config output, got: %s", out[:min(100, len(out))])
	}
}

func TestCurrentConfigOutputPrecedence(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(projectDir)

	if err := os.WriteFile(
		filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"output":"/global"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveOutputDir("", cfg); got != "/global" {
		t.Fatalf("global output = %q, want /global", got)
	}

	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"output":"/project"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveOutputDir("", cfg); got != "/project" {
		t.Fatalf("project output = %q, want /project", got)
	}
	if got := ResolveOutputDir("/explicit", cfg); got != "/explicit" {
		t.Fatalf("explicit output = %q, want /explicit", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
