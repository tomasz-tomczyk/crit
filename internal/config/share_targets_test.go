package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestResolveShareTargetsCompatibilityMatrix(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"absent uses public", Config{}, []string{DefaultShareURL}},
		{"legacy custom", Config{ShareURL: "https://Example.COM/", shareURLPresent: true}, []string{"https://example.com"}},
		{"legacy empty disables", Config{shareURLPresent: true}, []string{}},
		{"new empty disables", Config{ShareTargets: []ShareTarget{}, shareTargetsPresent: true, ShareURL: "https://ignored.example", shareURLPresent: true}, []string{}},
		{"new authoritative", Config{ShareTargets: []ShareTarget{{URL: "https://Acme.example/"}}, shareTargetsPresent: true, AuthToken: "legacy"}, []string{"https://acme.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets, err := ResolveShareTargets(tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(targets) != len(tt.want) {
				t.Fatalf("targets=%v, want %v", targets, tt.want)
			}
			for i := range targets {
				if targets[i].URL != tt.want[i] {
					t.Errorf("url=%q, want %q", targets[i].URL, tt.want[i])
				}
			}
		})
	}
}

func TestCanonicalShareURLSecurity(t *testing.T) {
	got, err := CanonicalShareURL("HTTPS://Example.COM:443/crit/")
	if err != nil || got != "https://example.com/crit" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, raw := range []string{"ftp://example.com", "http://example.com", "https://u:p@example.com", "https://example.com?q=1", "https://example.com/#x"} {
		if _, err := CanonicalShareURL(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
	www, _ := CanonicalShareURL("https://www.crit.md")
	if www == DefaultShareURL {
		t.Fatal("www hostname was aliased")
	}
}

func TestResolveShareTargetsRejectsDuplicatesAndDefaults(t *testing.T) {
	_, err := ResolveShareTargets(Config{ShareTargets: []ShareTarget{{URL: "https://A.example"}, {URL: "https://a.example/"}}})
	if err == nil {
		t.Fatal("duplicate canonical URLs accepted")
	}
	_, err = ResolveShareTargets(Config{ShareTargets: []ShareTarget{{URL: "https://a.example", Default: true}, {URL: "https://b.example", Default: true}}})
	if err == nil {
		t.Fatal("multiple defaults accepted")
	}
}

func TestSelectShareTargetEnvironmentPresence(t *testing.T) {
	t.Setenv("CRIT_SHARE_URL", "")
	_, ok, err := SelectShareTarget("", false, Config{ShareTargets: []ShareTarget{{URL: DefaultShareURL}}})
	if err != nil || ok {
		t.Fatalf("empty env should disable: ok=%v err=%v", ok, err)
	}
	t.Setenv("CRIT_SHARE_URL", DefaultShareURL)
	t.Setenv("CRIT_AUTH_TOKEN", "")
	target, ok, err := SelectShareTarget("", false, Config{ShareTargets: []ShareTarget{{URL: DefaultShareURL, Auth: TargetAuth{Token: "cached"}}}})
	if err != nil || !ok || target.Auth.Token != "" {
		t.Fatalf("empty auth env did not suppress token: %#v %v", target, err)
	}
}

func TestSameOriginRedirectPolicy(t *testing.T) {
	original, _ := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	same, _ := http.NewRequest(http.MethodGet, "https://EXAMPLE.com:443/next", nil)
	if err := SameOriginRedirectPolicy(same, []*http.Request{original}); err != nil {
		t.Fatal(err)
	}
	cross, _ := http.NewRequest(http.MethodGet, "https://other.example/next", nil)
	var redirectErr *CrossOriginRedirectError
	if err := SameOriginRedirectPolicy(cross, []*http.Request{original}); !errors.As(err, &redirectErr) {
		t.Fatalf("got %v", err)
	}
}

func TestInferShareBaseURLBoundaryAndLongest(t *testing.T) {
	targets := []ShareTarget{{URL: "https://example.com/crit"}, {URL: "https://example.com/crit/team"}}
	if got, ok := InferShareBaseURL("https://example.com/crit/team/r/x", targets); !ok || got != targets[1].URL {
		t.Fatalf("got %q %v", got, ok)
	}
	if got, ok := InferShareBaseURL("https://example.com/crit-extra/r/x", targets); ok || got != "" {
		t.Fatalf("boundary matched %q", got)
	}
	if got, ok := InferShareBaseURL("https://root.example/r/tok", nil); !ok || got != "https://root.example" {
		t.Fatalf("root inference got %q %v", got, ok)
	}
}

func TestCrossOriginRedirectErrorMessage(t *testing.T) {
	err := &CrossOriginRedirectError{From: "https://a.example", To: "https://b.example"}
	if got := err.Error(); got != "refusing cross-origin redirect from https://a.example to https://b.example" {
		t.Fatalf("Error()=%q", got)
	}
}

func TestCanonicalShareURLDevelopmentHTTP(t *testing.T) {
	got, err := CanonicalShareURL("http://localhost:4001/crit/")
	if err != nil || got != "http://localhost:4001/crit" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = CanonicalShareURL("http://127.0.0.1:3133")
	if err != nil || got != "http://127.0.0.1:3133" {
		t.Fatalf("loopback got %q %v", got, err)
	}
	if _, err := CanonicalShareURL(""); err == nil {
		t.Fatal("empty URL accepted")
	}
}

func TestSelectShareTargetDefaultsAndEphemeral(t *testing.T) {
	orig, had := os.LookupEnv("CRIT_SHARE_URL")
	os.Unsetenv("CRIT_SHARE_URL")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("CRIT_SHARE_URL", orig)
		} else {
			_ = os.Unsetenv("CRIT_SHARE_URL")
		}
	})
	cfg := Config{ShareTargets: []ShareTarget{
		{URL: "https://a.example"},
		{URL: "https://b.example", Default: true, Auth: TargetAuth{Token: "b"}},
	}, shareTargetsPresent: true}
	target, ok, err := SelectShareTarget("", false, cfg)
	if err != nil || !ok || target.URL != "https://b.example" || target.Auth.Token != "b" {
		t.Fatalf("default select=%#v ok=%v err=%v", target, ok, err)
	}

	_, ok, err = SelectShareTarget("", false, Config{ShareTargets: []ShareTarget{
		{URL: "https://a.example"}, {URL: "https://b.example"},
	}, shareTargetsPresent: true})
	if err == nil || ok {
		t.Fatal("expected multi-target without default to error")
	}

	target, ok, err = SelectShareTarget("https://ephemeral.example/path/", true, cfg)
	if err != nil || !ok || target.URL != "https://ephemeral.example/path" || target.Name == "" {
		t.Fatalf("ephemeral=%#v ok=%v err=%v", target, ok, err)
	}

	target, ok, err = SelectShareTarget("", false, Config{ShareTargets: []ShareTarget{{URL: "https://only.example"}}, shareTargetsPresent: true})
	if err != nil || !ok || target.URL != "https://only.example" {
		t.Fatalf("single=%#v ok=%v err=%v", target, ok, err)
	}
}

