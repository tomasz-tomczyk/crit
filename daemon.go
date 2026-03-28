package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// sessionEntry tracks a running daemon process in ~/.crit/sessions/.
type sessionEntry struct {
	PID       int      `json:"pid"`
	Port      int      `json:"port"`
	CWD       string   `json:"cwd"`
	Args      []string `json:"args,omitempty"`
	StartedAt string   `json:"started_at"`
}

// resolvedCWD returns the current working directory with symlinks resolved.
// This prevents macOS /var → /private/var mismatches in session keys.
func resolvedCWD() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return cwd, nil // fall back to unresolved
	}
	return resolved, nil
}

// sessionKey returns a deterministic hash for cwd + args, used as the session filename.
// Format: sha256(cwd + "\0" + arg1 + "\0" + arg2 + ...)[:12]
func sessionKey(cwd string, args []string) string {
	sorted := make([]string, len(args))
	copy(sorted, args)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(cwd))
	for _, a := range sorted {
		h.Write([]byte{0})
		h.Write([]byte(a))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// sessionsDir returns the path to ~/.crit/sessions/.
func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "sessions"), nil
}

// sessionFilePath returns the full path for a session file.
func sessionFilePath(key string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".json"), nil
}

// writeSessionFile writes a session entry to ~/.crit/sessions/<key>.json.
func writeSessionFile(key string, entry sessionEntry) error {
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating sessions directory: %w", err)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0644)
}

// readSessionFile reads a session entry from ~/.crit/sessions/<key>.json.
func readSessionFile(key string) (sessionEntry, error) {
	path, err := sessionFilePath(key)
	if err != nil {
		return sessionEntry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionEntry{}, err
	}
	var entry sessionEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return sessionEntry{}, err
	}
	return entry, nil
}

// removeSessionFile deletes a session file and its associated log and lock files.
func removeSessionFile(key string) {
	path, err := sessionFilePath(key)
	if err != nil {
		return
	}
	os.Remove(path)
	// Clean up associated log and lock files
	dir := filepath.Dir(path)
	os.Remove(filepath.Join(dir, key+".log"))
	os.Remove(filepath.Join(dir, key+".lock"))
}

// findAliveSession looks up a session by key and returns it if alive.
// Cleans up stale session files for dead processes.
func findAliveSession(key string) (sessionEntry, bool) {
	entry, err := readSessionFile(key)
	if err != nil {
		return sessionEntry{}, false
	}
	if !isDaemonAlive(entry) {
		removeSessionFile(key)
		return sessionEntry{}, false
	}
	return entry, true
}

// listSessionsForCWD returns all alive sessions whose CWD matches.
// Cleans up stale session files as a side effect.
func listSessionsForCWD(cwd string) ([]sessionEntry, []string) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var alive []sessionEntry
	var keys []string
	for _, de := range dirEntries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.CWD != cwd {
			continue
		}
		if isDaemonAlive(entry) {
			alive = append(alive, entry)
			keys = append(keys, key)
		} else {
			os.Remove(filepath.Join(dir, de.Name()))
			os.Remove(filepath.Join(dir, key+".log"))
			os.Remove(filepath.Join(dir, key+".lock"))
		}
	}
	return alive, keys
}

// isDaemonAlive checks if the daemon process is running AND responding to HTTP.
// After PID recycling, a different process could listen on the same port,
// so we validate that the response body contains {"status":"ok"}.
func isDaemonAlive(s sessionEntry) bool {
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
	// HTTP health probe — ensures the port belongs to our daemon, not a reused PID.
	// We validate the response body to guard against a non-crit process on the same port.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", s.Port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}
	return health.Status == "ok"
}

