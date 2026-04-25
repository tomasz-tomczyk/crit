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

func TestSessionKey_FileMode_BranchIndependent(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", []string{"plan.md"})
	k2 := sessionKey("/tmp/repo", "feature-x", []string{"plan.md"})
	if k1 != k2 {
		t.Errorf("file-mode key should be branch-independent: %s vs %s", k1, k2)
	}
}

func TestSessionKey_GitMode_BranchDependent(t *testing.T) {
	k1 := sessionKey("/tmp/repo", "main", nil)
	k2 := sessionKey("/tmp/repo", "feature-x", nil)
	if k1 == k2 {
		t.Errorf("git-mode key should differ by branch: %s", k1)
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

func TestFindSessionForCWDBranch_MatchesByBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/tmp/myrepo"

	// Create a mock HTTP server that responds to /api/health
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	// Write a session with file args (simulates "crit README.md")
	writeSessionFile("abc123def456", sessionEntry{
		PID:        os.Getpid(),
		Port:       port,
		CWD:        cwd,
		Args:       []string{"README.md"},
		Branch:     "main",
		ReviewPath: "/tmp/reviews/abc123def456.json",
	})

	// findSessionForCWDBranch should find it by cwd + branch
	entry, key, matchCount := findSessionForCWDBranch(cwd, "main")
	if matchCount != 1 {
		t.Fatalf("expected matchCount 1, got %d", matchCount)
	}
	if key != "abc123def456" {
		t.Errorf("expected key abc123def456, got %s", key)
	}
	if entry.Port != port {
		t.Errorf("expected port %d, got %d", port, entry.Port)
	}
}

func TestFindSessionForCWDBranch_NoBranchMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/tmp/myrepo"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	writeSessionFile("abc123def456", sessionEntry{
		PID:    os.Getpid(),
		Port:   port,
		CWD:    cwd,
		Branch: "feature/other",
	})

	_, _, matchCount := findSessionForCWDBranch(cwd, "main")
	if matchCount != 0 {
		t.Errorf("expected matchCount 0, got %d", matchCount)
	}
}

func TestFindSessionForCWDBranch_MultipleMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/tmp/myrepo"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	// Two sessions on same branch (different file args)
	writeSessionFile("session1aaaa", sessionEntry{
		PID:    os.Getpid(),
		Port:   port,
		CWD:    cwd,
		Args:   []string{"README.md"},
		Branch: "main",
	})
	writeSessionFile("session2bbbb", sessionEntry{
		PID:    os.Getpid(),
		Port:   port,
		CWD:    cwd,
		Branch: "main",
	})

	// Should return matchCount > 1 when ambiguous
	_, _, matchCount := findSessionForCWDBranch(cwd, "main")
	if matchCount != 2 {
		t.Errorf("expected matchCount 2 for ambiguous case, got %d", matchCount)
	}
}

func TestListSessionsForRepoRoot_MatchesSubdirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := "/tmp/myrepo"

	// Create mock HTTP servers for alive sessions
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts1.Close()
	port1, _ := strconv.Atoi(ts1.URL[strings.LastIndex(ts1.URL, ":")+1:])

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts2.Close()
	port2, _ := strconv.Atoi(ts2.URL[strings.LastIndex(ts2.URL, ":")+1:])

	ts3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts3.Close()
	port3, _ := strconv.Atoi(ts3.URL[strings.LastIndex(ts3.URL, ":")+1:])

	pid := os.Getpid()

	// Session started from repo/api subdirectory
	writeSessionFile("sub1", sessionEntry{PID: pid, Port: port1, CWD: repoRoot + "/api", Branch: "feat", ReviewPath: "/reviews/sub1.json"})
	// Session started from repo root
	writeSessionFile("sub2", sessionEntry{PID: pid, Port: port2, CWD: repoRoot, Branch: "main", ReviewPath: "/reviews/sub2.json"})
	// Session from a completely different repo
	writeSessionFile("other", sessionEntry{PID: pid, Port: port3, CWD: "/tmp/other-repo", Branch: "main", ReviewPath: "/reviews/other.json"})

	entries, keys := listSessionsForRepoRoot(repoRoot)
	if len(entries) != 2 {
		t.Fatalf("expected 2 sessions for repo root, got %d", len(entries))
	}

	// Verify both repo sessions found, other excluded
	foundKeys := map[string]bool{}
	for _, k := range keys {
		foundKeys[k] = true
	}
	if !foundKeys["sub1"] || !foundKeys["sub2"] {
		t.Errorf("expected keys sub1 and sub2, got %v", keys)
	}
	if foundKeys["other"] {
		t.Error("should not include session from different repo")
	}
}

