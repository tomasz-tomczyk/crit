package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"time"
)

func TestDaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}
	if runtime.GOOS == "windows" {
		// TODO(windows): re-evaluate now that terminateProcess uses
		// GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT) before falling back to
		// TerminateProcess — the daemon's signal handler should now run on
		// shutdown and let cmd.Wait return cleanly. The detached child +
		// readiness-pipe (ExtraFiles FD 3) combo still produces
		// subprocess-lifecycle behavior that differs from POSIX, so this
		// suite remains skipped pending hands-on validation on a Windows
		// runner. The runtime daemon code path is still exercised by
		// daemon_test.go unit tests on Windows.
		t.Skip("daemon E2E spawn/wait/kill semantics differ on Windows; covered by daemon_test.go unit tests; TODO: revisit after CTRL_BREAK shutdown lands")
	}

	// Build crit binary
	dir := t.TempDir()
	binaryName := "crit"
	if runtime.GOOS == "windows" {
		binaryName = "crit.exe"
	}
	binary := filepath.Join(dir, binaryName)
	_, testFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	build := exec.Command("go", "build", "-o", binary, "./cmd/crit")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatalf("chmod binary: %v", err)
	}

	// Create a test repo with a file
	repoDir := t.TempDir()
	testutil.Git(t, repoDir, "init")
	testutil.Git(t, repoDir, "config", "user.email", "test@test.com")
	testutil.Git(t, repoDir, "config", "user.name", "Test")
	testutil.Git(t, repoDir, "checkout", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n")
	testutil.Git(t, repoDir, "add", ".")
	testutil.Git(t, repoDir, "commit", "-m", "init")

	// Make a change so crit has something to review
	testutil.WriteFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n\nWorld\n")

	// Resolve symlinks so the session key matches (macOS: /var → /private/var)
	repoDir, _ = filepath.EvalSymlinks(repoDir)

	// Use temp HOME so session files don't pollute real HOME
	homeDir := t.TempDir()
	homeDir, _ = filepath.EvalSymlinks(homeDir)
	// A git process can outlive the daemon by a moment and still be writing
	// into these dirs, which makes t.TempDir's RemoveAll fail with "directory
	// not empty". Cleanups run in reverse order, so this one runs first and
	// retries until the dirs go.
	t.Cleanup(func() { removeAllEventually(t, dir, repoDir, homeDir) })

	// Compute expected session key (cwd=repoDir, branch=main, args=[])
	key := SessionKey(repoDir, "main", nil)
	sessDir := filepath.Join(homeDir, ".crit", "sessions")
	sessionPath := filepath.Join(sessDir, key+".json")

	// Start daemon via _serve
	cmd := exec.Command(binary, "_serve", "--no-open", "--port", "0")
	cmd.Dir = repoDir
	// Filter existing HOME (and Windows equivalents) so our override takes
	// effect. On Windows os.UserHomeDir reads USERPROFILE / HOMEDRIVE+HOMEPATH;
	// without filtering them, the spawned daemon would still resolve the
	// runner's real profile and write its session file there.
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		if runtime.GOOS == "windows" && (strings.HasPrefix(e, "USERPROFILE=") ||
			strings.HasPrefix(e, "HOMEDRIVE=") || strings.HasPrefix(e, "HOMEPATH=")) {
			continue
		}
		env = append(env, e)
	}
	// The startup integration check runs `<agent> --version` for every AI CLI
	// on the developer's PATH (seconds), delaying readiness past the waits
	// below; the update check calls the network.
	env = append(env, "HOME="+homeDir, "CRIT_NO_INTEGRATION_CHECK=1", "CRIT_NO_UPDATE_CHECK=1")
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+homeDir)
	}
	cmd.Env = env
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for session file
	var entry SessionEntry
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		data, err := os.ReadFile(sessionPath)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.Port > 0 {
			break
		}
	}
	if entry.Port == 0 {
		t.Fatalf("daemon did not write session file at %s\nstderr: %s", sessionPath, stderrBuf.String())
	}

	// Verify session file contents
	if entry.PID != cmd.Process.Pid {
		t.Errorf("session PID %d doesn't match process PID %d", entry.PID, cmd.Process.Pid)
	}
	if entry.CWD != repoDir {
		t.Errorf("session CWD %q doesn't match repo dir %q", entry.CWD, repoDir)
	}

	// Verify health endpoint
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", entry.Port))
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health check: got %d, want 200", resp.StatusCode)
	}

	// Wait for the session to be fully initialized (server returns 503 while loading)
	readyDeadline := time.Now().Add(10 * time.Second)
	sessionReady := false
	for time.Now().Before(readyDeadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/session", entry.Port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				sessionReady = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sessionReady {
		t.Fatalf("session did not become ready within 10s")
	}

	// Start review-cycle in background
	done := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		r, err := client.Post(
			fmt.Sprintf("http://127.0.0.1:%d/api/review-cycle", entry.Port),
			"application/json", nil,
		)
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
			return
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		done <- string(body)
	}()

	// Simulate the user finishing the review. The finish event only reaches
	// review-cycle requests already subscribed, so re-post until the
	// review-cycle call returns instead of guessing how long subscribing takes.
	finishURL := fmt.Sprintf("http://127.0.0.1:%d/api/finish", entry.Port)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(10 * time.Second)
	var result string
waitFinish:
	for {
		select {
		case result = <-done:
			break waitFinish
		case <-tick.C:
			if finishResp, err := http.Post(finishURL, "application/json", nil); err == nil {
				finishResp.Body.Close()
			}
		case <-timeout:
			t.Fatalf("review-cycle did not complete\nstderr: %s", stderrBuf.String())
		}
	}
	var cycle map[string]any
	if err := json.Unmarshal([]byte(result), &cycle); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, result)
	}
	if cycle["status"] != "finished" {
		t.Errorf("got status %q, want 'finished'", cycle["status"])
	}
	if cycle["approved"] != true {
		t.Errorf("expected approved=true (no comments), got %v", cycle["approved"])
	}

	// Kill daemon
	cmd.Process.Signal(os.Interrupt)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("daemon did not exit within 10s of SIGINT\nstderr: %s", stderrBuf.String())
	}

	// Session file should be cleaned up
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file not cleaned up after daemon shutdown")
	}
}

// removeAllEventually removes dirs, retrying for a few seconds while a
// process that outlived the test finishes writing into them.
func removeAllEventually(t *testing.T, dirs ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, dir := range dirs {
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				var left []string
				_ = filepath.WalkDir(dir, func(path string, _ os.DirEntry, _ error) error {
					left = append(left, path)
					return nil
				})
				t.Errorf("remove %s: %v\nstill present: %v", dir, err, left)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
