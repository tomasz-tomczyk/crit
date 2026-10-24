//go:build windows

package daemon

import "syscall"

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// daemonSysProcAttr returns the SysProcAttr used when spawning the daemon
// child. On Windows the daemon gets a new process group, so Ctrl+C in the
// parent console doesn't propagate to it, and its own hidden console.
//
// Sharing the parent's console would keep that console alive for as long as
// the daemon runs: a crit started in its own console window (Start-Process,
// a shortcut, an IDE task) would leave that window in the taskbar after the
// client exits, and closing the terminal would kill the daemon. The hidden
// console is the Windows counterpart of Setsid on Unix. A console Ctrl+Break
// cannot reach the daemon from another console, so clients stop it over
// /api/shutdown (see StopForSignal).
//
// We deliberately do NOT use DETACHED_PROCESS: a console-less daemon would
// make every console child it runs (git, gh, powershell) open a window.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
	}
}
