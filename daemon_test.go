package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSessionKey_Deterministic(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", []string{"plan.md"})
	k2 := sessionKey("/tmp/repo", "main", []string{"plan.md"})
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %s vs %s", k1, k2)
	}
}

func TestSessionKey_DifferentArgs(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", nil)
	k2 := sessionKey("/tmp/repo", "main", []string{"plan.md"})
	if k1 == k2 {
		t.Errorf("different args produced same key: %s", k1)
	}
}

func TestSessionKey_SortedArgs(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", []string{"a.md", "b.md"})
	k2 := sessionKey("/tmp/repo", "main", []string{"b.md", "a.md"})
	if k1 != k2 {
		t.Errorf("arg order should not matter: %s vs %s", k1, k2)
	}
}

func TestSessionKey_DifferentCWD(t *testing.T) {
	k1 := sessionKey("/tmp/repo1", "main", nil)
	k2 := sessionKey("/tmp/repo2", "main", nil)
	if k1 == k2 {
		t.Errorf("different CWDs produced same key: %s", k1)
	}
}

func TestSessionKey_NilVsEmpty(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", nil)
	k2 := sessionKey("/tmp/repo", "main", []string{})
	if k1 != k2 {
		t.Errorf("nil and empty args should produce same key: %s vs %s", k1, k2)
	}
}

func TestSessionKey_Length(t *testing.T) {
	k := sessionKey("/tmp/repo", "main", nil)
	if len(k) != 12 {
		t.Errorf("expected key length 12, got %d: %s", len(k), k)
	}
}

func TestSessionFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	key := "test123abc00"
	want := sessionEntry{
		PID:       12345,
		Port:      3456,
		CWD:       "/tmp/repo",
		Args:      []string{"plan.md"},
		StartedAt: "2026-03-20T12:00:00Z",
	}
	if err := writeSessionFile(key, want); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}

	got, err := readSessionFile(key)
	if err != nil {
		t.Fatalf("readSessionFile: %v", err)
	}
	if got.PID != want.PID || got.Port != want.Port || got.CWD != want.CWD {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if len(got.Args) != 1 || got.Args[0] != "plan.md" {
		t.Errorf("args mismatch: got %v, want [plan.md]", got.Args)
	}

	// Verify file exists in correct location
	sessDir := filepath.Join(home, ".crit", "sessions")
	if _, err := os.Stat(filepath.Join(sessDir, key+".json")); err != nil {
		t.Errorf("session file not found: %v", err)
	}

	// Remove and verify
	removeSessionFile(key)
	if _, err := os.Stat(filepath.Join(sessDir, key+".json")); !os.IsNotExist(err) {
		t.Error("session file not removed")
	}
}

func TestSessionFileRoundTrip_NoArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	key := "noargs123456"
	want := sessionEntry{
		PID:       99999,
		Port:      5678,
		CWD:       "/tmp/repo",
		StartedAt: "2026-03-20T12:00:00Z",
	}
	if err := writeSessionFile(key, want); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}

	got, err := readSessionFile(key)
	if err != nil {
		t.Fatalf("readSessionFile: %v", err)
	}
	if got.Args != nil {
		t.Errorf("expected nil args for no-arg session, got %v", got.Args)
	}
}

func TestReadSessionFileMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := readSessionFile("nonexistent")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestWriteSessionFile_Atomic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	key := "atomictest1234"
	entry := sessionEntry{
		PID:       42,
		Port:      8080,
		CWD:       "/tmp/repo",
		StartedAt: "2026-03-20T12:00:00Z",
	}

	// Write session file
	if err := writeSessionFile(key, entry); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}

	// Verify the file contains valid JSON
	path, _ := sessionFilePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading session file: %v", err)
	}
	var got sessionEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("session file contains invalid JSON: %v", err)
	}
	if got.PID != entry.PID || got.Port != entry.Port || got.CWD != entry.CWD {
		t.Errorf("got %+v, want %+v", got, entry)
	}

	// Verify no temp files are left behind
	sessDir := filepath.Join(home, ".crit", "sessions")
	entries, _ := os.ReadDir(sessDir)
	for _, de := range entries {
		if strings.HasSuffix(de.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", de.Name())
		}
	}

	// Overwrite and verify the new content is correct
	entry2 := sessionEntry{
		PID:       99,
		Port:      9090,
		CWD:       "/tmp/repo2",
		StartedAt: "2026-03-21T12:00:00Z",
	}
	if err := writeSessionFile(key, entry2); err != nil {
		t.Fatalf("writeSessionFile overwrite: %v", err)
	}
	got2, err := readSessionFile(key)
	if err != nil {
		t.Fatalf("readSessionFile after overwrite: %v", err)
	}
	if got2.PID != entry2.PID || got2.Port != entry2.Port || got2.CWD != entry2.CWD {
		t.Errorf("after overwrite: got %+v, want %+v", got2, entry2)
	}
}

