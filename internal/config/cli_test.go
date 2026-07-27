package config

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
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

	globalOutput := filepath.Join(t.TempDir(), "global")
	globalData, err := json.Marshal(map[string]string{"output": globalOutput})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(homeDir, ".crit.config.json"),
		globalData,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveOutputDir("", cfg); got != globalOutput {
		t.Fatalf("global output = %q, want %q", got, globalOutput)
	}

	projectOutput := filepath.Join(t.TempDir(), "project")
	projectData, err := json.Marshal(map[string]string{"output": projectOutput})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		projectData,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveOutputDir("", cfg); got != projectOutput {
		t.Fatalf("project output = %q, want %q", got, projectOutput)
	}
	explicitOutput := t.TempDir()
	if got := ResolveOutputDir(explicitOutput, cfg); got != explicitOutput {
		t.Fatalf("explicit output = %q, want %q", got, explicitOutput)
	}
}

func TestCurrentConfigRelativeOutputAnchoredToRepoRoot(t *testing.T) {
	repoDir := testutil.InitTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(
		filepath.Join(repoDir, ".crit.config.json"),
		[]byte(`{"output":"reviews"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repoDir)
	rootCfg, err := LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}

	nestedDir := filepath.Join(repoDir, "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nestedDir)
	nestedCfg, err := LoadCurrentConfig()
	if err != nil {
		t.Fatal(err)
	}

	canonicalRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalRepo, "reviews")
	if rootCfg.Output != want {
		t.Fatalf("root output = %q, want %q", rootCfg.Output, want)
	}
	if nestedCfg.Output != want {
		t.Fatalf("nested output = %q, want %q", nestedCfg.Output, want)
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
