package main

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
	"time"
)

func TestDaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}
	if runtime.GOOS == "windows" {
		// TODO(windows): the spawned daemon now starts and writes its
		// session file, but cmd.Wait hangs indefinitely after proc.Kill on
		// Windows. The detached child + readiness-pipe (ExtraFiles FD 3)
		// combination produces subprocess-lifecycle behavior that differs
		// from POSIX in ways we haven't unwound. The runtime daemon code
		// path is still exercised by daemon_test.go unit tests on Windows;
		// revisit this E2E once the spawn handshake is reworked (e.g.
		// port-file polling instead of FD inheritance).
		t.Skip("daemon E2E spawn/wait/kill semantics differ on Windows; covered by daemon_test.go unit tests; TODO: revisit")
	}

	// Build crit binary
	dir := t.TempDir()
	binaryName := "crit"
	if runtime.GOOS == "windows" {
		binaryName = "crit.exe"
	}
	binary := filepath.Join(dir, binaryName)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Create a test repo with a file
	repoDir := t.TempDir()
	gitT(t, repoDir, "init")
	gitT(t, repoDir, "config", "user.email", "test@test.com")
	gitT(t, repoDir, "config", "user.name", "Test")
	gitT(t, repoDir, "checkout", "-b", "main")
	writeFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n")
	gitT(t, repoDir, "add", ".")
	gitT(t, repoDir, "commit", "-m", "init")

	// Make a change so crit has something to review
	writeFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n\nWorld\n")

	// Resolve symlinks so the session key matches (macOS: /var → /private/var)
	repoDir, _ = filepath.EvalSymlinks(repoDir)

	// Use temp HOME so session files don't pollute real HOME
	homeDir := t.TempDir()
	homeDir, _ = filepath.EvalSymlinks(homeDir)

	// Compute expected session key (cwd=repoDir, branch=main, args=[])
	key := sessionKey(repoDir, "main", nil)
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
	env = append(env, "HOME="+homeDir)
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
	var entry sessionEntry
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

	// Simulate user finishing review
	time.Sleep(200 * time.Millisecond)
	finishResp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/api/finish", entry.Port),
		"application/json", nil,
	)
	if err != nil {
		t.Fatalf("finish failed: %v", err)
	}
	finishResp.Body.Close()

	// review-cycle should complete with finish response
	select {
	case result := <-done:
		var resp map[string]any
		if err := json.Unmarshal([]byte(result), &resp); err != nil {
			t.Fatalf("unmarshal result: %v (raw: %s)", err, result)
		}
		if resp["status"] != "finished" {
			t.Errorf("got status %q, want 'finished'", resp["status"])
		}
		if resp["approved"] != true {
			t.Errorf("expected approved=true (no comments), got %v", resp["approved"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review-cycle did not complete")
	}

	// Kill daemon
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()

	// Session file should be cleaned up
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file not cleaned up after daemon shutdown")
	}
}
