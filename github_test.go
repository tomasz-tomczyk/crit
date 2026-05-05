package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Avoid shelling out to `gh` during tests. Tests that want to exercise
// display-name resolution should pre-populate userNameCache directly.
func init() {
	fetchGHUserName = func(login string) (string, error) { return "", nil }
}

func TestMergeGHComments_BasicConversion(t *testing.T) {
	comments := []ghComment{
		{
			ID:   1,
			Path: "main.go",
			Line: 10,
			Side: "RIGHT",
			Body: "Fix this bug",
			User: struct {
				Login string `json:"login"`
			}{Login: "reviewer1"},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
		{
			ID:        2,
			Path:      "main.go",
			Line:      25,
			StartLine: 20,
			Side:      "RIGHT",
			Body:      "This whole block needs refactoring",
			User: struct {
				Login string `json:"login"`
			}{Login: "reviewer2"},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
	}

	cj := CritJSON{Branch: "feature-branch", BaseRef: "abc123", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if cj.Branch != "feature-branch" {
		t.Errorf("Branch = %q, want %q", cj.Branch, "feature-branch")
	}
	if cj.BaseRef != "abc123" {
		t.Errorf("BaseRef = %q, want %q", cj.BaseRef, "abc123")
	}

	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("expected main.go in files")
	}
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}

	// Single-line comment: StartLine should equal Line
	c1 := cf.Comments[0]
	if c1.StartLine != 10 || c1.EndLine != 10 {
		t.Errorf("c1 lines = %d-%d, want 10-10", c1.StartLine, c1.EndLine)
	}
	if c1.Body != "Fix this bug" {
		t.Errorf("c1 body = %q, want %q", c1.Body, "Fix this bug")
	}
	if c1.Author != "reviewer1" {
		t.Errorf("c1 author = %q, want %q", c1.Author, "reviewer1")
	}

	// Multi-line comment: StartLine from GitHub
	c2 := cf.Comments[1]
	if c2.StartLine != 20 || c2.EndLine != 25 {
		t.Errorf("c2 lines = %d-%d, want 20-25", c2.StartLine, c2.EndLine)
	}
	if c2.Author != "reviewer2" {
		t.Errorf("c2 author = %q, want %q", c2.Author, "reviewer2")
	}
}

func TestGHVersionSupportsSlurp(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "older than 2.48.0", version: "gh version 2.47.0 (2025-01-01)\n", want: false},
		{name: "exactly 2.48.0", version: "gh version 2.48.0 (2025-01-01)\n", want: true},
		{name: "newer patch release", version: "gh version 2.48.1 (2025-01-01)\n", want: true},
		{name: "newer minor release", version: "gh version 2.49.0 (2025-01-01)\n", want: true},
		{name: "prefixed version", version: "gh version v2.48.0 (2025-01-01)\n", want: true},
		{name: "prerelease", version: "gh version 2.48.0-rc1 (2025-01-01)\n", want: true},
		{name: "invalid output", version: "not a gh version", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ghVersionSupportsSlurp(tt.version); got != tt.want {
				t.Fatalf("ghVersionSupportsSlurp(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestMergeGHComments_FiltersLeftSide(t *testing.T) {
	comments := []ghComment{
		{ID: 1, Path: "old.go", Line: 5, Side: "LEFT", Body: "old code comment"},
		{ID: 2, Path: "new.go", Line: 5, Side: "RIGHT", Body: "new code comment"},
	}

	cj := CritJSON{Branch: "branch", BaseRef: "base", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if _, ok := cj.Files["old.go"]; ok {
		t.Error("LEFT-side comment should be filtered out")
	}
	if _, ok := cj.Files["new.go"]; !ok {
		t.Error("RIGHT-side comment should be included")
	}
}

func TestMergeGHComments_SkipsNoLineComments(t *testing.T) {
	comments := []ghComment{
		{ID: 1, Path: "main.go", Line: 0, Side: "RIGHT", Body: "PR-level comment"},
	}

	cj := CritJSON{Branch: "branch", BaseRef: "base", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if len(cj.Files) != 0 {
		t.Error("comments with Line=0 should be skipped")
	}
}

func TestBuildReviewPayload_EmptyMessageByDefault(t *testing.T) {
	comments := []map[string]any{{"path": "main.go", "line": 1, "side": "RIGHT", "body": "fix"}}
	data, err := buildReviewPayload(comments, "", "COMMENT")
	if err != nil {
		t.Fatalf("buildReviewPayload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["body"] != "" {
		t.Errorf("body = %q, want empty string", payload["body"])
	}
	if payload["event"] != "COMMENT" {
		t.Errorf("event = %q, want COMMENT", payload["event"])
	}
}

func TestBuildReviewPayload_CustomMessage(t *testing.T) {
	comments := []map[string]any{{"path": "main.go", "line": 1, "side": "RIGHT", "body": "fix"}}
	data, err := buildReviewPayload(comments, "Round 2 review", "COMMENT")
	if err != nil {
		t.Fatalf("buildReviewPayload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["body"] != "Round 2 review" {
		t.Errorf("body = %q, want %q", payload["body"], "Round 2 review")
	}
}

func TestBuildReviewPayload_ApproveEvent(t *testing.T) {
	comments := []map[string]any{{"path": "main.go", "line": 1, "side": "RIGHT", "body": "lgtm"}}
	data, err := buildReviewPayload(comments, "", "APPROVE")
	if err != nil {
		t.Fatalf("buildReviewPayload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["event"] != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", payload["event"])
	}
}

func TestBuildReviewPayload_RequestChangesEvent(t *testing.T) {
	comments := []map[string]any{{"path": "main.go", "line": 1, "side": "RIGHT", "body": "fix this"}}
	data, err := buildReviewPayload(comments, "Needs work", "REQUEST_CHANGES")
	if err != nil {
		t.Fatalf("buildReviewPayload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["event"] != "REQUEST_CHANGES" {
		t.Errorf("event = %q, want REQUEST_CHANGES", payload["event"])
	}
	if payload["body"] != "Needs work" {
		t.Errorf("body = %q, want %q", payload["body"], "Needs work")
	}
}

func TestCritJSONToGHComments_BasicConversion(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"auth.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 10, EndLine: 10, Body: "single line"},
					{ID: "c2", StartLine: 20, EndLine: 25, Body: "multi line"},
					{ID: "c3", StartLine: 30, EndLine: 30, Body: "resolved", Resolved: true},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments (resolved filtered), got %d", len(comments))
	}

	c1 := comments[0]
	if c1["path"] != "auth.go" || c1["line"] != 10 || c1["side"] != "RIGHT" {
		t.Errorf("c1 = %v", c1)
	}
	// Single-line: no start_line field
	if _, ok := c1["start_line"]; ok {
		t.Error("single-line comment should not have start_line")
	}

	c2 := comments[1]
	if c2["start_line"] != 20 || c2["line"] != 25 {
		t.Errorf("c2 = %v", c2)
	}
}

func TestMergeGHComments_PreservesExistingComments(t *testing.T) {
	cj := CritJSON{
		Branch: "feature", BaseRef: "abc", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 5, EndLine: 5, Body: "existing local comment", CreatedAt: "2025-01-01T00:00:00Z"},
				},
			},
		},
	}

	ghComments := []ghComment{
		{ID: 100, Path: "main.go", Line: 20, Side: "RIGHT", Body: "new from GH",
			User: struct {
				Login string `json:"login"`
			}{Login: "reviewer"},
			CreatedAt: "2025-01-02T00:00:00Z"},
	}

	added := mergeGHComments(&cj, ghComments)
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments (1 existing + 1 new), got %d", len(cf.Comments))
	}
	if cf.Comments[0].Body != "existing local comment" {
		t.Errorf("existing comment body = %q", cf.Comments[0].Body)
	}
	if cf.Comments[1].Body != "new from GH" {
		t.Errorf("new comment body = %q", cf.Comments[1].Body)
	}
	if cf.Comments[1].Author != "reviewer" {
		t.Errorf("new comment author = %q", cf.Comments[1].Author)
	}
	if !strings.HasPrefix(cf.Comments[1].ID, "c_") || len(cf.Comments[1].ID) != 8 {
		t.Errorf("new comment ID = %q, want c_ prefix + 6 hex chars", cf.Comments[1].ID)
	}
}

func TestCritJSONToGHComments_SkipsResolved(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "done", Resolved: true},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestCritJSONToGHComments_BodyNotPrefixedWithAuthor(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 10, EndLine: 10, Body: "fix this", Author: "reviewer1"},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0]["body"] != "fix this" {
		t.Errorf("body = %q, want %q (should not include author prefix)", comments[0]["body"], "fix this")
	}
}

func TestMergeGHComments_DeduplicatesOnRepeatedPull(t *testing.T) {
	comments := []ghComment{
		{ID: 1, Path: "main.go", Line: 10, Side: "RIGHT", Body: "Fix this",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"},
			CreatedAt: "2025-01-01T00:00:00Z"},
	}

	cj := CritJSON{Branch: "b", BaseRef: "r", ReviewRound: 1, Files: make(map[string]CritJSONFile)}

	// First pull
	added := mergeGHComments(&cj, comments)
	if added != 1 {
		t.Fatalf("first pull: added = %d, want 1", added)
	}

	// Second pull with same comments — should be deduplicated
	added = mergeGHComments(&cj, comments)
	if added != 0 {
		t.Fatalf("second pull: added = %d, want 0 (duplicate)", added)
	}

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment after dedup, got %d", len(cf.Comments))
	}
}

func TestMergeGHComments_Threading(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{}}
	ghComments := []ghComment{
		{ID: 101, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Too complex", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 102, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Agreed, split it", User: struct {
				Login string `json:"login"`
			}{"author"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 101},
		{ID: 103, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Will do", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:02:00Z",
			InReplyToID: 101},
	}

	added := mergeGHComments(cj, ghComments)

	// Should produce 1 root comment with 2 replies
	cf := cj.Files["server.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].GitHubID != 101 {
		t.Errorf("root GitHubID = %d, want 101", cf.Comments[0].GitHubID)
	}
	if len(cf.Comments[0].Replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(cf.Comments[0].Replies))
	}
	if cf.Comments[0].Replies[0].GitHubID != 102 {
		t.Errorf("reply 1 GitHubID = %d, want 102", cf.Comments[0].Replies[0].GitHubID)
	}
	if cf.Comments[0].Replies[1].GitHubID != 103 {
		t.Errorf("reply 2 GitHubID = %d, want 103", cf.Comments[0].Replies[1].GitHubID)
	}
	if added != 3 {
		t.Errorf("added = %d, want 3", added)
	}
}

func TestMergeGHComments_ThreadDedup(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{}}
	ghComments := []ghComment{
		{ID: 101, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Fix this", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 102, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Done", User: struct {
				Login string `json:"login"`
			}{"author"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 101},
	}

	mergeGHComments(cj, ghComments)          // first pull
	added := mergeGHComments(cj, ghComments) // second pull (should dedup)

	cf := cj.Files["server.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment after dedup, got %d", len(cf.Comments))
	}
	if len(cf.Comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply after dedup, got %d", len(cf.Comments[0].Replies))
	}
	if added != 0 {
		t.Errorf("added on second pull = %d, want 0", added)
	}
}

func TestMergeGHComments_NewReplyOnExistingRoot(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{}}
	// First pull: root + 1 reply
	ghComments1 := []ghComment{
		{ID: 101, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Fix this", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 102, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Done", User: struct {
				Login string `json:"login"`
			}{"author"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 101},
	}
	mergeGHComments(cj, ghComments1)

	// Second pull: same root + old reply + new reply
	ghComments2 := []ghComment{
		{ID: 101, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Fix this", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 102, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Done", User: struct {
				Login string `json:"login"`
			}{"author"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 101},
		{ID: 103, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Thanks!", User: struct {
				Login string `json:"login"`
			}{"reviewer"}, CreatedAt: "2025-01-01T00:02:00Z",
			InReplyToID: 101},
	}
	added := mergeGHComments(cj, ghComments2)

	cf := cj.Files["server.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if len(cf.Comments[0].Replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(cf.Comments[0].Replies))
	}
	if added != 1 {
		t.Errorf("added = %d, want 1 (only the new reply)", added)
	}
}

