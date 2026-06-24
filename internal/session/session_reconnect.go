package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

// startDaemonForReconnect is daemon.StartDaemon in production; tests may replace it.
var startDaemonForReconnect = daemon.StartDaemon

// ReconnectCommand returns the crit CLI command to reconnect to an existing review
// session. Works from any cwd; use for file, git, live, and preview modes.
func ReconnectCommand(sessionKey string) string {
	if sessionKey == "" {
		return "crit"
	}
	return "crit --session " + sessionKey
}

// PlanReconnectCommand returns the CLI command to submit a revised plan and
// start the next review round. Plan content must be piped or passed as a file.
func PlanReconnectCommand(slug string) string {
	if slug == "" {
		return "crit plan"
	}
	return "crit plan --name " + slug
}

// NextRoundCommand returns the command agents should run after addressing feedback.
func NextRoundCommand(sess *Session) string {
	if sess != nil && sess.Mode == "plan" {
		if slug := filepath.Base(sess.PlanDir); slug != "" && slug != "." {
			return PlanReconnectCommand(slug)
		}
	}
	if sess == nil {
		return "crit"
	}
	return ReconnectCommand(sess.SessionKey)
}

// daemonArgsFromCliArgs rebuilds _serve argv from stored cli_args in a review file.
func daemonArgsFromCliArgs(sessionKey string, cliArgs []string) []string {
	args := []string{"--session-key", sessionKey, "--quiet"}
	if len(cliArgs) == 0 {
		return args
	}
	if len(cliArgs) >= 2 && cliArgs[0] == "live" {
		return append(args, "live", cliArgs[1])
	}
	if len(cliArgs) >= 2 && cliArgs[0] == "preview" {
		return append(args, "preview", cliArgs[1])
	}
	if len(cliArgs) == 1 {
		switch {
		case strings.HasPrefix(cliArgs[0], "pr:"):
			return append(args, "--pr", strings.TrimPrefix(cliArgs[0], "pr:"))
		case strings.HasPrefix(cliArgs[0], "range:"):
			return append(args, "--range", strings.TrimPrefix(cliArgs[0], "range:"))
		}
	}
	return append(args, cliArgs...)
}

// reconnectDeadSession restarts a daemon for an existing review folder.
func reconnectDeadSession(key string) (daemon.SessionEntry, error) {
	revDir, err := daemon.ReviewFilePath(key)
	if err != nil {
		return daemon.SessionEntry{}, err
	}
	critPath := ReviewPathsFor(revDir).Review
	data, err := os.ReadFile(critPath)
	if err != nil {
		if os.IsNotExist(err) {
			return daemon.SessionEntry{}, fmt.Errorf("no review found for session %s", key)
		}
		return daemon.SessionEntry{}, fmt.Errorf("reading review for session %s: %w", key, err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return daemon.SessionEntry{}, fmt.Errorf("parsing review for session %s: %w", key, err)
	}
	daemonArgs := daemonArgsFromCliArgs(key, cj.CliArgs)
	entry, err := startDaemonForReconnect(key, daemonArgs)
	if err != nil {
		return daemon.SessionEntry{}, err
	}
	fmt.Fprintf(os.Stderr, "Restarted crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
	HintMissingIntegrations()
	return entry, nil
}
