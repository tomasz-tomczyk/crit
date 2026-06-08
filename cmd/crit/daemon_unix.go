//go:build !windows

package main

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning the daemon
// child. On Unix the daemon detaches into its own session so it survives a
// terminal hang-up.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // new session, fully detached from controlling terminal
	}
}
