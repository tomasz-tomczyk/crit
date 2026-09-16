//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestSignalProbeAlive(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "running", want: true},
		{name: "permission denied", err: syscall.EPERM, want: true},
		{name: "wrapped permission denied", err: fmt.Errorf("signal: %w", syscall.EPERM), want: true},
		{name: "missing process", err: syscall.ESRCH},
		{name: "finished process", err: os.ErrProcessDone},
		{name: "other error", err: syscall.EINVAL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := signalProbeAlive(tt.err); got != tt.want {
				t.Errorf("signalProbeAlive(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestProcessExistsForSelf(t *testing.T) {
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess(self): %v", err)
	}
	if !processExists(proc) {
		t.Error("processExists(self) = false, want true")
	}
}