// daemonHasBrowser checks if the daemon has any connected browser clients.
// Uses a pointer to distinguish "field missing" (older daemon) from "false".
// When the field is missing, assumes a browser is connected (safe default).
func daemonHasBrowser(s sessionEntry) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", s.Port))
	if err != nil {
		return true // can't reach daemon, assume browser exists
	}
	defer resp.Body.Close()
	var result struct {
		BrowserClients *bool `json:"browser_clients"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return true
	}
	if result.BrowserClients == nil {
		return true // older daemon without this field — assume browser exists
	}
	return *result.BrowserClients
}

// acquireSessionLock tries to acquire a file-based lock for a session key.
// Returns the lock file handle on success. The caller must call releaseSessionLock.
// Retries with a short sleep for up to 5 seconds. If the lock holder is dead
// (e.g. killed with SIGKILL), the stale lock is automatically removed.
func acquireSessionLock(key string) (*os.File, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating sessions directory: %w", err)
	}
	lockPath := filepath.Join(dir, key+".lock")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// Write our PID so other processes can detect stale locks
			fmt.Fprintf(f, "%d", os.Getpid())
			return f, nil
		}
		// Lock file exists — check if the holder is still alive
		if removeStaleLock(lockPath) {
			continue // stale lock removed, retry immediately
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("could not acquire session lock for %s", key)
}

// removeStaleLock reads the PID from a lock file and removes it if the
// holder process is no longer alive. Returns true if the lock was removed.
func removeStaleLock(lockPath string) bool {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		// Corrupt or empty lock file — remove it
		os.Remove(lockPath)
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(lockPath)
		return true
	}
	// Signal 0 checks if the process exists without actually signaling it
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is dead — remove stale lock
		os.Remove(lockPath)
		return true
	}
	return false
}

// releaseSessionLock closes and removes the lock file.
func releaseSessionLock(f *os.File) {
	name := f.Name()
	f.Close()
	os.Remove(name)
}

// startDaemon spawns a crit _serve process in the background and waits for it to be ready.
// The key must match what the daemon computes in runServe (sessionKey(cwd, fileArgs)).
// Raw args (including flags) are passed through to _serve which parses them itself.
func startDaemon(key string, args []string) (sessionEntry, error) {
	// Acquire file lock to prevent two simultaneous crit calls from racing
	lock, err := acquireSessionLock(key)
	if err != nil {
		return sessionEntry{}, err
	}
	defer releaseSessionLock(lock)

	// Re-check for alive session under the lock — another process may have won the race
	if entry, alive := findAliveSession(key); alive {
		return entry, nil
	}

	selfPath, err := os.Executable()
	if err != nil {
		return sessionEntry{}, fmt.Errorf("finding executable: %w", err)
	}

	cmdArgs := []string{"_serve"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(selfPath, cmdArgs...)
	cmd.Dir, _ = os.Getwd()
	cmd.Stdout = nil
	cmd.Stdin = nil

	// Redirect stderr to a log file so daemon errors are preserved
	logPath, err := sessionLogPath(key)
	if err != nil {
		return sessionEntry{}, fmt.Errorf("creating log path: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return sessionEntry{}, fmt.Errorf("creating daemon log file: %w", err)
	}
	cmd.Stderr = logFile

	// Detach from parent process group so it survives parent exit
	cmd.SysProcAttr = daemonSysProcAttr()

	// Clear existing session file so the poll loop doesn't find an old daemon
	removeSessionFile(key)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return sessionEntry{}, fmt.Errorf("starting daemon: %w", err)
	}
	// Close log file handle in parent — the daemon process owns it now
	logFile.Close()
	newPID := cmd.Process.Pid

	// Monitor for early exit in background
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for OUR daemon to write its session file (poll up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			// Daemon exited before becoming ready — read log for details
			msg := readDaemonLog(key)
			if msg != "" {
				return sessionEntry{}, fmt.Errorf("daemon exited: %s", msg)
			}
			return sessionEntry{}, fmt.Errorf("daemon exited: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
		entry, err := readSessionFile(key)
		if err != nil {
			continue
		}
		// Verify this is OUR daemon, not a leftover from a previous one
		if entry.PID == newPID && isDaemonAlive(entry) {
			return entry, nil
		}
	}

	// Timed out — kill the orphan process
	cmd.Process.Kill()
	<-exited // drain the Wait goroutine
	return sessionEntry{}, fmt.Errorf("daemon did not start within 5 seconds")
}

// startDaemonInDir is like startDaemon but runs the daemon with a specific working directory.
// Used by crit issue to run daemons in worktree directories.
func startDaemonInDir(key string, args []string, cwd string) (sessionEntry, error) {
	// Acquire file lock to prevent two simultaneous crit calls from racing
	lock, err := acquireSessionLock(key)
	if err != nil {
		return sessionEntry{}, err
	}
	defer releaseSessionLock(lock)

	// Re-check for alive session under the lock
	if entry, alive := findAliveSession(key); alive {
		return entry, nil
	}

	selfPath, err := os.Executable()
	if err != nil {
		return sessionEntry{}, fmt.Errorf("finding executable: %w", err)
	}

	cmdArgs := []string{"_serve"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(selfPath, cmdArgs...)
	cmd.Dir = cwd
	cmd.Stdout = nil
	cmd.Stdin = nil

	logPath, err := sessionLogPath(key)
	if err != nil {
		return sessionEntry{}, fmt.Errorf("creating log path: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return sessionEntry{}, fmt.Errorf("creating daemon log file: %w", err)
	}
	cmd.Stderr = logFile

	cmd.SysProcAttr = daemonSysProcAttr()
	removeSessionFile(key)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return sessionEntry{}, fmt.Errorf("starting daemon: %w", err)
	}
	logFile.Close()
	newPID := cmd.Process.Pid

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			msg := readDaemonLog(key)
			if msg != "" {
				return sessionEntry{}, fmt.Errorf("daemon exited: %s", msg)
			}
			return sessionEntry{}, fmt.Errorf("daemon exited: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
		entry, err := readSessionFile(key)
		if err != nil {
			continue
		}
		if entry.PID == newPID && isDaemonAlive(entry) {
			return entry, nil
		}
	}

	cmd.Process.Kill()
	<-exited
	return sessionEntry{}, fmt.Errorf("daemon did not start within 5 seconds")
}

// sessionLogPath returns the path for a daemon's log file.
func sessionLogPath(key string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".log"), nil
}

// readDaemonLog reads and returns the trimmed contents of a daemon log file.
func readDaemonLog(key string) string {
	logPath, err := sessionLogPath(key)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true, // new process group, survives parent exit
	}
}

// stopDaemon stops the daemon for the given session key.
func stopDaemon(key string) error {
	entry, err := readSessionFile(key)
	if err != nil {
		return fmt.Errorf("no session found: %w", err)
	}

	// Verify this PID is actually our crit daemon (not a reused PID)
	if !isDaemonAlive(entry) {
		removeSessionFile(key)
		return nil
	}

	proc, err := os.FindProcess(entry.PID)
	if err != nil {
		removeSessionFile(key)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		removeSessionFile(key)
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
	removeSessionFile(key)
	return nil
}

// stopAllDaemonsForCWD stops all daemons running in the given directory.
func stopAllDaemonsForCWD(cwd string) {
	_, keys := listSessionsForCWD(cwd)
	for _, key := range keys {
		stopDaemon(key)
	}
}
