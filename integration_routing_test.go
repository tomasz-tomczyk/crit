package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover the destination-routing rules for non-aider integrations.
// They protect against regressions in integrationMap entries: missing
// globalDest fields, wrong globalDestKind, or wrong destination paths.
// (The aider integration has its own end-to-end coverage in aider_install_test.go.)

func TestDestFor_ProjectMode(t *testing.T) {
	// Project install must always use the dest field, regardless of whether a
	// globalDest is set.
	for name, files := range integrationMap {
		for i, f := range files {
			got := destFor(f, false, "/home/me", name)
			if got != f.dest {
				t.Errorf("%s[%d] project: got %q, want %q", name, i, got, f.dest)
			}
		}
	}
}

func TestDestFor_GlobalMode(t *testing.T) {
	// In global mode, integrations with no globalDest fall through to the raw
	// relative dest (which lands under $HOME because cwd == $HOME during a
	// global install). Integrations with a globalDest get the redirected
	// absolute path. The two routes have meaningfully different semantics, so
	// the test exercises both.
	home := "/home/me"
	cases := []struct {
		tool    string
		fileIdx int
		want    string
	}{
		// No globalDest → raw relative dest, written cwd-relative (cwd == $HOME).
		{"claude-code", 0, ".claude/skills/crit/SKILL.md"},
		{"claude-code", 1, ".claude/skills/crit-cli/SKILL.md"},
		{"codex", 0, ".agents/skills/crit/SKILL.md"},
		{"codex", 1, ".agents/skills/crit-cli/SKILL.md"},
		{"qwen", 0, ".qwen/skills/crit/SKILL.md"},
		{"qwen", 1, ".qwen/skills/crit-cli/SKILL.md"},
		{"cursor", 0, ".cursor/skills/crit/SKILL.md"},
		{"cursor", 1, ".cursor/skills/crit-cli/SKILL.md"},
		// grok: same-shape .grok/skills/ project-locally and ~/.grok/skills/ globally (no globalDest redirect needed).
		{"grok", 0, ".grok/skills/crit/SKILL.md"},
		{"grok", 1, ".grok/skills/crit-cli/SKILL.md"},
		// opencode: command redirects to ~/.config/opencode/commands/; skill redirects to ~/.agents/skills/.
		{"opencode", 0, filepath.Join(home, ".config/opencode/commands/crit.md")},
		{"opencode", 1, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		// github-copilot: both skills redirect to ~/.agents/skills/.
		{"github-copilot", 0, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		{"github-copilot", 1, filepath.Join(home, ".agents/skills/crit-cli/SKILL.md")},
		// hermes: both skills redirect to ~/.hermes/skills/.
		{"hermes", 0, filepath.Join(home, ".hermes/skills/crit/SKILL.md")},
		{"hermes", 1, filepath.Join(home, ".hermes/skills/crit-cli/SKILL.md")},
		// pi: both skills redirect to ~/.pi/agent/skills/.
		{"pi", 0, filepath.Join(home, ".pi/agent/skills/crit/SKILL.md")},
		{"pi", 1, filepath.Join(home, ".pi/agent/skills/crit-cli/SKILL.md")},
	}
	for _, tc := range cases {
		f := integrationMap[tc.tool][tc.fileIdx]
		got := destFor(f, true, home, tc.tool)
		if got != tc.want {
			t.Errorf("%s[%d] global: got %q, want %q", tc.tool, tc.fileIdx, got, tc.want)
		}
	}
}

func TestDestFor_ClineGlobalUsesDocuments(t *testing.T) {
	// Cline's globalDest uses the platform Documents directory, not $HOME directly.
	prev := xdgUserDirFn
	t.Cleanup(func() { xdgUserDirFn = prev })
	xdgUserDirFn = func(string) (string, error) { return "", nil }

	home := "/home/me"
	f := integrationMap["cline"][0]
	got := destFor(f, true, home, "cline")
	want := filepath.Join(documentsDir(home), "Cline/Rules/crit.md")
	if got != want {
		t.Errorf("cline global: got %q, want %q", got, want)
	}
	// On non-Linux, this should always be $HOME/Documents/Cline/Rules/crit.md.
	if runtime.GOOS != "linux" {
		expected := filepath.Join(home, "Documents/Cline/Rules/crit.md")
		if got != expected {
			t.Errorf("cline global on %s: got %q, want %q", runtime.GOOS, got, expected)
		}
	}
}

func TestIntegrationMap_SnapshotGlobalRouting(t *testing.T) {
	// Snapshot test: verifies each tool's globalDest configuration matches
	// what the integration validation findings established. Update this test
	// when intentionally changing routing.
	type want struct {
		globalDest string
		kind       globalDestKind
	}
	expected := map[string][]want{
		"claude-code":    {{"", globalDestNone}, {"", globalDestNone}},
		"cursor":         {{"", globalDestNone}, {"", globalDestNone}},
		"codex":          {{"", globalDestNone}, {"", globalDestNone}},
		"qwen":           {{"", globalDestNone}, {"", globalDestNone}},
		"opencode":       {{".config/opencode/commands/crit.md", globalDestRelHome}, {".agents/skills/crit/SKILL.md", globalDestRelHome}, {".config/opencode/plugins/crit.ts", globalDestRelHome}},
		"github-copilot": {{".agents/skills/crit/SKILL.md", globalDestRelHome}, {".agents/skills/crit-cli/SKILL.md", globalDestRelHome}},
		"windsurf":       {{"", globalDestNone}},
		"cline":          {{"Cline/Rules/crit.md", globalDestDocuments}},
		"gemini":         {{".gemini/skills/crit-cli/SKILL.md", globalDestRelHome}, {".gemini/commands/crit.toml", globalDestRelHome}, {".gemini/policies/crit.toml", globalDestRelHome}},
		"grok":           {{"", globalDestNone}, {"", globalDestNone}},
		"codex-plugin":   {{".codex/plugins/crit/.codex-plugin/plugin.json", globalDestRelHome}, {".codex/plugins/crit/skills/crit/SKILL.md", globalDestRelHome}, {".codex/plugins/crit/skills/crit-cli/SKILL.md", globalDestRelHome}, {".codex/plugins/crit/hooks/hooks.json", globalDestRelHome}},
		"hermes":         {{".hermes/skills/crit/SKILL.md", globalDestRelHome}, {".hermes/skills/crit-cli/SKILL.md", globalDestRelHome}},
		"pi":             {{".pi/agent/skills/crit/SKILL.md", globalDestRelHome}, {".pi/agent/skills/crit-cli/SKILL.md", globalDestRelHome}},
	}
	for tool, files := range expected {
		got := integrationMap[tool]
		if len(got) != len(files) {
			t.Errorf("%s: got %d files, want %d", tool, len(got), len(files))
			continue
		}
		for i, w := range files {
			if got[i].globalDest != w.globalDest || got[i].globalDestKind != w.kind {
				t.Errorf("%s[%d]: got (%q, kind=%d), want (%q, kind=%d)",
					tool, i, got[i].globalDest, got[i].globalDestKind, w.globalDest, w.kind)
			}
		}
	}
}

func TestInstallOneFile_WritesAndSkips(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "subdir", "out.md")
	f := integration{source: "integrations/cline/crit.md", dest: dest}

	// First install: file written.
	installOneFile(f, dest, false)
	if _, err := os.ReadFile(dest); err != nil {
		t.Fatalf("expected file at %s: %v", dest, err)
	}

	// Second install without --force: should skip without erroring.
	// Modify the file to verify it's not overwritten.
	if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	installOneFile(f, dest, false)
	got, _ := os.ReadFile(dest)
	if string(got) != "hand-edited" {
		t.Errorf("non-force should skip; file was overwritten: %q", got)
	}

	// Force install: file overwritten with embedded content.
	installOneFile(f, dest, true)
	got, _ = os.ReadFile(dest)
	if string(got) == "hand-edited" {
		t.Errorf("force should overwrite; file still has hand-edited content")
	}
}

// TestInstallIntegration_GeminiWritesSettingsJSON verifies that the gemini
// special-case in installIntegration runs installGeminiSettings and produces
// a .gemini/settings.json in the project directory.
func TestInstallIntegration_GeminiWritesSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := installIntegration("gemini", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}
	settingsPath := filepath.Join(dir, ".gemini", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected .gemini/settings.json to be written: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	hooks, _ := m["hooks"].(map[string]interface{})
	before, _ := hooks["BeforeTool"].([]interface{})
	for _, e := range before {
		if em, ok := e.(map[string]interface{}); ok && em["matcher"] == "exit_plan_mode" {
			return
		}
	}
	t.Error("exit_plan_mode hook not found in .gemini/settings.json")
}

func TestInstallIntegration_CodexPluginEndToEnd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	setHome(t, home)
	t.Chdir(dir)

	if err := installIntegration("codex-plugin", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}

	for _, path := range []string{
		".agents/skills/crit/SKILL.md",
		".agents/skills/crit-cli/SKILL.md",
		"plugins/crit/.codex-plugin/plugin.json",
		"plugins/crit/skills/crit/SKILL.md",
		"plugins/crit/skills/crit-cli/SKILL.md",
		"plugins/crit/hooks/hooks.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}

	hookPath := filepath.Join(dir, "plugins/crit/hooks/hooks.json")
	hookData, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hookData), "crit plan-hook --mode codex") {
		t.Fatalf("plugin hook should invoke crit plan-hook --mode codex:\n%s", hookData)
	}

	marketplacePath := filepath.Join(dir, ".agents/plugins/marketplace.json")
	if got := countCritMarketplaceEntries(t, marketplacePath, "./plugins/crit"); got != 1 {
		t.Fatalf("expected one Crit marketplace entry after first install, got %d", got)
	}
	assertCritMarketplacePathExists(t, marketplacePath, dir)
	assertCodexPluginEnabled(t, filepath.Join(home, ".codex", "config.toml"), "crit@local")
	for _, path := range []string{
		".codex/plugins/cache/local/crit/local/.codex-plugin/plugin.json",
		".codex/plugins/cache/local/crit/local/skills/crit/SKILL.md",
		".codex/plugins/cache/local/crit/local/hooks/hooks.json",
	} {
		if _, err := os.Stat(filepath.Join(home, path)); err != nil {
			t.Fatalf("expected cache file %s to be written: %v", path, err)
		}
	}

	if err := installIntegration("codex-plugin", false); err != nil {
		t.Fatalf("second installIntegration: %v", err)
	}
	if got := countCritMarketplaceEntries(t, marketplacePath, "./plugins/crit"); got != 1 {
		t.Fatalf("expected idempotent marketplace registration, got %d entries", got)
	}
}

