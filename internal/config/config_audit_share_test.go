package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// These tests audit config merge behavior for sharing, live-mode, prompts and
// hooks keys. They intentionally overlap as little as possible with
// share_targets_test.go and config_test.go.

func TestMergeConfigs_ProjectPromptsOverridesAndExtends(t *testing.T) {
	global := Config{Prompts: map[string]string{
		"on_finish_approved": "global-approved",
		"keep_global":        "global-value",
	}}
	project := Config{Prompts: map[string]string{
		"on_finish_approved": "project-approved",
		"project_only":       "project-value",
	}}

	merged := mergeConfigs(global, project, ConfigPresence{})

	if len(merged.Prompts) != 3 {
		t.Fatalf("prompts = %v, want 3 entries", merged.Prompts)
	}
	if got := merged.Prompts["on_finish_approved"]; got != "project-approved" {
		t.Errorf("on_finish_approved = %q, want project override", got)
	}
	if got := merged.Prompts["keep_global"]; got != "global-value" {
		t.Errorf("keep_global = %q, want global value preserved", got)
	}
	if got := merged.Prompts["project_only"]; got != "project-value" {
		t.Errorf("project_only = %q, want project value", got)
	}
}

func TestMergeConfigs_ProjectPromptsEmptyMapPreservesGlobal(t *testing.T) {
	global := Config{Prompts: map[string]string{"keep": "global"}}
	project := Config{Prompts: map[string]string{}}

	merged := mergeConfigs(global, project, ConfigPresence{})

	if len(merged.Prompts) != 1 || merged.Prompts["keep"] != "global" {
		t.Errorf("prompts = %v, want global preserved", merged.Prompts)
	}
}

func TestMergeConfigs_GlobalPromptsNilAndProjectHasKeys(t *testing.T) {
	global := Config{}
	project := Config{Prompts: map[string]string{"project_only": "value"}}

	merged := mergeConfigs(global, project, ConfigPresence{})

	if len(merged.Prompts) != 1 || merged.Prompts["project_only"] != "value" {
		t.Errorf("prompts = %v, want project map", merged.Prompts)
	}
}

