//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
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
	data, _ := json.MarshalIndent(initialCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

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
	localData, err := os.ReadFile(filepath.Join(dir, ".crit.json"))
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

// writeTestCritJSON writes a CritJSON to .crit.json in dir.
// NOTE: readCritJSON is defined in github_test.go and shared across test files.
func writeTestCritJSON(t *testing.T, dir string, cj CritJSON) {
	t.Helper()
	d, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".crit.json"), d, 0644); err != nil {
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

	url, _, err := shareFilesToWeb(files, comments, baseURL, 2, "")
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
