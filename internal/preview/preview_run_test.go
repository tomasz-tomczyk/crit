package preview

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestConnectToPreviewDaemon_NoDaemon(t *testing.T) {
	if connectToPreviewDaemonForTest("nonexistent-key-12345", true, "", false) {
		t.Error("expected false when no daemon running")
	}
}

func TestRunPreview_NoFileExits(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunPreviewNoFile", "--")
		cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("exit error = %v, want exit code 1; output=%q", err, output)
		}
		if got := string(output); !strings.Contains(got, "Usage: crit preview <file.html>") {
			t.Fatalf("stderr = %q, want usage message", got)
		}
		return
	}
	RunPreview([]string{"--no-open"})
}

func TestHelperProcess_RunPreviewNoFile(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	RunPreview([]string{"--no-open"})
}

func TestRunPreview_MissingFileExits(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunPreviewMissingFile", "--")
		cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("exit error = %v, want exit code 1; output=%q", err, output)
		}
		if got := string(output); !strings.Contains(got, `crit preview: "/nonexistent/missing.html" is not a file`) {
			t.Fatalf("stderr = %q, want missing-file message", got)
		}
		return
	}
	RunPreview([]string{"/nonexistent/missing.html", "--no-open"})
}

func TestHelperProcess_RunPreviewMissingFile(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	RunPreview([]string{"/nonexistent/missing.html", "--no-open"})
}
