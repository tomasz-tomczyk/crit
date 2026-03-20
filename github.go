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

// nextCommentID scans ALL files' comments in a CritJSON and returns the next
// available globally-unique cN ID. This ensures IDs don't collide across files.
func nextCommentID(files map[string]CritJSONFile) int {
	next := 1
	for _, cf := range files {
		for _, c := range cf.Comments {
			id := 0
			_, _ = fmt.Sscanf(c.ID, "c%d", &id)
			if id >= next {
				next = id + 1
			}
		}
	}
	return next
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

// mergeGHComments appends GitHub PR comments into an existing CritJSON.
// Only includes RIGHT-side comments (comments on the new version of the file).
// Handles threading: root comments become top-level Comments, replies become Reply entries.
// Deduplicates by GitHubID (preferred) or author+lines+body to prevent duplicates from repeated pulls.
func mergeGHComments(cj *CritJSON, ghComments []ghComment) int {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now
	added := 0

	// Separate roots from replies, filtering as we go
	var roots []ghComment
	var replies []ghComment
	for _, gc := range ghComments {
		if gc.Line == 0 {
			continue // skip PR-level comments not attached to a line
		}
		if gc.Side == "LEFT" {
			continue // skip comments on deleted/old lines
		}
		if gc.InReplyToID == 0 {
			roots = append(roots, gc)
		} else {
			replies = append(replies, gc)
		}
	}

	// Group replies by their parent (InReplyToID)
	replyMap := make(map[int64][]ghComment)
	for _, r := range replies {
		replyMap[r.InReplyToID] = append(replyMap[r.InReplyToID], r)
	}

	// Sort each reply group by created_at
	for parentID := range replyMap {
		sort.Slice(replyMap[parentID], func(i, j int) bool {
			return replyMap[parentID][i].CreatedAt < replyMap[parentID][j].CreatedAt
		})
	}

	// Process root comments: create Comment with attached replies
	for _, gc := range roots {
		cf, ok := cj.Files[gc.Path]
		if !ok {
			cf = CritJSONFile{
				Status:   "modified",
				Comments: []Comment{},
			}
		}

		startLine := gc.StartLine
		if startLine == 0 {
			startLine = gc.Line // single-line comment
		}

		// Skip if this root comment already exists (dedup for repeated pulls)
		if isDuplicateGHComment(cf.Comments, gc.ID, gc.User.Login, startLine, gc.Line, gc.Body) {
			// Even if root is a dupe, check if there are new replies to add
			if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
				// Find the existing comment
				for ci, c := range cf.Comments {
					if c.GitHubID == gc.ID {
						for _, r := range childReplies {
							if isDuplicateGHReply(cf.Comments[ci].Replies, r.ID) {
								continue
							}
							cf.Comments[ci].Replies = append(cf.Comments[ci].Replies, Reply{
								ID:        nextReplyID(c.ID, cf.Comments[ci].Replies),
								Body:      r.Body,
								Author:    r.User.Login,
								CreatedAt: r.CreatedAt,
								GitHubID:  r.ID,
							})
							added++
						}
						break
					}
				}
				cj.Files[gc.Path] = cf
			}
			continue
		}

		commentID := fmt.Sprintf("c%d", nextCommentID(cj.Files))
		comment := Comment{
			ID:        commentID,
			StartLine: startLine,
			EndLine:   gc.Line,
			Body:      gc.Body,
			Author:    gc.User.Login,
			CreatedAt: gc.CreatedAt,
			UpdatedAt: now,
			GitHubID:  gc.ID,
		}

		// Attach replies for this root
		if childReplies, hasReplies := replyMap[gc.ID]; hasReplies {
			for _, r := range childReplies {
				comment.Replies = append(comment.Replies, Reply{
					ID:        nextReplyID(commentID, comment.Replies),
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
		added++ // count the root
	}

	// Process orphan replies: parent already exists in cj (from a previous pull)
	for parentID, childReplies := range replyMap {
		// Skip if we already handled this parent in the roots loop above
		handled := false
		for _, gc := range roots {
			if gc.ID == parentID {
				handled = true
				break
			}
		}
		if handled {
			continue
		}

		// Find the parent comment in the existing CritJSON
		filePath, ci, found := findCommentByGitHubID(cj, parentID)
		if !found {
			continue // orphan reply with no known parent — skip
		}

		cf := cj.Files[filePath]
		for _, r := range childReplies {
			if isDuplicateGHReply(cf.Comments[ci].Replies, r.ID) {
				continue
			}
			cf.Comments[ci].Replies = append(cf.Comments[ci].Replies, Reply{
				ID:        nextReplyID(cf.Comments[ci].ID, cf.Comments[ci].Replies),
				Body:      r.Body,
				Author:    r.User.Login,
				CreatedAt: r.CreatedAt,
				GitHubID:  r.ID,
			})
			added++
		}
		cj.Files[filePath] = cf
	}

	return added
}

// resolveCritDir returns the directory where .crit.json should be read/written.
// If outputDir is non-empty it is used directly. Otherwise falls back to repo root then CWD.
func resolveCritDir(outputDir string) (string, error) {
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return "", fmt.Errorf("resolving output directory: %w", err)
		}
		return abs, nil
	}
	root, err := RepoRoot()
	if err != nil {
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting working directory: %w", err)
		}
	}
	return root, nil
}

// writeCritJSON writes a CritJSON to the repo root or outputDir.
func writeCritJSON(cj CritJSON, outputDir string) error {
	root, err := resolveCritDir(outputDir)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(root, ".crit.json"), data, 0644); err != nil {
		return fmt.Errorf("writing .crit.json: %w", err)
	}
	return nil
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
		return 0, nil // non-fatal: reply was posted, just can't parse ID
	}
	return resp.ID, nil
}

