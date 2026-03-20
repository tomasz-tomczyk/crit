package main

import (
	"encoding/json"
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

	// Start daemon via _serve
	cmd := exec.Command(binary, "_serve", "--no-open", "--port", "0")
	cmd.Dir = repoDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for daemon state file
	statePath := filepath.Join(repoDir, ".crit.json")
	var state daemonState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		s, err := readDaemonState(statePath)
		if err == nil {
			state = s
			break
		}
	}
	if state.Port == 0 {
		t.Fatal("daemon did not write state file")
	}

	// Verify health endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/health", state.Port))
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
			fmt.Sprintf("http://localhost:%d/api/review-cycle", state.Port),
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
		fmt.Sprintf("http://localhost:%d/api/finish", state.Port),
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

	// Daemon state should be cleared from .crit.json
	cleanedState, err := readDaemonState(statePath)
	if err == nil && cleanedState.PID != 0 {
		t.Errorf("daemon state not cleaned up after shutdown: PID=%d Port=%d", cleanedState.PID, cleanedState.Port)
	}
}
