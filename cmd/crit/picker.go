package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// StackEntry is one row in the picker's "Your stack" section. Sorted by
// distance from HEAD; smaller distance = closer to current.
//
// DefaultSHA is the repo's default-branch tip (resolved once per /api/picker
// call). The frontend needs it to construct a complete Focus when the user
// later flips to full-stack scope — without it, /api/focus returns 400 for
// full-stack requests because Focus.DefaultSHA is required (see
// session.go SetFocus full-stack guard).
type StackEntry struct {
	Label       string `json:"label"`
	PRNumber    int    `json:"pr_number,omitempty"`
	HeadSHA     string `json:"head_sha"`
	BaseSHA     string `json:"base_sha,omitempty"`
	BaseRefName string `json:"base_ref_name,omitempty"`
	DefaultSHA  string `json:"default_sha,omitempty"`
	Distance    int    `json:"distance"`
	Current     bool   `json:"current"`
}

// BranchEntry is one row in the picker's "Remote branches" section.
type BranchEntry struct {
	Name    string `json:"name"`
	HeadSHA string `json:"head_sha"`
}

// detectStack walks back from HEAD up to 20 commits, intersects with local
// branch tips and open-PR head SHAs, and labels each entry. Returns nil
// silently when the VCS isn't ready or the walk fails — the picker degrades
// to the "Other PRs" + "Branches" sections.
//
// Label tiers (highest priority first):
//  1. PR head match → "PR #N: <title>"
//  2. Local branch tip → "<branch>" (sapling: bookmark or draft commit subject)
//  3. Naked commit → "<commit subject (firstline)>" (git fallback for stacks
//     of unbranched commits; supports Sapling-style workflows on git too)
func detectStack(vcs VCS, repoRoot string, openPRs []PRSummary) ([]StackEntry, error) {
	const maxDepth = 20

	headSHAs, err := walkAncestors(vcs, repoRoot, maxDepth)
	if err != nil {
		return nil, err
	}
	headSet := make(map[string]int, len(headSHAs))
	for i, sha := range headSHAs {
		headSet[sha] = i // smaller i = closer to HEAD
	}

	branchTips, _ := localBranchTips(vcs, repoRoot)
	prByHead := make(map[string]PRSummary, len(openPRs))
	for _, pr := range openPRs {
		prByHead[pr.HeadRefOid] = pr
	}

	topicSHAs := topicChainSHAs(vcs, repoRoot)

	// For git, gate every tier by topicSHAs (i.e. commits in mergeBase..HEAD).
	// Without this, stale local branches whose tips happen to lie on HEAD's
	// first-parent ancestor line — but in the default branch's history, before
	// the merge-base — leak into the picker. Sapling already restricts the
	// ancestor walk to draft() commits, so its tier-1/tier-2 matches are safe
	// even when the topic-chain set isn't strictly applied.
	gateByTopic := vcs != nil && vcs.Name() == "git"

	var branchEntries []StackEntry // tier 1 (PR head) + tier 2 (local branch tip)
	var nakedEntries []StackEntry  // tier 3 (commit-subject fallback)
	for sha, distance := range headSet {
		if gateByTopic && !topicSHAs[sha] {
			continue
		}
		entry, isBranch := classifyStackSHA(sha, distance, prByHead, branchTips, vcs, repoRoot)
		if entry == nil {
			continue
		}
		if isBranch {
			branchEntries = append(branchEntries, *entry)
		} else {
			nakedEntries = append(nakedEntries, *entry)
		}
	}

	entries := mergeStackEntries(branchEntries, nakedEntries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Distance < entries[j].Distance })
	return assignStackBases(vcs, entries, repoRoot), nil
}

// mergeStackEntries combines branch-tier and naked-commit-tier stack
// entries, dropping any naked entry whose distance from HEAD exceeds the
// closest branch-tier entry. The dropped entries are commits subsumed by
// a branch's history (long-lived parent branches would otherwise leak
// dozens of unrelated commits into the picker as separate rows). Naked
// commits with no closer branch ancestor stay — they're the user's own
// unbranched WIP between HEAD and the nearest branch.
func mergeStackEntries(branchEntries, nakedEntries []StackEntry) []StackEntry {
	minBranchDist := -1
	for _, e := range branchEntries {
		if minBranchDist < 0 || e.Distance < minBranchDist {
			minBranchDist = e.Distance
		}
	}
	out := branchEntries
	for _, e := range nakedEntries {
		if minBranchDist >= 0 && e.Distance > minBranchDist {
			continue
		}
		out = append(out, e)
	}
	return out
}

