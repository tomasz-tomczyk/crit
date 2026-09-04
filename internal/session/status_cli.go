package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/reviewpath"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func RunStatus(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}

	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return err
	}

	vcsName := ""
	branch := ""
	var backend vcs.VCS
	if backend = vcs.DetectVCS(""); backend != nil {
		vcsName = backend.Name()
		branch = backend.CurrentBranch()
	}

	sessions, sessionKeys, matchedSession, err := loadStatusSessions(cwd, branch, backend)
	if err != nil {
		return err
	}

	ambiguous := len(sessions) > 1
	var revPath string
	revExists := false
	if !ambiguous {
		revPath, err = resolveStatusReviewPath(cwd, branch, matchedSession)
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(ReviewPathsFor(revPath).Review); statErr == nil {
			revExists = true
		}
	}

	if jsonOutput {
		printStatusJSON(vcsName, branch, revPath, revExists, matchedSession, sessions, sessionKeys, ambiguous)
		return nil
	}

	printStatusHuman(vcsName, branch, revPath, revExists, matchedSession, sessions, sessionKeys, ambiguous)
	return nil
}

func loadStatusSessions(cwd, branch string, backend vcs.VCS) ([]daemon.SessionEntry, []string, *daemon.SessionEntry, error) {
	sessions, sessionKeys, err := daemon.ListSessionsForCWDWithKeys(cwd)
	if err != nil {
		return nil, nil, nil, err
	}
	sessions, sessionKeys = daemon.SessionsForBranch(sessions, sessionKeys, branch)
	matchedSession := selectStatusSession(sessions)
	if matchedSession == nil && len(sessions) == 0 && backend != nil {
		if repoRoot, rootErr := backend.RepoRoot(); rootErr == nil {
			repoSessions, repoKeys := daemon.ListSessionsForRepoRoot(repoRoot)
			repoSessions, repoKeys = daemon.SessionsForBranch(repoSessions, repoKeys, branch)
			matchedSession = selectStatusSession(repoSessions)
			if matchedSession != nil || len(repoSessions) > 0 {
				sessions, sessionKeys = repoSessions, repoKeys
			}
		}
	}
	return sessions, sessionKeys, matchedSession, nil
}

// selectStatusSession returns the sole matching session, or nil when zero or
// multiple sessions match (callers must not invent an arbitrary primary).
func selectStatusSession(sessions []daemon.SessionEntry) *daemon.SessionEntry {
	if len(sessions) != 1 {
		return nil
	}
	return &sessions[0]
}

func resolveStatusReviewPath(cwd, branch string, matchedSession *daemon.SessionEntry) (string, error) {
	if matchedSession != nil && matchedSession.ReviewPath != "" {
		return matchedSession.ReviewPath, nil
	}
	cfg, err := config.LoadCurrentConfig()
	if err != nil {
		return "", err
	}
	if cfg.Output != "" {
		return reviewpath.Identity(cfg.Output, daemon.SessionKey(cwd, branch, nil))
	}
	key := daemon.SessionKey(cwd, branch, nil)
	return daemon.ReviewFilePath(key)
}

func printStatusJSON(vcsName, branch, revPath string, revExists bool, selected *daemon.SessionEntry, sessions []daemon.SessionEntry, sessionKeys []string, ambiguous bool) {
	result := map[string]interface{}{
		"vcs":    vcsName,
		"branch": branch,
	}
	if ambiguous {
		result["review_file"] = nil
		result["review_file_exists"] = false
		result["note"] = "multiple active review sessions match; choose one with --session <id> (see sessions)"
	} else {
		result["review_file"] = ReviewPathsFor(revPath).Review
		result["review_file_exists"] = revExists
	}
	daemonInfo := map[string]interface{}{"running": false}
	if selected != nil {
		daemonInfo["running"] = true
		daemonInfo["pid"] = selected.PID
		daemonInfo["port"] = selected.Port
	}
	result["daemon"] = daemonInfo
	result["sessions"] = statusSessionsJSON(sessions, sessionKeys)

	if !ambiguous && revExists {
		addReviewStats(result, revPath)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func statusSessionsJSON(sessions []daemon.SessionEntry, keys []string) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(sessions))
	for i, entry := range sessions {
		if i >= len(keys) {
			break
		}
		result = append(result, map[string]interface{}{
			"id":          keys[i],
			"args":        entry.Args,
			"branch":      entry.Branch,
			"review_file": ReviewPathsFor(entry.ReviewPath).Review,
			"pid":         entry.PID,
			"port":        entry.Port,
		})
	}
	return result
}

func addReviewStats(result map[string]interface{}, revPath string) {
	data, err := os.ReadFile(ReviewPathsFor(revPath).Review)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
	}
	result["round"] = cj.ReviewRound
	if cj.ReviewType != "" {
		result["review_type"] = cj.ReviewType
	}
	if cj.Origin != "" {
		result["origin"] = cj.Origin
	}
	unresolved, resolved := countComments(cj)
	result["comments"] = map[string]int{
		"unresolved": unresolved,
		"resolved":   resolved,
	}
}

func printStatusHuman(vcsName, branch, revPath string, revExists bool, selected *daemon.SessionEntry, sessions []daemon.SessionEntry, sessionKeys []string, ambiguous bool) {
	if vcsName != "" {
		fmt.Printf("VCS:         %s\n", vcsName)
	}
	if branch != "" {
		fmt.Printf("Branch:      %s\n", branch)
	}
	if ambiguous {
		fmt.Println("Review file: (ambiguous — multiple active sessions; use --session <id>)")
		fmt.Println("Daemon:      ambiguous (see Active reviews)")
	} else {
		fmt.Printf("Review file: %s\n", ReviewPathsFor(revPath).Review)
		if selected != nil {
			fmt.Printf("Daemon:      running (PID %d, port %d)\n", selected.PID, selected.Port)
		} else {
			fmt.Println("Daemon:      not running")
		}
	}
	printActiveStatusSessions(sessions, sessionKeys)
	if ambiguous || !revExists {
		return
	}
	data, err := os.ReadFile(ReviewPathsFor(revPath).Review)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
	}
	if cj.ReviewType == "live" {
		fmt.Printf("Mode:        live\n")
		if cj.Origin != "" {
			fmt.Printf("Origin:      %s\n", cj.Origin)
		}
	}
	fmt.Printf("Round:       %d\n", cj.ReviewRound)
	unresolved, resolved := countComments(cj)
	fmt.Printf("Comments:    %d unresolved, %d resolved\n", unresolved, resolved)
}

func printActiveStatusSessions(sessions []daemon.SessionEntry, keys []string) {
	if len(sessions) == 0 {
		return
	}
	fmt.Printf("Active reviews: %d\n", len(sessions))
	for i, entry := range sessions {
		if i >= len(keys) {
			break
		}
		label := strings.Join(entry.Args, " ")
		if label == "" {
			label = entry.Branch
		}
		fmt.Printf("  %s  %s\n", keys[i], label)
		fmt.Printf("    Review file: %s\n", ReviewPathsFor(entry.ReviewPath).Review)
	}
}

func countComments(cj CritJSON) (unresolved, resolved int) {
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if c.Resolved {
				resolved++
			} else {
				unresolved++
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if c.Resolved {
			resolved++
		} else {
			unresolved++
		}
	}
	return
}
