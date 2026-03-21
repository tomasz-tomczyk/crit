package main

//go:generate go run gen_integration_hashes.go

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// computeFileHash returns the hex-encoded SHA256 hash of data.
func computeFileHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// location describes where a stale file was found, determining the update advice.
const (
	locationProject     = "project"     // ./  (crit install)
	locationHome        = "home"        // ~/  (crit install from home)
	locationMarketplace = "marketplace" // ~/.claude/plugins/marketplaces/crit/
	locationCache       = "cache"       // ~/.claude/plugins/cache/crit/
)

type staleFile struct {
	agent    string // e.g. "claude-code"
	file     string // source file name
	dest     string // absolute path where the stale file was found
	location string // one of the location* constants
}

// updateHint returns location-specific advice for how to fix this stale file.
func (s staleFile) updateHint() string {
	switch s.location {
	case locationProject:
		return fmt.Sprintf("Run: crit install %s --force", s.agent)
	case locationHome:
		return fmt.Sprintf("Run: cd ~ && crit install %s --force", s.agent)
	case locationMarketplace:
		return fmt.Sprintf("Update plugin: cd %s && git pull", filepath.Dir(filepath.Dir(s.dest)))
	case locationCache:
		// Navigate up to the cache root for this tool
		// dest is like ~/.cursor/plugins/cache/crit/crit/<hash>/commands/crit.md
		// We want ~/.cursor/plugins/cache/crit
		parts := strings.Split(s.dest, string(filepath.Separator))
		for i, p := range parts {
			if p == "cache" && i+1 < len(parts) && parts[i+1] == "crit" {
				cacheRoot := filepath.Join(parts[:i+2]...)
				if !filepath.IsAbs(cacheRoot) {
					cacheRoot = "/" + cacheRoot
				}
				return fmt.Sprintf("Clear cache: rm -rf %s", cacheRoot)
			}
		}
		return "Clear plugin cache and restart"
	default:
		return fmt.Sprintf("Run: crit install %s --force", s.agent)
	}
}

// checkInstalledIntegrations scans known integration destinations for files
// that exist but differ from the precomputed hash in integrationHashes.
// Checks four location types: project-local, home dir, marketplace source,
// and marketplace cache. Missing files are silently skipped.
func checkInstalledIntegrations(projectDir, homeDir string) []staleFile {
	var results []staleFile

	for agent, files := range integrationMap {
		for _, f := range files {
			expectedHash, ok := integrationHashes[f.source]
			if !ok {
				continue
			}

			// Build candidates: path + location type
			type candidate struct {
				path     string
				location string
			}
			candidates := []candidate{
				{filepath.Join(projectDir, f.dest), locationProject},
				{filepath.Join(homeDir, f.dest), locationHome},
			}

			// Derive tool config dir from dest prefix (e.g. ".claude/commands/crit.md" -> ".claude")
			toolDir := strings.SplitN(f.dest, "/", 2)[0] // ".claude", ".cursor", ".opencode", etc.

			// Marketplace source: ~/<toolDir>/plugins/marketplaces/crit/<f.source>
			marketplacePath := filepath.Join(homeDir, toolDir, "plugins", "marketplaces", "crit", f.source)
			candidates = append(candidates, candidate{marketplacePath, locationMarketplace})

			// Marketplace cache: ~/<toolDir>/plugins/cache/crit/crit/*/<plugin-relative-path>
			agentPrefix := fmt.Sprintf("integrations/%s/", agent)
			if strings.HasPrefix(f.source, agentPrefix) {
				relPath := strings.TrimPrefix(f.source, agentPrefix)
				cacheBase := filepath.Join(homeDir, toolDir, "plugins", "cache", "crit", "crit")
				if entries, err := os.ReadDir(cacheBase); err == nil {
					for _, entry := range entries {
						if entry.IsDir() {
							cachePath := filepath.Join(cacheBase, entry.Name(), relPath)
							candidates = append(candidates, candidate{cachePath, locationCache})
						}
					}
				}
			}

			for _, c := range candidates {
				installed, err := os.ReadFile(c.path)
				if err != nil {
					continue
				}
				if computeFileHash(installed) != expectedHash {
					results = append(results, staleFile{
						agent:    agent,
						file:     filepath.Base(f.dest),
						dest:     c.path,
						location: c.location,
					})
				}
			}
		}
	}
	return results
}

// printIntegrationWarnings checks for stale integrations and prints
// location-specific warnings to stderr. Returns the number of stale files found.
func printIntegrationWarnings(projectDir, homeDir string) int {
	stale := checkInstalledIntegrations(projectDir, homeDir)
	if len(stale) == 0 {
		return 0
	}

	// Deduplicate by agent+location — one warning per unique combo
	type key struct{ agent, location string }
	seen := make(map[key]bool)
	for _, s := range stale {
		k := key{s.agent, s.location}
		if seen[k] {
			continue
		}
		seen[k] = true
		fmt.Fprintf(os.Stderr, "Note: %s integration outdated (%s). %s\n", s.agent, s.dest, s.updateHint())
	}
	return len(seen)
}
