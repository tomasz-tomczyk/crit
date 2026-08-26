package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// ListOpenMRsFn is wired by cmd/crit to avoid a server→gitlab dependency.
var ListOpenMRsFn func(context.Context) ([]forge.ChangeSummary, error)

// DetectForgeKindFn is wired by cmd/crit so the working-tree picker can show
// GitLab MRs before the user has entered an MR focus.
var DetectForgeKindFn func() forge.Kind

// StackEntry is one row in the picker's "Your stack" section. Sorted by
// distance from HEAD; smaller distance = closer to current.
type StackEntry struct {
	Label       string `json:"label"`
	PRNumber    int    `json:"pr_number,omitempty"`
	MRNumber    int    `json:"mr_number,omitempty"`
	HeadSHA     string `json:"head_sha"`
	BaseSHA     string `json:"base_sha,omitempty"`
	BaseRefName string `json:"base_ref_name,omitempty"`
	DefaultSHA  string `json:"default_sha,omitempty"`
	Distance    int    `json:"distance"`
	Current     bool   `json:"current"`
}

func detectStack(v vcs.VCS, repoRoot string, openPRs []github.PRSummary) ([]StackEntry, error) { //nolint:unparam // compatibility wrapper retained for existing callers/tests
	return detectStackForKind(v, repoRoot, openPRs, false)
}

