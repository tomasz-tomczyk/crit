package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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
	if cf.Comments[1].ID != "c2" {
		t.Errorf("new comment ID = %q, want c2", cf.Comments[1].ID)
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

func TestAddCommentToCritJSON_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSON("../../../etc/passwd", 1, 1, "bad")
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

	err := addCommentToCritJSON("/etc/passwd", 1, 1, "bad")
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

	err := addCommentToCritJSON("main.go", 10, 15, "Fix this bug")
	if err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	data, err := os.ReadFile(dir + "/.crit.json")
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
	if cf.Comments[0].ID != "c1" {
		t.Errorf("ID = %q, want c1", cf.Comments[0].ID)
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

	if err := addCommentToCritJSON("main.go", 1, 1, "First"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := addCommentToCritJSON("main.go", 20, 20, "Second"); err != nil {
		t.Fatalf("second add: %v", err)
	}

	data, _ := os.ReadFile(dir + "/.crit.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}
	if cf.Comments[0].ID != "c1" || cf.Comments[1].ID != "c2" {
		t.Errorf("IDs = %q, %q — want c1, c2", cf.Comments[0].ID, cf.Comments[1].ID)
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

	addCommentToCritJSON("main.go", 1, 1, "Comment on main")
	addCommentToCritJSON("auth.go", 5, 10, "Comment on auth")

	data, _ := os.ReadFile(dir + "/.crit.json")
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