func TestInstallIntegration_CodexPluginGlobalEndToEnd(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Chdir(home)

	if err := installIntegration("codex-plugin", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}

	for _, path := range []string{
		".agents/skills/crit/SKILL.md",
		".agents/skills/crit-cli/SKILL.md",
		".codex/plugins/crit/.codex-plugin/plugin.json",
		".codex/plugins/crit/skills/crit/SKILL.md",
		".codex/plugins/crit/skills/crit-cli/SKILL.md",
		".codex/plugins/crit/hooks/hooks.json",
	} {
		if _, err := os.Stat(filepath.Join(home, path)); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}

	marketplacePath := filepath.Join(home, ".agents/plugins/marketplace.json")
	if got := countCritMarketplaceEntries(t, marketplacePath, "./.codex/plugins/crit"); got != 1 {
		t.Fatalf("expected one Crit marketplace entry after global install, got %d", got)
	}
	assertCritMarketplacePathExists(t, marketplacePath, home)
	assertCodexPluginEnabled(t, filepath.Join(home, ".codex", "config.toml"), "crit@local")
	if _, err := os.Stat(filepath.Join(home, ".codex/plugins/cache/local/crit/local/.codex-plugin/plugin.json")); err != nil {
		t.Fatalf("expected global cache to be written: %v", err)
	}
}

