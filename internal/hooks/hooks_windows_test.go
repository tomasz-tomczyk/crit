//go:build windows

package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/hooks"
)

// Windows counterparts of the POSIX inline-hook tests in hooks_test.go, which
// skip on Windows. They cover inlineShell's `cmd /c` branch (run.go). Commands
// are kept quote-free because cmd.exe's quote handling differs from sh. The
// redirect goes first so a digit at the end of a value ("sk123>") is never
// parsed as a handle number.

func TestRun_InlineCmdWindows_EnvAndStdout(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.txt")
	ec := hooks.ExecutableCommand{Kind: "inline", Arg: ">%HOOK_OUT% echo %CRIT_SESSION_KEY%& echo mode-%CRIT_MODE%"}
	res, err := hooks.Run(context.Background(), ec, hooks.Input{
		Env: map[string]string{
			"CRIT_SESSION_KEY": "sk123",
			"CRIT_MODE":        "diff",
			"HOOK_OUT":         marker,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, res.Stderr)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if strings.TrimSpace(string(got)) != "sk123" {
		t.Fatalf("marker = %q want sk123 (env propagation)", got)
	}
	if !strings.Contains(string(res.Stdout), "mode-diff") {
		t.Fatalf("stdout = %q want mode-diff", res.Stdout)
	}
}

func TestRun_InlineCmdWindows_NonZeroExit(t *testing.T) {
	ec := hooks.ExecutableCommand{Kind: "inline", Arg: "exit /b 3"}
	out, err := hooks.Run(context.Background(), ec, hooks.Input{Timeout: 10 * time.Second})
	if err == nil {
		t.Fatal("expected error for exit /b 3")
	}
	if out == nil || out.ExitCode != 3 {
		t.Fatalf("out = %+v, want ExitCode 3", out)
	}
}
