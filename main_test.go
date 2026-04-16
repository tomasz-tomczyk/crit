package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSubcommandDispatch_Help verifies that help flags are recognized.
func TestSubcommandDispatch_Help(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Help", "--")
			cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "GO_TEST_HELP_ARG="+arg)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("help %q exited with error: %v\noutput: %s", arg, err, out)
			}
		})
	}
}

func TestHelperProcess_Help(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	arg := os.Getenv("GO_TEST_HELP_ARG")
	os.Args = []string{"crit", arg}
	// printHelp writes to stderr and main() just returns (no os.Exit in the new code)
	// We just verify it doesn't panic
	printHelp()
}

// TestSubcommandDispatch_Version verifies the version flag.
func TestSubcommandDispatch_Version(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Version", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version exited with error: %v\noutput: %s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected version output, got empty")
	}
}

func TestHelperProcess_Version(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	printVersion()
}

// TestSubcommandDispatch_Config verifies that "crit config --generate" produces output.
func TestSubcommandDispatch_Config(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Config", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config --generate exited with error: %v\noutput: %s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected config output, got empty")
	}
}

func TestHelperProcess_Config(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runConfig([]string{"--generate"})
}

// TestRunComment_MissingArgs verifies that runComment exits with usage when given no args.
func TestRunComment_MissingArgs(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing comment args")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatal("expected non-zero exit code")
	}
}

func TestHelperProcess_CommentMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{})
}

// TestRunComment_InvalidLocation verifies that a bad location format exits with error.
func TestRunComment_InvalidLocation(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadLoc", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid location")
	}
}

func TestHelperProcess_CommentBadLoc(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	// No colon in location
	runComment([]string{"noColonHere", "some body"})
}

// TestRunComment_InvalidLineNumber verifies that a non-numeric line exits with error.
func TestRunComment_InvalidLineNumber(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadLine", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid line number")
	}
}

func TestHelperProcess_CommentBadLine(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{"file.go:abc", "some body"})
}

// TestRunInstall_MissingAgent verifies that runInstall with no args exits with usage.
func TestRunInstall_MissingAgent(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_InstallMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing install agent")
	}
}

func TestHelperProcess_InstallMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runInstall([]string{})
}

// TestRunShare_MissingFiles verifies that runShare with no files exits with usage.
func TestRunShare_MissingFiles(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ShareMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing share files")
	}
}

func TestHelperProcess_ShareMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runShare([]string{})
}

// TestRunComment_FlagParsing verifies that --output and --author flags are parsed correctly.
func TestRunComment_FlagParsing(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentFlags", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment with flags exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentFlags(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	// Write a dummy file so the comment can reference it
	os.WriteFile(tmp+"/test.go", []byte("package main\n"), 0o644)
	runComment([]string{"--output", tmp, "--author", "TestBot", "test.go:1", "test body"})
}

// TestRunComment_ClearFlag verifies that --clear works.
func TestRunComment_ClearFlag(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentClear", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment --clear exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentClear(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	// Write a .crit.json to clear
	os.WriteFile(tmp+"/.crit.json", []byte(`{"files":{}}`), 0o644)
	runComment([]string{"--output", tmp, "--clear"})
}

// TestRunComment_RangeLine verifies that a range line spec like "10-25" is parsed.
func TestRunComment_RangeLine(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentRange", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment with range exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentRange(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	runComment([]string{"--output", tmp, "--author", "Bot", "test.go:10-25", "range body"})
}

// TestRunComment_InvalidRange verifies that a bad range like "10-abc" exits with error.
func TestRunComment_InvalidRange(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadRange", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid range")
	}
}

func TestHelperProcess_CommentBadRange(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{"file.go:10-abc", "some body"})
}

// TestRunShare_OutputFlagMissingValue verifies that --output without value exits with error.
func TestRunShare_OutputFlagMissingValue(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ShareOutputMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for --output without value")
	}
}

func TestHelperProcess_ShareOutputMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runShare([]string{"--output"})
}

