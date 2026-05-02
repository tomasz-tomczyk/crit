package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ghComment represents a GitHub PR review comment from the API.
type ghComment struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`       // end line in the diff (RIGHT side = new file line)
	StartLine int    `json:"start_line"` // start line for multi-line comments (0 if single-line)
	Side      string `json:"side"`       // "RIGHT" or "LEFT"
	Body      string `json:"body"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	// Note: GitHub's /pulls/.../comments REST endpoint returns only `login`
	// per user. We resolve the display name lazily via /users/{login} —
	// see userNameCache.
	CreatedAt   string `json:"created_at"`
	InReplyToID int64  `json:"in_reply_to_id"`
}

// requireGH checks that the gh CLI is installed and authenticated.
func requireGH() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found. Install it: https://cli.github.com")
	}
	cmd := exec.Command("gh", "auth", "status")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh is not authenticated. Run: gh auth login")
	}
	return nil
}

// detectPR returns the PR number for the current branch.
// If prFlag is non-zero, it's used directly.
func detectPR(prFlag int) (int, error) {
	if prFlag > 0 {
		return prFlag, nil
	}
	out, err := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number").Output()
	if err != nil {
		return 0, fmt.Errorf("no PR found for current branch (try: crit pull <pr-number>)")
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("unexpected PR number: %s", string(out))
	}
	return n, nil
}

// PRInfo holds metadata about the PR for the current branch.
type PRInfo struct {
	URL          string `json:"url"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	IsDraft      bool   `json:"isDraft"`
	State        string `json:"state"`
	Body         string `json:"body"`
	BaseRefName  string `json:"baseRefName"`
	HeadRefName  string `json:"headRefName"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	AuthorLogin  string `json:"authorLogin"`
	CreatedAt    string `json:"createdAt"`

	// SHA pins from `gh pr view`. Populated by C5 fetchPRByNumber.
	BaseRefOid        string `json:"baseRefOid,omitempty"`
	HeadRefOid        string `json:"headRefOid,omitempty"`
	HeadRepoURL       string `json:"headRepoURL,omitempty"`       // fork URL when IsCrossRepository
	IsCrossRepository bool   `json:"isCrossRepository,omitempty"` // PR head is on a fork
}

// prAuthor is used to unmarshal the nested author field from gh output.
// Name is GitHub's optional display name; we prefer it over Login when set.
type prAuthor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// prHeadRepo carries the URL of the PR head's source repo. For cross-repo
// PRs (forks), this is the contributor's fork; for same-repo PRs it equals
// the base repo.
type prHeadRepo struct {
	URL string `json:"url"`
}

// displayName returns name when set, falling back to login. Used to convert
// GitHub-imported author identities into the friendlier display string we
// pass through to crit-web.
func displayName(login, name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return login
}

// userNameCache memoizes login → display-name lookups for the duration of a
// single `crit pull`. The /pulls/.../comments REST payload only returns
// `login`, so we hit /users/{login} once per unique commenter.
type userNameCache map[string]string

// fetchGHUserName is the seam used by userNameCache.lookup to resolve a login
// to a display name via the GitHub API. Tests override this; production code
// uses the default that shells out to `gh api`.
var fetchGHUserName = ghAPIUserName

// ghAPIUserName shells out to `gh api users/<login>` and returns the user's
// display name (empty string if unset).
func ghAPIUserName(login string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "api", "users/"+login, "--jq", ".name // \"\"").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// lookup returns the display name for a login, fetching from GitHub on cache
// miss. On any error or missing name, returns login (always a valid fallback)
// and caches that fallback so the warning is logged at most once per login.
func (c userNameCache) lookup(login string) string {
	if login == "" {
		return ""
	}
	if cached, ok := c[login]; ok {
		return cached
	}
	name, err := fetchGHUserName(login)
	if err != nil {
		log.Printf("warning: gh api users/%s: %v", login, err)
		c[login] = login
		return login
	}
	resolved := displayName(login, name)
	c[login] = resolved
	return resolved
}

// prInfoRaw mirrors the gh JSON output shape (author is nested).
type prInfoRaw struct {
	URL               string     `json:"url"`
	Number            int        `json:"number"`
	Title             string     `json:"title"`
	IsDraft           bool       `json:"isDraft"`
	State             string     `json:"state"`
	Body              string     `json:"body"`
	BaseRefName       string     `json:"baseRefName"`
	HeadRefName       string     `json:"headRefName"`
	BaseRefOid        string     `json:"baseRefOid"`
	HeadRefOid        string     `json:"headRefOid"`
	IsCrossRepository bool       `json:"isCrossRepository"`
	HeadRepository    prHeadRepo `json:"headRepository"`
	Additions         int        `json:"additions"`
	Deletions         int        `json:"deletions"`
	ChangedFiles      int        `json:"changedFiles"`
	Author            prAuthor   `json:"author"`
	CreatedAt         string     `json:"createdAt"`
}

// prJSONFields is the comma-separated `--json` field list passed to
// `gh pr view`. Shared between detectPRInfo and fetchPRByNumber so we extract
// the same shape regardless of which entry point we used.
const prJSONFields = "number,url,title,isDraft,state,body,baseRefName,headRefName,baseRefOid,headRefOid,isCrossRepository,headRepository,additions,deletions,changedFiles,author,createdAt"

// detectPRInfo returns PR metadata for the current branch.
// Returns nil if gh is unavailable, no PR exists, or the PR is merged/closed
// (to avoid associating a new local branch with a stale PR that had the same name).
func detectPRInfo() *PRInfo {
	if err := requireGH(); err != nil {
		return nil
	}
	out, err := exec.Command("gh", "pr", "view", "--json", prJSONFields).Output()
	if err != nil {
		return nil
	}
	info, err := parsePRViewJSON(out)
	if err != nil {
		return nil
	}
	if info.URL == "" || info.State == "MERGED" || info.State == "CLOSED" {
		return nil
	}
	return info
}

// parsePRViewJSON decodes `gh pr view --json` output into a PRInfo. Factored
// out so tests can drive it with fixture bytes without invoking gh.
func parsePRViewJSON(b []byte) (*PRInfo, error) {
	var raw prInfoRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parsing gh pr view: %w", err)
	}
	return &PRInfo{
		URL:               raw.URL,
		Number:            raw.Number,
		Title:             raw.Title,
		IsDraft:           raw.IsDraft,
		State:             raw.State,
		Body:              raw.Body,
		BaseRefName:       raw.BaseRefName,
		HeadRefName:       raw.HeadRefName,
		BaseRefOid:        raw.BaseRefOid,
		HeadRefOid:        raw.HeadRefOid,
		HeadRepoURL:       raw.HeadRepository.URL,
		IsCrossRepository: raw.IsCrossRepository,
		Additions:         raw.Additions,
		Deletions:         raw.Deletions,
		ChangedFiles:      raw.ChangedFiles,
		AuthorLogin:       displayName(raw.Author.Login, raw.Author.Name),
		CreatedAt:         raw.CreatedAt,
	}, nil
}

