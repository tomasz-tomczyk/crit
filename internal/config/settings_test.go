package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

func TestLoadConfigFileSettingsAllValidKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".crit.config.json")
	content := `{
		"settings": {
			"theme": "dark",
			"lightPalette": "min-light",
			"darkPalette": "tokyo-night",
			"boostContrast": true,
			"codeFont": "system",
			"lineNumbers": false,
			"codeOverflow": "wrap",
			"width": "wide",
			"diffMode": "unified",
			"inlineDiff": "char",
			"ignoreWhitespace": true,
			"unchangedContext": "expanded",
			"diffScope": "unstaged",
			"hideResolved": true,
			"live_hideResolved": false,
			"live_viewport": "tablet",
			"fileTree": "collapsed",
			"toc": "open",
			"commentsPanel": "open",
			"shortcuts": {
				"next_block": "ArrowDown",
				"previous_block": "ArrowUp",
				"toggle_toc": ""
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	var err error
	stderr := captureStderr(t, func() {
		cfg, _, err = LoadConfigFile(path)
	})
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr warnings, got:\n%s", stderr)
	}
	if len(cfg.Settings) != 20 {
		t.Fatalf("expected 20 validated settings, got %d: %#v", len(cfg.Settings), cfg.Settings)
	}
	if cfg.Settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", cfg.Settings["theme"])
	}
	if cfg.Settings["boostContrast"] != "on" {
		t.Errorf("boostContrast = %v, want on (normalized from true)", cfg.Settings["boostContrast"])
	}
	if cfg.Settings["lineNumbers"] != "off" {
		t.Errorf("lineNumbers = %v, want off (normalized from false)", cfg.Settings["lineNumbers"])
	}
	if cfg.Settings["codeFont"] != systemCodeFontStack {
		t.Errorf("codeFont = %q, want %q", cfg.Settings["codeFont"], systemCodeFontStack)
	}
	if cfg.Settings["diffMode"] != "unified" {
		t.Errorf("diffMode = %v, want unified", cfg.Settings["diffMode"])
	}
	if cfg.Settings["ignoreWhitespace"] != true {
		t.Errorf("ignoreWhitespace = %v, want true", cfg.Settings["ignoreWhitespace"])
	}
	if cfg.Settings["commentsPanel"] != "open" {
		t.Errorf("commentsPanel = %v, want open", cfg.Settings["commentsPanel"])
	}
	shortcuts, ok := cfg.Settings["shortcuts"].(map[string]string)
	if !ok {
		t.Fatalf("shortcuts has type %T, want map[string]string", cfg.Settings["shortcuts"])
	}
	if shortcuts["next_block"] != "ArrowDown" || shortcuts["previous_block"] != "ArrowUp" || shortcuts["toggle_toc"] != "" {
		t.Errorf("unexpected shortcuts: %#v", shortcuts)
	}
}

func TestLoadConfigFileSettingsNormalizationAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".crit.config.json")
	content := `{
		"no_open": true,
		"settings": {
			"theme": "sepia",
			"width": "wide",
			"boostContrast": "off",
			"lineNumbers": "on",
			"codeFont": "default",
			"diffMode": 42,
			"ignoreWhitespace": "yes",
			"unknownKey": "hello",
			"customCSS": true,
			"shortcuts": {
				"next_block": "ArrowDown",
				"bogus_action": "x",
				"previous_block": "Shift+/",
				"toggle_file_tree": "Ctrl+Enter",
				"toggle_toc": 123
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	var err error
	stderr := captureStderr(t, func() {
		cfg, _, err = LoadConfigFile(path)
	})
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if !cfg.NoOpen {
		t.Errorf("expected no_open to still load cleanly")
	}
	// Valid keys preserved
	if cfg.Settings["width"] != "wide" {
		t.Errorf("width = %v, want wide", cfg.Settings["width"])
	}
	if cfg.Settings["boostContrast"] != "off" {
		t.Errorf("boostContrast = %v, want off", cfg.Settings["boostContrast"])
	}
	if cfg.Settings["lineNumbers"] != "on" {
		t.Errorf("lineNumbers = %v, want on", cfg.Settings["lineNumbers"])
	}
	if cfg.Settings["codeFont"] != "" {
		t.Errorf("codeFont = %q, want empty string for default", cfg.Settings["codeFont"])
	}
	shortcuts, ok := cfg.Settings["shortcuts"].(map[string]string)
	if !ok {
		t.Fatalf("expected valid shortcut next_block to survive, got %#v", cfg.Settings["shortcuts"])
	}
	if len(shortcuts) != 1 || shortcuts["next_block"] != "ArrowDown" {
		t.Errorf("expected only next_block:ArrowDown, got %#v", shortcuts)
	}

	// Invalid keys skipped
	for _, dropped := range []string{"theme", "diffMode", "ignoreWhitespace", "unknownKey", "customCSS"} {
		if _, exists := cfg.Settings[dropped]; exists {
			t.Errorf("expected %q to be dropped, got %v", dropped, cfg.Settings[dropped])
		}
	}

	// Warnings emitted for every bad entry
	for _, want := range []string{
		`unknown setting settings.unknownKey`,
		`unknown setting settings.customCSS`,
		`invalid settings.theme`,
		`invalid settings.diffMode`,
		`invalid settings.ignoreWhitespace`,
		`unknown shortcut settings.shortcuts.bogus_action`,
		`invalid or reserved shortcut settings.shortcuts.previous_block "Shift+/"`,
		`invalid or reserved shortcut settings.shortcuts.toggle_file_tree "Ctrl+Enter"`,
		`invalid shortcut settings.shortcuts.toggle_toc`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, stderr)
		}
	}
}

func TestLoadConfigFileSettingsCodeFontValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".crit.config.json")

	// Valid custom stack
	if err := os.WriteFile(path, []byte(`{"settings":{"codeFont":"'Fira Code', monospace"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if cfg.Settings["codeFont"] != "'Fira Code', monospace" {
		t.Errorf("codeFont = %q, want custom stack", cfg.Settings["codeFont"])
	}

	// Dangerous CSS injection characters rejected
	if err := os.WriteFile(path, []byte(`{"settings":{"codeFont":"mono; background: url(evil)"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	stderr := captureStderr(t, func() {
		cfg, _, err = LoadConfigFile(path)
	})
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if _, ok := cfg.Settings["codeFont"]; ok {
		t.Errorf("expected unsafe codeFont to be rejected")
	}
	if !strings.Contains(stderr, `invalid settings.codeFont`) {
		t.Errorf("expected warning for unsafe codeFont, got %q", stderr)
	}
}

func TestLoadConfigFileMalformedSettingsDoesNotBreakConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(path, []byte(`{"no_open":true,"settings":"not-an-object","quiet":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	var err error
	stderr := captureStderr(t, func() {
		cfg, _, err = LoadConfigFile(path)
	})
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if !cfg.NoOpen || !cfg.Quiet {
		t.Errorf("expected other config fields to parse cleanly: no_open=%v quiet=%v", cfg.NoOpen, cfg.Quiet)
	}
	if len(cfg.Settings) != 0 {
		t.Errorf("expected empty settings, got %#v", cfg.Settings)
	}
	if !strings.Contains(stderr, `invalid settings`) {
		t.Errorf("expected warning for non-object settings, got %q", stderr)
	}
}

func TestFilterSettingsPalettes(t *testing.T) {
	light := map[string]bool{"github-light": true, "min-light": true}
	dark := map[string]bool{"tokyo-night": true, "github-dark": true}

	settings := map[string]any{
		"lightPalette": "tokyo-night", // wrong mode (dark)
		"darkPalette":  "nonexistent", // unknown
		"theme":        "dark",
	}
	stderr := captureStderr(t, func() {
		FilterSettingsPalettes(settings, light, dark, "/home/u/.crit.config.json")
	})
	if _, ok := settings["lightPalette"]; ok {
		t.Errorf("expected dark palette in lightPalette to be removed")
	}
	if _, ok := settings["darkPalette"]; ok {
		t.Errorf("expected unknown darkPalette to be removed")
	}
	if settings["theme"] != "dark" {
		t.Errorf("unrelated settings should remain untouched")
	}
	if !strings.Contains(stderr, `unknown settings.lightPalette "tokyo-night"`) {
		t.Errorf("expected lightPalette warning, got %q", stderr)
	}
	if !strings.Contains(stderr, `unknown settings.darkPalette "nonexistent"`) {
		t.Errorf("expected darkPalette warning, got %q", stderr)
	}

	valid := map[string]any{
		"lightPalette": "min-light",
		"darkPalette":  "tokyo-night",
	}
	stderr = captureStderr(t, func() {
		FilterSettingsPalettes(valid, light, dark, "/home/u/.crit.config.json")
	})
	if stderr != "" {
		t.Errorf("expected no warnings for valid palettes, got %q", stderr)
	}
	if valid["lightPalette"] != "min-light" || valid["darkPalette"] != "tokyo-night" {
		t.Errorf("expected valid palettes to be preserved: %#v", valid)
	}
}

func TestLoadConfigSettingsGlobalOnly(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	globalPath := filepath.Join(home, ".crit.config.json")
	if err := os.WriteFile(globalPath, []byte(`{"settings":{"theme":"dark","diffMode":"unified"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(project, ".crit.config.json")
	if err := os.WriteFile(projectPath, []byte(`{"settings":{"theme":"light","width":"wide"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	stderr := captureStderr(t, func() {
		cfg = LoadConfig(project)
	})
	if cfg.Settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark from global config", cfg.Settings["theme"])
	}
	if cfg.Settings["diffMode"] != "unified" {
		t.Errorf("diffMode = %v, want unified from global config", cfg.Settings["diffMode"])
	}
	if _, ok := cfg.Settings["width"]; ok {
		t.Errorf("project setting width should not be merged into global settings")
	}
	if !strings.Contains(stderr, "settings in ") || !strings.Contains(stderr, "global-only") {
		t.Errorf("expected global-only warning for project settings, got %q", stderr)
	}
}
