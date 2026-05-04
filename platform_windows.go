//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// shutdownSignals returns the OS signals that should trigger a graceful
// shutdown. Windows only delivers os.Interrupt and os.Kill via signal.Notify;
// SIGHUP/SIGTERM do not exist as deliverable signals on Windows.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// terminationSignals returns the signals the parent CLI forwards to the
// daemon child it started. Windows only supports os.Interrupt for signal.Notify.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// terminateProcess asks the given process to exit. On Windows there is no
// equivalent of SIGTERM for arbitrary processes — os.Process.Signal only
// accepts os.Kill and os.Interrupt, and os.Interrupt is unimplemented for
// non-console children. We fall back to TerminateProcess via os.Process.Kill.
func terminateProcess(proc *os.Process) error {
	return proc.Kill()
}

// processExists reports whether the given process is still running. On Windows
// os.FindProcess returns a handle that may outlive the process, so we open the
// process explicitly and inspect its exit code.
func processExists(proc *os.Process) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// STILL_ACTIVE = 259. If the process happens to exit with code 259 this
	// reports a false positive, but that's extremely unlikely for crit.
	return code == 259
}
