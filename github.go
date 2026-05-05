package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// bodyHashAtPush returns a short stable digest of a comment body. We use
// the first 16 hex chars (64 bits) of SHA-256 — collision risk is
// negligible at single-PR scale (≤50 comments) and keeps review files
// small for downstream agent consumption.
func bodyHashAtPush(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:8]) // 8 bytes → 16 hex chars
}

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
// Replies whose GitHubID is in pendingDeletes are skipped — the user has
// already deleted them locally and the next push will issue DELETE; importing
// them now would resurrect the row under a fresh ID and mask the pending intent.
func appendNewGHReplies(comments []Comment, ci int, childReplies []ghComment, names userNameCache, pendingDeletes map[int64]bool) int {
	added := 0
	for _, r := range childReplies {
		if isDuplicateGHReply(comments[ci].Replies, r.ID) {
			continue
		}
		if pendingDeletes[r.ID] {
			continue
		}
		comments[ci].Replies = append(comments[ci].Replies, Reply{
			ID:                 randomReplyID(),
			Body:               r.Body,
			Author:             names.lookup(r.User.Login),
			CreatedAt:          r.CreatedAt,
			GitHubID:           r.ID,
			LastPushedBodyHash: bodyHashAtPush(r.Body),
		})
		added++
	}
	return added
}

// mergeRootComment handles a single root ghComment: either deduplicates or creates it.
// scope stamps the imported comment's HeadSHA + DiffScope when called from a range-mode
// pull. Empty scope leaves the legacy working-tree fields unset.
func mergeRootComment(cj *CritJSON, gc ghComment, replyMap map[int64][]ghComment, now string, names userNameCache, scope inheritedScope, pendingDeletes map[int64]bool) int {
	cf, ok := cj.Files[gc.Path]
	if !ok {
		cf = CritJSONFile{Status: "modified", Comments: []Comment{}}
	}

	startLine := gc.StartLine
	if startLine == 0 {
		startLine = gc.Line
	}

	authorName := names.lookup(gc.User.Login)

	// Pending-delete-locally takes priority over import: the user has signaled
	// intent to DELETE this remote comment on the next push. Re-importing
	// would resurrect it under a fresh local ID and mask the pending intent.
	if pendingDeletes[gc.ID] {
		return 0
	}

	if isDuplicateGHComment(cf.Comments, gc.ID, authorName, startLine, gc.Line, gc.Body) {
		added := 0
		if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
			for ci, c := range cf.Comments {
				if c.GitHubID == gc.ID {
					added = appendNewGHReplies(cf.Comments, ci, childReplies, names, pendingDeletes)
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
		UpdatedAt: now, GitHubID: gc.ID, LastPushedBodyHash: bodyHashAtPush(gc.Body),
	}, scope.asFocus())

	added := 0
	if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
		for _, r := range childReplies {
			if pendingDeletes[r.ID] {
				continue
			}
			comment.Replies = append(comment.Replies, Reply{
				ID:                 randomReplyID(),
				Body:               r.Body,
				Author:             names.lookup(r.User.Login),
				CreatedAt:          r.CreatedAt,
				GitHubID:           r.ID,
				LastPushedBodyHash: bodyHashAtPush(r.Body),
			})
			added++
		}
	}

	cf.Comments = append(cf.Comments, comment)
	cj.Files[gc.Path] = cf
	return added + 1 // +1 for the root
}

