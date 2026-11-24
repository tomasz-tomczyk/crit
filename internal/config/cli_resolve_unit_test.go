package config

import (
	"os"
	"testing"
)

func TestResolvePortPrecedence(t *testing.T) {
	rng := PortRange{First: 47800, Last: 47819}
	tests := []struct {
		name     string
		flagPort PortRange
		envPort  string
		cfgPort  PortRange
		want     PortRange
	}{
		{name: "flag beats environment", flagPort: SinglePort(4100), envPort: "4200", cfgPort: SinglePort(4300), want: SinglePort(4100)},
		{name: "environment beats config", envPort: "4200", cfgPort: SinglePort(4300), want: SinglePort(4200)},
		{name: "malformed environment falls back to config", envPort: "not-a-port", cfgPort: SinglePort(4300), want: SinglePort(4300)},
		{name: "reversed environment range falls back to config", envPort: "47819-47800", cfgPort: SinglePort(4300), want: SinglePort(4300)},
		{name: "config is final fallback", cfgPort: SinglePort(4300), want: SinglePort(4300)},
		{name: "flag range beats environment", flagPort: rng, envPort: "4200", want: rng},
		{name: "environment range beats config", envPort: "47800-47819", cfgPort: SinglePort(4300), want: rng},
		{name: "config range is final fallback", cfgPort: rng, want: rng},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CRIT_PORT", tt.envPort)
			if got := ResolvePort(tt.flagPort, tt.cfgPort); got != tt.want {
				t.Fatalf("ResolvePort(%v, %v) = %v, want %v", tt.flagPort, tt.cfgPort, got, tt.want)
			}
		})
	}
}

func TestResolveHostPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		flagHost string
		envHost  string
		cfgHost  string
		want     string
	}{
		{name: "flag beats environment", flagHost: "flag.test", envHost: "env.test", cfgHost: "config.test", want: "flag.test"},
		{name: "environment beats config", envHost: "env.test", cfgHost: "config.test", want: "env.test"},
		{name: "config is final fallback", cfgHost: "config.test", want: "config.test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CRIT_HOST", tt.envHost)
			if got := ResolveHost(tt.flagHost, tt.cfgHost); got != tt.want {
				t.Fatalf("ResolveHost(%q, %q) = %q, want %q", tt.flagHost, tt.cfgHost, got, tt.want)
			}
		})
	}
}

func TestResolveShareURLPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		flagValue  string
		envValue   string
		envPresent bool
		cfgValue   string
		fallback   string
		want       string
	}{
		{name: "flag beats environment", flagValue: "https://flag.test", envValue: "https://env.test", envPresent: true, cfgValue: "https://config.test", fallback: "https://fallback.test", want: "https://flag.test"},
		{name: "environment beats config", envValue: "https://env.test", envPresent: true, cfgValue: "https://config.test", fallback: "https://fallback.test", want: "https://env.test"},
		{name: "explicit empty environment disables sharing", envPresent: true, cfgValue: "https://config.test", fallback: "https://fallback.test", want: ""},
		{name: "config beats fallback", cfgValue: "https://config.test", fallback: "https://fallback.test", want: "https://config.test"},
		{name: "fallback is used last", fallback: "https://fallback.test", want: "https://fallback.test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envPresent {
				t.Setenv("CRIT_SHARE_URL", tt.envValue)
			} else {
				unsetEnvForTest(t, "CRIT_SHARE_URL")
			}
			cfg := Config{ShareURL: tt.cfgValue}
			if got := ResolveShareURL(tt.flagValue, cfg, tt.fallback); got != tt.want {
				t.Fatalf("ResolveShareURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "") // registers restore of the original value
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}
