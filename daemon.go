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

// daemonState tracks a running daemon process.
type daemonState struct {
	PID  int
	Port int
}

// critJSONPathForDaemon returns the .crit.json path using the same logic as the session.
func critJSONPathForDaemon() string {
	dir, err := resolveCritDir("")
	if err != nil {
		dir, _ = os.Getwd()
	}
	return filepath.Join(dir, ".crit.json")
}

// writeDaemonState stores daemon PID/port in .crit.json alongside review data.
func writeDaemonState(path string, s daemonState) error {
	// Read existing .crit.json to preserve review data
	var cj CritJSON
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cj)
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
	}
	cj.DaemonPID = s.PID
	cj.DaemonPort = s.Port
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// readDaemonState reads daemon PID/port from .crit.json.
func readDaemonState(path string) (daemonState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return daemonState{}, err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return daemonState{}, err
	}
	if cj.DaemonPID == 0 && cj.DaemonPort == 0 {
		return daemonState{}, fmt.Errorf("no daemon state in .crit.json")
	}
	return daemonState{PID: cj.DaemonPID, Port: cj.DaemonPort}, nil
}

// removeDaemonState clears daemon PID/port from .crit.json.
func removeDaemonState(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}
	cj.DaemonPID = 0
	cj.DaemonPort = 0
	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, out, 0644)
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
	statePath := critJSONPathForDaemon()

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

	// Clear existing daemon state so the poll loop doesn't find an old daemon
	removeDaemonState(statePath)

	if err := cmd.Start(); err != nil {
		return daemonState{}, fmt.Errorf("starting daemon: %w", err)
	}
	newPID := cmd.Process.Pid

	// Monitor for early exit in background
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for OUR daemon to write its state file (poll up to 5 seconds)
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
		// Verify this is OUR daemon, not a leftover from a previous one
		if state.PID == newPID && isDaemonAlive(state) {
			return state, nil
		}
	}

	// Timed out — kill the orphan process
	cmd.Process.Kill()
	<-exited // drain the Wait goroutine
	return daemonState{}, fmt.Errorf("daemon did not start within 5 seconds")
}

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true, // new process group, survives parent exit
	}
}

// stopDaemon verifies the daemon is ours via HTTP health check, then sends SIGTERM.
func stopDaemon() error {
	statePath := critJSONPathForDaemon()
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

	// Poll for process exit, escalate to SIGKILL if needed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break // process is gone
		}
	}
	// Still alive? Force kill.
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		proc.Kill()
	}
	removeDaemonState(statePath)
	return nil
}