func TestMergeGHComments_OrphanReply(t *testing.T) {
	// Pre-populate cj with a root comment from a previous pull
	cj := &CritJSON{Files: map[string]CritJSONFile{
		"server.go": {
			Status: "modified",
			Comments: []Comment{
				{ID: "c1", StartLine: 42, EndLine: 42, Body: "Fix this",
					Author: "reviewer", GitHubID: 101, CreatedAt: "2025-01-01T00:00:00Z"},
			},
		},
	}}

	// Pull only the reply (root is not in the ghComments list)
	ghComments := []ghComment{
		{ID: 102, Path: "server.go", Line: 42, Side: "RIGHT",
			Body: "Done", User: struct {
				Login string `json:"login"`
			}{"author"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 101},
	}
	added := mergeGHComments(cj, ghComments)

	cf := cj.Files["server.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if len(cf.Comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(cf.Comments[0].Replies))
	}
	if cf.Comments[0].Replies[0].GitHubID != 102 {
		t.Errorf("reply GitHubID = %d, want 102", cf.Comments[0].Replies[0].GitHubID)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
}

func TestMergeGHComments_FlatCommentsStillWork(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{}}
	ghComments := []ghComment{
		{ID: 201, Path: "main.go", Line: 10, Side: "RIGHT",
			Body: "Fix this bug", User: struct {
				Login string `json:"login"`
			}{"reviewer1"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 202, Path: "main.go", Line: 25, StartLine: 20, Side: "RIGHT",
			Body: "Refactor this", User: struct {
				Login string `json:"login"`
			}{"reviewer2"}, CreatedAt: "2025-01-01T00:00:00Z"},
	}

	added := mergeGHComments(cj, ghComments)

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}
	if cf.Comments[0].GitHubID != 201 {
		t.Errorf("c1 GitHubID = %d, want 201", cf.Comments[0].GitHubID)
	}
	if cf.Comments[1].GitHubID != 202 {
		t.Errorf("c2 GitHubID = %d, want 202", cf.Comments[1].GitHubID)
	}
	if len(cf.Comments[0].Replies) != 0 {
		t.Errorf("c1 should have no replies, got %d", len(cf.Comments[0].Replies))
	}
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}
}

func TestMergeGHComments_ReplySortedByCreatedAt(t *testing.T) {
	cj := &CritJSON{Files: map[string]CritJSONFile{}}
	// Replies intentionally out of order
	ghComments := []ghComment{
		{ID: 301, Path: "util.go", Line: 5, Side: "RIGHT",
			Body: "Root", User: struct {
				Login string `json:"login"`
			}{"alice"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 303, Path: "util.go", Line: 5, Side: "RIGHT",
			Body: "Third", User: struct {
				Login string `json:"login"`
			}{"alice"}, CreatedAt: "2025-01-01T00:03:00Z",
			InReplyToID: 301},
		{ID: 302, Path: "util.go", Line: 5, Side: "RIGHT",
			Body: "Second", User: struct {
				Login string `json:"login"`
			}{"bob"}, CreatedAt: "2025-01-01T00:01:00Z",
			InReplyToID: 301},
	}

	mergeGHComments(cj, ghComments)

	cf := cj.Files["util.go"]
	if len(cf.Comments[0].Replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(cf.Comments[0].Replies))
	}
	if cf.Comments[0].Replies[0].Body != "Second" {
		t.Errorf("first reply body = %q, want 'Second'", cf.Comments[0].Replies[0].Body)
	}
	if cf.Comments[0].Replies[1].Body != "Third" {
		t.Errorf("second reply body = %q, want 'Third'", cf.Comments[0].Replies[1].Body)
	}
}

func TestCritJSONToGHComments_WithReplies(t *testing.T) {
	// Verify root comments with replies are still pushed as top-level comments
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"server.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 42, EndLine: 42, Body: "Fix this",
						Replies: []Reply{{ID: "c1-r1", Body: "Done", Author: "agent"}}},
				},
			},
		},
	}
	comments := critJSONToGHComments(cj)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	// The reply should NOT be in the top-level comments list
}

func TestCollectNewRepliesForPush(t *testing.T) {
	cf := CritJSONFile{
		Comments: []Comment{
			{ID: "c1", GitHubID: 101, StartLine: 42, EndLine: 42, Body: "Fix this",
				Replies: []Reply{
					{ID: "c1-r1", GitHubID: 201, Body: "Already on GH"},
					{ID: "c1-r2", GitHubID: 0, Body: "New reply to push", Author: "agent"},
				}},
		},
	}

	replies := collectNewRepliesForPush(cf)
	if len(replies) != 1 {
		t.Fatalf("expected 1 new reply, got %d", len(replies))
	}
	if replies[0].Body != "New reply to push" {
		t.Errorf("reply body = %q", replies[0].Body)
	}
	if replies[0].ParentGHID != 101 {
		t.Errorf("parent GHID = %d, want 101", replies[0].ParentGHID)
	}
}

func TestCollectNewRepliesForPush_NoGitHubRoot(t *testing.T) {
	// Local-only comments (no GitHubID) should not produce replies
	cf := CritJSONFile{
		Comments: []Comment{
			{ID: "c1", GitHubID: 0, StartLine: 42, EndLine: 42, Body: "Local comment",
				Replies: []Reply{
					{ID: "c1-r1", GitHubID: 0, Body: "Local reply"},
				}},
		},
	}

	replies := collectNewRepliesForPush(cf)
	if len(replies) != 0 {
		t.Fatalf("expected 0 replies for local-only comment, got %d", len(replies))
	}
}

// TestCollectNewRepliesForPush_ParentSources verifies that local-only replies
// are collected regardless of how their parent acquired its github_id —
// whether the parent was imported via `crit pull` or pushed by us in an
// earlier `crit push`. This pins the fix for issue #442.
func TestCollectNewRepliesForPush_ParentSources(t *testing.T) {
	cases := []struct {
		name        string
		cf          CritJSONFile
		wantReplies int
	}{
		{
			name: "parent imported via pull",
			cf: CritJSONFile{
				Comments: []Comment{{
					ID: "c1", GitHubID: 555, StartLine: 1, EndLine: 1, Body: "imported",
					Replies: []Reply{{ID: "c1-r1", GitHubID: 0, Body: "ack"}},
				}},
			},
			wantReplies: 1,
		},
		{
			name: "parent pushed by us previously",
			cf: CritJSONFile{
				Comments: []Comment{{
					ID: "c1", GitHubID: 777, StartLine: 1, EndLine: 1, Body: "ours",
					Replies: []Reply{{ID: "c1-r1", GitHubID: 0, Body: "follow-up"}},
				}},
			},
			wantReplies: 1,
		},
		{
			name: "reply already pushed — skipped",
			cf: CritJSONFile{
				Comments: []Comment{{
					ID: "c1", GitHubID: 555, StartLine: 1, EndLine: 1, Body: "imported",
					Replies: []Reply{{ID: "c1-r1", GitHubID: 999, Body: "synced"}},
				}},
			},
			wantReplies: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectNewRepliesForPush(tc.cf)
			if len(got) != tc.wantReplies {
				t.Fatalf("got %d replies, want %d: %+v", len(got), tc.wantReplies, got)
			}
		})
	}
}

func TestAddCommentToCritJSON_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSONScoped("../../../etc/passwd", 1, 1, "bad", "", "", "", inheritedScope{})
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "must be relative and within the repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddCommentToCritJSON_RejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSONScoped("/etc/passwd", 1, 1, "bad", "", "", "", inheritedScope{})
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestAddCommentToCritJSON_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()

	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSONScoped("main.go", 10, 15, "Fix this bug", "", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	data, err := os.ReadFile(dir + "/.crit/review.json")
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("expected main.go in files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if !strings.HasPrefix(cf.Comments[0].ID, "c_") || len(cf.Comments[0].ID) != 8 {
		t.Errorf("ID = %q, want c_ prefix + 6 hex chars", cf.Comments[0].ID)
	}
	if cf.Comments[0].StartLine != 10 || cf.Comments[0].EndLine != 15 {
		t.Errorf("lines = %d-%d, want 10-15", cf.Comments[0].StartLine, cf.Comments[0].EndLine)
	}
	if cf.Comments[0].Body != "Fix this bug" {
		t.Errorf("Body = %q", cf.Comments[0].Body)
	}
}

func TestAddCommentToCritJSON_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := addCommentToCritJSONScoped("main.go", 1, 1, "First", "", "", dir, inheritedScope{}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := addCommentToCritJSONScoped("main.go", 20, 20, "Second", "", "", dir, inheritedScope{}); err != nil {
		t.Fatalf("second add: %v", err)
	}

	data, _ := os.ReadFile(dir + "/.crit/review.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}
	if cf.Comments[0].ID == cf.Comments[1].ID {
		t.Errorf("comment IDs should be unique, both = %q", cf.Comments[0].ID)
	}
}

func TestAddCommentToCritJSON_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	addCommentToCritJSONScoped("main.go", 1, 1, "Comment on main", "", "", dir, inheritedScope{})
	addCommentToCritJSONScoped("auth.go", 5, 10, "Comment on auth", "", "", dir, inheritedScope{})

	data, _ := os.ReadFile(dir + "/.crit/review.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	if len(cj.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(cj.Files))
	}
	if _, ok := cj.Files["main.go"]; !ok {
		t.Error("missing main.go")
	}
	if _, ok := cj.Files["auth.go"]; !ok {
		t.Error("missing auth.go")
	}
}

func TestAddCommentToCritJSON_FileMode_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSONScoped("main.go", 5, 5, "File mode comment", "", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	data, err := os.ReadFile(dir + "/.crit/review.json")
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("expected main.go in files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Body != "File mode comment" {
		t.Errorf("body = %q", cf.Comments[0].Body)
	}
}

func TestAddCommentToCritJSON_FileMode_PathRelativeToCWD(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Path should be stored as given (relative to CWD), not resolved to anything else
	addCommentToCritJSONScoped("src/auth.go", 10, 10, "comment", "", "", dir, inheritedScope{})

	data, _ := os.ReadFile(dir + "/.crit/review.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	if _, ok := cj.Files["src/auth.go"]; !ok {
		t.Error("expected path stored as src/auth.go")
	}
}

func TestAddCommentToCritJSON_OutputDir(t *testing.T) {
	repoDir := t.TempDir()
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	outputDir := t.TempDir() // separate from repo

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	if err := addCommentToCritJSONScoped("main.go", 1, 1, "custom output dir", "", "", outputDir, inheritedScope{}); err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	// Should NOT be in repo root
	if _, err := os.Stat(repoDir + "/.crit.json"); err == nil {
		t.Error(".crit.json should not be written to repo root when --output is set")
	}

	// Should be in outputDir
	data, err := os.ReadFile(outputDir + "/.crit/review.json")
	if err != nil {
		t.Fatalf("expected .crit.json in outputDir: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if _, ok := cj.Files["main.go"]; !ok {
		t.Error("expected main.go comment in outputDir/.crit.json")
	}
}

// TestAddCommentToCritJSON_RespectsBaseBranchConfig verifies that when no .crit.json
// exists yet, addCommentToCritJSON reads base_branch from .crit.config.json rather
// than falling back to auto-detected default branch.
func TestAddCommentToCritJSON_RespectsBaseBranchConfig(t *testing.T) {
	dir := t.TempDir()

	// Reset DefaultBranch cache so auto-detection is fresh
	defaultBranchOnce = sync.Once{}
	defaultBranchOverride = ""
	defer func() {
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}
	}()

	// Init a git repo with user config so commits work
	runCmd := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@t.com",
		)
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	runCmd("git", "init", "-b", "main", dir)
	runCmd("git", "-C", dir, "commit", "--allow-empty", "-m", "initial")

	// Create a "base" branch with a commit
	runCmd("git", "-C", dir, "checkout", "-b", "base")
	writeFile(t, dir+"/base.go", "package main\n")
	runCmd("git", "-C", dir, "add", "base.go")
	runCmd("git", "-C", dir, "commit", "-m", "base commit")

	// Create a "feature" branch off "base"
	runCmd("git", "-C", dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/feature.go", "package main\n")
	runCmd("git", "-C", dir, "add", "feature.go")
	runCmd("git", "-C", dir, "commit", "-m", "feature commit")

	// Write config declaring "base" as the base branch
	writeFile(t, dir+"/.crit.config.json", `{"base_branch": "base"}`)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := addCommentToCritJSONScoped("feature.go", 1, 1, "test comment", "", "", dir, inheritedScope{}); err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	data, err := os.ReadFile(dir + "/.crit/review.json")
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// BaseRef must be set — proves MergeBase was called against "base", not auto-detected default
	if cj.BaseRef == "" {
		t.Error("BaseRef should be non-empty when base_branch is set in config")
	}
}

func TestAddReplyToCritJSON(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"test.md": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(filepath.Join(dir, ".crit", "review.json")), data, 0644)

	err := addReplyToCritJSON("c1", "Done, fixed it", "", "agent", false, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	var result CritJSON
	json.Unmarshal(data, &result)

	comments := result.Files["test.md"].Comments
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
	if comments[0].Replies[0].Body != "Done, fixed it" {
		t.Errorf("reply body = %q", comments[0].Replies[0].Body)
	}
	if comments[0].Resolved {
		t.Error("comment should not be resolved without --resolve")
	}
}

