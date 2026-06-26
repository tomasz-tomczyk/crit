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

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func runInstall(args []string) {
	target, force := parseInstallArgs(args)
	if target == "" {
		printInstallUsage()
		os.Exit(1)
	}
	if err := runInstallTarget(target, force); err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		os.Exit(1)
	}
}

func parseInstallArgs(args []string) (target string, force bool) {
	for _, a := range args {
		switch {
		case a == "--force" || a == "-f":
			force = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", a)
			os.Exit(1)
		default:
			if target != "" {
				fmt.Fprintf(os.Stderr, "Error: only one target allowed (got %q and %q)\n", target, a)
				os.Exit(1)
			}
			target = a
		}
	}
	return target, force
}

func printInstallUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit install <agent|prompts>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Available agents:")
	for _, a := range availableIntegrations() {
		fmt.Fprintf(os.Stderr, "  %s\n", a)
	}
	fmt.Fprintln(os.Stderr, "  all")
	fmt.Fprintln(os.Stderr, "  prompts")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Install stock finish templates:")
	fmt.Fprintln(os.Stderr, "  cd ~ && crit install prompts     # ~/.crit/prompts/")
	fmt.Fprintln(os.Stderr, "  crit install prompts             # .crit/prompts/ in cwd (from repo root)")
}

func runInstallTarget(target string, force bool) error {
	if target == "prompts" {
		return installPrompts(force)
	}
	if target == "all" {
		return installAllIntegrations(force)
	}
	return installIntegration(target, force)
}

func installAllIntegrations(force bool) error {
	cwd := mustGetwd()
	home, _ := os.UserHomeDir()
	global := isGlobalInstall(cwd, home)
	var hadErr bool
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
		return errors.New("one or more integrations failed to install")
	}
	return nil
}

