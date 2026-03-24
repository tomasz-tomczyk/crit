package main

//go:generate go run gen_integration_hashes.go

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// toolDirFromDest extracts the tool config directory from a dest path
// (e.g. ".claude/commands/crit.md" → ".claude").
func toolDirFromDest(dest string) string {
	return strings.SplitN(dest, "/", 2)[0]
}

// marketplaceUpdateHint returns tool-specific advice for updating a marketplace plugin.
var marketplaceUpdateHints = map[string]string{
	".claude": "In Claude Code: /plugin marketplace update crit\n    Then run: claude plugin update crit@crit",
	".cursor": "Update the crit plugin in Cursor settings",
}

// updateHint returns location-specific advice for how to fix this stale file.
func (s staleFile) updateHint() string {
	switch s.location {
	case locationProject:
		return fmt.Sprintf("Run: crit install %s --force", s.agent)
	case locationHome:
		return fmt.Sprintf("Run: cd ~ && crit install %s --force", s.agent)
	case locationMarketplace, locationCache:
		// Find the tool dir from the integration's dest path
		if files, ok := integrationMap[s.agent]; ok && len(files) > 0 {
			toolDir := toolDirFromDest(files[0].dest)
			if hint, ok := marketplaceUpdateHints[toolDir]; ok {
				return hint
			}
		}
		return "Update the crit plugin in your editor settings"
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

	// Sort agents for deterministic output order
	agents := make([]string, 0, len(integrationMap))
	for agent := range integrationMap {
		agents = append(agents, agent)
	}
	sort.Strings(agents)

	for _, agent := range agents {
		files := integrationMap[agent]
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
			toolDir := toolDirFromDest(f.dest)

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

// printStaleWarnings prints location-specific warnings for stale integrations
// to stderr. Returns the number of unique warnings printed.
func printStaleWarnings(stale []staleFile) int {
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

// runCheck implements the "crit check" subcommand.
func runCheck() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "crit %s — checking installed integrations...\n\n", version)

	stale := checkInstalledIntegrations(cwd, home)

	if len(stale) == 0 {
		fmt.Fprintln(os.Stderr, "All installed integrations are up to date.")
		return
	}

	// Deduplicate by hint — show each unique update action only once
	seenHints := make(map[string]bool)
	for _, s := range stale {
		hint := s.updateHint()
		if seenHints[hint] {
			continue
		}
		seenHints[hint] = true
		fmt.Fprintf(os.Stderr, "  outdated: %s\n", s.dest)
		fmt.Fprintf(os.Stderr, "    → %s\n\n", hint)
	}
}
