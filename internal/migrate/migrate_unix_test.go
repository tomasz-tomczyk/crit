//go:build !windows

package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A copy that fails leaves the source alone and no half copy behind.
func TestRun_FailedCopyKeepsSource(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)
	actAcrossFileSystems(t)
	fifo := filepath.Join(home, ".crit", "reviews", "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	err := run(&bytes.Buffer{}, home, false)
	if err == nil || !strings.Contains(err.Error(), "cannot copy") {
		t.Fatalf("err = %v, want the copy failure", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".crit", "reviews", "aaaaaaaaaaaa", "review.json")); err != nil {
		t.Errorf("source review gone after a failed copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "crit", "reviews")); !os.IsNotExist(err) {
		t.Errorf("half copy left behind (err=%v)", err)
	}
	// The next run picks up where this one stopped.
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	if err := run(&bytes.Buffer{}, home, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