func TestAddReplyToCritJSON_WithResolve(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"test.md": {
				Status:   "modified",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix"}},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(filepath.Join(dir, ".crit", "review.json")), data, 0644)

	err := addReplyToCritJSON("c1", "Split the function", "agent", "", true, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	var result CritJSON
	json.Unmarshal(data, &result)

	if !result.Files["test.md"].Comments[0].Resolved {
		t.Error("comment should be resolved with --resolve")
	}
}

func TestAddReplyToCritJSON_NotFound(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"test.md": {
				Status:   "modified",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix"}},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(filepath.Join(dir, ".crit", "review.json")), data, 0644)

	err := addReplyToCritJSON("c99", "reply", "agent", "", false, dir, "")
	if err == nil {
		t.Fatal("expected error for missing comment")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestAddReplyToCritJSON_FallbackByCommentID(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Create the reviews directory with a review file containing the target comment.
	reviewDir := filepath.Join(home, ".crit", "reviews")
	os.MkdirAll(reviewDir, 0755)
	targetCJ := CritJSON{
		Branch:      "feat",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c_target", StartLine: 10, EndLine: 10, Body: "Fix this", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z"},
				},
			},
		},
	}
	targetData, _ := json.MarshalIndent(targetCJ, "", "  ")
	os.WriteFile(filepath.Join(reviewDir, "correct.json"), targetData, 0644)

	// Create a local outputDir with a different review file that does NOT contain the comment.
	localDir := t.TempDir()
	localCJ := CritJSON{Branch: "other", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	localData, _ := json.MarshalIndent(localCJ, "", "  ")
	os.WriteFile(mustMkdirAll(filepath.Join(localDir, ".crit", "review.json")), localData, 0644)

	// Reply should fall back to the review file containing c_target.
	err := addReplyToCritJSON("c_target", "Done, fixed", "", "agent", false, localDir, "")
	if err != nil {
		t.Fatalf("expected fallback to find comment, got error: %v", err)
	}

	// Verify reply was written to the correct review file.
	// addReplyToCritJSON migrated the seeded flat correct.json into the v4
	// folder layout, so the canonical read is correct.json/review.json.
	data, _ := os.ReadFile(filepath.Join(reviewDir, "correct.json", "review.json"))
	var result CritJSON
	json.Unmarshal(data, &result)
	comments := result.Files["main.go"].Comments
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply in fallback file, got %d", len(comments[0].Replies))
	}
	if comments[0].Replies[0].Body != "Done, fixed" {
		t.Errorf("reply body = %q", comments[0].Replies[0].Body)
	}

	// Verify the local file was NOT modified.
	localData2, _ := os.ReadFile(filepath.Join(localDir, ".crit", "review.json"))
	var localResult CritJSON
	json.Unmarshal(localData2, &localResult)
	if len(localResult.Files) != 0 {
		t.Error("local review file should not have been modified")
	}
}

func TestFindReviewFileByCommentID_NotInAnyFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	reviewDir := filepath.Join(home, ".crit", "reviews")
	os.MkdirAll(reviewDir, 0755)

	cj := CritJSON{Files: map[string]CritJSONFile{
		"test.md": {Comments: []Comment{{ID: "c_abc", StartLine: 1, EndLine: 1, Body: "x"}}},
	}}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(reviewDir, "review1.json"), data, 0644)

	_, err := findReviewFileByCommentID("c_nonexistent", "/excluded.json")
	if err == nil {
		t.Fatal("expected error for comment not in any file")
	}
	if !strings.Contains(err.Error(), "not found in any") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestFindReviewFileByCommentID_InReviewComments(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	reviewDir := filepath.Join(home, ".crit", "reviews")
	os.MkdirAll(reviewDir, 0755)

	cj := CritJSON{
		ReviewComments: []Comment{{ID: "r_review1", Body: "General feedback"}},
		Files:          map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(reviewDir, "review1.json"), data, 0644)

	path, err := findReviewFileByCommentID("r_review1", "/excluded.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "review1.json" {
		t.Errorf("expected review1.json, got %s", filepath.Base(path))
	}
}

func TestFindReviewFileByCommentID_FolderForm(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	reviewDir := filepath.Join(home, ".crit", "reviews")
	folder := filepath.Join(reviewDir, "key1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}

	cj := CritJSON{
		ReviewComments: []Comment{{ID: "r_folder1", Body: "From folder"}},
		Files:          map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(folder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findReviewFileByCommentID("r_folder1", "/excluded.json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != folder {
		t.Errorf("got %q, want folder identity %q", got, folder)
	}
}

func TestCritJSONToGHComments_SkipsAlreadyPushed(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "c1", EndLine: 5, Body: "new", GitHubID: 0},
					{ID: "c2", EndLine: 10, Body: "already pushed", GitHubID: 123},
				},
			},
		},
	}
	comments := critJSONToGHComments(cj)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment (skipping pushed), got %d", len(comments))
	}
	if comments[0]["body"] != "new" {
		t.Errorf("wrong comment kept: %v", comments[0])
	}
}

func TestUpdateCritJSONWithGitHubIDs(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")

	cj := CritJSON{
		Branch: "main", BaseRef: "abc123", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 5, Body: "fix this", GitHubID: 0},
					{ID: "c2", StartLine: 10, EndLine: 10, Body: "also fix", GitHubID: 0},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(reviewPathsFor(critPath).Review), data, 0644)

	idMap := map[string]int64{
		"main.go:5":  111,
		"main.go:10": 222,
	}

	err := updateCritJSONWithGitHubIDs(critPath, idMap, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, _ := os.ReadFile(reviewPathsFor(critPath).Review)
	var got CritJSON
	json.Unmarshal(result, &got)

	comments := got.Files["main.go"].Comments
	if comments[0].GitHubID != 111 {
		t.Errorf("c1: expected GitHubID=111, got %d", comments[0].GitHubID)
	}
	if comments[1].GitHubID != 222 {
		t.Errorf("c2: expected GitHubID=222, got %d", comments[1].GitHubID)
	}
}

func TestUpdateCritJSONWithGitHubIDs_Replies(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")

	cj := CritJSON{
		Branch: "main", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{
						ID: "c1", EndLine: 5, Body: "fix", GitHubID: 100,
						Replies: []Reply{
							{ID: "c1-r1", Body: "Done, fixed it", GitHubID: 0},
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(reviewPathsFor(critPath).Review), data, 0644)

	replyIDs := map[replyKey]int64{
		{ParentGHID: 100, BodyPrefix: "Done, fixed it"}: 201,
	}

	err := updateCritJSONWithGitHubIDs(critPath, nil, replyIDs)
	if err != nil {
		t.Fatal(err)
	}

	result, _ := os.ReadFile(reviewPathsFor(critPath).Review)
	var got CritJSON
	json.Unmarshal(result, &got)

	reply := got.Files["main.go"].Comments[0].Replies[0]
	if reply.GitHubID != 201 {
		t.Errorf("reply: expected GitHubID=201, got %d", reply.GitHubID)
	}
}

// readCritJSON is a test helper that reads and parses .crit.json from the given directory.
func readCritJSON(t *testing.T, dir string) CritJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	if err != nil {
		t.Fatalf("reading .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("parsing .crit.json: %v", err)
	}
	return cj
}

func TestBulkAddCommentsToCritJSON_MixedCommentsAndReplies(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	entries := []BulkCommentEntry{
		{File: "main.go", Line: 1, Body: "Add package doc"},
		{File: "main.go", Line: 3, EndLine: 4, Body: "Extract to function"},
	}

	err := bulkAddCommentsToCritJSONScoped(entries, "TestBot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: 2 comments on main.go
	cj := readCritJSON(t, dir)
	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("main.go not in .crit.json")
	}
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}
	if cf.Comments[0].StartLine != 1 || cf.Comments[0].EndLine != 1 {
		t.Errorf("c1: expected line 1, got %d-%d", cf.Comments[0].StartLine, cf.Comments[0].EndLine)
	}
	if cf.Comments[1].StartLine != 3 || cf.Comments[1].EndLine != 4 {
		t.Errorf("c2: expected lines 3-4, got %d-%d", cf.Comments[1].StartLine, cf.Comments[1].EndLine)
	}
	if cf.Comments[0].Author != "TestBot" {
		t.Errorf("expected author TestBot, got %q", cf.Comments[0].Author)
	}

	// Now add a reply to the first comment (use its actual random ID)
	firstCommentID := cf.Comments[0].ID
	replyEntries := []BulkCommentEntry{
		{ReplyTo: firstCommentID, Body: "Done — added godoc comment", Resolve: true},
	}
	err = bulkAddCommentsToCritJSONScoped(replyEntries, "TestBot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error on reply: %v", err)
	}

	cj = readCritJSON(t, dir)
	cf = cj.Files["main.go"]
	if len(cf.Comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply on c1, got %d", len(cf.Comments[0].Replies))
	}
	if !cf.Comments[0].Resolved {
		t.Error("c1 should be resolved")
	}
}