func TestLoadConfig_PromptsProjectOverridesGlobal(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"prompts":{"keep_global":"global","override":"global"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"prompts":{"override":"project","project_only":"project"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if got := cfg.Prompts["override"]; got != "project" {
		t.Errorf("override = %q, want project", got)
	}
	if got := cfg.Prompts["keep_global"]; got != "global" {
		t.Errorf("keep_global = %q, want global", got)
	}
	if got := cfg.Prompts["project_only"]; got != "project" {
		t.Errorf("project_only = %q, want project", got)
	}
}

func TestMergeConfigs_HooksEdgeCases(t *testing.T) {
	t.Run("project overrides per key and preserves global", func(t *testing.T) {
		global := Config{Hooks: map[string]string{
			"on_finish_approved": "inline:global",
			"keep_global":        "inline:global-keep",
		}}
		project := Config{Hooks: map[string]string{
			"on_finish_approved": "inline:project",
			"project_only":       "inline:project-only",
		}}

		merged := mergeConfigs(global, project, ConfigPresence{})
		if len(merged.Hooks) != 3 {
			t.Fatalf("hooks = %v, want 3 entries", merged.Hooks)
		}
		if got := merged.Hooks["on_finish_approved"]; got != "inline:project" {
			t.Errorf("on_finish_approved = %q, want project", got)
		}
		if got := merged.Hooks["keep_global"]; got != "inline:global-keep" {
			t.Errorf("keep_global = %q, want global", got)
		}
		if got := merged.Hooks["project_only"]; got != "inline:project-only" {
			t.Errorf("project_only = %q, want project", got)
		}
	})

	t.Run("empty project map preserves global hooks", func(t *testing.T) {
		global := Config{Hooks: map[string]string{"keep": "global"}}
		project := Config{Hooks: map[string]string{}}

		merged := mergeConfigs(global, project, ConfigPresence{})
		if len(merged.Hooks) != 1 || merged.Hooks["keep"] != "global" {
			t.Errorf("hooks = %v, want global preserved", merged.Hooks)
		}
	})

	t.Run("global nil and project non-nil copies project hooks", func(t *testing.T) {
		global := Config{}
		project := Config{Hooks: map[string]string{"project_only": "inline:project"}}

		merged := mergeConfigs(global, project, ConfigPresence{})
		if len(merged.Hooks) != 1 || merged.Hooks["project_only"] != "inline:project" {
			t.Errorf("hooks = %v, want project map copied", merged.Hooks)
		}
	})
}

func TestLoadConfig_HooksProjectOverridesGlobal(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"hooks":{"keep_global":"inline:global","override":"inline:global"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"hooks":{"override":"inline:project","project_only":"inline:project"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if got := cfg.Hooks["override"]; got != "inline:project" {
		t.Errorf("override = %q, want project", got)
	}
	if got := cfg.Hooks["keep_global"]; got != "inline:global" {
		t.Errorf("keep_global = %q, want global", got)
	}
	if got := cfg.Hooks["project_only"]; got != "inline:project" {
		t.Errorf("project_only = %q, want project", got)
	}
}

func TestMergeConfigs_ProxyAuthProjectIgnored(t *testing.T) {
	global := Config{ProxyAuth: true}
	project := Config{ProxyAuth: false}

	merged := mergeConfigs(global, project, ConfigPresence{})
	if !merged.ProxyAuth {
		t.Error("proxy_auth project value overrode global; want global-only")
	}
}

func TestLoadConfig_ProxyAuthProjectIgnored(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"proxy_auth":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"proxy_auth":false}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if !cfg.ProxyAuth {
		t.Error("proxy_auth = false from project; want global-only true")
	}
}

func TestLoadConfig_LiveProjectOverridesGlobal(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{
		"live_cookie":"global=1",
		"live_cookie_file":"/global/cookies.txt",
		"live_cdp_url":"http://127.0.0.1:9222"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte(`{
		"live_cookie":"project=1",
		"live_cookie_file":".crit/live-cookies.txt",
		"live_cdp_url":"http://127.0.0.1:9333"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if cfg.LiveCookie != "project=1" {
		t.Errorf("live_cookie = %q, want project=1", cfg.LiveCookie)
	}
	if cfg.LiveCookieFile != ".crit/live-cookies.txt" {
		t.Errorf("live_cookie_file = %q, want .crit/live-cookies.txt", cfg.LiveCookieFile)
	}
	if cfg.LiveCDPURL != "http://127.0.0.1:9333" {
		t.Errorf("live_cdp_url = %q, want http://127.0.0.1:9333", cfg.LiveCDPURL)
	}
}

func TestLoadConfig_LiveProjectOnlyFallsBackToGlobal(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"live_cookie":"global=1","live_cookie_file":"/global/cookies.txt","live_cdp_url":"http://127.0.0.1:9222"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"live_cookie":"project=1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if cfg.LiveCookie != "project=1" {
		t.Errorf("live_cookie = %q, want project=1", cfg.LiveCookie)
	}
	if cfg.LiveCookieFile != "/global/cookies.txt" {
		t.Errorf("live_cookie_file = %q, want global fallback", cfg.LiveCookieFile)
	}
	if cfg.LiveCDPURL != "http://127.0.0.1:9222" {
		t.Errorf("live_cdp_url = %q, want global fallback", cfg.LiveCDPURL)
	}
}

func TestLoadConfig_ShareTargetsProjectIgnored(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{
		"share_targets":[{"url":"https://global.example"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte(`{
		"share_targets":[{"url":"https://project.example"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if len(cfg.ShareTargets) != 1 || cfg.ShareTargets[0].URL != "https://global.example" {
		t.Errorf("share_targets = %v, want global target", cfg.ShareTargets)
	}
}

func TestLoadConfig_ProjectOnlyShareTargetsIgnored(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte(`{
		"share_targets":[{"url":"https://project.example"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if len(cfg.ShareTargets) != 1 || cfg.ShareTargets[0].URL != DefaultShareURL {
		t.Errorf("share_targets = %v, want default public target", cfg.ShareTargets)
	}
}

func TestLoadConfig_ShareConsentedProjectIgnored(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"share_consented":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"share_consented":false}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if !cfg.ShareConsented {
		t.Error("share_consented = false from project; want global true")
	}
}

func TestLoadConfig_LiveCookieFileResolvesViaProject(t *testing.T) {
	// Regression guard: live_cookie_file must be overridable from project config
	// so local dev setups can point at a gitignored cookie file.
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"live_cookie_file":".crit/cookies.txt"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(projectDir)
	if cfg.LiveCookieFile != ".crit/cookies.txt" {
		t.Errorf("live_cookie_file = %q, want .crit/cookies.txt", cfg.LiveCookieFile)
	}
}
