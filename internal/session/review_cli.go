package session

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

//nolint:gocyclo // CLI review dispatcher
func RunReview(args []string) error {
	go backgroundCleanup()

	if ResolveServerConfigFn == nil {
		return errors.New("ResolveServerConfigFn not wired")
	}
	sc, err := ResolveServerConfigFn(args)
	if err != nil {
		return err
	}
	if sc == nil {
		return nil // --version
	}

	cwd, _ := daemon.ResolvedCWD()
	branch := ""
	if v := vcs.DetectVCS(sc.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	key := daemon.SessionKey(cwd, branch, FocusKeyArgs(sc))

	entry, alive := daemon.FindAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s\n", entry.BaseURL())
		if !sc.NoOpen && !daemon.DaemonHasBrowser(entry) {
			go browser.OpenBrowser(entry.BaseURL())
		}
	} else {
		if len(sc.Files) == 0 && sc.Focus == nil && sc.PlanDir == "" && PreflightCheckFn != nil {
			if msg := PreflightCheckFn(sc); msg != "" {
				fmt.Fprint(os.Stderr, msg)
				return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
			}
		}
		entry, err = daemon.StartDaemon(key, args)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Started crit daemon at %s (PID %d)\n", entry.BaseURL(), entry.PID)
		if dirs := dirArgs(sc.Files); len(dirs) > 0 {
			fmt.Fprintf(os.Stderr, "\nNote: scanning %s — file paths are intended for reviewing a small set of\n"+
				"documents or plans. To review code changes, run `crit` with no arguments\n"+
				"on a feature branch.\n\n", strings.Join(dirs, ", "))
		}
		if !sc.NoIntegrationCheck {
			HintMissingIntegrations()
		}
		weStartedDaemon = true
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := daemon.RunReviewClient(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, config.LoadConfig(cwd).CleanupOnApproveEnabled())
	return nil
}
