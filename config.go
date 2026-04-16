package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config holds all configuration values from config files.
type Config struct {
	Port               int      `json:"port,omitempty"`
	NoOpen             bool     `json:"no_open,omitempty"`
	ShareURL           string   `json:"share_url,omitempty"`
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
	CleanupOnApprove   *bool    `json:"cleanup_on_approve,omitempty"`
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
		NoOpen:     false,
		ShareURL:   "https://crit.md",
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
	}
}

// generatedConfig is like Config but without omitempty, so all keys appear in output.
// auth_token is intentionally excluded — it is global-only and should not appear
// in project config files where it could be accidentally committed.
type generatedConfig struct {
	Port               int      `json:"port"`
	NoOpen             bool     `json:"no_open"`
	ShareURL           string   `json:"share_url"`
	Quiet              bool     `json:"quiet"`
	Output             string   `json:"output"`
	Author             string   `json:"author"`
	BaseBranch         string   `json:"base_branch"`
	IgnorePatterns     []string `json:"ignore_patterns"`
	NoIntegrationCheck bool     `json:"no_integration_check"`
	NoUpdateCheck      bool     `json:"no_update_check"`
	AgentCmd           string   `json:"agent_cmd"`
	CleanupOnApprove   bool     `json:"cleanup_on_approve"`
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
	_, presence.ShareURL = raw["share_url"]
	_, presence.IgnorePatterns = raw["ignore_patterns"]
	_, presence.NoOpen = raw["no_open"]
	_, presence.Quiet = raw["quiet"]
	_, presence.NoIntegrationCheck = raw["no_integration_check"]
	_, presence.NoUpdateCheck = raw["no_update_check"]
	_, presence.CleanupOnApprove = raw["cleanup_on_approve"]

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
	if projectPresence.NoOpen {
		merged.NoOpen = project.NoOpen
	}
	if project.ShareURL != "" {
		merged.ShareURL = project.ShareURL
	}
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
	if projectPresence.NoIntegrationCheck {
		merged.NoIntegrationCheck = project.NoIntegrationCheck
	}
	if projectPresence.NoUpdateCheck {
		merged.NoUpdateCheck = project.NoUpdateCheck
	}
	if projectPresence.CleanupOnApprove {
		merged.CleanupOnApprove = project.CleanupOnApprove
	}
	// auth_token is global-only (like agent_cmd) — project config cannot override
	// Union ignore patterns
	merged.IgnorePatterns = append(merged.IgnorePatterns, project.IgnorePatterns...)
	return merged
}

// LoadConfig loads and merges configuration from all sources.
// projectDir is the repo root (or cwd if not in a git repo).
// Runtime defaults (share_url, ignore_patterns) are applied when no config
// file explicitly sets those fields. To disable defaults, set them to
// empty values in a config file (e.g. "share_url": "", "ignore_patterns": []).
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

	// 4. Apply runtime defaults for fields not explicitly set in any config file
	if !globalPresence.ShareURL && !projectPresence.ShareURL {
		merged.ShareURL = "https://crit.md"
	}
	if !globalPresence.IgnorePatterns && !projectPresence.IgnorePatterns {
		merged.IgnorePatterns = []string{".crit/"}
	}

	// 5. Fall back to git user.name if no author configured
	if merged.Author == "" {
		if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
			merged.Author = strings.TrimSpace(string(out))
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
func saveGlobalConfig(apply func(m map[string]json.RawMessage) error) error {
	path := globalConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

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
	return os.WriteFile(path, data, 0o600)
}

// matchPattern checks if a file path matches an ignore pattern.
// Pattern types:
//   - "*.ext"         → matches files ending in .ext anywhere
//   - "dir/"          → matches all files under dir/
//   - "exact.file"    → matches filename anywhere in tree
//   - "path/*.ext"    → filepath.Match against full path
func matchPattern(pattern, path string) bool {
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