// TestRunUnpublish_UnknownFlag verifies that an unknown flag prints usage and exits.
func TestRunUnpublish_UnknownFlag(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_UnpublishBadFlag", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown unpublish flag")
	}
}

func TestHelperProcess_UnpublishBadFlag(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runUnpublish([]string{"--bogus"})
}

// TestRunComment_JSONFlag verifies that --json reads from stdin and produces output.
func TestRunComment_JSONFlag(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_CommentJSON$", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	cmd.Stdin = strings.NewReader(`[{"file":"main.go","line":1,"body":"test"}]`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Added 1 comment") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestHelperProcess_CommentJSON(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	runComment([]string{"--json", "--output", tmp, "--author", "TestBot"})
}

// TestRunComment_JSONFlagMixed verifies that --json handles mixed comments and replies.
func TestRunComment_JSONFlagMixed(t *testing.T) {
	// Step 1: Create a comment and capture its ID
	tmp := t.TempDir()
	err := addCommentToCritJSON("main.go", 1, 1, "comment", "TestBot", tmp)
	if err != nil {
		t.Fatalf("setup comment: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".crit.json"))
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	commentID := cj.Files["main.go"].Comments[0].ID

	// Step 2: Run --json with a new comment + reply to the existing comment
	input := fmt.Sprintf(`[{"file":"main.go","line":5,"body":"another"},{"reply_to":%q,"body":"reply","resolve":true}]`, commentID)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_CommentJSONMix$", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "TEST_OUTPUT_DIR="+tmp)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "1 comment") || !strings.Contains(string(out), "1 reply") {
		t.Errorf("expected mixed success message, got: %s", out)
	}
}

func TestHelperProcess_CommentJSONMix(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := os.Getenv("TEST_OUTPUT_DIR")
	if tmp == "" {
		tmp = t.TempDir()
	}
	runComment([]string{"--json", "--output", tmp, "--author", "TestBot"})
}

// TestParsePushEvent is in github_test.go with comprehensive cases.

// TestResolveServerConfig_BaseBranch verifies that --base-branch sets defaultBranchOverride
// and that config file base_branch is used as a fallback when the flag is absent.
func TestResolveServerConfig_BaseBranch(t *testing.T) {
	// Reset global state before and after
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	t.Run("CLI flag sets override", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		_, err := resolveServerConfig([]string{"--base-branch", "uat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if defaultBranchOverride != "uat" {
			t.Errorf("expected defaultBranchOverride=uat, got %q", defaultBranchOverride)
		}
	})

	t.Run("config file used when no flag", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		cfgPath := filepath.Join(dir, ".crit.config.json")
		os.WriteFile(cfgPath, []byte(`{"base_branch": "develop"}`), 0644)

		// resolveServerConfig reads from cwd, so chdir to our temp dir
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if defaultBranchOverride != "develop" {
			t.Errorf("expected defaultBranchOverride=develop, got %q", defaultBranchOverride)
		}
	})

	t.Run("CLI flag overrides config file", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		cfgPath := filepath.Join(dir, ".crit.config.json")
		os.WriteFile(cfgPath, []byte(`{"base_branch": "develop"}`), 0644)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := resolveServerConfig([]string{"--base-branch", "uat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if defaultBranchOverride != "uat" {
			t.Errorf("expected defaultBranchOverride=uat (CLI wins), got %q", defaultBranchOverride)
		}
	})
}

func TestResolveServerConfig_PortPrecedence(t *testing.T) {
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_PORT", "5000")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--port", "6000"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.port != 6000 {
			t.Errorf("port = %d, want 6000 (CLI flag)", sc.port)
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_PORT", "5000")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.port != 5000 {
			t.Errorf("port = %d, want 5000 (env var)", sc.port)
		}
	})

	t.Run("config wins when no CLI flag or env var", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_PORT", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.port != 4000 {
			t.Errorf("port = %d, want 4000 (config file)", sc.port)
		}
	})

	t.Run("zero port when nothing set", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_PORT", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.port != 0 {
			t.Errorf("port = %d, want 0 (default)", sc.port)
		}
	})
}

