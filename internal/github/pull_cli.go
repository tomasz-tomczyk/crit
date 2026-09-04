package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type pullFlags struct {
	spec             string // positional number or URL (empty = detect from branch)
	outputDir        string
	configuredOutput string
	sessionID        string
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
		if arg == "--session" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: --session requires a value")
				return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
			}
			i++
			f.sessionID = args[i]
			continue
		}
		if f.spec != "" {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--session <id>] [--output <dir>] [number|url]\n")
			return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		if _, err := ParsePRSpec(arg); err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--session <id>] [--output <dir>] [number|url]\n")
			return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		f.spec = arg
	}
	return f, nil
}

func resolvePullFlags(f *pullFlags) error {
	cfg, err := config.LoadCurrentConfig()
	if err != nil {
		return err
	}
	f.configuredOutput = cfg.Output
	return nil
}

func parseResolvedPullFlags(args []string) (pullFlags, error) {
	f, err := parsePullFlags(args)
	if err != nil {
		return pullFlags{}, err
	}
	if err := resolvePullFlags(&f); err != nil {
		return pullFlags{}, err
	}
	return f, nil
}

func shouldRedirectReviewForPR(explicitSpec bool, pinnedOutput bool) bool {
	return explicitSpec && !pinnedOutput
}

func resolvePullChangeID(spec string) (forge.ChangeID, error) {
	if spec != "" {
		return ParsePRSpec(spec)
	}
	n, err := DetectPR(0)
	if err != nil {
		return forge.ChangeID{}, err
	}
	return forge.ChangeID{Number: n}, nil
}

func RunPull(args []string) error { //nolint:gocyclo
	if err := RequireGH(); err != nil {
		return err
	}

	f, err := parseResolvedPullFlags(args)
	if err != nil {
		return err
	}

	id, err := resolvePullChangeID(f.spec)
	if err != nil {
		return err
	}

	InvalidatePRCache(id.Number)

	ghComments, err := fetchPRComments(id)
	if err != nil {
		return err
	}

	threadResolved, threadErr := fetchPRThreadResolved(id)
	if threadErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch review-thread resolution state: %v\n", threadErr)
		threadResolved = nil
	}

	critPath, err := review.ResolveCommandReviewPathWithSession(f.sessionID, f.outputDir, f.configuredOutput)
	if err != nil {
		return err
	}
	var cj session.CritJSON
	if data, readErr := session.ReadFileShared(session.ReviewPathsFor(critPath).Review); readErr == nil {
		if jsonErr := json.Unmarshal(data, &cj); jsonErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing review file is invalid, starting fresh: %v\n", jsonErr)
		}
	}

	if shouldRedirectReviewForPR(f.spec != "", f.sessionID != "" || f.outputDir != "" || f.configuredOutput != "") {
		if altPath, altCJ, ok := review.RedirectReviewPathForPR(id.Number, cj.Branch, critPath); ok {
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
		fmt.Printf("No new inline comments found on PR #%d\n", id.Number)
		return nil
	}

	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return err
	}

	fmt.Printf("Pulled %d comments from PR #%d into %s\n", added, id.Number, critPath)
	fmt.Println("Run 'crit' to view them in the browser.")
	return nil
}