func TestInstallIntegration_CodexPluginDoesNotActivateExistingProjectFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(dir, "plugins/crit/hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "plugins/crit/.codex-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	setHome(t, home)
	t.Chdir(dir)

	staleHookPath := filepath.Join(dir, "plugins/crit/hooks/hooks.json")
	if err := os.WriteFile(staleHookPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"stale-command"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins/crit/.codex-plugin/plugin.json"), []byte(`{"name":"crit","hooks":"./hooks/hooks.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installIntegration("codex-plugin", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}

	embeddedHook, err := integrationsFS.ReadFile("integrations/codex/plugin/crit/hooks/hooks.json")
	if err != nil {
		t.Fatalf("reading embedded hook: %v", err)
	}
	for _, path := range []string{
		staleHookPath,
		filepath.Join(home, ".codex/plugins/cache/local/crit/local/hooks/hooks.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(data) != string(embeddedHook) {
			t.Fatalf("%s did not use embedded Crit hook:\n%s", path, data)
		}
	}
}

func TestInstallCodexPluginMarketplaceForceOverwritesInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", true); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	if got := countCritMarketplaceEntries(t, path, "./plugins/crit"); got != 1 {
		t.Fatalf("expected one Crit marketplace entry, got %d", got)
	}
}

func TestInstallCodexPluginMarketplaceForceOverwritesMalformedPlugins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(path, []byte(`{"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", true); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	if got := countCritMarketplaceEntries(t, path, "./plugins/crit"); got != 1 {
		t.Fatalf("expected one Crit marketplace entry, got %d", got)
	}
}

func TestInstallCodexPluginMarketplaceInvalidJSONReturnsErrorWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err == nil {
		t.Fatal("expected invalid marketplace JSON to return an error")
	}
}

func TestInstallCodexPluginMarketplaceMalformedPluginsReturnsErrorWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(path, []byte(`{"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err == nil {
		t.Fatal("expected malformed plugins field to return an error")
	}
}

func TestInstallCodexPluginMarketplaceRejectsWhitespaceNameWithoutForce(t *testing.T) {
	for _, name := range []string{"local ", "   "} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "marketplace.json")
			existing := fmt.Sprintf(`{
  "name": %q,
  "plugins": [
    {"name": "crit", "source": {"source": "local", "path": "./plugins/crit"}}
  ]
}`, name)
			if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err == nil {
				t.Fatal("expected whitespace marketplace name to be rejected without force")
			}
		})
	}
}

