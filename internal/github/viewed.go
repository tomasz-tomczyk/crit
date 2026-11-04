package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

const (
	ghViewed = "VIEWED"

	// viewedMutationBatch caps the aliased mutations sent in one request.
	viewedMutationBatch = 50
	// compareFileLimit is the most files GitHub lists in a compare response;
	// a response this long may be truncated.
	compareFileLimit = 300
)

// prViewedState is the viewer's viewed state on a PR, at its current head.
type prViewedState struct {
	nodeID  string
	headSHA string
	files   map[string]string // path -> VIEWED, UNVIEWED or DISMISSED
}

type viewedStateResp struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ID         string `json:"id"`
				HeadRefOid string `json:"headRefOid"`
				Files      struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Path              string `json:"path"`
						ViewerViewedState string `json:"viewerViewedState"`
					} `json:"nodes"`
				} `json:"files"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// fetchPRViewedState reads every file's viewed state, following pagination.
func fetchPRViewedState(id forge.ChangeID) (prViewedState, error) {
	owner, name, err := resolveRepoOwnerName(id)
	if err != nil {
		return prViewedState{}, err
	}
	const query = `query($owner:String!,$name:String!,$pr:Int!,$after:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$pr){
      id
      headRefOid
      files(first:100,after:$after){
        pageInfo{ hasNextPage endCursor }
        nodes{ path viewerViewedState }
      }
    }
  }
}`
	state := prViewedState{files: make(map[string]string)}
	cursor := ""
	for {
		args := []string{"api", "graphql",
			"-f", "owner=" + owner,
			"-f", "name=" + name,
			"-F", fmt.Sprintf("pr=%d", id.Number),
			"-f", "query=" + query,
		}
		if cursor != "" {
			args = append(args, "-f", "after="+cursor)
		}
		out, err := exec.Command("gh", args...).Output()
		if err != nil {
			return prViewedState{}, fmt.Errorf("fetching PR viewed files: %w", err)
		}
		var resp viewedStateResp
		if err := json.Unmarshal(out, &resp); err != nil {
			return prViewedState{}, fmt.Errorf("parsing PR viewed files: %w", err)
		}
		pr := resp.Data.Repository.PullRequest
		state.nodeID, state.headSHA = pr.ID, pr.HeadRefOid
		for _, f := range pr.Files.Nodes {
			state.files[f.Path] = f.ViewerViewedState
		}
		if !pr.Files.PageInfo.HasNextPage {
			break
		}
		cursor = pr.Files.PageInfo.EndCursor
	}
	if state.nodeID == "" {
		return prViewedState{}, fmt.Errorf("PR #%d not found", id.Number)
	}
	return state, nil
}

// compareChangedFiles lists the paths that differ between two commits. ok is
// false when the answer cannot be trusted. GitHub only offers a merge-base
// comparison, which is exact only while from is an ancestor of to: after a
// force-push it can miss changes, so anything but ahead or identical is
// refused.
func compareChangedFiles(id forge.ChangeID, from, to string) (paths map[string]struct{}, ok bool) {
	owner, name, err := resolveRepoOwnerName(id)
	if err != nil {
		return nil, false
	}
	out, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/%s/compare/%s...%s", owner, name, from, to),
		"--jq", `{status, files: [.files[]? | {filename, previous_filename}]}`,
	).Output()
	if err != nil {
		return nil, false
	}
	var resp struct {
		Status string `json:"status"`
		Files  []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, false
	}
	if (resp.Status != "ahead" && resp.Status != "identical") || len(resp.Files) >= compareFileLimit {
		return nil, false
	}
	paths = make(map[string]struct{}, len(resp.Files)*2)
	for _, f := range resp.Files {
		paths[f.Filename] = struct{}{}
		if f.PreviousFilename != "" {
			paths[f.PreviousFilename] = struct{}{}
		}
	}
	return paths, true
}

// viewedPushPlan is what a push would change on GitHub.
type viewedPushPlan struct {
	mark   []string
	unmark []string
	stale  []string // marks made on a version the PR no longer has
}

// planViewedPush compares local marks with GitHub. stale reports whether path
// may have changed since sha. A mark on an old version is not carried over:
// ticking it would claim a review of code nobody looked at, and unticking it
// could undo a newer tick made on GitHub.
func planViewedPush(entries map[string]session.ViewedFile, prNumber int, remote prViewedState,
	stale func(sha, path string) bool) viewedPushPlan {
	var plan viewedPushPlan
	for path, e := range entries {
		state, inPR := remote.files[path]
		if !inPR || e.Auto || e.HeadSHA == "" || e.ChangeNumber != prNumber {
			continue
		}
		if e.Viewed == (state == ghViewed) {
			continue
		}
		switch {
		case stale(e.HeadSHA, path):
			plan.stale = append(plan.stale, path)
		case e.Viewed:
			plan.mark = append(plan.mark, path)
		default:
			plan.unmark = append(plan.unmark, path)
		}
	}
	sort.Strings(plan.mark)
	sort.Strings(plan.unmark)
	sort.Strings(plan.stale)
	return plan
}

// buildViewedPushPlan plans a push, comparing each older revision once.
func buildViewedPushPlan(id forge.ChangeID, cj session.CritJSON, remote prViewedState) viewedPushPlan {
	type comparison struct {
		paths map[string]struct{}
		ok    bool
	}
	seen := make(map[string]comparison)
	return planViewedPush(cj.ViewedFiles, id.Number, remote, func(sha, path string) bool {
		if sha == remote.headSHA {
			return false
		}
		c, done := seen[sha]
		if !done {
			c.paths, c.ok = compareChangedFiles(id, sha, remote.headSHA)
			seen[sha] = c
		}
		if !c.ok {
			return true
		}
		_, changed := c.paths[path]
		return changed
	})
}

// viewedMutation builds one request holding n aliased calls of mutation.
func viewedMutation(mutation string, n int) string {
	var vars, calls strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&vars, ",$p%d:String!", i)
		fmt.Fprintf(&calls, "m%d: %s(input:{pullRequestId:$pr,path:$p%d}){ clientMutationId }\n", i, mutation, i)
	}
	return "mutation($pr:ID!" + vars.String() + "){\n" + calls.String() + "}"
}

// runViewedMutation applies mutation to paths in batches and returns how many
// GitHub accepted. It stops at the first failed batch; the next push plans
// from GitHub's state, so it retries whatever did not land.
func runViewedMutation(mutation, nodeID string, paths []string) (int, error) {
	done := 0
	for start := 0; start < len(paths); start += viewedMutationBatch {
		batch := paths[start:min(start+viewedMutationBatch, len(paths))]
		args := []string{"api", "graphql", "-f", "pr=" + nodeID, "-f", "query=" + viewedMutation(mutation, len(batch))}
		for i, path := range batch {
			args = append(args, "-f", fmt.Sprintf("p%d=%s", i, path))
		}
		// gh exits non-zero on a partial failure but still prints the data.
		out, err := exec.Command("gh", args...).Output()
		var resp struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if json.Unmarshal(out, &resp) == nil {
			for _, result := range resp.Data {
				if string(result) != "null" {
					done++
				}
			}
		}
		if err != nil {
			return done, fmt.Errorf("%s: %w", mutation, err)
		}
	}
	return done, nil
}

func describeViewedPlan(plan viewedPushPlan) string {
	if len(plan.mark)+len(plan.unmark)+len(plan.stale) == 0 {
		return "Viewed files: already in sync."
	}
	line := fmt.Sprintf("Viewed files: %d to mark, %d to unmark", len(plan.mark), len(plan.unmark))
	if len(plan.stale) > 0 {
		line += fmt.Sprintf(", %d skipped (changed since viewed, or could not be checked)", len(plan.stale))
	}
	return line + "."
}

// printViewedDryRun prints what `crit push --viewed` would change.
func printViewedDryRun(ctx pushContext) error {
	remote, err := fetchPRViewedState(ctx.id)
	if err != nil {
		return err
	}
	plan := buildViewedPushPlan(ctx.id, ctx.cj, remote)
	fmt.Println(describeViewedPlan(plan))
	for _, group := range []struct {
		label string
		paths []string
	}{{"mark", plan.mark}, {"unmark", plan.unmark}, {"skip", plan.stale}} {
		for _, path := range group.paths {
			fmt.Printf("  %s %s\n", group.label, path)
		}
	}
	return nil
}

// pushViewedFiles applies local marks to the PR.
func pushViewedFiles(ctx pushContext) error {
	remote, err := fetchPRViewedState(ctx.id)
	if err != nil {
		return err
	}
	plan := buildViewedPushPlan(ctx.id, ctx.cj, remote)
	marked, markErr := runViewedMutation("markFileAsViewed", remote.nodeID, plan.mark)
	unmarked, unmarkErr := 0, error(nil)
	if markErr == nil {
		unmarked, unmarkErr = runViewedMutation("unmarkFileAsViewed", remote.nodeID, plan.unmark)
	}
	if len(plan.mark)+len(plan.unmark)+len(plan.stale) == 0 {
		fmt.Println(describeViewedPlan(plan))
		return nil
	}
	line := fmt.Sprintf("Viewed files: marked %d, unmarked %d", marked, unmarked)
	if len(plan.stale) > 0 {
		line += fmt.Sprintf(", skipped %d (changed since viewed, or could not be checked)", len(plan.stale))
	}
	fmt.Println(line + ".")
	if markErr != nil {
		return markErr
	}
	return unmarkErr
}

// mergeRemoteViewed adds GitHub's viewed files to cj and returns how many it
// added. GitHub's tick applies to the PR's current head, so it replaces a
// local tick pinned anywhere else. A local untick stays: it is a decision
// that has not been pushed yet.
func mergeRemoteViewed(cj *session.CritJSON, prNumber int, remote prViewedState) int {
	added := 0
	for path, state := range remote.files {
		if state != ghViewed {
			continue
		}
		if e, exists := cj.ViewedFiles[path]; exists &&
			(!e.Viewed || (e.HeadSHA == remote.headSHA && e.ChangeNumber == prNumber)) {
			continue
		}
		if cj.ViewedFiles == nil {
			cj.ViewedFiles = make(map[string]session.ViewedFile)
		}
		cj.ViewedFiles[path] = session.ViewedFile{Viewed: true, HeadSHA: remote.headSHA, ChangeNumber: prNumber}
		added++
	}
	return added
}
