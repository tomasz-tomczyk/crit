package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func runInstall(args []string) {
	target := ""
	force := false
	for _, a := range args {
		switch {
		case a == "--force" || a == "-f":
			force = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", a)
			os.Exit(1)
		default:
			if target != "" {
				fmt.Fprintf(os.Stderr, "Error: only one agent name allowed (got %q and %q)\n", target, a)
				os.Exit(1)
			}
			target = a
		}
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit install <agent>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Available agents:")
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		fmt.Fprintln(os.Stderr, "  all")
		os.Exit(1)
	}

	if target == "all" {
		cwd := mustGetwd()
		home, _ := os.UserHomeDir()
		global := isGlobalInstall(cwd, home)
		hadErr := false
		for _, name := range availableIntegrations() {
			if name == "windsurf" && global {
				fmt.Fprintln(os.Stderr, "  Skipped: windsurf (no global install supported — run from a project)")
				continue
			}
			if err := installIntegration(name, force); err != nil {
				fmt.Fprintf(os.Stderr, "  Failed: %s: %v\n", name, err)
				hadErr = true
				continue
			}
		}
		if hadErr {
			os.Exit(1)
		}
		return
	}
	if err := installIntegration(target, force); err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		os.Exit(1)
	}
}

// globalDestKind selects how an integration's globalDest is interpreted.
type globalDestKind int

const (
	// globalDestNone means the integration has no separate global path —
	// use dest joined to home (the default for global installs).
	globalDestNone globalDestKind = iota
	// globalDestRelHome: globalDest is relative to $HOME.
	globalDestRelHome
	// globalDestDocuments: globalDest is relative to the platform Documents
	// directory (used by Cline).
	globalDestDocuments
	// globalDestAbsolute: globalDest is an absolute path used verbatim.
	globalDestAbsolute
)

type integration struct {
	source string // path inside integrations/ embed
	dest   string // destination relative to cwd
	hint   string // usage hint printed after install
	// globalDest, when set together with a non-zero globalDestKind, overrides
	// dest in global mode (cwd == $HOME). The kind determines how it's
	// resolved (see globalDestKind).
	globalDest     string
	globalDestKind globalDestKind
}

