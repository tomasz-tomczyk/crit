//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDaemonSysProcAttr_OwnHiddenConsoleAndProcessGroup(t *testing.T) {
	const detachedProcess = 0x00000008
	flags := daemonSysProcAttr().CreationFlags
	if flags&createNoWindow == 0 {
		t.Error("daemon must get its own hidden console (CREATE_NO_WINDOW), not share the parent's")
	}
	if flags&createNewProcessGroup == 0 {
		t.Error("daemon must run in its own process group (CREATE_NEW_PROCESS_GROUP)")
	}
	if flags&detachedProcess != 0 {
		t.Error("a console-less daemon (DETACHED_PROCESS) would open a window for every console child")
	}
}

func TestOpenProcessProbeAlive(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no error", nil, true},
		{"access denied", windows.ERROR_ACCESS_DENIED, true},
		{"wrapped access denied", fmt.Errorf("probe failed: %w", windows.ERROR_ACCESS_DENIED), true},
		{"invalid parameter (no such process)", windows.ERROR_INVALID_PARAMETER, false},
		{"unrelated error", errors.New("unexpected error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openProcessProbeAlive(tt.err); got != tt.want {
				t.Errorf("openProcessProbeAlive(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
