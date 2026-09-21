package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Trace (review/VCS keys):
//   cleanup_on_approve / notify_on_round_ready: *bool parsed by LoadConfigFile
//     with presence bits (explicit null parses as a nil pointer but still
//     counts as present); mergeConfigs copies the project pointer only when
//     the presence bit is set, so an explicit project false can override a
//     global true. Consumers: CleanupOnApproveEnabled() (default true) ->
//     internal/session review_cli.go / plan_cli.go cleanupOnApproval;
//     NotifyOnRoundReadyEnabled() (default false) -> internal/server
//     daemon_cli.go DaemonCLIConfig.NotifyOnRoundReady -> notify.RoundReady.
//   disable_stats: plain bool with presence tracking; project explicit
//     true/false overrides global (same pattern as no_integration_check).
//     Consumers: internal/server daemon_bootstrap.go
//     ShouldRecordStatsOnShutdown and server.go /api/finish stats gate.
//   ignore_patterns / auto_viewed_patterns: unioned on merge (global first).
//     ignore_patterns runtime default [".crit/"] applies only when neither
//     file sets the key. Consumers: FilterIgnored / FilterPathsIgnored and
//     session ignorePatterns; auto_viewed_patterns -> /api/config (server.go),
//     matched client-side only. Both are already covered in config_test.go
//     (TestMergeConfigs_IgnorePatternsUnion, TestLoadConfig*,
//     TestLoadConfigFile_AutoViewedPatterns*, TestMergeConfigs_AutoViewedPatternsUnion);
//     base_branch likewise (TestBaseBranchConfig). This file only adds the
//     LoadConfig-level union/no-default check for auto_viewed_patterns.
//   author: project non-empty wins; empty -> VCS-specific fallback in
//     LoadConfig step 5 ("sl"/"sapling" -> slUserName then git, "jj"/"jujutsu"
//     -> jjUserName then git, default git then sl), each shelling out
//     (sl strips the " <email>" suffix). Consumer: DaemonCLIConfig.Author.
//   vcs: project non-empty wins. Consumers: LoadConfig author fallback and
//     server daemon_cli.go resolveVCSOverride(flag, cfg.VCS) -> vcs.DetectVCS.
//   forge / gitlab_url: preferProjectString (project non-empty wins);
//     applyRemoteReviewDefaults fills empty values after merge (forge=auto,
//     gitlab_url=https://gitlab.com). Consumers: cmd/crit wire.go
//     selectProvider -> forge.DetectKind / gitlab.NewProvider(cfg.GitLabURL).
//   default_markdown_view: invalid values cleared with a stderr warning per
//     file at load (validDefaultMarkdownView); preferProjectString on merge.
//     Consumer: /api/config (server.go) -> frontend initial markdown view.
//     Basic coverage lives in config_test.go (TestLoadConfigFile_DefaultMarkdownView*,
//     TestMergeConfigs_DefaultMarkdownViewProjectOverrides, TestLoadConfig_DefaultMarkdownView*);
//     this file adds the invalid-project-value x merge interaction.