func installPrompts(force bool) error {
	cwd := mustGetwd()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(cwd, ".crit", "prompts")
	if isGlobalInstall(cwd, home) {
		dest = filepath.Join(home, ".crit", "prompts")
	}
	fmt.Printf("Installing stock finish prompts to %s\n", dest)
	return prompt.InstallPrompts(dest, force)
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
		// opencode reads commands from `.opencode/commands/` (project) and
		// `~/.config/opencode/commands/` (global) — NOT `~/.opencode/commands/`.
		// Without the globalDest redirect a global install lands in a dir
		// opencode never scans, so `/crit` silently never appears.
		{source: "integrations/opencode/crit.md", dest: ".opencode/commands/crit.md", globalDest: ".config/opencode/commands/crit.md", globalDestKind: globalDestRelHome, hint: "Run /crit in OpenCode to start a review loop"},
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
	"codex-plugin": {
		{source: "integrations/codex/plugin/crit/.codex-plugin/plugin.json", dest: "plugins/crit/.codex-plugin/plugin.json", globalDest: ".codex/plugins/crit/.codex-plugin/plugin.json", globalDestKind: globalDestRelHome, hint: "The Crit plugin is registered in the local Codex plugin marketplace"},
		{source: "integrations/codex/plugin/crit/skills/crit/SKILL.md", dest: "plugins/crit/skills/crit/SKILL.md", globalDest: ".codex/plugins/crit/skills/crit/SKILL.md", globalDestKind: globalDestRelHome, hint: "The plugin-packaged crit skill is available to Codex as $crit:crit"},
		{source: "integrations/codex/plugin/crit/skills/crit-cli/SKILL.md", dest: "plugins/crit/skills/crit-cli/SKILL.md", globalDest: ".codex/plugins/crit/skills/crit-cli/SKILL.md", globalDestKind: globalDestRelHome, hint: "The plugin-packaged crit-cli skill is available to Codex agents when needed"},
		{source: "integrations/codex/plugin/crit/hooks/hooks.json", dest: "plugins/crit/hooks/hooks.json", globalDest: ".codex/plugins/crit/hooks/hooks.json", globalDestKind: globalDestRelHome, hint: "The Crit plugin includes a Codex Stop hook for proposed-plan review"},
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
	"grok": {
		// Grok auto-discovers .grok/skills/ both project-locally and in ~/.grok/skills/ globally.
		// Same shape in both cases, so no globalDest redirect is needed.
		{source: "integrations/grok/skills/crit/SKILL.md", dest: ".grok/skills/crit/SKILL.md", hint: "Run /crit in Grok to start a review loop"},
		{source: "integrations/grok/skills/crit-cli/SKILL.md", dest: ".grok/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Grok agents when needed"},
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
//
//nolint:gocyclo // per-agent special cases are clearer inline than abstracted
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
	codexMarketplaceName := ""
	if name == "codex-plugin" {
		var err error
		codexMarketplaceName, err = installCodexPluginMarketplace(codexPluginMarketplacePath(global, home), codexPluginMarketplaceSourcePath(global), force)
		if err != nil {
			return err
		}
		for _, f := range integrationMap["codex"] {
			dest := destFor(f, global, home, "codex")
			installOneFile(f, dest, force)
			if f.hint != "" {
				hints = append(hints, f.hint)
			}
		}
	}
	for _, f := range files {
		dest := destFor(f, global, home, name)
		installOneFile(f, dest, force || name == "codex-plugin")
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
	if name == "codex-plugin" {
		if err := installCodexPluginActivation(global, home, codexMarketplaceName); err != nil {
			return err
		}
		hints = append(hints, fmt.Sprintf("The Crit Codex plugin is enabled as crit@%s", codexMarketplaceName))
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

func codexPluginMarketplacePath(global bool, home string) string {
	if global {
		return filepath.Join(home, ".agents", "plugins", "marketplace.json")
	}
	return filepath.Join(".agents", "plugins", "marketplace.json")
}

func codexPluginMarketplaceSourcePath(global bool) string {
	if global {
		return "./.codex/plugins/crit"
	}
	return "./plugins/crit"
}

type codexPluginMarketplace struct {
	Name      string
	Interface codexPluginMarketplaceInterface
	Plugins   []codexMarketplacePlugin
	Extra     map[string]json.RawMessage
}

func (m *codexPluginMarketplace) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["name"]; ok {
		if err := json.Unmarshal(v, &m.Name); err != nil {
			return fmt.Errorf("name: %w", err)
		}
		delete(raw, "name")
	}
	if v, ok := raw["interface"]; ok {
		if err := json.Unmarshal(v, &m.Interface); err != nil {
			return fmt.Errorf("interface: %w", err)
		}
		delete(raw, "interface")
	}
	if v, ok := raw["plugins"]; ok {
		if err := json.Unmarshal(v, &m.Plugins); err != nil {
			return fmt.Errorf("plugins: %w", err)
		}
		delete(raw, "plugins")
	}
	m.Extra = raw
	return nil
}

func (m codexPluginMarketplace) MarshalJSON() ([]byte, error) {
	raw := cloneRawMessages(m.Extra)
	if m.Name != "" {
		data, err := json.Marshal(m.Name)
		if err != nil {
			return nil, err
		}
		raw["name"] = data
	}
	if !m.Interface.empty() {
		data, err := json.Marshal(m.Interface)
		if err != nil {
			return nil, err
		}
		raw["interface"] = data
	}
	if m.Plugins != nil {
		data, err := json.Marshal(m.Plugins)
		if err != nil {
			return nil, err
		}
		raw["plugins"] = data
	}
	return json.Marshal(raw)
}

type codexPluginMarketplaceInterface struct {
	DisplayName string
	Extra       map[string]json.RawMessage
}

func (i *codexPluginMarketplaceInterface) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["displayName"]; ok {
		if err := json.Unmarshal(v, &i.DisplayName); err != nil {
			return fmt.Errorf("displayName: %w", err)
		}
		delete(raw, "displayName")
	}
	i.Extra = raw
	return nil
}

func (i codexPluginMarketplaceInterface) MarshalJSON() ([]byte, error) {
	raw := cloneRawMessages(i.Extra)
	if i.DisplayName != "" {
		data, err := json.Marshal(i.DisplayName)
		if err != nil {
			return nil, err
		}
		raw["displayName"] = data
	}
	return json.Marshal(raw)
}

func (i codexPluginMarketplaceInterface) empty() bool {
	return i.DisplayName == "" && len(i.Extra) == 0
}

type codexMarketplacePlugin struct {
	Name     string
	Source   codexMarketplacePluginSource
	Policy   codexMarketplacePluginPolicy
	Category string
	Extra    map[string]json.RawMessage
}

func (p *codexMarketplacePlugin) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["name"]; ok {
		if err := json.Unmarshal(v, &p.Name); err != nil {
			return fmt.Errorf("name: %w", err)
		}
		delete(raw, "name")
	}
	if v, ok := raw["source"]; ok {
		if err := json.Unmarshal(v, &p.Source); err != nil {
			return fmt.Errorf("source: %w", err)
		}
		delete(raw, "source")
	}
	if v, ok := raw["policy"]; ok {
		if err := json.Unmarshal(v, &p.Policy); err != nil {
			return fmt.Errorf("policy: %w", err)
		}
		delete(raw, "policy")
	}
	if v, ok := raw["category"]; ok {
		if err := json.Unmarshal(v, &p.Category); err != nil {
			return fmt.Errorf("category: %w", err)
		}
		delete(raw, "category")
	}
	p.Extra = raw
	return nil
}

func (p codexMarketplacePlugin) MarshalJSON() ([]byte, error) {
	raw := cloneRawMessages(p.Extra)
	if p.Name != "" {
		data, err := json.Marshal(p.Name)
		if err != nil {
			return nil, err
		}
		raw["name"] = data
	}
	if !p.Source.empty() {
		data, err := json.Marshal(p.Source)
		if err != nil {
			return nil, err
		}
		raw["source"] = data
	}
	if !p.Policy.empty() {
		data, err := json.Marshal(p.Policy)
		if err != nil {
			return nil, err
		}
		raw["policy"] = data
	}
	if p.Category != "" {
		data, err := json.Marshal(p.Category)
		if err != nil {
			return nil, err
		}
		raw["category"] = data
	}
	return json.Marshal(raw)
}

func (p codexMarketplacePlugin) matchesDesired(desired codexMarketplacePlugin) bool {
	return p.Name == desired.Name &&
		p.Source.matches(desired.Source) &&
		p.Policy.Installation == desired.Policy.Installation &&
		p.Policy.Authentication == desired.Policy.Authentication &&
		p.Category == desired.Category
}

type codexMarketplacePluginSource struct {
	Shorthand string
	Source    string
	Path      string
	Extra     map[string]json.RawMessage
}

func (s *codexMarketplacePluginSource) UnmarshalJSON(data []byte) error {
	var shorthand string
	if err := json.Unmarshal(data, &shorthand); err == nil {
		s.Shorthand = shorthand
		return nil
	}

	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["source"]; ok {
		if err := json.Unmarshal(v, &s.Source); err != nil {
			return fmt.Errorf("source: %w", err)
		}
		delete(raw, "source")
	}
	if v, ok := raw["path"]; ok {
		if err := json.Unmarshal(v, &s.Path); err != nil {
			return fmt.Errorf("path: %w", err)
		}
		delete(raw, "path")
	}
	s.Extra = raw
	return nil
}

func (s codexMarketplacePluginSource) MarshalJSON() ([]byte, error) {
	if s.Shorthand != "" && s.Source == "" && s.Path == "" && len(s.Extra) == 0 {
		return json.Marshal(s.Shorthand)
	}

	raw := cloneRawMessages(s.Extra)
	if s.Source != "" {
		data, err := json.Marshal(s.Source)
		if err != nil {
			return nil, err
		}
		raw["source"] = data
	}
	if s.Path != "" {
		data, err := json.Marshal(s.Path)
		if err != nil {
			return nil, err
		}
		raw["path"] = data
	}
	return json.Marshal(raw)
}

func (s codexMarketplacePluginSource) empty() bool {
	return s.Shorthand == "" && s.Source == "" && s.Path == "" && len(s.Extra) == 0
}

func (s codexMarketplacePluginSource) matches(desired codexMarketplacePluginSource) bool {
	if s.Shorthand != "" {
		return desired.Source == "local" && s.Shorthand == desired.Path
	}
	return s.Source == desired.Source && s.Path == desired.Path
}

func (s codexMarketplacePluginSource) matchesLocalPath(path string) bool {
	if s.Shorthand != "" {
		return s.Shorthand == path
	}
	return s.Source == "local" && s.Path == path
}

type codexMarketplacePluginPolicy struct {
	Installation   string
	Authentication string
	Extra          map[string]json.RawMessage
}

func (p *codexMarketplacePluginPolicy) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["installation"]; ok {
		if err := json.Unmarshal(v, &p.Installation); err != nil {
			return fmt.Errorf("installation: %w", err)
		}
		delete(raw, "installation")
	}
	if v, ok := raw["authentication"]; ok {
		if err := json.Unmarshal(v, &p.Authentication); err != nil {
			return fmt.Errorf("authentication: %w", err)
		}
		delete(raw, "authentication")
	}
	p.Extra = raw
	return nil
}

func (p codexMarketplacePluginPolicy) MarshalJSON() ([]byte, error) {
	raw := cloneRawMessages(p.Extra)
	if p.Installation != "" {
		data, err := json.Marshal(p.Installation)
		if err != nil {
			return nil, err
		}
		raw["installation"] = data
	}
	if p.Authentication != "" {
		data, err := json.Marshal(p.Authentication)
		if err != nil {
			return nil, err
		}
		raw["authentication"] = data
	}
	return json.Marshal(raw)
}

func (p codexMarketplacePluginPolicy) empty() bool {
	return p.Installation == "" && p.Authentication == "" && len(p.Extra) == 0
}

func cloneRawMessages(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in)+4)
	for key, value := range in {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func desiredCodexMarketplacePlugin(sourcePath string) codexMarketplacePlugin {
	return codexMarketplacePlugin{
		Name: "crit",
		Source: codexMarketplacePluginSource{
			Source: "local",
			Path:   sourcePath,
		},
		Policy: codexMarketplacePluginPolicy{
			Installation:   "INSTALLED_BY_DEFAULT",
			Authentication: "ON_INSTALL",
		},
		Category: "Developer Tools",
	}
}

//nolint:gocyclo // marketplace read-merge-write has inherent branching for force/exists/valid/changed
func installCodexPluginMarketplace(path, sourcePath string, force bool) (string, error) {
	existing := codexPluginMarketplace{}
	changed := false
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			if !force {
				return "", fmt.Errorf("%s contains invalid JSON - use --force to overwrite", path)
			}
			existing = codexPluginMarketplace{}
			changed = true
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	marketplaceName := existing.Name
	if marketplaceName == "" {
		marketplaceName = "local"
		existing.Name = "local"
		changed = true
	}
	if validated, err := validCodexPluginSegment(marketplaceName, "Codex marketplace name"); err != nil {
		if !force {
			return "", err
		}
		marketplaceName = "local"
		existing.Name = "local"
		changed = true
	} else {
		marketplaceName = validated
	}
	if strings.TrimSpace(existing.Interface.DisplayName) == "" {
		existing.Interface.DisplayName = "Local Plugins"
		changed = true
	}

	entry := desiredCodexMarketplacePlugin(sourcePath)
	next := make([]codexMarketplacePlugin, 0, len(existing.Plugins)+1)
	found := false
	for _, plugin := range existing.Plugins {
		if plugin.Name != "crit" {
			next = append(next, plugin)
			continue
		}

		if found {
			changed = true
			continue
		}
		found = true

		if !force && plugin.matchesDesired(entry) {
			next = append(next, plugin)
			continue
		}
		next = append(next, entry)
		changed = true
	}

	if !found {
		next = append(next, entry)
		changed = true
	}
	if !changed {
		fmt.Printf("  Skipped:   %s (Crit plugin already registered, use --force to overwrite)\n", path)
		return marketplaceName, nil
	}
	existing.Plugins = next
	return marketplaceName, writeCodexPluginMarketplace(path, existing)
}

func writeCodexPluginMarketplace(path string, marketplace codexPluginMarketplace) error {
	data, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := atomicWriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Printf("  Installed: %s\n", path)
	return nil
}

func codexHome(home string) string {
	if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
		if home == "" || samePath(home, currentUserHome()) {
			return env
		}
	}
	return filepath.Join(home, ".codex")
}

func currentUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}

const codexPluginEmbeddedPrefix = "integrations/codex/plugin/crit/"

//nolint:unparam // global is passed for symmetry with other install* funcs; activation always targets ~/.codex
func installCodexPluginActivation(global bool, home, marketplaceName string) error {
	if marketplaceName == "" {
		marketplaceName = "local"
	}
	marketplaceName, err := validCodexPluginSegment(marketplaceName, "Codex marketplace name")
	if err != nil {
		return err
	}

	version, err := codexPluginEmbeddedManifestVersion()
	if err != nil {
		return err
	}

	codexRoot := codexHome(home)
	cacheBase := filepath.Join(codexRoot, "plugins", "cache")
	cacheRoot := filepath.Join(cacheBase, marketplaceName, "crit", version)
	if err := requirePathWithin(cacheBase, cacheRoot); err != nil {
		return err
	}
	if err := copyCodexPluginCacheFromEmbedded(cacheRoot); err != nil {
		return fmt.Errorf("installing Codex plugin cache: %w", err)
	}
	fmt.Printf("  Installed: %s\n", cacheRoot)

	configPath := filepath.Join(codexRoot, "config.toml")
	if err := installCodexPluginConfig(configPath, fmt.Sprintf("crit@%s", marketplaceName)); err != nil {
		return err
	}
	return nil
}

