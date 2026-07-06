package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

const promptsSubdir = ".crit/prompts"

// hooksSubdir is the conventional on-disk directory for discovered hook
// scripts, parallel to promptsSubdir. Kept in the prompt package (alongside
// the trust gate) to avoid an import cycle with internal/hooks.
const hooksSubdir = ".crit/hooks"

// PromptFilenames returns conventional on-disk names for hook+mode (mode-specific first).
func PromptFilenames(hook, mode string) []string {
	specific, generic := ResolveHookKey(hook, mode)
	var out []string
	if specific != generic {
		out = append(out, hookKeyToFilename(specific))
	}
	out = append(out, hookKeyToFilename(generic))
	return out
}

func hookKeyToFilename(key string) string {
	return strings.ReplaceAll(key, ":", ".") + ".md"
}

// DiscoverPromptFile loads the first matching conventional prompt file under baseDir/.crit/prompts/.
func DiscoverPromptFile(baseDir, hook, mode string, layer Layer) (text, sourceLabel string, ok bool) {
	dir := filepath.Join(baseDir, promptsSubdir)
	for _, name := range PromptFilenames(hook, mode) {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		label := string(layer) + ":" + filepath.ToSlash(filepath.Join(promptsSubdir, name))
		return string(data), label, true
	}
	return "", "", false
}

// discoveredHookPrefixes are the conventional filename prefixes recognized
// under .crit/prompts/ for trust hashing and source listing. Extend this
// list when a new hook family is added.
var discoveredHookPrefixes = []string{"on_finish_", "on_story_"}

// ListDiscoveredProjectPromptFiles returns on_finish_*.md / on_story_*.md
// paths under project/.crit/prompts/.
func ListDiscoveredProjectPromptFiles(projectDir string) []string {
	dir := filepath.Join(projectDir, promptsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !hasAnyPrefix(e.Name(), discoveredHookPrefixes) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
