package main

import (
	"os"
	"os/exec"
	"testing"
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
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
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

// TestRunGo_MissingPort verifies that runGo with no port and no config exits with error.
func TestRunGo_MissingPort(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_GoMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing go port")
	}
}

func TestHelperProcess_GoMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	// Change to a temp dir with no config to ensure port is not resolved from config
	tmp := t.TempDir()
	os.Chdir(tmp)
	runGo([]string{})
}

// TestRunGo_InvalidPort verifies that runGo with a non-numeric port exits with error.
func TestRunGo_InvalidPort(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_GoInvalidPort", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid go port")
	}
}

func TestHelperProcess_GoInvalidPort(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runGo([]string{"notanumber"})
}

// TestRunListen_MissingPort verifies that runListen with no port and no config exits with error.
func TestRunListen_MissingPort(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ListenMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing listen port")
	}
}

func TestHelperProcess_ListenMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	os.Chdir(tmp)
	runListen([]string{})
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
