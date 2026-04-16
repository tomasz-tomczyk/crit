package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
}

// prAuthor is used to unmarshal the nested author field from gh output.
type prAuthor struct {
	Login string `json:"login"`
}

// prInfoRaw mirrors the gh JSON output shape (author is nested).
type prInfoRaw struct {
	URL          string   `json:"url"`
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	IsDraft      bool     `json:"isDraft"`
	State        string   `json:"state"`
	Body         string   `json:"body"`
	BaseRefName  string   `json:"baseRefName"`
	HeadRefName  string   `json:"headRefName"`
	Additions    int      `json:"additions"`
	Deletions    int      `json:"deletions"`
	ChangedFiles int      `json:"changedFiles"`
	Author       prAuthor `json:"author"`
	CreatedAt    string   `json:"createdAt"`
}

// detectPRInfo returns PR metadata for the current branch.
// Returns nil if gh is unavailable, no PR exists, or the PR is merged/closed
// (to avoid associating a new local branch with a stale PR that had the same name).
func detectPRInfo() *PRInfo {
	if err := requireGH(); err != nil {
		return nil
	}
	out, err := exec.Command("gh", "pr", "view", "--json",
		"number,url,title,isDraft,state,body,baseRefName,headRefName,additions,deletions,changedFiles,author,createdAt").Output()
	if err != nil {
		return nil
	}
	var raw prInfoRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	if raw.URL == "" || raw.State == "MERGED" || raw.State == "CLOSED" {
		return nil
	}
	return &PRInfo{
		URL:          raw.URL,
		Number:       raw.Number,
		Title:        raw.Title,
		IsDraft:      raw.IsDraft,
		State:        raw.State,
		Body:         raw.Body,
		BaseRefName:  raw.BaseRefName,
		HeadRefName:  raw.HeadRefName,
		Additions:    raw.Additions,
		Deletions:    raw.Deletions,
		ChangedFiles: raw.ChangedFiles,
		AuthorLogin:  raw.Author.Login,
		CreatedAt:    raw.CreatedAt,
	}
}