func TestListSessionsForRepoRoot_NoPartialMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	// /tmp/myrepo-extended should NOT match /tmp/myrepo
	writeSessionFile("extended", sessionEntry{PID: os.Getpid(), Port: port, CWD: "/tmp/myrepo-extended", Branch: "main"})

	entries, _ := listSessionsForRepoRoot("/tmp/myrepo")
	if len(entries) != 0 {
		t.Errorf("expected 0 sessions (no partial match), got %d", len(entries))
	}
}

func TestAtomicWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	if err := atomicWriteFile(target, data, 0644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading target: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("content = %q, want %q", string(got), "hello world")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("permissions = %o, want 0644", info.Mode().Perm())
	}
}

func TestAtomicWriteFile_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "dir", "test.txt")

	if err := atomicWriteFile(target, []byte("nested"), 0600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(got) != "nested" {
		t.Errorf("content = %q, want %q", string(got), "nested")
	}
}

func TestAtomicWriteFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "overwrite.txt")

	// Write initial content
	if err := atomicWriteFile(target, []byte("first"), 0644); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Overwrite
	if err := atomicWriteFile(target, []byte("second"), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", string(got), "second")
	}
}

func TestAtomicWriteFile_NoTempFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "clean.txt")

	if err := atomicWriteFile(target, []byte("clean"), 0644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, de := range entries {
		if strings.HasSuffix(de.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", de.Name())
		}
	}
}

func TestAtomicWriteFile_RestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret.txt")

	if err := atomicWriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestReadPortFromPipe_ValidPort(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	portCh, errCh := readPortFromPipe(r)

	// Write a valid port
	fmt.Fprintln(w, "8080")
	w.Close()

	select {
	case port := <-portCh:
		if port != 8080 {
			t.Errorf("port = %d, want 8080", port)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadPortFromPipe_ErrorPrefix(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	portCh, errCh := readPortFromPipe(r)

	fmt.Fprintln(w, "error:port already in use")
	w.Close()

	select {
	case <-portCh:
		t.Fatal("expected error, not port")
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "port already in use") {
			t.Errorf("error = %q, want to contain 'port already in use'", err.Error())
		}
	}
}

func TestReadPortFromPipe_InvalidPort(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	portCh, errCh := readPortFromPipe(r)

	fmt.Fprintln(w, "notanumber")
	w.Close()

	select {
	case <-portCh:
		t.Fatal("expected error for invalid port")
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	}
}

func TestReadPortFromPipe_ClosedWithoutWriting(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	portCh, errCh := readPortFromPipe(r)
	w.Close() // Close without writing anything

	select {
	case <-portCh:
		t.Fatal("expected error when pipe closed without writing")
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	}
}

func TestSignalReadiness(t *testing.T) {
	t.Run("writes port to pipe", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}

		signalReadiness(w, 3456)

		var got int
		fmt.Fscanf(r, "%d", &got)
		r.Close()

		if got != 3456 {
			t.Errorf("read port = %d, want 3456", got)
		}
	})

	t.Run("noop when pipe is nil", func(t *testing.T) {
		// Should not panic
		signalReadiness(nil, 3456)
	})
}

func TestReadDaemonLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("reads trimmed log content", func(t *testing.T) {
		key := "logtest123456"
		logPath, err := sessionLogPath(key)
		if err != nil {
			t.Fatalf("sessionLogPath: %v", err)
		}
		os.MkdirAll(filepath.Dir(logPath), 0700)
		os.WriteFile(logPath, []byte("  error: something bad happened  \n"), 0644)

		msg := readDaemonLog(key)
		if msg != "error: something bad happened" {
			t.Errorf("readDaemonLog = %q, want trimmed content", msg)
		}
	})

	t.Run("returns empty for missing log", func(t *testing.T) {
		msg := readDaemonLog("nonexistent123")
		if msg != "" {
			t.Errorf("readDaemonLog = %q, want empty", msg)
		}
	})
}

func TestOpenReadyPipe_NoEnvVar(t *testing.T) {
	t.Setenv("_CRIT_READY_FD", "")
	os.Unsetenv("_CRIT_READY_FD")

	pipe := openReadyPipe()
	if pipe != nil {
		pipe.Close()
		t.Error("expected nil when _CRIT_READY_FD is not set")
	}
}

func TestOpenReadyPipe_WrongValue(t *testing.T) {
	t.Setenv("_CRIT_READY_FD", "99")

	pipe := openReadyPipe()
	if pipe != nil {
		pipe.Close()
		t.Error("expected nil when _CRIT_READY_FD is not 3")
	}
}

func TestSessionLogPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := sessionLogPath("abc123def456")
	if err != nil {
		t.Fatalf("sessionLogPath: %v", err)
	}
	want := filepath.Join(home, ".crit", "sessions", "abc123def456.log")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestReviewFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := reviewFilePath("mykey123456")
	if err != nil {
		t.Fatalf("reviewFilePath: %v", err)
	}
	want := filepath.Join(home, ".crit", "reviews", "mykey123456.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestIsDaemonAlive_NegativePID(t *testing.T) {
	if isDaemonAlive(sessionEntry{PID: -1, Port: 9999}) {
		t.Error("negative PID should not be alive")
	}
}

func TestIsDaemonAlive_NoPort(t *testing.T) {
	if isDaemonAlive(sessionEntry{PID: os.Getpid(), Port: 0}) {
		t.Error("port 0 should not be alive")
	}
}

func TestRemoveSessionFile_CleansUpAssociatedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	key := "cleanup12345a"
	sessDir := filepath.Join(home, ".crit", "sessions")
	os.MkdirAll(sessDir, 0700)

	// Create session file and associated files
	os.WriteFile(filepath.Join(sessDir, key+".json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(sessDir, key+".log"), []byte("log"), 0644)
	os.WriteFile(filepath.Join(sessDir, key+".lock"), []byte(""), 0644)

	removeSessionFile(key)

	for _, ext := range []string{".json", ".log", ".lock"} {
		path := filepath.Join(sessDir, key+ext)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", path)
		}
	}
}

func TestCleanOrphanedSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sessDir := filepath.Join(home, ".crit", "sessions")

	// Create a session file with a dead PID (PID that cannot exist).
	deadEntry := sessionEntry{PID: 999999999, Port: 12345, CWD: "/tmp/repo"}
	if err := writeSessionFile("deadpid12345", deadEntry); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}

	// Create a session file with a live PID (our own process).
	// isDaemonAlive also checks HTTP, so this will fail the HTTP probe and
	// be treated as dead. Use an httptest server to keep it alive.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	liveEntry := sessionEntry{PID: os.Getpid(), Port: port, CWD: "/tmp/repo"}
	if err := writeSessionFile("livepid12345", liveEntry); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}

	cleanOrphanedSessions()

	// Dead PID session should be removed.
	if _, err := os.Stat(filepath.Join(sessDir, "deadpid12345.json")); !os.IsNotExist(err) {
		t.Error("expected dead PID session file to be removed")
	}

	// Live PID session should still exist.
	if _, err := os.Stat(filepath.Join(sessDir, "livepid12345.json")); err != nil {
		t.Error("expected live PID session file to still exist")
	}
}
