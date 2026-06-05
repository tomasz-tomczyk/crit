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
	"time"
)

type commonDaemonFlags struct {
	port     int
	host     string
	noOpen   bool
	quiet    bool
	shareURL string
}

func appendCommonDaemonFlags(args []string, f commonDaemonFlags) []string {
	if f.port != 0 {
		args = append(args, "--port", strconv.Itoa(f.port))
	}
	if f.host != "" && f.host != "127.0.0.1" {
		args = append(args, "--host", f.host)
	}
	if f.noOpen {
		args = append(args, "--no-open")
	}
	if f.quiet {
		args = append(args, "--quiet")
	}
	if f.shareURL != "" {
		args = append(args, "--share-url", f.shareURL)
	}
	return args
}

// aliveClient is used by isDaemonAlive which is called in a loop by
// listSessionsForCWD — a short timeout keeps listing responsive.
var aliveClient = &http.Client{Timeout: time.Second}

// browserClient is used by daemonHasBrowser which is called once per
// daemon lifecycle and can tolerate a longer timeout.
var browserClient = &http.Client{Timeout: 2 * time.Second}

// sessionEntry tracks a running daemon process in ~/.crit/sessions/.
type sessionEntry struct {
	PID        int      `json:"pid"`
	Port       int      `json:"port"`
	Host       string   `json:"host,omitempty"`
	CWD        string   `json:"cwd"`
	Args       []string `json:"args,omitempty"`
	Branch     string   `json:"branch"`
	ReviewPath string   `json:"review_path"`
	StartedAt  string   `json:"started_at"`
}

// displayHost returns the host suitable for user-facing URLs.
// Falls back to "localhost" for the default 127.0.0.1 binding or
// when host is empty (older session files).
func (e sessionEntry) displayHost() string {
	return hostForDisplay(e.Host)
}

// baseURL returns the user-facing HTTP base URL (browser, stderr).
func (e sessionEntry) baseURL() string {
	return fmt.Sprintf("http://%s:%d", e.displayHost(), e.Port)
}

// connURL returns the HTTP base URL for internal connectivity (health checks, API calls).
// Uses the raw bind address to avoid DNS resolution mismatches (e.g. localhost → [::1]).
func (e sessionEntry) connURL() string {
	host := e.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, e.Port)
}

// hostForDisplay maps a listen host to a user-facing hostname.
func hostForDisplay(host string) string {
	if host == "" || host == "127.0.0.1" {
		return "localhost"
	}
	return host
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
		return cwd, nil //nolint:nilerr // best-effort: fall back to unresolved path
	}
	return resolved, nil
}

// sessionKey returns a deterministic hash used as the session filename.
// Git mode (no args): sha256(cwd + "\0" + branch)[:12] — branch-scoped because diffs depend on it.
// File mode (args present): sha256(cwd + "\0" + arg1 + "\0" + arg2 + ...)[:12] — branch-independent
// because file reviews are not tied to a specific branch.
func sessionKey(cwd string, branch string, args []string) string {
	sorted := make([]string, len(args))
	copy(sorted, args)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte{0})
	if len(sorted) == 0 {
		// Git mode: include branch so different branches get separate sessions.
		h.Write([]byte(branch))
	}
	for _, a := range sorted {
		h.Write([]byte{0})
		h.Write([]byte(a))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// liveSessionKey returns the session/review key for a live-mode session.
// Formula: sha256(cwd + "\0live\0" + origin)[:12].
// The "\0live\0" separator ensures live reviews never collide with code
// reviews in the same cwd (code reviews use "\0" + branch or "\0" + args).
func liveSessionKey(cwd, origin string) string {
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte("\x00live\x00"))
	h.Write([]byte(origin))
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

// reviewsDir returns the path to ~/.crit/reviews/.
func reviewsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "reviews"), nil
}

// reviewFilePath returns the v4 review identity path: a folder named <key>
// (no extension) under ~/.crit/reviews/. The actual JSON files live inside as
// <key>/review.json and <key>/snapshots.json — see reviewPathsFor.
func reviewFilePath(key string) (string, error) {
	dir, err := reviewsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key), nil
}

// writeSessionFile writes a session entry to ~/.crit/sessions/<key>.json.
func writeSessionFile(key string, entry sessionEntry) error {
	path, err := sessionFilePath(key)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0600)
}

// readSessionFile reads a session entry from ~/.crit/sessions/<key>.json.
func readSessionFile(key string) (sessionEntry, error) {
	path, err := sessionFilePath(key)
	if err != nil {
		return sessionEntry{}, err
	}
	data, err := readFileShared(path)
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
	// Normalize on both sides so a session stored from any path style
	// matches a probe with another style (matters on Windows where
	// os.Getwd returns backslashes but tests/fixtures may use POSIX paths).
	cwdSlash := filepath.ToSlash(cwd)
	var alive []sessionEntry
	var keys []string
	for _, de := range dirEntries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".json")
		data, err := readFileShared(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if filepath.ToSlash(entry.CWD) != cwdSlash {
			continue
		}
		if isDaemonAlive(entry) {
			alive = append(alive, entry)
			keys = append(keys, key)
		} else {
			removeSessionFile(key)
		}
	}
	return alive, keys
}