// fetchPRByNumberFn is the live function that hits `gh`. Indirected through a
// package var so tests can stub it without a real gh dependency. Tests that
// swap this should also reset prMetaCache (see withFetchPRByNumber) or the
// new stub will be shadowed by a previous test's cached PRInfo.
var fetchPRByNumberFn = fetchPRByNumberReal

// fetchPRByNumber resolves a PR by explicit number using `gh pr view <num>`,
// memoized for the daemon's lifetime via prMetaCache so repeated focus
// switches return instantly. Unlike detectPRInfo, this does not filter
// MERGED/CLOSED — a user explicitly asking to review --pr <num> can review a
// merged PR (the comment-anchoring rules still apply because the head SHA is
// fixed). The cache is invalidated by invalidatePRCache after force-push
// detection (Session.SetFocus) and `crit pull`.
func fetchPRByNumber(num int) (*PRInfo, error) {
	return prMetaCache.get(num)
}

func fetchPRByNumberReal(num int) (*PRInfo, error) {
	if err := requireGH(); err != nil {
		return nil, err
	}
	out, err := exec.Command("gh", "pr", "view", strconv.Itoa(num), "--json", prJSONFields).Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view %d: %w", num, err)
	}
	return parsePRViewJSON(out)
}

// PRSummary is the lightweight shape returned for the picker's "Other PRs"
// section. Distinct from PRInfo so we don't pay the cost of fetching body etc.
type PRSummary struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
	BaseRefName string `json:"baseRefName"`
	IsDraft     bool   `json:"isDraft"`
}

// fetchOpenPRs lists open PRs visible to the current gh auth. Capped at 100
// (gh's max page size).
func fetchOpenPRs() ([]PRSummary, error) {
	return fetchOpenPRsCtx(context.Background())
}

// fetchOpenPRsCtx is the context-aware variant — the warm-prime path
// passes the daemon's shutdown ctx so a Ctrl+C during boot terminates
// the in-flight gh subprocess instead of orphaning it.
func fetchOpenPRsCtx(ctx context.Context) ([]PRSummary, error) {
	if err := requireGH(); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--limit", "100",
		"--json", "number,title,url,headRefName,headRefOid,baseRefName,isDraft",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	return parsePRListJSON(out)
}

// parsePRListJSON decodes `gh pr list --json` output. Factored for testing.
func parsePRListJSON(b []byte) ([]PRSummary, error) {
	var prs []PRSummary
	if err := json.Unmarshal(b, &prs); err != nil {
		return nil, fmt.Errorf("parsing gh pr list: %w", err)
	}
	return prs, nil
}

// prListCache caches `gh pr list` results for 60 seconds. The picker may be
// opened/closed multiple times; gh on big orgs can take 3-5s.
type prListCache struct {
	mu      sync.Mutex
	fetched time.Time
	data    []PRSummary
}

// get returns cached PR data, refreshing if older than 60s.
func (c *prListCache) get() ([]PRSummary, error) {
	return c.getCtx(context.Background())
}

// getCtx is the context-aware variant. The warm-prime path on daemon
// boot passes the daemon's shutdown context so the in-flight gh
// subprocess gets killed on Ctrl+C rather than orphaned.
func (c *prListCache) getCtx(ctx context.Context) ([]PRSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.fetched) < 60*time.Second && c.data != nil {
		return c.data, nil
	}
	data, err := fetchOpenPRsCtx(ctx)
	if err != nil {
		return nil, err
	}
	c.data = data
	c.fetched = time.Now()
	return data, nil
}

// IsStackedPR reports whether the PR's base branch is something other than
// the repo's default branch — the heuristic for "is this stacked?".
//
// Returns false when defaultBranch is empty (e.g. detached HEAD, missing
// remote): without a known default we can't classify, so degrade safely
// to "not stacked" rather than reporting every PR as stacked.
func IsStackedPR(info *PRInfo, vcs VCS) bool {
	if info == nil || vcs == nil {
		return false
	}
	def := vcs.DefaultBranch()
	if def == "" {
		return false
	}
	return info.BaseRefName != def
}

// ensureSHAFetched ensures sha is reachable in the local object store,
// attempting auto-fetch from origin (and forkURL when set) when missing.
// forkURL may be "" — same-repo PRs and CLI --range pass through with empty.
func ensureSHAFetched(vcs VCS, sha, repoRoot, forkURL string) error {
	if vcs == nil {
		return nil
	}
	if vcs.HasObject(sha, repoRoot) {
		return nil
	}
	if vcs.Name() == "sl" {
		return ensureSHAFetchedSapling(vcs, sha, repoRoot, forkURL)
	}
	if vcs.Name() != "git" {
		return fmt.Errorf("commit %s not present locally (auto-fetch not supported for vcs=%q)", sha, vcs.Name())
	}

	// First attempt: origin. Suffices for same-repo PRs.
	if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
		vcs.HasObject(sha, repoRoot) {
		return nil
	}

	// Second attempt: fork URL, if known.
	if forkURL != "" {
		if err := tryGitFetch(repoRoot, forkURL, sha); err == nil &&
			vcs.HasObject(sha, repoRoot) {
			return nil
		}
		return fmt.Errorf("commit %s not present locally; tried origin and fork %s — manual fetch required", sha, forkURL)
	}
	return fmt.Errorf("commit %s not present locally; manual fetch required (run `git fetch <remote> %s`)", sha, sha)
}

// ensureSHAFetchedSapling tries `sl pull -r <sha>` first, then falls back to
// `git fetch origin <sha>` when the repo has both .sl and .git
// (sapling-on-git). Sapling-on-git stores objects in the underlying git repo,
// so a git fetch populates them and `sl` will see them on the next HasObject.
//
// forkURL is the cross-repo HEAD repository URL when the PR comes from a
// fork. For sapling-on-git we attempt `git fetch <forkURL> <sha>` as a third
// step. Pure sapling has no clean cross-repo fetch primitive that takes an
// arbitrary URL on the command line — `sl pull <url>` only works when the
// remote is configured — so we surface a clear error telling the user to
// configure the fork as a path/remote and re-run.
func ensureSHAFetchedSapling(vcs VCS, sha, repoRoot, forkURL string) error {
	if err := trySLPull(repoRoot, sha); err == nil &&
		vcs.HasObject(sha, repoRoot) {
		return nil
	}
	if hasGitDirAt(repoRoot) {
		if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
			vcs.HasObject(sha, repoRoot) {
			return nil
		}
		if forkURL != "" {
			if err := tryGitFetch(repoRoot, forkURL, sha); err == nil &&
				vcs.HasObject(sha, repoRoot) {
				return nil
			}
			return fmt.Errorf("commit %s not present locally; tried `sl pull -r %s`, `git fetch origin %s`, and `git fetch %s %s` — manual fetch required", sha, sha, sha, forkURL, sha)
		}
	}
	if forkURL != "" {
		// Pure sapling, fork PR. We can't drive a cross-repo fetch by URL,
		// so explain what the user needs to do.
		return fmt.Errorf("commit %s not present locally; PR head is on fork %s. Pure sapling can't fetch by URL — run `sl pull %s` (configure the fork as a path first if needed) and re-run", sha, forkURL, forkURL)
	}
	return fmt.Errorf("commit %s not present locally; tried `sl pull -r %s` and `git fetch origin %s` — run `sl pull` manually with the right source", sha, sha, sha)
}

