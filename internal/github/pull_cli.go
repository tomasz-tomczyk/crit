package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type pullFlags struct {
	prFlag    int
	outputDir string
}

func parsePullFlags(args []string) (pullFlags, error) {
	var f pullFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
			}
			i++
			f.outputDir = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--output <dir>] [pr-number]\n")
			return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		f.prFlag = n
	}
	return f, nil
}

func RunPull(args []string) error { //nolint:gocyclo
	if err := RequireGH(); err != nil {
		return err
	}

	f, err := parsePullFlags(args)
	if err != nil {
		return err
	}

	prNumber, err := DetectPR(f.prFlag)
	if err != nil {
		return err
	}

	InvalidatePRCache(prNumber)

	ghComments, err := FetchPRComments(prNumber)
	if err != nil {
		return err
	}

	threadResolved, threadErr := FetchPRThreadResolved(prNumber)
	if threadErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch review-thread resolution state: %v\n", threadErr)
		threadResolved = nil
	}

	critPath, err := review.ResolveReviewPath(f.outputDir)
	if err != nil {
		return err
	}
	var cj session.CritJSON
	if data, readErr := session.ReadFileShared(session.ReviewPathsFor(critPath).Review); readErr == nil {
		if jsonErr := json.Unmarshal(data, &cj); jsonErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing review file is invalid, starting fresh: %v\n", jsonErr)
		}
	}

	if f.prFlag != 0 && f.outputDir == "" {
		if altPath, altCJ, ok := review.RedirectReviewPathForPR(prNumber, cj.Branch, critPath); ok {
			critPath = altPath
			cj = altCJ
		}
	}

	if cj.Files == nil {
		cj.Files = make(map[string]session.CritJSONFile)
		cj.Branch = vcs.CurrentBranch()
		cfg := config.LoadConfig("")
		base := cfg.BaseBranch
		if base == "" {
			base = vcs.DefaultBaseRef()
		}
		cj.BaseRef, _ = vcs.MergeBase(base)
		cj.ReviewRound = 1
	}

	if err := share.CheckGitHubSyncAllowed(cj, "crit pull"); err != nil {
		return err
	}

	scope := session.ResolvePullScope(&cj)
	added := MergeGHCommentsScoped(&cj, ghComments, scope, threadResolved)

	if added == 0 {
		fmt.Printf("No new inline comments found on PR #%d\n", prNumber)
		return nil
	}

	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return err
	}

	fmt.Printf("Pulled %d comments from PR #%d into %s\n", added, prNumber, critPath)
	fmt.Println("Run 'crit' to view them in the browser.")
	return nil
}
