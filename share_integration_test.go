//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func critWebURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("CRIT_WEB_URL"); u != "" {
		return u
	}
	return "http://localhost:4000"
}

func critBinary(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("CRIT_BINARY"); b != "" {
		return b
	}
	// Default: built binary next to the test
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "crit")
}

// TestShareSyncIntegration exercises the full share -> review -> re-share loop.
func TestShareSyncIntegration(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// a) Create a plan with a pre-resolved local comment
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nSection 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	initialCJ := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{
						ID: "c1", StartLine: 3, EndLine: 3,
						Body: "resolved local comment", Resolved: true, ReviewRound: 1,
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
	}
	writeTestCritJSON(t, dir, initialCJ)

	// b) Share to local crit-web (first share = POST, creates review)
	cmd := exec.Command(binary, "share", "--share-url", baseURL, "--output", dir, "plan.md")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %s\n%s", err, out)
	}

	// First share output is just the URL
	shareOutput := strings.TrimSpace(string(out))
	// Extract the URL — it may be preceded by warnings on stderr
	lines := strings.Split(shareOutput, "\n")
	shareURL := lines[len(lines)-1]
	if !strings.Contains(shareURL, "/r/") {
		t.Fatalf("expected a review URL, got: %s", shareURL)
	}
	token := path.Base(shareURL)
	t.Logf("Shared to: %s (token: %s)", shareURL, token)

	// c) Simulate a web reviewer adding a new comment via seed-comment endpoint
	seedPayload, _ := json.Marshal(map[string]any{
		"file": "plan.md", "start_line": 1, "end_line": 1,
		"body": "web reviewer comment",
	})
	seedResp, err := http.Post(
		baseURL+"/api/reviews/"+token+"/seed-comment",
		"application/json", bytes.NewReader(seedPayload),
	)
	if err != nil {
		t.Fatalf("seed-comment request failed: %v", err)
	}
	if seedResp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", seedResp.StatusCode)
	}
	seedResp.Body.Close()

	// d) Agent applies changes locally — update the plan
	if err := os.WriteFile(planPath, []byte("# Plan\n\nSection 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// e) Re-share: crit share should pull web comment, push new round
	cmd2 := exec.Command(binary, "share", "--share-url", baseURL, "--output", dir, "plan.md")
	cmd2.Dir = dir
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("second crit share failed: %s\n%s", err, out2)
	}
	output2 := string(out2)
	t.Logf("Second share output: %s", output2)
	if !strings.Contains(output2, "Updated (round 2)") {
		t.Errorf("expected 'Updated (round 2)' in output, got: %s", output2)
	}

	// f) Verify crit-web state: latest file content should be updated
	docResp, err := http.Get(baseURL + "/api/reviews/" + token + "/document")
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer docResp.Body.Close()
	if docResp.StatusCode != http.StatusOK {
		t.Fatalf("document returned %d", docResp.StatusCode)
	}

	var docBody struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(docResp.Body).Decode(&docBody); err != nil {
		t.Fatalf("decoding document response: %v", err)
	}
	if len(docBody.Files) == 0 {
		t.Fatal("expected at least one file in document response")
	}
	if !strings.Contains(docBody.Files[0].Content, "Section 1 (revised)") {
		t.Errorf("crit-web should have updated content, got: %s", docBody.Files[0].Content)
	}

	// g) Verify the web reviewer comment was pulled into local .crit.json
	// (post-v4 .crit.json is a folder; the canonical review payload lives at
	// .crit/review.json).
	localData, err := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	if err != nil {
		t.Fatalf("reading .crit.json: %v", err)
	}
	if !strings.Contains(string(localData), "web reviewer comment") {
		t.Errorf("expected web reviewer comment in local .crit.json, got: %s", string(localData))
	}

	// h) Verify export endpoint returns .crit.json-compatible shape
	exportResp, err := http.Get(baseURL + "/api/export/" + token + "/comments")
	if err != nil {
		t.Fatalf("export request failed: %v", err)
	}
	defer exportResp.Body.Close()
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export returned %d", exportResp.StatusCode)
	}

	var exportBody map[string]any
	if err := json.NewDecoder(exportResp.Body).Decode(&exportBody); err != nil {
		t.Fatalf("decoding export response: %v", err)
	}

	// Top-level .crit.json fields must be present
	if exportBody["review_round"] == nil {
		t.Error("export missing review_round")
	}
	if exportBody["share_url"] == nil {
		t.Error("export missing share_url")
	}
	if exportBody["delete_token"] == nil {
		t.Error("export missing delete_token")
	}

	// Comment shape must use "author" not "author_display_name"
	files, _ := exportBody["files"].(map[string]any)
	for _, fileEntry := range files {
		entry, _ := fileEntry.(map[string]any)
		comments, _ := entry["comments"].([]any)
		for _, raw := range comments {
			c, _ := raw.(map[string]any)
			if _, hasOld := c["author_display_name"]; hasOld {
				t.Error("export comment must not have author_display_name — use author instead")
			}
			if _, hasOld := c["author_identity"]; hasOld {
				t.Error("export comment must not have author_identity")
			}
			if _, hasAuthor := c["author"]; !hasAuthor {
				t.Error("export comment missing author field")
			}
		}
	}

	t.Logf("Share sync integration test passed. Review URL: %s", shareURL)
}

// --- Helpers ---

// seedComment is a helper for integration tests to simulate a web reviewer comment.
func seedComment(t *testing.T, baseURL, token, file, body string) {
	t.Helper()
	seedCommentAt(t, baseURL, token, file, body, 1, 1)
}

// seedCommentAt seeds a comment at a specific line range.
func seedCommentAt(t *testing.T, baseURL, token, file, body string, startLine, endLine int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"file": file, "start_line": startLine, "end_line": endLine, "body": body,
	})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-comment failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", resp.StatusCode)
	}
}

// seedCommentGetID seeds a comment and returns its crit-web ID (for use with seedReply).
func seedCommentGetID(t *testing.T, baseURL, token, file, body string, startLine, endLine int) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"file": file, "start_line": startLine, "end_line": endLine, "body": body,
	})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-comment failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding seed-comment response: %v", err)
	}
	return result.ID
}

// seedReply seeds a reply to an existing comment on crit-web.
func seedReply(t *testing.T, baseURL, token, commentID, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"body": body})
	resp, err := http.Post(
		baseURL+"/api/reviews/"+token+"/seed-reply/"+commentID,
		"application/json", bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("seed-reply failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-reply returned %d", resp.StatusCode)
	}
}

// seedReviewComment seeds a review-level (file-agnostic) comment on crit-web.
func seedReviewComment(t *testing.T, baseURL, token, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"body": body, "scope": "review",
	})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-review-comment failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-review-comment returned %d", resp.StatusCode)
	}
}

// seedReviewCommentGetID seeds a review-level comment and returns its crit-web ID
// (for use with seedReply on review-level comments).
func seedReviewCommentGetID(t *testing.T, baseURL, token, body string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"body": body, "scope": "review",
	})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-review-comment failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-review-comment returned %d", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding seed-review-comment response: %v", err)
	}
	return result.ID
}

