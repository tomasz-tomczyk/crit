package session

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
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
	if vcs := vcs.DetectVCS(""); vcs != nil {
		vcsName = vcs.Name()
		branch = vcs.CurrentBranch()
	}

	sessions, err := daemon.ListSessionsForCWD(cwd)
	if err != nil {
		return err
	}
	var matchedSession *daemon.SessionEntry
	for i, s := range sessions {
		if s.Branch == branch || (branch == "" && len(sessions) == 1) {
			matchedSession = &sessions[i]
			break
		}
	}

	var revPath string
	if matchedSession != nil && matchedSession.ReviewPath != "" {
		revPath = matchedSession.ReviewPath
	} else {
		key := daemon.SessionKey(cwd, branch, nil)
		revPath, _ = daemon.ReviewFilePath(key)
	}

	revExists := false
	if _, statErr := os.Stat(ReviewPathsFor(revPath).Review); statErr == nil {
		revExists = true
	}

	if jsonOutput {
		printStatusJSON(vcsName, branch, revPath, revExists, matchedSession)
		return nil
	}

	printStatusHuman(vcsName, branch, revPath, revExists, matchedSession)
	return nil
}

func printStatusJSON(vcsName, branch, revPath string, revExists bool, session *daemon.SessionEntry) {
	result := map[string]interface{}{
		"vcs":                vcsName,
		"branch":             branch,
		"review_file":        ReviewPathsFor(revPath).Review,
		"review_file_exists": revExists,
	}
	daemon := map[string]interface{}{"running": false}
	if session != nil {
		daemon["running"] = true
		daemon["pid"] = session.PID
		daemon["port"] = session.Port
	}
	result["daemon"] = daemon

	if revExists {
		addReviewStats(result, revPath)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
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

func printStatusHuman(vcsName, branch, revPath string, revExists bool, session *daemon.SessionEntry) {
	if vcsName != "" {
		fmt.Printf("VCS:         %s\n", vcsName)
	}
	if branch != "" {
		fmt.Printf("Branch:      %s\n", branch)
	}
	fmt.Printf("Review file: %s\n", ReviewPathsFor(revPath).Review)
	if session != nil {
		fmt.Printf("Daemon:      running (PID %d, port %d)\n", session.PID, session.Port)
	} else {
		fmt.Println("Daemon:      not running")
	}
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