func codexPluginManifestVersion(sourceRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(sourceRoot, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "", fmt.Errorf("reading Codex plugin manifest: %w", err)
	}
	return codexPluginManifestVersionFromBytes(data)
}

func codexPluginEmbeddedManifestVersion() (string, error) {
	data, err := integrationsFS.ReadFile(codexPluginEmbeddedPrefix + ".codex-plugin/plugin.json")
	if err != nil {
		return "", fmt.Errorf("reading embedded Codex plugin manifest: %w", err)
	}
	return codexPluginManifestVersionFromBytes(data)
}

func codexPluginManifestVersionFromBytes(data []byte) (string, error) {
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parsing Codex plugin manifest: %w", err)
	}
	if manifest.Version != "" {
		return validCodexPluginSegment(manifest.Version, "Codex plugin version")
	}
	return "local", nil
}

func copyCodexPluginCacheFromEmbedded(dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	for _, f := range integrationMap["codex-plugin"] {
		rel, ok := strings.CutPrefix(f.source, codexPluginEmbeddedPrefix)
		if !ok {
			return fmt.Errorf("embedded plugin source %s is outside %s", f.source, codexPluginEmbeddedPrefix)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := requirePathWithin(dst, target); err != nil {
			return err
		}
		data, err := integrationsFS.ReadFile(f.source)
		if err != nil {
			return fmt.Errorf("reading embedded plugin file %s: %w", f.source, err)
		}
		if err := atomicWriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
	}
	return nil
}

func validCodexPluginSegment(value, label string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("%s %q must contain only ASCII letters, digits, `_`, and `-`", label, value)
	}
	return value, nil
}

