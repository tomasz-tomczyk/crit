package server

import "testing"

func TestInstallationSourceForPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		goBin  string
		goPath string
		home   string
		want   string
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/crit/0.20.2/bin/crit", "", "", "/Users/me", installationSourceHomebrew},
		{"homebrew opt", "/opt/homebrew/opt/crit/bin/crit", "", "", "/Users/me", installationSourceHomebrew},
		{"nix store", "/nix/store/hash-crit-0.20.2/bin/crit", "", "", "/home/me", installationSourceNix},
		{"default Go bin", "/Users/me/go/bin/crit", "", "", "/Users/me", installationSourceGo},
		{"configured Go bin", "/tools/bin/crit", "/tools/bin", "", "/Users/me", installationSourceGo},
		{"GOPATH bin", "/workspace/bin/crit", "", "/workspace", "/Users/me", installationSourceGo},
		{"downloaded binary", "/usr/local/bin/crit", "", "", "/Users/me", installationSourceUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installationSourceForPath(tt.path, tt.goBin, tt.goPath, tt.home); got != tt.want {
				t.Errorf("installationSourceForPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
