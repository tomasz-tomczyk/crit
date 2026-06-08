package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestClientExitsOnFinish verifies the agent-integration contract: an agent
// runs `crit`, the process blocks until the user clicks Finish in the
// browser (here we simulate via POST /api/finish), then `crit` exits with
// code 0 and its stdout contains the review summary the agent reads.
//
// This is the core workflow every agent integration relies on. It must run
// on darwin, linux, and windows — if any platform breaks the
// finish-and-exit handshake, agent integrations break.
//
// Unlike TestDaemonLifecycle this test never invokes proc.Kill on the
// spawned process: we POST /api/finish and let crit exit naturally. cmd.Wait
// is bounded by a select with a 15s timeout so a hang fails fast.
func TestClientExitsOnFinish(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping client-exit E2E in short mode")
	}

	binDir := t.TempDir()
	binaryName := "crit"
	if runtime.GOOS == "windows" {
		binaryName = "crit.exe"
	}
	binary := filepath.Join(binDir, binaryName)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build crit: %v\n%s", err, out)
	}

	// Repo with a tracked file change so git mode has something to review.
	repoDir := t.TempDir()
	gitT(t, repoDir, "init")
	gitT(t, repoDir, "config", "user.email", "test@test.com")
	gitT(t, repoDir, "config", "user.name", "Test")
	gitT(t, repoDir, "checkout", "-b", "main")
	writeFile(t, filepath.Join(repoDir, "doc.md"), "# Hello\n")
	gitT(t, repoDir, "add", ".")
	gitT(t, repoDir, "commit", "-m", "init")
	writeFile(t, filepath.Join(repoDir, "doc.md"), "# Hello\n\nWorld\n")

	// Resolve symlinks so the daemon's session-key computation matches the
	// path the test uses to look up the session file (macOS /var → /private/var).
	resolvedRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatalf("eval symlinks repoDir: %v", err)
	}
	homeDir := t.TempDir()
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		t.Fatalf("eval symlinks homeDir: %v", err)
	}

	cmd := exec.Command(binary, "--no-open", "--port", "0")
	cmd.Dir = resolvedRepo
	cmd.Env = clientFinishEnv(resolvedHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// On Windows the daemon polls `git status` every second (watch.go) and
	// runs other git/gh subprocesses during diff/PR work. Each child inherits
	// repoDir as its cwd, so each holds a directory handle. terminateProcess
	// on the daemon PID alone leaks any child still in flight, and t.TempDir's
	// RemoveAll then fails with "file in use by another process". The Job
	// Object collects the entire tree (parent client + daemon + every
	// descendant) so we can tear it all down atomically on cleanup. Unix is
	// a no-op — SIGTERM to the daemon is enough there.
	group, err := newProcessGroup()
	if err != nil {
		t.Fatalf("create process group: %v", err)
	}
	t.Cleanup(group.close)

	if err := group.startInGroup(cmd); err != nil {
		t.Fatalf("start crit: %v", err)
	}

	// `crit` spawns a daemon, registers a session file, then connects to it.
	// Pick the port out of the session file rather than parsing stdout/stderr.
	entry := waitForDaemonSession(t, resolvedHome, resolvedRepo)
	port := entry.Port

	// Whatever happens, don't leave the client (or its daemon, or any of the
	// daemon's git/gh/sl children) running. On Windows the Job Object set up
	// above lets us terminate the entire tree atomically; on Unix we fall
	// back to terminateProcess on the known daemon PID plus killing the
	// parent client. This Cleanup runs before the t.TempDir RemoveAll
	// (LIFO ordering: registered after t.TempDir).
	t.Cleanup(func() {
		group.killAll()
		if entry.PID > 0 {
			if proc, err := os.FindProcess(entry.PID); err == nil {
				_ = terminateProcess(proc)
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					if !processExists(proc) {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if processExists(proc) {
					_ = proc.Kill()
				}
			}
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	waitForSessionReady(t, port)

	// Wait for the client to exit. A goroutine + select bounds the wait so a
	// hang fails quickly instead of hitting the 10-minute test timeout.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Simulate "user clicks Finish review". Poll /api/finish every 250ms:
	// the client subscribes inside handleReviewCycle on its own schedule,
	// and the SSE event is only delivered to subscribers present at fire
	// time. Re-firing until the client exits avoids racing the subscription.
	finishURL := fmt.Sprintf("http://127.0.0.1:%d/api/finish", port)
	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

waitLoop:
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("client exited with error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
			}
			break waitLoop
		case <-tick.C:
			resp, err := http.Post(finishURL, "application/json", nil)
			if err != nil {
				continue
			}
			resp.Body.Close()
		case <-deadline:
			t.Fatalf("client did not exit within 15s after repeated /api/finish\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	}

	// Verify the agent-readable summary is on stdout. runReviewClient writes
	// the /api/review-cycle JSON body verbatim — assert on the stable fields.
	out := stdout.String()
	var summary struct {
		Status     string `json:"status"`
		ReviewFile string `json:"review_file"`
		Approved   bool   `json:"approved"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &summary); err != nil {
		t.Fatalf("stdout is not the review summary JSON: %v\nstdout:\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if summary.Status != "finished" {
		t.Errorf("summary.status = %q, want \"finished\"", summary.Status)
	}
	if !summary.Approved {
		t.Errorf("summary.approved = false, want true (no comments were left)")
	}
	if summary.ReviewFile == "" {
		t.Errorf("summary.review_file is empty; agents rely on this path")
	}
}

// clientFinishEnv builds an env that pins HOME (and the Windows equivalents)
// to homeDir so the spawned daemon writes its session file under the test
// tempdir instead of the runner's real profile.
func clientFinishEnv(homeDir string) []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+4)
	for _, kv := range src {
		if strings.HasPrefix(kv, "HOME=") {
			continue
		}
		if runtime.GOOS == "windows" && (strings.HasPrefix(kv, "USERPROFILE=") ||
			strings.HasPrefix(kv, "HOMEDRIVE=") || strings.HasPrefix(kv, "HOMEPATH=")) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "HOME="+homeDir)
	if runtime.GOOS == "windows" {
		out = append(out, "USERPROFILE="+homeDir)
	}
	return out
}

// waitForDaemonSession polls ~/.crit/sessions for the session file the
// client's daemon should have written and returns the parsed entry (Port and
// PID — the test cleanup needs both). Fails the test on timeout.
func waitForDaemonSession(t *testing.T, homeDir, cwd string) sessionEntry {
	t.Helper()
	key := sessionKey(cwd, "main", nil)
	sessionPath := filepath.Join(homeDir, ".crit", "sessions", key+".json")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(sessionPath)
		if err == nil {
			var entry sessionEntry
			if err := json.Unmarshal(data, &entry); err == nil && entry.Port > 0 && entry.PID > 0 {
				return entry
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("daemon did not write session file at %s within 15s", sessionPath)
	return sessionEntry{}
}

// waitForSessionReady blocks until /api/session returns 200 (the daemon
// gates almost every endpoint behind a 503 until session init completes).
func waitForSessionReady(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/session", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("daemon /api/session did not return 200 within 15s on port %d", port)
}
