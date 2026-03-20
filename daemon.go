package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// daemonState is written to .crit.daemon.json by the daemon process.
type daemonState struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

// daemonStatePath returns the path to .crit.daemon.json for the current project.
func daemonStatePath() string {
	if IsGitRepo() {
		if root, err := RepoRoot(); err == nil {
			return filepath.Join(root, ".crit.daemon.json")
		}
	}
	dir, _ := os.Getwd()
	return filepath.Join(dir, ".crit.daemon.json")
}

func writeDaemonState(path string, s daemonState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readDaemonState(path string) (daemonState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return daemonState{}, err
	}
	var s daemonState
	if err := json.Unmarshal(data, &s); err != nil {
		return daemonState{}, err
	}
	return s, nil
}

func removeDaemonState(path string) {
	os.Remove(path)
}

// isDaemonAlive checks if the daemon process is running AND responding to HTTP.
func isDaemonAlive(s daemonState) bool {
	if s.PID <= 0 || s.Port <= 0 {
		return false
	}
	proc, err := os.FindProcess(s.PID)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Signal 0 checks existence without signaling.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	// HTTP health probe — ensures the port belongs to our daemon, not a reused PID
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", s.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// startDaemon spawns a crit _serve process in the background and waits for it to be ready.
// Returns the daemon state (PID + port) on success.
func startDaemon(args []string, port int) (daemonState, error) {
	statePath := daemonStatePath()

	// Build command: crit _serve [--port N] [args...]
	selfPath, err := os.Executable()
	if err != nil {
		return daemonState{}, fmt.Errorf("finding executable: %w", err)
	}

	cmdArgs := []string{"_serve"}
	if port > 0 {
		cmdArgs = append(cmdArgs, "--port", fmt.Sprintf("%d", port))
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(selfPath, cmdArgs...)
	cmd.Dir, _ = os.Getwd()
	cmd.Stdout = nil
	cmd.Stdin = nil

	// Capture stderr so we can report why the daemon failed to start
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Detach from parent process group so it survives parent exit
	cmd.SysProcAttr = daemonSysProcAttr()

	if err := cmd.Start(); err != nil {
		return daemonState{}, fmt.Errorf("starting daemon: %w", err)
	}

	// Monitor for early exit in background
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for daemon to write its state file (poll up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			// Daemon exited before becoming ready
			msg := strings.TrimSpace(stderrBuf.String())
			if msg != "" {
				return daemonState{}, fmt.Errorf("daemon exited: %s", msg)
			}
			return daemonState{}, fmt.Errorf("daemon exited: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
		state, err := readDaemonState(statePath)
		if err != nil {
			continue
		}
		if isDaemonAlive(state) {
			return state, nil
		}
	}

	return daemonState{}, fmt.Errorf("daemon did not start within 5 seconds")
}

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true, // new process group, survives parent exit
	}
}

// stopDaemon verifies the daemon is ours via HTTP health check, then sends SIGTERM.
func stopDaemon() error {
	statePath := daemonStatePath()
	state, err := readDaemonState(statePath)
	if err != nil {
		return fmt.Errorf("no daemon state found: %w", err)
	}

	// Verify this PID is actually our crit daemon (not a reused PID)
	if !isDaemonAlive(state) {
		// PID is dead or port belongs to something else — just clean up
		removeDaemonState(statePath)
		return nil
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		removeDaemonState(statePath)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		removeDaemonState(statePath)
		return nil // already gone
	}

	// Wait briefly for clean shutdown
	time.Sleep(500 * time.Millisecond)
	removeDaemonState(statePath)
	return nil
}
