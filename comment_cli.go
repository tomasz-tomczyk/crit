package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// isAbsoluteOrTraversal reports whether p looks like an absolute path or
// escapes via "..". Checks both POSIX (/foo) and host (C:\\foo) notations,
// so the validator rejects /etc/passwd-style inputs on Windows where
// filepath.IsAbs("/foo") returns false. Run on the raw input — after
// filepath.Clean a leading "/" can be rewritten to "\\" on Windows and
// the prefix check would silently miss the attack.
func isAbsoluteOrTraversal(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	cleaned := filepath.Clean(p)
	return strings.HasPrefix(cleaned, "..")
}

// appendCommentScoped adds a comment to the CritJSON struct in memory with
// HeadSHA / DiffScope stamping. Does not write to disk.
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
				ID:          randomReplyID(),
				Body:        body,
				Author:      author,
				UserID:      userID,
				CreatedAt:   now,
				ReviewRound: cj.ReviewRound,
			}
			cj.ReviewComments[i].Replies = append(cj.ReviewComments[i].Replies, reply)
			cj.ReviewComments[i].UpdatedAt = now
			if resolve {
				cj.ReviewComments[i].Resolved = true
				cj.ReviewComments[i].ResolvedRound = cj.ReviewRound
			} else {
				// Match HTTP AddReviewCommentReply semantics: a non-resolving
				// reply unresolves a previously-resolved comment so the new
				// reply doesn't get hidden by the resolution filter.
				cj.ReviewComments[i].Resolved = false
				cj.ReviewComments[i].ResolvedRound = 0
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
						ID:          randomReplyID(),
						Body:        body,
						Author:      author,
						UserID:      userID,
						CreatedAt:   now,
						ReviewRound: cj.ReviewRound,
					}
					cf.Comments[i].Replies = append(cf.Comments[i].Replies, reply)
					cf.Comments[i].UpdatedAt = now
					if resolve {
						cf.Comments[i].Resolved = true
						cf.Comments[i].ResolvedRound = cj.ReviewRound
					} else {
						// Match HTTP AddReply semantics: a non-resolving reply
						// unresolves a previously-resolved comment so the new
						// reply doesn't get hidden by the resolution filter.
						cf.Comments[i].Resolved = false
						cf.Comments[i].ResolvedRound = 0
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

// addCommentToCritJSONScoped appends a comment to the review file for the given file and line range.
// Creates the review file if it doesn't exist. Appends to existing comments if it does.
// Works in both git repos and plain directories (file mode).
// outputDir overrides the default location (repo root or CWD) when non-empty.
// scope carries optional HeadSHA / DiffScope stamping; the zero value produces
// working-tree behavior.
// userID is documented to support callers that pass an authenticated user; tests
// pass "" because they don't need that path.
//
//nolint:unparam // userID is part of the public contract; tests don't exercise it
func addCommentToCritJSONScoped(filePath string, startLine, endLine int, body, author, userID, outputDir string, scope inheritedScope) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	if isAbsoluteOrTraversal(filePath) {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}
	cleaned := filepath.Clean(filePath)
	// Review JSON is a cross-platform artefact (synced via crit-web, GitHub, share),
	// so paths are stored with forward slashes regardless of host OS.
	cleaned = filepath.ToSlash(cleaned)

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

	if len(aux.Line) > 0 && string(aux.Line) != "null" {
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
		return fmt.Errorf("line must be int or string, got %s", aux.Line)
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

	if isAbsoluteOrTraversal(filePath) {
		return fmt.Errorf("entry %d: path %q must be relative and within the repository", i, filePath)
	}
	// Normalize for cross-platform storage — see addCommentToCritJSONScoped.
	cleaned := filepath.ToSlash(filepath.Clean(filePath))

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

// bulkAddCommentsToCritJSONScoped writes a batch of comments to a single review
// file with scope stamping applied to every entry.
//
// Target resolution: a bulk call always writes to a single review file. If any
// entry uses reply_to and none of the referenced IDs live in the cwd-resolved
// primary file, the entire bulk is redirected to the alt file that contains
// them. New comments in the same bulk ride along. If reply IDs split across
// multiple review files (or some land in primary and others elsewhere), the
// call is rejected — callers should split into per-file bulks.
//
//nolint:unparam // globalUserID is part of the public contract; tests don't exercise it
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

// addReviewCommentToCritJSONScoped adds a review-level comment to the review
// file with scope stamping.
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

// addFileCommentToCritJSONScoped adds a file-level comment to the review file
// with scope stamping.
func addFileCommentToCritJSONScoped(filePath, body, author, userID, outputDir string, scope inheritedScope) error {
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return err
	}

	if isAbsoluteOrTraversal(filePath) {
		return fmt.Errorf("path %q must be relative and within the repository", filePath)
	}
	cleaned := filepath.Clean(filePath)
	// Review JSON is a cross-platform artefact (synced via crit-web, GitHub, share),
	// so paths are stored with forward slashes regardless of host OS.
	cleaned = filepath.ToSlash(cleaned)

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	appendFileCommentScoped(&cj, cleaned, body, author, userID, scope)
	return saveCritJSON(critPath, cj)
}

// appendReviewCommentScoped adds a review-level comment to the CritJSON struct
// in memory, stamping DiffScope/HeadSHA from scope.
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

// appendFileCommentScoped adds a file-level comment (scope: "file", lines: 0)
// to the CritJSON struct in memory, stamping DiffScope/HeadSHA from scope.
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
