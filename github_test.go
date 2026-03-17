package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
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

func TestBuildReviewPayload_EmptyMessageByDefault(t *testing.T) {
	comments := []map[string]any{{"path": "main.go", "line": 1, "side": "RIGHT", "body": "fix"}}
	data, err := buildReviewPayload(comments, "")
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
	data, err := buildReviewPayload(comments, "Round 2 review")
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

	err := addCommentToCritJSON("../../../etc/passwd", 1, 1, "bad", "", "")
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

	err := addCommentToCritJSON("/etc/passwd", 1, 1, "bad", "", "")
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

	err := addCommentToCritJSON("main.go", 10, 15, "Fix this bug", "", "")
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

	if err := addCommentToCritJSON("main.go", 1, 1, "First", "", ""); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := addCommentToCritJSON("main.go", 20, 20, "Second", "", ""); err != nil {
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

	addCommentToCritJSON("main.go", 1, 1, "Comment on main", "", "")
	addCommentToCritJSON("auth.go", 5, 10, "Comment on auth", "", "")

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

func TestAddCommentToCritJSON_FileMode_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSON("main.go", 5, 5, "File mode comment", "", "")
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
	addCommentToCritJSON("src/auth.go", 10, 10, "comment", "", "")

	data, _ := os.ReadFile(dir + "/.crit.json")
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

	if err := addCommentToCritJSON("main.go", 1, 1, "custom output dir", "", outputDir); err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	// Should NOT be in repo root
	if _, err := os.Stat(repoDir + "/.crit.json"); err == nil {
		t.Error(".crit.json should not be written to repo root when --output is set")
	}

	// Should be in outputDir
	data, err := os.ReadFile(outputDir + "/.crit.json")
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

	if err := addCommentToCritJSON("feature.go", 1, 1, "test comment", "", ""); err != nil {
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

	// BaseRef must be set — proves MergeBase was called against "base", not auto-detected default
	if cj.BaseRef == "" {
		t.Error("BaseRef should be non-empty when base_branch is set in config")
	}
}
