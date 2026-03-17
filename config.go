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
	Port           int      `json:"port,omitempty"`
	NoOpen         bool     `json:"no_open,omitempty"`
	ShareURL       string   `json:"share_url,omitempty"`
	Quiet          bool     `json:"quiet,omitempty"`
	Output         string   `json:"output,omitempty"`
	Author         string   `json:"author,omitempty"`
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
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
		Port:     0,
		NoOpen:   false,
		ShareURL: "https://crit.live",
		Quiet:    false,
		Output:   "",
		Author:   "",
		IgnorePatterns: []string{
			"*.lock",
			"*.min.js",
			"*.min.css",
			".crit.json",
		},
	}
}

// generatedConfig is like Config but without omitempty, so all keys appear in output.
type generatedConfig struct {
	Port           int      `json:"port"`
	NoOpen         bool     `json:"no_open"`
	ShareURL       string   `json:"share_url"`
	Quiet          bool     `json:"quiet"`
	Output         string   `json:"output"`
	Author         string   `json:"author"`
	IgnorePatterns []string `json:"ignore_patterns"`
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
	ShareURL       bool
	IgnorePatterns bool
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

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, presence, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, presence, nil
}

// mergeConfigs merges project config on top of global config.
// Non-zero project values override global. Ignore patterns are unioned.
func mergeConfigs(global, project Config) Config {
	merged := global
	if project.Port != 0 {
		merged.Port = project.Port
	}
	if project.NoOpen {
		merged.NoOpen = true
	}
	if project.ShareURL != "" {
		merged.ShareURL = project.ShareURL
	}
	if project.Quiet {
		merged.Quiet = true
	}
	if project.Output != "" {
		merged.Output = project.Output
	}
	if project.Author != "" {
		merged.Author = project.Author
	}
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
	merged := mergeConfigs(global, project)

	// 4. Apply runtime defaults for fields not explicitly set in any config file
	if !globalPresence.ShareURL && !projectPresence.ShareURL {
		merged.ShareURL = "https://crit.live"
	}
	if !globalPresence.IgnorePatterns && !projectPresence.IgnorePatterns {
		merged.IgnorePatterns = []string{".crit.json"}
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
// Currently exercised only by tests, but kept as a utility parallel to filterIgnored.
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