func TestInstallCodexPluginMarketplaceRepairsStaleSourcePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	stale := `{
  "name": "local",
  "plugins": [
    {
      "name": "crit",
      "source": {"source": "local", "path": "./plugins/crit"},
      "policy": {"installation": "INSTALLED_BY_DEFAULT", "authentication": "ON_INSTALL"},
      "category": "Developer Tools"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./.codex/plugins/crit", false); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	if got := countCritMarketplaceEntries(t, path, "./.codex/plugins/crit"); got != 1 {
		t.Fatalf("expected stale Crit marketplace entry to be replaced, got %d", got)
	}
}

func TestInstallCodexPluginMarketplaceRepairsStalePolicyAndDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	stale := `{
  "name": "local",
  "plugins": [
    {
      "name": "crit",
      "source": {"source": "local", "path": "./plugins/crit"},
      "policy": {"installation": "AVAILABLE", "authentication": "ON_USE"},
      "category": "Old Category"
    },
    {
      "name": "crit",
      "source": {"source": "local", "path": "./plugins/crit"},
      "policy": {"installation": "INSTALLED_BY_DEFAULT", "authentication": "ON_INSTALL"},
      "category": "Developer Tools"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	if got := countCritMarketplaceEntries(t, path, "./plugins/crit"); got != 1 {
		t.Fatalf("expected one repaired Crit marketplace entry, got %d", got)
	}
}

func TestInstallCodexPluginMarketplacePreservesOtherTypedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	existing := `{
  "name": "local",
  "sentinel": {"keep": true},
  "interface": {"displayName": "My Plugins", "theme": "dark"},
  "plugins": [
    {
      "name": "other",
      "source": {"source": "local", "path": "./other", "kind": "dev"},
      "policy": {"authentication": "ON_USE", "extra": "kept"},
      "category": "Other",
      "summary": "keep me"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marketplace codexPluginMarketplace
	if err := json.Unmarshal(data, &marketplace); err != nil {
		t.Fatal(err)
	}
	if _, ok := marketplace.Extra["sentinel"]; !ok {
		t.Fatalf("expected top-level sentinel to be preserved: %s", data)
	}
	if _, ok := marketplace.Interface.Extra["theme"]; !ok {
		t.Fatalf("expected interface theme to be preserved: %s", data)
	}
	var other codexMarketplacePlugin
	for _, plugin := range marketplace.Plugins {
		if plugin.Name == "other" {
			other = plugin
			break
		}
	}
	if other.Name == "" {
		t.Fatalf("expected other plugin to be preserved: %s", data)
	}
	if len(other.Extra["summary"]) == 0 {
		t.Fatalf("expected plugin summary to be preserved: %s", data)
	}
	if len(other.Source.Extra["kind"]) == 0 {
		t.Fatalf("expected source kind to be preserved: %s", data)
	}
	if len(other.Policy.Extra["extra"]) == 0 {
		t.Fatalf("expected policy extra to be preserved: %s", data)
	}
}

func TestInstallCodexPluginMarketplacePreservesShorthandSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	existing := `{
  "name": "local",
  "plugins": [
    {"name": "other", "source": "./plugins/other", "category": "Other"}
  ]
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCodexPluginMarketplace(path, "./plugins/crit", false); err != nil {
		t.Fatalf("installCodexPluginMarketplace: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marketplace struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &marketplace); err != nil {
		t.Fatal(err)
	}
	for _, plugin := range marketplace.Plugins {
		if plugin.Name == "other" && string(plugin.Source) == `"./plugins/other"` {
			return
		}
	}
	t.Fatalf("expected shorthand source to be preserved: %s", data)
}

func TestUpsertCodexPluginConfig(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "creates plugin table",
			want: "[features]\nplugins = true\nhooks = true\nplugin_hooks = true\n\n[plugins.\"crit@local\"]\nenabled = true\n",
		},
		{
			name: "enables existing plugin table",
			raw:  "[plugins.\"crit@local\"]\nenabled = false\nsource = \"keep\"\n",
			want: "[plugins.\"crit@local\"]\nenabled = true\nsource = \"keep\"\n\n[features]\nplugins = true\nhooks = true\nplugin_hooks = true\n",
		},
		{
			name: "preserves surrounding config",
			raw:  "model = \"gpt-5.1\"\n\n[features]\nhooks = false\n\n[plugins.\"other@local\"]\nenabled = true\n",
			want: "model = \"gpt-5.1\"\n\n[features]\nhooks = true\nplugins = true\nplugin_hooks = true\n\n[plugins.\"other@local\"]\nenabled = true\n\n[plugins.\"crit@local\"]\nenabled = true\n",
		},
		{
			name: "updates commented table headers",
			raw:  "[features] # managed by user\nhooks = false\n\n[plugins.\"crit@local\"] # existing plugin\nenabled = false\n",
			want: "[features] # managed by user\nhooks = true\nplugins = true\nplugin_hooks = true\n\n[plugins.\"crit@local\"] # existing plugin\nenabled = true\n",
		},
		{
			name: "stops before array table headers",
			raw:  "[features]\nhooks = false\n\n[[hooks.PreToolUse]]\nmatcher = \"shell\"\n\n[plugins.\"crit@local\"]\nenabled = false\n",
			want: "[features]\nhooks = true\nplugins = true\nplugin_hooks = true\n\n[[hooks.PreToolUse]]\nmatcher = \"shell\"\n\n[plugins.\"crit@local\"]\nenabled = true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upsertCodexPluginConfig(tt.raw, "crit@local"); got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestInstallCodexPluginConfigPreservesModeAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "managed-config.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("model = \"gpt-5.1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := installCodexPluginConfig(link, "crit@local"); err != nil {
		t.Fatalf("installCodexPluginConfig: %v", err)
	}
	if info, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected config path symlink to be preserved")
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[plugins.\"crit@local\"]") {
		t.Fatalf("expected target config to be updated, got:\n%s", data)
	}
}

func TestInstallCodexPluginConfigDefaultsNewFileTo0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := installCodexPluginConfig(path, "crit@local"); err != nil {
		t.Fatalf("installCodexPluginConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestCodexPluginCachePathComponentsAreValidated(t *testing.T) {
	for _, value := range []string{"", "..", ".", "bad/name", `bad\name`, "bad.name", "bad name", " ", " local", "local ", "ümlaut"} {
		if _, err := validCodexPluginSegment(value, "test"); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	for _, value := range []string{"local", "my_market-1"} {
		if got, err := validCodexPluginSegment(value, "test"); err != nil || got != value {
			t.Fatalf("valid component = %q, %v; want %q, nil", got, err, value)
		}
	}
}

func TestCodexPluginManifestVersionRejectsWhitespace(t *testing.T) {
	if _, err := codexPluginManifestVersionFromBytes([]byte(`{"version":"   "}`)); err == nil {
		t.Fatal("expected whitespace version to be rejected")
	}
	if got, err := codexPluginManifestVersionFromBytes([]byte(`{}`)); err != nil || got != "local" {
		t.Fatalf("version = %q, %v; want local, nil", got, err)
	}
}

func countCritMarketplaceEntries(t *testing.T, path, wantSourcePath string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected marketplace at %s: %v", path, err)
	}
	var marketplace codexPluginMarketplace
	if err := json.Unmarshal(data, &marketplace); err != nil {
		t.Fatalf("marketplace is not valid JSON: %v", err)
	}
	count := 0
	for _, plugin := range marketplace.Plugins {
		if plugin.Name != "crit" {
			continue
		}
		count++
		if plugin.Source.Source != "local" || plugin.Source.Path != wantSourcePath {
			t.Fatalf("unexpected Crit plugin source: %+v", plugin.Source)
		}
		if plugin.Policy.Installation != "INSTALLED_BY_DEFAULT" {
			t.Fatalf("unexpected Crit plugin policy: %+v", plugin.Policy)
		}
		if plugin.Policy.Authentication != "ON_INSTALL" {
			t.Fatalf("unexpected Crit plugin auth policy: %+v", plugin.Policy)
		}
		if plugin.Category != "Developer Tools" {
			t.Fatalf("unexpected Crit plugin category: %+v", plugin.Category)
		}
	}
	return count
}

func assertCritMarketplacePathExists(t *testing.T, marketplacePath, marketplaceRoot string) {
	t.Helper()

	data, err := os.ReadFile(marketplacePath)
	if err != nil {
		t.Fatal(err)
	}
	var marketplace codexPluginMarketplace
	if err := json.Unmarshal(data, &marketplace); err != nil {
		t.Fatal(err)
	}
	for _, plugin := range marketplace.Plugins {
		if plugin.Name != "crit" {
			continue
		}
		relPath := plugin.Source.Path
		pluginRoot := filepath.Join(marketplaceRoot, relPath)
		manifestPath := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		if _, err := os.Stat(manifestPath); err != nil {
			t.Fatalf("marketplace path %q should resolve to installed plugin manifest %s: %v", relPath, manifestPath, err)
		}
		return
	}
	t.Fatal("Crit marketplace entry not found")
}

func assertCodexPluginEnabled(t *testing.T, path, pluginKey string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected Codex config at %s: %v", path, err)
	}
	want := "[plugins.\"" + pluginKey + "\"]\nenabled = true"
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected Codex config to enable %s, got:\n%s", pluginKey, data)
	}
	if !codexPluginConfigReadyRaw(string(data), pluginKey) {
		t.Fatalf("expected Codex config to enable plugins, hooks, plugin_hooks, and %s, got:\n%s", pluginKey, data)
	}
}

// TestInstallIntegration_HermesPrintsExternalDirsNote verifies that on a
// project-mode install, the hermes special-case prints the external_dirs
// guidance — Hermes does not auto-discover project-local skills, so the
// note is the only thing that makes the project-install path useful.
func TestInstallIntegration_HermesPrintsExternalDirsNote(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = prev })

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()

	if err := installIntegration("hermes", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}
	_ = w.Close()
	out := <-done

	if _, err := os.Stat(filepath.Join(dir, ".hermes/skills/crit/SKILL.md")); err != nil {
		t.Fatalf("expected .hermes/skills/crit/SKILL.md to be written: %v", err)
	}
	for _, want := range []string{"~/.hermes/skills/", "external_dirs", "config.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("project install output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestPrintUniqueHints_Dedups(t *testing.T) {
	// printUniqueHints prints to stdout; we just verify it doesn't panic on
	// duplicates and empty input. Output ordering and dedup logic are simple
	// enough that visual inspection during integration use covers the rest.
	printUniqueHints(nil)
	printUniqueHints([]string{"a", "b", "a", "c", "b"})
}

func TestInstallGeminiSettings(t *testing.T) {
	hookEntry := func(data []byte) bool {
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return false
		}
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if ok && em["matcher"] == "exit_plan_mode" {
				return true
			}
		}
		return false
	}

	t.Run("creates file with hook when absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		installGeminiSettings(path, false)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected settings.json to be written: %v", err)
		}
		if !hookEntry(data) {
			t.Errorf("exit_plan_mode hook not found in %s", data)
		}
	})

	t.Run("skips when hook already present and no force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		// Write a valid settings.json that already has the hook plus a sentinel field.
		prebuilt := `{"hooks":{"BeforeTool":[{"matcher":"exit_plan_mode","hooks":[{"type":"command","command":"crit plan-hook","timeout":345600000}]}]},"sentinel":true}` + "\n"
		_ = os.WriteFile(path, []byte(prebuilt), 0o644)
		installGeminiSettings(path, false)
		got, _ := os.ReadFile(path)
		if string(got) != prebuilt {
			t.Errorf("no-force should skip; file was modified:\ngot:  %s\nwant: %s", got, prebuilt)
		}
	})

	t.Run("force overwrites existing hook entry", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		// write a settings file with a stale exit_plan_mode entry
		stale := `{"hooks":{"BeforeTool":[{"matcher":"exit_plan_mode","hooks":[{"type":"command","command":"old-cmd","timeout":1}]}]}}`
		_ = os.WriteFile(path, []byte(stale), 0o644)
		installGeminiSettings(path, true)
		data, _ := os.ReadFile(path)
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		// exactly one exit_plan_mode entry
		count := 0
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if ok && em["matcher"] == "exit_plan_mode" {
				count++
				inner, _ := em["hooks"].([]interface{})
				if len(inner) > 0 {
					cmd, _ := inner[0].(map[string]interface{})["command"].(string)
					if cmd == "old-cmd" {
						t.Error("stale command not replaced")
					}
				}
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 exit_plan_mode entry, got %d", count)
		}
	})

	t.Run("preserves existing unrelated hooks", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		existing := `{"hooks":{"BeforeTool":[{"matcher":"other_tool","hooks":[{"type":"command","command":"other"}]}]}}`
		_ = os.WriteFile(path, []byte(existing), 0o644)
		installGeminiSettings(path, false)
		data, _ := os.ReadFile(path)
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		hasOther, hasCrit := false, false
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			switch em["matcher"] {
			case "other_tool":
				hasOther = true
			case "exit_plan_mode":
				hasCrit = true
			}
		}
		if !hasOther {
			t.Error("pre-existing other_tool hook was removed")
		}
		if !hasCrit {
			t.Error("exit_plan_mode hook not added")
		}
	})
}