// tryGitFetch shells `git fetch <remote> <sha>` with a 30s timeout. Local git
// ops are normally context-free in this codebase; `git fetch` is the network
// path and warrants a timeout.
func tryGitFetch(repoRoot, remote, sha string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, sha)
	cmd.Dir = repoRoot
	return cmd.Run()
}

// trySLPull shells `sl pull -r <sha>` with a 30s timeout.
func trySLPull(repoRoot, sha string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sl", "pull", "-r", sha)
	cmd.Dir = repoRoot
	return cmd.Run()
}

// hasGitDirAt reports whether repoRoot has a .git/ directory. Cheap stat.
func hasGitDirAt(repoRoot string) bool {
	info, err := os.Stat(filepath.Join(repoRoot, ".git"))
	return err == nil && info.IsDir()
}

// fetchPRComments fetches all review comments for a PR.
func fetchPRComments(prNumber int) ([]ghComment, error) {
	if ghSupportsAPISlurp() {
		return fetchPRCommentsWithSlurp(prNumber)
	}
	// TODO: Remove this compatibility path once Crit requires gh v2.48.0+,
	// which added `gh api --slurp`.
	return fetchPRCommentsWithoutSlurp(prNumber)
}

func ghSupportsAPISlurp() bool {
	out, err := exec.Command("gh", "version").Output()
	if err != nil {
		return false
	}
	return ghVersionSupportsSlurp(string(out))
}

func ghVersionSupportsSlurp(versionOutput string) bool {
	fields := strings.Fields(versionOutput)
	if len(fields) < 3 || fields[0] != "gh" || fields[1] != "version" {
		return false
	}
	return versionAtLeast(fields[2], 2, 48, 0)
}

func versionAtLeast(version string, wantMajor, wantMinor, wantPatch int) bool {
	core := strings.SplitN(strings.TrimPrefix(version, "v"), "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) < 3 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return false
	}
	if major != wantMajor {
		return major > wantMajor
	}
	if minor != wantMinor {
		return minor > wantMinor
	}
	return patch >= wantPatch
}

func fetchPRCommentsWithSlurp(prNumber int) ([]ghComment, error) {
	out, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber),
		"--paginate",
		"--slurp",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("fetching PR comments: %w", err)
	}

	var pages [][]ghComment
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("parsing PR comments: %w", err)
	}

	var comments []ghComment
	for _, page := range pages {
		comments = append(comments, page...)
	}
	return comments, nil
}

func fetchPRCommentsWithoutSlurp(prNumber int) ([]ghComment, error) {
	out, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber),
		"--paginate",
		"--jq",
		".[]",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("fetching PR comments: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	var comments []ghComment
	for {
		var c ghComment
		if err := dec.Decode(&c); err != nil {
			if errors.Is(err, io.EOF) {
				return comments, nil
			}
			return nil, fmt.Errorf("parsing PR comments: %w", err)
		}
		comments = append(comments, c)
	}
}

// isDuplicateGHComment checks if a GitHub comment already exists in the comment list.
// If ghID is non-zero, matches by GitHubID. Otherwise falls back to author+lines+body.
func isDuplicateGHComment(comments []Comment, ghID int64, author string, startLine, endLine int, body string) bool {
	for _, c := range comments {
		if ghID != 0 && c.GitHubID == ghID {
			return true
		}
		if c.Author == author && c.StartLine == startLine && c.EndLine == endLine && c.Body == body {
			return true
		}
	}
	return false
}

// isDuplicateGHReply checks if a GitHub reply already exists in the reply list by GitHubID.
func isDuplicateGHReply(replies []Reply, ghID int64) bool {
	for _, r := range replies {
		if r.GitHubID == ghID {
			return true
		}
	}
	return false
}

// findCommentByGitHubID searches all files in a CritJSON for a comment with the given GitHubID.
// Returns the file path, comment index, and true if found.
func findCommentByGitHubID(cj *CritJSON, ghID int64) (string, int, bool) {
	for path, cf := range cj.Files {
		for i, c := range cf.Comments {
			if c.GitHubID == ghID {
				return path, i, true
			}
		}
	}
	return "", 0, false
}

// separateRootsAndReplies filters and categorizes ghComments into root comments
// and replies, grouped by parent ID.
func separateRootsAndReplies(ghComments []ghComment) ([]ghComment, map[int64][]ghComment) {
	var roots []ghComment
	replyMap := make(map[int64][]ghComment)
	for _, gc := range ghComments {
		if gc.Line == 0 || gc.Side == "LEFT" {
			continue
		}
		if gc.InReplyToID == 0 {
			roots = append(roots, gc)
		} else {
			replyMap[gc.InReplyToID] = append(replyMap[gc.InReplyToID], gc)
		}
	}
	for parentID := range replyMap {
		sort.Slice(replyMap[parentID], func(i, j int) bool {
			return replyMap[parentID][i].CreatedAt < replyMap[parentID][j].CreatedAt
		})
	}
	return roots, replyMap
}

// appendNewGHReplies adds non-duplicate replies to an existing comment, returning how many were added.
func appendNewGHReplies(comments []Comment, ci int, childReplies []ghComment, names userNameCache) int {
	added := 0
	for _, r := range childReplies {
		if isDuplicateGHReply(comments[ci].Replies, r.ID) {
			continue
		}
		comments[ci].Replies = append(comments[ci].Replies, Reply{
			ID:        randomReplyID(),
			Body:      r.Body,
			Author:    names.lookup(r.User.Login),
			CreatedAt: r.CreatedAt,
			GitHubID:  r.ID,
		})
		added++
	}
	return added
}

