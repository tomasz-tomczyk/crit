package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stringSliceFlag collects repeated --cookie values.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, "; ") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// resolveLiveCookies merges cookie values from CLI flags and config (global +
// project). Precedence: inline flags and explicit --cookie-file override config
// file paths, but all provided sources are merged (later duplicates are left to
// the upstream). Relative cookie file paths resolve against configDir.
func resolveLiveCookies(flagCookies []string, flagCookieFile string, cfg Config, configDir string) (string, error) {
	var parts []string
	parts = append(parts, flagCookies...)
	if cfg.LiveCookie != "" {
		parts = append(parts, cfg.LiveCookie)
	}

	filePath := flagCookieFile
	if filePath == "" {
		filePath = cfg.LiveCookieFile
	}
	if filePath != "" {
		filePath = resolveCookieFilePath(filePath, configDir)
		fromFile, err := readLiveCookieFile(filePath)
		if err != nil {
			return "", err
		}
		if fromFile != "" {
			parts = append(parts, fromFile)
		}
	}

	return joinCookieHeader(parts), nil
}

func resolveCookieFilePath(path, configDir string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if configDir == "" {
		return path
	}
	return filepath.Join(configDir, path)
}

func joinCookieHeader(parts []string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "; ")
}

// readLiveCookieFile reads cookies from a file. Supports:
//   - a raw Cookie header value (one line or multiple lines joined)
//   - Netscape cookie jar lines (tab-separated); name/value columns are used
//   - comment lines starting with # (ignored)
func readLiveCookieFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading cookie file %q: %w", path, err)
	}
	var parts []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, value, ok := parseNetscapeCookieLine(line); ok {
			parts = append(parts, name+"="+value)
			continue
		}
		parts = append(parts, line)
	}
	return joinCookieHeader(parts), nil
}

func parseNetscapeCookieLine(line string) (name, value string, ok bool) {
	fields := strings.Split(line, "\t")
	if len(fields) != 7 {
		return "", "", false
	}
	name = strings.TrimSpace(fields[5])
	value = strings.TrimSpace(fields[6])
	if name == "" {
		return "", "", false
	}
	return name, value, true
}
