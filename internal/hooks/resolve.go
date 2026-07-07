// Package hooks resolves and executes user-defined shell commands at Crit
// finish lifecycle points — the executable counterpart to internal/prompt's
// text templates.
//
// A hook value uses the same inline:/file: forms as a prompt value but resolves
// to an executable instead of template text:
//
//   - inline:<command>  → run `sh -c "<command>"`
//   - file:<path>       → execute the script at <path> directly (shebang + exec bit)
//
// Hooks are keyed exactly like prompt templates (on_finish_unresolved /
// on_finish_approved, optionally mode-suffixed :files/:diff/:live/:preview).
// Project-level hooks run arbitrary code and are gated by the same trust flow
// as project prompts (see internal/prompt/trust.go).
package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

// Subdir is the conventional on-disk directory for discovered hook scripts,
// parallel to .crit/prompts for prompt templates.
const Subdir = ".crit/hooks"

// Hook script filenames use a leading dot (not colon) for the mode separator,
// mirroring prompt filenames: on_finish_unresolved:diff → on_finish_unresolved.diff.sh
//
// .sh is the suffix used by DISCOVERED files (.crit/hooks/on_finish_*.sh) and
// is required for the conventional lookup — the discoverer needs a deterministic
// filename pattern. file: config values point at any executable and are run
// directly via its shebang, so .py / .ts / .fish / etc. work via the config
// form without changing this suffix.
const (
	PrefixInline = "inline:"
	PrefixFile   = "file:"
	fileSuffix   = ".sh"
)

// Layer re-exports prompt.Layer so callers don't need to import both packages
// for the global/project distinction.
type Layer = prompt.Layer

const (
	LayerGlobal  = prompt.LayerGlobal
	LayerProject = prompt.LayerProject
)

// ExecutableCommand is a resolved hook ready to run.
type ExecutableCommand struct {
	// Kind is "inline" (sh -c string) or "file" (direct exec of a script path).
	Kind string
	// Arg is the command string (inline) or the resolved absolute script path (file).
	Arg    string
	Hook   string // resolved key, e.g. on_finish_unresolved:diff
	Source string // provenance label, e.g. project:.crit/hooks/on_finish_unresolved.diff.sh
	Layer  Layer
}

// Filenames returns conventional on-disk names for hook+mode (mode-specific first).
func Filenames(hook, mode string) []string {
	specific, generic := prompt.ResolveHookKey(hook, mode)
	var out []string
	if specific != generic {
		out = append(out, hookKeyToFilename(specific))
	}
	out = append(out, hookKeyToFilename(generic))
	return out
}

func hookKeyToFilename(key string) string {
	return strings.ReplaceAll(key, ":", ".") + fileSuffix
}

// DiscoverFile loads the first matching conventional hook script under
// baseDir/.crit/hooks/. Returns the resolved command, its source label, and ok.
func DiscoverFile(baseDir, hook, mode string, layer Layer) (cmd ExecutableCommand, ok bool) {
	dir := filepath.Join(baseDir, Subdir)
	for _, name := range Filenames(hook, mode) {
		path := filepath.Join(dir, name)
		abs := path
		if a, err := filepath.Abs(path); err == nil {
			abs = a
		}
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		source := string(layer) + ":" + filepath.ToSlash(filepath.Join(Subdir, name))
		return ExecutableCommand{
			Kind:   "file",
			Arg:    abs,
			Hook:   modeSpecificKey(hook, mode),
			Source: source,
			Layer:  layer,
		}, true
	}
	return ExecutableCommand{}, false
}

func modeSpecificKey(hook, mode string) string {
	specific, _ := prompt.ResolveHookKey(hook, mode)
	return specific
}

// LoadCommand resolves a config value (inline:/file:) to an ExecutableCommand.
// baseDir is the directory for relative file: paths (project root or home).
// The returned Hook/Source/Layer are zero — ResolveFinishCommand fills them.
func LoadCommand(value, baseDir string) (ExecutableCommand, error) {
	switch {
	case strings.HasPrefix(value, PrefixInline):
		return ExecutableCommand{Kind: "inline", Arg: strings.TrimPrefix(value, PrefixInline)}, nil
	case strings.HasPrefix(value, PrefixFile):
		path := strings.TrimPrefix(value, PrefixFile)
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		abs := path
		if a, err := filepath.Abs(path); err == nil {
			abs = a
		}
		if _, err := os.Stat(abs); err != nil {
			return ExecutableCommand{}, fmt.Errorf("hook script %s: %w", abs, err)
		}
		return ExecutableCommand{Kind: "file", Arg: abs}, nil
	default:
		return ExecutableCommand{}, fmt.Errorf("hook value must start with %q or %q", PrefixInline, PrefixFile)
	}
}

// ResolveFinishCommand picks the effective executable command for a finish hook.
// Resolution order mirrors prompt.ResolveFinishTemplate:
//
//  1. Project config map (when useProject)
//  2. Global config map
//  3. Project discovered .crit/hooks/*.sh (when useProject)
//  4. Global discovered ~/.crit/hooks/*.sh
//
// There are no stock command hooks — returns nil when nothing is configured.
func ResolveFinishCommand(globalHooks, projectHooks map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (*ExecutableCommand, error) {
	if v, key := prompt.LookupPrompt(projectHooks, hook, mode); v != "" && useProject {
		ec, err := LoadCommand(v, projectDir)
		if err != nil {
			return nil, fmt.Errorf("loading project hook %s: %w", key, err)
		}
		ec.Hook = key
		ec.Source = commandSource(string(LayerProject), v)
		ec.Layer = LayerProject
		return &ec, nil
	}
	if v, key := prompt.LookupPrompt(globalHooks, hook, mode); v != "" {
		ec, err := LoadCommand(v, homeDir)
		if err != nil {
			return nil, fmt.Errorf("loading global hook %s: %w", key, err)
		}
		ec.Hook = key
		ec.Source = commandSource(string(LayerGlobal), v)
		ec.Layer = LayerGlobal
		return &ec, nil
	}
	if useProject {
		if ec, ok := DiscoverFile(projectDir, hook, mode, LayerProject); ok {
			return &ec, nil
		}
	}
	if ec, ok := DiscoverFile(homeDir, hook, mode, LayerGlobal); ok {
		return &ec, nil
	}
	return nil, nil
}

// commandSource describes where a config-driven hook came from (file: → path; inline → inline).
func commandSource(layer, value string) string {
	if strings.HasPrefix(value, PrefixFile) {
		return layer + ":" + strings.TrimPrefix(value, PrefixFile)
	}
	return layer + ":inline"
}

// Note: this file intentionally does not reference runtime.GOOS.
// Discovered project hook files and source labels are enumerated by the
// prompt package's trust gate (internal/prompt/trust.go) so that the trust
// hash covers both prompts and hooks without an import cycle
// (internal/hooks imports internal/prompt for resolution helpers).