// findSessionForCWDBranch scans all alive sessions for the given cwd and branch.
// Returns the session, its key, and the number of branch matches found.
// The session and key are only valid when matchCount == 1.
func findSessionForCWDBranch(cwd, branch string) (entry sessionEntry, key string, matchCount int) {
	sessions, keys := listSessionsForCWD(cwd)
	var matched []int
	for i, s := range sessions {
		if s.Branch == branch {
			matched = append(matched, i)
		}
	}
	if len(matched) == 1 {
		return sessions[matched[0]], keys[matched[0]], 1
	}
	return sessionEntry{}, "", len(matched)
}

// listSessionsForRepoRoot returns alive sessions whose CWD is within the given
// git repository root. This handles the case where `crit` was started from a
// subdirectory (e.g. repo/api) but `crit comment` is run from a different
// subdirectory or the repo root itself.
func listSessionsForRepoRoot(repoRoot string) ([]sessionEntry, []string) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	// Normalize separators on both sides — the stored CWD could have been
	// written from any host, and on Windows os.Getwd() returns backslashes
	// while subdirectory tests/fixtures often use forward slashes. Compare
	// using POSIX form so the prefix check works regardless of origin.
	repoRootSlash := filepath.ToSlash(repoRoot)
	prefix := repoRootSlash + "/"
	var alive []sessionEntry
	var keys []string
	for _, de := range dirEntries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".json")
		data, err := readFileShared(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entryCWD := filepath.ToSlash(entry.CWD)
		if entryCWD != repoRootSlash && !strings.HasPrefix(entryCWD, prefix) {
			continue
		}
		if isDaemonAlive(entry) {
			alive = append(alive, entry)
			keys = append(keys, key)
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
	if !processExists(proc) {
		return false
	}
	// HTTP health probe — ensures the port belongs to our daemon, not a reused PID.
	// We validate the response body to guard against a non-crit process on the same port.
	resp, err := aliveClient.Get(s.connURL() + "/api/health")
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
	resp, err := browserClient.Get(s.connURL() + "/api/health")
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
	if err := os.MkdirAll(dir, 0700); err != nil {
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
		err = flockExclusiveNB(f)
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
	_ = funlock(f)
	name := f.Name()
	f.Close()
	os.Remove(name)
}

// setupDaemonCmd creates and configures the daemon child process.
// Returns the command, readiness pipe read-end, write-end, log file, and any error.
// The caller must close writeEnd and logFile after Start().
//
// Readiness is signaled via the child's stdout (an OS pipe). We deliberately
// avoid cmd.ExtraFiles + FD 3: ExtraFiles is documented as unsupported on
// Windows (see os/exec/exec.go), so the inherited handle is silently dropped
// and the child's os.NewFile(3, ...) returns a stale handle whose writes go
// nowhere. Stdout inheritance works on every supported OS, so the child reads
// readiness via os.Stdout and the parent reads it via the pipe's read end.
// _CRIT_READY_STDOUT=1 tells the child to treat stdout as the readiness pipe
// (otherwise stdout is the user's terminal and we must not emit the port).
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
	cmd.Stdout = writeEnd
	cmd.Env = append(os.Environ(), "_CRIT_READY_STDOUT=1")
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

//nolint:unparam // error return is kept for consistent startDaemon select-case handling
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

//nolint:unparam // sessionEntry return is kept for consistent startDaemon select-case handling
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
	return sessionEntry{}, fmt.Errorf("daemon startup failed: %w", readErr)
}

// startDaemon spawns a crit _serve process in the background and waits for it to be ready.
// The key must match what the daemon computes in runServe (sessionKey(cwd, branch, fileArgs)).
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
		return sessionEntry{}, fmt.Errorf("daemon exited: %w", err)

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

// openReadyPipe returns the readiness pipe (the inherited stdout) if this
// process was spawned as a daemon with _CRIT_READY_STDOUT=1. Returns nil
// otherwise. The caller owns the returned file and must close it. After the
// pipe is returned, os.Stdout is repointed at the log file (stderr) so any
// stray writes to stdout don't corrupt the readiness handshake.
func openReadyPipe() *os.File {
	if os.Getenv("_CRIT_READY_STDOUT") != "1" {
		return nil
	}
	os.Unsetenv("_CRIT_READY_STDOUT")
	pipe := os.Stdout
	// Repoint stdout at stderr (the daemon log file) so subsequent writes
	// to fmt.Println/log don't accidentally race the port handshake or
	// keep the parent's read-end open after we close the pipe.
	os.Stdout = os.Stderr
	return pipe
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
		return nil //nolint:nilerr // process not found, session already cleaned up
	}

	if err := terminateProcess(proc); err != nil {
		removeSessionFile(key)
		return nil //nolint:nilerr // process already gone, cleanup is sufficient
	}

	// Poll for process exit, escalate to Kill if still alive after the deadline.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if !processExists(proc) {
			break
		}
	}
	if processExists(proc) {
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

// cleanOrphanedSessions removes session files whose daemon PID is dead.
// It silently ignores all errors — intended for best-effort background use.
func cleanOrphanedSessions() {
	sessDir, err := sessionsDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(sessDir, de.Name())
		data, err := readFileShared(path)
		if err != nil {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal(data, &entry) != nil {
			continue
		}
		if !isDaemonAlive(entry) {
			os.Remove(path)
			key := strings.TrimSuffix(de.Name(), ".json")
			os.Remove(filepath.Join(sessDir, key+".log"))
			os.Remove(filepath.Join(sessDir, key+".lock"))
		}
	}
}
