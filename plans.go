package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// planStorageDir returns the managed storage directory for a plan session.
// Uses the slug directly as the directory name (not a hash) for human readability.
func planStorageDir(slug string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "plans", slug), nil
}

// planSessionKey computes a session key for plan mode.
// Uses cwd + slug to produce a unique 12-char hex key.
func planSessionKey(cwd, slug string) string {
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte{0})
	h.Write([]byte("__plan:" + slug))
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a string to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// savePlanVersion saves content as the next numbered version and updates current.md.
// Returns the version number (1-based).
func savePlanVersion(dir string, content []byte) (int, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("creating plan storage dir: %w", err)
	}

	ver := latestPlanVersion(dir) + 1
	versionPath := filepath.Join(dir, fmt.Sprintf("v%03d.md", ver))
	currentPath := filepath.Join(dir, "current.md")

	if err := os.WriteFile(versionPath, content, 0644); err != nil {
		return 0, fmt.Errorf("writing version %d: %w", ver, err)
	}
	if err := os.WriteFile(currentPath, content, 0644); err != nil {
		return 0, fmt.Errorf("writing current.md: %w", err)
	}

	return ver, nil
}

// latestPlanVersion returns the highest version number in the directory,
// or 0 if no versions exist.
func latestPlanVersion(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	max := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "v") || !strings.HasSuffix(name, ".md") {
			continue
		}
		numStr := strings.TrimPrefix(strings.TrimSuffix(name, ".md"), "v")
		var n int
		if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max
}

func buildPlanDaemonArgs(currentPath, storageDir, slug string, port int, noOpen, quiet bool) []string {
	var args []string
	if port != 0 {
		args = append(args, "--port", fmt.Sprintf("%d", port))
	}
	if noOpen {
		args = append(args, "--no-open")
	}
	if quiet {
		args = append(args, "--quiet")
	}
	args = append(args, "--plan-dir", storageDir)
	args = append(args, "--name", slug)
	args = append(args, currentPath)
	return args
}

func applyPlanOverrides(s *Session, planDir, slug string) {
	s.Mode = "plan"
	s.PlanDir = planDir
	s.OutputDir = planDir
	s.RepoRoot = planDir
	for _, f := range s.Files {
		f.Path = slug + ".md"
	}
}

// resolveSlug determines the plan slug when --name is not provided.
// Extracts from the first markdown heading + today's date.
// Falls back to "plan-{date-time}" if no heading found.
func resolveSlug(content []byte) string {
	date := time.Now().Format("2006-01-02")

	// Try first markdown heading
	for _, line := range strings.SplitN(string(content), "\n", 30) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			heading := strings.TrimPrefix(line, "# ")
			if s := slugify(heading); s != "" {
				return s + "-" + date
			}
		}
	}

	// Timestamp fallback
	return "plan-" + time.Now().Format("2006-01-02-150405")
}

// planSessionsFile returns the path to the plan sessions mapping file.
func planSessionsFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "plan-sessions.json"), nil
}

type planSessionMapping struct {
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
}

// lookupPlanSlug returns the pinned slug for a session_id, if one exists.
func lookupPlanSlug(sessionID string) (string, bool) {
	path, err := planSessionsFile()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var m map[string]planSessionMapping
	if err := json.Unmarshal(data, &m); err != nil {
		return "", false
	}
	entry, ok := m[sessionID]
	if !ok {
		return "", false
	}
	return entry.Slug, true
}

// acquirePlanSessionsLock acquires an advisory file lock on the plan-sessions
// lock file, blocking until the lock is available. Returns the lock file handle;
// the caller must call releasePlanSessionsLock when done.
func acquirePlanSessionsLock(path string) (*os.File, error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("creating plan sessions directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening plan sessions lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquiring plan sessions lock: %w", err)
	}
	return f, nil
}

// releasePlanSessionsLock releases the advisory lock and closes the file.
func releasePlanSessionsLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}

// readPlanSessions reads and parses the plan sessions file.
// Returns an empty map if the file does not exist.
// Returns an error if the file exists but contains invalid JSON.
func readPlanSessions(path string) (map[string]planSessionMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]planSessionMapping), nil
		}
		return nil, fmt.Errorf("reading plan sessions: %w", err)
	}
	var m map[string]planSessionMapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing plan sessions %s: %w", path, err)
	}
	if m == nil {
		m = make(map[string]planSessionMapping)
	}
	return m, nil
}

// savePlanSlug pins a slug to a session_id. Prunes entries older than 7 days.
// Uses advisory file locking to prevent concurrent read-modify-write races,
// and returns an error if the existing file contains corrupt JSON rather than
// silently overwriting it.
func savePlanSlug(sessionID, slug string) error {
	path, err := planSessionsFile()
	if err != nil {
		return err
	}

	lock, err := acquirePlanSessionsLock(path)
	if err != nil {
		return err
	}
	defer releasePlanSessionsLock(lock)

	m, err := readPlanSessions(path)
	if err != nil {
		return err
	}

	m[sessionID] = planSessionMapping{
		Slug:      slug,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	// Prune stale entries
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for k, v := range m {
		if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil && t.Before(cutoff) {
			delete(m, k)
		}
	}

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write via temp file + rename to prevent partial writes.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0644); err != nil {
		return fmt.Errorf("writing plan sessions temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming plan sessions temp file: %w", err)
	}
	return nil
}

// isStdinPipe returns true if stdin is a pipe (not a terminal).
func isStdinPipe() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}
