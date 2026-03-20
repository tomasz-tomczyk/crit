package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionKey_Deterministic(t *testing.T) {
	k1 := sessionKey("/tmp/repo", []string{"plan.md"})
	k2 := sessionKey("/tmp/repo", []string{"plan.md"})
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %s vs %s", k1, k2)
	}
}

func TestSessionKey_DifferentArgs(t *testing.T) {
	k1 := sessionKey("/tmp/repo", nil)
	k2 := sessionKey("/tmp/repo", []string{"plan.md"})
	if k1 == k2 {
		t.Errorf("different args produced same key: %s", k1)
	}
}

func TestSessionKey_SortedArgs(t *testing.T) {
	k1 := sessionKey("/tmp/repo", []string{"a.md", "b.md"})
	k2 := sessionKey("/tmp/repo", []string{"b.md", "a.md"})
	if k1 != k2 {
		t.Errorf("arg order should not matter: %s vs %s", k1, k2)
	}
}

func TestSessionKey_DifferentCWD(t *testing.T) {
	k1 := sessionKey("/tmp/repo1", nil)
	k2 := sessionKey("/tmp/repo2", nil)
	if k1 == k2 {
		t.Errorf("different CWDs produced same key: %s", k1)
	}
}

func TestSessionKey_NilVsEmpty(t *testing.T) {
	k1 := sessionKey("/tmp/repo", nil)
	k2 := sessionKey("/tmp/repo", []string{})
	if k1 != k2 {
		t.Errorf("nil and empty args should produce same key: %s vs %s", k1, k2)
	}
}

func TestSessionKey_Length(t *testing.T) {
	k := sessionKey("/tmp/repo", nil)
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