func requirePathWithin(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return fmt.Errorf("checking cache path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("cache path %s escapes %s", target, base)
	}
	return nil
}

func installCodexPluginConfig(path, pluginKey string) error {
	readPath, writePath, err := resolveSymlinkWritePath(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	data, err := os.ReadFile(readPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", readPath, err)
	}
	perm := os.FileMode(0o600)
	if info, statErr := os.Stat(readPath); statErr == nil {
		perm = info.Mode().Perm()
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat %s: %w", readPath, statErr)
	}
	next := upsertCodexPluginConfig(string(data), pluginKey)
	if string(data) == next {
		fmt.Printf("  Skipped:   %s (Codex plugin already enabled)\n", path)
		return nil
	}
	if err := atomicWriteFile(writePath, []byte(next), perm); err != nil {
		return fmt.Errorf("writing %s: %w", writePath, err)
	}
	fmt.Printf("  Installed: %s\n", path)
	return nil
}

func resolveSymlinkWritePath(path string) (readPath, writePath string, err error) {
	root := path
	if abs, absErr := filepath.Abs(path); absErr == nil {
		root = abs
	}
	current := root
	seen := map[string]bool{}
	for {
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return current, current, nil
			}
			return "", "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return current, current, nil
		}
		if seen[current] {
			return "", "", fmt.Errorf("symlink cycle at %s", current)
		}
		seen[current] = true
		target, err := os.Readlink(current)
		if err != nil {
			return "", "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		current = filepath.Clean(target)
	}
}