func detectStackForKind(v vcs.VCS, repoRoot string, openPRs []github.PRSummary, gitlab bool) ([]StackEntry, error) {
	const maxDepth = 20

	headSHAs, err := vcs.WalkAncestors(v, repoRoot, maxDepth)
	if err != nil {
		return nil, err
	}
	headSet := make(map[string]int, len(headSHAs))
	for i, sha := range headSHAs {
		headSet[sha] = i
	}

	branchTips, _ := vcs.LocalBranchTips(v, repoRoot)
	prByHead := make(map[string]github.PRSummary, len(openPRs))
	for _, pr := range openPRs {
		prByHead[pr.HeadRefOid] = pr
	}

	topicSHAs := vcs.TopicChainSHAs(v, repoRoot)
	gateByTopic := v != nil && v.Name() == "git"

	var branchEntries []StackEntry
	var nakedEntries []StackEntry
	for sha, distance := range headSet {
		if gateByTopic && !topicSHAs[sha] {
			continue
		}
		entry, isBranch := classifyStackSHAForKind(sha, distance, prByHead, branchTips, v, repoRoot, gitlab)
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
	return assignStackBases(v, entries, repoRoot), nil
}

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

func classifyStackSHA(sha string, distance int, prByHead map[string]github.PRSummary, branchTips map[string]string, v vcs.VCS, repoRoot string) (*StackEntry, bool) {
	return classifyStackSHAForKind(sha, distance, prByHead, branchTips, v, repoRoot, false)
}

func classifyStackSHAForKind(sha string, distance int, prByHead map[string]github.PRSummary, branchTips map[string]string, v vcs.VCS, repoRoot string, gitlab bool) (*StackEntry, bool) {
	if pr, ok := prByHead[sha]; ok {
		entry := &StackEntry{
			Label:       fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
			PRNumber:    pr.Number,
			HeadSHA:     sha,
			BaseRefName: pr.BaseRefName,
			Distance:    distance,
		}
		if gitlab {
			entry.Label = fmt.Sprintf("MR !%d: %s", pr.Number, pr.Title)
			entry.MRNumber = pr.Number
			entry.PRNumber = 0
		}
		return entry, true
	}
	if branch, ok := branchTips[sha]; ok {
		return &StackEntry{
			Label:    branch,
			HeadSHA:  sha,
			Distance: distance,
		}, true
	}
	subject := vcs.CommitSubjectFor(v, repoRoot, sha)
	if subject == "" {
		return nil, false
	}
	return &StackEntry{
		Label:    subject,
		HeadSHA:  sha,
		Distance: distance,
	}, false
}

func assignStackBases(v vcs.VCS, entries []StackEntry, repoRoot string) []StackEntry {
	if v == nil || len(entries) == 0 {
		return entries
	}
	defaultBranch := v.DefaultBranch()
	defaultSHA, _ := vcs.ResolveDefaultBranchSHA(v, repoRoot, defaultBranch)
	for i := range entries {
		entries[i].DefaultSHA = defaultSHA
		if i < len(entries)-1 {
			entries[i].BaseSHA = entries[i+1].HeadSHA
			continue
		}
		switch v.Name() {
		case "git":
			out, err := vcs.RunGitInDir(repoRoot, "merge-base", defaultBranch, entries[i].HeadSHA)
			if err == nil {
				entries[i].BaseSHA = strings.TrimSpace(out)
			}
		case "jj":
			baseForMerge := defaultSHA
			if baseForMerge == "" {
				if sha, err := vcs.ResolveJJRevisionToCommitID(repoRoot, defaultBranch); err == nil {
					baseForMerge = sha
				}
			}
			if baseForMerge != "" {
				if mb, err := vcs.JJMergeBase(repoRoot, entries[i].HeadSHA, baseForMerge); err == nil {
					entries[i].BaseSHA = strings.TrimSpace(mb)
				}
			}
		default:
			out, err := vcs.SLCommandInDir(repoRoot, "log", "-r",
				fmt.Sprintf("ancestor(%s, %s)", entries[i].HeadSHA, defaultBranch),
				"-T", "{node}")
			if err == nil {
				entries[i].BaseSHA = strings.TrimSpace(out)
			}
		}
	}
	return entries
}

type pickerResponse struct {
	Current           Focus              `json:"current"`
	DefaultBranchName string             `json:"default_branch_name,omitempty"`
	Stack             []StackEntry       `json:"stack"`
	OtherPRs          []github.PRSummary `json:"other_prs"`
	OtherMRs          []github.PRSummary `json:"other_mrs,omitempty"`
	Branches          []vcs.BranchEntry  `json:"branches"`
	Errors            []string           `json:"errors,omitempty"`
	PRListError       string             `json:"pr_list_error,omitempty"`
	StackError        string             `json:"stack_error,omitempty"`
	BranchesError     string             `json:"branches_error,omitempty"`
}

func (s *Server) handlePicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := pickerResponse{}
	sess := s.session.Load()
	v, repoRoot, focus := sess.PickerContext()
	resp.Current = focus
	if v != nil {
		resp.DefaultBranchName = v.DefaultBranch()
	}

	openPRs, gitlabMode, err := s.openChanges(r.Context(), focus)
	if err != nil {
		resp.PRListError = err.Error()
		resp.Errors = append(resp.Errors, err.Error())
	}

	stack, sErr := detectStackForKind(v, repoRoot, openPRs, gitlabMode)
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
			if gitlabMode {
				resp.OtherMRs = append(resp.OtherMRs, pr)
			} else {
				resp.OtherPRs = append(resp.OtherPRs, pr)
			}
			covered[pr.HeadRefOid] = true
		}
	}

	if v != nil {
		defaultBranch := v.DefaultBranch()
		branches, bErr := vcs.RemoteBranchTips(v, repoRoot, defaultBranch)
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

func (s *Server) openChanges(ctx context.Context, focus Focus) ([]github.PRSummary, bool, error) {
	kind := forge.Kind(focus.Forge)
	if kind == "" && DetectForgeKindFn != nil {
		kind = DetectForgeKindFn()
	}
	if kind != forge.GitLab || ListOpenMRsFn == nil {
		prs, err := s.openPRsFromCache()
		return prs, false, err
	}
	changes, err := ListOpenMRsFn(ctx)
	if err != nil {
		return nil, true, err
	}
	mrs := make([]github.PRSummary, 0, len(changes))
	for _, change := range changes {
		mrs = append(mrs, github.PRSummary{
			Number: change.Number, Title: change.Title, URL: change.URL,
			HeadRefName: change.HeadRefName, HeadRefOid: change.HeadSHA,
			BaseRefName: change.BaseRefName, IsDraft: change.Draft,
		})
	}
	return mrs, true, nil
}

func (s *Server) openPRsFromCache() ([]github.PRSummary, error) {
	if s.prList == nil {
		return nil, nil
	}
	return s.prList.Get()
}
