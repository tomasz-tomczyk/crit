package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}

	// Build crit binary
	dir := t.TempDir()
	binary := filepath.Join(dir, "crit")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Create a test repo with a file
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	runGit(t, repoDir, "checkout", "-b", "main")
	writeFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "init")

	// Make a change so crit has something to review
	writeFile(t, filepath.Join(repoDir, "test.md"), "# Hello\n\nWorld\n")

	// Resolve symlinks so the session key matches (macOS: /var → /private/var)
	repoDir, _ = filepath.EvalSymlinks(repoDir)

	// Use temp HOME so session files don't pollute real HOME
	homeDir := t.TempDir()
	homeDir, _ = filepath.EvalSymlinks(homeDir)

	// Compute expected session key (cwd=repoDir, args=[])
	key := sessionKey(repoDir, nil)
	sessDir := filepath.Join(homeDir, ".crit", "sessions")
	sessionPath := filepath.Join(sessDir, key+".json")

	// Start daemon via _serve
	cmd := exec.Command(binary, "_serve", "--no-open", "--port", "0")
	cmd.Dir = repoDir
	// Filter existing HOME so our override takes effect (first match wins)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "HOME=") {
			env = append(env, e)
		}
	}
	env = append(env, "HOME="+homeDir)
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
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/health", entry.Port))
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health check: got %d, want 200", resp.StatusCode)
	}

	// Start review-cycle in background
	done := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		r, err := client.Post(
			fmt.Sprintf("http://localhost:%d/api/review-cycle", entry.Port),
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
		fmt.Sprintf("http://localhost:%d/api/finish", entry.Port),
		"application/json", nil,
	)
	if err != nil {
		t.Fatalf("finish failed: %v", err)
	}
	finishResp.Body.Close()

	// review-cycle should complete with finish response
	select {
	case result := <-done:
		var resp map[string]string
		if err := json.Unmarshal([]byte(result), &resp); err != nil {
			t.Fatalf("unmarshal result: %v (raw: %s)", err, result)
		}
		if resp["status"] != "finished" {
			t.Errorf("got status %q, want 'finished'", resp["status"])
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
