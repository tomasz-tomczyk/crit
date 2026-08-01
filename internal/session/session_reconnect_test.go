package session

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func daemonFlagValue(args []string, flag string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

func assertDaemonFlag(t *testing.T, args []string, flag, want string) {
	t.Helper()
	got, ok := daemonFlagValue(args, flag)
	if !ok {
		t.Fatalf("args %v missing flag %q", args, flag)
	}
	switch flag {
	case "--output", "--plan-dir":
		if filepath.Clean(got) != filepath.Clean(want) {
			t.Fatalf("flag %q = %q, want %q", flag, got, want)
		}
	default:
		if got != want {
			t.Fatalf("flag %q = %q, want %q", flag, got, want)
		}
	}
}

func TestReconnectDeadSession_MissingReview(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	_, err := reconnectDeadSession("839f3b4cd5d6", daemon.SessionEntry{}, false)
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

	_, err = reconnectDeadSession(key, daemon.SessionEntry{}, false)
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
		entry, err := reconnectDeadSession(key, daemon.SessionEntry{}, false)
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

func TestReconnectDeadSession_OutputReviewPath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	outputDir := filepath.Join(t.TempDir(), "out")
	revDir := filepath.Join(outputDir, "reviews", key)
	cj := CritJSON{CliArgs: []string{"a.md"}}
	if err := SaveCritJSON(revDir, cj); err != nil {
		t.Fatal(err)
	}

	stale := daemon.SessionEntry{ReviewPath: revDir}
	orig := startDaemonForReconnect
	startDaemonForReconnect = func(gotKey string, args []string) (daemon.SessionEntry, error) {
		if gotKey != key {
			t.Fatalf("key = %q, want %q", gotKey, key)
		}
		want := []string{"--session-key", key, "--quiet", "a.md", "--output", outputDir}
		if len(args) != len(want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
		for i := range want {
			if want[i] == outputDir {
				if filepath.Clean(args[i]) != filepath.Clean(outputDir) {
					t.Fatalf("args = %v, want %v", args, want)
				}
				continue
			}
			if args[i] != want[i] {
				t.Fatalf("args = %v, want %v", args, want)
			}
		}
		return daemon.SessionEntry{PID: 42, Port: 3001}, nil
	}
	t.Cleanup(func() { startDaemonForReconnect = orig })

	if _, err := reconnectDeadSession(key, stale, false); err != nil {
		t.Fatalf("reconnectDeadSession: %v", err)
	}
}

func TestDaemonArgsForReconnect_OutputAndPublicURL(t *testing.T) {
	key := "839f3b4cd5d6"
	outputDir := filepath.Join(os.TempDir(), "review-out")
	revDir := filepath.Join(outputDir, "reviews", key)
	stale := daemon.SessionEntry{
		ReviewPath: revDir,
		PublicURL:  "https://crit.example.com",
	}
	args := daemonArgsForReconnect(key, []string{"doc.md"}, stale, revDir)
	assertDaemonFlag(t, args, "--output", outputDir)
	assertDaemonFlag(t, args, "--public-url", "https://crit.example.com")
	if !containsArg(args, "doc.md") {
		t.Fatalf("args %v missing doc.md", args)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestResolveReconnectReviewDir_MissingStaleReview(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".crit")
	_, err := resolveReconnectReviewDir("839f3b4cd5d6", daemon.SessionEntry{ReviewPath: missing})
	if err == nil {
		t.Fatal("expected error for missing stale review_path")
	}
	if !strings.Contains(err.Error(), "no review found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveReconnectReviewDir_UsesStalePath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outputDir := filepath.Join(t.TempDir(), "out")
	revDir := filepath.Join(outputDir, ".crit")
	if err := SaveCritJSON(revDir, CritJSON{}); err != nil {
		t.Fatal(err)
	}
	got, err := resolveReconnectReviewDir("839f3b4cd5d6", daemon.SessionEntry{ReviewPath: revDir})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(revDir) {
		t.Fatalf("got %q, want %q", got, revDir)
	}
}

func TestAppendReconnectPathFlags_PlanDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"
	planStorage := filepath.Join(home, ".crit", "plans", "auth-flow")
	reviewDir := filepath.Join(planStorage, ".crit")
	args := appendReconnectPathFlags(key, []string{"--session-key", key}, reviewDir)
	assertDaemonFlag(t, args, "--plan-dir", planStorage)
	assertDaemonFlag(t, args, "--name", "auth-flow")
}

func TestAppendReconnectPathFlags_DefaultLayout(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"
	defaultDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	args := appendReconnectPathFlags(key, []string{"--session-key", key}, defaultDir)
	if _, ok := daemonFlagValue(args, "--output"); ok {
		t.Fatalf("default layout should not add --output: %v", args)
	}
	if _, ok := daemonFlagValue(args, "--plan-dir"); ok {
		t.Fatalf("default layout should not add --plan-dir: %v", args)
	}
}

func TestMigrateLegacyOutputReconnect_MovesToKeyedReview(t *testing.T) {
	key := "839f3b4cd5d6"
	root := t.TempDir()
	legacy := filepath.Join(root, ".crit")
	if err := SaveCritJSON(legacy, CritJSON{CliArgs: []string{"a.md"}}); err != nil {
		t.Fatal(err)
	}

	var args []string
	stderr := captureStderr(t, func() {
		args = migrateLegacyOutputReconnect(key, legacy)
	})
	assertDaemonFlag(t, args, "--output", root)
	dest := filepath.Join(root, "reviews", key)
	if _, err := os.Stat(filepath.Join(dest, "review.json")); err != nil {
		t.Fatalf("migrated review missing: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy path should be gone, err=%v", err)
	}
	if !strings.Contains(stderr, "migrated legacy review") {
		t.Fatalf("stderr = %q, want migrate notice", stderr)
	}
}

func TestMigrateLegacyOutputReconnect_DestExists(t *testing.T) {
	key := "839f3b4cd5d6"
	root := t.TempDir()
	legacy := filepath.Join(root, ".crit")
	if err := SaveCritJSON(legacy, CritJSON{}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "reviews", key)
	if err := SaveCritJSON(dest, CritJSON{CliArgs: []string{"keep.md"}}); err != nil {
		t.Fatal(err)
	}

	var args []string
	stderr := captureStderr(t, func() {
		args = migrateLegacyOutputReconnect(key, legacy)
	})
	assertDaemonFlag(t, args, "--output", root)
	if !strings.Contains(stderr, "ignored") {
		t.Fatalf("stderr = %q, want ignore warning", stderr)
	}
	// Existing keyed review must be preserved; legacy folder left in place.
	if _, err := os.Stat(filepath.Join(dest, "review.json")); err != nil {
		t.Fatalf("existing review missing: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy path should remain when dest exists: %v", err)
	}
}

func TestMigrateLegacyOutputReconnect_NotLegacy(t *testing.T) {
	if args := migrateLegacyOutputReconnect("839f3b4cd5d6", filepath.Join(t.TempDir(), "reviews", "839f3b4cd5d6")); args != nil {
		t.Fatalf("non-legacy path returned %v", args)
	}
}

func TestAppendReconnectPathFlags_MigratesLegacyOutput(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"
	root := t.TempDir()
	legacy := filepath.Join(root, ".crit")
	if err := SaveCritJSON(legacy, CritJSON{}); err != nil {
		t.Fatal(err)
	}

	var args []string
	captureStderr(t, func() {
		args = appendReconnectPathFlags(key, []string{"--session-key", key}, legacy)
	})
	assertDaemonFlag(t, args, "--output", root)
	if _, err := os.Stat(filepath.Join(root, "reviews", key, "review.json")); err != nil {
		t.Fatalf("expected migration via appendReconnectPathFlags: %v", err)
	}
}

func TestMigrateLegacyOutputReconnect_MkdirError(t *testing.T) {
	key := "839f3b4cd5d6"
	root := t.TempDir()
	legacy := filepath.Join(root, ".crit")
	if err := SaveCritJSON(legacy, CritJSON{}); err != nil {
		t.Fatal(err)
	}
	orig := migrateMkdirAll
	migrateMkdirAll = func(string, os.FileMode) error {
		return errors.New("mkdir failed")
	}
	t.Cleanup(func() { migrateMkdirAll = orig })

	var args []string
	stderr := captureStderr(t, func() {
		args = migrateLegacyOutputReconnect(key, legacy)
	})
	if args != nil {
		t.Fatalf("expected nil args on mkdir failure, got %v", args)
	}
	if !strings.Contains(stderr, "could not migrate") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMigrateLegacyOutputReconnect_RenameError(t *testing.T) {
	key := "839f3b4cd5d6"
	root := t.TempDir()
	legacy := filepath.Join(root, ".crit")
	if err := SaveCritJSON(legacy, CritJSON{}); err != nil {
		t.Fatal(err)
	}
	orig := migrateRename
	migrateRename = func(string, string) error {
		return errors.New("rename failed")
	}
	t.Cleanup(func() { migrateRename = orig })

	var args []string
	stderr := captureStderr(t, func() {
		args = migrateLegacyOutputReconnect(key, legacy)
	})
	if args != nil {
		t.Fatalf("expected nil args on rename failure, got %v", args)
	}
	if !strings.Contains(stderr, "could not migrate") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPlanReconnectFlags_HomeError(t *testing.T) {
	orig := userHomeDir
	userHomeDir = func() (string, error) {
		return "", errors.New("no home")
	}
	t.Cleanup(func() { userHomeDir = orig })
	if args := planReconnectFlags(filepath.Join(t.TempDir(), ".crit")); args != nil {
		t.Fatalf("expected nil when home fails, got %v", args)
	}
}

func TestDaemonArgsForReconnect_Host(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"
	defaultDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	stale := daemon.SessionEntry{Host: "0.0.0.0"}
	args := daemonArgsForReconnect(key, nil, stale, defaultDir)
	assertDaemonFlag(t, args, "--host", "0.0.0.0")
	foundAllow := false
	for _, a := range args {
		if a == "--allow-unauthenticated-network" {
			foundAllow = true
			break
		}
	}
	if !foundAllow {
		t.Fatalf("args %v missing --allow-unauthenticated-network", args)
	}
}

func TestReconnectDeadSession_StalePathMissing(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"
	missing := filepath.Join(t.TempDir(), ".crit")
	_, err := reconnectDeadSession(key, daemon.SessionEntry{ReviewPath: missing}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no review found") {
		t.Fatalf("unexpected error: %v", err)
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