func writeAuditReviewConfig(t *testing.T, dir, raw string) string {
	t.Helper()
	p := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func auditBoolPtr(v bool) *bool { return &v }

// auditTriBool describes one presence-tracked *bool review key.
type auditTriBool struct {
	key          string
	get          func(Config) *bool
	set          func(*Config, *bool)
	presence     func(ConfigPresence) bool
	enabled      func(Config) bool
	unsetDefault bool // Enabled() result when the value is nil
}

func auditTriBoolKeys() []auditTriBool {
	return []auditTriBool{
		{
			key:          "cleanup_on_approve",
			get:          func(c Config) *bool { return c.CleanupOnApprove },
			set:          func(c *Config, v *bool) { c.CleanupOnApprove = v },
			presence:     func(p ConfigPresence) bool { return p.CleanupOnApprove },
			enabled:      func(c Config) bool { return c.CleanupOnApproveEnabled() },
			unsetDefault: true,
		},
		{
			key:          "notify_on_round_ready",
			get:          func(c Config) *bool { return c.NotifyOnRoundReady },
			set:          func(c *Config, v *bool) { c.NotifyOnRoundReady = v },
			presence:     func(p ConfigPresence) bool { return p.NotifyOnRoundReady },
			enabled:      func(c Config) bool { return c.NotifyOnRoundReadyEnabled() },
			unsetDefault: false,
		},
	}
}

// TestAuditReviewTriStateBoolLoad verifies JSON parsing and presence tracking
// for cleanup_on_approve and notify_on_round_ready, including the explicit
// null edge (nil pointer, presence still recorded).
func TestAuditReviewTriStateBoolLoad(t *testing.T) {
	for _, tb := range auditTriBoolKeys() {
		t.Run(tb.key+"/explicit true", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditReviewConfig(t, dir, `{"`+tb.key+`": true}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if got := tb.get(cfg); got == nil || *got != true {
				t.Errorf("%s = %v, want pointer to true", tb.key, got)
			}
			if !tb.presence(presence) {
				t.Errorf("presence for %s = false, want true", tb.key)
			}
			if !tb.enabled(cfg) {
				t.Errorf("%sEnabled() = false, want true", tb.key)
			}
		})
		t.Run(tb.key+"/explicit false", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditReviewConfig(t, dir, `{"`+tb.key+`": false}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if got := tb.get(cfg); got == nil || *got != false {
				t.Errorf("%s = %v, want pointer to false", tb.key, got)
			}
			if !tb.presence(presence) {
				t.Errorf("presence for %s = false, want true (explicit false must be tracked)", tb.key)
			}
			if tb.enabled(cfg) {
				t.Errorf("%sEnabled() = true, want false", tb.key)
			}
		})
		t.Run(tb.key+"/explicit null keeps presence", func(t *testing.T) {
			// null unmarshals to a nil pointer but the key is present, so a
			// project null overrides a global value back to the unset default.
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditReviewConfig(t, dir, `{"`+tb.key+`": null}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if got := tb.get(cfg); got != nil {
				t.Errorf("%s = %v, want nil pointer", tb.key, *got)
			}
			if !tb.presence(presence) {
				t.Errorf("presence for %s = false, want true (null is still present)", tb.key)
			}
			if got := tb.enabled(cfg); got != tb.unsetDefault {
				t.Errorf("%sEnabled() = %v, want unset default %v", tb.key, got, tb.unsetDefault)
			}
		})
		t.Run(tb.key+"/absent", func(t *testing.T) {
			dir := t.TempDir()
			cfg, presence, err := LoadConfigFile(writeAuditReviewConfig(t, dir, `{}`))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if got := tb.get(cfg); got != nil {
				t.Errorf("%s = %v, want nil pointer when absent", tb.key, *got)
			}
			if tb.presence(presence) {
				t.Errorf("presence for %s = true, want false when absent", tb.key)
			}
			if got := tb.enabled(cfg); got != tb.unsetDefault {
				t.Errorf("%sEnabled() = %v, want unset default %v", tb.key, got, tb.unsetDefault)
			}
		})
	}
}

// TestAuditReviewTriStateBoolPresenceMerge verifies mergeConfigs copies the
// project pointer only when the presence bit is set.
func TestAuditReviewTriStateBoolPresenceMerge(t *testing.T) {
	for _, tb := range auditTriBoolKeys() {
		t.Run(tb.key+"/absent project keeps global true", func(t *testing.T) {
			global := Config{}
			tb.set(&global, auditBoolPtr(true))
			merged := mergeConfigs(global, Config{}, ConfigPresence{})
			if got := tb.get(merged); got == nil || *got != true {
				t.Errorf("merged %s = %v, want global pointer to true", tb.key, got)
			}
			if !tb.enabled(merged) {
				t.Errorf("%sEnabled() = false, want true", tb.key)
			}
		})
		t.Run(tb.key+"/absent project keeps global false", func(t *testing.T) {
			global := Config{}
			tb.set(&global, auditBoolPtr(false))
			merged := mergeConfigs(global, Config{}, ConfigPresence{})
			if got := tb.get(merged); got == nil || *got != false {
				t.Errorf("merged %s = %v, want global pointer to false", tb.key, got)
			}
			if tb.enabled(merged) {
				t.Errorf("%sEnabled() = true, want false", tb.key)
			}
		})
		t.Run(tb.key+"/project false overrides global true", func(t *testing.T) {
			global := Config{}
			tb.set(&global, auditBoolPtr(true))
			project := Config{}
			tb.set(&project, auditBoolPtr(false))
			presence := ConfigPresence{}
			setAuditTriBoolPresence(tb, &presence)
			merged := mergeConfigs(global, project, presence)
			if tb.enabled(merged) {
				t.Errorf("%sEnabled() = true, want false (explicit project false must win)", tb.key)
			}
		})
		t.Run(tb.key+"/project true overrides global false", func(t *testing.T) {
			global := Config{}
			tb.set(&global, auditBoolPtr(false))
			project := Config{}
			tb.set(&project, auditBoolPtr(true))
			presence := ConfigPresence{}
			setAuditTriBoolPresence(tb, &presence)
			merged := mergeConfigs(global, project, presence)
			if !tb.enabled(merged) {
				t.Errorf("%sEnabled() = false, want true (explicit project true must win)", tb.key)
			}
		})
		t.Run(tb.key+"/project true without presence bit is ignored", func(t *testing.T) {
			// Guards the presence gate itself: a project value that was never
			// recorded as present (e.g. zero Config from a missing file) must
			// not clobber the global pointer.
			global := Config{}
			tb.set(&global, auditBoolPtr(false))
			project := Config{}
			tb.set(&project, auditBoolPtr(true))
			merged := mergeConfigs(global, project, ConfigPresence{})
			if got := tb.get(merged); got == nil || *got != false {
				t.Errorf("merged %s = %v, want global pointer to false (project without presence ignored)", tb.key, got)
			}
			if tb.enabled(merged) {
				t.Errorf("%sEnabled() = true, want false (global false kept)", tb.key)
			}
		})
		t.Run(tb.key+"/project null resets to unset default", func(t *testing.T) {
			// Current behavior: presence(true) + nil pointer replaces the
			// global pointer with nil, so Enabled() falls back to the default.
			global := Config{}
			tb.set(&global, auditBoolPtr(false))
			presence := ConfigPresence{}
			setAuditTriBoolPresence(tb, &presence)
			merged := mergeConfigs(global, Config{}, presence)
			if got := tb.get(merged); got != nil {
				t.Errorf("merged %s = %v, want nil pointer", tb.key, *got)
			}
			if got := tb.enabled(merged); got != tb.unsetDefault {
				t.Errorf("%sEnabled() = %v, want unset default %v", tb.key, got, tb.unsetDefault)
			}
		})
	}
}

func setAuditTriBoolPresence(tb auditTriBool, p *ConfigPresence) {
	switch tb.key {
	case "cleanup_on_approve":
		p.CleanupOnApprove = true
	case "notify_on_round_ready":
		p.NotifyOnRoundReady = true
	}
}

func TestAuditReviewCleanupOnApproveEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"cleanup_on_approve": true}`)

	// Project explicit false wins over global true.
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"cleanup_on_approve": false}`)
	if got := LoadConfig(projectDir).CleanupOnApproveEnabled(); got {
		t.Error("CleanupOnApproveEnabled() = true, want false (project explicit false wins)")
	}

	// No project config: global true stands.
	emptyDir := t.TempDir()
	if got := LoadConfig(emptyDir).CleanupOnApproveEnabled(); !got {
		t.Error("CleanupOnApproveEnabled() = false, want true (global value)")
	}
}

func TestAuditReviewCleanupOnApproveGlobalFalseEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"cleanup_on_approve": false}`)
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).CleanupOnApproveEnabled(); got {
		t.Error("CleanupOnApproveEnabled() = true, want false (global explicit false survives merge)")
	}
}

func TestAuditReviewNotifyOnRoundReadyEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"notify_on_round_ready": true}`)

	// Global true stands when the project says nothing.
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).NotifyOnRoundReadyEnabled(); !got {
		t.Error("NotifyOnRoundReadyEnabled() = false, want true (global value)")
	}

	// Project explicit false wins over global true.
	writeAuditReviewConfig(t, projectDir, `{"notify_on_round_ready": false}`)
	if got := LoadConfig(projectDir).NotifyOnRoundReadyEnabled(); got {
		t.Error("NotifyOnRoundReadyEnabled() = true, want false (project explicit false wins)")
	}
}

func TestAuditReviewDisableStatsLoad(t *testing.T) {
	dir := t.TempDir()
	cfg, presence, err := LoadConfigFile(writeAuditReviewConfig(t, dir, `{"disable_stats": true}`))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if !cfg.DisableStats {
		t.Error("DisableStats = false, want true")
	}
	if !presence.DisableStats {
		t.Error("presence.DisableStats = false, want true")
	}
}

func TestAuditReviewDisableStatsGlobalEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"disable_stats": true}`)
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).DisableStats; !got {
		t.Error("DisableStats = false, want true (global value reaches server stats gate)")
	}

	// Unset everywhere -> false (stats recording enabled).
	homeDir2 := t.TempDir()
	testutil.SetHome(t, homeDir2)
	if got := LoadConfig(t.TempDir()).DisableStats; got {
		t.Error("DisableStats = true, want false when unset")
	}
}

