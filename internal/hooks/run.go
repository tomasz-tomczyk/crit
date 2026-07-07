package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// DefaultTimeout caps a finish hook so a hung script cannot block the review
// finish indefinitely. Override per-call via Input.Timeout.
const DefaultTimeout = 60 * time.Second

// Input configures a hook run.
type Input struct {
	// Stdin bytes are piped to the hook's standard input (the JSON payload).
	Stdin []byte
	// Env are extra environment variables (merged onto os.Environ()).
	Env map[string]string
	// Dir is the working directory (defaults to the process cwd when empty).
	Dir string
	// Timeout caps execution. Zero falls back to DefaultTimeout; negative means
	// no timeout (not recommended — a hung hook would block finish).
	Timeout time.Duration
}

// Output is the captured result of running a hook.
type Output struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
	TimedOut bool
}

// Run executes the resolved hook and captures its output.
//
//   - inline hooks run via `sh -c "<Arg>"`
//   - file hooks exec the resolved path directly (its shebang/interpreter applies)
//
// Hook stdout/stderr are captured for auditability — they are NOT forwarded to
// the blocking crit client's stdout (that channel carries the agent prompt).
// A non-zero exit or timeout returns an error so the caller can log a warning,
// but finish is never blocked by a hook failure.
func Run(ctx context.Context, ec ExecutableCommand, in Input) (*Output, error) {
	if ec.Kind != "inline" && ec.Kind != "file" {
		return nil, fmt.Errorf("unknown hook kind %q", ec.Kind)
	}

	ctx, cancel := buildCtx(ctx, in)
	if cancel != nil {
		defer cancel()
	}

	var cmd *exec.Cmd
	if ec.Kind == "file" {
		cmd = exec.CommandContext(ctx, ec.Arg)
	} else {
		name, args := inlineShell(ec.Arg)
		cmd = exec.CommandContext(ctx, name, args...)
	}

	cmd.Stdin = bytes.NewReader(in.Stdin)
	if in.Dir != "" {
		cmd.Dir = in.Dir
	}
	cmd.Env = mergeEnv(in.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	out := &Output{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: time.Since(start),
	}
	if cmd.ProcessState != nil {
		out.ExitCode = cmd.ProcessState.ExitCode()
	}

	switch {
	case err == nil:
		return out, nil
	case isTimeout(ctx, err):
		out.TimedOut = true
		return out, fmt.Errorf("hook %s timed out after %s", ec.Hook, timeoutFor(in))
	default:
		return out, fmt.Errorf("hook %s exited %d: %w", ec.Hook, out.ExitCode, err)
	}
}

// inlineShell returns the interpreter + args for an inline hook.
// Uses `sh -c` on Unix; on Windows falls back to `cmd /c` for quotes-free commands.
func inlineShell(command string) (name string, args []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

func buildCtx(ctx context.Context, in Input) (context.Context, context.CancelFunc) {
	if in.Timeout < 0 {
		if ctx == nil {
			return context.Background(), nil
		}
		return ctx, nil
	}
	d := in.Timeout
	if d == 0 {
		d = DefaultTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, d)
}

func timeoutFor(in Input) time.Duration {
	if in.Timeout > 0 {
		return in.Timeout
	}
	return DefaultTimeout
}

func isTimeout(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