// mergeRootComment handles a single root ghComment: either deduplicates or creates it.
// scope stamps the imported comment's HeadSHA + DiffScope when called from a range-mode
// pull. Empty scope leaves the legacy working-tree fields unset.
func mergeRootComment(cj *CritJSON, gc ghComment, replyMap map[int64][]ghComment, now string, names userNameCache, scope inheritedScope) int {
	cf, ok := cj.Files[gc.Path]
	if !ok {
		cf = CritJSONFile{Status: "modified", Comments: []Comment{}}
	}

	startLine := gc.StartLine
	if startLine == 0 {
		startLine = gc.Line
	}

	authorName := names.lookup(gc.User.Login)

	if isDuplicateGHComment(cf.Comments, gc.ID, authorName, startLine, gc.Line, gc.Body) {
		added := 0
		if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
			for ci, c := range cf.Comments {
				if c.GitHubID == gc.ID {
					added = appendNewGHReplies(cf.Comments, ci, childReplies, names)
					break
				}
			}
			cj.Files[gc.Path] = cf
		}
		return added
	}

	commentID := randomCommentID()
	comment := stampWithFocus(Comment{
		ID: commentID, StartLine: startLine, EndLine: gc.Line,
		Body: gc.Body, Author: authorName, CreatedAt: gc.CreatedAt,
		UpdatedAt: now, GitHubID: gc.ID,
	}, scope.asFocus())

	added := 0
	if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
		for _, r := range childReplies {
			comment.Replies = append(comment.Replies, Reply{
				ID:        randomReplyID(),
				Body:      r.Body,
				Author:    names.lookup(r.User.Login),
				CreatedAt: r.CreatedAt,
				GitHubID:  r.ID,
			})
			added++
		}
	}

	cf.Comments = append(cf.Comments, comment)
	cj.Files[gc.Path] = cf
	return added + 1 // +1 for the root
}

// mergeOrphanReplies processes replies whose parent was already in cj from a previous pull.
func mergeOrphanReplies(cj *CritJSON, roots []ghComment, replyMap map[int64][]ghComment, names userNameCache) int {
	rootIDs := make(map[int64]struct{}, len(roots))
	for _, gc := range roots {
		rootIDs[gc.ID] = struct{}{}
	}

	added := 0
	for parentID, childReplies := range replyMap {
		if _, handled := rootIDs[parentID]; handled {
			continue
		}
		filePath, ci, found := findCommentByGitHubID(cj, parentID)
		if !found {
			continue
		}
		cf := cj.Files[filePath]
		added += appendNewGHReplies(cf.Comments, ci, childReplies, names)
		cj.Files[filePath] = cf
	}
	return added
}

// mergeGHComments appends GitHub PR comments into an existing CritJSON.
// Only includes RIGHT-side comments (comments on the new version of the file).
// Handles threading: root comments become top-level Comments, replies become Reply entries.
// Deduplicates by GitHubID (preferred) or author+lines+body to prevent duplicates from repeated pulls.
func mergeGHComments(cj *CritJSON, ghComments []ghComment) int {
	return mergeGHCommentsScoped(cj, ghComments, inheritedScope{})
}

// mergeGHCommentsScoped is mergeGHComments with optional HeadSHA + DiffScope
// stamping for range-mode pulls. scope.DiffScope == "" matches legacy
// working-tree behavior. See spec §E "Write path — `crit pull` import path".
func mergeGHCommentsScoped(cj *CritJSON, ghComments []ghComment, scope inheritedScope) int {
	return mergeGHCommentsWithNames(cj, ghComments, make(userNameCache), scope)
}

// mergeGHCommentsWithNames is the form of mergeGHComments that lets callers
// supply a pre-populated cache. Production uses mergeGHComments (fresh
// cache, lazy /users/{login} lookups). Tests can pre-populate to assert on
// resolved display names without going to the network. scope stamps
// HeadSHA + DiffScope on newly imported root comments (no-op when empty).
func mergeGHCommentsWithNames(cj *CritJSON, ghComments []ghComment, names userNameCache, scope inheritedScope) int {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	roots, replyMap := separateRootsAndReplies(ghComments)

	added := 0
	for _, gc := range roots {
		added += mergeRootComment(cj, gc, replyMap, now, names, scope)
	}
	added += mergeOrphanReplies(cj, roots, replyMap, names)

	return added
}

// resolveReviewPath returns the full path to the review file for the current context.
// Resolution order:
//  1. If outputDir is set, return outputDir/.crit.json (explicit override)
//  2. Check daemon registry for running sessions matching this cwd
//  3. If one daemon matches, use its ReviewPath
//  4. If multiple daemons match, use the one matching current branch
//  5. If no daemon found, compute the centralized path: ~/.crit/reviews/<key>.json
func resolveReviewPath(outputDir string) (string, error) {
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit.json"), nil
	}

	cwd, err := resolvedCWD()
	if err != nil {
		return "", err
	}

	if path := resolveReviewPathFromDaemon(cwd); path != "" {
		return path, nil
	}

	// No daemon — compute centralized path.
	branch := ""
	if vcs := DetectVCS(""); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	key := sessionKey(cwd, branch, nil)
	path, err := reviewFilePath(key)
	if err != nil {
		return "", err
	}

	return path, nil
}

// resolveReviewPathFromDaemon checks the daemon registry for a running session
// and returns its review path. Tries exact CWD match first, then falls back to
// matching by git repo root (handles subdirectory mismatch — e.g. daemon started
// from repo/api but crit comment run from repo/).
func resolveReviewPathFromDaemon(cwd string) string {
	sessions, _ := listSessionsForCWD(cwd)
	if path := pickReviewPath(sessions); path != "" {
		return path
	}

	// Fallback: match by VCS repo root.
	if len(sessions) == 0 {
		vcs := DetectVCS("")
		if vcs == nil {
			return ""
		}
		if repoRoot, err := vcs.RepoRoot(); err == nil && repoRoot != cwd {
			repoSessions, _ := listSessionsForRepoRoot(repoRoot)
			if path := pickReviewPath(repoSessions); path != "" {
				return path
			}
		}
	}
	return ""
}

// pickReviewPath selects a review path from a list of sessions.
// Returns the path if exactly one session has one, or defers to branch matching for multiple.
func pickReviewPath(sessions []sessionEntry) string {
	if len(sessions) == 1 && sessions[0].ReviewPath != "" {
		return sessions[0].ReviewPath
	}
	if len(sessions) > 1 {
		return resolveReviewPathFromSessions(sessions)
	}
	return ""
}

// resolveReviewPathFromSessions picks the best ReviewPath from multiple daemon sessions.
// Tries current branch first, then falls back to the first session with a ReviewPath.
func resolveReviewPathFromSessions(sessions []sessionEntry) string {
	branch := CurrentBranch()
	for _, s := range sessions {
		if s.Branch == branch && s.ReviewPath != "" {
			return s.ReviewPath
		}
	}
	for _, s := range sessions {
		if s.ReviewPath != "" {
			return s.ReviewPath
		}
	}
	return ""
}