func TestBulkAddCommentsToCritJSON_EmptyBody(t *testing.T) {
	dir := initTestRepo(t)
	entries := []BulkCommentEntry{{File: "main.go", Line: 1, Body: ""}}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "body is required") {
		t.Errorf("expected body required error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_MissingFile(t *testing.T) {
	dir := initTestRepo(t)
	entries := []BulkCommentEntry{{Line: 1, Body: "test"}}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "file is required") {
		t.Errorf("expected file required error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_InvalidLine(t *testing.T) {
	dir := initTestRepo(t)
	entries := []BulkCommentEntry{{File: "main.go", Line: 0, Body: "test"}}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "line must be > 0") {
		t.Errorf("expected line error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_PathTraversal(t *testing.T) {
	dir := initTestRepo(t)
	entries := []BulkCommentEntry{{File: "../etc/passwd", Line: 1, Body: "test"}}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Errorf("expected path traversal error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_EmptyEntries(t *testing.T) {
	dir := initTestRepo(t)
	err := bulkAddCommentsToCritJSONScoped(nil, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "no comment entries") {
		t.Errorf("expected empty entries error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_ReplyNotFound(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir()) // isolate from any real ~/.crit/reviews
	entries := []BulkCommentEntry{{ReplyTo: "c99", Body: "reply"}}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

// writeAltReviewFile writes a CritJSON to ~/.crit/reviews/<name>.json under
// the (presumed test-stubbed) HOME, returning its path. Used to set up a sibling
// review file that the bulk fallback should discover via findReviewFileByCommentID.
func writeAltReviewFile(t *testing.T, name string, cj CritJSON) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".crit", "reviews")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBulkAddCommentsToCritJSON_RedirectsRepliesToAltFile(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	altPath := writeAltReviewFile(t, "alt_review", CritJSON{
		Files: map[string]CritJSONFile{
			"spec.md": {Comments: []Comment{{ID: "c_spec1", StartLine: 1, EndLine: 1, Body: "original"}}},
		},
	})

	entries := []BulkCommentEntry{
		{ReplyTo: "c_spec1", Body: "addressed"},
		{Body: "general note on the spec"}, // new review-level comment, rides along
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Primary (cwd-resolved) file should be untouched / empty.
	primaryPath := filepath.Join(dir, ".crit")
	if data, err := os.ReadFile(primaryPath); err == nil {
		var cj CritJSON
		json.Unmarshal(data, &cj)
		for _, cf := range cj.Files {
			if len(cf.Comments) > 0 {
				t.Errorf("primary file should not have been modified, found %d comments", len(cf.Comments))
			}
		}
		if len(cj.ReviewComments) > 0 {
			t.Errorf("primary file should not have review comments, got %d", len(cj.ReviewComments))
		}
	}

	// Alt file should have the reply on c_spec1 plus the new review comment.
	// addReplyToCritJSON migrated alt_review.json into the v4 folder layout.
	altData, err := os.ReadFile(reviewPathsFor(altPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var alt CritJSON
	if err := json.Unmarshal(altData, &alt); err != nil {
		t.Fatal(err)
	}
	cf := alt.Files["spec.md"]
	if len(cf.Comments) != 1 || len(cf.Comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply on c_spec1, got comments=%d replies=%d",
			len(cf.Comments), len(cf.Comments[0].Replies))
	}
	if cf.Comments[0].Replies[0].Body != "addressed" {
		t.Errorf("reply body = %q", cf.Comments[0].Replies[0].Body)
	}
	if len(alt.ReviewComments) != 1 || alt.ReviewComments[0].Body != "general note on the spec" {
		t.Errorf("expected new review comment in alt file, got %+v", alt.ReviewComments)
	}
}

func TestBulkAddCommentsToCritJSON_RejectsSplitTargets(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	// Seed the primary (cwd) review file with a comment.
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	if err := bulkAddCommentsToCritJSONScoped(
		[]BulkCommentEntry{{File: "main.go", Line: 1, Body: "primary comment"}},
		"Bot", "", dir, inheritedScope{},
	); err != nil {
		t.Fatal(err)
	}
	primaryCJ := readCritJSON(t, dir)
	primaryCommentID := primaryCJ.Files["main.go"].Comments[0].ID

	// Alt review file with a different comment ID.
	writeAltReviewFile(t, "alt_review", CritJSON{
		Files: map[string]CritJSONFile{
			"spec.md": {Comments: []Comment{{ID: "c_alt1", StartLine: 1, EndLine: 1, Body: "alt"}}},
		},
	})

	// Bulk that mixes a primary-file reply with an alt-file reply → rejected.
	entries := []BulkCommentEntry{
		{ReplyTo: primaryCommentID, Body: "reply to primary"},
		{ReplyTo: "c_alt1", Body: "reply to alt"},
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil {
		t.Fatal("expected split-target error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple review files") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_RejectsRepliesAcrossTwoAltFiles(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	writeAltReviewFile(t, "alt_one", CritJSON{
		Files: map[string]CritJSONFile{
			"a.md": {Comments: []Comment{{ID: "c_one", StartLine: 1, EndLine: 1, Body: "a"}}},
		},
	})
	writeAltReviewFile(t, "alt_two", CritJSON{
		Files: map[string]CritJSONFile{
			"b.md": {Comments: []Comment{{ID: "c_two", StartLine: 1, EndLine: 1, Body: "b"}}},
		},
	})

	entries := []BulkCommentEntry{
		{ReplyTo: "c_one", Body: "x"},
		{ReplyTo: "c_two", Body: "y"},
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "multiple review files") {
		t.Fatalf("expected multi-file error, got: %v", err)
	}
}

func TestBulkAddCommentsToCritJSON_PerEntryAuthor(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	entries := []BulkCommentEntry{
		{File: "main.go", Line: 1, Body: "from global author"},
		{File: "main.go", Line: 1, Body: "from custom author", Author: "CustomBot"},
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "GlobalBot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatal(err)
	}
	cj := readCritJSON(t, dir)
	if cj.Files["main.go"].Comments[0].Author != "GlobalBot" {
		t.Errorf("expected GlobalBot, got %q", cj.Files["main.go"].Comments[0].Author)
	}
	if cj.Files["main.go"].Comments[1].Author != "CustomBot" {
		t.Errorf("expected CustomBot, got %q", cj.Files["main.go"].Comments[1].Author)
	}
}

func TestBulkAddCommentsToCritJSON_MultipleFiles(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package b\n")
	entries := []BulkCommentEntry{
		{File: "a.go", Line: 1, Body: "comment on a"},
		{File: "b.go", Line: 1, Body: "comment on b"},
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatal(err)
	}
	cj := readCritJSON(t, dir)
	if len(cj.Files["a.go"].Comments) != 1 {
		t.Errorf("expected 1 comment on a.go, got %d", len(cj.Files["a.go"].Comments))
	}
	if len(cj.Files["b.go"].Comments) != 1 {
		t.Errorf("expected 1 comment on b.go, got %d", len(cj.Files["b.go"].Comments))
	}
}

func TestBuildReviewPayload_ApproveNoComments(t *testing.T) {
	data, err := buildReviewPayload(nil, "Looks good!", "APPROVE")
	if err != nil {
		t.Fatalf("buildReviewPayload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["event"] != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", payload["event"])
	}
	if payload["body"] != "Looks good!" {
		t.Errorf("body = %q, want %q", payload["body"], "Looks good!")
	}
	comments, ok := payload["comments"]
	if ok && comments != nil {
		arr, isArr := comments.([]any)
		if isArr && len(arr) != 0 {
			t.Errorf("expected nil or empty comments, got %d", len(arr))
		}
	}
}

func TestBulkAddCommentsToCritJSON_EndLineDefaultsToLine(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)
	writeFile(t, filepath.Join(dir, "main.go"), "package main\nline2\nline3\nline4\nline5\n")
	entries := []BulkCommentEntry{
		{File: "main.go", Line: 2, Body: "single line - no end_line"},
		{File: "main.go", Line: 3, EndLine: 5, Body: "explicit range"},
	}
	err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{})
	if err != nil {
		t.Fatal(err)
	}
	cj := readCritJSON(t, dir)
	cf := cj.Files["main.go"]
	// When EndLine is omitted (0), it should default to Line
	if cf.Comments[0].StartLine != 2 || cf.Comments[0].EndLine != 2 {
		t.Errorf("expected line 2-2, got %d-%d", cf.Comments[0].StartLine, cf.Comments[0].EndLine)
	}
	// When EndLine is explicit, it should be preserved
	if cf.Comments[1].StartLine != 3 || cf.Comments[1].EndLine != 5 {
		t.Errorf("expected lines 3-5, got %d-%d", cf.Comments[1].StartLine, cf.Comments[1].EndLine)
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"short ASCII", "hello", 10, "hello"},
		{"exact ASCII", "hello", 5, "hello"},
		{"truncate ASCII", "hello world", 5, "hello"},
		{"empty", "", 5, ""},
		{"zero limit", "hello", 0, ""},
		{"emoji preserved", "Hello 🌍🌎🌏", 8, "Hello 🌍🌎"},
		{"CJK preserved", "日本語テスト", 3, "日本語"},
		{"no mid-rune split", "café", 4, "café"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// TestCreateGHReview_IDMapping tests the zip-by-position logic that maps
// GitHub review comment IDs back to the input comments. The GitHub API
// returns comments in the same order as the input, so the mapping relies
// on index alignment. This test verifies the mapping works correctly,
// including when response has fewer or more items than input.
func TestCreateGHReview_IDMapping(t *testing.T) {
	t.Run("zip maps IDs by position", func(t *testing.T) {
		// Simulate the mapping logic from createGHReview without shelling out to gh.
		// The core logic: for i, rc := range reviewComments { idMap[key(comments[i])] = rc.ID }
		comments := []map[string]any{
			{"path": "auth.go", "line": 10, "side": "RIGHT", "body": "fix auth"},
			{"path": "server.go", "line": 42, "side": "RIGHT", "body": "refactor"},
			{"path": "auth.go", "line": 30, "side": "RIGHT", "body": "add test"},
		}

		// Simulated GitHub response: same order, different IDs
		reviewComments := []struct{ ID int64 }{
			{ID: 1001},
			{ID: 1002},
			{ID: 1003},
		}

		idMap := make(map[string]int64)
		for i, rc := range reviewComments {
			if i < len(comments) {
				path, _ := comments[i]["path"].(string)
				line, _ := comments[i]["line"].(int)
				key := fmt.Sprintf("%s:%d", path, line)
				idMap[key] = rc.ID
			}
		}

		expected := map[string]int64{
			"auth.go:10":   1001,
			"server.go:42": 1002,
			"auth.go:30":   1003,
		}
		for k, v := range expected {
			if idMap[k] != v {
				t.Errorf("idMap[%q] = %d, want %d", k, idMap[k], v)
			}
		}
	})

	t.Run("fewer response items than input", func(t *testing.T) {
		comments := []map[string]any{
			{"path": "a.go", "line": 1, "side": "RIGHT", "body": "fix"},
			{"path": "b.go", "line": 2, "side": "RIGHT", "body": "fix"},
			{"path": "c.go", "line": 3, "side": "RIGHT", "body": "fix"},
		}
		// GitHub only returned 2 comments (partial failure)
		reviewComments := []struct{ ID int64 }{
			{ID: 2001},
			{ID: 2002},
		}

		idMap := make(map[string]int64)
		for i, rc := range reviewComments {
			if i < len(comments) {
				path, _ := comments[i]["path"].(string)
				line, _ := comments[i]["line"].(int)
				key := fmt.Sprintf("%s:%d", path, line)
				idMap[key] = rc.ID
			}
		}

		if idMap["a.go:1"] != 2001 {
			t.Errorf("a.go:1 = %d, want 2001", idMap["a.go:1"])
		}
		if idMap["b.go:2"] != 2002 {
			t.Errorf("b.go:2 = %d, want 2002", idMap["b.go:2"])
		}
		if _, ok := idMap["c.go:3"]; ok {
			t.Error("c.go:3 should not be in map (no response for it)")
		}
	})

	t.Run("more response items than input (should not panic)", func(t *testing.T) {
		comments := []map[string]any{
			{"path": "a.go", "line": 1, "side": "RIGHT", "body": "fix"},
		}
		// Extra response items should be safely ignored
		reviewComments := []struct{ ID int64 }{
			{ID: 3001},
			{ID: 3002}, // extra
		}

		idMap := make(map[string]int64)
		for i, rc := range reviewComments {
			if i < len(comments) {
				path, _ := comments[i]["path"].(string)
				line, _ := comments[i]["line"].(int)
				key := fmt.Sprintf("%s:%d", path, line)
				idMap[key] = rc.ID
			}
		}

		if idMap["a.go:1"] != 3001 {
			t.Errorf("a.go:1 = %d, want 3001", idMap["a.go:1"])
		}
		if len(idMap) != 1 {
			t.Errorf("expected 1 entry, got %d", len(idMap))
		}
	})

	t.Run("duplicate path:line overwrites with last match", func(t *testing.T) {
		// Two comments on the same file:line — the second should win
		comments := []map[string]any{
			{"path": "auth.go", "line": 10, "side": "RIGHT", "body": "first"},
			{"path": "auth.go", "line": 10, "side": "RIGHT", "body": "second"},
		}
		reviewComments := []struct{ ID int64 }{
			{ID: 4001},
			{ID: 4002},
		}

		idMap := make(map[string]int64)
		for i, rc := range reviewComments {
			if i < len(comments) {
				path, _ := comments[i]["path"].(string)
				line, _ := comments[i]["line"].(int)
				key := fmt.Sprintf("%s:%d", path, line)
				idMap[key] = rc.ID
			}
		}

		// Last one wins because same key
		if idMap["auth.go:10"] != 4002 {
			t.Errorf("auth.go:10 = %d, want 4002 (last match wins)", idMap["auth.go:10"])
		}
	})
}

// TestUpdateCritJSONWithGitHubIDs_ReplyMapping tests the replyKey-based mapping
// that matches pushed replies back to their .crit.json entries.
func TestUpdateCritJSONWithGitHubIDs_ReplyMapping(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")

	cj := CritJSON{
		Branch: "feature", BaseRef: "abc", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"server.go": {
				Status: "modified",
				Comments: []Comment{
					{
						ID: "c1", StartLine: 42, EndLine: 42, Body: "Fix this",
						GitHubID: 101,
						Replies: []Reply{
							{ID: "c1-r1", Body: "Done, fixed the auth check", GitHubID: 0},
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(reviewPathsFor(critPath).Review), append(data, '\n'), 0644)

	commentIDs := map[string]int64{} // no new root comments
	replyIDs := map[replyKey]int64{
		{ParentGHID: 101, BodyPrefix: truncateStr("Done, fixed the auth check", 60)}: 5001,
	}

	if err := updateCritJSONWithGitHubIDs(critPath, commentIDs, replyIDs); err != nil {
		t.Fatalf("updateCritJSONWithGitHubIDs: %v", err)
	}

	// Re-read and verify
	data, _ = os.ReadFile(reviewPathsFor(critPath).Review)
	var result CritJSON
	json.Unmarshal(data, &result)

	cf := result.Files["server.go"]
	if cf.Comments[0].Replies[0].GitHubID != 5001 {
		t.Errorf("reply GitHubID = %d, want 5001", cf.Comments[0].Replies[0].GitHubID)
	}
}

func TestParseLineSpec(t *testing.T) {
	tests := []struct {
		spec      string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{"5", 5, 5, false},
		{"10-20", 10, 20, false},
		{"1-1", 1, 1, false},
		{"abc", 0, 0, true},
		{"1-abc", 0, 0, true},
		{"abc-5", 0, 0, true},
	}
	for _, tc := range tests {
		start, end, err := parseLineSpec(tc.spec)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseLineSpec(%q): expected error", tc.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLineSpec(%q): unexpected error: %v", tc.spec, err)
			continue
		}
		if start != tc.wantStart || end != tc.wantEnd {
			t.Errorf("parseLineSpec(%q) = %d,%d; want %d,%d", tc.spec, start, end, tc.wantStart, tc.wantEnd)
		}
	}
}

func TestBulkCommentEntry_UnmarshalJSON_IntLine(t *testing.T) {
	data := `{"file": "main.go", "line": 42, "body": "fix"}`
	var e BulkCommentEntry
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		t.Fatal(err)
	}
	if e.Line != 42 {
		t.Errorf("Line = %d, want 42", e.Line)
	}
	if e.LineSpec != "" {
		t.Errorf("LineSpec = %q, want empty", e.LineSpec)
	}
}

func TestBulkCommentEntry_UnmarshalJSON_StringLine(t *testing.T) {
	data := `{"file": "main.go", "line": "10-20", "body": "fix"}`
	var e BulkCommentEntry
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		t.Fatal(err)
	}
	if e.LineSpec != "10-20" {
		t.Errorf("LineSpec = %q, want 10-20", e.LineSpec)
	}
	if e.Line != 0 {
		t.Errorf("Line = %d, want 0", e.Line)
	}
}

func TestBulkCommentEntry_UnmarshalJSON_NoLine(t *testing.T) {
	data := `{"file": "main.go", "body": "file-level note", "scope": "file"}`
	var e BulkCommentEntry
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		t.Fatal(err)
	}
	if e.Line != 0 {
		t.Errorf("Line = %d, want 0", e.Line)
	}
	if e.LineSpec != "" {
		t.Errorf("LineSpec = %q, want empty", e.LineSpec)
	}
	if e.Scope != "file" {
		t.Errorf("Scope = %q, want file", e.Scope)
	}
}

func TestBulkCommentEntry_UnmarshalJSON_InvalidLineType(t *testing.T) {
	data := `{"file": "main.go", "line": true, "body": "fix"}`
	var e BulkCommentEntry
	err := json.Unmarshal([]byte(data), &e)
	if err == nil {
		t.Fatal("expected error for non-int/non-string line, got nil")
	}
	if !strings.Contains(err.Error(), "line must be int or string") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAppendReply_ToReviewComment(t *testing.T) {
	cj := &CritJSON{
		ReviewComments: []Comment{
			{ID: "r0", Body: "general note", Scope: "review"},
		},
		Files: make(map[string]CritJSONFile),
	}

	err := appendReply(cj, "r0", "done, addressed", "", "agent", false, "")
	if err != nil {
		t.Fatalf("appendReply to review comment: %v", err)
	}
	if len(cj.ReviewComments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(cj.ReviewComments[0].Replies))
	}
	if cj.ReviewComments[0].Replies[0].Body != "done, addressed" {
		t.Errorf("reply body = %q", cj.ReviewComments[0].Replies[0].Body)
	}
}

func TestAppendReply_ToReviewCommentWithResolve(t *testing.T) {
	cj := &CritJSON{
		ReviewComments: []Comment{
			{ID: "r0", Body: "needs fixing", Scope: "review"},
		},
		Files: make(map[string]CritJSONFile),
	}

	err := appendReply(cj, "r0", "fixed", "agent", "", true, "")
	if err != nil {
		t.Fatalf("appendReply: %v", err)
	}
	if !cj.ReviewComments[0].Resolved {
		t.Error("expected review comment to be resolved after reply with resolve=true")
	}
}

func TestAppendReply_NotFound(t *testing.T) {
	cj := &CritJSON{
		Files: make(map[string]CritJSONFile),
	}
	err := appendReply(cj, "c99", "reply", "agent", "", false, "")
	if err == nil {
		t.Fatal("expected error for nonexistent comment")
	}
}

func TestAppendReviewComment(t *testing.T) {
	cj := &CritJSON{Files: make(map[string]CritJSONFile)}

	appendReviewCommentScoped(cj, "general observation", "reviewer", "", inheritedScope{})

	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(cj.ReviewComments))
	}
	if cj.ReviewComments[0].Body != "general observation" {
		t.Errorf("body = %q", cj.ReviewComments[0].Body)
	}
	if cj.ReviewComments[0].Author != "reviewer" {
		t.Errorf("author = %q", cj.ReviewComments[0].Author)
	}
	if cj.ReviewComments[0].Scope != "review" {
		t.Errorf("scope = %q, want review", cj.ReviewComments[0].Scope)
	}
	if !strings.HasPrefix(cj.ReviewComments[0].ID, "r_") || len(cj.ReviewComments[0].ID) != 8 {
		t.Errorf("ID = %q, want r_ prefix + 6 hex chars", cj.ReviewComments[0].ID)
	}

	// Add another
	appendReviewCommentScoped(cj, "second note", "reviewer", "", inheritedScope{})
	if !strings.HasPrefix(cj.ReviewComments[1].ID, "r_") || len(cj.ReviewComments[1].ID) != 8 {
		t.Errorf("second ID = %q, want r_ prefix + 6 hex chars", cj.ReviewComments[1].ID)
	}
	if cj.ReviewComments[0].ID == cj.ReviewComments[1].ID {
		t.Errorf("review comment IDs should be unique, both = %q", cj.ReviewComments[0].ID)
	}
}

func TestAppendFileComment(t *testing.T) {
	cj := &CritJSON{Files: make(map[string]CritJSONFile)}

	appendFileCommentScoped(cj, "server.go", "needs restructuring", "reviewer", "", inheritedScope{})

	cf, ok := cj.Files["server.go"]
	if !ok {
		t.Fatal("expected server.go in files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Scope != "file" {
		t.Errorf("scope = %q, want file", cf.Comments[0].Scope)
	}
	if cf.Comments[0].StartLine != 0 || cf.Comments[0].EndLine != 0 {
		t.Errorf("expected zero lines for file-level comment, got %d-%d", cf.Comments[0].StartLine, cf.Comments[0].EndLine)
	}
}

func TestAppendComment_IDIncrementsGlobally(t *testing.T) {
	cj := &CritJSON{Files: make(map[string]CritJSONFile)}

	appendCommentScoped(cj, "main.go", 1, 1, "first", "reviewer", "", inheritedScope{})
	appendCommentScoped(cj, "server.go", 5, 5, "second", "reviewer", "", inheritedScope{})

	c1 := cj.Files["main.go"].Comments[0]
	c2 := cj.Files["server.go"].Comments[0]

	if c1.ID == c2.ID {
		t.Errorf("comment IDs should be unique across files: both = %q", c1.ID)
	}
}

func TestAddCommentToCritJSON_RoundTrip(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Add a comment via the CLI function
	err := addCommentToCritJSONScoped("README.md", 1, 1, "fix typo", "reviewer", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	// Read back and verify
	critPath := filepath.Join(dir, ".crit")
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatalf("reading .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("parsing .crit.json: %v", err)
	}
	cf, ok := cj.Files["README.md"]
	if !ok {
		t.Fatal("expected README.md in .crit.json files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Body != "fix typo" {
		t.Errorf("body = %q, want fix typo", cf.Comments[0].Body)
	}
	if cf.Comments[0].Author != "reviewer" {
		t.Errorf("author = %q, want reviewer", cf.Comments[0].Author)
	}

	// Add a second comment to same file
	err = addCommentToCritJSONScoped("README.md", 3, 5, "refactor this section", "agent", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("second addCommentToCritJSON: %v", err)
	}

	data, _ = os.ReadFile(reviewPathsFor(critPath).Review)
	json.Unmarshal(data, &cj)
	if len(cj.Files["README.md"].Comments) != 2 {
		t.Errorf("expected 2 comments after second add, got %d", len(cj.Files["README.md"].Comments))
	}
}

func TestAddReplyToCritJSON_RoundTrip(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Add a comment first
	err := addCommentToCritJSONScoped("README.md", 1, 1, "fix this", "reviewer", "", dir, inheritedScope{})
	if err != nil {
		t.Fatal(err)
	}

	// Read to get the comment ID
	critPath := filepath.Join(dir, ".crit")
	data, _ := os.ReadFile(reviewPathsFor(critPath).Review)
	var cj CritJSON
	json.Unmarshal(data, &cj)
	commentID := cj.Files["README.md"].Comments[0].ID

	// Add a reply
	err = addReplyToCritJSON(commentID, "done, fixed", "", "agent", false, dir, "")
	if err != nil {
		t.Fatalf("addReplyToCritJSON: %v", err)
	}

	data, _ = os.ReadFile(reviewPathsFor(critPath).Review)
	json.Unmarshal(data, &cj)
	if len(cj.Files["README.md"].Comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(cj.Files["README.md"].Comments[0].Replies))
	}
	if cj.Files["README.md"].Comments[0].Replies[0].Body != "done, fixed" {
		t.Errorf("reply body = %q", cj.Files["README.md"].Comments[0].Replies[0].Body)
	}
}

func TestAddReplyToCritJSON_WithResolve_ViaFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	addCommentToCritJSONScoped("README.md", 1, 1, "fix this", "reviewer", "", dir, inheritedScope{})

	critPath := filepath.Join(dir, ".crit")
	data, _ := os.ReadFile(reviewPathsFor(critPath).Review)
	var cj CritJSON
	json.Unmarshal(data, &cj)
	commentID := cj.Files["README.md"].Comments[0].ID

	err := addReplyToCritJSON(commentID, "done", "agent", "", true, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(reviewPathsFor(critPath).Review)
	json.Unmarshal(data, &cj)
	if !cj.Files["README.md"].Comments[0].Resolved {
		t.Error("expected comment to be resolved after reply with resolve=true")
	}
}

func TestRandomCommentID_Format(t *testing.T) {
	id := randomCommentID()
	if !strings.HasPrefix(id, "c_") || len(id) != 8 {
		t.Errorf("randomCommentID() = %q, want c_ prefix + 6 hex chars", id)
	}
}

func TestRandomReviewCommentID_Format(t *testing.T) {
	id := randomReviewCommentID()
	if !strings.HasPrefix(id, "r_") || len(id) != 8 {
		t.Errorf("randomReviewCommentID() = %q, want r_ prefix + 6 hex chars", id)
	}
}

func TestRandomReplyID_Format(t *testing.T) {
	id := randomReplyID()
	if !strings.HasPrefix(id, "rp_") || len(id) != 9 {
		t.Errorf("randomReplyID() = %q, want rp_ prefix + 6 hex chars", id)
	}
}

func TestParsePushEvent(t *testing.T) {
	tests := []struct {
		flag    string
		want    string
		wantErr bool
	}{
		{"comment", "COMMENT", false},
		{"approve", "APPROVE", false},
		{"request-changes", "REQUEST_CHANGES", false},
		{"", "COMMENT", false},
		{"invalid", "", true},
	}
	for _, tc := range tests {
		got, err := parsePushEvent(tc.flag)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parsePushEvent(%q): expected error", tc.flag)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePushEvent(%q): %v", tc.flag, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parsePushEvent(%q) = %q, want %q", tc.flag, got, tc.want)
		}
	}
}

func TestAddFileCommentToCritJSON_RejectsAbsolutePath(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addFileCommentToCritJSONScoped("/etc/passwd", "test", "author", "", "", inheritedScope{})
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestAddFileCommentToCritJSON_RejectsTraversal(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addFileCommentToCritJSONScoped("../outside", "test", "author", "", "", inheritedScope{})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestAddReviewCommentToCritJSON_RoundTrip(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addReviewCommentToCritJSONScoped("overall the code is good", "reviewer", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("addReviewCommentToCritJSON: %v", err)
	}

	critPath := filepath.Join(dir, ".crit")
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(cj.ReviewComments))
	}
	if cj.ReviewComments[0].Body != "overall the code is good" {
		t.Errorf("body = %q", cj.ReviewComments[0].Body)
	}
}

func TestClearCritJSON(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a .crit.json
	addCommentToCritJSONScoped("README.md", 1, 1, "test", "author", "", dir, inheritedScope{})

	critPath := filepath.Join(dir, ".crit")
	if _, err := os.Stat(critPath); err != nil {
		t.Fatal("expected .crit.json to exist")
	}

	err := clearCritJSON(dir)
	if err != nil {
		t.Fatalf("clearCritJSON: %v", err)
	}

	if _, err := os.Stat(critPath); !os.IsNotExist(err) {
		t.Error("expected .crit.json to be deleted")
	}
}

// TestAddReplyToCritJSON_RandomIDs exercises the reply threading workflow
// end-to-end with the new random hex ID format (c_XXXXXX, r_XXXXXX, rp_XXXXXX).
func TestAddReplyToCritJSON_RandomIDs(t *testing.T) {
	dir := t.TempDir()

	// Build a .crit.json with random-format IDs across multiple files
	cj := CritJSON{
		Branch:      "feature",
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "r_aabb01", Body: "general architecture note", Scope: "review",
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		},
		Files: map[string]CritJSONFile{
			"src/main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c_a3f8b2", StartLine: 10, EndLine: 12, Body: "Extract this",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
			"src/util.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c_d4e5f6", StartLine: 5, EndLine: 5, Body: "Rename this",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(mustMkdirAll(filepath.Join(dir, ".crit", "review.json")), data, 0644)

	t.Run("reply to file comment by random ID", func(t *testing.T) {
		err := addReplyToCritJSON("c_a3f8b2", "Done, extracted", "", "agent", false, dir, "")
		if err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
		var result CritJSON
		json.Unmarshal(data, &result)

		replies := result.Files["src/main.go"].Comments[0].Replies
		if len(replies) != 1 {
			t.Fatalf("expected 1 reply, got %d", len(replies))
		}
		if replies[0].Body != "Done, extracted" {
			t.Errorf("reply body = %q", replies[0].Body)
		}
		if !strings.HasPrefix(replies[0].ID, "rp_") || len(replies[0].ID) != 9 {
			t.Errorf("reply ID = %q, want rp_ prefix + 6 hex chars", replies[0].ID)
		}
	})

	t.Run("reply to review comment by random ID", func(t *testing.T) {
		err := addReplyToCritJSON("r_aabb01", "Acknowledged", "agent", "", false, dir, "")
		if err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
		var result CritJSON
		json.Unmarshal(data, &result)

		replies := result.ReviewComments[0].Replies
		if len(replies) != 1 {
			t.Fatalf("expected 1 reply, got %d", len(replies))
		}
		if replies[0].Body != "Acknowledged" {
			t.Errorf("reply body = %q", replies[0].Body)
		}
		if !strings.HasPrefix(replies[0].ID, "rp_") {
			t.Errorf("reply ID = %q, want rp_ prefix", replies[0].ID)
		}
	})

	t.Run("review comment reply does not need path disambiguation", func(t *testing.T) {
		// Review comments are global — no filterPath needed
		err := addReplyToCritJSON("r_aabb01", "No path needed", "agent", "", true, dir, "")
		if err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
		var result CritJSON
		json.Unmarshal(data, &result)

		if !result.ReviewComments[0].Resolved {
			t.Error("review comment should be resolved")
		}
		// Should have 2 replies now (from previous subtest + this one)
		if len(result.ReviewComments[0].Replies) != 2 {
			t.Fatalf("expected 2 replies, got %d", len(result.ReviewComments[0].Replies))
		}
	})
}

// TestAppendReply_AmbiguousID verifies the --path disambiguation error when
// the same comment ID appears in multiple files.
func TestAppendReply_AmbiguousID(t *testing.T) {
	duplicateID := "c_abcdef"
	cj := &CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: duplicateID, StartLine: 1, EndLine: 1, Body: "fix"},
				},
			},
			"b.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: duplicateID, StartLine: 5, EndLine: 5, Body: "fix"},
				},
			},
		},
	}

	t.Run("error without filterPath", func(t *testing.T) {
		err := appendReply(cj, duplicateID, "done", "agent", "", false, "")
		if err == nil {
			t.Fatal("expected disambiguation error")
		}
		if !strings.Contains(err.Error(), "use --path <file> to disambiguate") {
			t.Errorf("error = %q, want disambiguation message", err.Error())
		}
		if !strings.Contains(err.Error(), duplicateID) {
			t.Errorf("error should mention comment ID %q: %s", duplicateID, err.Error())
		}
	})

	t.Run("success with filterPath", func(t *testing.T) {
		// Reset: clear any replies added by the ambiguous attempt
		cjClean := &CritJSON{
			Files: map[string]CritJSONFile{
				"a.go": {
					Status: "modified",
					Comments: []Comment{
						{ID: duplicateID, StartLine: 1, EndLine: 1, Body: "fix"},
					},
				},
				"b.go": {
					Status: "modified",
					Comments: []Comment{
						{ID: duplicateID, StartLine: 5, EndLine: 5, Body: "fix"},
					},
				},
			},
		}

		err := appendReply(cjClean, duplicateID, "done", "agent", "", false, "a.go")
		if err != nil {
			t.Fatalf("appendReply with filterPath: %v", err)
		}
		if len(cjClean.Files["a.go"].Comments[0].Replies) != 1 {
			t.Fatalf("expected 1 reply on a.go, got %d", len(cjClean.Files["a.go"].Comments[0].Replies))
		}
		if len(cjClean.Files["b.go"].Comments[0].Replies) != 0 {
			t.Error("b.go should have no replies when filterPath=a.go")
		}
	})

	t.Run("filterPath with wrong file", func(t *testing.T) {
		cjClean := &CritJSON{
			Files: map[string]CritJSONFile{
				"a.go": {
					Status:   "modified",
					Comments: []Comment{{ID: duplicateID, StartLine: 1, EndLine: 1, Body: "fix"}},
				},
			},
		}

		err := appendReply(cjClean, duplicateID, "done", "agent", "", false, "nonexistent.go")
		if err == nil {
			t.Fatal("expected not-found error with wrong filterPath")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want 'not found'", err.Error())
		}
	})
}

func TestAddCommentToCritJSON_PopulatesAnchor(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Write a file with known content.
	writeFile(t, filepath.Join(dir, "hello.go"), "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	if err := addCommentToCritJSONScoped("hello.go", 3, 4, "Fix this function", "Bot", "", dir, inheritedScope{}); err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	critPath, _ := resolveReviewPath(dir)
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatalf("read review file: %v", err)
	}

	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	comments := cj.Files["hello.go"].Comments
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}

	want := "func main() {\n\tfmt.Println(\"hello\")"
	if comments[0].Anchor != want {
		t.Errorf("Anchor = %q, want %q", comments[0].Anchor, want)
	}
}

func TestBulkAddCommentsToCritJSON_PopulatesAnchor(t *testing.T) {
	dir := initTestRepo(t)
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	writeFile(t, filepath.Join(dir, "server.go"), "package main\n\nimport \"net/http\"\n\nfunc handler() {}\n")

	entries := []BulkCommentEntry{
		{File: "server.go", Line: 3, Body: "Why this import?"},
	}
	if err := bulkAddCommentsToCritJSONScoped(entries, "Bot", "", dir, inheritedScope{}); err != nil {
		t.Fatalf("bulkAddCommentsToCritJSON: %v", err)
	}

	critPath, _ := resolveReviewPath(dir)
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatalf("read review file: %v", err)
	}

	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	comments := cj.Files["server.go"].Comments
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}

	if comments[0].Anchor != "import \"net/http\"" {
		t.Errorf("Anchor = %q, want %q", comments[0].Anchor, "import \"net/http\"")
	}
}