func upsertCodexPluginConfig(raw, pluginKey string) string {
	raw = upsertTomlBool(raw, "features", "plugins", true)
	raw = upsertTomlBool(raw, "features", "hooks", true)
	raw = upsertTomlBool(raw, "features", "plugin_hooks", true)
	return upsertTomlBool(raw, fmt.Sprintf("plugins.%q", pluginKey), "enabled", true)
}

//nolint:unparam // value is always true today, but the function is designed for general TOML bool upsert
func upsertTomlBool(raw, table, key string, value bool) string {
	header := fmt.Sprintf("[%s]", table)
	replacement := fmt.Sprintf("%s = %t", key, value)
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}

	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if !tomlTableMatches(line, table) {
			continue
		}

		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if _, ok := tomlTableName(lines[j]); ok {
				end = j
				break
			}
		}
		for j := i + 1; j < end; j++ {
			if tomlKeyIs(lines[j], key) {
				lines[j] = leadingWhitespace(lines[j]) + replacement
				return ensureTrailingNewline(strings.Join(lines, "\n"))
			}
		}

		insert := end
		for insert > i+1 && strings.TrimSpace(lines[insert-1]) == "" {
			insert--
		}
		next := make([]string, 0, len(lines)+1)
		next = append(next, lines[:insert]...)
		next = append(next, replacement)
		next = append(next, lines[insert:]...)
		return ensureTrailingNewline(strings.Join(next, "\n"))
	}

	if strings.TrimSpace(raw) == "" {
		return header + "\n" + replacement + "\n"
	}
	return raw + "\n" + header + "\n" + replacement + "\n"
}

func tomlTableMatches(line, table string) bool {
	name, array, ok := tomlTableHeader(line)
	return ok && !array && name == table
}

func tomlTableName(line string) (string, bool) {
	name, _, ok := tomlTableHeader(line)
	return name, ok
}

func tomlTableHeader(line string) (name string, array bool, ok bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[[") {
		end := strings.Index(trimmed, "]]")
		if end <= 2 {
			return "", false, false
		}
		rest := strings.TrimSpace(trimmed[end+2:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", false, false
		}
		return strings.TrimSpace(trimmed[2:end]), true, true
	}
	if !strings.HasPrefix(trimmed, "[") {
		return "", false, false
	}
	end := strings.IndexByte(trimmed, ']')
	if end <= 1 {
		return "", false, false
	}
	rest := strings.TrimSpace(trimmed[end+1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", false, false
	}
	return strings.TrimSpace(trimmed[1:end]), false, true
}

func tomlKeyIs(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	return trimmed == key || strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=")
}

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
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
