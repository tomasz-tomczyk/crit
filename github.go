package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ghComment represents a GitHub PR review comment from the API.
type ghComment struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`       // end line in the diff (RIGHT side = new file line)
	StartLine int    `json:"start_line"` // start line for multi-line comments (0 if single-line)
	Side      string `json:"side"`       // "RIGHT" or "LEFT"
	Body      string `json:"body"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
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

// nextCommentID scans existing comments and returns the next available cN ID.
func nextCommentID(comments []Comment) int {
	next := 1
	for _, c := range comments {
		id := 0
		_, _ = fmt.Sscanf(c.ID, "c%d", &id)
		if id >= next {
			next = id + 1
		}
	}
	return next
}

// isDuplicateGHComment checks if a GitHub comment already exists in the comment list
// by matching on author, line range, and body.
func isDuplicateGHComment(comments []Comment, author string, startLine, endLine int, body string) bool {
	for _, c := range comments {
		if c.Author == author && c.StartLine == startLine && c.EndLine == endLine && c.Body == body {
			return true
		}
	}
	return false
}

// mergeGHComments appends GitHub PR comments into an existing CritJSON.
// Only includes RIGHT-side comments (comments on the new version of the file).
// Deduplicates by author+lines+body to prevent duplicates from repeated pulls.
func mergeGHComments(cj *CritJSON, comments []ghComment) int {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now
	added := 0

	for _, gc := range comments {
		if gc.Line == 0 {
			continue // skip PR-level comments not attached to a line
		}
		if gc.Side == "LEFT" {
			continue // skip comments on deleted/old lines
		}

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

		// Skip if this exact comment already exists (dedup for repeated pulls)
		if isDuplicateGHComment(cf.Comments, gc.User.Login, startLine, gc.Line, gc.Body) {
			continue
		}

		cf.Comments = append(cf.Comments, Comment{
			ID:        fmt.Sprintf("c%d", nextCommentID(cf.Comments)),
			StartLine: startLine,
			EndLine:   gc.Line,
			Body:      gc.Body,
			Author:    gc.User.Login,
			CreatedAt: gc.CreatedAt,
			UpdatedAt: now,
		})
		cj.Files[gc.Path] = cf
		added++
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

// critJSONToGHComments converts .crit.json comments to GitHub review comment format.
// Returns the list of comments suitable for the GitHub "create review" API.
func critJSONToGHComments(cj CritJSON) []map[string]any {
	var result []map[string]any
	for path, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.Resolved {
				continue // don't post resolved comments
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

// buildReviewPayload constructs the JSON body for a GitHub PR review request.
func buildReviewPayload(comments []map[string]any, message string) ([]byte, error) {
	review := map[string]any{
		"event":    "COMMENT",
		"body":     message,
		"comments": comments,
	}
	return json.Marshal(review)
}

// createGHReview posts a review with inline comments to a GitHub PR.
// message is the top-level review body (empty string posts no top-level comment).
func createGHReview(prNumber int, comments []map[string]any, message string) error {
	data, err := buildReviewPayload(comments, message)
	if err != nil {
		return fmt.Errorf("marshaling review: %w", err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNumber),
		"--method", "POST",
		"--input", "-",
	)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("creating review: %s", strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("creating review: %w", err)
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

	// Validate path is relative and doesn't escape the working directory
	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}

	critPath := filepath.Join(root, ".crit.json")

	// Load existing or create new
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return fmt.Errorf("invalid existing .crit.json: %w", err)
		}
	} else {
		branch := CurrentBranch()
		cfg := LoadConfig(root)
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
	}

	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	// Get or create the file entry
	cf, ok := cj.Files[cleaned]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	cf.Comments = append(cf.Comments, Comment{
		ID:        fmt.Sprintf("c%d", nextCommentID(cf.Comments)),
		StartLine: startLine,
		EndLine:   endLine,
		Body:      body,
		Author:    author,
		CreatedAt: now,
		UpdatedAt: now,
	})
	cj.Files[cleaned] = cf

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}
	return os.WriteFile(critPath, data, 0644)
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