// critJSONToGHComments converts .crit.json comments to GitHub review comment format.
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

	// Parse response to extract comment IDs
	var resp struct {
		Comments []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
			Line int    `json:"line"`
		} `json:"comments"`
	}
	idMap := make(map[string]int64)
	if err := json.Unmarshal(stdout.Bytes(), &resp); err == nil {
		for _, c := range resp.Comments {
			key := fmt.Sprintf("%s:%d", c.Path, c.Line)
			idMap[key] = c.ID
		}
	}
	return idMap, nil
}

// replyKey uniquely identifies a reply for GitHubID mapping after push.
type replyKey struct {
	ParentGHID int64
	BodyPrefix string
}

// updateCritJSONWithGitHubIDs writes GitHub IDs back to .crit.json after a push.
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
	return os.WriteFile(critPath, append(out, '\n'), 0644)
}

// truncateStr returns the first n bytes of s, or all of s if shorter.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// loadCritJSON reads .crit.json from disk, or returns a fresh CritJSON if the file doesn't exist.
func loadCritJSON(critPath string) (CritJSON, error) {
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return cj, fmt.Errorf("invalid existing .crit.json: %w", err)
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
		return cj, fmt.Errorf("reading .crit.json: %w", err)
	}
	return cj, nil
}

// saveCritJSON writes the CritJSON struct to disk with pretty-printed JSON.
func saveCritJSON(critPath string, cj CritJSON) error {
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}
	return os.WriteFile(critPath, data, 0644)
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
		ID:        fmt.Sprintf("c%d", nextCommentID(cj.Files)),
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
func appendReply(cj *CritJSON, commentID, body, author string, resolve bool, filterPath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

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
						ID:        nextReplyID(commentID, c.Replies),
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
		return fmt.Errorf("comment %q found in multiple files (%s); specify the file with \"file\" field",
			commentID, strings.Join(foundPaths, ", "))
	}
	if !found {
		if filterPath != "" {
			return fmt.Errorf("comment %q not found in file %q in .crit.json", commentID, filterPath)
		}
		return fmt.Errorf("comment %q not found in .crit.json", commentID)
	}
	return nil
}

