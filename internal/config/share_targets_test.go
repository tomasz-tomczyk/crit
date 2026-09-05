package config

import (
	"errors"
	"net/http"
	"testing"
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
}
