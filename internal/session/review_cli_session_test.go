package session

import (
	"encoding/json"
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

func TestRunReview_InvalidSessionID(t *testing.T) {
	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })

	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{SessionID: "not-a-valid-id"}, nil
	}

	err := RunReview(nil)
	if err == nil {
		t.Fatal("expected error for invalid session ID")
	}
	if !strings.Contains(err.Error(), "invalid session ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReview_SessionByID_ConnectsToAliveDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"mode": "git"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"approved":     false,
				"next_command": "crit --session " + key,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
		PID:  os.Getpid(),
		Port: port,
		CWD:  t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })
	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{SessionID: key, NoOpen: true}, nil
	}

	stderr := captureStderr(t, func() {
		if err := RunReview(nil); err != nil {
			t.Errorf("RunReview: %v", err)
		}
	})
	if !strings.Contains(stderr, "Connected to crit daemon") {
		t.Fatalf("stderr = %q, want connected message", stderr)
	}
	if !strings.Contains(stderr, "session "+key) {
		t.Fatalf("stderr = %q, want session id", stderr)
	}
}

func TestRunReview_DefaultKey_ConnectsToAliveDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(resolvedDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	key := daemon.SessionKey(resolvedDir, "", []string{"a.md"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"mode": "files"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
		PID:  os.Getpid(),
		Port: port,
		CWD:  resolvedDir,
	}); err != nil {
		t.Fatal(err)
	}

	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })
	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{Files: []string{"a.md"}, NoOpen: true}, nil
	}

	stderr := captureStderr(t, func() {
		if err := RunReview([]string{"a.md"}); err != nil {
			t.Errorf("RunReview: %v", err)
		}
	})
	if !strings.Contains(stderr, "Connected to crit daemon") {
		t.Fatalf("stderr = %q, want connected message", stderr)
	}
	if !strings.Contains(stderr, "session "+key) {
		t.Fatalf("stderr = %q, want session id", stderr)
	}
}

func TestRunReview_DefaultKey_StartsNewDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(resolvedDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"mode": "files"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	origStart := startDaemonForReview
	startDaemonForReview = func(string, []string) (daemon.SessionEntry, error) {
		return daemon.SessionEntry{PID: 999999999, Port: port, ReviewPath: resolvedDir}, nil
	}
	t.Cleanup(func() { startDaemonForReview = origStart })

	origClient := runReviewClientForReview
	runReviewClientForReview = func(daemon.SessionEntry, string) bool { return false }
	t.Cleanup(func() { runReviewClientForReview = origClient })

	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })
	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{
			Files:              []string{resolvedDir},
			NoOpen:             true,
			NoIntegrationCheck: true,
		}, nil
	}

	stderr := captureStderr(t, func() {
		if err := RunReview([]string{resolvedDir}); err != nil {
			t.Errorf("RunReview: %v", err)
		}
	})
	if !strings.Contains(stderr, "Started crit daemon") {
		t.Fatalf("stderr = %q, want start message", stderr)
	}
	if !strings.Contains(stderr, "Note: scanning") {
		t.Fatalf("stderr = %q, want file-scan note", stderr)
	}
}