var integrationMap = map[string][]integration{
	"claude-code": {
		{source: "integrations/claude-code/skills/crit/SKILL.md", dest: ".claude/skills/crit/SKILL.md", hint: "Run /crit in Claude Code to start a review loop"},
		{source: "integrations/claude-code/skills/crit-cli/SKILL.md", dest: ".claude/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Claude Code agents when needed"},
	},
	"cursor": {
		{source: "integrations/cursor/skills/crit/SKILL.md", dest: ".cursor/skills/crit/SKILL.md", hint: "Run /crit in Cursor to start a review loop"},
		{source: "integrations/cursor/skills/crit-cli/SKILL.md", dest: ".cursor/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Cursor agents when needed"},
	},
	"opencode": {
		// command stays at ~/.opencode/commands/ globally (works there)
		{source: "integrations/opencode/crit.md", dest: ".opencode/commands/crit.md", hint: "Run /crit in OpenCode to start a review loop"},
		// opencode does NOT read ~/.opencode/skills/ globally — redirect to ~/.agents/skills/
		{source: "integrations/opencode/SKILL.md", dest: ".opencode/skills/crit/SKILL.md", globalDest: ".agents/skills/crit/SKILL.md", globalDestKind: globalDestRelHome, hint: "The crit skill is available to OpenCode agents when needed"},
		// Plugin file auto-loaded from project `.opencode/plugins/` or global
		// `~/.config/opencode/plugins/`. Conditionally injects sharing instructions
		// only when share_url is set in crit config.
		{source: "integrations/opencode/plugin/crit.ts", dest: ".opencode/plugins/crit.ts", globalDest: ".config/opencode/plugins/crit.ts", globalDestKind: globalDestRelHome, hint: "Crit's opencode plugin gates sharing instructions on share_url being set"},
	},
	"windsurf": {
		// windsurf has no per-tool global rules dir — global install rejected in installIntegration.
		{source: "integrations/windsurf/crit.md", dest: ".windsurf/rules/crit.md", hint: "Windsurf will suggest Crit when writing plans"},
	},
	"github-copilot": {
		// Copilot does NOT read ~/.github/skills/ globally — redirect to ~/.agents/skills/
		{source: "integrations/github-copilot/skills/crit/SKILL.md", dest: ".github/skills/crit/SKILL.md", globalDest: ".agents/skills/crit/SKILL.md", globalDestKind: globalDestRelHome, hint: "Run /crit in GitHub Copilot to start a review loop"},
		{source: "integrations/github-copilot/skills/crit-cli/SKILL.md", dest: ".github/skills/crit-cli/SKILL.md", globalDest: ".agents/skills/crit-cli/SKILL.md", globalDestKind: globalDestRelHome, hint: "The crit-cli skill is available to GitHub Copilot agents when needed"},
	},
	"cline": {
		// Cline does NOT read ~/.clinerules/ globally — redirect to platform Documents dir.
		{source: "integrations/cline/crit.md", dest: ".clinerules/crit.md", globalDest: "Cline/Rules/crit.md", globalDestKind: globalDestDocuments, hint: "Cline will suggest Crit when writing plans"},
	},
	"codex": {
		{source: "integrations/codex/skills/crit/SKILL.md", dest: ".agents/skills/crit/SKILL.md", hint: "Use $crit in Codex to start a review loop"},
		{source: "integrations/codex/skills/crit-cli/SKILL.md", dest: ".agents/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Codex agents when needed"},
	},
	"qwen": {
		// Qwen Code auto-discovers .qwen/skills/ project-locally and ~/.qwen/skills/ globally —
		// same shape both modes, so no globalDest redirect is needed.
		{source: "integrations/qwen/skills/crit/SKILL.md", dest: ".qwen/skills/crit/SKILL.md", hint: "Run /crit in Qwen Code to start a review loop"},
		{source: "integrations/qwen/skills/crit-cli/SKILL.md", dest: ".qwen/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Qwen Code agents when needed"},
	},
	"pi": {
		// Pi auto-discovers skills in both .pi/skills/ (project-local) and
		// ~/.pi/agent/skills/ (global). Different shape between modes, so
		// globalDest redirects the global install to the agent/skills path.
		{source: "integrations/pi/skills/crit/SKILL.md", dest: ".pi/skills/crit/SKILL.md", globalDest: ".pi/agent/skills/crit/SKILL.md", globalDestKind: globalDestRelHome, hint: "Run /crit in Pi to start a review loop"},
		{source: "integrations/pi/skills/crit-cli/SKILL.md", dest: ".pi/skills/crit-cli/SKILL.md", globalDest: ".pi/agent/skills/crit-cli/SKILL.md", globalDestKind: globalDestRelHome, hint: "The crit-cli skill is available to Pi agents when needed"},
	},
	"hermes": {
		// Hermes only auto-discovers skills under HERMES_HOME (default ~/.hermes/skills/).
		// Project-local .hermes/skills/ is not loaded unless added to `external_dirs` in
		// ~/.hermes/config.yaml — surfaced via the hint below.
		{source: "integrations/hermes/skills/crit/SKILL.md", dest: ".hermes/skills/crit/SKILL.md", globalDest: ".hermes/skills/crit/SKILL.md", globalDestKind: globalDestRelHome, hint: "Run /crit in Hermes to start a review loop"},
		{source: "integrations/hermes/skills/crit-cli/SKILL.md", dest: ".hermes/skills/crit-cli/SKILL.md", globalDest: ".hermes/skills/crit-cli/SKILL.md", globalDestKind: globalDestRelHome, hint: "The crit-cli skill is available to Hermes agents when needed"},
	},
	"gemini": {
		{source: "integrations/gemini/skills/crit-cli/SKILL.md", dest: ".gemini/skills/crit-cli/SKILL.md", globalDest: ".gemini/skills/crit-cli/SKILL.md", globalDestKind: globalDestRelHome, hint: "The crit-cli skill is available to Gemini CLI agents when needed"},
		{source: "integrations/gemini/commands/crit.toml", dest: ".gemini/commands/crit.toml", globalDest: ".gemini/commands/crit.toml", globalDestKind: globalDestRelHome, hint: "Run /crit in Gemini CLI to start a review loop"},
		{source: "integrations/gemini/hooks/policy.toml", dest: ".gemini/policies/crit.toml", globalDest: ".gemini/policies/crit.toml", globalDestKind: globalDestRelHome, hint: "The crit policy allows exit_plan_mode without confirmation"},
	},
}