// classifyStackSHA returns a StackEntry for the given SHA along with a flag
// indicating whether it's a branch-tier entry (tier 1 PR head or tier 2
// local branch tip) versus a naked-commit-subject fallback. Returns
// (nil, _) when no label can be derived (no PR, no branch, empty commit
// subject). Extracted to keep detectStack's per-SHA loop linear.
func classifyStackSHA(sha string, distance int, prByHead map[string]PRSummary, branchTips map[string]string, vcs VCS, repoRoot string) (*StackEntry, bool) {
	if pr, ok := prByHead[sha]; ok {
		return &StackEntry{
			Label:       fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
			PRNumber:    pr.Number,
			HeadSHA:     sha,
			BaseRefName: pr.BaseRefName,
			Distance:    distance,
		}, true
	}
	if branch, ok := branchTips[sha]; ok {
		return &StackEntry{
			Label:    branch,
			HeadSHA:  sha,
			Distance: distance,
		}, true
	}
	// Tier 3: naked ancestor commit. Subject lookup may return empty
	// (e.g. for a commit no longer in the local object store); skip.
	subject := commitSubjectFor(vcs, repoRoot, sha)
	if subject == "" {
		return nil, false
	}
	return &StackEntry{
		Label:    subject,
		HeadSHA:  sha,
		Distance: distance,
	}, false
}

// topicChainSHAs returns the set of ancestor SHAs that are reachable from
// HEAD but not from the default branch — i.e. the user's in-progress topic
// chain. Without this filter, naked-commit fallback labels would surface
// every recent commit on the default branch as a stack entry, drowning the
// breadcrumb in noise on long-lived feature branches.
func topicChainSHAs(vcs VCS, repoRoot string) map[string]bool {
	out := make(map[string]bool)
	if vcs == nil {
		return out
	}
	defaultBranch := vcs.DefaultBranch()
	if vcs.Name() == "git" {
		mergeBase, err := runGitInDir(repoRoot, "merge-base", defaultBranch, "HEAD")
		if err != nil {
			return out
		}
		baseSHA := strings.TrimSpace(mergeBase)
		if baseSHA == "" {
			return out
		}
		revs, err := runGitInDir(repoRoot, "rev-list", baseSHA+"..HEAD")
		if err != nil {
			return out
		}
		for _, sha := range splitNonEmpty(revs) {
			out[sha] = true
		}
		return out
	}
	if vcs.Name() == "jj" {
		revs, err := jjCommandInDir(repoRoot, "log", "-r", jjTopicChainRevset(repoRoot, 0), "--no-graph", "-T", "commit_id ++ \"\\n\"")
		if err != nil {
			return out
		}
		for _, sha := range splitNonEmpty(revs) {
			out[sha] = true
		}
		return out
	}
	// Sapling: draft() exactly captures the topic chain.
	revs, err := slCommandInDir(repoRoot, "log", "-r", "draft() & ::.", "-T", "{node}\n")
	if err != nil {
		return out
	}
	for _, sha := range splitNonEmpty(revs) {
		out[sha] = true
	}
	return out
}

// commitSubjectFor returns the first line of the commit message at sha,
// truncated to fit a breadcrumb entry. Empty result on any error — callers
// drop the entry rather than rendering an empty label.
func commitSubjectFor(vcs VCS, repoRoot, sha string) string {
	if vcs == nil {
		return ""
	}
	var subject string
	switch vcs.Name() {
	case "git":
		out, err := runGitInDir(repoRoot, "log", "-1", "--format=%s", sha)
		if err != nil {
			return ""
		}
		subject = strings.TrimSpace(out)
	case "jj":
		subject = jjCommitSubject(repoRoot, sha)
	default:
		out, err := slCommandInDir(repoRoot, "log", "-r", sha, "-T", "{desc|firstline}")
		if err != nil {
			return ""
		}
		subject = strings.TrimSpace(out)
	}
	if len(subject) > 60 {
		subject = subject[:60] + "\u2026"
	}
	return subject
}