// reviewRoundFromAPI fetches the current review_round for a token from crit-web.
func reviewRoundFromAPI(t *testing.T, baseURL, token string) int {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		ReviewRound int `json:"review_round"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding document: %v", err)
	}
	return body.ReviewRound
}

// writeTestCritJSON writes a CritJSON to .crit/review.json in dir.
// NOTE: readCritJSON is defined in github_test.go and shared across test files.
func writeTestCritJSON(t *testing.T, dir string, cj CritJSON) {
	t.Helper()
	d, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(dir, ".crit")
	if err := os.MkdirAll(identity, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity, "review.json"), d, 0644); err != nil {
		t.Fatal(err)
	}
}

// critShareCmd runs `crit share` and returns stdout. Fails the test on error.
// Uses --output to point at the temp dir so crit reads/writes .crit.json there.
func critShareCmd(t *testing.T, binary, baseURL, dir string, files ...string) string {
	t.Helper()
	args := append([]string{"share", "--share-url", baseURL, "--output", dir}, files...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %s\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// critUnpublishCmd runs `crit unpublish` and returns stdout.
func critUnpublishCmd(t *testing.T, binary, baseURL, dir string) string {
	t.Helper()
	cmd := exec.Command(binary, "unpublish", "--share-url", baseURL, "--output", dir)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit unpublish failed: %s\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// extractToken extracts the review token from a share URL or output containing one.
func extractToken(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "/r/") {
			return path.Base(lines[i])
		}
	}
	t.Fatalf("no review URL found in output: %s", output)
	return ""
}

// extractURL extracts the full review URL from share output.
func extractURL(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "/r/") {
			return strings.TrimSpace(lines[i])
		}
	}
	t.Fatalf("no review URL found in output: %s", output)
	return ""
}

// logReview logs the review URL for manual inspection.
func logReview(t *testing.T, output string) {
	t.Helper()
	t.Logf("  → Review: %s", extractURL(t, output))
}

// commentsFromAPI fetches all comments for a review from crit-web.
func commentsFromAPI(t *testing.T, baseURL, token string) []webComment {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/comments", baseURL, token))
	if err != nil {
		t.Fatalf("comments request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("comments returned %d", resp.StatusCode)
	}
	var comments []webComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		t.Fatalf("decoding comments: %v", err)
	}
	return comments
}

// documentFromAPI fetches the review document files from crit-web.
func documentFromAPI(t *testing.T, baseURL, token string) []map[string]any {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("document returned %d", resp.StatusCode)
	}
	var body struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding document: %v", err)
	}
	return body.Files
}

// --- New test cases ---

// TestShareSyncNoComments verifies sharing a file with no comments.
func TestShareSyncNoComments(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "readme.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Verify document content on crit-web
	files := documentFromAPI(t, baseURL, token)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0]["path"] != "readme.md" {
		t.Errorf("expected path readme.md, got %v", files[0]["path"])
	}
	if content, _ := files[0]["content"].(string); !strings.Contains(content, "# Hello") {
		t.Errorf("expected file content '# Hello', got %q", content)
	}

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

// TestShareSyncLineComments verifies line-scoped comments with correct body, position, and scope on web.
func TestShareSyncLineComments(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "clarify this step", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					{ID: "c2", StartLine: 5, EndLine: 5, Body: "needs more detail", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	bodies := map[string]webComment{}
	for _, c := range comments {
		bodies[c.Body] = c
	}
	for _, want := range []struct {
		body  string
		line  int
		scope string
		file  string
	}{
		{"clarify this step", 3, "line", "plan.md"},
		{"needs more detail", 5, "line", "plan.md"},
	} {
		got, ok := bodies[want.body]
		if !ok {
			t.Errorf("missing comment %q on crit-web", want.body)
			continue
		}
		if got.StartLine != want.line {
			t.Errorf("comment %q: start_line = %d, want %d", want.body, got.StartLine, want.line)
		}
		if got.Scope != want.scope {
			t.Errorf("comment %q: scope = %q, want %q", want.body, got.Scope, want.scope)
		}
		if got.FilePath != want.file {
			t.Errorf("comment %q: file_path = %q, want %q", want.body, got.FilePath, want.file)
		}
	}
}

// TestShareSyncFileComment verifies file-scoped comments appear correctly on web.
func TestShareSyncFileComment(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n\nSome content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"notes.md": {
				Comments: []Comment{
					{ID: "fc1", Body: "this file needs restructuring", Scope: "file",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "notes.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Scope != "file" {
		t.Errorf("scope = %q, want 'file'", comments[0].Scope)
	}
	if comments[0].Body != "this file needs restructuring" {
		t.Errorf("body = %q, want 'this file needs restructuring'", comments[0].Body)
	}
	if comments[0].FilePath != "notes.md" {
		t.Errorf("file_path = %q, want 'notes.md'", comments[0].FilePath)
	}
}

// TestShareSyncReviewLevelComments verifies that review-level comments are shared.
// This is the fix for https://github.com/tomasz-tomczyk/crit/issues/297.
func TestShareSyncReviewLevelComments(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "rc1", Body: "overall this plan needs work", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			{ID: "rc2", Body: "consider adding a timeline", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		},
		Files: map[string]CritJSONFile{"plan.md": {}},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	reviewBodies := map[string]bool{}
	for _, c := range comments {
		if c.Scope == "review" {
			reviewBodies[c.Body] = true
		}
	}
	if len(reviewBodies) != 2 {
		t.Fatalf("expected 2 review-level comments, got %d (total: %d)", len(reviewBodies), len(comments))
	}
	if !reviewBodies["overall this plan needs work"] {
		t.Error("missing review comment 'overall this plan needs work' on crit-web")
	}
	if !reviewBodies["consider adding a timeline"] {
		t.Error("missing review comment 'consider adding a timeline' on crit-web")
	}
}

// TestShareSyncMixedCommentTypes verifies all 3 comment scopes appear correctly on web.
func TestShareSyncMixedCommentTypes(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "rc1", Body: "review-level comment", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		},
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "lc1", StartLine: 3, EndLine: 3, Body: "line-level comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					{ID: "fc1", Body: "file-level comment", Scope: "file",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(comments))
	}

	byScope := map[string]webComment{}
	for _, c := range comments {
		byScope[c.Scope] = c
	}
	if len(byScope) != 3 {
		t.Fatalf("expected 3 distinct scopes, got: %v", byScope)
	}
	if byScope["review"].Body != "review-level comment" {
		t.Errorf("review comment body = %q, want 'review-level comment'", byScope["review"].Body)
	}
	if byScope["line"].Body != "line-level comment" {
		t.Errorf("line comment body = %q, want 'line-level comment'", byScope["line"].Body)
	}
	if byScope["line"].StartLine != 3 || byScope["line"].EndLine != 3 {
		t.Errorf("line comment position = %d-%d, want 3-3", byScope["line"].StartLine, byScope["line"].EndLine)
	}
	if byScope["file"].Body != "file-level comment" {
		t.Errorf("file comment body = %q, want 'file-level comment'", byScope["file"].Body)
	}
}

// TestShareSyncResolvedExcluded verifies resolved comments are NOT shared.
func TestShareSyncResolvedExcluded(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nDone\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "resolved comment", Scope: "line",
						Resolved: true, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					{ID: "c2", StartLine: 3, EndLine: 3, Body: "unresolved comment", Scope: "line",
						Resolved: false, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment (resolved excluded), got %d", len(comments))
	}
	if comments[0].Body != "unresolved comment" {
		t.Errorf("body = %q, want 'unresolved comment'", comments[0].Body)
	}
	if comments[0].StartLine != 3 || comments[0].EndLine != 3 {
		t.Errorf("position = %d-%d, want 3-3", comments[0].StartLine, comments[0].EndLine)
	}
}

// TestShareSyncReshareNoDuplicates verifies re-sharing preserves comments without duplication.
func TestShareSyncReshareNoDuplicates(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "original comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output1 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output1)
	token := extractToken(t, output1)

	// Update content to force a change
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output2 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	if !strings.Contains(output2, "Updated (round 2)") {
		t.Errorf("expected 'Updated (round 2)', got: %s", output2)
	}
	token2 := extractToken(t, output2)
	if token != token2 {
		t.Errorf("re-share should use same token: %s vs %s", token, token2)
	}

	// Verify comment content on web: no duplicates, correct body and position
	comments := commentsFromAPI(t, baseURL, token)
	origCount := 0
	for _, c := range comments {
		if c.Body == "original comment" {
			origCount++
			if c.StartLine != 3 || c.EndLine != 3 {
				t.Errorf("comment position changed: %d-%d", c.StartLine, c.EndLine)
			}
		}
	}
	if origCount != 1 {
		t.Errorf("expected exactly 1 'original comment', got %d (total: %d)", origCount, len(comments))
	}

	// Verify updated content is on web
	files := documentFromAPI(t, baseURL, token)
	if len(files) == 0 {
		t.Fatal("no files in document after re-share")
	}
	if content, _ := files[0]["content"].(string); !strings.Contains(content, "Step 1 (revised)") {
		t.Errorf("expected revised content on web, got %q", content)
	}
}

// TestShareSyncReshareNoChanges verifies re-sharing with no changes is a no-op.
func TestShareSyncReshareNoChanges(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output1 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output1)
	_ = extractToken(t, output1)
	round1 := readCritJSON(t, dir).ReviewRound

	output2 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	if strings.Contains(output2, "Updated") {
		t.Errorf("expected no update for unchanged content, got: %s", output2)
	}

	round2 := readCritJSON(t, dir).ReviewRound
	if round2 != round1 {
		t.Errorf("round should not increment: %d → %d", round1, round2)
	}
}

// TestShareSyncFetchWebComments verifies web-authored comments are pulled back locally
// and verifies they appear correctly on both web and local.
func TestShareSyncFetchWebComments(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Seed two web comments at different lines
	seedCommentAt(t, baseURL, token, "plan.md", "web comment on step 1", 3, 3)
	seedCommentAt(t, baseURL, token, "plan.md", "web comment on step 2", 5, 5)

	// Verify web comments exist on crit-web before sync
	webComments := commentsFromAPI(t, baseURL, token)
	if len(webComments) != 2 {
		t.Fatalf("expected 2 web comments, got %d", len(webComments))
	}
	for _, wc := range webComments {
		if wc.FilePath != "plan.md" {
			t.Errorf("web comment file = %q, want plan.md", wc.FilePath)
		}
	}

	// Update content so re-share triggers a PUT
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (done)\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Re-share — should pull web comments into local .crit.json
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	cj := readCritJSON(t, dir)
	localComments := cj.Files["plan.md"].Comments
	webCount := 0
	for _, c := range localComments {
		if strings.HasPrefix(c.ID, "web-") {
			webCount++
			// Verify the bodies match what we seeded
			if c.Body != "web comment on step 1" && c.Body != "web comment on step 2" {
				t.Errorf("unexpected web comment body: %q", c.Body)
			}
		}
	}
	if webCount != 2 {
		t.Errorf("expected 2 web comments merged locally, got %d (total: %d)", webCount, len(localComments))
	}
}

// TestShareSyncFetchWebCommentsNoDuplicates verifies repeated syncs don't duplicate web comments.
func TestShareSyncFetchWebCommentsNoDuplicates(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	seedComment(t, baseURL, token, "plan.md", "web reviewer says hi")

	// Re-share twice with content changes each time
	for i := 2; i <= 3; i++ {
		content := fmt.Sprintf("# Plan\n\nContent v%d\n", i)
		if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		critShareCmd(t, binary, baseURL, dir, "plan.md")
	}

	// Count — should be exactly 1
	cj := readCritJSON(t, dir)
	webCount := 0
	for _, c := range cj.Files["plan.md"].Comments {
		if c.Body == "web reviewer says hi" {
			webCount++
		}
	}
	if webCount != 1 {
		t.Errorf("expected 1 web comment after 2 re-shares, got %d", webCount)
	}
}

// TestShareSyncMultipleFiles verifies sharing multiple files with per-file comments.
func TestShareSyncMultipleFiles(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md":  {Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "plan comment", Scope: "line", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}}},
			"notes.md": {Comments: []Comment{{ID: "c2", StartLine: 1, EndLine: 1, Body: "notes comment", Scope: "line", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}}},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md", "notes.md")
	logReview(t, output)
	token := extractToken(t, output)

	files := documentFromAPI(t, baseURL, token)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Verify comments are associated with correct files on web
	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	byFile := map[string]string{}
	for _, c := range comments {
		byFile[c.FilePath] = c.Body
	}
	if byFile["plan.md"] != "plan comment" {
		t.Errorf("plan.md comment = %q, want 'plan comment'", byFile["plan.md"])
	}
	if byFile["notes.md"] != "notes comment" {
		t.Errorf("notes.md comment = %q, want 'notes comment'", byFile["notes.md"])
	}
}

// TestShareSyncMultipleRounds verifies round progression and content updates across 3 cycles.
func TestShareSyncMultipleRounds(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	for round := 2; round <= 3; round++ {
		content := fmt.Sprintf("# Plan v%d\n", round)
		if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		out := critShareCmd(t, binary, baseURL, dir, "plan.md")
		expected := fmt.Sprintf("Updated (round %d)", round)
		if !strings.Contains(out, expected) {
			t.Errorf("round %d: expected %q, got: %s", round, expected, out)
		}
	}

	finalRound := readCritJSON(t, dir).ReviewRound
	if finalRound != 3 {
		t.Errorf("expected round 3, got %d", finalRound)
	}

	// Verify latest content on web
	files := documentFromAPI(t, baseURL, token)
	if len(files) == 0 {
		t.Fatal("no files")
	}
	if content, _ := files[0]["content"].(string); !strings.Contains(content, "Plan v3") {
		t.Errorf("expected 'Plan v3' on web, got %q", content)
	}
}

// TestShareSyncCommentWithReplies verifies comments with reply threads on web.
func TestShareSyncCommentWithReplies(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{
						ID: "c1", StartLine: 3, EndLine: 3, Body: "parent comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						Replies: []Reply{
							{ID: "r1", Body: "first reply", Author: "agent"},
							{ID: "r2", Body: "second reply", Author: "reviewer"},
						},
					},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "parent comment" {
		t.Errorf("body = %q, want 'parent comment'", comments[0].Body)
	}
	if comments[0].StartLine != 3 {
		t.Errorf("start_line = %d, want 3", comments[0].StartLine)
	}
}

// TestShareSyncUnpublish verifies the full unpublish flow clears web and local state.
func TestShareSyncUnpublish(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Verify review exists on web
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("review should exist, got %d", resp.StatusCode)
	}

	unpubOut := critUnpublishCmd(t, binary, baseURL, dir)
	if !strings.Contains(unpubOut, "unpublished") {
		t.Errorf("expected 'unpublished', got: %s", unpubOut)
	}

	// Verify review gone from web
	resp2, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after unpublish, got %d", resp2.StatusCode)
	}

	// Verify local state cleared
	cj := readCritJSON(t, dir)
	if cj.ShareURL != "" {
		t.Errorf("ShareURL should be cleared, got %q", cj.ShareURL)
	}
	if cj.DeleteToken != "" {
		t.Errorf("DeleteToken should be cleared, got %q", cj.DeleteToken)
	}
}

// TestShareSyncExport verifies the export endpoint returns correct .crit.json-compatible data.
func TestShareSyncExport(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "rc1", Body: "review comment for export", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		},
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "lc1", StartLine: 3, EndLine: 3, Body: "line comment for export", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	exportResp, err := http.Get(baseURL + "/api/export/" + token + "/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer exportResp.Body.Close()
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export returned %d", exportResp.StatusCode)
	}

	var exportBody map[string]any
	if err := json.NewDecoder(exportResp.Body).Decode(&exportBody); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"review_round", "share_url", "delete_token"} {
		if exportBody[key] == nil {
			t.Errorf("export missing %q", key)
		}
	}

	// Verify comment shape uses "author" not internal fields
	expFiles, _ := exportBody["files"].(map[string]any)
	for _, fileEntry := range expFiles {
		entry, _ := fileEntry.(map[string]any)
		comments, _ := entry["comments"].([]any)
		for _, raw := range comments {
			c, _ := raw.(map[string]any)
			if _, has := c["author_display_name"]; has {
				t.Error("export comment must not have author_display_name")
			}
			if _, has := c["author_identity"]; has {
				t.Error("export comment must not have author_identity")
			}
		}
	}
}

// TestShareSyncFetchReviewLevelWebComment verifies review-level web comments
// are pulled back into local ReviewComments.
func TestShareSyncFetchReviewLevelWebComment(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Seed a review-level comment on crit-web
	seedReviewComment(t, baseURL, token, "overall feedback from web")

	// Verify it exists on web
	webComments := commentsFromAPI(t, baseURL, token)
	reviewFound := false
	for _, c := range webComments {
		if c.Body == "overall feedback from web" && c.Scope == "review" {
			reviewFound = true
		}
	}
	if !reviewFound {
		t.Fatal("review-level comment not found on crit-web after seeding")
	}

	// Change content and re-share to trigger fetch
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nContent v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	// Verify review-level comment was merged locally
	cj := readCritJSON(t, dir)
	found := false
	for _, c := range cj.ReviewComments {
		if c.Body == "overall feedback from web" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected review-level web comment in local ReviewComments, got: %+v", cj.ReviewComments)
	}
}

// TestShareSyncFetchReviewLevelReplies verifies replies on review-level (general,
// doc-anchored) comments round-trip through the share flow. This complements
// TestShareSyncFetchReplies (line-anchored) and TestShareSyncFetchReviewLevelWebComment
// (review-level body without replies). It locks the parity gap surfaced when
// crit-web's review-level cards were missing the reply UI even though the API worked.
func TestShareSyncFetchReviewLevelReplies(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// Share a simple review.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Seed a review-level comment on crit-web and reply to it twice from the web.
	commentID := seedReviewCommentGetID(t, baseURL, token, "general feedback from web")
	seedReply(t, baseURL, token, commentID, "first review-level reply from web")
	seedReply(t, baseURL, token, commentID, "second review-level reply from web")

	// Re-share to trigger a fetch.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	// Verify the review-level comment is in local ReviewComments WITH its replies.
	cj := readCritJSON(t, dir)
	var webReview *Comment
	for i, c := range cj.ReviewComments {
		if c.Body == "general feedback from web" {
			webReview = &cj.ReviewComments[i]
			break
		}
	}
	if webReview == nil {
		t.Fatalf("review-level web comment not found in local ReviewComments, got: %+v", cj.ReviewComments)
	}
	if len(webReview.Replies) != 2 {
		t.Fatalf("expected 2 replies on review-level web comment, got %d", len(webReview.Replies))
	}
	bodies := map[string]bool{}
	for _, r := range webReview.Replies {
		bodies[r.Body] = true
		if r.Author == "" {
			t.Errorf("review-level reply %q has empty author", r.Body)
		}
	}
	if !bodies["first review-level reply from web"] {
		t.Error("missing reply 'first review-level reply from web'")
	}
	if !bodies["second review-level reply from web"] {
		t.Error("missing reply 'second review-level reply from web'")
	}

	t.Logf("Review-level reply round-trip test passed. Review: %s", extractURL(t, output))
}

// TestShareSyncFullLifecycle exercises the complete round-trip:
//
//	local comments (with replies) → share → web comments added → fetch →
//	re-share (web comments preserved) → fetch again (no duplicates)
func TestShareSyncFullLifecycle(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// --- Round 1: local review with threaded comments ---
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n\nStep 3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "rc1", Body: "overall looks good but needs detail", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
				Replies: []Reply{
					{ID: "rr1", Body: "agreed, will expand step 2", Author: "agent"},
				}},
		},
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "clarify what step 1 means", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						Replies: []Reply{
							{ID: "cr1", Body: "it means the first thing", Author: "agent"},
							{ID: "cr2", Body: "ok that makes sense", Author: "reviewer"},
						}},
					{ID: "c2", StartLine: 5, EndLine: 5, Body: "step 2 is too vague", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	// Share
	output1 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output1)
	token := extractToken(t, output1)

	// Verify all comments on web
	webComments := commentsFromAPI(t, baseURL, token)
	if len(webComments) != 3 {
		t.Fatalf("round 1: expected 3 comments on web (1 review + 2 line), got %d", len(webComments))
	}
	webBodies := map[string]bool{}
	for _, c := range webComments {
		webBodies[c.Body] = true
	}
	for _, want := range []string{"overall looks good but needs detail", "clarify what step 1 means", "step 2 is too vague"} {
		if !webBodies[want] {
			t.Errorf("round 1: missing comment %q on web", want)
		}
	}

	// --- Round 2: web reviewer adds comments ---
	seedCommentAt(t, baseURL, token, "plan.md", "web: step 3 needs acceptance criteria", 7, 7)
	seedReviewComment(t, baseURL, token, "web: overall timeline is missing")

	// Update content locally and re-share — should fetch web comments
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (clarified)\n\nStep 2 (expanded)\n\nStep 3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output2 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	if !strings.Contains(output2, "Updated") {
		t.Errorf("round 2: expected update, got: %s", output2)
	}

	// Verify web comments were pulled locally
	cj2 := readCritJSON(t, dir)
	localWebCount := 0
	for _, c := range cj2.Files["plan.md"].Comments {
		if strings.HasPrefix(c.ID, "web-") {
			localWebCount++
			if c.Body != "web: step 3 needs acceptance criteria" {
				t.Errorf("unexpected web file comment: %q", c.Body)
			}
		}
	}
	if localWebCount != 1 {
		t.Errorf("round 2: expected 1 web file comment locally, got %d", localWebCount)
	}
	localWebReviewCount := 0
	for _, c := range cj2.ReviewComments {
		if strings.HasPrefix(c.ID, "web-") {
			localWebReviewCount++
			if c.Body != "web: overall timeline is missing" {
				t.Errorf("unexpected web review comment: %q", c.Body)
			}
		}
	}
	if localWebReviewCount != 1 {
		t.Errorf("round 2: expected 1 web review comment locally, got %d", localWebReviewCount)
	}

	// Verify original web comments still exist on web (not overwritten by re-share)
	webComments2 := commentsFromAPI(t, baseURL, token)
	webBodies2 := map[string]int{}
	for _, c := range webComments2 {
		webBodies2[c.Body]++
	}
	if webBodies2["web: step 3 needs acceptance criteria"] != 1 {
		t.Errorf("round 2: web comment 'step 3 needs acceptance criteria' count = %d, want 1", webBodies2["web: step 3 needs acceptance criteria"])
	}
	if webBodies2["web: overall timeline is missing"] != 1 {
		t.Errorf("round 2: web review comment 'timeline is missing' count = %d, want 1", webBodies2["web: overall timeline is missing"])
	}

	// --- Round 3: re-share again with more local changes (no new web comments) ---
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (done)\n\nStep 2 (done)\n\nStep 3 (done)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output3 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	if !strings.Contains(output3, "Updated") {
		t.Errorf("round 3: expected update, got: %s", output3)
	}

	// Verify NO duplicate web comments locally
	cj3 := readCritJSON(t, dir)
	localWebFileCount := 0
	for _, c := range cj3.Files["plan.md"].Comments {
		if strings.HasPrefix(c.ID, "web-") {
			localWebFileCount++
		}
	}
	if localWebFileCount != 1 {
		t.Errorf("round 3: expected 1 web file comment locally (no dup), got %d", localWebFileCount)
	}
	localWebRevCount := 0
	for _, c := range cj3.ReviewComments {
		if strings.HasPrefix(c.ID, "web-") {
			localWebRevCount++
		}
	}
	if localWebRevCount != 1 {
		t.Errorf("round 3: expected 1 web review comment locally (no dup), got %d", localWebRevCount)
	}

	// Verify NO duplicates on web either
	webComments3 := commentsFromAPI(t, baseURL, token)
	webBodies3 := map[string]int{}
	for _, c := range webComments3 {
		webBodies3[c.Body]++
	}
	for body, count := range webBodies3 {
		if count > 1 {
			t.Errorf("round 3: duplicate on web: %q appears %d times", body, count)
		}
	}

	// Original local comments should still be on web
	for _, want := range []string{"clarify what step 1 means", "step 2 is too vague"} {
		if webBodies3[want] != 1 {
			t.Errorf("round 3: original comment %q count = %d, want 1", want, webBodies3[want])
		}
	}

	t.Logf("Full lifecycle passed across 3 rounds. Review: %s", extractURL(t, output1))
}

// TestShareSyncOrphanedFile verifies the full end-to-end flow for orphaned files:
// share a review with an active file and an orphaned file (with unresolved comments),
// then verify crit-web stores both files and their comments correctly.
//
// This uses shareFilesToWeb directly because orphaned files are shared via the
// browser share path (LoadShareFilesFromDisk on a live session), not the CLI
// `crit share` command (which reads files from disk — orphaned files don't exist on disk).
func TestShareSyncOrphanedFile(t *testing.T) {
	baseURL := critWebURL(t)

	// Build the share payload as LoadShareFilesFromDisk would produce it:
	// an active file with content and an orphaned file with empty content + "removed" status.
	files := []shareFile{
		{Path: "plan.md", Content: "# Plan\n\nActive content\n", Status: "modified"},
		{Path: "old-code.go", Content: "", Status: "removed"},
	}
	comments := []shareComment{
		{File: "old-code.go", StartLine: 10, EndLine: 12,
			Body: "this logic was important — where did it move?", Scope: "line"},
		{File: "old-code.go", Body: "file-level note about removal", Scope: "file"},
	}

	url, _, err := shareFilesToWeb(files, comments, baseURL, 2, "", nil, "", "")
	if err != nil {
		t.Fatalf("sharing with orphaned file failed: %v", err)
	}
	t.Logf("  → Review: %s", url)
	token := path.Base(url)

	// Verify document on crit-web has both files
	webFiles := documentFromAPI(t, baseURL, token)
	if len(webFiles) != 2 {
		t.Fatalf("expected 2 files on web, got %d", len(webFiles))
	}

	var orphanedFile, activeFile map[string]any
	for _, f := range webFiles {
		switch f["path"] {
		case "old-code.go":
			orphanedFile = f
		case "plan.md":
			activeFile = f
		}
	}

	// Verify active file
	if activeFile == nil {
		t.Fatal("active file 'plan.md' not found on web")
	}
	if s, _ := activeFile["status"].(string); s != "modified" {
		t.Errorf("active file status = %q, want 'modified'", s)
	}

	// Verify orphaned file metadata
	if orphanedFile == nil {
		t.Fatal("orphaned file 'old-code.go' not found on web")
	}
	if s, _ := orphanedFile["status"].(string); s != "removed" {
		t.Errorf("orphaned file status = %q, want 'removed'", s)
	}
	if c, _ := orphanedFile["content"].(string); c != "" {
		t.Errorf("orphaned file content should be empty, got %q", c)
	}

	// Verify comments on the orphaned file made it to crit-web
	webComments := commentsFromAPI(t, baseURL, token)

	var lineComment, fileComment *webComment
	for i, c := range webComments {
		if c.FilePath == "old-code.go" {
			switch c.Scope {
			case "line":
				lineComment = &webComments[i]
			case "file":
				fileComment = &webComments[i]
			}
		}
	}

	if lineComment == nil {
		t.Fatal("line comment on orphaned file not found on web")
	}
	if lineComment.Body != "this logic was important — where did it move?" {
		t.Errorf("line comment body = %q", lineComment.Body)
	}
	if lineComment.StartLine != 10 || lineComment.EndLine != 12 {
		t.Errorf("line comment position = %d-%d, want 10-12", lineComment.StartLine, lineComment.EndLine)
	}

	if fileComment == nil {
		t.Fatal("file-level comment on orphaned file not found on web")
	}
	if fileComment.Body != "file-level note about removal" {
		t.Errorf("file comment body = %q", fileComment.Body)
	}
	if fileComment.Scope != "file" {
		t.Errorf("file comment scope = %q, want 'file'", fileComment.Scope)
	}
}

// TestShareSyncFetchReplies verifies that replies to web-authored comments
// are fetched back into the local .crit.json when running crit fetch (via re-share).
func TestShareSyncFetchReplies(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// Share a simple review
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Seed a comment on crit-web and add two replies to it
	commentID := seedCommentGetID(t, baseURL, token, "plan.md", "needs more detail", 3, 3)
	seedReply(t, baseURL, token, commentID, "first reply from web")
	seedReply(t, baseURL, token, commentID, "second reply from web")

	// Update content to force a re-share (triggers fetch)
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	// Verify the web comment was fetched with its replies
	cj := readCritJSON(t, dir)
	var webComment *Comment
	for i, c := range cj.Files["plan.md"].Comments {
		if strings.HasPrefix(c.ID, "web-") && c.Body == "needs more detail" {
			webComment = &cj.Files["plan.md"].Comments[i]
			break
		}
	}
	if webComment == nil {
		t.Fatal("web comment 'needs more detail' not found in local .crit.json")
	}

	// This is the core assertion: replies must be present
	if len(webComment.Replies) != 2 {
		t.Fatalf("expected 2 replies on web comment, got %d", len(webComment.Replies))
	}

	replyBodies := map[string]bool{}
	for _, r := range webComment.Replies {
		replyBodies[r.Body] = true
	}
	if !replyBodies["first reply from web"] {
		t.Error("missing reply 'first reply from web'")
	}
	if !replyBodies["second reply from web"] {
		t.Error("missing reply 'second reply from web'")
	}

	// Verify reply authors are set
	for _, r := range webComment.Replies {
		if r.Author == "" {
			t.Errorf("reply %q has empty author", r.Body)
		}
	}

	t.Logf("Fetch replies test passed. Review: %s", extractURL(t, output))
}

// TestShareSyncFetchRepliesOnExistingComments verifies that when a shared comment
// (with external_id) gets replies on crit-web, those replies are fetched back.
// This tests the case where the comment already exists locally (not a new web comment).
// Note: the first share (POST) doesn't set external_id — only upsert (PUT) does.
// So we need to re-share once to set external_ids before seeding the reply.
func TestShareSyncFetchRepliesOnExistingComments(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// Share a review with a local comment
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "local comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Re-share with a content change so the upsert (PUT) sets external_id on the comment.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (v2)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	// Now the comment on crit-web has external_id "c1". Find its crit-web internal ID.
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/comments", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var full []struct {
		ID         string `json:"id"`
		ExternalID string `json:"external_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		t.Fatalf("decoding comments: %v", err)
	}
	var webID string
	for _, f := range full {
		if f.ExternalID == "c1" {
			webID = f.ID
			break
		}
	}
	if webID == "" {
		t.Fatalf("could not find crit-web ID for comment with external_id c1, got: %+v", full)
	}

	// Add a reply on crit-web to the shared comment
	seedReply(t, baseURL, token, webID, "web reply to local comment")

	// Update content and re-share again to trigger fetch with reply updates
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (v3)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")

	// Verify the local comment now has the reply
	cj := readCritJSON(t, dir)
	var localComment *Comment
	for i, c := range cj.Files["plan.md"].Comments {
		if c.ID == "c1" {
			localComment = &cj.Files["plan.md"].Comments[i]
			break
		}
	}
	if localComment == nil {
		t.Fatal("local comment c1 not found after re-share")
	}

	replyFound := false
	for _, r := range localComment.Replies {
		if r.Body == "web reply to local comment" {
			replyFound = true
			break
		}
	}
	if !replyFound {
		t.Errorf("expected reply 'web reply to local comment' on comment c1, got replies: %+v", localComment.Replies)
	}
}

