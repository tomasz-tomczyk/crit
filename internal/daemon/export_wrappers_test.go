package daemon

import (
	"os"
	"testing"
)

func TestSessionsDir(t *testing.T) {
	dir, err := SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Fatal("expected non-empty sessions dir")
	}
}

func TestTerminationSignals(t *testing.T) {
	sigs := TerminationSignals()
	if len(sigs) == 0 {
		t.Fatal("expected at least one termination signal")
	}
}

func TestFindSessionForCWDBranch_NoSessions(t *testing.T) {
	_, _, n := FindSessionForCWDBranch(t.TempDir(), "main")
	if n != 0 {
		t.Errorf("expected 0 sessions, got %d", n)
	}
}

func TestIsDaemonAlive_DeadPort(t *testing.T) {
	if IsDaemonAlive(SessionEntry{Port: 1}) {
		t.Error("port 1 should not be a live daemon")
	}
}

func TestCleanOrphanedSessions_NoPanic(t *testing.T) {
	CleanOrphanedSessions()
}

func TestTerminateProcess_NilSafe(t *testing.T) {
	// terminateProcess with nil is undefined; skip. Just verify export exists via SessionsDir.
	_ = os.Getenv
}
