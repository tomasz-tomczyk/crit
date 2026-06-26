package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

const promptsSubdir = ".crit/prompts"

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

// ListDiscoveredProjectPromptFiles returns on_finish_*.md paths under project/.crit/prompts/.
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
		if !strings.HasPrefix(e.Name(), "on_finish_") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}
