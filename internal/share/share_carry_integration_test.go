//go:build integration

package share

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const (
	planV1 = "# Plan\n\nStep 1\n\nStep 2\n"
	planV2 = "# Plan\n\nNew line A\nNew line B\nStep 1\n\nStep 2\n"
)

func TestShareSyncCarryOmitUnchanged(t *testing.T) {
	dir, token, deleteToken := sharePlan(t, planV1, lineComment("c1", 3, "Expand this"))

	changed, round := putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", planV1)
	if changed {
		t.Fatal("unchanged file was reported as changed")
	}
	if round != 1 {
		t.Fatalf("review_round = %d, want 1", round)
	}
	got := commentByBody(t, commentsFromAPI(t, critWebURL(t), token), "Expand this")
	if got.StartLine != 3 || got.EndLine != 3 {
		t.Fatalf("comment moved on an unchanged file: %+v", got)
	}
	if len(commentsFromAPI(t, critWebURL(t), token)) != 1 {
		t.Fatalf("comment count changed: %+v", commentsFromAPI(t, critWebURL(t), token))
	}
	_ = dir
}

func TestShareSyncCarryFollowsInsertedLines(t *testing.T) {
	_, token, deleteToken := sharePlan(t, planV1,
		lineComment("c1", 3, "Expand this"),
		lineComment("c2", 5, "And this"),
	)

	changed, round := putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", planV2)
	if !changed || round != 2 {
		t.Fatalf("changed=%v round=%d, want true, 2", changed, round)
	}
	comments := commentsFromAPI(t, critWebURL(t), token)
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	first := commentByBody(t, comments, "Expand this")
	second := commentByBody(t, comments, "And this")
	if first.StartLine != 5 || first.EndLine != 5 || first.Anchor != "Step 1" || first.Drifted {
		t.Fatalf("first comment = %+v", first)
	}
	if second.StartLine != 7 || second.EndLine != 7 || second.Anchor != "Step 2" || second.Drifted {
		t.Fatalf("second comment = %+v", second)
	}
}

func TestShareSyncCarryMarksDrifted(t *testing.T) {
	_, token, deleteToken := sharePlan(t, planV1, lineComment("c1", 3, "Expand this"))

	putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", "# Plan\n\nSomething else\n")
	got := commentByBody(t, commentsFromAPI(t, critWebURL(t), token), "Expand this")
	if !got.Drifted || got.Anchor != "Step 1" || got.StartLine != 2 || got.EndLine != 2 {
		t.Fatalf("drifted comment = %+v", got)
	}
}

func TestShareSyncCarryKeepsInPlaceEdit(t *testing.T) {
	original := "# Plan\n\nKeep this sentence intact please\n"
	edited := "# Plan\n\nKeep this sentence intact please, and more\n"
	_, token, deleteToken := sharePlan(t, original, lineComment("c1", 3, "still here"))

	putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", edited)
	got := commentByBody(t, commentsFromAPI(t, critWebURL(t), token), "still here")
	if got.Drifted || got.StartLine != 3 || got.Anchor != "Keep this sentence intact please" {
		t.Fatalf("in-place comment = %+v", got)
	}
}

func TestShareSyncCarrySkipsFileAndReviewComments(t *testing.T) {
	_, token, deleteToken := sharePlanWithReview(t, planV1,
		lineComment("c1", 3, "Expand this"),
		Comment{
			ID: "file-1", Body: "whole file", Scope: "file",
			CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		},
		Comment{
			ID: "review-1", Body: "about the review", Scope: "review",
			CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		},
	)

	putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", planV2)
	comments := commentsFromAPI(t, critWebURL(t), token)
	line := commentByBody(t, comments, "Expand this")
	file := commentByBody(t, comments, "whole file")
	review := commentByBody(t, comments, "about the review")
	if line.StartLine != 5 || line.Drifted {
		t.Fatalf("line comment = %+v", line)
	}
	if file.Scope != "file" || file.Drifted || file.StartLine != 0 {
		t.Fatalf("file comment = %+v", file)
	}
	if review.Scope != "review" || review.Drifted || review.StartLine != 0 {
		t.Fatalf("review comment = %+v", review)
	}
}

func TestShareSyncCarryExplicitListStaysWhereSent(t *testing.T) {
	baseURL := critWebURL(t)
	dir, token, _ := sharePlan(t, planV1, lineComment("c1", 3, "Expand this"))
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(planV2), 0o644); err != nil {
		t.Fatal(err)
	}

	critShareCmd(t, critBinary(t), baseURL, dir, "plan.md")
	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	got := comments[0]
	if got.StartLine != 3 || got.EndLine != 3 || got.Drifted {
		t.Fatalf("explicit list was remapped again: %+v", got)
	}
}

