package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds all configuration values from config files.
type Config struct {
	Port               int      `json:"port,omitempty"`
	Host               string   `json:"host,omitempty"` // listen host (default 127.0.0.1)
	NoOpen             bool     `json:"no_open,omitempty"`
	ShareURL           string   `json:"share_url,omitempty"`
	ProxyAuth          bool     `json:"proxy_auth,omitempty"`
	Quiet              bool     `json:"quiet,omitempty"`
	Output             string   `json:"output,omitempty"`
	Author             string   `json:"author,omitempty"`
	BaseBranch         string   `json:"base_branch,omitempty"`
	IgnorePatterns     []string `json:"ignore_patterns,omitempty"`
	NoIntegrationCheck bool     `json:"no_integration_check,omitempty"`
	NoUpdateCheck      bool     `json:"no_update_check,omitempty"`
	AgentCmd           string   `json:"agent_cmd,omitempty"`
	AuthToken          string   `json:"auth_token,omitempty"`
	AuthUserName       string   `json:"auth_user_name,omitempty"`
	AuthUserEmail      string   `json:"auth_user_email,omitempty"`
	AuthUserID         string   `json:"auth_user_id,omitempty"`
	CleanupOnApprove   *bool    `json:"cleanup_on_approve,omitempty"`
	DisableStats       bool     `json:"disable_stats,omitempty"`
	VCS                string   `json:"vcs,omitempty"` // preferred VCS backend: "git", "sl", "jj"
	ShareConsented     bool     `json:"share_consented,omitempty"`
	LiveCookie         string   `json:"live_cookie,omitempty"`
	LiveCookieFile     string   `json:"live_cookie_file,omitempty"`
}

// needsShareConsent reports whether the user must confirm before sharing.
// Only applies to the default crit.md service — self-hosted users opted in by
// configuring a custom URL.
func needsShareConsent(cfg Config, shareURL string) bool {
	return shareURL == defaultShareURL && !cfg.ShareConsented
}

// CleanupOnApproveEnabled returns whether review files should be cleaned up
// when a review is approved. Defaults to true if not explicitly set.
func (c Config) CleanupOnApproveEnabled() bool {
	if c.CleanupOnApprove != nil {
		return *c.CleanupOnApprove
	}
	return true
}

// String returns a human-readable JSON representation of the resolved config.
func (c Config) String() string {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data) + "\n"
}

// defaultConfig returns a config template with all keys present,
// suitable for generating a starter config file.
// Uses a map to avoid omitempty suppressing zero-value fields.
func defaultConfig() generatedConfig {
	return generatedConfig{
		Port:       0,
		Host:       "127.0.0.1",
		NoOpen:     false,
		ShareURL:   "https://crit.md",
		ProxyAuth:  false,
		Quiet:      false,
		Output:     "",
		Author:     "",
		BaseBranch: "",
		IgnorePatterns: []string{
			"*.lock",
			"*.min.js",
			"*.min.css",
			".crit/",
		},
		AgentCmd:         "",
		CleanupOnApprove: true,
		VCS:              "",
	}
}

// generatedConfig is like Config but without omitempty, so all keys appear in output.
// auth_token is intentionally excluded — it is global-only and should not appear
// in project config files where it could be accidentally committed.
type generatedConfig struct {
	Port               int      `json:"port"`
	Host               string   `json:"host"`
	NoOpen             bool     `json:"no_open"`
	ShareURL           string   `json:"share_url"`
	ProxyAuth          bool     `json:"proxy_auth"`
	Quiet              bool     `json:"quiet"`
	Output             string   `json:"output"`
	Author             string   `json:"author"`
	BaseBranch         string   `json:"base_branch"`
	IgnorePatterns     []string `json:"ignore_patterns"`
	NoIntegrationCheck bool     `json:"no_integration_check"`
	NoUpdateCheck      bool     `json:"no_update_check"`
	DisableStats       bool     `json:"disable_stats"`
	AgentCmd           string   `json:"agent_cmd"`
	CleanupOnApprove   bool     `json:"cleanup_on_approve"`
	VCS                string   `json:"vcs"`
}

func (c generatedConfig) String() string {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data) + "\n"
}

// configPresence tracks which fields were explicitly present in a JSON config file.
// This allows distinguishing "not set" from "explicitly set to empty/zero".
type configPresence struct {
	ShareURL           bool
	IgnorePatterns     bool
	NoOpen             bool
	Quiet              bool
	NoIntegrationCheck bool
	NoUpdateCheck      bool
	CleanupOnApprove   bool
	ShareConsented     bool
}

