package main

//go:generate go run gen_integration_hashes.go

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// computeFileHash returns the hex-encoded SHA256 hash of data.
func computeFileHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// latestCacheDir returns the lexicographically last subdirectory name
// inside dir, or "" if dir doesn't exist or has no subdirectories.
// Version directories sort correctly by string comparison (e.g. "1.0.1" > "1.0.0").
func latestCacheDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latest string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() > latest {
			latest = entry.Name()
		}
	}
	return latest
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
	hash     string // expected (bundled) content hash for this integration source
}

// toolDirFromDest extracts the tool config directory from a dest path
// (e.g. ".claude/skills/crit/SKILL.md" → ".claude").
func toolDirFromDest(dest string) string {
	return strings.SplitN(dest, "/", 2)[0]
}

// marketplaceUpdateHint returns tool-specific advice for updating a marketplace plugin.
var marketplaceUpdateHints = map[string]string{
	".claude": "claude plugin marketplace update crit\nclaude plugin update crit@crit",
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

// integrationStatus describes a detected integration and whether it is current.
type integrationStatus struct {
	Agent    string `json:"agent"`
	Status   string `json:"status"`   // "current" or "stale"
	Location string `json:"location"` // "project", "home", "marketplace", "cache"
	Hint     string `json:"hint"`     // update hint (stale only)
	Hash     string `json:"hash,omitempty"`
}

// detectInstalledIntegrations scans all candidate paths for each agent
// and returns the status of every agent that has at least one file installed.
// Unlike checkInstalledIntegrations (which only returns stale files),
// this reports both current and stale agents.
func detectInstalledIntegrations(projectDir, homeDir string) []integrationStatus {
	var results []integrationStatus
	seen := make(map[string]bool)

	agents := make([]string, 0, len(integrationMap))
	for agent := range integrationMap {
		agents = append(agents, agent)
	}
	sort.Strings(agents)

	for _, agent := range agents {
		if seen[agent] {
			continue
		}
		files := integrationMap[agent]
		for _, f := range files {
			expectedHash, ok := integrationHashes[f.source]
			if !ok {
				continue
			}

			candidates := buildCandidates(f, agent, projectDir, homeDir)

			for _, c := range candidates {
				installed, err := os.ReadFile(c.path)
				if err != nil {
					continue
				}
				status := "current"
				hint := ""
				if computeFileHash(installed) != expectedHash {
					status = "stale"
					sf := staleFile{agent: agent, file: filepath.Base(f.dest), dest: c.path, location: c.location, hash: expectedHash}
					hint = sf.updateHint()
				}
				results = append(results, integrationStatus{
					Agent:    agent,
					Status:   status,
					Location: c.location,
					Hint:     hint,
					Hash:     expectedHash,
				})
				seen[agent] = true
				break // first found file per agent is enough
			}
			if seen[agent] {
				break // found this agent, move to next
			}
		}
	}
	return results
}

// candidate is a path + location pair for integration file lookup.
type candidate struct {
	path     string
	location string
}

// buildCandidates returns the list of candidate paths to check for an integration file.
func buildCandidates(f integration, agent, projectDir, homeDir string) []candidate {
	candidates := []candidate{
		{filepath.Join(projectDir, f.dest), locationProject},
		{filepath.Join(homeDir, f.dest), locationHome},
	}

	toolDir := toolDirFromDest(f.dest)
	marketplacePath := filepath.Join(homeDir, toolDir, "plugins", "marketplaces", "crit", f.source)
	candidates = append(candidates, candidate{marketplacePath, locationMarketplace})

	agentPrefix := fmt.Sprintf("integrations/%s/", agent)
	if strings.HasPrefix(f.source, agentPrefix) {
		relPath := strings.TrimPrefix(f.source, agentPrefix)
		cacheBase := filepath.Join(homeDir, toolDir, "plugins", "cache", "crit", "crit")
		if latest := latestCacheDir(cacheBase); latest != "" {
			cachePath := filepath.Join(cacheBase, latest, relPath)
			candidates = append(candidates, candidate{cachePath, locationCache})
		}
	}

	return candidates
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

			candidates := buildCandidates(f, agent, projectDir, homeDir)

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
						hash:     expectedHash,
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
		fmt.Fprintf(os.Stderr, "Note: %s integration outdated (%s). %s\n", s.agent, s.dest, strings.ReplaceAll(s.updateHint(), "|", ": "))
	}
	return len(seen)
}

// agentProbe describes how to detect whether an AI coding tool is present on
// the system. Checked in order: CLI binary on PATH, then config directory in
// $HOME. For ambiguous binary names (e.g. "pi", "gemini") that collide with
// unrelated tools, versionMatch requires a secondary --version output check.
type agentProbe struct {
	agent        string   // integration name (key in integrationMap)
	bins         []string // CLI binary names to look for on PATH
	homeDirs     []string // directories relative to $HOME whose existence signals the agent
	versionMatch string   // if set, --version output must contain this substring (case-insensitive)
}

// versionTimeout is the maximum time to wait for a binary's --version output.
var versionTimeout = 2 * time.Second

