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

func TestPrintConfigHelp(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	PrintConfigHelp()
	_ = w.Close()
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"crit config",
		"output <dir>",
		"plan_approve_mode",
		"notify_on_round_ready",
		"close_on_approve_after_ms",
		"reviews/<key>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PrintConfigHelp missing %q; got:\n%s", want, got)
		}
	}
}

func TestRunConfig_HelpFlagIsNoOp(t *testing.T) {
	// Help is handled by the command registry helpFn, not RunConfig.
	if err := RunConfig([]string{"--help"}); err != nil {
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

func TestRunConfig_MigrateLegacyShareURL(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	initial := `{"share_url":"https://migrate.example","auth_token":"tok","keep":1}`
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	runErr := RunConfig([]string{"--migrate"})
	_ = w.Close()
	os.Stderr = old
	if runErr != nil {
		t.Fatal(runErr)
	}
	stderr, _ := io.ReadAll(r)
	if !strings.Contains(string(stderr), "Migrated sharing configuration") {
		t.Fatalf("stderr=%q", stderr)
	}
	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["share_url"]; ok {
		t.Fatal("share_url should be removed")
	}
	var targets []ShareTarget
	if err := json.Unmarshal(raw["share_targets"], &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].URL != "https://migrate.example" || targets[0].Auth.Token != "tok" {
		t.Fatalf("targets=%#v", targets)
	}
}

func TestCurrentConfigOutputPrecedence(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	testutil.SetHome(t, homeDir)
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
	testutil.SetHome(t, t.TempDir())
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
