package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// aliveClient is used by isDaemonAlive which is called in a loop by
// listSessionsForCWD — a short timeout keeps listing responsive.
var aliveClient = &http.Client{Timeout: time.Second}

// browserClient is used by daemonHasBrowser which is called once per
// daemon lifecycle and can tolerate a longer timeout.
var browserClient = &http.Client{Timeout: 2 * time.Second}

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
// Uses atomic temp file + fsync + rename to prevent corruption.
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

	target := filepath.Join(dir, key+".json")
	tmp, err := os.CreateTemp(dir, key+"*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, target)
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
	resp, err := aliveClient.Get(fmt.Sprintf("http://localhost:%d/api/health", s.Port))
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
	resp, err := browserClient.Get(fmt.Sprintf("http://localhost:%d/api/health", s.Port))
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

// acquireSessionLock tries to acquire a file-based lock for a session key using flock().
// Returns the lock file handle on success. The caller must call releaseSessionLock.
// flock is automatically released when the process dies, preventing stale locks.
// Uses exponential backoff starting at 100ms, doubling up to 500ms.
func acquireSessionLock(key string) (*os.File, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating sessions directory: %w", err)
	}
	lockPath := filepath.Join(dir, key+".lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(5 * time.Second)
	backoff := 100 * time.Millisecond
	for time.Now().Before(deadline) {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		time.Sleep(backoff)
		if backoff < 500*time.Millisecond {
			backoff *= 2
		}
	}
	f.Close()
	return nil, fmt.Errorf("could not acquire session lock for %s", key)
}

// releaseSessionLock unlocks, closes, and removes the lock file.
func releaseSessionLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	name := f.Name()
	f.Close()
	os.Remove(name)
}

// setupDaemonCmd creates and configures the daemon child process.
// Returns the command, readiness pipe read-end, write-end, log file, and any error.
// The caller must close writeEnd and logFile after Start().
func setupDaemonCmd(key string, args []string) (*exec.Cmd, *os.File, *os.File, *os.File, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("finding executable: %w", err)
	}

	cmdArgs := append([]string{"_serve"}, args...)
	cmd := exec.Command(selfPath, cmdArgs...)

	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("getting working directory: %w", err)
	}
	cmd.Dir = cwd
	cmd.Stdout = nil
	cmd.Stdin = nil

	logPath, err := sessionLogPath(key)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating log path: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating daemon log file: %w", err)
	}
	cmd.Stderr = logFile

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		logFile.Close()
		return nil, nil, nil, nil, fmt.Errorf("creating readiness pipe: %w", err)
	}
	cmd.ExtraFiles = []*os.File{writeEnd}
	cmd.Env = append(os.Environ(), "_CRIT_READY_FD=3")
	cmd.SysProcAttr = daemonSysProcAttr()

	return cmd, readEnd, writeEnd, logFile, nil
}

func readPortFromPipe(readEnd *os.File) (portCh chan int, errCh chan error) {
	portCh = make(chan int, 1)
	errCh = make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(readEnd)
		if !scanner.Scan() {
			errCh <- fmt.Errorf("daemon closed readiness pipe without writing")
			return
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "error:") {
			errCh <- fmt.Errorf("%s", strings.TrimPrefix(line, "error:"))
			return
		}
		port, err := strconv.Atoi(line)
		if err != nil {
			errCh <- fmt.Errorf("daemon wrote invalid port: %q", line)
			return
		}
		portCh <- port
	}()
	return portCh, errCh
}

func handleDaemonReady(key string, port, pid int, readEnd *os.File, cmd *exec.Cmd) (sessionEntry, error) {
	readEnd.Close()
	cmd.Process.Release()

	entry, err := readSessionFile(key)
	if err != nil {
		log.Printf("Warning: failed to read session file for key %s: %v (using partial entry)", key, err)
		cwd, _ := resolvedCWD()
		entry = sessionEntry{
			PID:       pid,
			Port:      port,
			CWD:       cwd,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	return entry, nil
}

func handleDaemonPipeError(key string, readErr error, readEnd *os.File, cmd *exec.Cmd, exited chan error) (sessionEntry, error) {
	readEnd.Close()
	// Wait briefly for daemon exit — pipe EOF usually means it already crashed.
	// cmd.Wait() completes near-instantly for a dead process; the timeout
	// handles the rare case where the daemon closed FD 3 but is still running.
	select {
	case <-exited:
	case <-time.After(500 * time.Millisecond):
		cmd.Process.Kill()
		<-exited
	}
	msg := readDaemonLog(key)
	if msg != "" {
		return sessionEntry{}, fmt.Errorf("daemon exited: %s", msg)
	}
	return sessionEntry{}, fmt.Errorf("daemon startup failed: %v", readErr)
}

// startDaemon spawns a crit _serve process in the background and waits for it to be ready.
// The key must match what the daemon computes in runServe (sessionKey(cwd, fileArgs)).
// Raw args (including flags) are passed through to _serve which parses them itself.
// Uses an OS pipe (FD 3) for the daemon to signal readiness by writing its port number.
func startDaemon(key string, args []string) (sessionEntry, error) {
	lock, err := acquireSessionLock(key)
	if err != nil {
		return sessionEntry{}, err
	}
	defer releaseSessionLock(lock)

	if entry, alive := findAliveSession(key); alive {
		return entry, nil
	}

	cmd, readEnd, writeEnd, logFile, err := setupDaemonCmd(key, args)
	if err != nil {
		return sessionEntry{}, err
	}

	removeSessionFile(key)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		readEnd.Close()
		writeEnd.Close()
		return sessionEntry{}, fmt.Errorf("starting daemon: %w", err)
	}
	writeEnd.Close()
	logFile.Close()
	newPID := cmd.Process.Pid

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	portCh, errCh := readPortFromPipe(readEnd)

	select {
	case port := <-portCh:
		return handleDaemonReady(key, port, newPID, readEnd, cmd)

	case readErr := <-errCh:
		return handleDaemonPipeError(key, readErr, readEnd, cmd, exited)

	case err := <-exited:
		readEnd.Close()
		msg := readDaemonLog(key)
		if msg != "" {
			return sessionEntry{}, fmt.Errorf("daemon exited: %s", msg)
		}
		return sessionEntry{}, fmt.Errorf("daemon exited: %v", err)

	case <-time.After(10 * time.Second):
		readEnd.Close()
		cmd.Process.Kill()
		<-exited
		return sessionEntry{}, fmt.Errorf("daemon did not start within 10 seconds")
	}
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

// openReadyPipe returns the readiness pipe (FD 3) if this process was
// spawned as a daemon with _CRIT_READY_FD=3. Returns nil otherwise.
// The caller owns the returned file and must close it.
func openReadyPipe() *os.File {
	if os.Getenv("_CRIT_READY_FD") != "3" {
		return nil
	}
	os.Unsetenv("_CRIT_READY_FD")
	return os.NewFile(3, "ready-pipe")
}

// signalReadiness writes the port number to the readiness pipe.
// pipe may be nil (not running as daemon), in which case this is a no-op.
func signalReadiness(pipe *os.File, port int) {
	if pipe == nil {
		return
	}
	fmt.Fprintf(pipe, "%d\n", port)
	pipe.Close()
}

// daemonFatal reports a startup error through the readiness pipe so the
// parent process receives a structured message, then exits.
// pipe may be nil (not running as daemon); the error is always logged to stderr.
func daemonFatal(pipe *os.File, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	if pipe != nil {
		fmt.Fprintf(pipe, "error:%s\n", msg)
		pipe.Close()
	}
	os.Exit(1)
}

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // new session, fully detached from controlling terminal
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
