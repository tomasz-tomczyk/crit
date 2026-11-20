package prompt

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/userdirs"
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

// DiscoveryDir returns the directory holding conventional files for a layer
// and the form of it used in source labels. For the project layer baseDir is
// the project root and the directory is baseDir/<subdir>. For the global layer
// baseDir is the home directory and globalDir is the resolved user directory
// (XDG or legacy ~/.crit/...); its label is relative to home when possible.
func DiscoveryDir(baseDir, subdir, globalDir string, layer Layer) (dir, label string) {
	if layer != LayerGlobal {
		return filepath.Join(baseDir, subdir), subdir
	}
	if rel, err := filepath.Rel(baseDir, globalDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return globalDir, rel
	}
	return globalDir, globalDir
}

func promptDir(baseDir string, layer Layer) (dir, label string) {
	return DiscoveryDir(baseDir, promptsSubdir, userdirs.PromptsDir(baseDir), layer)
}

// DiscoverPromptFile loads the first matching conventional prompt file. For
// the project layer baseDir is the project root (baseDir/.crit/prompts/); for
// the global layer it is the home directory and the user's prompts directory
// is used (~/.config/crit/prompts/ or the legacy ~/.crit/prompts/).
func DiscoverPromptFile(baseDir, hook, mode string, layer Layer) (text, sourceLabel string, ok bool) {
	dir, labelDir := promptDir(baseDir, layer)
	for _, name := range PromptFilenames(hook, mode) {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		label := string(layer) + ":" + filepath.ToSlash(filepath.Join(labelDir, name))
		return string(data), label, true
	}
	return "", "", false
}

// DiscoverPromptFileSpecific loads exactly hook+mode from a conventional
// prompt file. Pass mode="" to load only the generic hook file.
func DiscoverPromptFileSpecific(baseDir, hook, mode string, layer Layer) (text, sourceLabel string, ok bool) {
	key := hook
	if mode != "" {
		key, _ = ResolveHookKey(hook, mode)
	}
	name := hookKeyToFilename(key)
	dir, labelDir := promptDir(baseDir, layer)
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", "", false
	}
	label := string(layer) + ":" + filepath.ToSlash(filepath.Join(labelDir, name))
	return string(data), label, true
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