// loadConfigFile reads and parses a single JSON config file.
// Returns a zero Config and empty presence if the file doesn't exist.
func loadConfigFile(path string) (Config, configPresence, error) {
	var cfg Config
	var presence configPresence
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, presence, nil
		}
		return cfg, presence, err
	}

	// Detect which keys are explicitly present in the JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, presence, fmt.Errorf("parsing %s: %w", path, err)
	}
	_, presence.ShareURL = raw["share_url"] // for global config only; project-side ShareURL presence is intentionally ignored by mergeConfigs
	_, presence.IgnorePatterns = raw["ignore_patterns"]
	_, presence.NoOpen = raw["no_open"]
	_, presence.Quiet = raw["quiet"]
	_, presence.NoIntegrationCheck = raw["no_integration_check"]
	_, presence.NoUpdateCheck = raw["no_update_check"]
	_, presence.CleanupOnApprove = raw["cleanup_on_approve"]
	_, presence.ShareConsented = raw["share_consented"]

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, presence, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, presence, nil
}

// mergeConfigs merges project config on top of global config.
// Non-zero project values override global. Bool fields use presence tracking
// so that project config `no_open: false` can override global `no_open: true`.
// Ignore patterns are unioned.
func mergeConfigs(global, project Config, projectPresence configPresence) Config {
	merged := global
	if project.Port != 0 {
		merged.Port = project.Port
	}
	// Security: host is intentionally NOT merged from project config.
	// A malicious repo setting host to "0.0.0.0" would disable the
	// DNS-rebinding defense. Use --host flag or CRIT_HOST env var instead.
	if projectPresence.NoOpen {
		merged.NoOpen = project.NoOpen
	}
	// Security: proxy_auth is intentionally NOT merged from project config.
	// It is global-only, like agent_cmd, auth_token, and share_url.
	if projectPresence.Quiet {
		merged.Quiet = project.Quiet
	}
	if project.Output != "" {
		merged.Output = project.Output
	}
	if project.Author != "" {
		merged.Author = project.Author
	}
	if project.BaseBranch != "" {
		merged.BaseBranch = project.BaseBranch
	}
	if project.VCS != "" {
		merged.VCS = project.VCS
	}
	if projectPresence.NoIntegrationCheck {
		merged.NoIntegrationCheck = project.NoIntegrationCheck
	}
	if projectPresence.NoUpdateCheck {
		merged.NoUpdateCheck = project.NoUpdateCheck
	}
	if projectPresence.CleanupOnApprove {
		merged.CleanupOnApprove = project.CleanupOnApprove
	}
	if project.LiveCookie != "" {
		merged.LiveCookie = project.LiveCookie
	}
	if project.LiveCookieFile != "" {
		merged.LiveCookieFile = project.LiveCookieFile
	}
	// Security: agent_cmd, auth_token, share_url, and proxy_auth are intentionally
	// NOT merged from project config. They must remain global-only: agent_cmd to
	// prevent untrusted repos from hijacking the agent command; auth_token and
	// share_url to prevent a malicious repo's .crit.config.json from redirecting
	// share requests (and the bearer token) to an attacker-controlled host;
	// proxy_auth to prevent a repo from silently changing the transport mode.
	// live_cookie/live_cookie_file DO merge from project config — common for local
	// dev auth. Prefer live_cookie_file pointing at a gitignored path (e.g.
	// .crit/live-cookies.txt) over committing live_cookie inline.
	// Union ignore patterns
	merged.IgnorePatterns = append(merged.IgnorePatterns, project.IgnorePatterns...)
	return merged
}

// LoadConfig loads and merges configuration from all sources.
// projectDir is the repo root (or cwd if not in a git repo).
// Runtime defaults (share_url, ignore_patterns) are applied when no config
// file explicitly sets those fields. share_url is global-only — project config
// cannot override it. To suppress the share_url default, set it to "" in
// ~/.crit.config.json.
func LoadConfig(projectDir string) Config {
	// 1. Global config
	global, globalPresence, err := loadConfigFile(globalConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: reading global config: %v\n", err)
	}

	// 2. Project config (skip if same file as global config, e.g. when CWD is home dir)
	var project Config
	var projectPresence configPresence
	projectConfigPath := filepath.Join(projectDir, ".crit.config.json")
	globalAbs, _ := filepath.Abs(globalConfigPath())
	projectAbs, _ := filepath.Abs(projectConfigPath)
	if globalAbs != projectAbs {
		project, projectPresence, err = loadConfigFile(projectConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: reading project config: %v\n", err)
		}
	}

	// 3. Merge global + project
	merged := mergeConfigs(global, project, projectPresence)

	// 4. Apply runtime defaults for fields not explicitly set in any config file.
	// share_url is global-only, so only globalPresence controls whether the default applies.
	if !globalPresence.ShareURL {
		merged.ShareURL = "https://crit.md"
	}
	if !globalPresence.IgnorePatterns && !projectPresence.IgnorePatterns {
		merged.IgnorePatterns = []string{".crit/"}
	}
	if merged.Host == "" {
		merged.Host = "127.0.0.1"
	}

	// 5. Fall back to VCS user name if no author configured.
	// Try the configured VCS first, then fall back to the other.
	if merged.Author == "" {
		switch merged.VCS {
		case "sl", "sapling":
			merged.Author = slUserName()
			if merged.Author == "" {
				merged.Author = gitUserName()
			}
		case "jj", "jujutsu":
			merged.Author = jjUserName()
			if merged.Author == "" {
				merged.Author = gitUserName()
			}
		default:
			merged.Author = gitUserName()
			if merged.Author == "" {
				merged.Author = slUserName()
			}
		}
	}

	return merged
}

