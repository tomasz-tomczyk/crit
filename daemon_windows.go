//go:build windows

package main

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning the daemon
// child. On Windows we mark the child as a new process group and detach it
// from the parent console so closing the parent terminal does not kill the
// daemon.
//
// CREATE_NEW_PROCESS_GROUP (0x00000200) | DETACHED_PROCESS (0x00000008).
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: 0x00000200 | 0x00000008,
	}
}
