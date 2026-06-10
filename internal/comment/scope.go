package comment

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// CommentFocusOverride captures the user's --scope flag for `crit comment`.
type CommentFocusOverride string

const (
	ScopeOverrideUnset       CommentFocusOverride = ""
	ScopeOverrideLayer       CommentFocusOverride = "layer"
	ScopeOverrideFullStack   CommentFocusOverride = "full-stack"
	ScopeOverrideWorkingTree CommentFocusOverride = "working-tree"
)

// CommentScopeOverrideFromFlag normalizes the raw --scope string for crit comment.
func CommentScopeOverrideFromFlag(s string) (CommentFocusOverride, error) {
	if s == "" {
		return ScopeOverrideUnset, nil
	}
	scope, isWorkingTree, err := normalizeScopeSpec(s)
	if err != nil {
		return "", fmt.Errorf("%w (expected layer | full-stack | working-tree)", err)
	}
	switch {
	case isWorkingTree:
		return ScopeOverrideWorkingTree, nil
	case scope == session.DiffScopeFullStack:
		return ScopeOverrideFullStack, nil
	default:
		return ScopeOverrideLayer, nil
	}
}

func normalizeScopeSpec(s string) (session.DiffScope, bool, error) {
	switch s {
	case "", "layer":
		return session.DiffScopeLayer, false, nil
	case "full-stack", "full_stack":
		return session.DiffScopeFullStack, false, nil
	case "working-tree", "working_tree":
		return session.DiffScopeLayer, true, nil
	default:
		return "", false, fmt.Errorf("invalid --scope value %q", s)
	}
}

func loadCritJSONForOutputDir(outputDir string) (session.CritJSON, bool) {
	critPath, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot resolve review path: %v\n", err)
		return session.CritJSON{}, false
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return session.CritJSON{}, false
		}
		fmt.Fprintf(os.Stderr, "Warning: cannot read review file %q: %v\n", critPath, err)
		return session.CritJSON{}, false
	}
	return cj, true
}

// ResolveCommentScope decides which scope tags `crit comment` should stamp,
// based on the --scope flag, a running daemon's Focus, and the on-disk
// ActiveDiffScope.
func ResolveCommentScope(override CommentFocusOverride, outputDir string) (session.InheritedScope, error) {
	daemonFocus := session.ProbeDaemonFocus()

	switch override {
	case ScopeOverrideWorkingTree:
		return session.InheritedScope{}, nil
	case ScopeOverrideFullStack:
		return resolveExplicitCommentScope(daemonFocus, outputDir, session.DiffScopeFullStack, "full_stack",
			"--scope=full-stack: no active full-stack focus to attach to (start `crit --pr <n> --scope=full-stack` first)")
	case ScopeOverrideLayer:
		return resolveExplicitCommentScope(daemonFocus, outputDir, session.DiffScopeLayer, "layer",
			"--scope=layer: no active layer focus to attach to (start `crit --pr <n>` first)")
	case ScopeOverrideUnset:
		return resolveAutoCommentScope(daemonFocus, outputDir), nil
	}
	return session.InheritedScope{}, fmt.Errorf("invalid --scope value %q", override)
}

func resolveExplicitCommentScope(daemonFocus *session.Focus, outputDir string, want session.DiffScope, wantStr, errMsg string) (session.InheritedScope, error) {
	if daemonFocus != nil && daemonFocus.Kind == session.FocusRange && daemonFocus.DiffScope == want {
		return session.InheritedScope{
			HeadSHA:   daemonFocus.HeadSHA,
			BaseSHA:   daemonFocus.BaseSHA,
			PRNumber:  daemonFocus.PRNumber,
			DiffScope: wantStr,
		}, nil
	}
	if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope == wantStr {
		return session.InheritedScope{DiffScope: wantStr}, nil
	}
	return session.InheritedScope{}, fmt.Errorf("%s", errMsg)
}

func resolveAutoCommentScope(daemonFocus *session.Focus, outputDir string) session.InheritedScope {
	if daemonFocus != nil && daemonFocus.Kind == session.FocusRange {
		return session.InheritedScope{
			HeadSHA:   daemonFocus.HeadSHA,
			BaseSHA:   daemonFocus.BaseSHA,
			PRNumber:  daemonFocus.PRNumber,
			DiffScope: string(daemonFocus.DiffScope),
		}
	}
	if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope != "" {
		fmt.Fprintf(os.Stderr,
			"Note: stamping comment with diff_scope=%q from review file (no daemon running; head_sha unknown)\n",
			cj.ActiveDiffScope)
		return session.InheritedScope{DiffScope: cj.ActiveDiffScope}
	}
	return session.InheritedScope{}
}

// SetProbeDaemonFocusFnForTest replaces daemon focus probing during tests.
func SetProbeDaemonFocusFnForTest(fn func() *session.Focus) (restore func()) {
	return session.SetProbeDaemonFocusFnForTest(fn)
}