func TestAuditReviewDisableStatsProjectMerge(t *testing.T) {
	merged := mergeConfigs(Config{}, Config{DisableStats: true}, ConfigPresence{DisableStats: true})
	if !merged.DisableStats {
		t.Error("mergeConfigs dropped project disable_stats=true; want true")
	}

	// Explicit project false overrides global true.
	merged = mergeConfigs(Config{DisableStats: true}, Config{DisableStats: false}, ConfigPresence{DisableStats: true})
	if merged.DisableStats {
		t.Error("mergeConfigs kept global disable_stats; want project explicit false")
	}

	// Absent project key leaves global alone.
	merged = mergeConfigs(Config{DisableStats: true}, Config{}, ConfigPresence{})
	if !merged.DisableStats {
		t.Error("mergeConfigs cleared global disable_stats when project omitted key")
	}

	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"disable_stats": true}`)
	if got := LoadConfig(projectDir).DisableStats; !got {
		t.Error("LoadConfig ignored project-only disable_stats=true; want true")
	}

	writeAuditReviewConfig(t, homeDir, `{"disable_stats": false}`)
	writeAuditReviewConfig(t, projectDir, `{"disable_stats": true}`)
	if got := LoadConfig(projectDir).DisableStats; !got {
		t.Error("LoadConfig ignored project disable_stats over global false; want true")
	}
}

func TestAuditReviewAutoViewedPatternsEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"auto_viewed_patterns": ["*.lock"]}`)
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"auto_viewed_patterns": ["PLAN.md"]}`)

	got := LoadConfig(projectDir).AutoViewedPatterns
	want := []string{"*.lock", "PLAN.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutoViewedPatterns = %v, want union %v (global first)", got, want)
	}
}

func TestAuditReviewAutoViewedPatternsNoRuntimeDefault(t *testing.T) {
	// Unlike ignore_patterns (runtime default [".crit/"]), auto_viewed_patterns
	// has no runtime default: unset everywhere stays empty.
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()
	cfg := LoadConfig(projectDir)
	if len(cfg.AutoViewedPatterns) != 0 {
		t.Errorf("AutoViewedPatterns = %v, want empty when unset", cfg.AutoViewedPatterns)
	}
	if !reflect.DeepEqual(cfg.IgnorePatterns, []string{".crit/"}) {
		t.Errorf("IgnorePatterns = %v, want runtime default [.crit/] in the same load (contrast)", cfg.IgnorePatterns)
	}
}

func TestAuditReviewAuthorMerge(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    string
	}{
		{"project overrides global", "Global Author", "Project Author", "Project Author"},
		{"global kept when project empty", "Global Author", "", "Global Author"},
		{"project sets when global empty", "", "Project Author", "Project Author"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{Author: tt.global}, Config{Author: tt.project}, ConfigPresence{})
			if merged.Author != tt.want {
				t.Errorf("merged.Author = %q, want %q", merged.Author, tt.want)
			}
		})
	}
}

func TestAuditReviewAuthorEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"author": "Global Author"}`)
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"author": "Project Author"}`)
	if got := LoadConfig(projectDir).Author; got != "Project Author" {
		t.Errorf("Author = %q, want Project Author (project wins end-to-end)", got)
	}

	// Empty project config keeps the global author and skips the VCS fallback.
	emptyDir := t.TempDir()
	if got := LoadConfig(emptyDir).Author; got != "Global Author" {
		t.Errorf("Author = %q, want Global Author when project unset", got)
	}
}

// TestAuditReviewAuthorVCSFallback exercises LoadConfig step 5: the author
// fallback chain is selected by the merged vcs key. Shims replace git/sl/jj on
// PATH so the fallback order and the sl email-stripping are deterministic.
func TestAuditReviewAuthorVCSFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git/sl/jj shims are POSIX shell scripts")
	}
	tests := []struct {
		name        string
		projectJSON string
		shims       map[string]string // binary name -> shell body
		want        string
	}{
		{
			name:        "default prefers git",
			projectJSON: `{}`,
			shims:       map[string]string{"git": "echo 'Git Shim User'", "sl": "echo 'Sl Shim User <sl@example.com>'"},
			want:        "Git Shim User",
		},
		{
			name:        "default falls back to sl when git fails",
			projectJSON: `{}`,
			shims:       map[string]string{"git": "exit 1", "sl": "echo 'Sl Shim User <sl@example.com>'"},
			want:        "Sl Shim User",
		},
		{
			name:        "jj preferred when vcs=jj",
			projectJSON: `{"vcs": "jj"}`,
			shims:       map[string]string{"jj": "echo 'JJ Shim User'", "git": "echo 'Git Shim User'"},
			want:        "JJ Shim User",
		},
		{
			name:        "jj falls back to git when jj missing",
			projectJSON: `{"vcs": "jj"}`,
			shims:       map[string]string{"jj": "exit 1", "git": "echo 'Git Shim User'"},
			want:        "Git Shim User",
		},
		{
			name:        "jujutsu alias selects jj",
			projectJSON: `{"vcs": "jujutsu"}`,
			shims:       map[string]string{"jj": "echo 'JJ Shim User'"},
			want:        "JJ Shim User",
		},
		{
			name:        "sl strips email suffix",
			projectJSON: `{"vcs": "sl"}`,
			shims:       map[string]string{"sl": "echo 'Sl Shim User <sl@example.com>'"},
			want:        "Sl Shim User",
		},
		{
			name:        "sapling alias selects sl",
			projectJSON: `{"vcs": "sapling"}`,
			shims:       map[string]string{"sl": "echo 'Sl Shim User <sl@example.com>'"},
			want:        "Sl Shim User",
		},
		{
			name:        "sl falls back to git when sl missing",
			projectJSON: `{"vcs": "sl"}`,
			shims:       map[string]string{"sl": "exit 1", "git": "echo 'Git Shim User'"},
			want:        "Git Shim User",
		},
		{
			name:        "explicit author skips fallback entirely",
			projectJSON: `{"vcs": "jj", "author": "Configured Author"}`,
			shims:       map[string]string{"jj": "echo 'JJ Shim User'", "git": "echo 'Git Shim User'"},
			want:        "Configured Author",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			for name, body := range tt.shims {
				shim := filepath.Join(binDir, name)
				if err := os.WriteFile(shim, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			homeDir := t.TempDir()
			testutil.SetHome(t, homeDir)
			projectDir := t.TempDir()
			writeAuditReviewConfig(t, projectDir, tt.projectJSON)

			if got := LoadConfig(projectDir).Author; got != tt.want {
				t.Errorf("Author = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuditReviewForgeMergeAndEndToEnd(t *testing.T) {
	// Project-override direction is covered by TestForgeConfigMergesProjectOverride
	// (config_test.go); these are the remaining preferProjectString directions.
	tests := []struct {
		name    string
		global  string
		project string
		want    string
	}{
		{"global kept when project empty", "gitlab", "", "gitlab"},
		{"project sets when global empty", "", "gitlab", "gitlab"},
		{"both empty stays empty (default applied later)", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{Forge: tt.global}, Config{Forge: tt.project}, ConfigPresence{})
			if merged.Forge != tt.want {
				t.Errorf("merged.Forge = %q, want %q", merged.Forge, tt.want)
			}
		})
	}

	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"forge": "gitlab", "author": "A"}`)

	// Global value survives LoadConfig; applyRemoteReviewDefaults must not
	// overwrite a configured forge with "auto".
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).Forge; got != "gitlab" {
		t.Errorf("Forge = %q, want gitlab (global kept, default not applied)", got)
	}

	// Project non-empty wins end-to-end.
	writeAuditReviewConfig(t, projectDir, `{"forge": "github"}`)
	if got := LoadConfig(projectDir).Forge; got != "github" {
		t.Errorf("Forge = %q, want github (project wins end-to-end)", got)
	}
}

