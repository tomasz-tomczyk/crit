package main

import (
	"os"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestWire_ResolveServerConfigPassesSessionID(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc, err := session.ResolveServerConfigFn([]string{"--session", "839f3b4cd5d6", "--no-open"})
	if err != nil {
		t.Fatalf("ResolveServerConfigFn: %v", err)
	}
	if sc.SessionID != "839f3b4cd5d6" {
		t.Errorf("SessionID = %q", sc.SessionID)
	}
}
