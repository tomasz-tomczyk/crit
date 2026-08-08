package gitlab

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type commandResponse struct {
	stdout     string
	stderr     string
	exitCode   int
	wantStdin  string
	checkStdin bool
}

type commandCall struct {
	name string
	args []string
}

func stubCommands(t *testing.T, responses ...commandResponse) *[]commandCall {
	t.Helper()
	oldCommandContext := commandContext
	calls := make([]commandCall, 0, len(responses))
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callIndex := len(calls)
		calls = append(calls, commandCall{name: name, args: append([]string(nil), args...)})
		response := commandResponse{stderr: "unexpected command", exitCode: 99}
		if callIndex < len(responses) {
			response = responses[callIndex]
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestGitLabCommandHelperProcess")
		cmd.Env = append(os.Environ(),
			"CRIT_GL_HELPER=1",
			"CRIT_GL_STDOUT="+base64.StdEncoding.EncodeToString([]byte(response.stdout)),
			"CRIT_GL_STDERR="+base64.StdEncoding.EncodeToString([]byte(response.stderr)),
			"CRIT_GL_EXIT="+strconv.Itoa(response.exitCode),
			"CRIT_GL_STDIN="+base64.StdEncoding.EncodeToString([]byte(response.wantStdin)),
			"CRIT_GL_CHECK_STDIN="+strconv.FormatBool(response.checkStdin),
		)
		return cmd
	}
	t.Cleanup(func() { commandContext = oldCommandContext })

	binDir := t.TempDir()
	glabPath := filepath.Join(binDir, "glab")
	if err := os.WriteFile(glabPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &calls
}

func decodeHelperEnv(name string) string {
	decoded, _ := base64.StdEncoding.DecodeString(os.Getenv(name))
	return string(decoded)
}

func TestGitLabCommandHelperProcess(t *testing.T) {
	if os.Getenv("CRIT_GL_HELPER") != "1" {
		return
	}
	input, _ := io.ReadAll(os.Stdin)
	if os.Getenv("CRIT_GL_CHECK_STDIN") == "true" && string(input) != decodeHelperEnv("CRIT_GL_STDIN") {
		_, _ = fmt.Fprintf(os.Stderr, "stdin = %q, want %q", input, decodeHelperEnv("CRIT_GL_STDIN"))
		os.Exit(98)
	}
	_, _ = io.WriteString(os.Stdout, decodeHelperEnv("CRIT_GL_STDOUT"))
	_, _ = io.WriteString(os.Stderr, decodeHelperEnv("CRIT_GL_STDERR"))
	exitCode, _ := strconv.Atoi(os.Getenv("CRIT_GL_EXIT"))
	os.Exit(exitCode)
}

func assertCommand(t *testing.T, call commandCall, name string, args ...string) {
	t.Helper()
	if call.name != name || strings.Join(call.args, "\x00") != strings.Join(args, "\x00") {
		t.Fatalf("command = %s %q, want %s %q", call.name, call.args, name, args)
	}
}
