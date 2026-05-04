//go:build !windows

package main

import (
	"os"
	"syscall"
)

// shutdownSignals returns the OS signals that should trigger a graceful
// shutdown. On Unix this includes SIGHUP so terminal hang-up cleans up the
// daemon; SIGHUP does not exist on Windows.
func shutdownSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}

// terminationSignals returns the signals used by the parent CLI to forward
// shutdown to the daemon child it started. SIGHUP is intentionally excluded
// here; users hitting Ctrl+C or sending TERM should propagate, but the
// CLI should not propagate its own SIGHUP if its tty disconnects.
func terminationSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}

// terminateProcess asks the given process to exit gracefully. On Unix this is
// SIGTERM; on Windows there is no equivalent for arbitrary processes so the
// process is killed outright.
func terminateProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// processExists reports whether the given process is still running. On Unix
// this is signal-0; on Windows os.FindProcess returns a handle that is alive
// only while the process exists, so we use a Windows-specific probe.
func processExists(proc *os.Process) bool {
	return proc.Signal(syscall.Signal(0)) == nil
}
