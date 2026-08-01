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

	sessions, sessionKeys, err := daemon.ListSessionsForCWDWithKeys(cwd)
	if err != nil {
		return err
	}
	sessions, sessionKeys = daemon.SessionsForBranch(sessions, sessionKeys, branch)
	matchedSession := selectStatusSession(sessions, branch)
	if matchedSession == nil && backend != nil {
		if repoRoot, rootErr := backend.RepoRoot(); rootErr == nil {
			repoSessions, repoKeys := daemon.ListSessionsForRepoRoot(repoRoot)
			repoSessions, repoKeys = daemon.SessionsForBranch(repoSessions, repoKeys, branch)
			matchedSession = selectStatusSession(repoSessions, branch)
			if matchedSession != nil {
				sessions, sessionKeys = repoSessions, repoKeys
			}
		}
	}

	revPath, err := resolveStatusReviewPath(cwd, branch, matchedSession)
	if err != nil {
		return err
	}

	revExists := false
	if _, statErr := os.Stat(ReviewPathsFor(revPath).Review); statErr == nil {
		revExists = true
	}

	if jsonOutput {
		printStatusJSON(vcsName, branch, revPath, revExists, matchedSession, sessions, sessionKeys)
		return nil
	}

	printStatusHuman(vcsName, branch, revPath, revExists, matchedSession, sessions, sessionKeys)
	return nil
}

func selectStatusSession(sessions []daemon.SessionEntry, branch string) *daemon.SessionEntry {
	for i, s := range sessions {
		if s.Branch == branch || (branch == "" && len(sessions) == 1) {
			return &sessions[i]
		}
	}
	return nil
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
		return reviewpath.IdentityUnderDataRoot(cfg.Output, daemon.SessionKey(cwd, branch, nil))
	}
	key := daemon.SessionKey(cwd, branch, nil)
	return daemon.ReviewFilePath(key)
}

func printStatusJSON(vcsName, branch, revPath string, revExists bool, selected *daemon.SessionEntry, sessions []daemon.SessionEntry, sessionKeys []string) {
	result := map[string]interface{}{
		"vcs":                vcsName,
		"branch":             branch,
		"review_file":        ReviewPathsFor(revPath).Review,
		"review_file_exists": revExists,
	}
	daemon := map[string]interface{}{"running": false}
	if selected != nil {
		daemon["running"] = true
		daemon["pid"] = selected.PID
		daemon["port"] = selected.Port
	}
	result["daemon"] = daemon
	result["sessions"] = statusSessionsJSON(sessions, sessionKeys)

	if revExists {
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

func printStatusHuman(vcsName, branch, revPath string, revExists bool, selected *daemon.SessionEntry, sessions []daemon.SessionEntry, sessionKeys []string) {
	if vcsName != "" {
		fmt.Printf("VCS:         %s\n", vcsName)
	}
	if branch != "" {
		fmt.Printf("Branch:      %s\n", branch)
	}
	fmt.Printf("Review file: %s\n", ReviewPathsFor(revPath).Review)
	if selected != nil {
		fmt.Printf("Daemon:      running (PID %d, port %d)\n", selected.PID, selected.Port)
	} else {
		fmt.Println("Daemon:      not running")
	}
	printActiveStatusSessions(sessions, sessionKeys)
	if !revExists {
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
