//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

type signalProbe struct {
	err    error
	signal os.Signal
}

func (p *signalProbe) Signal(signal os.Signal) error {
	p.signal = signal
	return p.err
}

func TestProcessExists(t *testing.T) {
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
			probe := &signalProbe{err: tt.err}
			if got := processExists(probe); got != tt.want {
				t.Errorf("processExists() = %v, want %v", got, tt.want)
			}
			if probe.signal != syscall.Signal(0) {
				t.Errorf("Signal() argument = %v, want signal 0", probe.signal)
			}
		})
	}
}
