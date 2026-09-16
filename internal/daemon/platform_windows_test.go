//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

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