// mergeOrphanReplies processes replies whose parent was already in cj from a previous pull.
func mergeOrphanReplies(cj *CritJSON, roots []ghComment, replyMap map[int64][]ghComment, names userNameCache, pendingDeletes map[int64]bool) int {
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
		added += appendNewGHReplies(cf.Comments, ci, childReplies, names, pendingDeletes)
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

	pendingDeletes := make(map[int64]bool, len(cj.PendingGitHubDeletes))
	for _, id := range cj.PendingGitHubDeletes {
		pendingDeletes[id] = true
	}

	added := 0
	for _, gc := range roots {
		added += mergeRootComment(cj, gc, replyMap, now, names, scope, pendingDeletes)
	}
	added += mergeOrphanReplies(cj, roots, replyMap, names, pendingDeletes)

	return added
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

// ghEditForPush represents one already-pushed comment or reply whose local
// body has diverged from its recorded push-time hash and therefore needs a
// PATCH.
//
// Path is empty for replies (replies are addressed by GitHubID alone in the
// GitHub API). For comments it carries the file path so the review file can
// be updated by location after a successful PATCH.
type ghEditForPush struct {
	GitHubID int64
	Path     string // file path for root comments; empty for replies
	Body     string
	IsReply  bool
}

// collectEditedForPush returns root comments and replies whose local Body
// differs from the digest recorded at the last push and therefore need a
// PATCH.
//
// A record with GitHubID != 0 and empty LastPushedBodyHash is enqueued: the
// remote ID exists but we never recorded what we pushed, so the local body
// is canonical and should be PATCHed up. After a successful PATCH the hash
// is stamped, future pushes are no-ops.
//
// Resolved comments are skipped (they're not pushable in the new-comment
// path either; consistent treatment for edits).
func collectEditedForPush(cj CritJSON) []ghEditForPush {
	var out []ghEditForPush
	paths := make([]string, 0, len(cj.Files))
	for p := range cj.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		cf := cj.Files[path]
		for _, c := range cf.Comments {
			if c.Resolved {
				continue
			}
			if c.GitHubID != 0 && c.LastPushedBodyHash != bodyHashAtPush(c.Body) {
				out = append(out, ghEditForPush{
					GitHubID: c.GitHubID,
					Path:     path,
					Body:     c.Body,
				})
			}
			for _, r := range c.Replies {
				if r.GitHubID != 0 && r.LastPushedBodyHash != bodyHashAtPush(r.Body) {
					out = append(out, ghEditForPush{
						GitHubID: r.GitHubID,
						Body:     r.Body,
						IsReply:  true,
					})
				}
			}
		}
	}
	return out
}

// patchGHComment edits the body of an existing PR review comment via the
// GitHub API. Works for both root comments and replies — they share the same
// /pulls/comments/{id} endpoint.
func patchGHComment(ghID int64, body string) error {
	payload, err := json.Marshal(map[string]any{"body": body})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/comments/%d", ghID),
		"--method", "PATCH",
		"--input", "-",
	)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh api patch: %s: %w", string(output), err)
	}
	return nil
}