func TestFindShareTargetAndNeedsConsent(t *testing.T) {
	cfg := Config{ShareTargets: []ShareTarget{
		{URL: DefaultShareURL, ShareConsented: false},
		{URL: "https://acme.example", Auth: TargetAuth{Token: "x"}},
	}, shareTargetsPresent: true}
	target, ok, err := FindShareTarget(cfg, "https://ACME.example/")
	if err != nil || !ok || target.Auth.Token != "x" {
		t.Fatalf("find=%#v ok=%v err=%v", target, ok, err)
	}
	if _, ok, err := FindShareTarget(cfg, "https://missing.example"); err != nil || ok {
		t.Fatalf("missing should be ok=false, got ok=%v err=%v", ok, err)
	}
	if _, ok, err := FindShareTarget(cfg, "ftp://bad"); err == nil || ok {
		t.Fatal("invalid URL should error")
	}

	public := ShareTarget{URL: DefaultShareURL}
	if !public.NeedsShareConsent() {
		t.Fatal("public without consent needs consent")
	}
	public.ShareConsented = true
	if public.NeedsShareConsent() {
		t.Fatal("consented public should not need consent")
	}
	if (ShareTarget{URL: "https://acme.example"}).NeedsShareConsent() {
		t.Fatal("self-hosted should not need public consent")
	}
}

func TestMutateShareTargetsMigratesLegacyAndPersists(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	initial := `{
		"share_url":"https://legacy.example",
		"auth_token":"legacy-tok",
		"share_consented":true,
		"custom_key":true
	}`
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MutateShareTargets(func(targets *[]ShareTarget) error {
		(*targets)[0].Name = "Legacy"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"share_url", "auth_token", "share_consented"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("%s still present after migrate", key)
		}
	}
	if _, ok := raw["custom_key"]; !ok {
		t.Fatal("custom_key should be preserved")
	}
	var targets []ShareTarget
	if err := json.Unmarshal(raw["share_targets"], &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].URL != "https://legacy.example" || targets[0].Name != "Legacy" || targets[0].Auth.Token != "legacy-tok" {
		t.Fatalf("targets=%#v", targets)
	}
}

func TestSaveTargetConsent(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	if err := SaveTargetConsent("https://acme.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".crit.config.json")); !os.IsNotExist(err) {
		t.Fatal("non-public consent should not create config")
	}

	if err := SaveTargetConsent(DefaultShareURL); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if string(raw["share_consented"]) != "true" {
		t.Fatalf("share_consented=%s", raw["share_consented"])
	}

	targetsJSON := fmt.Sprintf(`{"share_targets":[{"url":%q,"share_consented":false},{"url":"https://acme.example"}]}`, DefaultShareURL)
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(targetsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTargetConsent(DefaultShareURL); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var cfg struct {
		ShareTargets []ShareTarget `json:"share_targets"`
	}
	_ = json.Unmarshal(data, &cfg)
	if len(cfg.ShareTargets) != 2 || !cfg.ShareTargets[0].ShareConsented || cfg.ShareTargets[1].ShareConsented {
		t.Fatalf("consent targets=%#v", cfg.ShareTargets)
	}
}