// seedResolve toggles the resolved state of a comment via the test-only
// seed-resolve endpoint and returns the resolved_round from the response.
func seedResolve(t *testing.T, baseURL, token, commentID string, resolved bool) (bool, int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"resolved": resolved})
	resp, err := http.Post(
		baseURL+"/api/reviews/"+token+"/seed-resolve/"+commentID,
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("seed-resolve failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-resolve returned %d", resp.StatusCode)
	}
	var result struct {
		Resolved      bool `json:"resolved"`
		ResolvedRound int  `json:"resolved_round"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding seed-resolve response: %v", err)
	}
	return result.Resolved, result.ResolvedRound
}

// TestShareSyncResolvedRoundMapping verifies that crit-web stamps resolved_round
// with the review's current review_round on resolve, clears it on unresolve,
// and that both fields round-trip through the public API that crit reads.
func TestShareSyncResolvedRoundMapping(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "carry-forward", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	// First share: review_round = 1 in crit-web.
	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Bump to round 2 by re-sharing with a content change.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output2 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	if !strings.Contains(output2, "Updated (round 2)") {
		t.Fatalf("expected round 2, got: %s", output2)
	}

	// Web reviewer adds a fresh comment in round 2.
	commentID := seedCommentGetID(t, baseURL, token, "plan.md", "needs detail", 3, 3)

	// Unresolved → resolved_round must be absent (zero) in the API response.
	beforeComments := commentsFromAPI(t, baseURL, token)
	var before *webComment
	for i, c := range beforeComments {
		if c.Body == "needs detail" {
			before = &beforeComments[i]
			break
		}
	}
	if before == nil {
		t.Fatal("seeded comment not found in API response")
	}
	if before.Resolved || before.ResolvedRound != 0 {
		t.Errorf("pre-resolve: expected resolved=false, resolved_round=0; got resolved=%v, resolved_round=%d",
			before.Resolved, before.ResolvedRound)
	}

	// Resolve via seed endpoint → response should reflect round 2.
	resolved, round := seedResolve(t, baseURL, token, commentID, true)
	if !resolved || round != 2 {
		t.Errorf("seed-resolve(true): got resolved=%v, resolved_round=%d; want true, 2", resolved, round)
	}

	// Public GET must agree.
	afterResolve := commentsFromAPI(t, baseURL, token)
	var resolvedComment *webComment
	for i, c := range afterResolve {
		if c.Body == "needs detail" {
			resolvedComment = &afterResolve[i]
			break
		}
	}
	if resolvedComment == nil {
		t.Fatal("resolved comment not found in API response")
	}
	if !resolvedComment.Resolved || resolvedComment.ResolvedRound != 2 {
		t.Errorf("after resolve: got resolved=%v, resolved_round=%d; want true, 2",
			resolvedComment.Resolved, resolvedComment.ResolvedRound)
	}

	// Unresolve → resolved_round cleared.
	resolved, round = seedResolve(t, baseURL, token, commentID, false)
	if resolved || round != 0 {
		t.Errorf("seed-resolve(false): got resolved=%v, resolved_round=%d; want false, 0", resolved, round)
	}

	afterUnresolve := commentsFromAPI(t, baseURL, token)
	var unresolvedComment *webComment
	for i, c := range afterUnresolve {
		if c.Body == "needs detail" {
			unresolvedComment = &afterUnresolve[i]
			break
		}
	}
	if unresolvedComment == nil {
		t.Fatal("unresolved comment not found in API response")
	}
	if unresolvedComment.Resolved || unresolvedComment.ResolvedRound != 0 {
		t.Errorf("after unresolve: got resolved=%v, resolved_round=%d; want false, 0",
			unresolvedComment.Resolved, unresolvedComment.ResolvedRound)
	}
}

// --- Share-receiver popup-relay coverage (issue #50) ---
//
// These tests use httptest.NewServer as a stub crit-web rather than a live
// instance, because they exercise paths that don't depend on real persistence:
//
//   - SSO failure path: stub returns HTML on /api/reviews to simulate a
//     reverse-proxy login page intercepting the request.
//   - proxy_auth=true config plumbing through the local server isn't
//     exercised here — that's covered by server_test.go (handleConfig). These
//     tests focus on what happens at the binary boundary.
//
// What is NOT covered (by design):
//   - The real popup window auth flow — there's no browser in `go test`. The
//     popup-relay endpoints (/api/share/payload, /api/share/upsert-payload,
//     /api/comments/merge) are unit-tested in server_test.go.
//   - tokenFromHostedURL — covered by share_test.go.

// htmlStubServer returns an httptest server that responds with an HTML login
// page on every request — the canonical SSO reverse-proxy failure mode.
func htmlStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>SSO login page</body></html>"))
	}))
}

// jsonShareStub returns a stub server that mimics a successful crit-web
// /api/reviews POST: returns {url, delete_token} so `crit share` can record
// share state. Used to verify the legacy (non-popup) share path still works.
func jsonShareStub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.stub/r/stubtoken",
			"delete_token": "del-stub",
		})
	})
	return httptest.NewServer(mux)
}

// runCritCmd is a thin wrapper over exec.Command that keeps env clean (no
// CRIT_AUTH_TOKEN, no HOME inheriting global config) and returns combined
// output + error.
func runCritCmd(t *testing.T, binary, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = envWithout("CRIT_AUTH_TOKEN=", "HOME=", "CRIT_SHARE_URL=")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// seedOrg creates a test org on crit-web and adds the given user as admin.
// Returns the org slug.
func seedOrg(t *testing.T, baseURL, userID, name, slug string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"user_id": userID, "name": name, "slug": slug})
	resp, err := http.Post(baseURL+"/api/test/seed-org", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("seed-org request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-org returned %d", resp.StatusCode)
	}
	var out struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding seed-org: %v", err)
	}
	return out.Slug
}

// reviewDocFromAPI fetches /api/reviews/:token/document from crit-web.
// authToken is optional — pass "" for unauthenticated access.
// Returns the decoded JSON body (files, visibility, comment_policy).
func reviewDocFromAPI(t *testing.T, baseURL, token string, authToken ...string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token), nil)
	if err != nil {
		t.Fatalf("creating review doc request: %v", err)
	}
	if len(authToken) > 0 && authToken[0] != "" {
		req.Header.Set("Authorization", "Bearer "+authToken[0])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("review doc request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("review doc returned %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding review doc: %v", err)
	}
	return body
}

// reviewDocInaccessible verifies that /api/reviews/:token/document returns non-200
// without auth (org-scoped reviews should be inaccessible anonymously).
func reviewDocInaccessible(t *testing.T, baseURL, token string) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatalf("review doc request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("org-scoped review should not be accessible without auth, got 200")
	}
}

// critShareCmdWithArgs runs `crit share` with arbitrary extra arguments before the file list.
// Returns combined output. Fails the test on non-zero exit.
func critShareCmdWithArgs(t *testing.T, binary, baseURL, dir string, extraArgs []string, files ...string) string {
	t.Helper()
	return critShareCmdWithEnv(t, binary, baseURL, dir, extraArgs, nil, files...)
}

// critShareCmdWithEnv runs `crit share` with extra args and env vars.
func critShareCmdWithEnv(t *testing.T, binary, baseURL, dir string, extraArgs, extraEnv []string, files ...string) string {
	t.Helper()
	args := []string{"share", "--share-url", baseURL, "--output", dir}
	args = append(args, extraArgs...)
	args = append(args, files...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %s\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// critShareCmdExpectFail runs `crit share` and expects a non-zero exit code.
// Returns combined output and the error.
func critShareCmdExpectFail(t *testing.T, binary, baseURL, dir string, extraArgs, extraEnv, files []string) (string, error) {
	t.Helper()
	args := []string{"share", "--share-url", baseURL, "--output", dir}
	args = append(args, extraArgs...)
	args = append(args, files...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestShareReceiver_HTMLPostReturnsProxyAuthError verifies that when crit-web
// (or its reverse proxy) returns HTML on POST /api/reviews — the canonical
// SSO failure path — `crit share` exits non-zero with an error message
// pointing the user at proxy_auth=true.
func TestShareReceiver_HTMLPostReturnsProxyAuthError(t *testing.T) {
	binary := critBinary(t)
	ts := htmlStubServer(t)
	defer ts.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	out, err := runCritCmd(t, binary, dir,
		"share", "--share-url", ts.URL, "--output", dir, "plan.md")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success. output: %s", out)
	}
	if !strings.Contains(out, "proxy_auth") {
		t.Errorf("expected error to mention proxy_auth, got: %s", out)
	}
	if !strings.Contains(out, "popup") {
		t.Errorf("expected error to mention popup, got: %s", out)
	}
}

// TestShareReceiver_FetchHTMLReturnsProxyAuthError verifies that `crit fetch`
// against an HTML-returning stub crit-web (SSO proxy intercepting GET
// /api/reviews/:token/comments) gives the helpful proxy_auth=true hint.
func TestShareReceiver_FetchHTMLReturnsProxyAuthError(t *testing.T) {
	binary := critBinary(t)
	ts := htmlStubServer(t)
	defer ts.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-seed .crit.json with a share_url pointing at the stub so `crit
	// fetch` has somewhere to look. tokenFromHostedURL parses /r/<token>.
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		ShareURL:    ts.URL + "/r/sometoken",
		DeleteToken: "del-x",
		Files:       map[string]CritJSONFile{"plan.md": {}},
	})

	out, err := runCritCmd(t, binary, dir, "fetch", "--output", dir)
	if err == nil {
		t.Fatalf("expected non-zero exit, got success. output: %s", out)
	}
	if !strings.Contains(out, "proxy_auth") {
		t.Errorf("expected error to mention proxy_auth, got: %s", out)
	}
}