func TestAcquireSessionLock_FlockBased(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	key := "locktest123456"

	// Acquire lock
	lock, err := acquireSessionLock(key)
	if err != nil {
		t.Fatalf("acquireSessionLock: %v", err)
	}

	// Verify lock file exists
	sessDir := filepath.Join(home, ".crit", "sessions")
	lockPath := filepath.Join(sessDir, key+".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file not found: %v", err)
	}

	// Release lock
	releaseSessionLock(lock)

	// Verify lock file is cleaned up
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file not removed after release")
	}
}

func TestIsDaemonAlive_NoPID(t *testing.T) {
	if isDaemonAlive(sessionEntry{PID: 0, Port: 9999}) {
		t.Error("PID 0 should not be alive")
	}
}

func TestFindAliveSession_Stale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a session file with a PID that doesn't exist
	key := "staletest1234"
	writeSessionFile(key, sessionEntry{PID: 999999999, Port: 12345, CWD: "/tmp"})

	_, alive := findAliveSession(key)
	if alive {
		t.Error("stale session should not be alive")
	}

	// Verify stale file was cleaned up
	path, _ := sessionFilePath(key)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale session file should have been cleaned up")
	}
}

func TestListSessionsForCWD_FiltersAndCleans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/tmp/myrepo"

	// Write sessions: one for our cwd, one for different cwd
	writeSessionFile("session1", sessionEntry{PID: 999999999, Port: 1111, CWD: cwd})
	writeSessionFile("session2", sessionEntry{PID: 999999998, Port: 2222, CWD: "/other/repo"})
	writeSessionFile("session3", sessionEntry{PID: 999999997, Port: 3333, CWD: cwd})

	// All PIDs are dead, so all matching sessions should be cleaned up
	entries, keys := listSessionsForCWD(cwd)
	if len(entries) != 0 {
		t.Errorf("expected 0 alive sessions, got %d", len(entries))
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}

	// Verify stale sessions for our cwd were cleaned up
	sessDir := filepath.Join(home, ".crit", "sessions")
	if _, err := os.Stat(filepath.Join(sessDir, "session1.json")); !os.IsNotExist(err) {
		t.Error("stale session1 should have been cleaned up")
	}
	if _, err := os.Stat(filepath.Join(sessDir, "session3.json")); !os.IsNotExist(err) {
		t.Error("stale session3 should have been cleaned up")
	}
	// Different cwd session should not be touched
	if _, err := os.Stat(filepath.Join(sessDir, "session2.json")); err != nil {
		t.Error("session2 (different cwd) should not have been cleaned up")
	}
}

func TestDaemonHasBrowser(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{
			name: "returns true when browser_clients is true",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
			},
			want: true,
		},
		{
			name: "returns false when browser_clients is false",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": false})
			},
			want: false,
		},
		{
			name: "returns true when browser_clients is null (older daemon)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Encode a struct where BrowserClients is a nil pointer -> JSON null
				type resp struct {
					Status         string `json:"status"`
					BrowserClients *bool  `json:"browser_clients"`
				}
				json.NewEncoder(w).Encode(resp{Status: "ok", BrowserClients: nil})
			},
			want: true,
		},
		{
			name: "returns true when browser_clients field is missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			},
			want: true,
		},
		{
			name: "returns true on invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "not json")
			},
			want: true,
		},
		{
			name: "returns true on HTTP error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "fail", http.StatusInternalServerError)
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			// Parse port from test server URL
			port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])

			entry := sessionEntry{PID: os.Getpid(), Port: port}
			got := daemonHasBrowser(entry)
			if got != tt.want {
				t.Errorf("daemonHasBrowser() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDaemonHasBrowser_Unreachable(t *testing.T) {
	// Port that nothing listens on
	entry := sessionEntry{PID: os.Getpid(), Port: 1}
	got := daemonHasBrowser(entry)
	if !got {
		t.Error("daemonHasBrowser should return true when daemon is unreachable (safe default)")
	}
}

func TestIsDaemonAlive_ValidatesResponseBody(t *testing.T) {
	// Test that isDaemonAlive rejects a non-crit process responding on the port
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A non-crit server returning 200 but without {"status":"ok"}
		fmt.Fprint(w, `{"service":"not-crit"}`)
	}))
	defer srv.Close()

	port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])
	entry := sessionEntry{PID: os.Getpid(), Port: port}
	if isDaemonAlive(entry) {
		t.Error("isDaemonAlive should return false when response body lacks status:ok")
	}
}

func TestIsDaemonAlive_AcceptsCritResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
	}))
	defer srv.Close()

	port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])
	entry := sessionEntry{PID: os.Getpid(), Port: port}
	if !isDaemonAlive(entry) {
		t.Error("isDaemonAlive should return true for valid crit health response")
	}
}
