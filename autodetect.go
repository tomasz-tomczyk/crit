package main

import (
	"fmt"
	"os"
	"strings"
)

// detectPRInfoFn is the live function that detects a PR for the current
// branch. Indirected through a package var so tests can stub it without
// requiring `gh` on PATH or network access.
var detectPRInfoFn = detectPRInfo

// autoDetectMaxDepth caps the first-parent ancestry walk for local-stack
// detection. Matches the picker's stack walk depth.
const autoDetectMaxDepth = 20

// autoDetectStackedFocus checks whether the current branch sits on a stacked
// PR or local branch stack. Returns a Range Focus if detected, nil otherwise.
//
// Errors during detection (gh missing, network down, etc.) are swallowed —
// auto-detect is best-effort and falls back to working-tree mode.
func autoDetectStackedFocus(vcs VCS, repoRoot string) *Focus {
	if vcs == nil {
		return nil
	}
	// 1. Try the pushed-PR path.
	if info := detectPRInfoFn(); info != nil && IsStackedPR(info, vcs) {
		full, err := fetchPRByNumber(info.Number)
		switch {
		case err != nil:
			// We had a partial detection (PR number, but couldn't load
			// metadata). Surface the failure once on stderr so users
			// understand why auto-detect didn't promote them into a range
			// view. The common "no PR detected" path stays silent.
			fmt.Fprintf(os.Stderr,
				"crit: detected PR #%d but couldn't fetch metadata: %v; falling back to working-tree\n",
				info.Number, err)
		case full != nil:
			if focus := buildRangeFocusFromPR(full, vcs, repoRoot); focus != nil {
				return focus
			}
		}
	}
	// 2. Try the local-stack path.
	if focus := detectLocalStackFocus(vcs, repoRoot); focus != nil {
		return focus
	}
	return nil
}

// buildRangeFocusFromPR mirrors resolveFocusFromPR but takes an already-fetched
// PRInfo and skips the local-fetch side effects (auto-detect must not mutate
// the local object store). DefaultSHA is best-effort; full-stack scope is not
// the auto-detect default, so a missing DefaultSHA is not fatal.
func buildRangeFocusFromPR(info *PRInfo, vcs VCS, repoRoot string) *Focus {
	if info == nil {
		return nil
	}
	forkURL := ""
	if info.IsCrossRepository {
		forkURL = info.HeadRepoURL
	}
	defaultBranch := ""
	if vcs != nil {
		defaultBranch = vcs.DefaultBranch()
	}
	defaultSHA, _ := ResolveDefaultBranchSHA(vcs, repoRoot, defaultBranch)
	return &Focus{
		Kind:        FocusRange,
		PRNumber:    info.Number,
		PRURL:       info.URL,
		Label:       fmt.Sprintf("PR #%d: %s", info.Number, info.Title),
		BaseSHA:     info.BaseRefOid,
		HeadSHA:     info.HeadRefOid,
		DefaultSHA:  defaultSHA,
		ForkURL:     forkURL,
		BaseRefName: info.BaseRefName,
		HeadRefName: info.HeadRefName,
		DiffScope:   DiffScopeLayer,
		IsStacked:   IsStackedPR(info, vcs),
	}
}

// detectLocalStackFocus walks first-parent ancestors of HEAD (capped at
// autoDetectMaxDepth) and looks for the FIRST ancestor that is the tip of
// a local or remote-tracking branch other than the default branch. When
// found, builds a Range focus pinned to that ancestor (BaseSHA) and HEAD.
//
// Returns nil if:
//   - no ancestors match
//   - the only matches are the default branch or its remote tracking ref
//   - HEAD itself is the default-branch tip (we're sitting on main, not
//     stacked on top of it)
func detectLocalStackFocus(vcs VCS, repoRoot string) *Focus {
	if vcs == nil {
		return nil
	}
	ancestors, err := walkAncestors(vcs, repoRoot, autoDetectMaxDepth)
	if err != nil || len(ancestors) == 0 {
		return nil
	}
	headSHA := ancestors[0]
	defaultBranch := vcs.DefaultBranch()
	defaultSHA, _ := ResolveDefaultBranchSHA(vcs, repoRoot, defaultBranch)
	if defaultSHA != "" && headSHA == defaultSHA {
		// HEAD is on the default branch — nothing to focus on.
		return nil
	}

	tipLabels := stackTipLabels(vcs, repoRoot, defaultBranch)

	// Walk parents (skip ancestors[0] = HEAD itself). First match wins.
	for _, sha := range ancestors[1:] {
		label, ok := tipLabels[sha]
		if !ok {
			continue
		}
		return &Focus{
			Kind:       FocusRange,
			BaseSHA:    sha,
			HeadSHA:    headSHA,
			DefaultSHA: defaultSHA,
			Label:      fmt.Sprintf("%s..HEAD", label),
			DiffScope:  DiffScopeLayer,
			IsStacked:  true,
		}
	}
	return nil
}

// stackTipLabels returns a map of branch-tip SHA → display label, covering
// both local and remote-tracking branches, excluding the default branch and
// its remote-tracking counterpart. The returned label is preferred for
// display in the Focus.Label.
func stackTipLabels(vcs VCS, repoRoot, defaultBranch string) map[string]string {
	labels := make(map[string]string)
	// Local branches first — they take priority since they reflect the user's
	// active workflow more directly than remote-tracking refs.
	if local, err := localBranchTips(vcs, repoRoot); err == nil {
		for sha, name := range local {
			if name == defaultBranch {
				continue
			}
			labels[sha] = name
		}
	}
	// Remote-tracking branches fill in any SHAs not already labeled.
	if remotes, err := remoteBranchTips(vcs, repoRoot, defaultBranch); err == nil {
		for _, b := range remotes {
			if _, exists := labels[b.HeadSHA]; exists {
				continue
			}
			// Defensive: remoteBranchTipsGit already filters origin/<default>,
			// but guard here too in case a future caller passes raw entries.
			if isDefaultRemoteRef(b.Name, defaultBranch) {
				continue
			}
			labels[b.HeadSHA] = b.Name
		}
	}
	return labels
}

// isDefaultRemoteRef reports whether name is a remote-tracking ref pointing
// at the default branch (e.g. "origin/main" when defaultBranch is "main").
func isDefaultRemoteRef(name, defaultBranch string) bool {
	if defaultBranch == "" {
		return false
	}
	if name == defaultBranch {
		return true
	}
	return strings.HasSuffix(name, "/"+defaultBranch)
}
