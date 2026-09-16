//go:build windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// stillActive is the value GetExitCodeProcess returns while a process is
// still running. The Win32 SDK names this STILL_ACTIVE; golang.org/x/sys/windows
// does not export it, so we mirror the constant here for grep-ability and
// reference it by name instead of by literal 259.
const stillActive uint32 = 259

// ShutdownSignals returns the OS signals that should trigger a graceful
// shutdown. Windows only delivers os.Interrupt and os.Kill via signal.Notify;
// SIGHUP/SIGTERM do not exist as deliverable signals on Windows.
func ShutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// terminationSignals returns the signals the parent CLI forwards to the
// daemon child it started. Windows only supports os.Interrupt for signal.Notify.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// terminateProcess asks the given process to exit. On Windows we first try
// GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid) so the daemon's
// signal.NotifyContext handler runs (the daemon child is spawned with
// CREATE_NEW_PROCESS_GROUP, so the event targets only that group).
// StopDaemon's polling loop gives the graceful path a moment to shut down
// before falling back to TerminateProcess via os.Process.Kill.
func terminateProcess(proc *os.Process) error {
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(proc.Pid)); err == nil {
		return nil
	}
	return proc.Kill()
}

// processExists reports whether the given process is still running. On Windows
// os.FindProcess returns a handle that may outlive the process, so we open the
// process explicitly and inspect its exit code.
func processExists(proc *os.Process) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		return openProcessProbeAlive(err)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return openProcessProbeAlive(err)
	}
	// If the process happens to exit with code STILL_ACTIVE (259) this
	// reports a false positive, but that's extremely unlikely for crit.
	return code == stillActive
}

// openProcessProbeAlive classifies the result of an OpenProcess /
// GetExitCodeProcess probe. ERROR_ACCESS_DENIED tells us our own token is too
// restricted to query the process, not that the process is gone — a sandboxed
// or AppContainer daemon would keep running while every probe was denied.
// Treating it as dead would make crit believe its own live daemon died and
// spawn a duplicate on every review round. ERROR_INVALID_PARAMETER is what
// OpenProcess documents for a PID that does not exist, so that — and any other
// unrecognised error — still reports dead. The /api/health probe in
// isDaemonAlive is the real authority for liveness; this helper only avoids
// the false negative that would keep that probe from ever being reached.
func openProcessProbeAlive(err error) bool {
	if err == nil {
		return true
	}
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
