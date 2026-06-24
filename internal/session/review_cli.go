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

var (
	startDaemonForReview     = daemon.StartDaemon
	runReviewClientForReview = daemon.RunReviewClient
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

	var key string
	var entry daemon.SessionEntry
	var alive bool
	weStartedDaemon := false

	if sc.SessionID != "" {
		if !daemon.ValidSessionKey(sc.SessionID) {
			return fmt.Errorf("invalid session ID %q (expected 12-character hex)", sc.SessionID)
		}
		key = sc.SessionID
		entry, alive = daemon.FindAliveSession(key)
		if alive {
			fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s (session %s)\n", entry.BaseURL(), key)
			if !sc.NoOpen && !daemon.DaemonHasBrowser(entry) {
				go browser.OpenBrowserWithCommand(entry.BaseURL(), sc.OpenCmd)
			}
		} else {
			entry, err = reconnectDeadSession(key)
			if err != nil {
				return err
			}
			weStartedDaemon = true
		}
	} else {
		branch := ""
		if v := vcs.DetectVCS(sc.VCSOverride); v != nil {
			branch = v.CurrentBranch()
		}
		key = daemon.SessionKey(cwd, branch, FocusKeyArgs(sc))

		entry, alive = daemon.FindAliveSession(key)
		if alive {
			fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s (session %s)\n", entry.BaseURL(), key)
			if !sc.NoOpen && !daemon.DaemonHasBrowser(entry) {
				go browser.OpenBrowserWithCommand(entry.BaseURL(), sc.OpenCmd)
			}
		} else {
			if len(sc.Files) == 0 && sc.Focus == nil && sc.PlanDir == "" && PreflightCheckFn != nil {
				if msg := PreflightCheckFn(sc); msg != "" {
					fmt.Fprint(os.Stderr, msg)
					return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
				}
			}
			entry, err = startDaemonForReview(key, args)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Started crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
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
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := runReviewClientForReview(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, config.LoadConfig(cwd).CleanupOnApproveEnabled())
	return nil
}