// TestShareReceiver_LegacyShareStillWorks verifies that a successful JSON
// response on POST /api/reviews — the legacy default path with no
// proxy_auth=true — produces a review URL and writes share state to
// .crit.json. Regression coverage so the popup-relay work doesn't break the
// default share flow.
func TestShareReceiver_LegacyShareStillWorks(t *testing.T) {
	binary := critBinary(t)
	ts := jsonShareStub(t)
	defer ts.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	out, err := runCritCmd(t, binary, dir,
		"share", "--share-url", ts.URL, "--output", dir, "plan.md")
	if err != nil {
		t.Fatalf("expected success, got %v. output: %s", err, out)
	}
	if !strings.Contains(out, "/r/stubtoken") {
		t.Errorf("expected output to contain stub review URL, got: %s", out)
	}

	// .crit.json should now record the share URL + delete token.
	data, err := os.ReadFile(filepath.Join(dir, ".crit.json"))
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("decode .crit.json: %v", err)
	}
	if cj.ShareURL != "https://crit.stub/r/stubtoken" {
		t.Errorf("share_url = %q, want https://crit.stub/r/stubtoken", cj.ShareURL)
	}
	if cj.DeleteToken != "del-stub" {
		t.Errorf("delete_token = %q, want del-stub", cj.DeleteToken)
	}
}

