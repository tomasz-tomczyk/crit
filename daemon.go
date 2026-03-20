package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