func TestAuditReviewGitLabURLMergeAndEndToEnd(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    string
	}{
		{"project overrides global", "https://global.gitlab.example.com", "https://project.gitlab.example.com", "https://project.gitlab.example.com"},
		{"global kept when project empty", "https://global.gitlab.example.com", "", "https://global.gitlab.example.com"},
		{"project sets when global empty", "", "https://project.gitlab.example.com", "https://project.gitlab.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{GitLabURL: tt.global}, Config{GitLabURL: tt.project}, ConfigPresence{})
			if merged.GitLabURL != tt.want {
				t.Errorf("merged.GitLabURL = %q, want %q", merged.GitLabURL, tt.want)
			}
		})
	}

	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"gitlab_url": "https://global.gitlab.example.com", "author": "A"}`)

	// Custom global value survives LoadConfig (runtime default only fills empty).
	projectDir := t.TempDir()
	if got := LoadConfig(projectDir).GitLabURL; got != "https://global.gitlab.example.com" {
		t.Errorf("GitLabURL = %q, want custom global value (default must not overwrite)", got)
	}

	// Project non-empty wins end-to-end.
	writeAuditReviewConfig(t, projectDir, `{"gitlab_url": "https://project.gitlab.example.com"}`)
	if got := LoadConfig(projectDir).GitLabURL; got != "https://project.gitlab.example.com" {
		t.Errorf("GitLabURL = %q, want project value (project wins end-to-end)", got)
	}
}

func TestAuditReviewVCSMerge(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    string
	}{
		{"project overrides global", "git", "sl", "sl"},
		{"global kept when project empty", "jj", "", "jj"},
		{"project sets when global empty", "", "jj", "jj"},
		{"both empty stays empty (auto-detect)", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeConfigs(Config{VCS: tt.global}, Config{VCS: tt.project}, ConfigPresence{})
			if merged.VCS != tt.want {
				t.Errorf("merged.VCS = %q, want %q", merged.VCS, tt.want)
			}
		})
	}
}

func TestAuditReviewVCSEndToEnd(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"vcs": "git", "author": "A"}`)
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"vcs": "jj", "author": "A"}`)
	if got := LoadConfig(projectDir).VCS; got != "jj" {
		t.Errorf("VCS = %q, want jj (project wins end-to-end)", got)
	}

	// No project config: global value stands.
	emptyDir := t.TempDir()
	if got := LoadConfig(emptyDir).VCS; got != "git" {
		t.Errorf("VCS = %q, want git when project unset", got)
	}
}

// TestAuditReviewDefaultMarkdownViewInvalidProjectKeepsGlobal covers the
// interaction between per-file validation and preferProjectString: an invalid
// project value is cleared at load, so the valid global value survives instead
// of being overridden by garbage.
func TestAuditReviewDefaultMarkdownViewInvalidProjectKeepsGlobal(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	writeAuditReviewConfig(t, homeDir, `{"default_markdown_view": "document", "author": "A"}`)
	projectDir := t.TempDir()
	writeAuditReviewConfig(t, projectDir, `{"default_markdown_view": "side-by-side"}`)

	if got := LoadConfig(projectDir).DefaultMarkdownView; got != "document" {
		t.Errorf("DefaultMarkdownView = %q, want global document (invalid project value cleared at load)", got)
	}

	// Invalid with no global value stays unset.
	homeDir2 := t.TempDir()
	testutil.SetHome(t, homeDir2)
	projectDir2 := t.TempDir()
	writeAuditReviewConfig(t, projectDir2, `{"default_markdown_view": "DOC"}`)
	if got := LoadConfig(projectDir2).DefaultMarkdownView; got != "" {
		t.Errorf("DefaultMarkdownView = %q, want empty (invalid value ignored)", got)
	}
}