func TestShareSyncCarryExplicitEmptyClears(t *testing.T) {
	// crit share pulls remote comments before it uploads, so emptying the
	// local file and re-sharing does not delete them. An explicit empty
	// list on the update does.
	baseURL := critWebURL(t)
	_, token, deleteToken := sharePlan(t, planV1, lineComment("c1", 3, "Expand this"))

	payload, err := json.Marshal(map[string]any{
		"delete_token": deleteToken,
		"files":        []map[string]string{{"path": "plan.md", "content": planV2}},
		"comments":     []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/reviews/"+token, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}
	if comments := commentsFromAPI(t, baseURL, token); len(comments) != 0 {
		t.Fatalf("empty comment list left %d comments", len(comments))
	}
}

func TestShareSyncCarryRoundTripThroughShare(t *testing.T) {
	baseURL := critWebURL(t)
	dir, token, deleteToken := sharePlan(t, planV1, lineComment("c1", 3, "Expand this"))
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(planV2), 0o644); err != nil {
		t.Fatal(err)
	}
	putFilesOnly(t, baseURL, token, deleteToken, "plan.md", planV2)

	critShareCmd(t, critBinary(t), baseURL, dir, "plan.md")

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1: %+v", len(comments), comments)
	}
	got := comments[0]
	if got.StartLine != 5 || got.EndLine != 5 || got.Anchor != "Step 1" || got.Drifted {
		t.Fatalf("shared comment after round trip = %+v", got)
	}
	local := readCritJSON(t, dir).Files["plan.md"].Comments
	if len(local) != 1 || local[0].ID != "c1" || local[0].StartLine != 5 || local[0].Anchor != "Step 1" || local[0].Drifted {
		t.Fatalf("local comment after round trip = %+v", local)
	}
}

func TestShareSyncCarryMissingEndLineDoesNotFail(t *testing.T) {
	comment := lineComment("c1", 3, "Expand this")
	comment.EndLine = 0
	_, token, deleteToken := sharePlan(t, planV1, comment)

	changed, _ := putFilesOnly(t, critWebURL(t), token, deleteToken, "plan.md", planV2)
	if !changed {
		t.Fatal("expected the file update to succeed")
	}
	got := commentByBody(t, commentsFromAPI(t, critWebURL(t), token), "Expand this")
	if got.Body != "Expand this" {
		t.Fatalf("comment = %+v", got)
	}
}

func sharePlan(t *testing.T, content string, comments ...Comment) (dir, token, deleteToken string) {
	t.Helper()
	return sharePlanWithReview(t, content, comments...)
}

func sharePlanWithReview(t *testing.T, content string, comments ...Comment) (dir, token, deleteToken string) {
	t.Helper()
	baseURL := critWebURL(t)
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fileComments := []Comment{}
	var reviewComments []Comment
	for _, c := range comments {
		if c.Scope == "review" {
			reviewComments = append(reviewComments, c)
			continue
		}
		fileComments = append(fileComments, c)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound:    1,
		ReviewComments: reviewComments,
		Files:          map[string]CritJSONFile{"plan.md": {Comments: fileComments}},
	})
	output := critShareCmd(t, critBinary(t), baseURL, dir, "plan.md")
	logReview(t, output)
	token = extractToken(t, output)
	deleteToken = readCritJSON(t, dir).DeleteToken
	if deleteToken == "" {
		t.Fatal("share did not record a delete token")
	}
	return dir, token, deleteToken
}

func lineComment(id string, line int, body string) Comment {
	return Comment{
		ID: id, StartLine: line, EndLine: line, Body: body, Scope: "line",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
}

func putFilesOnly(t *testing.T, baseURL, token, deleteToken, path, content string) (changed bool, round int) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"delete_token": deleteToken,
		"files":        []map[string]string{{"path": path, "content": content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/reviews/"+token, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Changed     bool `json:"changed"`
		ReviewRound int  `json:"review_round"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decoding PUT response: %v\n%s", err, body)
	}
	return result.Changed, result.ReviewRound
}

func commentByBody(t *testing.T, comments []webComment, body string) webComment {
	t.Helper()
	for _, c := range comments {
		if c.Body == body {
			return c
		}
	}
	t.Fatalf("comment %q not found in %+v", body, comments)
	return webComment{}
}
