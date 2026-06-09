//go:build windows

package main

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsWindowsTransientIOErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"not exist", os.ErrNotExist, true},
		{"file not found", windows.Errno(windows.ERROR_FILE_NOT_FOUND), true},
		{"sharing violation", windows.Errno(windows.ERROR_SHARING_VIOLATION), true},
		{"lock violation", windows.Errno(windows.ERROR_LOCK_VIOLATION), true},
		{"access denied", windows.Errno(windows.ERROR_ACCESS_DENIED), true},
		{"other", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWindowsTransientIOErr(tc.err); got != tc.want {
				t.Fatalf("isWindowsTransientIOErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
