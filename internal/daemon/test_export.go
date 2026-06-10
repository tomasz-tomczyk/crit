package daemon

import (
	"os"
	"os/exec"
)

// NewProcessGroupForTest returns a process group handle for tests.
func NewProcessGroupForTest() (*ProcessGroup, error) {
	return newProcessGroup()
}

// ProcessGroup is the exported process-group handle for tests.
type ProcessGroup = processGroup

// ProcessExists reports whether a process is still running.
func ProcessExists(proc *os.Process) bool {
	return processExists(proc)
}

// Close releases process-group resources.
func (g *ProcessGroup) Close() {
	g.close()
}

// StartInGroup starts cmd in the process group.
func (g *ProcessGroup) StartInGroup(cmd *exec.Cmd) error {
	return g.startInGroup(cmd)
}

// KillAll terminates every process in the group.
func (g *ProcessGroup) KillAll() {
	g.killAll()
}
