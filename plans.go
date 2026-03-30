package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// planStorageDir returns the managed storage directory for a plan session.
// Uses the slug directly as the directory name (not a hash) for human readability.
func planStorageDir(slug string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".crit", "plans", slug)
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
func planSessionsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".crit", "plan-sessions.json")
}

type planSessionMapping struct {
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
}

// lookupPlanSlug returns the pinned slug for a session_id, if one exists.
func lookupPlanSlug(sessionID string) (string, bool) {
	data, err := os.ReadFile(planSessionsFile())
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

// savePlanSlug pins a slug to a session_id. Prunes entries older than 7 days.
func savePlanSlug(sessionID, slug string) error {
	path := planSessionsFile()

	var m map[string]planSessionMapping
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &m)
	}
	if m == nil {
		m = make(map[string]planSessionMapping)
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

	os.MkdirAll(filepath.Dir(path), 0755)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// isStdinPipe returns true if stdin is a pipe (not a terminal).
func isStdinPipe() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}
