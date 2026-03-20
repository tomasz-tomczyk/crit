package main

import (
	"path/filepath"
	"testing"
)

func TestDaemonStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".crit.json")

	want := daemonState{PID: 12345, Port: 3456}
	if err := writeDaemonState(path, want); err != nil {
		t.Fatalf("writeDaemonState: %v", err)
	}

	got, err := readDaemonState(path)
	if err != nil {
		t.Fatalf("readDaemonState: %v", err)
	}
	if got.PID != want.PID || got.Port != want.Port {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadDaemonStateMissing(t *testing.T) {
	_, err := readDaemonState("/nonexistent/.crit.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestIsDaemonAlive_NoPID(t *testing.T) {
	if isDaemonAlive(daemonState{PID: 0, Port: 9999}) {
		t.Error("PID 0 should not be alive")
	}
}
