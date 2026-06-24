package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestReconnectDeadSession_MissingReview(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	_, err := reconnectDeadSession("839f3b4cd5d6")
	if err == nil {
		t.Fatal("expected error for missing review")
	}
	if !strings.Contains(err.Error(), "no review found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconnectDeadSession_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	revDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	critPath := ReviewPathsFor(revDir).Review
	if err := ensureReviewFolder(critPath); err != nil {
		t.Fatal(err)
	}
	writeFile(t, critPath, "not json")

	_, err = reconnectDeadSession(key)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parsing review") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconnectDeadSession_RestartsDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	revDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{CliArgs: []string{"a.md"}}
	if err := SaveCritJSON(revDir, cj); err != nil {
		t.Fatal(err)
	}

	orig := startDaemonForReconnect
	startDaemonForReconnect = func(gotKey string, args []string) (daemon.SessionEntry, error) {
		if gotKey != key {
			t.Fatalf("key = %q, want %q", gotKey, key)
		}
		want := []string{"--session-key", key, "--quiet", "a.md"}
		if len(args) != len(want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
		for i := range want {
			if args[i] != want[i] {
				t.Fatalf("args = %v, want %v", args, want)
			}
		}
		return daemon.SessionEntry{PID: 42, Port: 3001}, nil
	}
	t.Cleanup(func() { startDaemonForReconnect = orig })

	stderr := captureStderr(t, func() {
		entry, err := reconnectDeadSession(key)
		if err != nil {
			t.Fatalf("reconnectDeadSession: %v", err)
		}
		if entry.Port != 3001 {
			t.Fatalf("port = %d, want 3001", entry.Port)
		}
	})
	if !strings.Contains(stderr, "Restarted crit daemon") {
		t.Fatalf("stderr = %q, want restart message", stderr)
	}
}

func TestRunReview_SessionByID_ReconnectsDeadDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	revDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCritJSON(revDir, CritJSON{CliArgs: nil}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"mode": "git"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	origStart := startDaemonForReconnect
	startDaemonForReconnect = func(string, []string) (daemon.SessionEntry, error) {
		return daemon.SessionEntry{PID: os.Getpid(), Port: port}, nil
	}
	t.Cleanup(func() { startDaemonForReconnect = origStart })

	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })
	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{SessionID: key, NoOpen: true}, nil
	}

	if err := RunReview(nil); err != nil {
		t.Fatalf("RunReview: %v", err)
	}
}

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
	if got := NextRoundCommand(nil); got != "crit" {
		t.Errorf("nil NextRoundCommand() = %q", got)
	}
	planNoSlug := &Session{Mode: "plan", PlanDir: ".", SessionKey: "abc123def456"}
	if got := NextRoundCommand(planNoSlug); got != "crit --session abc123def456" {
		t.Errorf("plan without slug NextRoundCommand() = %q", got)
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