// --- processBulkReviewEntry tests ---

func TestProcessBulkReviewEntry_ReviewScope(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	e := BulkCommentEntry{Body: "overall looks good", Scope: "review"}
	err := processBulkReviewEntry(&cj, 0, e, "reviewer", "", inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(cj.ReviewComments))
	}
	if cj.ReviewComments[0].Body != "overall looks good" {
		t.Errorf("body = %q, want 'overall looks good'", cj.ReviewComments[0].Body)
	}
	if cj.ReviewComments[0].Author != "reviewer" {
		t.Errorf("author = %q, want 'reviewer'", cj.ReviewComments[0].Author)
	}
}

func TestProcessBulkReviewEntry_WithLineRejectsNoFile(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	e := BulkCommentEntry{Body: "bad", Scope: "review", Line: 5}
	err := processBulkReviewEntry(&cj, 0, e, "author", "", inheritedScope{})
	if err == nil {
		t.Error("expected error when review entry has line number")
	}
}

func TestProcessBulkReviewEntry_NonReviewScopeWithFile(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	// When scope != "review" but File/Path are set, it errors because
	// file-scoped comments should go through processBulkFileOrLineEntry.
	e := BulkCommentEntry{Body: "bad", Scope: "line", File: "test.go"}
	err := processBulkReviewEntry(&cj, 1, e, "author", "", inheritedScope{})
	if err == nil {
		t.Error("expected error when non-review scope has file set")
	}
}

