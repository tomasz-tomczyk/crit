package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		ShareURL: "",
		Quiet:    false,
		Output:   "",
		IgnorePatterns: []string{
			"*.lock",
			"*.min.js",
			"*.min.css",
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
	IgnorePatterns []string `json:"ignore_patterns"`
}

func (c generatedConfig) String() string {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data) + "\n"
}

// loadConfigFile reads and parses a single JSON config file.
// Returns a zero Config if the file doesn't exist.
func loadConfigFile(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
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
	// Union ignore patterns
	merged.IgnorePatterns = append(merged.IgnorePatterns, project.IgnorePatterns...)
	return merged
}

// LoadConfig loads and merges configuration from all sources.
// projectDir is the repo root (or cwd if not in a git repo).
func LoadConfig(projectDir string) Config {
	// 1. Global config
	global, err := loadConfigFile(globalConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: reading global config: %v\n", err)
	}

	// 2. Project config
	projectConfigPath := filepath.Join(projectDir, ".crit.config.json")
	project, err := loadConfigFile(projectConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: reading project config: %v\n", err)
	}

	// 3. Merge global + project
	return mergeConfigs(global, project)
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
