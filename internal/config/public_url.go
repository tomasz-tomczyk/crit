package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// ResolvePublicURL returns the effective advertised base URL (flag > env > config).
// Unlike share_url there is no runtime default — empty means use the listen address.
func ResolvePublicURL(flagValue string, cfg Config) string {
	if flagValue != "" {
		return flagValue
	}
	if envPublic, ok := os.LookupEnv("CRIT_PUBLIC_URL"); ok {
		return envPublic
	}
	return cfg.PublicURL
}

// NormalizePublicURL validates and normalizes a user-facing base URL for stderr,
// browser-open, and reverse-proxy access (e.g. tailscale serve). Trailing slashes
// are stripped. Scheme must be http or https; host is required. An optional path
// prefix is preserved (e.g. https://host.example/design).
func NormalizePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid public URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid public URL %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid public URL %q: missing host", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("invalid public URL %q: userinfo is not allowed", raw)
	}
	hostname := u.Hostname()
	if hostname == "localhost" {
		return "", fmt.Errorf("invalid public URL %q: loopback host is not allowed", raw)
	}
	if ip := net.ParseIP(hostname); ip != nil && ip.IsLoopback() {
		return "", fmt.Errorf("invalid public URL %q: loopback host is not allowed", raw)
	}
	normalized := u.Scheme + "://" + u.Host + strings.TrimSuffix(u.EscapedPath(), "/")
	return normalized, nil
}

// PublicURLHost returns the hostname from a normalized public URL for Host-header
// validation when the daemon is reached through a reverse proxy.
func PublicURLHost(publicURL string) string {
	if publicURL == "" {
		return ""
	}
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if host == "" || host == "localhost" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return host
}