func TestProcessBulkReviewEntry_NoFileFallsThrough(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	// When no file/path is set and scope is not "review", it still adds a review comment.
	e := BulkCommentEntry{Body: "general feedback", Scope: "line"}
	err := processBulkReviewEntry(&cj, 0, e, "author", "", inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cj.ReviewComments) != 1 {
		t.Errorf("expected 1 review comment, got %d", len(cj.ReviewComments))
	}
}

// --- addFileCommentToCritJSON additional tests ---

func TestAddFileCommentToCritJSON_Success(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "test.go"), "package main\n")

	// Create initial crit.json.
	cj := CritJSON{
		Branch:  "main",
		BaseRef: "abc",
		Files:   map[string]CritJSONFile{},
	}
	critPath := filepath.Join(dir, ".crit")
	data, _ := json.Marshal(cj)
	os.WriteFile(mustMkdirAll(reviewPathsFor(critPath).Review), data, 0644)

	err := addFileCommentToCritJSONScoped("test.go", "file-level feedback", "reviewer", "", dir, inheritedScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Re-read and verify.
	data, err = os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var result CritJSON
	json.Unmarshal(data, &result)

	cf, ok := result.Files["test.go"]
	if !ok {
		t.Fatal("expected test.go in files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Body != "file-level feedback" {
		t.Errorf("body = %q", cf.Comments[0].Body)
	}
	if cf.Comments[0].Scope != "file" {
		t.Errorf("scope = %q, want file", cf.Comments[0].Scope)
	}
}

func TestDisplayName(t *testing.T) {
	for _, tc := range []struct {
		login, name, want string
	}{
		{"alice", "Alice Liddell", "Alice Liddell"},
		{"alice", "", "alice"},
		{"alice", "   ", "alice"},
		{"", "Alice", "Alice"},
		{"bob", "Bob", "Bob"},
	} {
		if got := displayName(tc.login, tc.name); got != tc.want {
			t.Errorf("displayName(%q, %q) = %q, want %q", tc.login, tc.name, got, tc.want)
		}
	}
}

func TestMergeGHComments_UsesDisplayNameFromCache(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{"main.go": {Status: "modified"}}}
	ghComments := []ghComment{
		{ID: 100, Path: "main.go", Line: 5, Side: "RIGHT", Body: "looks good",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"},
			CreatedAt: "2025-01-01T00:00:00Z"},
	}
	names := userNameCache{"alice": "Alice Liddell"}
	if added := mergeGHCommentsWithNames(&cj, ghComments, names, inheritedScope{}, nil); added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	if got := cj.Files["main.go"].Comments[0].Author; got != "Alice Liddell" {
		t.Errorf("Author = %q, want Alice Liddell", got)
	}
}

// TestMergeGHCommentsScoped_StampsHeadAndLayer verifies the C-1 fix: when
// mergeGHCommentsScoped is invoked with a non-empty scope (i.e., `crit pull`
// running inside an active range-mode focus), imported root comments are
// stamped with HeadSHA + DiffScope=layer so visibleInFocus shows them.
func TestMergeGHCommentsScoped_StampsHeadAndLayer(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	ghComments := []ghComment{
		{ID: 7, Path: "main.go", Line: 12, Side: "RIGHT", Body: "looks wrong",
			User: struct {
				Login string `json:"login"`
			}{Login: "rev"},
			CreatedAt: "2025-01-01T00:00:00Z"},
	}
	scope := inheritedScope{HeadSHA: "headXYZ", DiffScope: "layer"}
	if added := mergeGHCommentsScoped(&cj, ghComments, scope, nil); added != 1 {
		t.Fatalf("added=%d want 1", added)
	}
	c := cj.Files["main.go"].Comments[0]
	if c.HeadSHA != "headXYZ" {
		t.Errorf("HeadSHA=%q want headXYZ", c.HeadSHA)
	}
	if c.DiffScope != "layer" {
		t.Errorf("DiffScope=%q want layer", c.DiffScope)
	}
}

// TestMergeGHCommentsScoped_NoStampWithEmptyScope confirms the legacy
// working-tree path: when scope is empty (no daemon, no on-disk
// ActiveDiffScope), pulled comments are NOT stamped, preserving today's
// behavior.
func TestMergeGHCommentsScoped_NoStampWithEmptyScope(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	ghComments := []ghComment{
		{ID: 8, Path: "main.go", Line: 3, Side: "RIGHT", Body: "x",
			User: struct {
				Login string `json:"login"`
			}{Login: "u"},
			CreatedAt: "2025-01-01T00:00:00Z"},
	}
	mergeGHCommentsScoped(&cj, ghComments, inheritedScope{}, nil)
	c := cj.Files["main.go"].Comments[0]
	if c.HeadSHA != "" || c.DiffScope != "" {
		t.Errorf("comment was stamped despite empty scope: %+v", c)
	}
}

// TestResolvePullScope verifies the daemon-probe + on-disk fallback used by
// `crit pull` and mergeWebComments. Mirrors the precedence in
// resolveCommentScope (focus_cli.go) but always emits DiffScope=layer.
func TestResolvePullScope(t *testing.T) {
	cases := []struct {
		name        string
		daemonFocus *Focus
		diskScope   string
		wantHead    string
		wantScope   string
	}{
		{name: "no daemon, no disk -> empty (legacy)", wantHead: "", wantScope: ""},
		{
			name:        "daemon range/layer -> head + layer",
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc", DiffScope: DiffScopeLayer},
			wantHead:    "abc", wantScope: "layer",
		},
		{
			name:        "daemon range/full-stack -> still layer (pulls anchor to PR diff)",
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "def", DiffScope: DiffScopeFullStack},
			wantHead:    "def", wantScope: "layer",
		},
		{
			name:        "daemon working-tree -> empty",
			daemonFocus: &Focus{Kind: FocusWorkingTree},
			wantHead:    "", wantScope: "",
		},
		{
			name:      "no daemon, disk says layer -> layer with empty head",
			diskScope: "layer",
			wantHead:  "", wantScope: "layer",
		},
		{
			name:      "no daemon, disk says full_stack -> still layer",
			diskScope: "full_stack",
			wantHead:  "", wantScope: "layer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDaemonFocus(t, tc.daemonFocus)
			cj := &CritJSON{ActiveDiffScope: tc.diskScope}
			got := resolvePullScope(cj)
			if got.HeadSHA != tc.wantHead || got.DiffScope != tc.wantScope {
				t.Errorf("got=%+v want head=%q scope=%q", got, tc.wantHead, tc.wantScope)
			}
		})
	}
}

