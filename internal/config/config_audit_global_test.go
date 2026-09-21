package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestAuditGlobalOnlyConfigProjectCannotOverride(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()

	writeAuditConfig(t, filepath.Join(homeDir, ".crit.config.json"), `{
		"agent_cmd":"global-agent",
		"auth_token":"global-token",
		"auth_user_name":"Global User",
		"auth_user_email":"global@example.com",
		"auth_user_id":"global-id",
		"plan_approve_mode":"acceptEdits",
		"close_on_approve_after_ms":2500,
		"public_url":"https://global-public.example.com",
		"open_cmd":"global-open",
		"share_url":"https://global-share.example.com"
	}`)
	writeAuditConfig(t, filepath.Join(projectDir, ".crit.config.json"), `{
		"agent_cmd":"project-agent",
		"auth_token":"project-token",
		"auth_user_name":"Project User",
		"auth_user_email":"project@example.com",
		"auth_user_id":"project-id",
		"plan_approve_mode":"bypassPermissions",
		"close_on_approve_after_ms":1,
		"public_url":"https://project-public.example.com",
		"open_cmd":"project-open",
		"share_url":"https://project-share.example.com"
	}`)

	cfg := LoadConfig(projectDir)
	assertAuditGlobalValues(t, cfg)
}

func TestAuditGlobalOnlyConfigProjectCannotEnable(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()

	writeAuditConfig(t, filepath.Join(projectDir, ".crit.config.json"), `{
		"agent_cmd":"project-agent",
		"auth_token":"project-token",
		"auth_user_name":"Project User",
		"auth_user_email":"project@example.com",
		"auth_user_id":"project-id",
		"plan_approve_mode":"bypassPermissions",
		"close_on_approve_after_ms":1,
		"public_url":"https://project-public.example.com",
		"open_cmd":"project-open",
		"share_url":"https://project-share.example.com"
	}`)

	cfg := LoadConfig(projectDir)
	checks := []struct {
		key string
		got string
	}{
		{"agent_cmd", cfg.AgentCmd},
		{"auth_token", cfg.AuthToken},
		{"auth_user_name", cfg.AuthUserName},
		{"auth_user_email", cfg.AuthUserEmail},
		{"auth_user_id", cfg.AuthUserID},
		{"plan_approve_mode", cfg.PlanApproveMode},
		{"public_url", cfg.PublicURL},
		{"open_cmd", cfg.OpenCmd},
	}
	for _, check := range checks {
		if check.got != "" {
			t.Errorf("%s enabled by project config: got %q, want empty", check.key, check.got)
		}
	}
	if cfg.CloseOnApproveAfterMs != nil {
		t.Errorf("close_on_approve_after_ms enabled by project config: got %d, want unset", *cfg.CloseOnApproveAfterMs)
	}
	if cfg.ShareURL != DefaultShareURL {
		t.Errorf("share_url enabled by project config: got %q, want default %q", cfg.ShareURL, DefaultShareURL)
	}
	if len(cfg.ShareTargets) != 1 || cfg.ShareTargets[0].URL != DefaultShareURL {
		t.Fatalf("project share_url affected resolved targets: got %+v", cfg.ShareTargets)
	}
	if got := cfg.ShareTargets[0].Auth; got != (TargetAuth{}) {
		t.Errorf("project credentials attached to default share target: got %+v", got)
	}
}

func TestAuditGlobalLegacyAuthAttachedToSelectedShareTarget(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	projectDir := t.TempDir()
	writeAuditConfig(t, filepath.Join(homeDir, ".crit.config.json"), `{
		"share_url":"https://global-share.example.com",
		"auth_token":"global-token",
		"auth_user_name":"Global User",
		"auth_user_email":"global@example.com",
		"auth_user_id":"global-id"
	}`)
	t.Setenv("CRIT_AUTH_TOKEN", "global-token")

	cfg := LoadConfig(projectDir)
	want := TargetAuth{
		Token:     "global-token",
		UserName:  "Global User",
		UserEmail: "global@example.com",
		UserID:    "global-id",
	}
	if len(cfg.ShareTargets) != 1 || cfg.ShareTargets[0].Auth != want {
		t.Fatalf("resolved legacy target auth = %+v, want %+v", cfg.ShareTargets, want)
	}
	target, selected, err := SelectShareTarget("", false, cfg)
	if err != nil {
		t.Fatalf("SelectShareTarget() error: %v", err)
	}
	if !selected {
		t.Fatal("SelectShareTarget() selected = false, want true")
	}
	if target.Auth != want {
		t.Errorf("selected target auth = %+v, want %+v", target.Auth, want)
	}
}

func TestAuditConfigStringRedactsAuthToken(t *testing.T) {
	cfg := Config{
		AuthToken: "legacy-secret",
		ShareTargets: []ShareTarget{{
			URL:  DefaultShareURL,
			Auth: TargetAuth{Token: "target-secret"},
		}},
	}

	got := cfg.String()
	for _, secret := range []string{"legacy-secret", "target-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("Config.String() exposed token %q: %s", secret, got)
		}
	}
	if count := strings.Count(got, `"[redacted]"`); count != 2 {
		t.Errorf("Config.String() redaction count = %d, want 2: %s", count, got)
	}
	if strings.Contains(DefaultConfigString(), `"auth_token"`) {
		t.Errorf("DefaultConfigString() contains auth_token: %s", DefaultConfigString())
	}
}

func writeAuditConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
}

func assertAuditGlobalValues(t *testing.T, cfg Config) {
	t.Helper()
	checks := []struct {
		key  string
		got  string
		want string
	}{
		{"agent_cmd", cfg.AgentCmd, "global-agent"},
		{"auth_token", cfg.AuthToken, "global-token"},
		{"auth_user_name", cfg.AuthUserName, "Global User"},
		{"auth_user_email", cfg.AuthUserEmail, "global@example.com"},
		{"auth_user_id", cfg.AuthUserID, "global-id"},
		{"plan_approve_mode", cfg.PlanApproveMode, "acceptEdits"},
		{"public_url", cfg.PublicURL, "https://global-public.example.com"},
		{"open_cmd", cfg.OpenCmd, "global-open"},
		{"share_url", cfg.ShareURL, "https://global-share.example.com"},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %q, want global value %q", check.key, check.got, check.want)
		}
	}
	if cfg.CloseOnApproveAfterMs == nil || *cfg.CloseOnApproveAfterMs != 2500 {
		t.Errorf("close_on_approve_after_ms = %v, want global value 2500", cfg.CloseOnApproveAfterMs)
	}
}