// TestShareSyncOrgShare verifies that sharing with --org sets the org field on crit-web.
func TestShareSyncOrgShare(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, userID, _ := seedUser(t, baseURL, "Org Sharer")
	slug := seedOrg(t, baseURL, userID, "Share Test Org", fmt.Sprintf("share-test-%d", time.Now().UnixNano()))
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	output := critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug}, authEnv, "readme.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Org-scoped review should be accessible with auth
	doc := reviewDocFromAPI(t, baseURL, token, authToken)
	if doc == nil {
		t.Fatal("expected review document, got nil")
	}

	// Org-scoped review should NOT be accessible without auth
	reviewDocInaccessible(t, baseURL, token)
}

// TestShareSyncOrgVisibility verifies that --visibility overrides the org default.
func TestShareSyncOrgVisibility(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, userID, _ := seedUser(t, baseURL, "Vis Sharer")
	slug := seedOrg(t, baseURL, userID, "Vis Test Org", fmt.Sprintf("vis-test-%d", time.Now().UnixNano()))
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	output := critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug, "--visibility", "unlisted"}, authEnv, "readme.md")
	logReview(t, output)
	token := extractToken(t, output)

	doc := reviewDocFromAPI(t, baseURL, token, authToken)
	visibility, _ := doc["visibility"].(string)
	if visibility != "unlisted" {
		t.Errorf("expected visibility 'unlisted', got %q", visibility)
	}

	// Org-scoped review should NOT be accessible without auth
	reviewDocInaccessible(t, baseURL, token)
}

