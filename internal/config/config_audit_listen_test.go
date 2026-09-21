package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Trace (listen/CLI keys):
//   LoadConfigFile (config.go) parses each key from JSON + records presence
//     for no_open, quiet, no_integration_check, no_update_check.
//   mergeConfigs overlays project on global:
//     port: project wins when != 0; output: project wins when != "";
//     host: NEVER merged (global-only, DNS-rebinding defense);
//     no_open/quiet/no_integration_check/no_update_check: project wins when
//     presence bit set (so explicit false overrides global true).
//   LoadConfig applies Host default "127.0.0.1" when empty.
//   Consumers (cli_resolve.go + call sites):
//     port/output/host -> ResolvePort/ResolveHost/ResolveOutputDir
//     (flag > env > config), used by internal/live, internal/session plan_cli,
//     internal/preview; no_open -> f.noOpen || cfg.NoOpen; quiet ->
//     pc.quiet || cfg.Quiet -> connectOrStartDaemon quiet suppression;
//     no_integration_check/no_update_check gate tips/update in cmd/crit/cli_serve.go.

func writeAuditListenConfig(t *testing.T, dir, raw string) string {
	t.Helper()
	p := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAuditListenPortMerge(t *testing.T) {
	tests := []struct {
		name        string
		globalPort  int
		projectPort int
		want        int
	}{
		{"both unset stays zero (random port)", 0, 0, 0},
		{"global kept when project unset", 3000, 0, 3000},
		{"project overrides global", 3000, 8080, 8080},
		{"project sets when global unset", 0, 8080, 8080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{Port: tt.globalPort}, Config{Port: tt.projectPort}, ConfigPresence{})
			if merged.Port != tt.want {
				t.Errorf("merged.Port = %d, want %d", merged.Port, tt.want)
			}
		})
	}
}

func TestAuditListenPortLoadFromJSON(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := LoadConfigFile(writeAuditListenConfig(t, dir, `{"port": 3456}`))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Port != 3456 {
		t.Errorf("Port = %d, want 3456", cfg.Port)
	}

	// Absent key loads as zero (random port) without error.
	dir2 := t.TempDir()
	cfg2, _, err := LoadConfigFile(writeAuditListenConfig(t, dir2, `{}`))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg2.Port != 0 {
		t.Errorf("Port = %d, want 0 when unset", cfg2.Port)
	}
}

func TestAuditListenOutputMerge(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    string
	}{
		{"both unset stays empty", "", "", ""},
		{"global kept when project empty", "/global", "", "/global"},
		{"project overrides global", "/global", "/project", "/project"},
		{"project sets when global unset", "", "/project", "/project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{Output: tt.global}, Config{Output: tt.project}, ConfigPresence{})
			if merged.Output != tt.want {
				t.Errorf("merged.Output = %q, want %q", merged.Output, tt.want)
			}
		})
	}
}

func TestAuditListenHostMergeProjectIgnored(t *testing.T) {
	// Even with explicit presence, project host must not win (global-only).
	global := Config{Host: "127.0.0.1"}
	project := Config{Host: "0.0.0.0"}
	merged := mergeConfigs(global, project, ConfigPresence{})
	if merged.Host != "127.0.0.1" {
		t.Errorf("merged.Host = %q, want global 127.0.0.1 (project must not override)", merged.Host)
	}

	// Empty global + hostile project still yields empty here; LoadConfig
	// later defaults it to 127.0.0.1 (covered by TestAuditListenHostDefault).
	merged2 := mergeConfigs(Config{}, Config{Host: "0.0.0.0"}, ConfigPresence{})
	if merged2.Host != "" {
		t.Errorf("merged.Host = %q, want empty (project ignored, default applied later)", merged2.Host)
	}
}

func TestAuditListenHostLoadAndDefault(t *testing.T) {
	// Global host loads verbatim.
	dir := t.TempDir()
	cfg, _, err := LoadConfigFile(writeAuditListenConfig(t, dir, `{"host": "127.0.0.1"}`))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}

	// No config anywhere -> LoadConfig defaults host to loopback.
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).Host; got != "127.0.0.1" {
		t.Errorf("LoadConfig Host = %q, want default 127.0.0.1", got)
	}
}

func TestAuditListenHostProjectIgnoredEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"host": "127.0.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"host": "0.0.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).Host; got != "127.0.0.1" {
		t.Errorf("Host = %q, want global 127.0.0.1 (project override ignored)", got)
	}
}

func TestAuditListenHostProjectAloneCannotSet(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"host": "0.0.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).Host; got != "127.0.0.1" {
		t.Errorf("Host = %q, want default 127.0.0.1 (project-only host ignored)", got)
	}
}