// globalConfigPath returns the path to the global config file.
func globalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".crit.config.json")
}

// saveGlobalConfig performs a read-modify-write on ~/.crit.config.json.
// It uses map[string]json.RawMessage to preserve unknown keys.
// The apply function receives the raw map and should set or delete keys as needed.
// The file is written with 0600 permissions since it may contain auth_token.
//
// A sibling lockfile (`<path>.lock`) is held with flock for the duration of the
// read-modify-write so concurrent crit invocations (e.g. login + lazy backfill)
// cannot lose updates. flock is released automatically when the process exits.
func saveGlobalConfig(apply func(m map[string]json.RawMessage) error) error {
	path := globalConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	unlock, err := lockGlobalConfig(path)
	if err != nil {
		return err
	}
	defer unlock()

	raw := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	if err := apply(raw); err != nil {
		return err
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')
	// atomicWriteFile (defined in daemon.go) writes via temp + fsync + rename.
	// Critical for the global config because it holds the bearer token —
	// a crash mid-write would otherwise truncate it.
	return atomicWriteFile(path, data, 0o600)
}

// lockGlobalConfig acquires an exclusive flock on `<path>.lock`, mirroring the
// pattern in acquireSessionLock. Returns an unlock func the caller must defer.
// Retries with exponential backoff up to a 5s deadline.
func lockGlobalConfig(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", lockPath, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	backoff := 50 * time.Millisecond
	for {
		if err := flockExclusiveNB(f); err == nil {
			return func() {
				_ = funlock(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("could not acquire lock on %s within 5s", lockPath)
		}
		time.Sleep(backoff)
		if backoff < 500*time.Millisecond {
			backoff *= 2
		}
	}
}

// gitUserName returns the git-configured user name, or empty string on error.
func gitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// slUserName returns the Sapling-configured user name, or empty string on error.
// Strips the email suffix ("Name <email>" -> "Name").
func slUserName() string {
	out, err := exec.Command("sl", "config", "ui.username").Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	if idx := strings.Index(name, " <"); idx >= 0 {
		name = name[:idx]
	}
	return name
}

// matchPattern checks if a file path matches an ignore pattern.
// Pattern types:
//   - "*.ext"         → matches files ending in .ext anywhere
//   - "dir/"          → matches all files under dir/
//   - "exact.file"    → matches filename anywhere in tree
//   - "path/*.ext"    → filepath.Match against full path
func matchPattern(pattern, path string) bool {
	// Patterns are POSIX-style ("dir/", "path/*.ext"). On Windows incoming
	// paths may carry backslashes (raw output from filepath.WalkDir / Rel);
	// normalize so the matcher behaves identically across platforms.
	path = filepath.ToSlash(path)

	// Directory prefix match
	if strings.HasSuffix(pattern, "/") {
		prefix := pattern // includes trailing /
		return strings.HasPrefix(path, prefix) || strings.Contains(path, "/"+prefix)
	}

	// If pattern contains /, match against full path
	if strings.Contains(pattern, "/") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Match against filename only
	filename := filepath.Base(path)
	matched, _ := filepath.Match(pattern, filename)
	return matched
}

// filterIgnored removes FileChange entries matching any ignore pattern.
func filterIgnored(files []FileChange, patterns []string) []FileChange {
	if len(patterns) == 0 {
		return files
	}
	var result []FileChange
	for _, f := range files {
		ignored := false
		for _, p := range patterns {
			if matchPattern(p, f.Path) {
				ignored = true
				break
			}
		}
		if !ignored {
			result = append(result, f)
		}
	}
	return result
}

// filterPathsIgnored removes string paths matching any ignore pattern.
// Used by handleFilesList to filter the file picker results.
func filterPathsIgnored(paths []string, patterns []string) []string {
	if len(patterns) == 0 {
		return paths
	}
	var result []string
	for _, p := range paths {
		ignored := false
		for _, pat := range patterns {
			if matchPattern(pat, p) {
				ignored = true
				break
			}
		}
		if !ignored {
			result = append(result, p)
		}
	}
	return result
}