// TestShareSyncPersonalNoOrg verifies that sharing without --org produces a personal review with no org.
func TestShareSyncPersonalNoOrg(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	output := critShareCmd(t, binary, baseURL, dir, "readme.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Personal review should be accessible without auth (not org-scoped)
	doc := reviewDocFromAPI(t, baseURL, token)
	if doc == nil {
		t.Fatal("personal review should be accessible without auth")
	}
}

// TestShareSyncOrgReshare verifies that re-sharing with --org preserves the same URL (upsert) and org.
func TestShareSyncOrgReshare(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, userID, _ := seedUser(t, baseURL, "Reshare Sharer")
	slug := seedOrg(t, baseURL, userID, "Reshare Test Org", fmt.Sprintf("reshare-test-%d", time.Now().UnixNano()))
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	// First share with org
	output1 := critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug}, authEnv, "readme.md")
	logReview(t, output1)
	token1 := extractToken(t, output1)

	// Update content and re-share with same org
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output2 := critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug}, authEnv, "readme.md")
	token2 := extractToken(t, output2)

	// Same token means same URL (upsert, not new review)
	if token1 != token2 {
		t.Errorf("re-share should use same token: %s vs %s", token1, token2)
	}

	// Verify review still accessible with auth after re-share (org persists)
	doc := reviewDocFromAPI(t, baseURL, token2, authToken)

	// Verify updated content is on web
	docFiles, _ := doc["files"].([]any)
	if len(docFiles) == 0 {
		t.Fatal("no files in document after re-share")
	}
	firstFile, _ := docFiles[0].(map[string]any)
	if content, _ := firstFile["content"].(string); !strings.Contains(content, "World (revised)") {
		t.Errorf("expected revised content on web, got %q", content)
	}
}