// assignStackBases sets each entry's BaseSHA to the previous entry's HeadSHA,
// or merge-base with the default branch for the deepest entry. Best-effort —
// errors leave BaseSHA empty.
//
// Also stamps DefaultSHA (the literal default-branch tip) onto every
// entry. This is the diff base for full-stack scope, so navigating to
// any entry in the popover and flipping to full-stack always shows
// `default..entry`. The frontend uses this for full-stack POSTs to
// /api/focus.
func assignStackBases(vcs VCS, entries []StackEntry, repoRoot string) []StackEntry {
	if vcs == nil || len(entries) == 0 {
		return entries
	}
	defaultBranch := vcs.DefaultBranch()
	defaultSHA, _ := ResolveDefaultBranchSHA(vcs, repoRoot, defaultBranch)
	for i := range entries {
		entries[i].DefaultSHA = defaultSHA
		if i < len(entries)-1 {
			entries[i].BaseSHA = entries[i+1].HeadSHA
			continue
		}
		// Deepest: base = merge-base(default, head). Direct shell since the
		// VCS interface only exposes MergeBase(ref-vs-HEAD).
		switch vcs.Name() {
		case "git":
			out, err := runGitInDir(repoRoot, "merge-base", defaultBranch, entries[i].HeadSHA)
			if err == nil {
				entries[i].BaseSHA = strings.TrimSpace(out)
			}
		case "jj":
			baseForMerge := defaultSHA
			if baseForMerge == "" {
				if sha, err := resolveJJRevisionToCommitID(repoRoot, defaultBranch); err == nil {
					baseForMerge = sha
				}
			}
			if baseForMerge != "" {
				if mb, err := jjMergeBase(repoRoot, entries[i].HeadSHA, baseForMerge); err == nil {
					entries[i].BaseSHA = strings.TrimSpace(mb)
				}
			}
		default:
			out, err := slCommandInDir(repoRoot, "log", "-r",
				fmt.Sprintf("ancestor(%s, %s)", entries[i].HeadSHA, defaultBranch),
				"-T", "{node}")
			if err == nil {
				entries[i].BaseSHA = strings.TrimSpace(out)
			}
		}
	}
	return entries
}

// pickerResponse is the wire shape returned by /api/picker.
//
// Errors is retained for older clients that just join-and-display. New clients
// should prefer the per-source *Error fields so they can render gracefully —
// e.g. show local stack entries even when `gh pr list` fails, and only render
// a single subtle "Couldn't list remote PRs" footnote rather than a stack of
// raw error strings inside the picker UI (issue: picker renders gh failures
// as visible entries).
type pickerResponse struct {
	Current           Focus         `json:"current"`
	DefaultBranchName string        `json:"default_branch_name,omitempty"`
	Stack             []StackEntry  `json:"stack"`
	OtherPRs          []PRSummary   `json:"other_prs"`
	Branches          []BranchEntry `json:"branches"`
	Errors            []string      `json:"errors,omitempty"`
	PRListError       string        `json:"pr_list_error,omitempty"`
	StackError        string        `json:"stack_error,omitempty"`
	BranchesError     string        `json:"branches_error,omitempty"`
}

// handlePicker returns the focus picker payload: current focus, detected stack,
// other open PRs, and recent remote branches not already represented.
// Best-effort — gh failures degrade to per-source error fields without 5xx.
func (s *Server) handlePicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := pickerResponse{}
	sess := s.session.Load()
	sess.mu.RLock()
	vcs := sess.VCS
	repoRoot := sess.RepoRoot
	resp.Current = sess.Focus
	sess.mu.RUnlock()
	if vcs != nil {
		resp.DefaultBranchName = vcs.DefaultBranch()
	}

	openPRs, err := s.openPRsFromCache()
	if err != nil {
		resp.PRListError = err.Error()
		resp.Errors = append(resp.Errors, err.Error())
	}

	stack, sErr := detectStack(vcs, repoRoot, openPRs)
	if sErr != nil {
		resp.StackError = sErr.Error()
		resp.Errors = append(resp.Errors, sErr.Error())
	}
	resp.Stack = stack

	covered := make(map[string]bool)
	for _, e := range stack {
		covered[e.HeadSHA] = true
	}
	for _, pr := range openPRs {
		if !covered[pr.HeadRefOid] {
			resp.OtherPRs = append(resp.OtherPRs, pr)
			covered[pr.HeadRefOid] = true
		}
	}

	if vcs != nil {
		defaultBranch := vcs.DefaultBranch()
		branches, bErr := remoteBranchTips(vcs, repoRoot, defaultBranch)
		if bErr != nil {
			resp.BranchesError = bErr.Error()
			resp.Errors = append(resp.Errors, bErr.Error())
		}
		for _, b := range branches {
			if !covered[b.HeadSHA] {
				resp.Branches = append(resp.Branches, b)
			}
		}
	}

	writeJSON(w, resp)
}

// openPRsFromCache returns the cached open-PR list, or nil + error when gh
// isn't available. Tolerates missing s.prList for older test setups.
func (s *Server) openPRsFromCache() ([]PRSummary, error) {
	if s.prList == nil {
		return nil, nil
	}
	return s.prList.get()
}
