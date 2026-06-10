package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// StackEntry is one row in the picker's "Your stack" section. Sorted by
// distance from HEAD; smaller distance = closer to current.
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

func detectStack(v vcs.VCS, repoRoot string, openPRs []github.PRSummary) ([]StackEntry, error) {
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
		entry, isBranch := classifyStackSHA(sha, distance, prByHead, branchTips, v, repoRoot)
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

	openPRs, err := s.openPRsFromCache()
	if err != nil {
		resp.PRListError = err.Error()
		resp.Errors = append(resp.Errors, err.Error())
	}

	stack, sErr := detectStack(v, repoRoot, openPRs)
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

func (s *Server) openPRsFromCache() ([]github.PRSummary, error) {
	if s.prList == nil {
		return nil, nil
	}
	return s.prList.Get()
}