var agentProbes = []agentProbe{
	{"claude-code", []string{"claude"}, []string{".claude"}, ""},
	{"cursor", []string{"cursor"}, []string{".cursor"}, ""},
	{"windsurf", []string{"windsurf"}, []string{".windsurf"}, ""},
	{"github-copilot", []string{"github-copilot"}, []string{".config/github-copilot"}, ""},
	{"cline", nil, []string{".cline"}, ""},
	{"codex", []string{"codex"}, nil, ""},
	{"opencode", []string{"opencode"}, []string{".opencode"}, ""},
	{"aider", []string{"aider"}, nil, ""},
	{"qwen", []string{"qwen"}, []string{".qwen"}, ""},
	// Ambiguous names: secondary version check prevents false positives from
	// Raspberry Pi utils, Meta Hermes JS engine, Gemini protocol clients, etc.
	// NOTE: match strings are best guesses — update if the real AI tool output differs.
	{"pi", []string{"pi"}, []string{".pi"}, "inflection"},
	{"hermes", []string{"hermes"}, []string{".hermes"}, "hermes-ai"},
	{"gemini", []string{"gemini"}, []string{".gemini"}, "google"},
	{"grok", []string{"grok"}, []string{".grok"}, "xai"},
}

// detectPresentAgents returns the names of AI coding tools that appear to be
// installed on the system (binary on PATH or config dir in $HOME).
func detectPresentAgents(homeDir string) []string {
	var present []string
	seen := make(map[string]bool)
	for _, p := range agentProbes {
		if seen[p.agent] {
			continue
		}
		for _, bin := range p.bins {
			if _, err := exec.LookPath(bin); err == nil {
				if p.versionMatch == "" || confirmBinaryVersion(bin, p.versionMatch) {
					present = append(present, p.agent)
					seen[p.agent] = true
				}
				break
			}
		}
		if seen[p.agent] {
			continue
		}
		for _, dir := range p.homeDirs {
			if fi, err := os.Stat(filepath.Join(homeDir, dir)); err == nil && fi.IsDir() {
				present = append(present, p.agent)
				seen[p.agent] = true
				break
			}
		}
	}
	return present
}

// confirmBinaryVersion runs "<bin> --version" with a short timeout and checks
// whether the output contains the expected substring (case-insensitive). This
// prevents false positives from unrelated binaries that share an ambiguous name.
// Returns false if the binary doesn't support --version, times out, or the
// output doesn't contain the expected string.
func confirmBinaryVersion(bin, expected string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(expected))
}

// installedAgents returns the set of agents that have at least one crit
// integration file installed (project-local, home, marketplace, or cache),
// plus aider if its conventions file exists.
func installedAgents(projectDir, homeDir string) map[string]bool {
	installed := make(map[string]bool)
	for _, s := range detectInstalledIntegrations(projectDir, homeDir) {
		installed[s.Agent] = true
	}
	paths := aiderPaths(projectDir, homeDir)
	if _, err := os.Stat(paths.conventionsDest); err == nil {
		installed["aider"] = true
	}
	return installed
}

// checkMissingIntegrations returns agents that are present on the system but
// have no crit integration installed (project-local or home).
func checkMissingIntegrations(projectDir, homeDir string) []string {
	present := detectPresentAgents(homeDir)
	if len(present) == 0 {
		return nil
	}

	installed := installedAgents(projectDir, homeDir)

	var missing []string
	for _, agent := range present {
		if !installed[agent] {
			missing = append(missing, agent)
		}
	}
	return missing
}

// printMissingHints prints a suggestion for each detected-but-not-installed
// agent. Returns the number of hints printed.
func printMissingHints(missing []string) int {
	if len(missing) == 0 {
		return 0
	}
	if len(missing) == 1 {
		fmt.Fprintf(os.Stderr, "Tip: %s detected but crit integration not installed.\n", missing[0])
		fmt.Fprintf(os.Stderr, "     Run: crit install %s\n", missing[0])
	} else {
		fmt.Fprintf(os.Stderr, "Tip: detected AI tools without crit integration: %s\n", strings.Join(missing, ", "))
		fmt.Fprintf(os.Stderr, "     Run: crit install all  (or crit install <agent> for a specific one)\n")
	}
	fmt.Fprintf(os.Stderr, "     Disable: CRIT_NO_INTEGRATION_CHECK=1\n")
	return len(missing)
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
	missing := checkMissingIntegrations(cwd, home)

	if len(stale) == 0 && len(missing) == 0 {
		fmt.Fprintln(os.Stderr, "All installed integrations are up to date.")
		return
	}

	if len(stale) > 0 {
		// Deduplicate by hint — show each unique update action only once
		seenHints := make(map[string]bool)
		for _, s := range stale {
			hint := s.updateHint()
			if seenHints[hint] {
				continue
			}
			seenHints[hint] = true
			fmt.Fprintf(os.Stderr, "  outdated: %s\n", s.dest)
			termHint := strings.ReplaceAll(hint, "|", ": ")
			fmt.Fprintf(os.Stderr, "    → %s\n", termHint)
		}
	}

	if len(missing) > 0 {
		if len(stale) > 0 {
			fmt.Fprintln(os.Stderr)
		}
		printMissingHints(missing)
	}
}