// availableIntegrations returns the sorted list of integration names that
// `crit install <name>` accepts. Derived from integrationMap keys plus the
// special-cased "aider" entry (which does not live in the map because its
// install flow is bespoke — see installAider).
func availableIntegrations() []string {
	names := make([]string, 0, len(integrationMap)+1)
	for name := range integrationMap {
		names = append(names, name)
	}
	names = append(names, "aider")
	sort.Strings(names)
	return names
}

// isGlobalInstall reports whether the install should be treated as global
// (user-wide) rather than project-scoped. True when cwd == $HOME.
func isGlobalInstall(cwd, home string) bool {
	if cwd == "" || home == "" {
		return false
	}
	a, errA := filepath.Abs(cwd)
	b, errB := filepath.Abs(home)
	if errA != nil || errB != nil {
		return cwd == home
	}
	return a == b
}

// resolveGlobalDest expands an integration's globalDest into an absolute
// path according to its globalDestKind.
func resolveGlobalDest(kind globalDestKind, globalDest, home string) (string, error) {
	switch kind {
	case globalDestAbsolute:
		return globalDest, nil
	case globalDestDocuments:
		return filepath.Join(documentsDir(home), globalDest), nil
	case globalDestRelHome, globalDestNone:
		if filepath.IsAbs(globalDest) {
			return globalDest, nil
		}
		return filepath.Join(home, globalDest), nil
	default:
		return "", fmt.Errorf("unknown globalDestKind %d", kind)
	}
}

// xdgUserDirFn is the seam used by documentsDir to query xdg-user-dir.
// Tests override this; production code uses the default that shells out.
var xdgUserDirFn = xdgUserDir

// documentsDir returns the platform Documents directory for the current user.
//
//	macOS:   $HOME/Documents
//	Linux:   $(xdg-user-dir DOCUMENTS), falling back to $HOME/Documents.
//	         If xdg-user-dir returns $HOME (its documented behavior when
//	         user-dirs.dirs is missing), we treat that as "no answer" and
//	         fall back to $HOME/Documents to avoid polluting the home dir.
//	Windows: filepath.Join(home, "Documents") — the real MyDocuments folder
//	         can differ; querying FOLDERID_Documents needs x/sys/windows.
//	         The $USERPROFILE\Documents convention is a pragmatic default.
func documentsDir(home string) string {
	if runtime.GOOS == "linux" {
		path, err := xdgUserDirFn("DOCUMENTS")
		if err == nil && path != "" && path != home {
			return path
		}
	}
	return filepath.Join(home, "Documents")
}

