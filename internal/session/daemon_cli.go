package session

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/tomasz-tomczyk/crit/internal/browser"
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
func connectOrStartDaemon(key string, args []string, noOpen bool, openCmd string) (daemon.SessionEntry, bool, error) {
	entry, alive := daemon.FindAliveSession(key)
	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s (session %s)\n", entry.BaseURL(), key)
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
	fmt.Fprintf(os.Stderr, "Started crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
	HintMissingIntegrations()
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

func installDaemonSignalHandler(pid int) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, daemon.TerminationSignals()...)
	go func() {
		<-sigCh
		if proc, err := os.FindProcess(pid); err == nil {
			_ = daemon.TerminateProcess(proc)
		}
		os.Exit(0)
	}()
}

func killDaemonOnApproval(approved bool, pid int) {
	if approved {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = daemon.TerminateProcess(proc)
		}
	}
}

func backgroundCleanup() {
	revDir, err := daemon.ReviewsDir()
	if err == nil {
		stale := findStaleReviews(revDir, 14)
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
		return os.RemoveAll(path) == nil
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