// TestAuditListenBoolPresenceLoad verifies explicit false still records
// presence for all four presence-tracked listen keys.
func TestAuditListenBoolPresenceLoad(t *testing.T) {
	tests := []struct {
		key      string
		get      func(Config) bool
		presence func(ConfigPresence) bool
	}{
		{"no_open", func(c Config) bool { return c.NoOpen }, func(p ConfigPresence) bool { return p.NoOpen }},
		{"quiet", func(c Config) bool { return c.Quiet }, func(p ConfigPresence) bool { return p.Quiet }},
		{"no_integration_check", func(c Config) bool { return c.NoIntegrationCheck }, func(p ConfigPresence) bool { return p.NoIntegrationCheck }},
		{"no_update_check", func(c Config) bool { return c.NoUpdateCheck }, func(p ConfigPresence) bool { return p.NoUpdateCheck }},
	}
	for _, tt := range tests {
		t.Run(tt.key+"/explicit true", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditListenConfig(t, dir, `{"`+tt.key+`": true}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if !tt.get(cfg) {
				t.Errorf("%s = false, want true", tt.key)
			}
			if !tt.presence(presence) {
				t.Errorf("presence for %s = false, want true", tt.key)
			}
		})
		t.Run(tt.key+"/explicit false keeps presence", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditListenConfig(t, dir, `{"`+tt.key+`": false}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if tt.get(cfg) {
				t.Errorf("%s = true, want false", tt.key)
			}
			if !tt.presence(presence) {
				t.Errorf("presence for %s = false, want true (explicit false must be tracked)", tt.key)
			}
		})
		t.Run(tt.key+"/absent has no presence", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditListenConfig(t, dir, `{}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if tt.get(cfg) {
				t.Errorf("%s = true, want false when absent", tt.key)
			}
			if tt.presence(presence) {
				t.Errorf("presence for %s = true, want false when absent", tt.key)
			}
		})
	}
}

// TestAuditListenBoolFalseOverride verifies explicit project false overrides
// global true (and vice versa) for every presence-tracked listen key.
func TestAuditListenBoolFalseOverride(t *testing.T) {
	tests := []struct {
		key    string
		setCfg func(*Config, bool)
		setPrs func(*ConfigPresence)
		get    func(Config) bool
	}{
		{"no_open",
			func(c *Config, v bool) { c.NoOpen = v },
			func(p *ConfigPresence) { p.NoOpen = true },
			func(c Config) bool { return c.NoOpen }},
		{"quiet",
			func(c *Config, v bool) { c.Quiet = v },
			func(p *ConfigPresence) { p.Quiet = true },
			func(c Config) bool { return c.Quiet }},
		{"no_integration_check",
			func(c *Config, v bool) { c.NoIntegrationCheck = v },
			func(p *ConfigPresence) { p.NoIntegrationCheck = true },
			func(c Config) bool { return c.NoIntegrationCheck }},
		{"no_update_check",
			func(c *Config, v bool) { c.NoUpdateCheck = v },
			func(p *ConfigPresence) { p.NoUpdateCheck = true },
			func(c Config) bool { return c.NoUpdateCheck }},
	}
	for _, tt := range tests {
		t.Run(tt.key+"/project false overrides global true", func(t *testing.T) {
			global := Config{}
			tt.setCfg(&global, true)
			project := Config{}
			tt.setCfg(&project, false)
			presence := ConfigPresence{}
			tt.setPrs(&presence)
			if got := tt.get(mergeConfigs(global, project, presence)); got {
				t.Errorf("%s = true, want false (explicit project false must win)", tt.key)
			}
		})
		t.Run(tt.key+"/project true overrides global false", func(t *testing.T) {
			global := Config{}
			tt.setCfg(&global, false)
			project := Config{}
			tt.setCfg(&project, true)
			presence := ConfigPresence{}
			tt.setPrs(&presence)
			if got := tt.get(mergeConfigs(global, project, presence)); !got {
				t.Errorf("%s = false, want true (explicit project true must win)", tt.key)
			}
		})
		t.Run(tt.key+"/absent project keeps global true", func(t *testing.T) {
			global := Config{}
			tt.setCfg(&global, true)
			if got := tt.get(mergeConfigs(global, Config{}, ConfigPresence{})); !got {
				t.Errorf("%s = false, want true (absent project must not clear global)", tt.key)
			}
		})
	}
}

func TestAuditListenNoOpenEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"no_open": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"no_open": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).NoOpen; got {
		t.Errorf("NoOpen = true, want false (project explicit false wins end-to-end)")
	}
}

func TestAuditListenQuietEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"quiet": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"quiet": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).Quiet; got {
		t.Errorf("Quiet = true, want false (project explicit false wins end-to-end)")
	}
}

func TestAuditListenNoChecksEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"no_integration_check": true, "no_update_check": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"no_integration_check": false, "no_update_check": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(projectDir)
	if cfg.NoIntegrationCheck {
		t.Errorf("NoIntegrationCheck = true, want false (project explicit false wins)")
	}
	if cfg.NoUpdateCheck {
		t.Errorf("NoUpdateCheck = true, want false (project explicit false wins)")
	}
}

func TestAuditListenOutputEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"output": "/global/out"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"output": "/project/out"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).Output; got != "/project/out" {
		t.Errorf("Output = %q, want /project/out", got)
	}

	// Empty project output keeps global.
	projectDir2 := t.TempDir()
	if got := LoadConfig(projectDir2).Output; got != "/global/out" {
		t.Errorf("Output = %q, want global /global/out when project unset", got)
	}
}

func TestAuditListenPortEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"port": 3000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"port": 8080}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(projectDir).Port; got != 8080 {
		t.Errorf("Port = %d, want 8080 (project wins end-to-end)", got)
	}
}

func TestAuditListenResolveFromMergedConfig(t *testing.T) {
	unsetEnvForTest(t, "CRIT_PORT")
	unsetEnvForTest(t, "CRIT_HOST")

	merged := mergeConfigs(Config{Port: 3000, Host: "127.0.0.1", Output: "/global/out"},
		Config{Port: 8080, Host: "0.0.0.0", Output: "/project/out"}, ConfigPresence{})
	// Host merge ignores the hostile project value; port/output take project.
	if got := ResolvePort(0, merged.Port); got != 8080 {
		t.Errorf("ResolvePort(0, merged) = %d, want 8080", got)
	}
	if got := ResolveHost("", merged.Host); got != "127.0.0.1" {
		t.Errorf("ResolveHost = %q, want 127.0.0.1", got)
	}
	if got := ResolveOutputDir("", merged); got != "/project/out" {
		t.Errorf("ResolveOutputDir = %q, want /project/out", got)
	}
	if got := ResolveOutputDir("/explicit", merged); got != "/explicit" {
		t.Errorf("ResolveOutputDir explicit = %q, want /explicit", got)
	}
}
