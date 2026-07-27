package config

import (
	"os"
	"strconv"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

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
	return LoadConfig(configDir), nil
}

// ResolveOutputDir returns the explicit CLI/plan output when set, otherwise
// the output from merged config.
func ResolveOutputDir(explicit string, cfg Config) string {
	if explicit != "" {
		return explicit
	}
	return cfg.Output
}

// ResolvePort returns the effective listen port (flag > env > config).
func ResolvePort(flagPort, cfgPort int) int {
	if flagPort != 0 {
		return flagPort
	}
	if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			return p
		}
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