// TestShareSyncOrgNonMemberError verifies that sharing with a non-existent org fails.
func TestShareSyncOrgNonMemberError(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, _, _ := seedUser(t, baseURL, "NonMember Sharer")
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	output, err := critShareCmdExpectFail(t, binary, baseURL, dir, []string{"--org", "nonexistent-org-xyz"}, authEnv, []string{"readme.md"})
	if err == nil {
		t.Fatalf("expected crit share to fail for non-existent org, but it succeeded.\nOutput: %s", output)
	}
	t.Logf("Expected failure output: %s", output)
	t.Logf("Expected failure error: %v", err)
}

// TestShareSyncOrgUnpublish verifies that unpublishing an org-scoped review works.
// The delete_token should be sufficient authorization — no bearer token needed.
func TestShareSyncOrgUnpublish(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, userID, _ := seedUser(t, baseURL, "Unpub Sharer")
	slug := seedOrg(t, baseURL, userID, "Unpub Test Org", fmt.Sprintf("unpub-test-%d", time.Now().UnixNano()))
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Unpublish Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	// Share with org
	output := critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug}, authEnv, "readme.md")
	logReview(t, output)
	token := extractToken(t, output)

	// Verify it's accessible with auth
	doc := reviewDocFromAPI(t, baseURL, token, authToken)
	if doc == nil {
		t.Fatal("review should be accessible after share")
	}

	// Unpublish (uses delete_token from the review file, no bearer token needed)
	unpubCmd := exec.Command(binary, "unpublish", "--share-url", baseURL, "--output", dir)
	unpubCmd.Dir = dir
	unpubOut, err := unpubCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit unpublish failed: %s\n%s", err, unpubOut)
	}
	t.Logf("Unpublish output: %s", unpubOut)

	// Verify the review is gone (should be 404; crit-web currently returns
	// 401 for deleted org-scoped reviews because the org access check runs
	// before the nil check — either is acceptable as "not accessible").
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected review to be inaccessible after unpublish, got 200")
	}
}