// writeCritJSON resolves the review path and writes a CritJSON via saveCritJSON.
func writeCritJSON(cj CritJSON, outputDir string) error {
	path, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	return saveCritJSON(path, cj)
}

// ghReplyForPush represents a reply that needs to be posted to GitHub.
type ghReplyForPush struct {
	ParentGHID int64
	Body       string
}

// collectNewRepliesForPush finds replies that haven't been pushed to GitHub yet.
// A reply needs pushing if its GitHubID is 0 (local-only) and its parent Comment has a GitHubID (on GitHub).
func collectNewRepliesForPush(cf CritJSONFile) []ghReplyForPush {
	var replies []ghReplyForPush
	for _, c := range cf.Comments {
		if c.GitHubID == 0 {
			continue // root not on GitHub, can't reply to it
		}
		for _, r := range c.Replies {
			if r.GitHubID == 0 {
				replies = append(replies, ghReplyForPush{
					ParentGHID: c.GitHubID,
					Body:       r.Body,
				})
			}
		}
	}
	return replies
}

// postGHReply posts a reply to an existing GitHub PR review comment.
// Returns the GitHub ID of the newly created reply.
func postGHReply(prNumber int, parentGHID int64, body string) (int64, error) {
	payload, err := json.Marshal(map[string]any{
		"body":        body,
		"in_reply_to": parentGHID,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal reply: %w", err)
	}
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber),
		"--method", "POST",
		"--input", "-",
	)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("gh api: %s: %w", string(output), err)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(output, &resp); err != nil {
		return 0, fmt.Errorf("parsing reply response: %w", err)
	}
	return resp.ID, nil
}

// critJSONToGHComments converts review file comments to GitHub review comment format.
// Returns the list of comments suitable for the GitHub "create review" API.
func critJSONToGHComments(cj CritJSON) []map[string]any {
	var result []map[string]any
	for path, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.Resolved {
				continue // don't post resolved comments
			}
			if c.GitHubID != 0 {
				continue // already pushed
			}
			comment := map[string]any{
				"path": path,
				"line": c.EndLine,
				"side": "RIGHT",
				"body": c.Body,
			}
			if c.StartLine != c.EndLine {
				comment["start_line"] = c.StartLine
				comment["start_side"] = "RIGHT"
			}
			result = append(result, comment)
		}
	}
	return result
}

// parsePushEvent maps a user-facing event flag value to the GitHub API event string.
// Valid values: "" or "comment" -> "COMMENT", "approve" -> "APPROVE", "request-changes" -> "REQUEST_CHANGES".
func parsePushEvent(flag string) (string, error) {
	switch flag {
	case "", "comment":
		return "COMMENT", nil
	case "approve":
		return "APPROVE", nil
	case "request-changes":
		return "REQUEST_CHANGES", nil
	default:
		return "", fmt.Errorf("invalid --event value %q (valid: comment, approve, request-changes)", flag)
	}
}

// buildReviewPayload constructs the JSON body for a GitHub PR review request.
func buildReviewPayload(comments []map[string]any, message string, event string) ([]byte, error) {
	if comments == nil {
		comments = []map[string]any{}
	}
	review := map[string]any{
		"event":    event,
		"body":     message,
		"comments": comments,
	}
	return json.Marshal(review)
}

// createGHReview posts a review with inline comments to a GitHub PR.
// message is the top-level review body (empty string posts no top-level comment).
// Returns a map of "path:endLine" -> GitHubID for each created comment.
func createGHReview(prNumber int, comments []map[string]any, message string, event string) (map[string]int64, error) {
	data, err := buildReviewPayload(comments, message, event)
	if err != nil {
		return nil, fmt.Errorf("marshaling review: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNumber),
		"--method", "POST",
		"--input", "-",
	)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("creating review: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("creating review: %w", err)
	}

	// Parse review ID from response, then fetch its comments in a second call.
	// The create-review response does not include comment objects — only the review itself.
	var reviewResp struct {
		ID int64 `json:"id"`
	}
	idMap := make(map[string]int64)
	if err := json.Unmarshal(stdout.Bytes(), &reviewResp); err != nil || reviewResp.ID == 0 {
		return idMap, nil //nolint:nilerr // non-fatal: review was created, just can't map IDs
	}

	// Fetch this review's comments and zip with our input to map IDs by position.
	// We use the review-scoped endpoint (only returns this review's comments, in order).
	commentOut, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews/%d/comments", prNumber, reviewResp.ID),
	).Output()
	if err != nil {
		return idMap, nil //nolint:nilerr // non-fatal: review was created, comment ID mapping is best-effort
	}
	var reviewComments []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(commentOut, &reviewComments); err == nil {
		for i, rc := range reviewComments {
			if i < len(comments) {
				path, _ := comments[i]["path"].(string)
				line, _ := comments[i]["line"].(int)
				key := fmt.Sprintf("%s:%d", path, line)
				idMap[key] = rc.ID
			}
		}
	}
	return idMap, nil
}

// replyKey uniquely identifies a reply for GitHubID mapping after push.
type replyKey struct {
	ParentGHID int64
	BodyPrefix string
}

// updateCritJSONWithGitHubIDs writes GitHub IDs back to the review file after a push.
// commentIDs maps "path:endLine" -> GitHubID for root comments.
// replyIDs maps replyKey -> GitHubID for replies.
func updateCritJSONWithGitHubIDs(critPath string, commentIDs map[string]int64, replyIDs map[replyKey]int64) error {
	data, err := os.ReadFile(critPath)
	if err != nil {
		return err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return err
	}

	for path, cf := range cj.Files {
		for i, c := range cf.Comments {
			if c.GitHubID == 0 {
				key := fmt.Sprintf("%s:%d", path, c.EndLine)
				if id, ok := commentIDs[key]; ok {
					cf.Comments[i].GitHubID = id
				}
			}
			for j, r := range c.Replies {
				if r.GitHubID == 0 && cf.Comments[i].GitHubID != 0 {
					rk := replyKey{ParentGHID: cf.Comments[i].GitHubID, BodyPrefix: truncateStr(r.Body, 60)}
					if id, ok := replyIDs[rk]; ok {
						cf.Comments[i].Replies[j].GitHubID = id
					}
				}
			}
		}
		cj.Files[path] = cf
	}

	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(critPath, append(out, '\n'), 0644)
}

// truncateStr returns the first n runes of s, or all of s if shorter.
func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// loadCritJSON reads the review file from disk, or returns a fresh CritJSON if the file doesn't exist.
func loadCritJSON(critPath string) (CritJSON, error) {
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return cj, fmt.Errorf("invalid existing review file: %w", err)
		}
	} else if os.IsNotExist(err) {
		branch := CurrentBranch()
		cfg := LoadConfig(filepath.Dir(critPath))
		base := cfg.BaseBranch
		if base == "" {
			base = defaultBaseRef()
		}
		baseRef, _ := MergeBase(base)
		cj = CritJSON{
			Branch:      branch,
			BaseRef:     baseRef,
			ReviewRound: 1,
			Files:       make(map[string]CritJSONFile),
		}
	} else {
		return cj, fmt.Errorf("reading review file: %w", err)
	}
	return cj, nil
}

