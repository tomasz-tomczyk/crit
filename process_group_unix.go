//go:build !windows

package main

import "os/exec"

// processGroup is a no-op on Unix. The test's existing terminateProcess +
// processExists path is sufficient: SIGTERM to the daemon brings the whole
// session down, and child git/sl/gh processes complete fast enough that
// t.TempDir cleanup never collides with their cwd handles (Unix doesn't
// hold the directory open the way Windows does anyway).
type processGroup struct{}

func newProcessGroup() (*processGroup, error) { return &processGroup{}, nil }

// startInGroup is exec.Cmd.Start on Unix — there is nothing to attach to.
func (g *processGroup) startInGroup(cmd *exec.Cmd) error { return cmd.Start() }

// killAll is a no-op on Unix.
func (g *processGroup) killAll() {}

// close is a no-op on Unix.
func (g *processGroup) close() {}