func TestResolveServerConfig_ShareURLPrecedence(t *testing.T) {
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_SHARE_URL", "https://env.example.com")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--share-url", "https://cli.example.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.shareURL != "https://cli.example.com" {
			t.Errorf("shareURL = %q, want CLI value", sc.shareURL)
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("CRIT_SHARE_URL", "https://env.example.com")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.shareURL != "https://env.example.com" {
			t.Errorf("shareURL = %q, want env value", sc.shareURL)
		}
	})

	t.Run("config used when no CLI or env", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		os.Unsetenv("CRIT_SHARE_URL")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.shareURL != "https://config.example.com" {
			t.Errorf("shareURL = %q, want config value", sc.shareURL)
		}
	})
}

func TestResolveServerConfig_BoolFlags(t *testing.T) {
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	t.Run("--no-open flag sets noOpen", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--no-open"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.noOpen {
			t.Error("noOpen should be true when --no-open is passed")
		}
	})

	t.Run("config no_open used when no flag", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"no_open": true}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.noOpen {
			t.Error("noOpen should be true from config")
		}
	})

	t.Run("--quiet flag sets quiet", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--quiet"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.quiet {
			t.Error("quiet should be true when --quiet is passed")
		}
	})

	t.Run("--no-ignore disables ignore patterns", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"ignore_patterns": ["*.lock", "vendor/"]}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--no-ignore"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sc.ignorePatterns) != 0 {
			t.Errorf("ignorePatterns = %v, want empty (--no-ignore)", sc.ignorePatterns)
		}
	})
}