// saveCritJSON writes the CritJSON struct to disk with pretty-printed JSON
// and a trailing newline. Uses atomic writes to prevent corruption.
func saveCritJSON(critPath string, cj CritJSON) error {
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling review file: %w", err)
	}
	// Ensure parent directory exists (centralized path may not exist yet).
	if err := os.MkdirAll(filepath.Dir(critPath), 0700); err != nil {
		return fmt.Errorf("creating review directory: %w", err)
	}
	return atomicWriteFile(critPath, append(data, '\n'), 0644)
}

// appendComment adds a comment to the CritJSON struct in memory. Does not write to disk.
// Defers to appendCommentScoped with no scope inheritance.
func appendComment(cj *CritJSON, filePath string, startLine, endLine int, body, author, userID string) {
	appendCommentScoped(cj, filePath, startLine, endLine, body, author, userID, inheritedScope{})
}

// appendCommentScoped is appendComment with HeadSHA / DiffScope stamping.
// scope.DiffScope == "" produces today's working-tree behavior.
func appendCommentScoped(cj *CritJSON, filePath string, startLine, endLine int, body, author, userID string, scope inheritedScope) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	cf, ok := cj.Files[filePath]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	// Populate anchor from the file on disk.
	anchor := readAnchorFromDisk(filePath, startLine, endLine)

	c := stampWithFocus(Comment{
		ID:        randomCommentID(),
		StartLine: startLine,
		EndLine:   endLine,
		Body:      body,
		Anchor:    anchor,
		Author:    author,
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
	}, scope.asFocus())
	cf.Comments = append(cf.Comments, c)
	cj.Files[filePath] = cf
}

// readAnchorFromDisk reads the file from disk and extracts lines startLine..endLine
// as the anchor text. Returns empty string on any error or if the file is not found.
func readAnchorFromDisk(filePath string, startLine, endLine int) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return extractAnchor(string(data), startLine, endLine)
}

// appendReply adds a reply to an existing comment in the CritJSON struct in memory.
// Returns an error if the comment ID is not found or is ambiguous across files.
// Searches both file comments and review_comments.
func appendReply(cj *CritJSON, commentID, body, author, userID string, resolve bool, filterPath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	// Check all review comments (not just those starting with "r" — web-fetched ones use "web-N").
	for i, c := range cj.ReviewComments {
		if c.ID == commentID {
			reply := Reply{
				ID:        randomReplyID(),
				Body:      body,
				Author:    author,
				UserID:    userID,
				CreatedAt: now,
			}
			cj.ReviewComments[i].Replies = append(cj.ReviewComments[i].Replies, reply)
			cj.ReviewComments[i].UpdatedAt = now
			if resolve {
				cj.ReviewComments[i].Resolved = true
			}
			return nil
		}
	}

	// Search file comments
	var found bool
	var foundPaths []string
	for filePath, cf := range cj.Files {
		if filterPath != "" && filePath != filterPath {
			continue
		}
		for i, c := range cf.Comments {
			if c.ID == commentID {
				foundPaths = append(foundPaths, filePath)
				if !found {
					found = true
					reply := Reply{
						ID:        randomReplyID(),
						Body:      body,
						Author:    author,
						UserID:    userID,
						CreatedAt: now,
					}
					cf.Comments[i].Replies = append(cf.Comments[i].Replies, reply)
					cf.Comments[i].UpdatedAt = now
					if resolve {
						cf.Comments[i].Resolved = true
					}
					cj.Files[filePath] = cf
				}
			}
		}
	}

	if len(foundPaths) > 1 {
		return fmt.Errorf("comment %q found in multiple files (%s); use --path <file> to disambiguate",
			commentID, strings.Join(foundPaths, ", "))
	}
	if !found {
		if filterPath != "" {
			return fmt.Errorf("comment %q not found in file %q in review file", commentID, filterPath)
		}
		return fmt.Errorf("comment %q not found in review file", commentID)
	}
	return nil
}

// addCommentToCritJSON appends a comment to the review file for the given file and line range.
// Creates the review file if it doesn't exist. Appends to existing comments if it does.
// Works in both git repos and plain directories (file mode).
// outputDir overrides the default location (repo root or CWD) when non-empty.
// userID is documented to support callers that pass an authenticated user; tests
// pass "" because they don't need that path. The lint exception keeps the signature
// stable for the API.
//
//nolint:unparam // userID is part of the public contract; tests don't exercise it
func addCommentToCritJSON(filePath string, startLine, endLine int, body string, author, userID string, outputDir string) error {
	return addCommentToCritJSONScoped(filePath, startLine, endLine, body, author, userID, outputDir, inheritedScope{})
}

// addCommentToCritJSONScoped is addCommentToCritJSON with scope stamping.
func addCommentToCritJSONScoped(filePath string, startLine, endLine int, body, author, userID, outputDir string, scope inheritedScope) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendCommentScoped(&cj, cleaned, startLine, endLine, body, author, userID, scope)
	return saveCritJSON(critPath, cj)
}

// addReplyToCritJSON adds a reply to an existing comment in the review file.
// It searches all files for the comment ID. If resolve is true, it also marks the comment as resolved.
func addReplyToCritJSON(commentID, body, author, userID string, resolve bool, outputDir string, filterPath string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	if err := appendReply(&cj, commentID, body, author, userID, resolve, filterPath); err != nil {
		// Only fall back to scanning when the comment genuinely wasn't found.
		// Don't fall back for ambiguity errors ("found in multiple files").
		if strings.Contains(err.Error(), "not found") {
			if altPath, err2 := findReviewFileByCommentID(commentID, critPath); err2 == nil {
				altCJ, loadErr := loadCritJSON(altPath)
				if loadErr != nil {
					return err // return original error
				}
				if replyErr := appendReply(&altCJ, commentID, body, author, userID, resolve, filterPath); replyErr != nil {
					return err // return original error
				}
				fmt.Fprintf(os.Stderr, "Note: comment %s found in %s (not the resolved review file)\n", commentID, filepath.Base(altPath))
				return saveCritJSON(altPath, altCJ)
			}
		}
		return err
	}
	return saveCritJSON(critPath, cj)
}