// fetchPRComments fetches all review comments for a PR.
func fetchPRComments(prNumber int) ([]ghComment, error) {
	// Use --paginate --slurp to collect all pages into a single JSON structure.
	// --slurp wraps each page into an outer array: [[page1...], [page2...], ...]
	// So we unmarshal into [][]ghComment and flatten.
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
func appendNewGHReplies(comments []Comment, ci int, childReplies []ghComment) int {
	added := 0
	for _, r := range childReplies {
		if isDuplicateGHReply(comments[ci].Replies, r.ID) {
			continue
		}
		comments[ci].Replies = append(comments[ci].Replies, Reply{
			ID:        randomReplyID(),
			Body:      r.Body,
			Author:    r.User.Login,
			CreatedAt: r.CreatedAt,
			GitHubID:  r.ID,
		})
		added++
	}
	return added
}

// mergeRootComment handles a single root ghComment: either deduplicates or creates it.
func mergeRootComment(cj *CritJSON, gc ghComment, replyMap map[int64][]ghComment, now string) int {
	cf, ok := cj.Files[gc.Path]
	if !ok {
		cf = CritJSONFile{Status: "modified", Comments: []Comment{}}
	}

	startLine := gc.StartLine
	if startLine == 0 {
		startLine = gc.Line
	}

	if isDuplicateGHComment(cf.Comments, gc.ID, gc.User.Login, startLine, gc.Line, gc.Body) {
		added := 0
		if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
			for ci, c := range cf.Comments {
				if c.GitHubID == gc.ID {
					added = appendNewGHReplies(cf.Comments, ci, childReplies)
					break
				}
			}
			cj.Files[gc.Path] = cf
		}
		return added
	}

	commentID := randomCommentID()
	comment := Comment{
		ID: commentID, StartLine: startLine, EndLine: gc.Line,
		Body: gc.Body, Author: gc.User.Login, CreatedAt: gc.CreatedAt,
		UpdatedAt: now, GitHubID: gc.ID,
	}

	added := 0
	if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
		for _, r := range childReplies {
			comment.Replies = append(comment.Replies, Reply{
				ID:        randomReplyID(),
				Body:      r.Body,
				Author:    r.User.Login,
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
func mergeOrphanReplies(cj *CritJSON, roots []ghComment, replyMap map[int64][]ghComment) int {
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
		added += appendNewGHReplies(cf.Comments, ci, childReplies)
		cj.Files[filePath] = cf
	}
	return added
}

// mergeGHComments appends GitHub PR comments into an existing CritJSON.
// Only includes RIGHT-side comments (comments on the new version of the file).
// Handles threading: root comments become top-level Comments, replies become Reply entries.
// Deduplicates by GitHubID (preferred) or author+lines+body to prevent duplicates from repeated pulls.
func mergeGHComments(cj *CritJSON, ghComments []ghComment) int {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	roots, replyMap := separateRootsAndReplies(ghComments)

	added := 0
	for _, gc := range roots {
		added += mergeRootComment(cj, gc, replyMap, now)
	}
	added += mergeOrphanReplies(cj, roots, replyMap)

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

	// Check daemon registry for running sessions.
	sessions, _ := listSessionsForCWD(cwd)
	if len(sessions) == 1 && sessions[0].ReviewPath != "" {
		return sessions[0].ReviewPath, nil
	}
	if len(sessions) > 1 {
		path := resolveReviewPathFromSessions(sessions)
		if path != "" {
			return path, nil
		}
	}

	// No daemon — compute centralized path.
	branch := ""
	if IsGitRepo() {
		branch = CurrentBranch()
	}
	key := sessionKey(cwd, branch, nil)
	path, err := reviewFilePath(key)
	if err != nil {
		return "", err
	}

	return path, nil
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
			base = DefaultBranch()
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
func appendComment(cj *CritJSON, filePath string, startLine, endLine int, body, author string) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	cf, ok := cj.Files[filePath]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	cf.Comments = append(cf.Comments, Comment{
		ID:        randomCommentID(),
		StartLine: startLine,
		EndLine:   endLine,
		Body:      body,
		Author:    author,
		CreatedAt: now,
		UpdatedAt: now,
	})
	cj.Files[filePath] = cf
}

// appendReply adds a reply to an existing comment in the CritJSON struct in memory.
// Returns an error if the comment ID is not found or is ambiguous across files.
// Searches both file comments and review_comments.
func appendReply(cj *CritJSON, commentID, body, author string, resolve bool, filterPath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	// Check all review comments (not just those starting with "r" — web-fetched ones use "web-N").
	for i, c := range cj.ReviewComments {
		if c.ID == commentID {
			reply := Reply{
				ID:        randomReplyID(),
				Body:      body,
				Author:    author,
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
func addCommentToCritJSON(filePath string, startLine, endLine int, body string, author string, outputDir string) error {
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

	appendComment(&cj, cleaned, startLine, endLine, body, author)
	return saveCritJSON(critPath, cj)
}

// addReplyToCritJSON adds a reply to an existing comment in the review file.
// It searches all files for the comment ID. If resolve is true, it also marks the comment as resolved.
func addReplyToCritJSON(commentID, body, author string, resolve bool, outputDir string, filterPath string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	if err := appendReply(&cj, commentID, body, author, resolve, filterPath); err != nil {
		return err
	}
	return saveCritJSON(critPath, cj)
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

// bulkAddCommentsToCritJSON applies multiple comments and replies in a single load-save cycle.
// globalAuthor is used when an entry doesn't specify its own author.
// outputDir overrides the review file location (empty = centralized storage).
func processBulkEntry(cj *CritJSON, i int, e BulkCommentEntry, globalAuthor string) error {
	if e.Body == "" {
		return fmt.Errorf("entry %d: body is required", i)
	}

	author := e.Author
	if author == "" {
		author = globalAuthor
	}

	if e.ReplyTo != "" {
		if err := appendReply(cj, e.ReplyTo, e.Body, author, e.Resolve, e.File); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		return nil
	}

	if e.Scope == "review" || (e.File == "" && e.Path == "" && e.Line <= 0 && e.LineSpec == "") {
		return processBulkReviewEntry(cj, i, e, author)
	}

	return processBulkFileOrLineEntry(cj, i, e, author)
}

func processBulkReviewEntry(cj *CritJSON, i int, e BulkCommentEntry, author string) error {
	if e.Line > 0 || e.LineSpec != "" {
		return fmt.Errorf("entry %d: file is required for new comments", i)
	}
	if e.Scope != "review" && (e.File != "" || e.Path != "") {
		return fmt.Errorf("entry %d: file is required for new comments", i)
	}
	appendReviewComment(cj, e.Body, author)
	return nil
}

func processBulkFileOrLineEntry(cj *CritJSON, i int, e BulkCommentEntry, author string) error {
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
		appendFileComment(cj, cleaned, e.Body, author)
		return nil
	}

	if e.Line <= 0 && e.LineSpec == "" {
		if e.Path != "" && e.File == "" {
			appendFileComment(cj, cleaned, e.Body, author)
			return nil
		}
		return fmt.Errorf("entry %d: line must be > 0", i)
	}

	return processBulkLineComment(cj, i, e, cleaned, author)
}

func processBulkLineComment(cj *CritJSON, i int, e BulkCommentEntry, cleaned, author string) error {
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

	appendComment(cj, cleaned, startLine, endLine, e.Body, author)
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

func bulkAddCommentsToCritJSON(entries []BulkCommentEntry, globalAuthor string, outputDir string) error {
	if len(entries) == 0 {
		return fmt.Errorf("no comment entries provided")
	}

	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	for i, e := range entries {
		if err := processBulkEntry(&cj, i, e, globalAuthor); err != nil {
			return err
		}
	}

	return saveCritJSON(critPath, cj)
}

// addReviewCommentToCritJSON adds a review-level comment to the review file.
func addReviewCommentToCritJSON(body, author, outputDir string) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendReviewComment(&cj, body, author)
	return saveCritJSON(critPath, cj)
}

// addFileCommentToCritJSON adds a file-level comment to the review file.
func addFileCommentToCritJSON(filePath, body, author, outputDir string) error {
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

	appendFileComment(&cj, cleaned, body, author)
	return saveCritJSON(critPath, cj)
}

// appendReviewComment adds a review-level comment to the CritJSON struct in memory.
func appendReviewComment(cj *CritJSON, body, author string) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	cj.ReviewComments = append(cj.ReviewComments, Comment{
		ID:        randomReviewCommentID(),
		Body:      body,
		Author:    author,
		Scope:     "review",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// appendFileComment adds a file-level comment (scope: "file", lines: 0) to the CritJSON struct in memory.
func appendFileComment(cj *CritJSON, filePath, body, author string) {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	cf, ok := cj.Files[filePath]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	cf.Comments = append(cf.Comments, Comment{
		ID:        randomCommentID(),
		Body:      body,
		Author:    author,
		Scope:     "file",
		CreatedAt: now,
		UpdatedAt: now,
	})
	cj.Files[filePath] = cf
}
