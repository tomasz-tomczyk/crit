package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// LoadConfigForCommands loads merged config and anchors a relative configured
// output to the project/config directory. Explicit CLI outputs are normalized
// later by review-path resolution and remain relative to the invoking CWD.
func LoadConfigForCommands(configDir string) Config {
	cfg := LoadConfig(configDir)
	if cfg.Output != "" && !filepath.IsAbs(cfg.Output) {
		cfg.Output = filepath.Join(configDir, cfg.Output)
	}
	return cfg
}

// LoadCurrentConfig loads merged global and project config for the current
// repository, falling back to the current directory outside a repository.
func LoadCurrentConfig() (Config, error) {
	configDir, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	if v := vcs.DetectVCS(""); v != nil {
		if root, rootErr := v.RepoRoot(); rootErr == nil {
			configDir = root
		}
	}
	return LoadConfigForCommands(configDir), nil
}

// ResolveOutputDir returns the explicit CLI/plan output when set, otherwise
// the output from merged config.
func ResolveOutputDir(explicit string, cfg Config) string {
	if explicit != "" {
		return explicit
	}
	return cfg.Output
}

// ResolvePort returns the effective requested port or port range
// (flag > env > config). An invalid CRIT_PORT is ignored with a warning.
func ResolvePort(flagPort, cfgPort PortRange) PortRange {
	if !flagPort.IsZero() {
		return flagPort
	}
	if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
		p, err := ParsePortRange(envPort)
		if err == nil {
			return p
		}
		fmt.Fprintf(os.Stderr, "Warning: ignoring CRIT_PORT: %v\n", err)
	}
	return cfgPort
}

// ResolveHost returns the effective listen host (flag > env > config).
func ResolveHost(flagHost, cfgHost string) string {
	if flagHost != "" {
		return flagHost
	}
	if envHost := os.Getenv("CRIT_HOST"); envHost != "" {
		return envHost
	}
	return cfgHost
}

// ResolveShareURL returns the effective share service URL.
func ResolveShareURL(flagValue string, cfg Config, fallback string) string {
	if flagValue != "" {
		return flagValue
	}
	if envShare, ok := os.LookupEnv("CRIT_SHARE_URL"); ok {
		return envShare
	}
	if cfg.ShareURL != "" {
		return cfg.ShareURL
	}
	return fallback
}