// findReviewFileByCommentID scans all review files in ~/.crit/reviews/ for the given
// comment ID, skipping excludePath. Returns the path if found in exactly one file,
// or an error wrapping commentID if it's missing or appears in multiple files.
func findReviewFileByCommentID(commentID string, excludePath string) (string, error) {
	dir, err := reviewsDir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("comment %q not found in any review file", commentID)
		}
		return "", err
	}

	var matchPath string
	for _, de := range entries {
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		if path == excludePath {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		if reviewFileContainsComment(data, commentID) {
			if matchPath != "" {
				return "", fmt.Errorf("comment %q found in multiple review files", commentID)
			}
			matchPath = path
		}
	}
	if matchPath == "" {
		return "", fmt.Errorf("comment %q not found in any review file", commentID)
	}
	return matchPath, nil
}

// reviewFileContainsComment does a quick check if a review JSON file contains
// a comment with the given ID. Uses string search first as a fast path to
// avoid parsing files that definitely don't contain the ID.
func reviewFileContainsComment(data []byte, commentID string) bool {
	// Fast path: if the ID string doesn't appear at all, skip JSON parsing.
	if !bytes.Contains(data, []byte(commentID)) {
		return false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return false
	}
	for _, c := range cj.ReviewComments {
		if c.ID == commentID {
			return true
		}
	}
	for _, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.ID == commentID {
				return true
			}
		}
	}
	return false
}

// clearCritJSON removes the review file from the resolved path or outputDir.
func clearCritJSON(outputDir string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}
	if err := os.Remove(critPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// BulkCommentEntry represents one entry in a bulk comment JSON array.
// Supports review-level, file-level, line-level comments, and replies.
type BulkCommentEntry struct {
	// New comment fields
	File     string `json:"file,omitempty"`
	Path     string `json:"path,omitempty"`     // alias for File
	Line     int    `json:"-"`                  // parsed from "line" (int or string like "45-47")
	LineSpec string `json:"-"`                  // string line spec like "45-47" (from "line" field)
	EndLine  int    `json:"end_line,omitempty"` // defaults to Line if omitted
	Body     string `json:"body"`
	Author   string `json:"author,omitempty"` // overrides per-entry; falls back to global
	Scope    string `json:"scope,omitempty"`  // "review", "file", or "" (inferred)

	// Reply fields
	ReplyTo string `json:"reply_to,omitempty"`
	Resolve bool   `json:"resolve,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for BulkCommentEntry
// to handle the "line" field being either an int (42) or a string ("45-47").
func (e *BulkCommentEntry) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias BulkCommentEntry
	aux := &struct {
		Line json.RawMessage `json:"line,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.Line) > 0 {
		// Try int first
		var lineInt int
		if err := json.Unmarshal(aux.Line, &lineInt); err == nil {
			e.Line = lineInt
			return nil
		}
		// Try string
		var lineStr string
		if err := json.Unmarshal(aux.Line, &lineStr); err == nil {
			e.LineSpec = lineStr
			return nil
		}
	}
	return nil
}

// processBulkEntry routes a single bulk comment entry to the appropriate
// authoring helper. globalAuthor is used when an entry doesn't specify its own
// author. scope is stamped on every authored comment (empty = today's behavior).
func processBulkEntry(cj *CritJSON, i int, e BulkCommentEntry, globalAuthor, globalUserID string, scope inheritedScope) error {
	if e.Body == "" {
		return fmt.Errorf("entry %d: body is required", i)
	}

	author := e.Author
	if author == "" {
		author = globalAuthor
	}
	// UserID always comes from the local config — entry-level override would
	// be a spoof vector. The CLI never trusts a payload-supplied user_id.
	userID := globalUserID

	if e.ReplyTo != "" {
		if err := appendReply(cj, e.ReplyTo, e.Body, author, userID, e.Resolve, e.File); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		return nil
	}

	if e.Scope == "review" || (e.File == "" && e.Path == "" && e.Line <= 0 && e.LineSpec == "") {
		return processBulkReviewEntry(cj, i, e, author, userID, scope)
	}

	return processBulkFileOrLineEntry(cj, i, e, author, userID, scope)
}

func processBulkReviewEntry(cj *CritJSON, i int, e BulkCommentEntry, author, userID string, scope inheritedScope) error {
	if e.Line > 0 || e.LineSpec != "" {
		return fmt.Errorf("entry %d: file is required for new comments", i)
	}
	if e.Scope != "review" && (e.File != "" || e.Path != "") {
		return fmt.Errorf("entry %d: file is required for new comments", i)
	}
	appendReviewCommentScoped(cj, e.Body, author, userID, scope)
	return nil
}

func processBulkFileOrLineEntry(cj *CritJSON, i int, e BulkCommentEntry, author, userID string, scope inheritedScope) error {
	filePath := e.File
	if filePath == "" {
		filePath = e.Path
	}
	if filePath == "" {
		return fmt.Errorf("entry %d: file is required for new comments", i)
	}

	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("entry %d: path %q must be relative and within the repository", i, filePath)
	}

	if e.Scope == "file" {
		appendFileCommentScoped(cj, cleaned, e.Body, author, userID, scope)
		return nil
	}

	if e.Line <= 0 && e.LineSpec == "" {
		if e.Path != "" && e.File == "" {
			appendFileCommentScoped(cj, cleaned, e.Body, author, userID, scope)
			return nil
		}
		return fmt.Errorf("entry %d: line must be > 0", i)
	}

	return processBulkLineComment(cj, i, e, cleaned, author, userID, scope)
}

func processBulkLineComment(cj *CritJSON, i int, e BulkCommentEntry, cleaned, author, userID string, scope inheritedScope) error {
	startLine := e.Line
	endLine := e.EndLine

	if e.LineSpec != "" && startLine == 0 {
		var err error
		startLine, endLine, err = parseLineSpec(e.LineSpec)
		if err != nil {
			return fmt.Errorf("entry %d: invalid line spec %q", i, e.LineSpec)
		}
	}

	if startLine <= 0 {
		return fmt.Errorf("entry %d: line must be > 0", i)
	}
	if endLine == 0 {
		endLine = startLine
	}

	appendCommentScoped(cj, cleaned, startLine, endLine, e.Body, author, userID, scope)
	return nil
}

func parseLineSpec(spec string) (start, end int, err error) {
	if dashIdx := strings.Index(spec, "-"); dashIdx >= 0 {
		s, err1 := strconv.Atoi(spec[:dashIdx])
		e, err2 := strconv.Atoi(spec[dashIdx+1:])
		if err1 != nil || err2 != nil {
			if err1 != nil {
				return 0, 0, err1
			}
			return 0, 0, err2
		}
		return s, e, nil
	}
	n, err := strconv.Atoi(spec)
	if err != nil {
		return 0, 0, err
	}
	return n, n, nil
}

//nolint:unparam // globalUserID is part of the public contract; tests don't exercise it
func bulkAddCommentsToCritJSON(entries []BulkCommentEntry, globalAuthor, globalUserID string, outputDir string) error {
	return bulkAddCommentsToCritJSONScoped(entries, globalAuthor, globalUserID, outputDir, inheritedScope{})
}