// TestShareSyncOrgPersistence verifies that org info is persisted in the review
// file after sharing with --org, so it survives session restarts.
func TestShareSyncOrgPersistence(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()
	authToken, userID, _ := seedUser(t, baseURL, "Persist Sharer")
	slug := seedOrg(t, baseURL, userID, "Persist Test Org", fmt.Sprintf("persist-test-%d", time.Now().UnixNano()))
	authEnv := []string{"CRIT_AUTH_TOKEN=" + authToken}

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Persist Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"readme.md": {}}})

	// Share with org and visibility
	critShareCmdWithEnv(t, binary, baseURL, dir, []string{"--org", slug, "--visibility", "organization"}, authEnv, "readme.md")

	// Read the review file and verify org fields are persisted
	reviewPath := filepath.Join(dir, ".crit", "review.json")
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("reading review file: %v", err)
	}

	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("parsing review file: %v", err)
	}

	if cj.ShareOrg != slug {
		t.Errorf("expected share_org=%q in review file, got %q", slug, cj.ShareOrg)
	}
	if cj.ShareVisibility != "organization" {
		t.Errorf("expected share_visibility=%q in review file, got %q", "organization", cj.ShareVisibility)
	}
	if cj.ShareURL == "" {
		t.Error("expected share_url to be set in review file")
	}
}