// xdgUserDir shells out to the xdg-user-dir binary to query a user dir.
// Returns ("", error) if the binary is missing or returns non-zero. The
// returned path is whitespace-trimmed; it may equal $HOME when the spec
// says no user-dirs.dirs entry exists — callers must handle that case.
func xdgUserDir(name string) (string, error) {
	out, err := exec.Command("xdg-user-dir", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// installIntegration installs the named agent integration. It returns an
// error suitable for printing to stderr; callers decide whether to exit or
// continue (the `install all` loop continues past per-agent failures).
func installIntegration(name string, force bool) error {
	if name == "aider" {
		return installAider(force)
	}

	files, ok := integrationMap[name]
	if !ok {
		var b strings.Builder
		fmt.Fprintf(&b, "unknown agent: %s\n\nAvailable agents:\n", name)
		for _, a := range availableIntegrations() {
			fmt.Fprintf(&b, "  %s\n", a)
		}
		return errors.New(strings.TrimRight(b.String(), "\n"))
	}

	cwd := mustGetwd()
	home, _ := os.UserHomeDir()
	global := isGlobalInstall(cwd, home)

	if name == "windsurf" && global {
		return errors.New("windsurf does not support a global per-tool install. " +
			"Windsurf only loads a single ~/.codeium/windsurf/memories/global_rules.md (6k char cap), " +
			"not a per-tool rules directory. Run `crit install windsurf` from a project directory " +
			"instead, which writes .windsurf/rules/crit.md (workspace-scoped)")
	}

	var hints []string
	for _, f := range files {
		dest := destFor(f, global, home, name)
		installOneFile(f, dest, force)
		if f.hint != "" {
			hints = append(hints, f.hint)
		}
	}
	if name == "gemini" {
		settingsPath := filepath.Join(".gemini", "settings.json")
		if global {
			settingsPath = filepath.Join(home, ".gemini", "settings.json")
		}
		installGeminiSettings(settingsPath, force)
	}
	if name == "opencode" {
		if err := installOpencodePluginEntry(opencodeConfigPath(global, home), opencodePluginEntry(global), force); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not register plugin in opencode config: %v\n", err)
		}
	}
	if name == "hermes" && !global {
		fmt.Println()
		fmt.Println("  Note: Hermes only auto-discovers skills from ~/.hermes/skills/.")
		fmt.Println("  For project-local skills to load, either:")
		fmt.Println("    a) run `cd ~ && crit install hermes` to install globally, or")
		fmt.Println("    b) add `.hermes/skills` to `external_dirs` under the `skills` section")
		fmt.Println("       of ~/.hermes/config.yaml (Hermes scans those dirs read-only).")
	}
	printUniqueHints(hints)
	fmt.Println()
	return nil
}

// destFor returns the destination path for an integration file, accounting
// for global vs project install mode.
func destFor(f integration, global bool, home, name string) string {
	if global && f.globalDest != "" {
		resolved, err := resolveGlobalDest(f.globalDestKind, f.globalDest, home)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving global destination for %s: %v\n", name, err)
			os.Exit(1)
		}
		return resolved
	}
	return f.dest
}

// installOneFile copies a single embedded integration file to dest, skipping
// if it already exists (unless force is set). Exits on I/O errors.
func installOneFile(f integration, dest string, force bool) {
	if !force {
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", dest)
			return
		}
	}
	data, err := integrationsFS.ReadFile(f.source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading embedded file %s: %v\n", f.source, err)
		os.Exit(1)
	}
	if err := atomicWriteFile(dest, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", dest, err)
		os.Exit(1)
	}
	fmt.Printf("  Installed: %s\n", dest)
}

// installGeminiSettings merges the crit plan-hook into .gemini/settings.json,
// creating the file if it doesn't exist. Idempotent: skips if the
// exit_plan_mode hook is already present (unless --force).
func installGeminiSettings(path string, force bool) {
	existing := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s contains invalid JSON — use --force to overwrite\n", path)
			os.Exit(1)
		}
	}

	hooks, _ := existing["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}
	before, _ := hooks["BeforeTool"].([]interface{})

	for _, entry := range before {
		m, ok := entry.(map[string]interface{})
		if !ok || m["matcher"] != "exit_plan_mode" {
			continue
		}
		if !force {
			fmt.Printf("  Skipped:   %s (hooks already configured, use --force to overwrite)\n", path)
			return
		}
		// force: strip the existing entry and re-add below
		var next []interface{}
		for _, e := range before {
			if mm, ok2 := e.(map[string]interface{}); ok2 && mm["matcher"] == "exit_plan_mode" {
				continue
			}
			next = append(next, e)
		}
		before = next
		break
	}

	before = append(before, map[string]interface{}{
		"matcher": "exit_plan_mode",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "crit plan-hook",
				"timeout": 345600000,
			},
		},
	})
	hooks["BeforeTool"] = before
	existing["hooks"] = hooks

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding %s: %v\n", path, err)
		os.Exit(1)
	}
	if err := atomicWriteFile(path, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("  Installed: %s\n", path)
}

// printUniqueHints prints each hint once, in the order it first appeared.
func printUniqueHints(hints []string) {
	seen := make(map[string]bool)
	for _, hint := range hints {
		if seen[hint] {
			continue
		}
		seen[hint] = true
		fmt.Printf("  %s\n", hint)
	}
}
