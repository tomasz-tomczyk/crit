//go:build windows

package main

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning the daemon
// child. On Windows we mark the child as a new process group so Ctrl+C in
// the parent console doesn't propagate to the daemon.
//
// We deliberately do NOT set DETACHED_PROCESS: combining DETACHED_PROCESS
// with handle inheritance (ExtraFiles for the readiness pipe) is fragile —
// the child often fails to inherit FD 3, breaking the port handshake.
// CREATE_NEW_PROCESS_GROUP alone is enough Ctrl+C isolation for crit's
// daemon model; on Windows the daemon shares the parent console briefly
// while it starts, then keeps running after the parent exits.
//
// CREATE_NEW_PROCESS_GROUP = 0x00000200.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: 0x00000200,
	}
}
