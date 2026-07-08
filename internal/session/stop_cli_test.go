package session

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestRunStopAllStopsEverySession(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	repoRootRaw := testutil.InitTestRepo(t)
	repoRoot, err := filepath.EvalSymlinks(repoRootRaw)
	if err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repoRoot, "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repoRoot)

	rootKey := "root11111111"
	subdirKey := "sub222222222"
	otherKey := "other3333333"
	pid := os.Getpid()
	writeAliveSession(t, rootKey, daemon.SessionEntry{PID: pid, CWD: repoRoot, Branch: "main"})
	writeAliveSession(t, subdirKey, daemon.SessionEntry{PID: pid, CWD: subdir, Branch: "main"})
	writeAliveSession(t, otherKey, daemon.SessionEntry{PID: pid, CWD: filepath.Join(t.TempDir(), "other"), Branch: "main"})

	stopped := map[string]bool{}
	origStop := stopDaemonByKey
	t.Cleanup(func() { stopDaemonByKey = origStop })
	stopDaemonByKey = func(key string) error {
		stopped[key] = true
		return nil
	}

	if err := RunStop([]string{"--all"}); err != nil {
		t.Fatalf("RunStop: %v", err)
	}

	if !stopped[rootKey] {
		t.Fatalf("root session was not stopped; stopped keys: %v", stopped)
	}
	if !stopped[subdirKey] {
		t.Fatalf("subdirectory session was not stopped; stopped keys: %v", stopped)
	}
	if !stopped[otherKey] {
		t.Fatalf("other repo session was not stopped; stopped keys: %v", stopped)
	}
}

func TestRunStopAllReturnsErrorWhenRepoSessionStopFails(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	repoRootRaw := testutil.InitTestRepo(t)
	repoRoot, err := filepath.EvalSymlinks(repoRootRaw)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(repoRoot)

	rootKey := "root11111111"
	writeAliveSession(t, rootKey, daemon.SessionEntry{PID: os.Getpid(), CWD: repoRoot, Branch: "main"})

	origStop := stopDaemonByKey
	t.Cleanup(func() { stopDaemonByKey = origStop })
	stopDaemonByKey = func(key string) error {
		return errors.New("boom")
	}

	err = RunStop([]string{"--all"})
	if err == nil {
		t.Fatal("RunStop returned nil, want stop error")
	}
	if !strings.Contains(err.Error(), rootKey) {
		t.Fatalf("RunStop error = %q, want failed session key", err)
	}
}

func TestRunStopAllSucceedsWithNoSessions(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	if err := RunStop([]string{"--all"}); err != nil {
		t.Fatalf("RunStop: %v", err)
	}
}

func TestRunStopWithFileArgsStopsExactKey(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolvedDir)

	wantKey := daemon.SessionKey(resolvedDir, "", []string{"plan.md"})
	var gotKey string
	origStop := stopDaemonByKey
	t.Cleanup(func() { stopDaemonByKey = origStop })
	stopDaemonByKey = func(key string) error {
		gotKey = key
		return nil
	}

	if err := RunStop([]string{"plan.md"}); err != nil {
		t.Fatalf("RunStop: %v", err)
	}
	if gotKey != wantKey {
		t.Fatalf("stopped key = %q, want %q", gotKey, wantKey)
	}
}

func TestRunStopWithoutArgsStopsExactKey(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolvedDir)

	key := daemon.SessionKey(resolvedDir, "", nil)
	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{PID: 1234, CWD: resolvedDir}); err != nil {
		t.Fatal(err)
	}

	var gotKey string
	origStop := stopDaemonByKey
	t.Cleanup(func() { stopDaemonByKey = origStop })
	stopDaemonByKey = func(key string) error {
		gotKey = key
		return nil
	}

	if err := RunStop(nil); err != nil {
		t.Fatalf("RunStop: %v", err)
	}
	if gotKey != key {
		t.Fatalf("stopped key = %q, want %q", gotKey, key)
	}
}

func writeAliveSession(t *testing.T, key string, entry daemon.SessionEntry) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	entry.Port = port
	entry.Host = strings.TrimPrefix(u.Hostname(), "[")
	entry.Host = strings.TrimSuffix(entry.Host, "]")
	if err := daemon.WriteSessionFile(key, entry); err != nil {
		t.Fatal(err)
	}
}
