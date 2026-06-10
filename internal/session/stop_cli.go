package session

import (
	"errors"
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func RunStop(args []string) error {
	all := false
	var fileArgs []string
	for _, arg := range args {
		if arg == "--all" {
			all = true
		} else {
			fileArgs = append(fileArgs, arg)
		}
	}

	cwd, _ := daemon.ResolvedCWD()

	if all {
		daemon.StopAllDaemonsForCWD(cwd)
		fmt.Println("All daemons stopped.")
		return nil
	}

	branch := ""
	if vcs := vcs.DetectVCS(""); vcs != nil {
		branch = vcs.CurrentBranch()
	}

	// If file args were given, use the exact key (user knows which session).
	if len(fileArgs) > 0 {
		key := daemon.SessionKey(cwd, branch, fileArgs)
		if err := daemon.StopDaemon(key); err != nil {
			return err
		}
		fmt.Println("Daemon stopped.")
		return nil
	}

	// No file args: try exact key first (git-mode session with no args).
	key := daemon.SessionKey(cwd, branch, nil)
	if entry, _ := daemon.ReadSessionFile(key); entry.PID > 0 {
		if err := daemon.StopDaemon(key); err != nil {
			return err
		}
		fmt.Println("Daemon stopped.")
		return nil
	}

	// Exact key not found — fall back to scanning by cwd + branch.
	_, foundKey, matchCount := daemon.FindSessionForCWDBranch(cwd, branch)
	if matchCount > 1 {
		fmt.Fprintf(os.Stderr, "Error: multiple daemons running on branch %q. Use 'crit stop --all' or specify file args.\n", branch)
		return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}
	if matchCount == 0 {
		fmt.Fprintln(os.Stderr, "Error: no running daemon found for current directory and branch.")
		return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}

	if err := daemon.StopDaemon(foundKey); err != nil {
		return err
	}
	fmt.Println("Daemon stopped.")
	return nil
}
