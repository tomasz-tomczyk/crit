package config

import (
	"os"
	"strconv"
)

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