// TestParsePRViewJSON_ForkPR locks down the parser's handling of fork PRs:
// isCrossRepository must round-trip and HeadRepoURL must come from
// headRepository.url so ensureSHAFetched can fall back to the fork remote.
// This guards against future refactors stripping fields from prJSONFields.
func TestParsePRViewJSON_ForkPR(t *testing.T) {
	if !strings.Contains(prJSONFields, "isCrossRepository") {
		t.Fatalf("prJSONFields missing isCrossRepository: %q", prJSONFields)
	}
	if !strings.Contains(prJSONFields, "headRepository") {
		t.Fatalf("prJSONFields missing headRepository: %q", prJSONFields)
	}
	raw := `{
		"number": 42,
		"url": "https://github.com/upstream/repo/pull/42",
		"title": "fork PR",
		"isDraft": false,
		"state": "OPEN",
		"baseRefName": "main",
		"headRefName": "feature",
		"baseRefOid": "aaaaaaa",
		"headRefOid": "bbbbbbb",
		"isCrossRepository": true,
		"headRepository": {"url": "https://github.com/contributor/repo"},
		"author": {"login": "contributor", "name": "Contributor"},
		"createdAt": "2026-04-30T00:00:00Z"
	}`
	info, err := parsePRViewJSON([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !info.IsCrossRepository {
		t.Error("IsCrossRepository false; want true")
	}
	if info.HeadRepoURL != "https://github.com/contributor/repo" {
		t.Errorf("HeadRepoURL=%q want fork URL", info.HeadRepoURL)
	}
}

// TestParsePRViewJSON_SameRepoPR confirms HeadRepoURL is still populated for
// same-repo PRs but IsCrossRepository is false, so callers know not to pass
// the URL as a fallback remote (which would point at the same origin).
func TestParsePRViewJSON_SameRepoPR(t *testing.T) {
	raw := `{
		"number": 7,
		"url": "https://github.com/owner/repo/pull/7",
		"isCrossRepository": false,
		"headRepository": {"url": "https://github.com/owner/repo"},
		"baseRefName": "main",
		"headRefName": "feat",
		"author": {"login": "owner"}
	}`
	info, err := parsePRViewJSON([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.IsCrossRepository {
		t.Error("IsCrossRepository true; want false for same-repo PR")
	}
	if info.HeadRepoURL == "" {
		t.Error("HeadRepoURL empty; want owner/repo URL")
	}
}

// TestCollectEditedForPush_DetectsBodyDivergence verifies the diff-detection
// rules behind the edit-push path (#446):
//   - root comment with edited body relative to recorded hash → enqueued
//   - reply with edited body relative to recorded hash → enqueued
//   - GitHubID == 0 (never pushed) → not enqueued (handled by POST path)
//   - GitHubID != 0 with empty hash → enqueued (canonical local body must be PATCHed up)
//   - GitHubID != 0 and hash matches Body → not enqueued
//   - Resolved comment with edited body → not enqueued
func TestCollectEditedForPush_DetectsBodyDivergence(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				// edited (should be enqueued)
				{ID: "c1", GitHubID: 100, Body: "edited", LastPushedBodyHash: bodyHashAtPush("original")},
				// new (no GH ID — POST path handles it)
				{ID: "c2", GitHubID: 0, Body: "new", LastPushedBodyHash: ""},
				// pushed but no hash recorded — local body is canonical, PATCH it up
				{ID: "c3", GitHubID: 101, Body: "current", LastPushedBodyHash: ""},
				// in sync
				{ID: "c4", GitHubID: 102, Body: "same", LastPushedBodyHash: bodyHashAtPush("same")},
				// resolved, even if edited — skipped
				{ID: "c5", GitHubID: 103, Body: "edited", LastPushedBodyHash: bodyHashAtPush("original"), Resolved: true},
				// reply edits via parent c6
				{ID: "c6", GitHubID: 104, Body: "parent", LastPushedBodyHash: bodyHashAtPush("parent"), Replies: []Reply{
					{ID: "r1", GitHubID: 200, Body: "reply edit", LastPushedBodyHash: bodyHashAtPush("reply orig")}, // enqueued
					{ID: "r2", GitHubID: 0, Body: "new reply", LastPushedBodyHash: ""},                              // POST path
				}},
			}},
		},
	}

	edits := collectEditedForPush(cj)
	if len(edits) != 3 {
		t.Fatalf("collectEditedForPush returned %d edits, want 3: %+v", len(edits), edits)
	}

	gotIDs := map[int64]string{}
	for _, e := range edits {
		gotIDs[e.GitHubID] = e.Body
	}
	if gotIDs[100] != "edited" {
		t.Errorf("expected root c1 (id=100) → 'edited', got %q", gotIDs[100])
	}
	if gotIDs[101] != "current" {
		t.Errorf("expected root c3 (id=101) → 'current' (empty hash means PATCH), got %q", gotIDs[101])
	}
	if gotIDs[200] != "reply edit" {
		t.Errorf("expected reply r1 (id=200) → 'reply edit', got %q", gotIDs[200])
	}
}

// TestUpdateCritJSONWithEditedBodies_StampsLastPushedBodyHash verifies that
// after PATCH, the review file is rewritten with the edited body's digest
// stored in LastPushedBodyHash so the next push is a no-op.
func TestUpdateCritJSONWithEditedBodies_StampsLastPushedBodyHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.json")

	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{{
				ID: "c1", GitHubID: 500, Body: "edited", LastPushedBodyHash: bodyHashAtPush("original"),
				Replies: []Reply{
					{ID: "r1", GitHubID: 600, Body: "reply edit", LastPushedBodyHash: bodyHashAtPush("reply orig")},
				},
			}}},
		},
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	succeeded := []ghEditForPush{
		{GitHubID: 500, Body: "edited", Path: "a.go"},
		{GitHubID: 600, Body: "reply edit", IsReply: true},
	}
	if err := updateCritJSONWithEditedBodies(path, succeeded); err != nil {
		t.Fatalf("update: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got CritJSON
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := got.Files["a.go"].Comments[0]
	if want := bodyHashAtPush("edited"); c.LastPushedBodyHash != want {
		t.Errorf("comment LastPushedBodyHash=%q, want %q", c.LastPushedBodyHash, want)
	}
	if want := bodyHashAtPush("reply edit"); c.Replies[0].LastPushedBodyHash != want {
		t.Errorf("reply LastPushedBodyHash=%q, want %q", c.Replies[0].LastPushedBodyHash, want)
	}

	// Idempotency: re-running collectEditedForPush should now find nothing.
	if more := collectEditedForPush(got); len(more) != 0 {
		t.Errorf("after stamping, collectEditedForPush returned %d edits, want 0: %+v", len(more), more)
	}
}

// TestMergeGHComments_DoesNotValidatePRProvenance pins the current
// contract for gap #21 (cross-PR cross-pollination). The ghComment shape
// (see github.go: ghComment struct) carries no PR number or branch ref —
// the GitHub REST endpoint /pulls/{N}/comments returns comments without
// any back-reference to the PR they came from.
//
// As a result, mergeGHComments cannot reject "foreign" comments at this
// layer; the PR-scoping defense lives upstream in the caller (which
// chooses which /pulls/{N}/comments URL to fetch). This test pins the
// current behavior so a future contract change (adding a defensive check
// here, or threading PR metadata into ghComment) is a deliberate decision
// and not an accidental break of merge logic.
//
// If you change merge behavior to reject foreign comments, this test
// should fail and be updated to assert the new contract.
func TestMergeGHComments_DoesNotValidatePRProvenance(t *testing.T) {
	cj := CritJSON{
		Branch:  "feature-A",
		BaseRef: "main",
		Files: map[string]CritJSONFile{
			"main.go": {
				Status:   "modified",
				Comments: []Comment{},
			},
		},
	}

	// Synthetic comments — caller's responsibility to ensure these came
	// from feature-A's PR, not feature-B's. The merge function has no
	// way to tell them apart.
	foreign := []ghComment{
		{
			ID: 9999, Path: "main.go", Line: 3, Side: "RIGHT",
			Body: "comment from a different PR",
			User: struct {
				Login string `json:"login"`
			}{Login: "stranger"},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
	}

	added := mergeGHComments(&cj, foreign)
	if added != 1 {
		t.Fatalf("merge added = %d, want 1 (merge is metadata-blind by design); "+
			"if this dropped to 0 because a defense was added, update the test", added)
	}

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 merged comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Body != "comment from a different PR" {
		t.Errorf("merged body = %q", cf.Comments[0].Body)
	}

	// Document the invariant: ghComment carries no PR/branch field, so
	// merge cannot self-defend. Any cross-PR safety must live in the
	// fetch path (the caller decides which /pulls/{N}/comments to read).
	// If this property changes (e.g. ghComment grows a PullRequestURL
	// field), this test should be updated to assert the new defense.
}

// TestZeroGitHubID_TreatedAsNotPushed pins gap #26: a comment or reply
// with GitHubID == 0 must be treated as "not yet pushed" by every
// predicate that decides whether to send something to GitHub. If 0 ever
// becomes a legitimate GitHub ID, multiple call sites would need to
// change in lockstep — this table test serves as a tripwire.
//
// Predicates exercised:
//   - critJSONToGHComments (root push selector): GitHubID==0 → push,
//     GitHubID!=0 → skip ("already pushed").
//   - collectNewRepliesForPush (reply push selector): parent must be on
//     GH (GitHubID!=0) AND reply must be local (GitHubID==0).
func TestZeroGitHubID_TreatedAsNotPushed(t *testing.T) {
	t.Run("root_zero_is_pushed", func(t *testing.T) {
		cj := CritJSON{
			Files: map[string]CritJSONFile{
				"a.go": {Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "local-only", GitHubID: 0},
				}},
			},
		}
		out := critJSONToGHComments(cj)
		if len(out) != 1 {
			t.Fatalf("GitHubID==0 root must be pushed; got %d entries", len(out))
		}
	})

	t.Run("root_nonzero_is_skipped", func(t *testing.T) {
		cj := CritJSON{
			Files: map[string]CritJSONFile{
				"a.go": {Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "already on GH", GitHubID: 7},
				}},
			},
		}
		out := critJSONToGHComments(cj)
		if len(out) != 0 {
			t.Fatalf("GitHubID!=0 root must be skipped; got %d entries", len(out))
		}
	})

	t.Run("reply_with_zero_parent_skipped", func(t *testing.T) {
		// Parent root is local (GitHubID==0): reply has nothing to attach
		// to on GitHub, so collectNewRepliesForPush must skip it.
		cf := CritJSONFile{Comments: []Comment{
			{ID: "c1", GitHubID: 0, Body: "local root", Replies: []Reply{
				{ID: "rp1", GitHubID: 0, Body: "local reply"},
			}},
		}}
		got := collectNewRepliesForPush(cf)
		if len(got) != 0 {
			t.Fatalf("reply with GitHubID==0 parent must be skipped; got %d", len(got))
		}
	})

	t.Run("reply_with_nonzero_parent_zero_self_pushed", func(t *testing.T) {
		// Parent on GitHub (GitHubID!=0), reply local (GitHubID==0):
		// must be queued for push.
		cf := CritJSONFile{Comments: []Comment{
			{ID: "c1", GitHubID: 100, Body: "remote root", Replies: []Reply{
				{ID: "rp1", GitHubID: 0, Body: "new local reply"},
			}},
		}}
		got := collectNewRepliesForPush(cf)
		if len(got) != 1 {
			t.Fatalf("reply (GitHubID==0, parent!=0) must be queued; got %d", len(got))
		}
		if got[0].ParentGHID != 100 || got[0].Body != "new local reply" {
			t.Errorf("queued reply = %+v", got[0])
		}
	})

	t.Run("reply_with_nonzero_parent_nonzero_self_skipped", func(t *testing.T) {
		// Both parent and reply on GitHub: nothing to do.
		cf := CritJSONFile{Comments: []Comment{
			{ID: "c1", GitHubID: 100, Body: "remote root", Replies: []Reply{
				{ID: "rp1", GitHubID: 200, Body: "remote reply"},
			}},
		}}
		got := collectNewRepliesForPush(cf)
		if len(got) != 0 {
			t.Fatalf("reply with GitHubID!=0 must be skipped; got %d", len(got))
		}
	})
}

// TestCollectDeletesForPush_ReturnsPendingList verifies that
// collectDeletesForPush returns a copy of CritJSON.PendingGitHubDeletes.
func TestCollectDeletesForPush_ReturnsPendingList(t *testing.T) {
	cj := CritJSON{
		PendingGitHubDeletes: []int64{100, 200, 300},
		Files:                map[string]CritJSONFile{},
	}
	got := collectDeletesForPush(cj)
	if len(got) != 3 || got[0] != 100 || got[1] != 200 || got[2] != 300 {
		t.Fatalf("collectDeletesForPush = %v, want [100 200 300]", got)
	}

	// Caller may mutate without affecting cj — verify independence.
	got[0] = 999
	if cj.PendingGitHubDeletes[0] != 100 {
		t.Errorf("caller mutation leaked back into cj.PendingGitHubDeletes")
	}

	// Empty input returns nil (no allocation).
	if x := collectDeletesForPush(CritJSON{}); x != nil {
		t.Errorf("empty pending list should return nil, got %v", x)
	}
}

