package session

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

var startDaemonForConnect = daemon.StartDaemon

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// connectOrStartDaemon finds an alive session or starts a new daemon.
// Returns the session entry, whether we started a new daemon, and any error.
// When quiet is true, connect/start status lines and integration tips are omitted.
func connectOrStartDaemon(key string, args []string, noOpen bool, openCmd string, quiet bool) (daemon.SessionEntry, bool, error) {
	entry, alive := daemon.FindAliveSession(key)
	if alive {
		if !quiet {
			fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s (session %s)\n", entry.BaseURL(), key)
		}
		if !noOpen && !daemon.DaemonHasBrowser(entry) {
			go browser.OpenBrowserWithCommand(entry.BaseURL(), openCmd)
		}
		return entry, false, nil
	}

	var err error
	entry, err = startDaemonForConnect(key, args)
	if err != nil {
		return daemon.SessionEntry{}, false, err
	}
	if !quiet {
		fmt.Fprintf(os.Stderr, "Started crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
		HintMissingIntegrations()
	}
	return entry, true, nil
}

// HintMissingIntegrations prints a suggestion when AI tools are detected but
// no crit integration is installed. Skipped when any integration already exists
// or when CRIT_NO_INTEGRATION_CHECK is set.
func HintMissingIntegrations() {
	if os.Getenv("CRIT_NO_INTEGRATION_CHECK") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	hintMissingIntegrationsFor(mustGetwd(), home)
}

func hintMissingIntegrationsFor(cwd, home string) {
	if InstalledAgentsFn != nil && len(InstalledAgentsFn(cwd, home)) > 0 {
		return
	}
	if CheckMissingIntegrationsFn == nil || PrintMissingHintsFn == nil {
		return
	}
	if missing := CheckMissingIntegrationsFn(cwd, home); len(missing) > 0 {
		PrintMissingHintsFn(missing)
	}
}

func installDaemonSignalHandler(entry daemon.SessionEntry) {
	daemon.StopOnSignal(entry, daemon.TerminationSignals()...)
}

// terminateDaemonProcess, waitForDaemonExit and approvalSignalGrace are seams
// for tests.
var (
	terminateDaemonProcess = daemon.TerminateProcess
	waitForDaemonExit      = daemon.WaitForExit
	approvalSignalGrace    = 500 * time.Millisecond
)

// stopDaemonOnApproval stops the daemon once the review is approved and runs
// cleanup_on_approve.
func stopDaemonOnApproval(approved bool, entry daemon.SessionEntry, key string, cleanup bool) {
	if !approved {
		return
	}
	switch daemon.RequestShutdown(entry) {
	case daemon.ShutdownWrongDaemon:
		return
	case daemon.ShutdownUnavailable:
		file, err := daemon.ReadSessionFile(key)
		switch {
		case err != nil:
			// The daemon removed its session file, so it is already shutting
			// down for another client.
		case file.PID != entry.PID:
			// A newer daemon has the key; this one is not ours to signal or
			// clean up after.
			return
		default:
			// An older daemon without /api/shutdown gets the signal it always
			// got. On Windows that can be a hard kill, so its approved
			// /api/finish response gets a moment to reach the browser first.
			if runtime.GOOS == "windows" {
				time.Sleep(approvalSignalGrace)
			}
			if proc, err := os.FindProcess(entry.PID); err == nil {
				_ = terminateDaemonProcess(proc)
			}
		}
	}
	if !cleanup {
		return
	}
	// A stopping daemon can still write the review, and a daemon started
	// meanwhile under the same key has loaded it, so the review goes only once
	// the daemon has exited and nobody took the key over.
	if !waitForDaemonExit(entry.PID) {
		return
	}
	daemon.UnlessSessionTakenOver(key, entry.PID, func() {
		cleanupOnApproval(approved, entry.ReviewPath, cleanup)
	})
}

func backgroundCleanup() {
	revDir, err := daemon.ReviewsDir()
	if err == nil {
		global, _, _ := config.LoadConfigFile(config.GlobalConfigPath())
		stale := findStaleReviews(revDir, global.StaleReviewDaysOrDefault())
		deleteStaleReviewsSilent(stale)
	}
	daemon.CleanOrphanedSessions()
}

func deleteStaleReviewsSilent(stale []staleReview) {
	sessDir, _ := daemon.SessionsDir()
	for _, s := range stale {
		if !removeStaleReviewPath(s.path) {
			continue
		}
		if sessDir != "" {
			os.Remove(filepath.Join(sessDir, s.key+".json"))
			os.Remove(filepath.Join(sessDir, s.key+".lock"))
			os.Remove(filepath.Join(sessDir, s.key+".log"))
		}
	}
}

func removeStaleReviewPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return ClearReviewFolder(path) == nil
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	_ = os.Remove(path + ".snapshots.json")
	return true
}

func cleanupOnApproval(approved bool, reviewPath string, cleanupEnabled bool) {
	if !(approved && cleanupEnabled && reviewPath != "") {
		return
	}
	_ = removeStaleReviewPath(reviewPath)
}

func dirArgs(paths []string) []string {
	var dirs []string
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return dirs
}
