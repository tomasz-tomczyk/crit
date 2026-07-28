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

// resolveReconnectReviewDir picks the review folder for a dead-daemon reconnect.
// Prefers the stale session registry's review_path (needed for --output reviews).
func resolveReconnectReviewDir(key string, stale daemon.SessionEntry) (string, error) {
	if stale.ReviewPath != "" {
		critPath := ReviewPathsFor(stale.ReviewPath).Review
		if _, err := os.Stat(critPath); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("no review found for session %s at %s", key, stale.ReviewPath)
			}
			return "", fmt.Errorf("stat review for session %s: %w", key, err)
		}
		return stale.ReviewPath, nil
	}
	return daemon.ReviewFilePath(key)
}

// daemonArgsForReconnect rebuilds _serve argv including flags not stored in cli_args.
func daemonArgsForReconnect(sessionKey string, cliArgs []string, stale daemon.SessionEntry, reviewDir string) []string {
	args := daemonArgsFromCliArgs(sessionKey, cliArgs)
	args = appendReconnectPathFlags(sessionKey, args, reviewDir)
	args = daemon.AppendCommonDaemonFlags(args, daemon.CommonDaemonFlags{
		Host:      stale.Host,
		PublicURL: stale.PublicURL,
	})
	return args
}

// appendReconnectPathFlags adds --output or --plan-dir/--name when the review
// folder is outside the default ~/.crit/reviews/<key> layout.
func appendReconnectPathFlags(sessionKey string, args []string, reviewDir string) []string {
	defaultDir, err := daemon.ReviewFilePath(sessionKey)
	if err == nil && filepath.Clean(reviewDir) == filepath.Clean(defaultDir) {
		return args
	}
	home, err := os.UserHomeDir()
	if err == nil {
		plansRoot := filepath.Join(home, ".crit", "plans")
		parent := filepath.Dir(reviewDir)
		if strings.HasSuffix(filepath.ToSlash(reviewDir), "/.crit") &&
			strings.HasPrefix(filepath.Clean(parent), filepath.Clean(plansRoot)+string(filepath.Separator)) {
			slug := filepath.Base(parent)
			if slug != "" && slug != "." {
				return append(args, "--plan-dir", parent, "--name", slug)
			}
		}
	}
	// Custom data root: {root}/reviews/<key> → --output {root}
	if filepath.Base(reviewDir) == sessionKey && filepath.Base(filepath.Dir(reviewDir)) == "reviews" {
		root := filepath.Dir(filepath.Dir(reviewDir))
		if root != "" && root != "." {
			return append(args, "--output", root)
		}
	}
	return args
}

// reconnectDeadSession restarts a daemon for an existing review folder.
func reconnectDeadSession(key string, stale daemon.SessionEntry) (daemon.SessionEntry, error) {
	revDir, err := resolveReconnectReviewDir(key, stale)
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
	daemonArgs := daemonArgsForReconnect(key, cj.CliArgs, stale, revDir)
	entry, err := startDaemonForReconnect(key, daemonArgs)
	if err != nil {
		return daemon.SessionEntry{}, err
	}
	fmt.Fprintf(os.Stderr, "Restarted crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
	HintMissingIntegrations()
	return entry, nil
}