// updateCritJSONWithEditedBodies stamps LastPushedBodyHash on records that
// were successfully PATCHed in this push. edited is the slice that was queued
// for PATCH; succeeded is the subset whose PATCH returned cleanly.
//
// Match key for both roots and replies is the GitHubID — it's stable and
// unique across the review file.
func updateCritJSONWithEditedBodies(critPath string, succeeded []ghEditForPush) error {
	if len(succeeded) == 0 {
		return nil
	}
	successByID := make(map[int64]string, len(succeeded))
	for _, e := range succeeded {
		successByID[e.GitHubID] = e.Body
	}

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
			if body, ok := successByID[c.GitHubID]; ok && c.GitHubID != 0 {
				cf.Comments[i].Body = body
				cf.Comments[i].LastPushedBodyHash = bodyHashAtPush(body)
			}
			for j, r := range c.Replies {
				if body, ok := successByID[r.GitHubID]; ok && r.GitHubID != 0 {
					cf.Comments[i].Replies[j].Body = body
					cf.Comments[i].Replies[j].LastPushedBodyHash = bodyHashAtPush(body)
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
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
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
					cf.Comments[i].LastPushedBodyHash = bodyHashAtPush(cf.Comments[i].Body)
				}
			}
			for j, r := range c.Replies {
				if r.GitHubID == 0 && cf.Comments[i].GitHubID != 0 {
					rk := replyKey{ParentGHID: cf.Comments[i].GitHubID, BodyPrefix: truncateStr(r.Body, 60)}
					if id, ok := replyIDs[rk]; ok {
						cf.Comments[i].Replies[j].GitHubID = id
						cf.Comments[i].Replies[j].LastPushedBodyHash = bodyHashAtPush(cf.Comments[i].Replies[j].Body)
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
	return atomicWriteFile(reviewPathsFor(critPath).Review, append(out, '\n'), 0644)
}

// collectDeletesForPush returns the snapshot of pending GitHub DELETE
// intents recorded against this review file. The returned slice is a copy
// so the caller can safely mutate or iterate while updating cj concurrently.
func collectDeletesForPush(cj CritJSON) []int64 {
	if len(cj.PendingGitHubDeletes) == 0 {
		return nil
	}
	out := make([]int64, len(cj.PendingGitHubDeletes))
	copy(out, cj.PendingGitHubDeletes)
	return out
}

// deleteGHComment issues DELETE on a GitHub PR review comment. The endpoint
// works for both root comments and replies — they share /pulls/comments/{id}.
//
// Returns the HTTP status code and an error. A 200 (or 204) is success;
// 404 is treated as success by the caller (the remote comment is already
// gone). 403 means the authenticated user is not the author and GitHub
// won't let us delete; the caller drops the tombstone anyway because
// retrying is futile.
func deleteGHComment(ghID int64) (int, error) {
	// `gh api --include` writes the response headers (incl. status line) to
	// stdout so we can extract the status. On non-2xx gh exits non-zero, so
	// we read CombinedOutput and parse regardless.
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/comments/%d", ghID),
		"--method", "DELETE",
		"--include",
	)
	output, runErr := cmd.CombinedOutput()
	status := parseGHIncludeStatus(output)
	if status >= 200 && status < 300 {
		return status, nil
	}
	if status == 404 || status == 403 {
		// Caller decides how to handle these; surface the status without
		// treating the gh non-zero exit as a fatal error.
		return status, nil
	}
	if runErr != nil {
		return status, fmt.Errorf("gh api delete: %s: %w", strings.TrimSpace(string(output)), runErr)
	}
	return status, fmt.Errorf("gh api delete: unexpected status %d: %s", status, strings.TrimSpace(string(output)))
}

// parseGHIncludeStatus extracts the HTTP status code from `gh api --include`
// output. The first line is "HTTP/2 204" or similar. Returns 0 if it can't
// be parsed.
func parseGHIncludeStatus(out []byte) int {
	line := string(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	for _, f := range fields {
		if n, err := strconv.Atoi(f); err == nil && n >= 100 && n < 600 {
			return n
		}
	}
	return 0
}

// updateCritJSONAfterDeletes removes drained GitHub IDs from
// PendingGitHubDeletes on disk. IDs not in `drained` (DELETE failed or was
// not attempted) remain in the list so the next push retries them.
func updateCritJSONAfterDeletes(critPath string, drained []int64) error {
	if len(drained) == 0 {
		return nil
	}
	drainedSet := make(map[int64]struct{}, len(drained))
	for _, id := range drained {
		drainedSet[id] = struct{}{}
	}

	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		return err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return err
	}

	kept := make([]int64, 0, len(cj.PendingGitHubDeletes))
	for _, id := range cj.PendingGitHubDeletes {
		if _, drained := drainedSet[id]; drained {
			continue
		}
		kept = append(kept, id)
	}
	if len(kept) == 0 {
		cj.PendingGitHubDeletes = nil
	} else {
		cj.PendingGitHubDeletes = kept
	}

	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(reviewPathsFor(critPath).Review, append(out, '\n'), 0644)
}

// truncateStr returns the first n runes of s, or all of s if shorter.
func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
