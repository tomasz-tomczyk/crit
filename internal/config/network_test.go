package config

import (
	"strings"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", false},
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"10.0.0.1", false},
		{"example.com", false},
	}
	for _, tt := range tests {
		if got := IsLoopbackHost(tt.host); got != tt.want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestNeedsUnauthenticatedNetworkAck(t *testing.T) {
	tests := []struct {
		host, publicURL string
		want            bool
	}{
		{"", "", false},
		{"127.0.0.1", "", false},
		{"localhost", "", false},
		{"0.0.0.0", "", true},
		{"10.0.0.1", "", true},
		{"127.0.0.1", "https://mymac.ts.net", true},
		{"0.0.0.0", "https://mymac.ts.net", true},
	}
	for _, tt := range tests {
		if got := NeedsUnauthenticatedNetworkAck(tt.host, tt.publicURL); got != tt.want {
			t.Errorf("NeedsUnauthenticatedNetworkAck(%q, %q) = %v, want %v",
				tt.host, tt.publicURL, got, tt.want)
		}
	}
}

func TestEnvAllowsUnauthenticatedNetwork(t *testing.T) {
	t.Setenv(AllowUnauthenticatedNetworkEnv, "")
	if EnvAllowsUnauthenticatedNetwork() {
		t.Fatal("empty env should be false")
	}
	t.Setenv(AllowUnauthenticatedNetworkEnv, "1")
	if !EnvAllowsUnauthenticatedNetwork() {
		t.Fatal("1 should be true")
	}
	t.Setenv(AllowUnauthenticatedNetworkEnv, "true")
	if !EnvAllowsUnauthenticatedNetwork() {
		t.Fatal("true should be true")
	}
	t.Setenv(AllowUnauthenticatedNetworkEnv, "no")
	if EnvAllowsUnauthenticatedNetwork() {
		t.Fatal("no should be false")
	}
}

func TestErrUnauthenticatedNetwork(t *testing.T) {
	err := ErrUnauthenticatedNetwork("0.0.0.0", "")
	msg := err.Error()
	for _, want := range []string{
		"0.0.0.0",
		AllowUnauthenticatedNetworkFlag,
		AllowUnauthenticatedNetworkEnv,
		"127.0.0.1",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
}
