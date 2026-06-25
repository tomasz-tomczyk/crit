package config

import (
	"os"
	"testing"
)

func TestNormalizePublicURL(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"https://mymac.ts.net", "https://mymac.ts.net", false},
		{"https://mymac.ts.net/", "https://mymac.ts.net", false},
		{"https://mymac.ts.net/design", "https://mymac.ts.net/design", false},
		{"https://mymac.ts.net/design/", "https://mymac.ts.net/design", false},
		{"http://localhost:8080", "", true},
		{"https://user:pass@example.com", "", true},
		{"https://127.0.0.1", "", true},
		{"https://[::1]", "", true},
		{"https://example.com:443/path/", "https://example.com:443/path", false},
		{"ftp://example.com", "", true},
		{"not-a-url", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizePublicURL(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizePublicURL(%q) = %q, want error", tt.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePublicURL(%q): %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizePublicURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestPublicURLHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"", ""},
		{"https://mymac.tail-scale.ts.net", "mymac.tail-scale.ts.net"},
		{"https://mymac.ts.net/design", "mymac.ts.net"},
		{"https://localhost", ""},
		{"https://127.0.0.1", ""},
		{"https://[::1]", ""},
		{"not-a-url", ""},
		{"http://localhost:3000", ""},
	}
	for _, tt := range tests {
		if got := PublicURLHost(tt.url); got != tt.want {
			t.Errorf("PublicURLHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestResolvePublicURLPrecedence(t *testing.T) {
	cfg := Config{PublicURL: "https://config.example.com"}
	t.Setenv("CRIT_PUBLIC_URL", "https://env.example.com")

	if got := ResolvePublicURL("https://cli.example.com", cfg); got != "https://cli.example.com" {
		t.Errorf("flag precedence: got %q", got)
	}
	if got := ResolvePublicURL("", cfg); got != "https://env.example.com" {
		t.Errorf("env precedence: got %q", got)
	}

	os.Unsetenv("CRIT_PUBLIC_URL")
	if got := ResolvePublicURL("", cfg); got != "https://config.example.com" {
		t.Errorf("config precedence: got %q", got)
	}
}