func TestResolveServerConfig_FileArgs(t *testing.T) {
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	defaultBranchOverride = ""
	defaultBranchOnce = sync.Once{}

	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	sc, err := resolveServerConfig([]string{"plan.md", "notes.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sc.files) != 2 || sc.files[0] != "plan.md" || sc.files[1] != "notes.md" {
		t.Errorf("files = %v, want [plan.md notes.md]", sc.files)
	}
}

func TestResolveServerConfig_OutputDir(t *testing.T) {
	orig := defaultBranchOverride
	defer func() {
		defaultBranchOverride = orig
		defaultBranchOnce = sync.Once{}
	}()

	t.Run("CLI --output sets outputDir", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{"--output", "/tmp/out"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.outputDir != "/tmp/out" {
			t.Errorf("outputDir = %q, want /tmp/out", sc.outputDir)
		}
	})

	t.Run("config output used when no flag", func(t *testing.T) {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"output": "/tmp/cfg-out"}`), 0644)
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := resolveServerConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.outputDir != "/tmp/cfg-out" {
			t.Errorf("outputDir = %q, want /tmp/cfg-out (from config)", sc.outputDir)
		}
	})
}

func TestResolvePlanConfig_NameAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	os.WriteFile(path, []byte("# Test Plan"), 0644)

	pc := resolvePlanConfig([]string{"--name", "auth-flow", path})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != path {
		t.Errorf("filePath = %q, want %q", pc.filePath, path)
	}
}

func TestResolvePlanConfig_NameOnly(t *testing.T) {
	pc := resolvePlanConfig([]string{"--name", "auth-flow"})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != "" {
		t.Errorf("filePath should be empty, got %q", pc.filePath)
	}
	if !pc.stdinExpected {
		t.Error("expected stdinExpected=true when no file arg")
	}
}

func TestCountComments(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Resolved: false},
				{ID: "c2", Resolved: true},
			}},
			"b.go": {Comments: []Comment{
				{ID: "c3", Resolved: false},
			}},
		},
		ReviewComments: []Comment{
			{ID: "r1", Resolved: true},
		},
	}
	unresolved, resolved := countComments(cj)
	if unresolved != 2 {
		t.Errorf("unresolved = %d, want 2", unresolved)
	}
	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}
}

func TestFindStaleReviews(t *testing.T) {
	dir := t.TempDir()

	// Create a review file with an old updated_at.
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	cj := CritJSON{
		Branch:      "old-branch",
		UpdatedAt:   oldTime,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {Comments: []Comment{{ID: "c1"}}},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "stale123.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Create a recent review file.
	recentCJ := CritJSON{
		Branch:      "recent-branch",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	}
	recentData, _ := json.MarshalIndent(recentCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "recent456.json"), recentData, 0644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale review, got %d", len(stale))
	}
	if stale[0].branch != "old-branch" {
		t.Errorf("branch = %q, want %q", stale[0].branch, "old-branch")
	}
	if stale[0].comments != 1 {
		t.Errorf("comments = %d, want 1", stale[0].comments)
	}
}

func TestDeleteStaleReviews(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	stale := []staleReview{{key: "test", path: path}}
	deleted := deleteStaleReviews(stale)
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale review file should be deleted")
	}
}

func TestCleanupOnApproval_DeletesReviewFile(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	// approved=true with cleanup enabled should delete the file.
	cleanupOnApproval(true, reviewPath, true)

	if _, err := os.Stat(reviewPath); !os.IsNotExist(err) {
		t.Error("expected review file to be deleted after approval")
	}
}

func TestCleanupOnApproval_KeepsFileWhenNotApproved(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	cleanupOnApproval(false, reviewPath, true)

	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		t.Error("expected review file to still exist when not approved")
	}
}

func TestCleanupOnApproval_KeepsFileWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	os.WriteFile(reviewPath, []byte(`{"branch":"main"}`), 0644)

	// approved=true but cleanup disabled — file should stay.
	cleanupOnApproval(true, reviewPath, false)

	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		t.Error("expected review file to still exist when cleanup is disabled")
	}
}

// TestRunReviewClientRaw_WaitsForReadiness verifies that runReviewClientRaw
// polls /api/session until the daemon is ready (non-503) before hitting
// /api/review-cycle. Regression test for the plan-hook auto-approve bug where
// review-cycle was called immediately after daemon start, got 503, and
// allowed through on error.
func TestRunReviewClientRaw_WaitsForReadiness(t *testing.T) {
	var sessionCalls atomic.Int32
	var reviewCycleCalled atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			n := sessionCalls.Add(1)
			if n <= 2 {
				// First two calls return 503 (still initializing)
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"status": "loading"})
				return
			}
			// Third call: ready
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case "/api/review-cycle":
			reviewCycleCalled.Store(true)
			// Verify session was polled past the 503 phase
			if sessionCalls.Load() < 3 {
				t.Errorf("review-cycle called after only %d session polls, expected >=3", sessionCalls.Load())
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": true, "prompt": ""})

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Extract port from test server URL
	port := 0
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
	}
	if port == 0 {
		t.Fatalf("could not parse port from test server URL: %s", ts.URL)
	}

	entry := sessionEntry{Port: port}
	approved, _ := runReviewClientRaw(entry)

	if !reviewCycleCalled.Load() {
		t.Error("review-cycle was never called")
	}
	if !approved {
		t.Error("expected approved=true")
	}
	if n := sessionCalls.Load(); n < 3 {
		t.Errorf("expected at least 3 session polls (2x503 + 1x200), got %d", n)
	}
}

// TestRunReviewClientRaw_NoReadinessDelay verifies that when the daemon is
// already ready, runReviewClientRaw proceeds immediately without extra delay.
func TestRunReviewClientRaw_NoReadinessDelay(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": false, "prompt": "fix this"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	port := 0
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
	}
	if port == 0 {
		t.Fatalf("could not parse port from test server URL: %s", ts.URL)
	}

	start := time.Now()
	approved, prompt := runReviewClientRaw(sessionEntry{Port: port})
	elapsed := time.Since(start)

	if approved {
		t.Error("expected approved=false")
	}
	if prompt != "fix this" {
		t.Errorf("expected prompt='fix this', got %q", prompt)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v, expected near-instant when daemon is already ready", elapsed)
	}
}
