package session

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

func TestReconnectCommand(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"839f3b4cd5d6", "crit --session 839f3b4cd5d6"},
		{"", "crit"},
	}
	for _, tc := range tests {
		if got := ReconnectCommand(tc.key); got != tc.want {
			t.Errorf("ReconnectCommand(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestPlanReconnectCommand(t *testing.T) {
	if got := PlanReconnectCommand("auth-flow"); got != "crit plan --name auth-flow" {
		t.Errorf("PlanReconnectCommand() = %q", got)
	}
	if got := PlanReconnectCommand(""); got != "crit plan" {
		t.Errorf("PlanReconnectCommand(empty) = %q", got)
	}
}

func TestNextRoundCommand(t *testing.T) {
	plan := &Session{Mode: "plan", PlanDir: "/home/user/.crit/plans/auth-flow", SessionKey: "abc123def456"}
	if got := NextRoundCommand(plan); got != "crit plan --name auth-flow" {
		t.Errorf("plan NextRoundCommand() = %q", got)
	}
	file := &Session{Mode: "files", SessionKey: "839f3b4cd5d6"}
	if got := NextRoundCommand(file); got != "crit --session 839f3b4cd5d6" {
		t.Errorf("file NextRoundCommand() = %q", got)
	}
}

func TestValidSessionKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"839f3b4cd5d6", true},
		{"ABCDEF123456", false},
		{"839f3b4cd5d", false},
		{"839f3b4cd5d6x", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := daemon.ValidSessionKey(tc.key); got != tc.want {
			t.Errorf("ValidSessionKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestDaemonArgsFromCliArgs(t *testing.T) {
	key := "839f3b4cd5d6"
	tests := []struct {
		name    string
		cliArgs []string
		want    []string
	}{
		{"git mode", nil, []string{"--session-key", key, "--quiet"}},
		{"files", []string{"a.md", "b.md"}, []string{"--session-key", key, "--quiet", "a.md", "b.md"}},
		{"pr", []string{"pr:42"}, []string{"--session-key", key, "--quiet", "--pr", "42"}},
		{"range", []string{"range:abc..def"}, []string{"--session-key", key, "--quiet", "--range", "abc..def"}},
		{"live", []string{"live", "http://localhost:3000"}, []string{"--session-key", key, "--quiet", "live", "http://localhost:3000"}},
		{"preview", []string{"preview", "/tmp/x.html"}, []string{"--session-key", key, "--quiet", "preview", "/tmp/x.html"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := daemonArgsFromCliArgs(key, tc.cliArgs)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