// TestUpdateCritJSONAfterDeletes_DrainsPendingList verifies that drained IDs
// are removed from PendingGitHubDeletes on disk; non-drained IDs persist for
// retry on the next push.
func TestUpdateCritJSONAfterDeletes_DrainsPendingList(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reviewPath := filepath.Join(dir, "review.json")

	cj := CritJSON{
		PendingGitHubDeletes: []int64{100, 101, 102},
		Files:                map[string]CritJSONFile{},
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(reviewPath, data, 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 100 + 102 drained; 101 stays pending.
	if err := updateCritJSONAfterDeletes(dir, []int64{100, 102}); err != nil {
		t.Fatalf("update: %v", err)
	}

	out, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got CritJSON
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.PendingGitHubDeletes) != 1 || got.PendingGitHubDeletes[0] != 101 {
		t.Errorf("PendingGitHubDeletes after drain = %v, want [101]", got.PendingGitHubDeletes)
	}
}

// TestUpdateCritJSONAfterDeletes_FullDrainOmitsField asserts the JSON does
// not retain an empty array (omitempty on the struct tag) — the field should
// vanish from disk once everything has been DELETEd upstream.
func TestUpdateCritJSONAfterDeletes_FullDrainOmitsField(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	cj := CritJSON{PendingGitHubDeletes: []int64{42}, Files: map[string]CritJSONFile{}}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(reviewPath, data, 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := updateCritJSONAfterDeletes(dir, []int64{42}); err != nil {
		t.Fatalf("update: %v", err)
	}
	raw, _ := os.ReadFile(reviewPath)
	if strings.Contains(string(raw), "pending_github_deletes") {
		t.Errorf("pending_github_deletes still present after full drain:\n%s", raw)
	}
}

// TestMergeRootComment_SkipsPendingDelete verifies that pull does not
// resurrect a comment whose ID is in PendingGitHubDeletes — the user has
// already issued an intent to DELETE that must survive intermediate pulls.
func TestMergeRootComment_SkipsPendingDelete(t *testing.T) {
	cj := CritJSON{
		PendingGitHubDeletes: []int64{999},
		Files: map[string]CritJSONFile{
			"a.go": {Status: "modified", Comments: []Comment{}},
		},
	}
	gc := []ghComment{{
		ID: 999, Path: "a.go", Line: 5, Side: "RIGHT",
		Body: "should not resurrect",
		User: struct {
			Login string `json:"login"`
		}{Login: "alice"},
	}}
	added := mergeGHCommentsWithNames(&cj, gc, userNameCache{"alice": "alice"}, inheritedScope{}, nil)
	if added != 0 {
		t.Errorf("merge re-imported pending-delete comment (added=%d, want 0)", added)
	}
	if cf := cj.Files["a.go"]; len(cf.Comments) != 0 {
		t.Errorf("expected zero comments after merge, got %+v", cf.Comments)
	}
}

// TestMergeOrphanReplies_SkipsPendingDelete asserts that a reply queued for
// DELETE locally is not re-imported when its parent is already in cj.
func TestMergeOrphanReplies_SkipsPendingDelete(t *testing.T) {
	cj := CritJSON{
		PendingGitHubDeletes: []int64{777},
		Files: map[string]CritJSONFile{
			"a.go": {Status: "modified", Comments: []Comment{
				{ID: "c1", GitHubID: 500, Body: "parent", StartLine: 5, EndLine: 5, Author: "alice"},
			}},
		},
	}
	gc := []ghComment{{
		ID: 777, Path: "a.go", Line: 5, Side: "RIGHT",
		Body: "deleted reply",
		User: struct {
			Login string `json:"login"`
		}{Login: "alice"},
		InReplyToID: 500,
	}}
	mergeGHCommentsWithNames(&cj, gc, userNameCache{"alice": "alice"}, inheritedScope{}, nil)
	cf := cj.Files["a.go"]
	if len(cf.Comments) != 1 || len(cf.Comments[0].Replies) != 0 {
		t.Errorf("expected parent with no replies; got %+v", cf.Comments)
	}
}

// TestParseGHIncludeStatus parses representative `gh api --include` outputs.
func TestParseGHIncludeStatus(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"http2 204", "HTTP/2 204\nDate: ...\n\n", 204},
		{"http1.1 200", "HTTP/1.1 200 OK\n\n", 200},
		{"http2 404", "HTTP/2 404\n\n{}", 404},
		{"http2 403", "HTTP/2 403\n\n{}", 403},
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGHIncludeStatus([]byte(tc.in)); got != tc.want {
				t.Errorf("parseGHIncludeStatus(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// installFakeGHForDelete installs a `gh` shim on PATH that emits a fixed
// HTTP status line so deleteGHComment can be exercised without network.
// Setting exitNonZero mirrors `gh api`'s real behavior on non-2xx responses.
func installFakeGHForDelete(t *testing.T, statusLine string, exitNonZero bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}
	dir := t.TempDir()
	exit := "0"
	if exitNonZero {
		exit = "1"
	}
	script := "#!/bin/sh\nprintf '%s\\n\\n' '" + statusLine + "'\nexit " + exit + "\n"
	fakeGH := filepath.Join(dir, "gh")
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestDeleteGHComment_StatusHandling exercises the three paths the push
// drainer relies on: 2xx success, 404 already-gone, 403 not-author, and a
// generic 500 surface as an error.
func TestDeleteGHComment_StatusHandling(t *testing.T) {
	cases := []struct {
		name        string
		statusLine  string
		exitNonZero bool
		wantStatus  int
		wantErr     bool
	}{
		{"204_no_content", "HTTP/2 204", false, 204, false},
		{"404_already_gone", "HTTP/2 404", true, 404, false},
		{"403_not_author", "HTTP/2 403", true, 403, false},
		{"500_server_error", "HTTP/2 500", true, 500, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeGHForDelete(t, tc.statusLine, tc.exitNonZero)
			status, err := deleteGHComment(42)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestThreadResolvedForRoot covers the lookup precedence: root databaseID
// first, then any reply, then absent.
func TestThreadResolvedForRoot(t *testing.T) {
	for _, tc := range []struct {
		name           string
		rootID         int64
		replies        []ghComment
		threadResolved map[int64]bool
		wantResolved   bool
		wantPresent    bool
	}{
		{
			name:           "nil map returns absent",
			rootID:         1,
			threadResolved: nil,
			wantPresent:    false,
		},
		{
			name:           "root hit takes precedence",
			rootID:         1,
			replies:        []ghComment{{ID: 2}},
			threadResolved: map[int64]bool{1: true, 2: false},
			wantResolved:   true,
			wantPresent:    true,
		},
		{
			name:           "reply fallback when root missing",
			rootID:         1,
			replies:        []ghComment{{ID: 2}},
			threadResolved: map[int64]bool{2: true},
			wantResolved:   true,
			wantPresent:    true,
		},
		{
			name:           "neither root nor reply -> absent",
			rootID:         1,
			replies:        []ghComment{{ID: 2}},
			threadResolved: map[int64]bool{99: true},
			wantPresent:    false,
		},
		{
			name:           "explicit unresolved is present=true",
			rootID:         1,
			threadResolved: map[int64]bool{1: false},
			wantResolved:   false,
			wantPresent:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotResolved, gotPresent := threadResolvedForRoot(tc.rootID, tc.replies, tc.threadResolved)
			if gotResolved != tc.wantResolved || gotPresent != tc.wantPresent {
				t.Errorf("got (%v,%v), want (%v,%v)",
					gotResolved, gotPresent, tc.wantResolved, tc.wantPresent)
			}
		})
	}
}

// TestMergeGHComments_ThreadResolved_NewComment verifies a freshly imported
// root inherits Resolved=true and ResolvedRound=cj.ReviewRound when its
// thread is resolved on github.com.
func TestMergeGHComments_ThreadResolved_NewComment(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}, ReviewRound: 3}
	ghComments := []ghComment{
		{ID: 42, Path: "main.go", Line: 5, Side: "RIGHT", Body: "decide", CreatedAt: "2025-01-01T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"}},
	}
	threadResolved := map[int64]bool{42: true}
	added := mergeGHCommentsWithNames(&cj, ghComments, userNameCache{"alice": "alice"}, inheritedScope{}, threadResolved)
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	c := cj.Files["main.go"].Comments[0]
	if !c.Resolved {
		t.Errorf("Resolved = false, want true")
	}
	if c.ResolvedRound != 3 {
		t.Errorf("ResolvedRound = %d, want 3", c.ResolvedRound)
	}
}

// TestMergeGHComments_ThreadResolved_ExistingDedupedComment exercises the
// crit pull workflow: an earlier pull imported the comment with
// Resolved=false; the reviewer later clicked Resolve on github.com; the
// next pull must update Resolved on the deduplicated local comment.
func TestMergeGHComments_ThreadResolved_ExistingDedupedComment(t *testing.T) {
	cj := CritJSON{
		ReviewRound: 4,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", GitHubID: 42, Author: "alice", Body: "decide",
						StartLine: 5, EndLine: 5, Resolved: false},
				},
			},
		},
	}
	ghComments := []ghComment{
		{ID: 42, Path: "main.go", Line: 5, Side: "RIGHT", Body: "decide", CreatedAt: "2025-01-01T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"}},
	}
	threadResolved := map[int64]bool{42: true}
	added := mergeGHCommentsWithNames(&cj, ghComments, userNameCache{"alice": "alice"}, inheritedScope{}, threadResolved)
	// added is 1 because the resolution flip is a meaningful change that
	// must trigger a save in runPull (added==0 short-circuits persistence).
	if added != 1 {
		t.Fatalf("added = %d, want 1 (resolution flip should be counted)", added)
	}
	c := cj.Files["main.go"].Comments[0]
	if !c.Resolved {
		t.Errorf("Resolved = false, want true")
	}
	if c.ResolvedRound != 4 {
		t.Errorf("ResolvedRound = %d, want 4 (cj.ReviewRound)", c.ResolvedRound)
	}
}

// TestMergeGHComments_ThreadUnresolved_DoesNotClobberLocallyResolved
// pins the asymmetric merge: a thread that is unresolved on github.com
// must NOT undo a locally-resolved comment. Local users may resolve via
// crit / crit-web independently of github.com, and crit pull is not
// authoritative on the unresolved->resolved transition direction.
func TestMergeGHComments_ThreadUnresolved_DoesNotClobberLocallyResolved(t *testing.T) {
	cj := CritJSON{
		ReviewRound: 5,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", GitHubID: 42, Author: "alice", Body: "decide",
						StartLine: 5, EndLine: 5, Resolved: true, ResolvedRound: 2},
				},
			},
		},
	}
	ghComments := []ghComment{
		{ID: 42, Path: "main.go", Line: 5, Side: "RIGHT", Body: "decide", CreatedAt: "2025-01-01T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"}},
	}
	threadResolved := map[int64]bool{42: false}
	added := mergeGHCommentsWithNames(&cj, ghComments, userNameCache{"alice": "alice"}, inheritedScope{}, threadResolved)
	if added != 0 {
		t.Errorf("added = %d, want 0 (no-op when remote is unresolved and local is resolved)", added)
	}
	c := cj.Files["main.go"].Comments[0]
	if !c.Resolved {
		t.Errorf("Resolved = false, want true (local resolution must not be clobbered)")
	}
	if c.ResolvedRound != 2 {
		t.Errorf("ResolvedRound = %d, want 2 (preserved from prior local resolve)", c.ResolvedRound)
	}
}

// TestMergeGHComments_NilThreadMap_NoOp confirms that callers passing nil
// (e.g., a GraphQL fetch failed best-effort) preserve all existing
// behavior — no Resolved bits are flipped, no new Resolved bits set.
func TestMergeGHComments_NilThreadMap_NoOp(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}, ReviewRound: 1}
	ghComments := []ghComment{
		{ID: 7, Path: "main.go", Line: 3, Side: "RIGHT", Body: "x", CreatedAt: "2025-01-01T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "u"}},
	}
	mergeGHCommentsWithNames(&cj, ghComments, userNameCache{"u": "u"}, inheritedScope{}, nil)
	c := cj.Files["main.go"].Comments[0]
	if c.Resolved {
		t.Errorf("Resolved = true, want false (nil thread map must not set resolved)")
	}
	if c.ResolvedRound != 0 {
		t.Errorf("ResolvedRound = %d, want 0", c.ResolvedRound)
	}
}

// TestMergeGHComments_ThreadResolvedViaReplyID covers the case where the
// resolved bit lookup for the root falls back to a reply's databaseID.
// (e.g., the GraphQL response indexed only the reply's databaseID — both
// are valid keys since the bit is shared across the thread.)
func TestMergeGHComments_ThreadResolvedViaReplyID(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{}, ReviewRound: 2}
	ghComments := []ghComment{
		{ID: 42, Path: "main.go", Line: 5, Side: "RIGHT", Body: "root", CreatedAt: "2025-01-01T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "alice"}},
		{ID: 43, Path: "main.go", Line: 5, Side: "RIGHT", Body: "reply", CreatedAt: "2025-01-01T00:01:00Z",
			User: struct {
				Login string `json:"login"`
			}{Login: "bob"}, InReplyToID: 42},
	}
	// Only the reply's databaseID is in the map — root must still be flagged.
	threadResolved := map[int64]bool{43: true}
	mergeGHCommentsWithNames(&cj, ghComments, userNameCache{"alice": "alice", "bob": "bob"}, inheritedScope{}, threadResolved)
	c := cj.Files["main.go"].Comments[0]
	if !c.Resolved {
		t.Errorf("Resolved = false, want true (lookup must fall back to reply databaseID)")
	}
	if c.ResolvedRound != 2 {
		t.Errorf("ResolvedRound = %d, want 2", c.ResolvedRound)
	}
}