// bulkAddCommentsToCritJSONScoped is bulkAddCommentsToCritJSON with scope stamping
// applied to every entry.
//
// Target resolution: a bulk call always writes to a single review file. If any
// entry uses reply_to and none of the referenced IDs live in the cwd-resolved
// primary file, the entire bulk is redirected to the alt file that contains
// them. New comments in the same bulk ride along. If reply IDs split across
// multiple review files (or some land in primary and others elsewhere), the
// call is rejected — callers should split into per-file bulks.
func bulkAddCommentsToCritJSONScoped(entries []BulkCommentEntry, globalAuthor, globalUserID string, outputDir string, scope inheritedScope) error {
	if len(entries) == 0 {
		return fmt.Errorf("no comment entries provided")
	}

	primaryPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	primary, err := loadCritJSON(primaryPath)
	if err != nil {
		return err
	}

	targetPath, targetCJ, redirected, err := resolveBulkTarget(entries, primaryPath, primary)
	if err != nil {
		return err
	}

	for i, e := range entries {
		if err := processBulkEntry(&targetCJ, i, e, globalAuthor, globalUserID, scope); err != nil {
			return err
		}
	}

	if err := saveCritJSON(targetPath, targetCJ); err != nil {
		return err
	}
	if redirected {
		fmt.Fprintf(os.Stderr, "Note: replies routed to %s (not the cwd-resolved review file)\n", filepath.Base(targetPath))
	}
	return nil
}

// resolveBulkTarget picks the single review file that this bulk call should
// write to. Returns the path, the loaded CritJSON to mutate, and whether the
// target differs from the cwd-resolved primary.
func resolveBulkTarget(entries []BulkCommentEntry, primaryPath string, primary CritJSON) (string, CritJSON, bool, error) {
	var replyIDs []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.ReplyTo == "" || seen[e.ReplyTo] {
			continue
		}
		seen[e.ReplyTo] = true
		replyIDs = append(replyIDs, e.ReplyTo)
	}

	if len(replyIDs) == 0 {
		return primaryPath, primary, false, nil
	}

	var inPrimary, missing []string
	for _, id := range replyIDs {
		if cjContainsCommentID(&primary, id) {
			inPrimary = append(inPrimary, id)
		} else {
			missing = append(missing, id)
		}
	}

	if len(missing) == 0 {
		return primaryPath, primary, false, nil
	}
	if len(inPrimary) > 0 {
		return "", CritJSON{}, false, fmt.Errorf(
			"bulk targets multiple review files: %v exist in %s, but %v do not — split into per-file bulks",
			inPrimary, filepath.Base(primaryPath), missing,
		)
	}

	// None in primary — every reply ID must live in the same alt file.
	var altPath string
	for _, id := range missing {
		path, err := findReviewFileByCommentID(id, primaryPath)
		if err != nil {
			return "", CritJSON{}, false, fmt.Errorf("reply target %s: %w", id, err)
		}
		if altPath == "" {
			altPath = path
			continue
		}
		if path != altPath {
			return "", CritJSON{}, false, fmt.Errorf(
				"bulk targets multiple review files: %s in %s, %s in %s — split into per-file bulks",
				missing[0], filepath.Base(altPath), id, filepath.Base(path),
			)
		}
	}

	altCJ, err := loadCritJSON(altPath)
	if err != nil {
		return "", CritJSON{}, false, fmt.Errorf("load %s: %w", filepath.Base(altPath), err)
	}
	return altPath, altCJ, true, nil
}

// cjContainsCommentID reports whether the given comment ID exists in the
// in-memory CritJSON, across review-level and per-file comments.
func cjContainsCommentID(cj *CritJSON, id string) bool {
	for _, c := range cj.ReviewComments {
		if c.ID == id {
			return true
		}
	}
	for _, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}

// addReviewCommentToCritJSON adds a review-level comment to the review file.
func addReviewCommentToCritJSON(body, author, userID, outputDir string) error {
	return addReviewCommentToCritJSONScoped(body, author, userID, outputDir, inheritedScope{})
}

// addReviewCommentToCritJSONScoped is addReviewCommentToCritJSON with scope stamping.
func addReviewCommentToCritJSONScoped(body, author, userID, outputDir string, scope inheritedScope) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendReviewCommentScoped(&cj, body, author, userID, scope)
	return saveCritJSON(critPath, cj)
}

// addFileCommentToCritJSON adds a file-level comment to the review file.
func addFileCommentToCritJSON(filePath, body, author, userID, outputDir string) error {
	return addFileCommentToCritJSONScoped(filePath, body, author, userID, outputDir, inheritedScope{})
}

// addFileCommentToCritJSONScoped is addFileCommentToCritJSON with scope stamping.
func addFileCommentToCritJSONScoped(filePath, body, author, userID, outputDir string, scope inheritedScope) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendFileCommentScoped(&cj, cleaned, body, author, userID, scope)
	return saveCritJSON(critPath, cj)
}

// appendReviewComment adds a review-level comment to the CritJSON struct in memory.
// Defers to appendReviewCommentScoped with no scope inheritance.
func appendReviewComment(cj *CritJSON, body, author, userID string) {
	appendReviewCommentScoped(cj, body, author, userID, inheritedScope{})
}

// appendReviewCommentScoped stamps DiffScope/HeadSHA from scope.
func appendReviewCommentScoped(cj *CritJSON, body, author, userID string, scope inheritedScope) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	c := stampWithFocus(Comment{
		ID:        randomReviewCommentID(),
		Body:      body,
		Author:    author,
		UserID:    userID,
		Scope:     "review",
		CreatedAt: now,
		UpdatedAt: now,
	}, scope.asFocus())
	cj.ReviewComments = append(cj.ReviewComments, c)
}

// appendFileComment adds a file-level comment (scope: "file", lines: 0) to the CritJSON struct in memory.
// Defers to appendFileCommentScoped with no scope inheritance.
func appendFileComment(cj *CritJSON, filePath, body, author, userID string) {
	appendFileCommentScoped(cj, filePath, body, author, userID, inheritedScope{})
}

// appendFileCommentScoped stamps DiffScope/HeadSHA from scope.
func appendFileCommentScoped(cj *CritJSON, filePath, body, author, userID string, scope inheritedScope) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	cf, ok := cj.Files[filePath]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	c := stampWithFocus(Comment{
		ID:        randomCommentID(),
		Body:      body,
		Author:    author,
		UserID:    userID,
		Scope:     "file",
		CreatedAt: now,
		UpdatedAt: now,
	}, scope.asFocus())
	cf.Comments = append(cf.Comments, c)
	cj.Files[filePath] = cf
}
