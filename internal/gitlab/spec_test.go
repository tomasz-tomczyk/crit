package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func TestParseMRSpec(t *testing.T) {
	tests := []struct {
		input, project, host string
		number               int
	}{
		{"42", "", "", 42},
		{"https://gitlab.com/acme/widget/-/merge_requests/7", "acme/widget", "gitlab.com", 7},
		{"https://gitlab.example.com/a/b/widget/-/merge_requests/9/diffs", "a/b/widget", "gitlab.example.com", 9},
	}
	for _, tt := range tests {
		got, err := ParseMRSpec(tt.input)
		if err != nil {
			t.Fatalf("ParseMRSpec(%q): %v", tt.input, err)
		}
		if got.Number != tt.number || got.Project != tt.project || got.Host != tt.host {
			t.Errorf("ParseMRSpec(%q) = %+v", tt.input, got)
		}
	}
}

func TestNewProviderUsesSingleConfiguredHost(t *testing.T) {
	provider, err := NewProvider("")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Host != "gitlab.com" {
		t.Fatalf("default host = %q, want gitlab.com", provider.Host)
	}

	provider, err = NewProvider("https://gitlab.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Host != "gitlab.example.com:8443" {
		t.Fatalf("configured host = %q", provider.Host)
	}
	if _, err := provider.changeID(forge.ChangeID{Number: 7, Host: "gitlab.com"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched URL host error = %v", err)
	}
}

func TestResolveChangeIDRequiresURLHostToMatchConfig(t *testing.T) {
	_, err := resolveChangeID(context.Background(), forge.RepoContext{Host: "gitlab.example.com"}, "https://gitlab.com/acme/widget/-/merge_requests/7")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("resolve mismatch error = %v", err)
	}
}

func TestParseMRSpecRejectsInvalid(t *testing.T) {
	for _, input := range []string{"0", "abc", "https://gitlab.com/acme/widget/issues/1"} {
		if _, err := ParseMRSpec(input); err == nil {
			t.Errorf("ParseMRSpec(%q) unexpectedly succeeded", input)
		}
	}
}
