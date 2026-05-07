//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGroup wraps a Windows Job Object so an entire process tree
// (parent + every descendant — daemon, git, gh, sl, etc.) can be torn
// down atomically. This is the canonical Windows answer to "kill all
// children": TerminateProcess only kills the named process, leaving
// orphans that hold handles on inherited resources (notably, the cwd
// directory handle that blocks t.TempDir's RemoveAll).
//
// Used by Windows-sensitive tests that spawn `crit`. Production code
// does not use this — daemons are intentionally detached on Windows.
type processGroup struct {
	job windows.Handle
}

// newProcessGroup creates a Job Object with KILL_ON_JOB_CLOSE so closing
// the handle (or calling killAll) tears down every assigned process.
func newProcessGroup() (*processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return &processGroup{job: job}, nil
}

// startInGroup starts cmd suspended, assigns it to the Job Object (so every
// descendant inherits membership), then resumes it. Starting suspended is
// essential: between cmd.Start() and AssignProcessToJobObject there is a
// window in which the process could spawn children that escape the job;
// CREATE_SUSPENDED closes that window.
func (g *processGroup) startInGroup(cmd *exec.Cmd) error {
	const createSuspended = 0x00000004
	const createNewProcessGroup = 0x00000200
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createSuspended | createNewProcessGroup

	if err := cmd.Start(); err != nil {
		return err
	}

	hProc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME|
			windows.PROCESS_QUERY_INFORMATION,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		// Best-effort cleanup: kill the suspended process so it doesn't leak.
		_ = cmd.Process.Kill()
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(hProc)

	if err := windows.AssignProcessToJobObject(g.job, hProc); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}

	if err := ntResumeProcess(hProc); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("NtResumeProcess: %w", err)
	}
	return nil
}

// killAll terminates every process currently in the Job Object. The Job
// remains usable afterward — close() releases the kernel handle.
func (g *processGroup) killAll() {
	if g.job != 0 {
		_ = windows.TerminateJobObject(g.job, 1)
	}
}

// close releases the Job Object handle. Because the job was created with
// KILL_ON_JOB_CLOSE, this also terminates any still-living members.
func (g *processGroup) close() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}

var (
	modntdll             = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess  = modntdll.NewProc("NtResumeProcess")
	procNtSuspendProcess = modntdll.NewProc("NtSuspendProcess") //nolint:unused // kept symmetric for future use
)

// ntResumeProcess resumes every thread in the target process. Equivalent
// to iterating the thread list and calling ResumeThread on each, but
// atomic and simpler. The function is undocumented but stable on every
// supported Windows version.
func ntResumeProcess(h windows.Handle) error {
	r1, _, _ := procNtResumeProcess.Call(uintptr(h))
	if r1 != 0 {
		return fmt.Errorf("NtResumeProcess returned NTSTATUS 0x%x", r1)
	}
	return nil
}