// addCommentToCritJSON appends a comment to .crit.json for the given file and line range.
// Creates .crit.json if it doesn't exist. Appends to existing comments if it does.
// Works in both git repos and plain directories (file mode).
// outputDir overrides the default location (repo root or CWD) when non-empty.
func addCommentToCritJSON(filePath string, startLine, endLine int, body string, author string, outputDir string) error {
	root, err := resolveCritDir(outputDir)
	if err != nil {
		return err
	}

	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}

	critPath := filepath.Join(root, ".crit.json")
	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendComment(&cj, cleaned, startLine, endLine, body, author)
	return saveCritJSON(critPath, cj)
}

// addReplyToCritJSON adds a reply to an existing comment in .crit.json.
// It searches all files for the comment ID. If resolve is true, it also marks the comment as resolved.
func addReplyToCritJSON(commentID, body, author string, resolve bool, outputDir string, filterPath string) error {
	root, err := resolveCritDir(outputDir)
	if err != nil {
		return err
	}

	critPath := filepath.Join(root, ".crit.json")
	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	if err := appendReply(&cj, commentID, body, author, resolve, filterPath); err != nil {
		return err
	}
	return saveCritJSON(critPath, cj)
}

// clearCritJSON removes .crit.json from the repo root, working directory, or outputDir.
func clearCritJSON(outputDir string) error {
	root, err := resolveCritDir(outputDir)
	if err != nil {
		return err
	}
	critPath := filepath.Join(root, ".crit.json")
	if err := os.Remove(critPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// BulkCommentEntry represents one entry in a bulk comment JSON array.
// Either (file + line) for a new comment, or (reply_to) for a reply.
type BulkCommentEntry struct {
	// New comment fields
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line,omitempty"` // defaults to Line if omitted
	Body    string `json:"body"`
	Author  string `json:"author,omitempty"` // overrides per-entry; falls back to global

	// Reply fields
	ReplyTo string `json:"reply_to,omitempty"`
	Resolve bool   `json:"resolve,omitempty"`
}

// bulkAddCommentsToCritJSON applies multiple comments and replies in a single load-save cycle.
// globalAuthor is used when an entry doesn't specify its own author.
// outputDir overrides the .crit.json location (empty = repo root or CWD).
func bulkAddCommentsToCritJSON(entries []BulkCommentEntry, globalAuthor string, outputDir string) error {
	if len(entries) == 0 {
		return fmt.Errorf("no comment entries provided")
	}

	root, err := resolveCritDir(outputDir)
	if err != nil {
		return err
	}

	critPath := filepath.Join(root, ".crit.json")
	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	for i, e := range entries {
		if e.Body == "" {
			return fmt.Errorf("entry %d: body is required", i)
		}

		author := e.Author
		if author == "" {
			author = globalAuthor
		}

		if e.ReplyTo != "" {
			// Reply mode
			if err := appendReply(&cj, e.ReplyTo, e.Body, author, e.Resolve, e.File); err != nil {
				return fmt.Errorf("entry %d: %w", i, err)
			}
		} else {
			// New comment mode
			if e.File == "" {
				return fmt.Errorf("entry %d: file is required for new comments", i)
			}
			if e.Line <= 0 {
				return fmt.Errorf("entry %d: line must be > 0", i)
			}

			cleaned := filepath.Clean(e.File)
			if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
				return fmt.Errorf("entry %d: path %q must be relative and within the repository", i, e.File)
			}

			endLine := e.EndLine
			if endLine == 0 {
				endLine = e.Line
			}

			appendComment(&cj, cleaned, e.Line, endLine, e.Body, author)
		}
	}

	return saveCritJSON(critPath, cj)
}
