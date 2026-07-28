package server

import (
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// CreateSession builds the review session for a daemon from resolved CLI config.
func CreateSession(sc *DaemonCLIConfig) (*Session, error) {
	if sc.LiveOrigin != "" {
		return session.NewLiveSession(sc.LiveOrigin, sc.ReviewPath)
	}
	if sc.PreviewFile != "" {
		return session.NewPreviewSession(sc.PreviewFile, sc.ReviewPath)
	}
	var sess *Session
	var err error
	if len(sc.Files) == 0 {
		v := vcs.DetectVCS(sc.VCSOverride)
		if v == nil {
			return nil, fmt.Errorf("not in a version-controlled repository and no files specified")
		}
		if sc.BaseBranch != "" {
			v.SetDefaultBranchOverride(sc.BaseBranch)
		}
		if sc.Focus == nil {
			sess, err = session.NewGitSession(v, sc.IgnorePatterns)
		} else {
			sess, err = session.NewGitSessionLenient(v, sc.IgnorePatterns)
		}
	} else {
		sess, err = session.NewSessionFromFiles(sc.Files, sc.IgnorePatterns)
	}
	if err != nil {
		return nil, err
	}
	if sc.BaseBranch != "" && sess.VCS != nil {
		sess.VCS.SetDefaultBranchOverride(sc.BaseBranch)
	}
	if sc.ReviewPath != "" {
		sess.ReviewFilePath = sc.ReviewPath
		sess.LoadCritJSON()
	}
	return sess, nil
}

// ApplySessionOverrides applies plan/output/focus overrides after session creation.
//
// User --output / config output is resolved earlier into sc.ReviewPath
// ({dataRoot}/reviews/<key>) and loaded via CreateSession's ReviewFilePath.
// Plan mode still uses sess.OutputDir → <planDir>/.crit via critJSONPath.
// Explicit output wins over plan: skip plan OutputDir when OutputDir is set.
func ApplySessionOverrides(sess *Session, sc *DaemonCLIConfig) {
	if sc.PlanDir != "" && sc.OutputDir == "" {
		session.ApplyPlanOverrides(sess, sc.PlanDir, sc.PlanName)
		for _, f := range sess.Files {
			f.Comments = []Comment{}
		}
		sess.ClearReviewComments()
		sess.LoadCritJSON()
	}
	// sc.OutputDir: do not set sess.OutputDir — that would rematerialize the
	// legacy parent/.crit layout and disagree with ReviewFilePath.
	if sc.Focus != nil {
		sess.RemoteFiles = sc.RemoteFiles
		if err := sess.SetFocus(*sc.Focus); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to apply focus: %v\n", err)
			return
		}
	}
}
