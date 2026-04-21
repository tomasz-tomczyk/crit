package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	writeFile(t, mdPath, "# Plan\n\n## Step 1\n\nDo the thing\n")
	goPath := filepath.Join(dir, "main.go")
	writeFile(t, goPath, "package main\n\nfunc main() {}\n")

	s := &Session{
		RepoRoot:    dir,
		ReviewRound: 1,

		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  mdPath,
				Status:   "added",
				FileType: "markdown",
				Content:  "# Plan\n\n## Step 1\n\nDo the thing\n",
				FileHash: "sha256:test1",
				Comments: []Comment{},
			},
			{
				Path:     "main.go",
				AbsPath:  goPath,
				Status:   "modified",
				FileType: "code",
				Content:  "package main\n\nfunc main() {}\n",
				FileHash: "sha256:test2",
				Comments: []Comment{},
			},
		},
	}
	return s
}

func TestSession_FileByPath(t *testing.T) {
	s := newTestSession(t)
	f := s.FileByPath("plan.md")
	if f == nil {
		t.Fatal("expected to find plan.md")
	}
	if f.FileType != "markdown" {
		t.Errorf("FileType = %q, want markdown", f.FileType)
	}
	if s.FileByPath("nonexistent.txt") != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestSession_AddComment(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddComment("plan.md", 1, 3, "", "Rethink this", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if !strings.HasPrefix(c.ID, "c_") || len(c.ID) != 8 {
		t.Errorf("ID = %q, want c_ prefix + 6 hex chars", c.ID)
	}
	if c.Body != "Rethink this" {
		t.Errorf("Body = %q", c.Body)
	}

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(comments))
	}
}

func TestSession_AddComment_NonexistentFile(t *testing.T) {
	s := newTestSession(t)
	_, ok := s.AddComment("nonexistent.go", 1, 1, "", "test", "", "")
	if ok {
		t.Error("expected AddComment to fail for nonexistent file")
	}
}

func TestSession_UpdateComment(t *testing.T) {
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "original", "", "")
	updated, ok := s.UpdateComment("plan.md", c.ID, "updated body")
	if !ok {
		t.Fatal("UpdateComment failed")
	}
	if updated.Body != "updated body" {
		t.Errorf("Body = %q", updated.Body)
	}
}

func TestSession_UpdateComment_NotFound(t *testing.T) {
	s := newTestSession(t)
	_, ok := s.UpdateComment("plan.md", "c999", "body")
	if ok {
		t.Error("expected update to fail for nonexistent comment")
	}
}

func TestSession_DeleteComment(t *testing.T) {
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "to delete", "", "")
	if !s.DeleteComment("plan.md", c.ID) {
		t.Fatal("DeleteComment failed")
	}
	if len(s.GetComments("plan.md")) != 0 {
		t.Error("comment should be deleted")
	}
}

func TestSession_DeleteComment_NotFound(t *testing.T) {
	s := newTestSession(t)
	if s.DeleteComment("plan.md", "c999") {
		t.Error("expected delete to fail for nonexistent comment")
	}
}

func TestSession_GetComments_ReturnsCopy(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "test", "", "")
	comments := s.GetComments("plan.md")
	comments[0].Body = "mutated"
	if s.GetComments("plan.md")[0].Body == "mutated" {
		t.Error("GetComments should return a copy")
	}
}

func TestSession_GetAllComments(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "md comment", "", "")
	s.AddComment("main.go", 1, 1, "", "go comment", "", "")

	all := s.GetAllComments()
	if len(all) != 2 {
		t.Errorf("expected 2 files with comments, got %d", len(all))
	}
	if len(all["plan.md"]) != 1 {
		t.Errorf("plan.md comments = %d", len(all["plan.md"]))
	}
}

func TestSession_TotalCommentCount(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "one", "", "")
	s.AddComment("plan.md", 2, 2, "", "two", "", "")
	s.AddComment("main.go", 1, 1, "", "three", "", "")

	if s.TotalCommentCount() != 3 {
		t.Errorf("TotalCommentCount = %d, want 3", s.TotalCommentCount())
	}
}

func TestSession_NewCommentCount(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "new one", "", "")
	s.AddComment("plan.md", 2, 2, "", "new two", "", "")

	// Simulate carried-forward comments (as happens after round complete)
	s.mu.Lock()
	f := s.fileByPathLocked("main.go")
	f.Comments = append(f.Comments, Comment{
		ID:             "c1",
		StartLine:      1,
		EndLine:        1,
		Body:           "carried",
		CarriedForward: true,
	})
	s.mu.Unlock()

	if got := s.TotalCommentCount(); got != 3 {
		t.Errorf("TotalCommentCount = %d, want 3", got)
	}
	if got := s.NewCommentCount(); got != 2 {
		t.Errorf("NewCommentCount = %d, want 2", got)
	}
}

func TestSession_NewCommentCount_AllCarriedForward(t *testing.T) {
	s := newTestSession(t)
	s.mu.Lock()
	f := s.fileByPathLocked("plan.md")
	f.Comments = []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "resolved", CarriedForward: true, Resolved: true},
		{ID: "c2", StartLine: 2, EndLine: 2, Body: "open", CarriedForward: true},
	}
	s.mu.Unlock()

	if got := s.TotalCommentCount(); got != 2 {
		t.Errorf("TotalCommentCount = %d, want 2", got)
	}
	if got := s.NewCommentCount(); got != 0 {
		t.Errorf("NewCommentCount = %d, want 0", got)
	}
}

func TestSession_UnresolvedCommentCount(t *testing.T) {
	s := newTestSession(t)
	s.mu.Lock()
	f := s.fileByPathLocked("plan.md")
	f.Comments = []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "resolved one", Resolved: true},
		{ID: "c2", StartLine: 2, EndLine: 2, Body: "open one"},
		{ID: "c3", StartLine: 3, EndLine: 3, Body: "resolved two", Resolved: true},
	}
	g := s.fileByPathLocked("main.go")
	g.Comments = []Comment{
		{ID: "c4", StartLine: 1, EndLine: 1, Body: "open two"},
	}
	s.mu.Unlock()

	if got := s.UnresolvedCommentCount(); got != 2 {
		t.Errorf("UnresolvedCommentCount = %d, want 2", got)
	}
	if got := s.TotalCommentCount(); got != 4 {
		t.Errorf("TotalCommentCount = %d, want 4", got)
	}
}

func TestSession_WriteFiles(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "fix", "", "")

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("crit.json not written: %v", err)
	}

	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if cj.ReviewRound != 1 {
		t.Errorf("review_round = %d, want 1", cj.ReviewRound)
	}
	if len(cj.Files) != 1 {
		t.Errorf("expected 1 file (only files with comments), got %d", len(cj.Files))
	}
	if len(cj.Files["plan.md"].Comments) != 1 {
		t.Errorf("plan.md comments = %d, want 1", len(cj.Files["plan.md"].Comments))
	}
}

func TestSession_WriteFiles_NoCommentsSkips(t *testing.T) {
	s := newTestSession(t)
	s.WriteFiles()

	if _, err := os.Stat(s.critJSONPath()); !os.IsNotExist(err) {
		t.Error("expected .crit.json to not be written with no comments")
	}
}

func TestSession_WriteFiles_SharedURLOnly(t *testing.T) {
	s := newTestSession(t)
	s.SetSharedURLAndToken("https://crit.md/r/abc", "token123")

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if cj.ShareURL != "https://crit.md/r/abc" {
		t.Errorf("share_url = %q", cj.ShareURL)
	}
}

func TestSession_LoadCritJSON(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "persisted comment", "", "")

	flushWrites(s)
	s.WriteFiles()

	// Create a new session pointing to same dir
	s2 := newTestSession(t)
	s2.RepoRoot = s.RepoRoot
	s2.Files[0].FileHash = s.Files[0].FileHash // match hash
	s2.loadCritJSON()

	comments := s2.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "persisted comment" {
		t.Errorf("Body = %q", comments[0].Body)
	}
}

func TestSession_LoadCritJSON_NoHash(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json without file_hash fields (simulating agent-generated review)
	cj := `{
		"branch": "test",
		"base_ref": "",
		"updated_at": "2025-01-01T00:00:00Z",
		"review_round": 1,
		"files": {
			"plan.md": {
				"status": "added",
				"comments": [
					{
						"id": "c1",
						"start_line": 1,
						"end_line": 1,
						"body": "agent review comment",
						"created_at": "2025-01-01T00:00:00Z",
						"resolved": false
					}
				]
			}
		}
	}`
	if err := os.WriteFile(s.critJSONPath(), []byte(cj), 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	s.loadCritJSON()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "agent review comment" {
		t.Errorf("Body = %q, want %q", comments[0].Body, "agent review comment")
	}
}

func TestSession_WriteFiles_PreservesNonSessionFiles(t *testing.T) {
	s := newTestSession(t)

	// Simulate `crit comment` having written a comment on a file not in the session
	cj := `{
		"branch": "test",
		"base_ref": "",
		"review_round": 1,
		"files": {
			"unrelated.go": {
				"status": "modified",
				"comments": [{"id": "c1", "start_line": 5, "end_line": 5, "body": "external comment", "resolved": false}]
			}
		}
	}`
	if err := os.WriteFile(s.critJSONPath(), []byte(cj), 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	// Add a comment on a session file (plan.md) and trigger a write
	s.AddComment("plan.md", 1, 1, "", "session comment", "", "")
	s.WriteFiles()

	// Reload and verify both files are present
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var written CritJSON
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := written.Files["unrelated.go"]; !ok {
		t.Error("unrelated.go comments should be preserved after WriteFiles")
	}
	if _, ok := written.Files["plan.md"]; !ok {
		t.Error("plan.md comments should be written")
	}
}

func TestSession_LoadCritJSON_MismatchedHash(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json with a stale/wrong file_hash
	cj := `{
		"branch": "test",
		"base_ref": "",
		"updated_at": "2025-01-01T00:00:00Z",
		"review_round": 1,
		"files": {
			"plan.md": {
				"status": "added",
				"file_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"comments": [
					{
						"id": "c1",
						"start_line": 1,
						"end_line": 1,
						"body": "stale hash comment",
						"created_at": "2025-01-01T00:00:00Z",
						"resolved": false
					}
				]
			}
		}
	}`
	if err := os.WriteFile(s.critJSONPath(), []byte(cj), 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	s.loadCritJSON()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "stale hash comment" {
		t.Errorf("Body = %q, want %q", comments[0].Body, "stale hash comment")
	}
}

func TestSession_SignalRoundComplete(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "fix this", "", "")
	s.AddComment("main.go", 1, 1, "", "and this", "", "")
	s.IncrementEdits()
	s.IncrementEdits()

	s.SignalRoundComplete()

	if s.GetPendingEdits() != 0 {
		t.Errorf("pending edits = %d after round-complete", s.GetPendingEdits())
	}
	if s.GetLastRoundEdits() != 2 {
		t.Errorf("last round edits = %d, want 2", s.GetLastRoundEdits())
	}
	// ReviewRound is NOT incremented by SignalRoundComplete — it is deferred
	// to the watcher's handleRoundComplete* handler to prevent a race where
	// GetSessionInfo could observe the new round before carry-forward completes.
	if s.GetReviewRound() != 1 {
		t.Errorf("review round = %d, want 1 (deferred to watcher)", s.GetReviewRound())
	}
	if len(s.GetComments("plan.md")) != 0 {
		t.Error("plan.md comments should be cleared")
	}
	if len(s.GetComments("main.go")) != 0 {
		t.Error("main.go comments should be cleared")
	}
}

func TestSession_ConcurrentAccess(t *testing.T) {
	s := newTestSession(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := s.AddComment("plan.md", 1, 1, "", "concurrent", "", "")
			s.UpdateComment("plan.md", c.ID, "updated")
			s.GetComments("plan.md")
			s.DeleteComment("plan.md", c.ID)
		}()
	}
	wg.Wait()
}

func TestSession_Subscribe(t *testing.T) {
	s := newTestSession(t)
	ch := s.Subscribe()
	defer s.Unsubscribe(ch)

	event := SSEEvent{Type: "file-changed", Content: "test"}
	s.notify(event)

	received := <-ch
	if received.Type != "file-changed" {
		t.Errorf("unexpected event type: %s", received.Type)
	}
}

func TestSession_GetSessionInfo(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "note", "", "")
	s.Files[1].DiffHunks = []DiffHunk{
		{Lines: []DiffLine{
			{Type: "add"},
			{Type: "add"},
			{Type: "del"},
			{Type: "context"},
		}},
	}

	info := s.GetSessionInfo()
	if info.ReviewRound != 1 {
		t.Errorf("ReviewRound = %d", info.ReviewRound)
	}
	if len(info.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(info.Files))
	}

	// plan.md
	if info.Files[0].CommentCount != 1 {
		t.Errorf("plan.md comment count = %d", info.Files[0].CommentCount)
	}
	// main.go
	if info.Files[1].Additions != 2 {
		t.Errorf("main.go additions = %d, want 2", info.Files[1].Additions)
	}
	if info.Files[1].Deletions != 1 {
		t.Errorf("main.go deletions = %d, want 1", info.Files[1].Deletions)
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"plan.md", "markdown"},
		{"README.MD", "markdown"},
		{"doc.markdown", "markdown"},
		{"main.go", "code"},
		{"server.py", "code"},
		{"index.html", "code"},
		{"Makefile", "code"},
	}
	for _, tc := range tests {
		got := detectFileType(tc.path)
		if got != tc.expected {
			t.Errorf("detectFileType(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}

func TestSession_CritJSONPath_Default(t *testing.T) {
	s := newTestSession(t)
	want := filepath.Join(s.RepoRoot, ".crit.json")
	if got := s.critJSONPath(); got != want {
		t.Errorf("critJSONPath() = %q, want %q", got, want)
	}
}

func TestSession_CritJSONPath_OutputDir(t *testing.T) {
	s := newTestSession(t)
	outDir := t.TempDir()
	s.OutputDir = outDir

	want := filepath.Join(outDir, ".crit.json")
	if got := s.critJSONPath(); got != want {
		t.Errorf("critJSONPath() = %q, want %q", got, want)
	}
}

func TestSession_WriteFiles_OutputDir(t *testing.T) {
	s := newTestSession(t)
	outDir := t.TempDir()
	s.OutputDir = outDir

	s.AddComment("plan.md", 1, 1, "", "output dir comment", "", "")
	flushWrites(s)
	s.WriteFiles()

	// Should be written to OutputDir, not RepoRoot
	outPath := filepath.Join(outDir, ".crit.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf(".crit.json not written to output dir: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if len(cj.Files["plan.md"].Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(cj.Files["plan.md"].Comments))
	}

	// Should NOT exist in RepoRoot
	repoPath := filepath.Join(s.RepoRoot, ".crit.json")
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Error("expected .crit.json to NOT be written to RepoRoot when OutputDir is set")
	}
}

func TestSession_LoadCritJSON_OutputDir(t *testing.T) {
	s := newTestSession(t)
	outDir := t.TempDir()
	s.OutputDir = outDir

	s.AddComment("plan.md", 1, 1, "", "persisted in output dir", "", "")
	flushWrites(s)
	s.WriteFiles()

	// Create a new session pointing to same output dir
	s2 := newTestSession(t)
	s2.OutputDir = outDir
	s2.Files[0].FileHash = s.Files[0].FileHash
	s2.loadCritJSON()

	comments := s2.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "persisted in output dir" {
		t.Errorf("Body = %q", comments[0].Body)
	}
}

func TestGetFileDiffSnapshotScoped_AddedFileUnstagedScope(t *testing.T) {
	// Issue #25: When a file has status "added" (committed on branch, new relative
	// to merge-base) and the user switches to "unstaged" scope, we should NOT show
	// the entire file as a diff. Only truly untracked files should get that treatment.
	s := newTestSession(t)
	// Simulate a file that is "added" relative to merge-base (committed on branch)
	s.Files[1].Status = "added"
	s.Files[1].Content = "package main\n\nfunc main() {}\n"

	result, ok := s.GetFileDiffSnapshotScoped("main.go", "unstaged", "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	hunks := result["hunks"].([]DiffHunk)

	// With "added" status + "unstaged" scope, the bug would show the entire file
	// as added lines (3 lines). The fix should return empty hunks because there
	// are no actual unstaged changes.
	if len(hunks) != 0 {
		totalLines := 0
		for _, h := range hunks {
			totalLines += len(h.Lines)
		}
		t.Errorf("expected 0 hunks for committed 'added' file in unstaged scope, got %d hunks with %d lines", len(hunks), totalLines)
	}
}

func TestGetFileDiffSnapshotScoped_UntrackedFileUnstagedScope(t *testing.T) {
	// Truly untracked files should still show the full file as added in unstaged scope
	s := newTestSession(t)
	s.Files[1].Status = "untracked"
	s.Files[1].Content = "package main\n\nfunc main() {}\n"

	result, ok := s.GetFileDiffSnapshotScoped("main.go", "unstaged", "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	hunks := result["hunks"].([]DiffHunk)

	// Untracked files should show full content as added
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk for untracked file, got %d", len(hunks))
	}
	addCount := 0
	for _, l := range hunks[0].Lines {
		if l.Type == "add" {
			addCount++
		}
	}
	if addCount != 3 {
		t.Errorf("expected 3 added lines, got %d", addCount)
	}
}

func TestSession_GlobalCommentIDs(t *testing.T) {
	s := newTestSession(t)
	c1, _ := s.AddComment("plan.md", 1, 1, "", "md comment", "", "")
	c2, _ := s.AddComment("main.go", 1, 1, "", "go comment", "", "")

	// IDs are globally unique across files
	if !strings.HasPrefix(c1.ID, "c_") || len(c1.ID) != 8 {
		t.Errorf("plan.md first comment ID = %q, want c_ prefix + 6 hex chars", c1.ID)
	}
	if !strings.HasPrefix(c2.ID, "c_") || len(c2.ID) != 8 {
		t.Errorf("main.go first comment ID = %q, want c_ prefix + 6 hex chars", c2.ID)
	}
	if c1.ID == c2.ID {
		t.Errorf("comment IDs should be unique across files, both = %q", c1.ID)
	}
}

// TestNewSessionFromGit_SubdirectoryCwd verifies that diff hunks are correctly
// populated when crit's working directory is a subdirectory of the git repo.
//
// This reproduces GitHub issue #24: `git diff --name-status` returns paths
// relative to the repo root (e.g. "src/main.go"), but `git diff HEAD -- src/main.go`
// interprets the pathspec relative to cwd. From src/, git looks for src/src/main.go
// which doesn't exist, producing empty diff output. The fix sets cmd.Dir to the
// repo root so pathspecs resolve correctly.
func TestNewSessionFromGit_SubdirectoryCwd(t *testing.T) {
	dir := initTestRepo(t)

	// Reset DefaultBranch cache so it detects the test repo's branch
	defaultBranchOnce = sync.Once{}

	// Create a file in a subdirectory and commit it
	writeFile(t, filepath.Join(dir, "src", "main.go"), "package main\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add src/main.go")

	// Make an unstaged modification (the kind that shows in git diff HEAD)
	writeFile(t, filepath.Join(dir, "src", "main.go"), "package main\n\nfunc main() {}\n")

	// Change process cwd to the subdirectory — this is the key trigger.
	// Claude Code or other tools may run crit from a subdirectory of the repo.
	origDir, _ := os.Getwd()
	os.Chdir(filepath.Join(dir, "src"))
	defer os.Chdir(origDir)

	session, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Find the file and verify it has non-empty diff hunks
	for _, f := range session.Files {
		if strings.HasSuffix(f.Path, "main.go") {
			if len(f.DiffHunks) == 0 {
				t.Errorf("file %s has empty diff hunks — git diff pathspec likely failed to resolve from subdirectory cwd", f.Path)
			}
			return
		}
	}
	t.Error("expected to find main.go in session files")
}

// TestNewSessionFromGit_SubdirectoryCwd_UntrackedFiles verifies that untracked files
// are correctly detected with repo-root-relative paths when cwd is a subdirectory.
// git ls-files returns paths relative to cwd, so without cmd.Dir set to the repo root,
// untracked files would get cwd-relative paths that don't match the expected repo layout.
func TestNewSessionFromGit_SubdirectoryCwd_UntrackedFiles(t *testing.T) {
	dir := initTestRepo(t)

	defaultBranchOnce = sync.Once{}

	// Create a subdirectory with an untracked file
	writeFile(t, filepath.Join(dir, "src", "new.go"), "package main\n\nfunc New() {}\n")

	// Also make a tracked change so NewSessionFromGit doesn't fail with "no changed files"
	writeFile(t, filepath.Join(dir, "README.md"), "# Modified\n")

	origDir, _ := os.Getwd()
	os.Chdir(filepath.Join(dir, "src"))
	defer os.Chdir(origDir)

	session, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatal(err)
	}

	// The untracked file should have a repo-root-relative path (src/new.go), not just "new.go"
	for _, f := range session.Files {
		if f.Path == "src/new.go" {
			if len(f.DiffHunks) == 0 {
				t.Error("expected diff hunks for untracked file src/new.go")
			}
			return
		}
	}

	// Show what paths we got for debugging
	var paths []string
	for _, f := range session.Files {
		paths = append(paths, f.Path)
	}
	t.Errorf("expected to find src/new.go in session files, got: %v", paths)
}

// TestNewSessionFromGit_BaseBranchParam verifies that setting defaultBranchOverride
// causes NewSessionFromGit to diff against that branch instead of auto-detecting.
func TestNewSessionFromGit_BaseBranchParam(t *testing.T) {
	dir := initTestRepo(t)

	// Reset DefaultBranch cache
	defaultBranchOnce = sync.Once{}
	defaultBranchOverride = ""

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer func() {
		os.Chdir(origDir)
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}
	}()

	// Create a second branch "base" that acts as our custom base
	runGit(t, dir, "checkout", "-b", "base")
	writeFile(t, filepath.Join(dir, "base.go"), "package main\n")
	runGit(t, dir, "add", "base.go")
	runGit(t, dir, "commit", "-m", "base branch commit")

	// Now create a feature branch off "base" with a new file
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main\n")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "feature commit")

	// Set the override — this is how resolveServerConfig() wires --base-branch
	defaultBranchOverride = "base"

	session, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var paths []string
	for _, f := range session.Files {
		paths = append(paths, f.Path)
	}

	// feature.go should appear (added relative to base), base.go should not
	found := false
	for _, p := range paths {
		if p == "feature.go" {
			found = true
		}
		if p == "base.go" {
			t.Errorf("base.go should not appear — it was committed before the base branch point")
		}
	}
	if !found {
		t.Errorf("feature.go not found in session files: %v", paths)
	}

	// BaseRef should be non-empty (a merge-base commit SHA was computed)
	if session.BaseRef == "" {
		t.Error("session.BaseRef should be set when diffing against a custom base branch")
	}
}

// TestChangeBaseBranch verifies that changing the base branch updates the session's
// BaseRef, BaseBranchName, and file list to reflect the new diff base.
func TestChangeBaseBranch(t *testing.T) {
	dir := initTestRepo(t)

	defaultBranchOnce = sync.Once{}
	defaultBranchOverride = ""

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer func() {
		os.Chdir(origDir)
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}
	}()

	// main has: main.go
	// Create "production" branch off main with extra files
	runGit(t, dir, "checkout", "-b", "production")
	writeFile(t, filepath.Join(dir, "prod.go"), "package main\n")
	runGit(t, dir, "add", "prod.go")
	runGit(t, dir, "commit", "-m", "production commit")

	// Create feature branch off production
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main\n")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "feature commit")

	// Create session with default base (main)
	session, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With main as base, both prod.go and feature.go should appear
	hasProd := false
	hasFeature := false
	for _, f := range session.Files {
		if f.Path == "prod.go" {
			hasProd = true
		}
		if f.Path == "feature.go" {
			hasFeature = true
		}
	}
	if !hasProd {
		t.Error("expected prod.go in file list when diffing against main")
	}
	if !hasFeature {
		t.Error("expected feature.go in file list when diffing against main")
	}
	session.mu.RLock()
	baseName := session.BaseBranchName
	session.mu.RUnlock()
	if baseName != "main" {
		t.Errorf("BaseBranchName = %q, want %q", baseName, "main")
	}

	// Now change base to "production"
	err = session.ChangeBaseBranch("production")
	if err != nil {
		t.Fatalf("ChangeBaseBranch: %v", err)
	}

	// With production as base, only feature.go should appear (not prod.go)
	hasProd = false
	hasFeature = false
	session.mu.RLock()
	for _, f := range session.Files {
		if f.Path == "prod.go" {
			hasProd = true
		}
		if f.Path == "feature.go" {
			hasFeature = true
		}
	}
	baseName = session.BaseBranchName
	baseRef := session.BaseRef
	session.mu.RUnlock()
	if hasProd {
		t.Error("prod.go should NOT appear when diffing against production")
	}
	if !hasFeature {
		t.Error("expected feature.go in file list when diffing against production")
	}
	if baseName != "production" {
		t.Errorf("BaseBranchName = %q, want %q", baseName, "production")
	}
	if baseRef == "" {
		t.Error("BaseRef should be set after changing base branch")
	}
}

// TestChangeBaseBranch_CommentsPreserved verifies that comments on files that still
// appear after changing the base branch are preserved through the transition, and
// that rollback works when the new branch is invalid.
func TestChangeBaseBranch_CommentsPreserved(t *testing.T) {
	dir := initTestRepo(t)

	defaultBranchOnce = sync.Once{}
	defaultBranchOverride = ""

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer func() {
		os.Chdir(origDir)
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}
	}()

	// main has: README.md (initial commit)
	// Create "production" branch with prod.go
	runGit(t, dir, "checkout", "-b", "production")
	writeFile(t, filepath.Join(dir, "prod.go"), "package main\n")
	runGit(t, dir, "add", "prod.go")
	runGit(t, dir, "commit", "-m", "production commit")

	// Create feature branch with feature.go
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main\nfunc Feature() {}\n")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "feature commit")

	// Create session (base=main, so both prod.go and feature.go appear)
	session, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("NewSessionFromGit: %v", err)
	}

	// Add a comment on feature.go (should survive base branch change)
	_, ok := session.AddComment("feature.go", 1, 1, "", "keep this comment", "", "")
	if !ok {
		t.Fatal("AddComment on feature.go failed")
	}
	// Add a comment on prod.go (should be lost when switching base to production)
	_, ok = session.AddComment("prod.go", 1, 1, "", "will disappear", "", "")
	if !ok {
		t.Fatal("AddComment on prod.go failed")
	}

	// Switch base to production — feature.go remains, prod.go drops out
	if err := session.ChangeBaseBranch("production"); err != nil {
		t.Fatalf("ChangeBaseBranch(production): %v", err)
	}

	// feature.go should still have its comment
	featureComments := session.GetComments("feature.go")
	if len(featureComments) != 1 {
		t.Fatalf("expected 1 comment on feature.go, got %d", len(featureComments))
	}
	if featureComments[0].Body != "keep this comment" {
		t.Errorf("comment body = %q, want %q", featureComments[0].Body, "keep this comment")
	}

	// prod.go should no longer be in the session
	if session.FileByPath("prod.go") != nil {
		t.Error("prod.go should not appear when base is production")
	}

	// Rollback: changing to a non-existent branch should fail and preserve state
	session.mu.RLock()
	baseBefore := session.BaseBranchName
	session.mu.RUnlock()

	err = session.ChangeBaseBranch("nonexistent-branch")
	if err == nil {
		t.Fatal("expected error for nonexistent branch")
	}

	session.mu.RLock()
	baseAfter := session.BaseBranchName
	session.mu.RUnlock()
	if baseAfter != baseBefore {
		t.Errorf("BaseBranchName changed to %q after failed switch, want %q", baseAfter, baseBefore)
	}
}

// TestNewSessionFromFiles_BaseBranch verifies that setting defaultBranchOverride
// causes NewSessionFromFiles to compute a baseRef against the override branch.
func TestNewSessionFromFiles_BaseBranch(t *testing.T) {
	dir := initTestRepo(t)

	defaultBranchOnce = sync.Once{}
	defaultBranchOverride = ""

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer func() {
		os.Chdir(origDir)
		defaultBranchOverride = ""
		defaultBranchOnce = sync.Once{}
	}()

	// Create a "base" branch with one file
	runGit(t, dir, "checkout", "-b", "base")
	writeFile(t, filepath.Join(dir, "base.go"), "package main\n")
	runGit(t, dir, "add", "base.go")
	runGit(t, dir, "commit", "-m", "base branch commit")

	// Create a "feature" branch off "base" with an additional file
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main\n")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "feature commit")

	// Set the override — same mechanism as resolveServerConfig()
	defaultBranchOverride = "base"

	session, err := NewSessionFromFiles([]string{filepath.Join(dir, "feature.go")}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BaseRef should be set since we're on "feature", not "base"
	if session.BaseRef == "" {
		t.Error("session.BaseRef should be set when defaultBranchOverride points to a different branch")
	}
}

// TestParseUnifiedDiff_WithANSIColors verifies that ANSI color codes in git diff
// output break ParseUnifiedDiff. This motivates the --no-color flag on git commands.
func TestParseUnifiedDiff_WithANSIColors(t *testing.T) {
	// Simulate git diff output with color.diff=always — ANSI codes wrap the @@ header and +/- lines
	coloredDiff := "" +
		"\033[1mdiff --git a/file.go b/file.go\033[m\n" +
		"\033[1mindex abc..def 100644\033[m\n" +
		"\033[1m--- a/file.go\033[m\n" +
		"\033[1m+++ b/file.go\033[m\n" +
		"\033[36m@@ -1,3 +1,3 @@\033[m\n" +
		" line1\n" +
		"\033[31m-old line\033[m\n" +
		"\033[32m+new line\033[m\n" +
		" line3\n"

	hunks := ParseUnifiedDiff(coloredDiff)

	// With ANSI codes wrapping the @@ header, the regex won't match and
	// ParseUnifiedDiff returns no hunks — this is the bug that --no-color prevents.
	if len(hunks) != 0 {
		t.Skip("ANSI-colored @@ headers parsed successfully (unexpected) — --no-color is still good defense")
	}

	// Verify that clean (no-color) output parses correctly
	cleanDiff := "" +
		"diff --git a/file.go b/file.go\n" +
		"index abc..def 100644\n" +
		"--- a/file.go\n" +
		"+++ b/file.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" line1\n" +
		"-old line\n" +
		"+new line\n" +
		" line3\n"

	hunks = ParseUnifiedDiff(cleanDiff)
	if len(hunks) != 1 {
		t.Errorf("clean diff: expected 1 hunk, got %d", len(hunks))
	}
}

// TestSession_CarryForward_PreservesAuthor verifies that when comments are carried
// forward from a previous round, the Author field is preserved.
func TestSession_CarryForward_PreservesAuthor(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json with a comment that has an author set (e.g. from crit pull)
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{
						ID:        "c1",
						StartLine: 2,
						EndLine:   2,
						Body:      "missing error check",
						Author:    "reviewer-bot",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(s.critJSONPath(), data, 0644); err != nil {
		t.Fatalf("writing .crit.json: %v", err)
	}

	// No active comments on main.go — simulate carry-forward by calling
	// loadResolvedComments (populates PreviousComments) then handleRoundCompleteFiles.
	s.loadResolvedComments()
	s.handleRoundCompleteFiles()

	comments := s.GetComments("main.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 carried comment, got %d", len(comments))
	}
	if comments[0].Author != "reviewer-bot" {
		t.Errorf("Author = %q, want %q", comments[0].Author, "reviewer-bot")
	}
	if comments[0].Body != "missing error check" {
		t.Errorf("Body = %q", comments[0].Body)
	}
	if !comments[0].CarriedForward {
		t.Error("expected CarriedForward = true")
	}
}

func TestCarryForward_FileScopeComments_NoDuplicates(t *testing.T) {
	s := newTestSession(t)

	fileComment := Comment{
		ID:        "c2",
		Body:      "file-level observation",
		Author:    "Tomasz",
		Scope:     "file",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	// Simulate the state before carry-forward: the file already has the comment
	// in Comments (loaded from .crit.json at session start), and PreviousComments
	// is also set (by loadResolvedComments before round-complete).
	for _, f := range s.Files {
		if f.Path == "plan.md" {
			f.Comments = []Comment{fileComment}
			f.PreviousComments = []Comment{fileComment}
			f.PreviousContent = f.Content
			break
		}
	}

	s.carryForwardComments()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Errorf("expected 1 comment after carry-forward, got %d (duplicates!)", len(comments))
	}
	if len(comments) > 0 && comments[0].Body != "file-level observation" {
		t.Errorf("Body = %q", comments[0].Body)
	}
}

func TestCarryForward_FileScopeComments_NoDuplicatesAcrossRounds(t *testing.T) {
	s := newTestSession(t)

	fileComment := Comment{
		ID:        "c2",
		Body:      "file-level observation",
		Author:    "Tomasz",
		Scope:     "file",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	// Write .crit.json with the file-level comment
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {
				Status:   "added",
				Comments: []Comment{fileComment},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(s.critJSONPath(), data, 0644); err != nil {
		t.Fatalf("writing .crit.json: %v", err)
	}

	// Set PreviousContent to trigger carryForwardComments (markdown file path)
	for _, f := range s.Files {
		if f.Path == "plan.md" {
			f.PreviousContent = f.Content
			break
		}
	}

	// Round 1: carry forward
	s.loadResolvedComments()
	s.carryForwardComments()
	s.carryForwardAllComments()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("round 1: expected 1 comment, got %d", len(comments))
	}

	// Save state to .crit.json (simulating session save)
	cj.Files = map[string]CritJSONFile{
		"plan.md": {
			Status:   "added",
			Comments: comments,
		},
	}
	data, _ = json.MarshalIndent(cj, "", "  ")
	os.WriteFile(s.critJSONPath(), data, 0644)

	// Set up for round 2
	for _, f := range s.Files {
		if f.Path == "plan.md" {
			f.PreviousContent = f.Content
			break
		}
	}

	// Round 2: carry forward again
	s.loadResolvedComments()
	s.carryForwardComments()
	s.carryForwardAllComments()

	comments = s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Errorf("round 2: expected 1 comment, got %d (duplicates!)", len(comments))
	}
}

func TestCarryForward_MixedScopeComments(t *testing.T) {
	s := newTestSession(t)

	fileComment := Comment{
		ID:        "c1",
		Body:      "file-level note",
		Scope:     "file",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}
	lineComment := Comment{
		ID:        "c2",
		StartLine: 3,
		EndLine:   3,
		Body:      "line-level note",
		Scope:     "line",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	for _, f := range s.Files {
		if f.Path == "plan.md" {
			f.Comments = []Comment{fileComment, lineComment}
			f.PreviousComments = []Comment{fileComment, lineComment}
			f.PreviousContent = f.Content
			break
		}
	}

	s.carryForwardComments()

	comments := s.GetComments("plan.md")
	if len(comments) != 2 {
		t.Errorf("expected 2 comments (one file, one line), got %d", len(comments))
	}
	scopes := map[string]int{}
	for _, c := range comments {
		scopes[c.Scope]++
	}
	if scopes["file"] != 1 {
		t.Errorf("expected 1 file-scope comment, got %d", scopes["file"])
	}
	if scopes["line"] != 1 {
		t.Errorf("expected 1 line-scope comment, got %d", scopes["line"])
	}
}

func TestAddCommentSetsReviewRound(t *testing.T) {
	s := newTestSession(t)
	s.ReviewRound = 2

	c, ok := s.AddComment("plan.md", 1, 1, "", "test body", "", "user")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.ReviewRound != 2 {
		t.Errorf("ReviewRound = %d, want 2", c.ReviewRound)
	}
}

func TestCarryForwardPreservesReviewRound(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json with a comment that has ReviewRound set
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{
						ID:          "c1",
						StartLine:   2,
						EndLine:     2,
						Body:        "round 1 feedback",
						Author:      "reviewer",
						CreatedAt:   "2026-01-01T00:00:00Z",
						UpdatedAt:   "2026-01-01T00:00:00Z",
						ReviewRound: 1,
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(s.critJSONPath(), data, 0644); err != nil {
		t.Fatalf("writing .crit.json: %v", err)
	}

	// Load previous comments and trigger carry-forward
	s.loadResolvedComments()
	s.handleRoundCompleteFiles()

	comments := s.GetComments("main.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 carried comment, got %d", len(comments))
	}
	if comments[0].ReviewRound != 1 {
		t.Errorf("ReviewRound = %d, want 1", comments[0].ReviewRound)
	}
	if !comments[0].CarriedForward {
		t.Error("expected CarriedForward = true")
	}
}

// TestFileDiffUnified_ColorConfigDoesNotBreakParsing verifies that even with
// color.diff=always in gitconfig, the --no-color flag produces parseable output.
func TestFileDiffUnified_ColorConfigDoesNotBreakParsing(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Set color.diff=always in the repo config (simulates a user's gitconfig)
	runGit(t, dir, "config", "color.diff", "always")

	// Modify a file to create a diff
	writeFile(t, filepath.Join(dir, "README.md"), "# Modified\n\nNew content\n")

	// fileDiffUnified uses --no-color, so it should parse correctly despite the config
	hunks, err := fileDiffUnified("README.md", "HEAD", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected non-empty diff hunks even with color.diff=always configured")
	}
}

func TestSession_WriteFiles_MergesExternalComments(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		BaseRef:     "abc123",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", FileHash: "hash1", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "from browser", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Simulate an external tool writing a comment to .crit.json that the session doesn't know about
	cj := CritJSON{
		Branch: "main", BaseRef: "abc123", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified", FileHash: "hash1",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "from browser", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					{ID: "c2", StartLine: 10, EndLine: 10, Body: "from CLI", Author: "Claude", CreatedAt: "2026-01-01T00:00:01Z", UpdatedAt: "2026-01-01T00:00:01Z"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	// WriteFiles should merge, not overwrite
	s.WriteFiles()

	// Read back .crit.json and verify both comments are present
	result, _ := os.ReadFile(filepath.Join(dir, ".crit.json"))
	var got CritJSON
	json.Unmarshal(result, &got)

	if len(got.Files["main.go"].Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(got.Files["main.go"].Comments))
	}

	found := false
	for _, c := range got.Files["main.go"].Comments {
		if c.ID == "c2" && c.Body == "from CLI" {
			found = true
		}
	}
	if !found {
		t.Error("external comment c2 was lost during WriteFiles")
	}
}

func TestSession_MergeExternalCritJSON_NewComment(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		BaseRef:     "abc123",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	ch := s.Subscribe()
	defer s.Unsubscribe(ch)

	// Simulate external write of .crit.json with a new comment
	cj := CritJSON{
		Branch: "main", BaseRef: "abc123", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 5, EndLine: 5, Body: "from CLI", Author: "Claude", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	changed := s.mergeExternalCritJSON()
	if !changed {
		t.Fatal("expected mergeExternalCritJSON to detect changes")
	}

	comments := s.GetComments("main.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "from CLI" || comments[0].Author != "Claude" {
		t.Errorf("unexpected comment: %+v", comments[0])
	}

	select {
	case event := <-ch:
		if event.Type != "comments-changed" {
			t.Errorf("expected comments-changed, got %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestSession_MergeExternalCritJSON_NoChange(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		ReviewRound: 1,
		Files:       []*FileEntry{},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// No .crit.json exists — should return false
	changed := s.mergeExternalCritJSON()
	if changed {
		t.Error("expected no change when .crit.json doesn't exist")
	}
}

func TestSession_MergeExternalCritJSON_IgnoresOwnWrites(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		BaseRef:     "abc123",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "existing"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write via WriteFiles and record mtime as our own
	s.WriteFiles()

	// mergeExternalCritJSON should see mtime matches and skip
	changed := s.mergeExternalCritJSON()
	if changed {
		t.Error("expected no change after our own write")
	}
}

func TestSession_MergeExternalCritJSON_ClearDetected(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "existing"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write .crit.json with no comments (simulating crit comment --clear)
	cj := CritJSON{Branch: "main", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	changed := s.mergeExternalCritJSON()
	if !changed {
		t.Fatal("expected change detected on clear")
	}

	comments := s.GetComments("main.go")
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after clear, got %d", len(comments))
	}
}

func TestLoadCritJSON_IgnoresStaleShareState(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit.json")

	// Write .crit.json with share state for a different file set
	scope := shareScope([]string{"old-plan.md"})
	cj := CritJSON{
		ShareURL:    "https://crit.md/r/old",
		DeleteToken: "old-token",
		ShareScope:  scope,
		Files:       map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(critPath, data, 0644)

	// Create session with DIFFERENT files
	sess := &Session{
		OutputDir:   dir,
		Files:       []*FileEntry{{Path: "new-plan.md", Content: "# New"}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	sess.loadCritJSON()

	url, token := sess.GetShareState()
	if url != "" || token != "" {
		t.Errorf("expected stale share state to be ignored, got url=%q token=%q", url, token)
	}
}

func TestLoadCritJSON_RestoresMatchingShareState(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit.json")

	scope := shareScope([]string{"plan.md"})
	cj := CritJSON{
		ShareURL:    "https://crit.md/r/current",
		DeleteToken: "current-token",
		ShareScope:  scope,
		Files:       map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(critPath, data, 0644)

	// Create session with SAME files
	sess := &Session{
		OutputDir:   dir,
		Files:       []*FileEntry{{Path: "plan.md", Content: "# Plan"}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	sess.loadCritJSON()

	url, token := sess.GetShareState()
	if url != "https://crit.md/r/current" {
		t.Errorf("expected share state restored, got url=%q", url)
	}
	if token != "current-token" {
		t.Errorf("expected token restored, got token=%q", token)
	}
}

// TestSession_LoadCritJSON_RestoresReviewRound verifies that when crit restarts
// with an existing .crit.json, the ReviewRound is restored from the file.
// Without this, the session starts at round 1 while comments claim higher rounds,
// causing mismatches between the UI header and comment badges.
func TestComment_RepliesJSON(t *testing.T) {
	c := Comment{
		ID:        "c1",
		StartLine: 10,
		EndLine:   15,
		Body:      "Fix this",
		CreatedAt: "2025-01-01T00:00:00Z",
		UpdatedAt: "2025-01-01T00:00:00Z",
		Replies: []Reply{
			{
				ID:        "c1-r1",
				Body:      "Done, split the function",
				Author:    "agent",
				CreatedAt: "2025-01-01T00:01:00Z",
			},
		},
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Comment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(decoded.Replies))
	}
	if decoded.Replies[0].ID != "c1-r1" {
		t.Errorf("reply ID = %q, want %q", decoded.Replies[0].ID, "c1-r1")
	}
	if decoded.Replies[0].Body != "Done, split the function" {
		t.Errorf("reply body = %q, want %q", decoded.Replies[0].Body, "Done, split the function")
	}
}

func TestComment_NoRepliesBackwardCompat(t *testing.T) {
	// Old .crit.json format with deprecated resolution_note — silently ignored by Go JSON decoder
	data := `{"id":"c1","start_line":5,"end_line":5,"body":"Fix","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","resolution_note":"Done"}`

	var c Comment
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		t.Fatal(err)
	}
	if c.Replies != nil {
		t.Errorf("expected nil replies, got %v", c.Replies)
	}
	// resolution_note is no longer in the struct — Go silently ignores unknown JSON fields
}

func TestSession_AddReply(t *testing.T) {
	s := &Session{
		ReviewRound: 1,

		Files: []*FileEntry{
			{
				Path:     "test.md",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"}},
			},
		},
	}

	reply, ok := s.AddReply("test.md", "c1", "Done, fixed it", "agent")
	if !ok {
		t.Fatal("AddReply returned false")
	}
	if !strings.HasPrefix(reply.ID, "rp_") || len(reply.ID) != 9 {
		t.Errorf("reply ID = %q, want rp_ prefix + 6 hex chars", reply.ID)
	}
	if reply.Body != "Done, fixed it" {
		t.Errorf("reply body = %q", reply.Body)
	}
	if reply.Author != "agent" {
		t.Errorf("reply author = %q", reply.Author)
	}

	comments := s.GetComments("test.md")
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
}

func TestSession_AddReply_UnresolvesComment(t *testing.T) {
	s := &Session{
		ReviewRound: 1,

		Files: []*FileEntry{
			{
				Path:     "test.md",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this", Resolved: true}},
			},
		},
	}

	// Verify comment is resolved before reply
	comments := s.GetComments("test.md")
	if !comments[0].Resolved {
		t.Fatal("expected comment to be resolved before reply")
	}

	_, ok := s.AddReply("test.md", "c1", "Actually, this needs more work", "reviewer")
	if !ok {
		t.Fatal("AddReply returned false")
	}

	// Comment should be unresolves after reply
	comments = s.GetComments("test.md")
	if comments[0].Resolved {
		t.Error("expected comment to be unresolved after reply, but it is still resolved")
	}
}

func TestSession_UpdateReply(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path: "test.md",
				Comments: []Comment{{
					ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix",
					Replies: []Reply{{ID: "c1-r1", Body: "Done", Author: "agent"}},
				}},
			},
		},
	}

	reply, ok := s.UpdateReply("test.md", "c1", "c1-r1", "Actually, refactored instead")
	if !ok {
		t.Fatal("UpdateReply returned false")
	}
	if reply.Body != "Actually, refactored instead" {
		t.Errorf("reply body = %q", reply.Body)
	}
}

func TestSession_DeleteReply(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path: "test.md",
				Comments: []Comment{{
					ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix",
					Replies: []Reply{
						{ID: "c1-r1", Body: "Done", Author: "agent"},
						{ID: "c1-r2", Body: "More changes", Author: "user"},
					},
				}},
			},
		},
	}

	ok := s.DeleteReply("test.md", "c1", "c1-r1")
	if !ok {
		t.Fatal("DeleteReply returned false")
	}
	comments := s.GetComments("test.md")
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
	if comments[0].Replies[0].ID != "c1-r2" {
		t.Errorf("remaining reply ID = %q", comments[0].Replies[0].ID)
	}
}

func TestSession_AddReply_SequentialIDs(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:     "test.md",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix"}},
			},
		},
	}

	r1, _ := s.AddReply("test.md", "c1", "First reply", "agent")
	r2, _ := s.AddReply("test.md", "c1", "Second reply", "user")
	r3, _ := s.AddReply("test.md", "c1", "Third reply", "agent")

	// All reply IDs should have rp_ prefix and be unique
	for _, r := range []Reply{r1, r2, r3} {
		if !strings.HasPrefix(r.ID, "rp_") || len(r.ID) != 9 {
			t.Errorf("reply ID = %q, want rp_ prefix + 6 hex chars", r.ID)
		}
	}
	if r1.ID == r2.ID || r2.ID == r3.ID || r1.ID == r3.ID {
		t.Errorf("reply IDs should be unique: %q, %q, %q", r1.ID, r2.ID, r3.ID)
	}
}

func TestSession_LoadCritJSON_RestoresReviewRound(t *testing.T) {
	s := newTestSession(t)

	// Simulate a .crit.json left over from a previous session at round 3
	cj := CritJSON{
		ReviewRound: 3,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Status: "added",
				Comments: []Comment{
					{
						ID:          "c1",
						StartLine:   1,
						EndLine:     1,
						Body:        "round 1 feedback",
						CreatedAt:   "2026-01-01T00:00:00Z",
						UpdatedAt:   "2026-01-01T00:00:00Z",
						ReviewRound: 1,
					},
					{
						ID:          "c2",
						StartLine:   3,
						EndLine:     3,
						Body:        "round 2 feedback",
						CreatedAt:   "2026-01-02T00:00:00Z",
						UpdatedAt:   "2026-01-02T00:00:00Z",
						ReviewRound: 2,
					},
				},
			},
		},
	}
	data, _ := json.Marshal(cj)
	writeFile(t, filepath.Join(s.RepoRoot, ".crit.json"), string(data))

	s.loadCritJSON()

	// ReviewRound should be restored from .crit.json
	if s.GetReviewRound() != 3 {
		t.Errorf("ReviewRound = %d after loadCritJSON, want 3 (value from .crit.json)", s.GetReviewRound())
	}

	// Comments should retain their original review_round values
	comments := s.GetComments("plan.md")
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ReviewRound != 1 {
		t.Errorf("first comment ReviewRound = %d, want 1", comments[0].ReviewRound)
	}
	if comments[1].ReviewRound != 2 {
		t.Errorf("second comment ReviewRound = %d, want 2", comments[1].ReviewRound)
	}

	// New comments should get the restored round number
	c, ok := s.AddComment("plan.md", 5, 5, "", "round 3 feedback", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.ReviewRound != 3 {
		t.Errorf("new comment ReviewRound = %d, want 3", c.ReviewRound)
	}
}

func TestCarryForwardComment(t *testing.T) {
	old := Comment{
		ID:             "original-id",
		StartLine:      5,
		EndLine:        10,
		Side:           "RIGHT",
		Body:           "needs refactoring",
		Quote:          "func foo() {}",
		Author:         "reviewer-bot",
		CreatedAt:      "2026-01-01T00:00:00Z",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		Resolved:       true,
		CarriedForward: false,
		ReviewRound:    1,
	}

	carried := carryForwardComment(old, "c42", "2026-02-01T00:00:00Z")

	if carried.ID != "c42" {
		t.Errorf("ID = %q, want c42", carried.ID)
	}
	if carried.StartLine != 5 {
		t.Errorf("StartLine = %d, want 5", carried.StartLine)
	}
	if carried.EndLine != 10 {
		t.Errorf("EndLine = %d, want 10", carried.EndLine)
	}
	if carried.Side != "RIGHT" {
		t.Errorf("Side = %q, want RIGHT", carried.Side)
	}
	if carried.Body != "needs refactoring" {
		t.Errorf("Body = %q", carried.Body)
	}
	if carried.Author != "reviewer-bot" {
		t.Errorf("Author = %q, want reviewer-bot", carried.Author)
	}
	if carried.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("CreatedAt = %q, want original timestamp", carried.CreatedAt)
	}
	if carried.UpdatedAt != "2026-02-01T00:00:00Z" {
		t.Errorf("UpdatedAt = %q, want new timestamp", carried.UpdatedAt)
	}
	if !carried.Resolved {
		t.Error("Resolved should be preserved as true")
	}
	if !carried.CarriedForward {
		t.Error("CarriedForward should be true")
	}
	if carried.ReviewRound != 1 {
		t.Errorf("ReviewRound = %d, want 1", carried.ReviewRound)
	}
	if carried.Quote != "func foo() {}" {
		t.Errorf("Quote = %q, want %q", carried.Quote, "func foo() {}")
	}
}

func TestSession_CarryForward_PreservesReplies(t *testing.T) {
	c := Comment{
		ID: "c1", StartLine: 5, EndLine: 5, Body: "Fix this",
		CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z",
		Replies: []Reply{
			{ID: "c1-r1", Body: "Done", Author: "agent", CreatedAt: "2025-01-01T00:01:00Z"},
		},
	}

	// Simulate carry-forward: marshal/unmarshal round-trip
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var carried Comment
	if err := json.Unmarshal(data, &carried); err != nil {
		t.Fatal(err)
	}
	carried.CarriedForward = true

	if len(carried.Replies) != 1 {
		t.Fatalf("replies lost during carry-forward: got %d", len(carried.Replies))
	}
	if carried.Replies[0].Body != "Done" {
		t.Errorf("reply body = %q after carry-forward", carried.Replies[0].Body)
	}
	if carried.Replies[0].ID != "c1-r1" {
		t.Errorf("reply ID = %q after carry-forward", carried.Replies[0].ID)
	}
}

func TestSession_MergeExternalCritJSON_SkippedDuringPendingWrite(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "existing"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write initial .crit.json so merge has something to read
	s.WriteFiles()

	// Delete the comment (this sets pendingWrite)
	s.DeleteComment("main.go", "c1")

	// Write .crit.json externally with the old comment still present
	cj := CritJSON{
		Branch: "main", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "existing"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	// Touch with different mtime to bypass own-write check
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	// Merge should be skipped because a write is pending
	changed := s.mergeExternalCritJSON()
	if changed {
		t.Error("expected merge to be skipped during pending write")
	}

	// Comment should still be deleted
	comments := s.GetComments("main.go")
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d — deleted comment was re-added", len(comments))
	}
}

func TestSession_MergeExternalCritJSON_SyncsResolvedState(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "fix this", Resolved: false},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	s.WriteFiles()

	cj := CritJSON{
		Branch: "main", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "fix this", Resolved: true},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	changed := s.mergeExternalCritJSON()
	if !changed {
		t.Fatal("expected change detected")
	}

	comments := s.GetComments("main.go")
	if !comments[0].Resolved {
		t.Error("expected comment to be resolved after external edit")
	}
}

func TestSession_EnsureFileEntry_NewFile(t *testing.T) {
	dir := t.TempDir()
	// Create a file on disk that is NOT in the session
	newFilePath := filepath.Join(dir, "newfile.py")
	writeFile(t, newFilePath, "def hello():\n    print('hi')\n")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:     "existing.go",
				AbsPath:  filepath.Join(dir, "existing.go"),
				Status:   "modified",
				FileType: "code",
				Content:  "package main\n",
				FileHash: "sha256:test",
				Comments: []Comment{},
			},
		},

		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	// File not in session, should not be findable
	if s.FileByPath("newfile.py") != nil {
		t.Fatal("file should not exist in session yet")
	}

	// EnsureFileEntry should add it
	ok := s.EnsureFileEntry("newfile.py")
	if !ok {
		t.Fatal("EnsureFileEntry returned false")
	}

	// Now it should be findable
	f := s.FileByPath("newfile.py")
	if f == nil {
		t.Fatal("file not found after EnsureFileEntry")
	}
	if f.Content != "def hello():\n    print('hi')\n" {
		t.Errorf("unexpected content: %q", f.Content)
	}
	if f.FileType != "code" {
		t.Errorf("FileType = %q, want code", f.FileType)
	}
	if f.FileHash == "" {
		t.Error("FileHash should be set")
	}
	if len(f.Comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(f.Comments))
	}
}

func TestSession_EnsureFileEntry_AlreadyExists(t *testing.T) {
	s := newTestSession(t)

	// plan.md is already in the session
	ok := s.EnsureFileEntry("plan.md")
	if !ok {
		t.Fatal("EnsureFileEntry returned false for existing file")
	}

	// Should still have exactly 2 files (no duplicate added)
	s.mu.RLock()
	count := len(s.Files)
	s.mu.RUnlock()
	if count != 2 {
		t.Errorf("expected 2 files, got %d (duplicate may have been added)", count)
	}
}

func TestSession_EnsureFileEntry_NonexistentFile(t *testing.T) {
	s := newTestSession(t)

	ok := s.EnsureFileEntry("does-not-exist.txt")
	if ok {
		t.Error("EnsureFileEntry should return false for file that doesn't exist on disk")
	}
}

func TestSession_EnsureFileEntry_ThenAddComment(t *testing.T) {
	dir := t.TempDir()
	newFilePath := filepath.Join(dir, "runtime.py")
	writeFile(t, newFilePath, "# Runtime file\ndef greet():\n    pass\n")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		ReviewRound: 1,

		Files:         []*FileEntry{},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	// Ensure the file is added to the session
	ok := s.EnsureFileEntry("runtime.py")
	if !ok {
		t.Fatal("EnsureFileEntry failed")
	}

	// Now AddComment should work
	c, ok := s.AddComment("runtime.py", 2, 2, "", "Add docstring", "", "reviewer")
	if !ok {
		t.Fatal("AddComment failed after EnsureFileEntry")
	}
	if !strings.HasPrefix(c.ID, "c_") || len(c.ID) != 8 {
		t.Errorf("comment ID = %q, want c_ prefix + 6 hex chars", c.ID)
	}
	if c.Body != "Add docstring" {
		t.Errorf("comment body = %q", c.Body)
	}

	// Verify the comment is retrievable
	comments := s.GetComments("runtime.py")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
}

func TestGetFileDiffSnapshotScoped_RuntimeFile(t *testing.T) {
	// A file that exists on disk but is NOT in s.Files should still get
	// a proper all-addition diff when it's untracked.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "newfile.py"), "line1\nline2\nline3\n")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		ReviewRound: 1,
		Files:       []*FileEntry{}, // file NOT in session
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// When the file status can't be determined from git (no git repo),
	// we still get a valid response (empty hunks, not a 404).
	result, ok := s.GetFileDiffSnapshotScoped("newfile.py", "unstaged", "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	hunks := result["hunks"].([]DiffHunk)
	// Without a git repo, ChangedFilesScoped will fail, so status stays empty
	// and we fall through to FileDiffScoped which also fails -> empty hunks.
	// The key thing is we don't panic or return !ok.
	_ = hunks
}

func TestSession_MergeExternalCritJSON_SyncsUnresolve(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "fix this", Resolved: true},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	s.WriteFiles()

	cj := CritJSON{
		Branch: "main", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "fix this", Resolved: false},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	changed := s.mergeExternalCritJSON()
	if !changed {
		t.Fatal("expected change detected")
	}

	comments := s.GetComments("main.go")
	if comments[0].Resolved {
		t.Error("expected comment to be unresolved after external edit")
	}
}

func TestCommentScopeDefault(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddComment("plan.md", 1, 1, "", "test body", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.Scope != "line" {
		t.Errorf("expected scope 'line', got %q", c.Scope)
	}
}

func TestAddFileComment(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddFileComment("plan.md", "this file needs work", "")
	if !ok {
		t.Fatal("AddFileComment failed")
	}
	if c.Scope != "file" {
		t.Errorf("expected scope 'file', got %q", c.Scope)
	}
	if c.StartLine != 0 || c.EndLine != 0 {
		t.Errorf("expected zero lines for file comment, got %d-%d", c.StartLine, c.EndLine)
	}
	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
}

func TestAddReviewComment(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("please address all issues", "")
	if c.Scope != "review" {
		t.Errorf("expected scope 'review', got %q", c.Scope)
	}
	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	comments := s.GetReviewComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(comments))
	}
}

func TestDeleteReviewComment(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("temp", "")
	if !s.DeleteReviewComment(c.ID) {
		t.Fatal("DeleteReviewComment failed")
	}
	if len(s.GetReviewComments()) != 0 {
		t.Error("expected 0 review comments after delete")
	}
}

func TestUpdateReviewComment(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("original", "")
	updated, ok := s.UpdateReviewComment(c.ID, "revised")
	if !ok {
		t.Fatal("UpdateReviewComment failed")
	}
	if updated.Body != "revised" {
		t.Errorf("expected 'revised', got %q", updated.Body)
	}
}

func TestCritJSONIncludesReviewComments(t *testing.T) {
	s := newTestSession(t)
	s.AddReviewComment("general feedback", "")
	s.AddComment("plan.md", 1, 1, "", "line comment", "", "")
	s.AddFileComment("plan.md", "file comment", "")
	s.WriteFiles()
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(cj.ReviewComments))
	}
	if cj.ReviewComments[0].Scope != "review" {
		t.Errorf("expected scope 'review', got %q", cj.ReviewComments[0].Scope)
	}
	fc := cj.Files["plan.md"]
	if len(fc.Comments) != 2 {
		t.Fatalf("expected 2 comments on plan.md, got %d", len(fc.Comments))
	}
}

func TestLoadCritJSONRestoresReviewComments(t *testing.T) {
	s := newTestSession(t)
	s.AddReviewComment("restored comment", "")
	s.WriteFiles()
	s.reviewComments = nil

	s.loadCritJSON()
	rc := s.GetReviewComments()
	if len(rc) != 1 {
		t.Fatalf("expected 1 review comment after reload, got %d", len(rc))
	}
	if rc[0].Body != "restored comment" {
		t.Errorf("unexpected body: %q", rc[0].Body)
	}
}

func TestCommentCountsIncludeReviewComments(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "line", "", "")
	s.AddFileComment("plan.md", "file", "")
	s.AddReviewComment("review", "")
	if got := s.TotalCommentCount(); got != 3 {
		t.Errorf("TotalCommentCount: expected 3, got %d", got)
	}
	if got := s.UnresolvedCommentCount(); got != 3 {
		t.Errorf("UnresolvedCommentCount: expected 3, got %d", got)
	}
}

func TestClearAllCommentsIncludesReview(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "line", "", "")
	s.AddReviewComment("review", "")
	s.ClearAllComments()
	if got := s.TotalCommentCount(); got != 0 {
		t.Errorf("expected 0 after clear, got %d", got)
	}
	if len(s.GetReviewComments()) != 0 {
		t.Error("expected 0 review comments after clear")
	}
}

func TestReviewCommentsSurviveRound(t *testing.T) {
	s := newTestSession(t)
	s.AddReviewComment("carry me forward", "")
	s.WriteFiles()

	// Simulate round: clear in-memory state and reload
	s.reviewComments = nil

	s.loadCritJSON()

	comments := s.GetReviewComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 review comment after round, got %d", len(comments))
	}
	if comments[0].Body != "carry me forward" {
		t.Errorf("unexpected body: %q", comments[0].Body)
	}
}

func TestFileCommentsSurviveRoundWithoutLineMutation(t *testing.T) {
	s := newTestSession(t)
	s.AddFileComment("plan.md", "restructure this", "")
	s.AddComment("plan.md", 1, 1, "", "line comment", "", "")

	// Simulate round: snapshot previous state
	s.mu.Lock()
	f := s.fileByPathLocked("plan.md")
	s.mu.Unlock()
	f.PreviousContent = f.Content
	f.PreviousComments = make([]Comment, len(f.Comments))
	copy(f.PreviousComments, f.Comments)
	f.Comments = nil

	s.carryForwardComments()

	comments := s.GetComments("plan.md")
	var fileComment *Comment
	for i := range comments {
		if comments[i].Scope == "file" {
			fileComment = &comments[i]
			break
		}
	}
	if fileComment == nil {
		t.Fatal("file-level comment was not carried forward")
	}
	if fileComment.StartLine != 0 || fileComment.EndLine != 0 {
		t.Errorf("file comment lines mutated: got %d-%d, want 0-0", fileComment.StartLine, fileComment.EndLine)
	}
}

func TestLoadCritJSONDefaultsScope(t *testing.T) {
	s := newTestSession(t)
	// Write a .crit.json with comments that have no scope field
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c0", StartLine: 1, EndLine: 1, Body: "old comment"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(s.critJSONPath(), data, 0644)

	s.loadCritJSON()
	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Scope != "line" {
		t.Errorf("expected default scope 'line', got %q", comments[0].Scope)
	}
}

func TestResolveReviewComment(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "")
	if c.Resolved {
		t.Error("new review comment should not be resolved")
	}

	// Resolve
	resolved, ok := s.ResolveReviewComment(c.ID, true)
	if !ok {
		t.Fatal("ResolveReviewComment failed")
	}
	if !resolved.Resolved {
		t.Error("expected comment to be resolved")
	}

	// Unresolve
	unresolved, ok := s.ResolveReviewComment(c.ID, false)
	if !ok {
		t.Fatal("ResolveReviewComment (unresolve) failed")
	}
	if unresolved.Resolved {
		t.Error("expected comment to be unresolved")
	}

	// Not found
	_, ok = s.ResolveReviewComment("nonexistent", true)
	if ok {
		t.Error("expected ResolveReviewComment to return false for unknown ID")
	}
}

func TestResolveReviewCommentAffectsUnresolvedCount(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("review", "")
	if got := s.UnresolvedCommentCount(); got != 1 {
		t.Fatalf("expected 1 unresolved, got %d", got)
	}
	s.ResolveReviewComment(c.ID, true)
	if got := s.UnresolvedCommentCount(); got != 0 {
		t.Fatalf("expected 0 unresolved after resolve, got %d", got)
	}
}

func TestFileCommentHasReviewRound(t *testing.T) {
	s := newTestSession(t)
	s.ReviewRound = 3
	c, ok := s.AddFileComment("plan.md", "file-level feedback", "")
	if !ok {
		t.Fatal("AddFileComment failed")
	}
	if c.ReviewRound != 3 {
		t.Errorf("expected ReviewRound 3, got %d", c.ReviewRound)
	}
}

func TestReviewCommentHasReviewRound(t *testing.T) {
	s := newTestSession(t)
	s.ReviewRound = 2
	c := s.AddReviewComment("general feedback", "")
	if c.ReviewRound != 2 {
		t.Errorf("expected ReviewRound 2, got %d", c.ReviewRound)
	}
}

func TestAddReviewCommentReply(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "reviewer")
	reply, ok := s.AddReviewCommentReply(c.ID, "fixed it", "author")
	if !ok {
		t.Fatal("AddReviewCommentReply failed")
	}
	if reply.Body != "fixed it" {
		t.Errorf("expected body 'fixed it', got %q", reply.Body)
	}
	if reply.Author != "author" {
		t.Errorf("expected author 'author', got %q", reply.Author)
	}
	// Verify reply ID format: rp_ prefix + 6 hex chars
	if !strings.HasPrefix(reply.ID, "rp_") || len(reply.ID) != 9 {
		t.Errorf("reply ID = %q, want rp_ prefix + 6 hex chars", reply.ID)
	}
	// Verify the reply is attached to the comment
	comments := s.GetReviewComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
}

func TestAddReviewCommentReply_NotFound(t *testing.T) {
	s := newTestSession(t)
	_, ok := s.AddReviewCommentReply("nonexistent", "body", "author")
	if ok {
		t.Error("expected AddReviewCommentReply to return false for nonexistent comment")
	}
}

func TestUpdateReviewCommentReply(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "reviewer")
	reply, _ := s.AddReviewCommentReply(c.ID, "initial reply", "author")
	updated, ok := s.UpdateReviewCommentReply(c.ID, reply.ID, "updated reply")
	if !ok {
		t.Fatal("UpdateReviewCommentReply failed")
	}
	if updated.Body != "updated reply" {
		t.Errorf("expected body 'updated reply', got %q", updated.Body)
	}
}

func TestUpdateReviewCommentReply_NotFound(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "reviewer")
	_, ok := s.UpdateReviewCommentReply(c.ID, "nonexistent", "body")
	if ok {
		t.Error("expected UpdateReviewCommentReply to return false for nonexistent reply")
	}
}

func TestDeleteReviewCommentReply(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "reviewer")
	reply, _ := s.AddReviewCommentReply(c.ID, "to delete", "author")
	if !s.DeleteReviewCommentReply(c.ID, reply.ID) {
		t.Fatal("DeleteReviewCommentReply failed")
	}
	comments := s.GetReviewComments()
	if len(comments[0].Replies) != 0 {
		t.Errorf("expected 0 replies after delete, got %d", len(comments[0].Replies))
	}
}

func TestDeleteReviewCommentReply_NotFound(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("needs work", "reviewer")
	if s.DeleteReviewCommentReply(c.ID, "nonexistent") {
		t.Error("expected DeleteReviewCommentReply to return false for nonexistent reply")
	}
}

func TestEnsureLoaded(t *testing.T) {
	dir := initTestRepo(t)

	writeFile(t, filepath.Join(dir, "lazy.go"), "package main\n\nfunc lazy() {}\n")
	runGit(t, dir, "add", "lazy.go")
	runGit(t, dir, "commit", "-m", "add lazy.go")
	runGit(t, dir, "checkout", "-b", "feature-lazy")
	writeFile(t, filepath.Join(dir, "lazy.go"), "package main\n\nfunc lazy() {\n\tfmt.Println(\"loaded\")\n}\n")
	runGit(t, dir, "add", "lazy.go")
	runGit(t, dir, "commit", "-m", "modify lazy.go")

	base := strings.TrimSpace(runGit(t, dir, "merge-base", "main", "HEAD"))

	fe := &FileEntry{
		Path:    "lazy.go",
		AbsPath: filepath.Join(dir, "lazy.go"),
		Status:  "modified",
		Lazy:    true,
	}

	if fe.Content != "" {
		t.Fatal("expected empty content before ensureLoaded")
	}
	if len(fe.DiffHunks) != 0 {
		t.Fatal("expected no diff hunks before ensureLoaded")
	}

	err := fe.ensureLoaded(dir, base)
	if err != nil {
		t.Fatalf("ensureLoaded failed: %v", err)
	}

	if fe.Content == "" {
		t.Fatal("expected non-empty content after ensureLoaded")
	}
	if !strings.Contains(fe.Content, "loaded") {
		t.Fatalf("content should contain 'loaded', got: %s", fe.Content)
	}
	if len(fe.DiffHunks) == 0 {
		t.Fatal("expected diff hunks after ensureLoaded")
	}
	if fe.Lazy {
		t.Fatal("expected Lazy=false after ensureLoaded")
	}

	// Second call is a no-op (sync.Once)
	err = fe.ensureLoaded(dir, base)
	if err != nil {
		t.Fatalf("second ensureLoaded should not fail: %v", err)
	}
}

func TestEnsureLoadedNotLazy(t *testing.T) {
	fe := &FileEntry{
		Path:    "eager.go",
		Content: "already loaded",
		Lazy:    false,
	}
	err := fe.ensureLoaded("/tmp", "abc123")
	if err != nil {
		t.Fatalf("ensureLoaded on non-lazy file should be no-op, got: %v", err)
	}
	if fe.Content != "already loaded" {
		t.Fatal("content should be unchanged for non-lazy file")
	}
}

func TestNewSessionFromGitLazyThreshold(t *testing.T) {
	dir := initTestRepo(t)
	defaultBranchOverride = ""
	defer func() { defaultBranchOverride = "" }()

	runGit(t, dir, "checkout", "-b", "feature-many-files")
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("file%03d.go", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("package main\n// file %d\n", i))
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add 120 files")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	s, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("NewSessionFromGit failed: %v", err)
	}

	if len(s.Files) != 120 {
		t.Fatalf("expected 120 files, got %d", len(s.Files))
	}

	eagerCount, lazyCount := 0, 0
	for _, f := range s.Files {
		if f.Lazy {
			lazyCount++
			if f.Content != "" {
				t.Errorf("lazy file %s should not have content loaded", f.Path)
			}
		} else {
			eagerCount++
			if f.Content == "" && f.Status != "deleted" {
				t.Errorf("eager file %s should have content", f.Path)
			}
		}
	}

	if eagerCount != 100 {
		t.Errorf("expected 100 eager files, got %d", eagerCount)
	}
	if lazyCount != 20 {
		t.Errorf("expected 20 lazy files, got %d", lazyCount)
	}
}

func TestNewSessionFromGitUnderThreshold(t *testing.T) {
	dir := initTestRepo(t)
	defaultBranchOverride = ""
	defer func() { defaultBranchOverride = "" }()

	runGit(t, dir, "checkout", "-b", "feature-few-files")
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("small%d.go", i)), "package main\n")
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add 5 files")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	s, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("NewSessionFromGit failed: %v", err)
	}

	for _, f := range s.Files {
		if f.Lazy {
			t.Errorf("file %s should not be lazy when under threshold", f.Path)
		}
		if f.Content == "" && f.Status != "deleted" {
			t.Errorf("file %s should have content loaded", f.Path)
		}
	}
}

func TestGetFileSnapshotLazy(t *testing.T) {
	dir := initTestRepo(t)

	runGit(t, dir, "checkout", "-b", "feature-snap")
	writeFile(t, filepath.Join(dir, "snap.go"), "package main\nfunc snap() {}\n")
	runGit(t, dir, "add", "snap.go")
	runGit(t, dir, "commit", "-m", "add snap.go")

	base := strings.TrimSpace(runGit(t, dir, "merge-base", "main", "HEAD"))

	s := &Session{
		Mode:     "git",
		BaseRef:  base,
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:          "snap.go",
				AbsPath:       filepath.Join(dir, "snap.go"),
				Status:        "added",
				FileType:      "code",
				Lazy:          true,
				LazyAdditions: 2,
				Comments:      []Comment{},
			},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// GetFileSnapshot should trigger ensureLoaded
	snapshot, ok := s.GetFileSnapshot("snap.go")
	if !ok {
		t.Fatal("expected file to be found")
	}
	content, _ := snapshot["content"].(string)
	if content == "" {
		t.Fatal("expected content to be loaded on demand")
	}
	if !strings.Contains(content, "func snap") {
		t.Fatalf("unexpected content: %s", content)
	}

	// GetFileDiffSnapshot should also work for the now-loaded file
	diffSnap, ok := s.GetFileDiffSnapshot("snap.go")
	if !ok {
		t.Fatal("expected diff snapshot to be found")
	}
	hunks, _ := diffSnap["hunks"].([]DiffHunk)
	if len(hunks) == 0 {
		t.Fatal("expected diff hunks after lazy load")
	}

	// GetSessionInfo should show Lazy=false now (file was loaded)
	info := s.GetSessionInfo()
	for _, fi := range info.Files {
		if fi.Path == "snap.go" && fi.Lazy {
			t.Error("expected Lazy=false in session info after file was loaded")
		}
	}
}

// TestDeleteComment_NotReAddedFromDisk verifies that when a file comment is
// deleted in-memory and WriteFiles is called, the deleted comment does not
// reappear in .crit.json due to the merge-from-disk logic.
func TestDeleteComment_NotReAddedFromDisk(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		BaseRef:     "abc123",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", FileHash: "hash1", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "delete me", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write .crit.json with the comment present
	s.WriteFiles()

	// Verify the comment is on disk
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if len(cj.Files["main.go"].Comments) != 1 {
		t.Fatalf("expected 1 comment on disk before delete, got %d", len(cj.Files["main.go"].Comments))
	}

	// Delete the comment in-memory
	if !s.DeleteComment("main.go", "c1") {
		t.Fatal("DeleteComment failed")
	}

	// Flush and write again — the merge should NOT re-add c1 from disk
	flushWrites(s)
	s.WriteFiles()

	// Read .crit.json and verify the deleted comment is gone
	data, err = os.ReadFile(s.critJSONPath())
	if err != nil {
		// If .crit.json was removed (empty), that's also correct
		return
	}
	json.Unmarshal(data, &cj)
	if fileData, ok := cj.Files["main.go"]; ok {
		for _, c := range fileData.Comments {
			if c.ID == "c1" {
				t.Error("deleted comment c1 reappeared in .crit.json after WriteFiles")
			}
		}
	}
}

// TestDeleteReviewComment_NotReAddedFromDisk verifies that when a review-level
// comment is deleted and WriteFiles is called, it does not reappear from disk.
func TestDeleteReviewComment_NotReAddedFromDisk(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files:       []*FileEntry{},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Add a review comment and write to disk
	rc := s.AddReviewComment("delete this review comment", "")
	s.WriteFiles()

	// Verify it's on disk
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment on disk, got %d", len(cj.ReviewComments))
	}

	// Delete the review comment in-memory
	if !s.DeleteReviewComment(rc.ID) {
		t.Fatal("DeleteReviewComment failed")
	}

	// Write again
	flushWrites(s)
	s.WriteFiles()

	// Read .crit.json — should have no review comments
	data, err = os.ReadFile(s.critJSONPath())
	if err != nil {
		// File removed is also acceptable
		return
	}
	json.Unmarshal(data, &cj)
	for _, c := range cj.ReviewComments {
		if c.ID == rc.ID {
			t.Error("deleted review comment reappeared in .crit.json after WriteFiles")
		}
	}
}

// TestDeleteReply_NotReAddedFromDisk verifies that when a reply on a file
// comment is deleted, it does not reappear from disk after WriteFiles.
func TestDeleteReply_NotReAddedFromDisk(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", FileHash: "hash1", Comments: []Comment{
				{
					ID: "c1", StartLine: 1, EndLine: 1, Body: "parent comment",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					Replies: []Reply{
						{ID: "c1-r1", Body: "delete this reply", Author: "agent", CreatedAt: "2026-01-01T00:00:01Z"},
					},
				},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write to disk with the reply
	s.WriteFiles()

	// Delete the reply in-memory
	if !s.DeleteReply("main.go", "c1", "c1-r1") {
		t.Fatal("DeleteReply failed")
	}

	// Write again
	flushWrites(s)
	s.WriteFiles()

	// Read .crit.json and verify the reply is gone
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	for _, c := range cj.Files["main.go"].Comments {
		if c.ID == "c1" {
			for _, r := range c.Replies {
				if r.ID == "c1-r1" {
					t.Error("deleted reply c1-r1 reappeared in .crit.json after WriteFiles")
				}
			}
		}
	}
}

// TestDeleteReviewCommentReply_NotReAddedFromDisk verifies that when a reply
// on a review comment is deleted, it does not reappear from disk after WriteFiles.
func TestDeleteReviewCommentReply_NotReAddedFromDisk(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		ReviewRound: 1,

		Files:       []*FileEntry{},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Add review comment with a reply, then write to disk
	rc := s.AddReviewComment("parent review comment", "")
	reply, ok := s.AddReviewCommentReply(rc.ID, "delete this reply", "agent")
	if !ok {
		t.Fatal("AddReviewCommentReply failed")
	}
	s.WriteFiles()

	// Delete the reply in-memory
	if !s.DeleteReviewCommentReply(rc.ID, reply.ID) {
		t.Fatal("DeleteReviewCommentReply failed")
	}

	// Write again
	flushWrites(s)
	s.WriteFiles()

	// Read back .crit.json
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	for _, c := range cj.ReviewComments {
		if c.ID == rc.ID {
			for _, r := range c.Replies {
				if r.ID == reply.ID {
					t.Error("deleted review reply reappeared in .crit.json after WriteFiles")
				}
			}
		}
	}
}

// TestExternalCommentStillMerged verifies that comments added externally to
// .crit.json (not deleted by user) are still properly merged in — this is
// a regression test for the existing merge behavior.
func TestExternalCommentStillMerged(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		Branch:      "main",
		BaseRef:     "abc123",
		ReviewRound: 1,

		Files: []*FileEntry{
			{Path: "main.go", Status: "modified", FileHash: "hash1", Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "from browser", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			}},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Simulate an external tool writing an additional comment to .crit.json
	cj := CritJSON{
		Branch: "main", BaseRef: "abc123", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified", FileHash: "hash1",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "from browser", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					{ID: "c2", StartLine: 10, EndLine: 10, Body: "from CLI", Author: "Claude", CreatedAt: "2026-01-01T00:00:01Z", UpdatedAt: "2026-01-01T00:00:01Z"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	// WriteFiles should merge the external comment in
	s.WriteFiles()

	// Read back .crit.json and verify both comments are present
	result, _ := os.ReadFile(filepath.Join(dir, ".crit.json"))
	var got CritJSON
	json.Unmarshal(result, &got)

	if len(got.Files["main.go"].Comments) != 2 {
		t.Fatalf("expected 2 comments (browser + external), got %d", len(got.Files["main.go"].Comments))
	}

	foundExternal := false
	for _, c := range got.Files["main.go"].Comments {
		if c.ID == "c2" && c.Body == "from CLI" {
			foundExternal = true
		}
	}
	if !foundExternal {
		t.Error("external comment c2 was lost during WriteFiles — merge is broken")
	}
}

func TestSession_SetCommentResolved(t *testing.T) {
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "needs fix", "", "")

	// Resolve
	resolved, ok := s.SetCommentResolved("plan.md", c.ID, true)
	if !ok {
		t.Fatal("SetCommentResolved returned false")
	}
	if !resolved.Resolved {
		t.Error("expected comment to be resolved")
	}

	// Verify the resolved state persists in GetComments
	comments := s.GetComments("plan.md")
	if !comments[0].Resolved {
		t.Error("resolved state not persisted in GetComments")
	}

	// Unresolve
	unresolved, ok := s.SetCommentResolved("plan.md", c.ID, false)
	if !ok {
		t.Fatal("SetCommentResolved returned false on unresolve")
	}
	if unresolved.Resolved {
		t.Error("expected comment to be unresolved")
	}

	comments = s.GetComments("plan.md")
	if comments[0].Resolved {
		t.Error("unresolve not persisted in GetComments")
	}
}

func TestSession_SetCommentResolved_NotFound(t *testing.T) {
	s := newTestSession(t)
	_, ok := s.SetCommentResolved("plan.md", "c999", true)
	if ok {
		t.Error("expected false for nonexistent comment")
	}
	_, ok = s.SetCommentResolved("nonexistent.go", "c1", true)
	if ok {
		t.Error("expected false for nonexistent file")
	}
}

func TestSession_FindCommentByID(t *testing.T) {
	s := newTestSession(t)
	c1, _ := s.AddComment("plan.md", 1, 1, "", "md comment", "", "")
	c2, _ := s.AddComment("main.go", 5, 5, "", "go comment", "", "")

	// Find with filePath hint
	found, path, ok := s.FindCommentByID(c1.ID, "plan.md")
	if !ok {
		t.Fatal("FindCommentByID with hint returned false")
	}
	if path != "plan.md" {
		t.Errorf("path = %q, want plan.md", path)
	}
	if found.Body != "md comment" {
		t.Errorf("body = %q, want md comment", found.Body)
	}

	// Find without filePath hint (cross-file search)
	found2, path2, ok2 := s.FindCommentByID(c2.ID, "")
	if !ok2 {
		t.Fatal("FindCommentByID without hint returned false")
	}
	if path2 != "main.go" {
		t.Errorf("path = %q, want main.go", path2)
	}
	if found2.Body != "go comment" {
		t.Errorf("body = %q, want go comment", found2.Body)
	}

	// Not found
	_, _, ok3 := s.FindCommentByID("nonexistent", "")
	if ok3 {
		t.Error("expected false for nonexistent comment ID")
	}
}

func TestSession_ClearAllComments_RemovesCritJSONFromFileList(t *testing.T) {
	s := newTestSession(t)

	// Add a .crit.json entry to the file list (as would happen when git detects it)
	s.mu.Lock()
	s.Files = append(s.Files, &FileEntry{
		Path:     ".crit.json",
		AbsPath:  filepath.Join(s.RepoRoot, ".crit.json"),
		Status:   "untracked",
		FileType: "code",
		Comments: []Comment{},
	})
	s.mu.Unlock()

	s.AddComment("plan.md", 1, 1, "", "test", "", "")
	s.AddReviewComment("review", "")

	if len(s.GetReviewComments()) != 1 {
		t.Fatal("expected 1 review comment before clear")
	}

	s.ClearAllComments()

	// .crit.json should be removed from the file list
	for _, f := range s.Files {
		if filepath.Base(f.Path) == ".crit.json" {
			t.Error(".crit.json should be removed from file list after ClearAllComments")
		}
	}

	// All comments should be gone
	if s.TotalCommentCount() != 0 {
		t.Errorf("expected 0 total comments, got %d", s.TotalCommentCount())
	}
	if len(s.GetReviewComments()) != 0 {
		t.Errorf("expected 0 review comments, got %d", len(s.GetReviewComments()))
	}

	// Review round should reset to 1
	if s.GetReviewRound() != 1 {
		t.Errorf("ReviewRound = %d, want 1 after clear", s.GetReviewRound())
	}
}

func TestSession_ClearAllComments_DeletesCritJSONFromDisk(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "test", "", "")
	flushWrites(s)
	s.WriteFiles()

	// Verify .crit.json exists
	if _, err := os.Stat(s.critJSONPath()); err != nil {
		t.Fatalf(".crit.json should exist before clear: %v", err)
	}

	s.ClearAllComments()

	// .crit.json should be deleted from disk
	if _, err := os.Stat(s.critJSONPath()); !os.IsNotExist(err) {
		t.Error(".crit.json should be deleted from disk after ClearAllComments")
	}
}

func TestSession_HandleExternalDeletion(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "test", "", "")
	flushWrites(s)
	s.WriteFiles()

	// Verify comments exist
	if s.TotalCommentCount() != 1 {
		t.Fatal("expected 1 comment before deletion")
	}

	// Delete .crit.json externally
	os.Remove(s.critJSONPath())

	// handleExternalDeletion should detect the deletion and clear in-memory state
	deleted := s.handleExternalDeletion(s.critJSONPath())
	if !deleted {
		t.Fatal("handleExternalDeletion should return true when file was deleted")
	}

	if s.TotalCommentCount() != 0 {
		t.Errorf("expected 0 comments after external deletion, got %d", s.TotalCommentCount())
	}
}

func TestSession_HandleExternalDeletion_NoMtime(t *testing.T) {
	s := newTestSession(t)
	// Without ever writing, lastCritJSONMtime is zero — should not detect deletion
	deleted := s.handleExternalDeletion(s.critJSONPath())
	if deleted {
		t.Error("handleExternalDeletion should return false when mtime is zero")
	}
}

func TestSession_WriteFiles_RoundTrip(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 3, "", "fix formatting", "", "reviewer")
	s.AddComment("main.go", 2, 2, "RIGHT", "handle error", "func main() {}", "agent")
	s.AddReviewComment("overall looks good", "reviewer")

	flushWrites(s)
	s.WriteFiles()

	// Read back the .crit.json
	data1, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("reading first write: %v", err)
	}

	// Write again (no changes) and compare
	flushWrites(s)
	s.WriteFiles()
	data2, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("reading second write: %v", err)
	}

	// Parse both to compare semantically (timestamps may differ)
	var cj1, cj2 CritJSON
	json.Unmarshal(data1, &cj1)
	json.Unmarshal(data2, &cj2)

	if len(cj1.Files) != len(cj2.Files) {
		t.Errorf("file count mismatch: %d vs %d", len(cj1.Files), len(cj2.Files))
	}
	for path, f1 := range cj1.Files {
		f2, ok := cj2.Files[path]
		if !ok {
			t.Errorf("file %q missing in second write", path)
			continue
		}
		if len(f1.Comments) != len(f2.Comments) {
			t.Errorf("%s: comment count %d vs %d", path, len(f1.Comments), len(f2.Comments))
		}
	}
	if len(cj1.ReviewComments) != len(cj2.ReviewComments) {
		t.Errorf("review comment count %d vs %d", len(cj1.ReviewComments), len(cj2.ReviewComments))
	}
}

func TestSession_AddComment_PreservesSideAndQuote(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddComment("main.go", 5, 10, "RIGHT", "fix this", "func main() {}", "reviewer")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.Side != "RIGHT" {
		t.Errorf("Side = %q, want RIGHT", c.Side)
	}
	if c.Quote != "func main() {}" {
		t.Errorf("Quote = %q, want func main() {}", c.Quote)
	}
	if c.Scope != "line" {
		t.Errorf("Scope = %q, want line", c.Scope)
	}

	// Verify roundtrip through WriteFiles
	flushWrites(s)
	s.WriteFiles()

	data, _ := os.ReadFile(s.critJSONPath())
	var cj CritJSON
	json.Unmarshal(data, &cj)

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment in .crit.json, got %d", len(cf.Comments))
	}
	if cf.Comments[0].Side != "RIGHT" {
		t.Errorf("persisted Side = %q, want RIGHT", cf.Comments[0].Side)
	}
	if cf.Comments[0].Quote != "func main() {}" {
		t.Errorf("persisted Quote = %q", cf.Comments[0].Quote)
	}
}

func TestSession_WriteFiles_ReviewCommentsPersisted(t *testing.T) {
	s := newTestSession(t)
	s.AddReviewComment("general note", "reviewer")

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)

	if len(cj.ReviewComments) != 1 {
		t.Fatalf("expected 1 review comment in .crit.json, got %d", len(cj.ReviewComments))
	}
	if cj.ReviewComments[0].Body != "general note" {
		t.Errorf("body = %q, want general note", cj.ReviewComments[0].Body)
	}
	if cj.ReviewComments[0].Scope != "review" {
		t.Errorf("scope = %q, want review", cj.ReviewComments[0].Scope)
	}
}

func TestSession_RandomCommentID_Format(t *testing.T) {
	s := newTestSession(t)

	c, ok := s.AddComment("plan.md", 1, 1, "", "test", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if !strings.HasPrefix(c.ID, "c_") || len(c.ID) != 8 {
		t.Errorf("comment ID %q does not match c_XXXXXX format", c.ID)
	}

	// Two comments should get different IDs
	c2, ok := s.AddComment("plan.md", 2, 2, "", "test2", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.ID == c2.ID {
		t.Errorf("two comments got the same ID: %q", c.ID)
	}
}

func TestSession_ClearAllComments(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "md comment", "", "")
	s.AddComment("main.go", 1, 1, "", "go comment", "", "")
	s.AddReviewComment("review comment", "")

	if s.TotalCommentCount() != 3 {
		t.Fatalf("precondition: expected 3 comments, got %d", s.TotalCommentCount())
	}

	s.ClearAllComments()

	if len(s.GetComments("plan.md")) != 0 {
		t.Error("plan.md comments should be cleared")
	}
	if len(s.GetComments("main.go")) != 0 {
		t.Error("main.go comments should be cleared")
	}
	if len(s.GetReviewComments()) != 0 {
		t.Error("review comments should be cleared")
	}
	if s.TotalCommentCount() != 0 {
		t.Errorf("TotalCommentCount = %d, want 0", s.TotalCommentCount())
	}
}

func TestSession_AddComment_WithSide(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddComment("main.go", 5, 10, "RIGHT", "check this", "", "")
	if !ok {
		t.Fatal("AddComment with side failed")
	}
	if c.Side != "RIGHT" {
		t.Errorf("Side = %q, want RIGHT", c.Side)
	}
	if c.StartLine != 5 || c.EndLine != 10 {
		t.Errorf("lines = %d-%d, want 5-10", c.StartLine, c.EndLine)
	}
}

func TestSession_WriteFiles_IncludesResolvedComments(t *testing.T) {
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "fix", "", "")
	s.SetCommentResolved("plan.md", c.ID, true)

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}

	comments := cj.Files["plan.md"].Comments
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment in .crit.json, got %d", len(comments))
	}
	if !comments[0].Resolved {
		t.Error("resolved state should be persisted to .crit.json")
	}
}

func TestLoadCritJSON_RestoresShareState(t *testing.T) {
	s := newTestSession(t)

	// Compute the share scope for this session's files.
	paths := make([]string, len(s.Files))
	for i, f := range s.Files {
		paths[i] = f.Path
	}
	scope := shareScope(paths)

	// Write a review file with share state.
	reviewPath := filepath.Join(s.RepoRoot, "review.json")
	cj := CritJSON{
		Branch:      "main",
		BaseRef:     "abc123",
		UpdatedAt:   time.Now().Format(time.RFC3339),
		ReviewRound: 1,
		ShareURL:    "https://crit.example.com/review/abc",
		DeleteToken: "tok_secret123",
		ShareScope:  scope,
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reviewPath, string(data))

	// Point the session at the review file and load.
	s.ReviewFilePath = reviewPath
	s.loadCritJSON()

	if s.sharedURL != "https://crit.example.com/review/abc" {
		t.Errorf("sharedURL = %q, want %q", s.sharedURL, "https://crit.example.com/review/abc")
	}
	if s.deleteToken != "tok_secret123" {
		t.Errorf("deleteToken = %q, want %q", s.deleteToken, "tok_secret123")
	}
	if s.shareScope != scope {
		t.Errorf("shareScope = %q, want %q", s.shareScope, scope)
	}
}

func TestLoadCritJSON_ShareScopeMismatch(t *testing.T) {
	s := newTestSession(t)

	// Write a review file with a share scope that does not match this session.
	reviewPath := filepath.Join(s.RepoRoot, "review.json")
	cj := CritJSON{
		ShareURL:    "https://crit.example.com/review/old",
		DeleteToken: "tok_old",
		ShareScope:  "mismatched_scope_value",
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reviewPath, string(data))

	s.ReviewFilePath = reviewPath
	s.loadCritJSON()

	// Share state should NOT be loaded because the scope doesn't match.
	if s.sharedURL != "" {
		t.Errorf("sharedURL = %q, want empty (scope mismatch)", s.sharedURL)
	}
	if s.deleteToken != "" {
		t.Errorf("deleteToken = %q, want empty (scope mismatch)", s.deleteToken)
	}
}

func TestLoadCritJSON_NoScope(t *testing.T) {
	s := newTestSession(t)

	// Review file without ShareScope — should load unconditionally.
	reviewPath := filepath.Join(s.RepoRoot, "review.json")
	cj := CritJSON{
		ShareURL:    "https://crit.example.com/review/legacy",
		DeleteToken: "tok_legacy",
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reviewPath, string(data))

	s.ReviewFilePath = reviewPath
	s.loadCritJSON()

	if s.sharedURL != "https://crit.example.com/review/legacy" {
		t.Errorf("sharedURL = %q, want %q", s.sharedURL, "https://crit.example.com/review/legacy")
	}
	if s.deleteToken != "tok_legacy" {
		t.Errorf("deleteToken = %q, want %q", s.deleteToken, "tok_legacy")
	}
}

func TestCreateSession_LoadsShareFromReviewPath(t *testing.T) {
	dir := initTestRepo(t)

	// Create a file on a feature branch so git mode detects changes.
	runGit(t, dir, "checkout", "-b", "feat-share")
	writeFile(t, filepath.Join(dir, "new.md"), "# New\n\nContent\n")
	runGit(t, dir, "add", "new.md")
	runGit(t, dir, "commit", "-m", "add new.md")

	// Prepare a review file with share state.
	reviewPath := filepath.Join(t.TempDir(), "review.json")
	scope := shareScope([]string{"new.md"})
	cj := CritJSON{
		Branch:      "feat-share",
		BaseRef:     "abc",
		UpdatedAt:   time.Now().Format(time.RFC3339),
		ReviewRound: 2,
		ShareURL:    "https://crit.example.com/review/shared",
		DeleteToken: "tok_shared",
		ShareScope:  scope,
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reviewPath, string(data))

	// Change to the repo dir so git operations work.
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	sc := &serverConfig{
		reviewPath: reviewPath,
	}
	session, err := createSession(sc)
	if err != nil {
		t.Fatal(err)
	}

	if session.ReviewFilePath != reviewPath {
		t.Errorf("ReviewFilePath = %q, want %q", session.ReviewFilePath, reviewPath)
	}
	if session.sharedURL != "https://crit.example.com/review/shared" {
		t.Errorf("sharedURL = %q, want %q", session.sharedURL, "https://crit.example.com/review/shared")
	}
	if session.deleteToken != "tok_shared" {
		t.Errorf("deleteToken = %q, want %q", session.deleteToken, "tok_shared")
	}
	if session.ReviewRound != 2 {
		t.Errorf("ReviewRound = %d, want 2", session.ReviewRound)
	}
}

func TestCreateSession_FilesMode_LoadsShareFromReviewPath(t *testing.T) {
	dir := initTestRepo(t)
	mdPath := filepath.Join(dir, "doc.md")
	writeFile(t, mdPath, "# Doc\n\nHello\n")

	// createSession -> NewSessionFromFiles will resolve the path relative to
	// the git repo root. Compute the expected relative path so we can build
	// a matching share scope.
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	// NewSessionFromFiles resolves paths relative to repo root ("doc.md").
	scope := shareScope([]string{"doc.md"})

	// Prepare a review file with share state.
	reviewPath := filepath.Join(t.TempDir(), "review.json")
	cj := CritJSON{
		ShareURL:    "https://crit.example.com/review/files",
		DeleteToken: "tok_files",
		ShareScope:  scope,
		ReviewRound: 3,
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reviewPath, string(data))

	sc := &serverConfig{
		files:      []string{"doc.md"},
		reviewPath: reviewPath,
	}
	session, err := createSession(sc)
	if err != nil {
		t.Fatal(err)
	}

	if session.sharedURL != "https://crit.example.com/review/files" {
		t.Errorf("sharedURL = %q, want %q", session.sharedURL, "https://crit.example.com/review/files")
	}
	if session.deleteToken != "tok_files" {
		t.Errorf("deleteToken = %q, want %q", session.deleteToken, "tok_files")
	}
	if session.ReviewRound != 3 {
		t.Errorf("ReviewRound = %d, want 3", session.ReviewRound)
	}
}

func TestLoadCritJSON_OrphanedComments(t *testing.T) {
	dir := initTestRepo(t)
	branch := "main"

	// Create session with just one file
	writeFile(t, filepath.Join(dir, "existing.md"), "# Hello")
	s := &Session{
		Mode:     "git",
		Branch:   branch,
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "existing.md", AbsPath: filepath.Join(dir, "existing.md"), Status: "modified", FileType: "markdown"},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write a review file with comments on both existing and orphaned paths
	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      branch,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"existing.md": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c_exist1", Body: "comment on existing", Scope: "line", StartLine: 1, EndLine: 1},
				},
			},
			"removed.go": {
				Status: "added",
				Comments: []Comment{
					{ID: "c_orphan1", Body: "file-level comment", Scope: "file"},
					{ID: "c_orphan2", Body: "line comment on removed file", Scope: "line", StartLine: 5, EndLine: 10},
				},
			},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(critPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(critPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s.loadCritJSON()

	// Should now have 2 files
	if len(s.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(s.Files))
	}

	// Find the orphaned file
	var orphaned *FileEntry
	for _, f := range s.Files {
		if f.Path == "removed.go" {
			orphaned = f
			break
		}
	}
	if orphaned == nil {
		t.Fatal("orphaned file not found in session")
	}
	if !orphaned.Orphaned {
		t.Error("expected Orphaned=true")
	}
	if orphaned.Status != "removed" {
		t.Errorf("expected status 'removed', got %q", orphaned.Status)
	}
	if orphaned.FileType != "code" {
		t.Errorf("expected file type 'code', got %q", orphaned.FileType)
	}
	if len(orphaned.Comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(orphaned.Comments))
	}

	// Existing file should still have its comment
	var existing *FileEntry
	for _, f := range s.Files {
		if f.Path == "existing.md" {
			existing = f
			break
		}
	}
	if len(existing.Comments) != 1 {
		t.Errorf("expected 1 comment on existing file, got %d", len(existing.Comments))
	}
}

func TestLoadCritJSON_OrphanedNoComments(t *testing.T) {
	dir := initTestRepo(t)

	s := &Session{
		Mode:     "git",
		Branch:   "main",
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "existing.md", AbsPath: filepath.Join(dir, "existing.md"), Status: "modified", FileType: "markdown"},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"existing.md": {Status: "modified"},
			"removed.go":  {Status: "added", Comments: []Comment{}}, // no comments
		},
	}
	data, _ := json.Marshal(cj)
	os.MkdirAll(filepath.Dir(critPath), 0o755)
	os.WriteFile(critPath, data, 0o644)

	s.loadCritJSON()

	if len(s.Files) != 1 {
		t.Fatalf("expected 1 file (no phantom for empty comments), got %d", len(s.Files))
	}
}

func TestGetSessionInfo_OrphanedField(t *testing.T) {
	s := &Session{
		Mode:   "git",
		Branch: "main",
		Files: []*FileEntry{
			{Path: "real.go", Status: "modified", FileType: "code"},
			{Path: "gone.go", Status: "removed", FileType: "code", Orphaned: true,
				Comments: []Comment{{ID: "c1", Body: "orphaned", Scope: "file"}}},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	info := s.GetSessionInfo()
	if len(info.Files) != 2 {
		t.Fatalf("expected 2 files in session info, got %d", len(info.Files))
	}

	var orphanedInfo *SessionFileInfo
	for i := range info.Files {
		if info.Files[i].Path == "gone.go" {
			orphanedInfo = &info.Files[i]
			break
		}
	}
	if orphanedInfo == nil {
		t.Fatal("orphaned file not in session info")
	}
	if !orphanedInfo.Orphaned {
		t.Error("expected Orphaned=true in session info")
	}
	if orphanedInfo.Status != "removed" {
		t.Errorf("expected status 'removed', got %q", orphanedInfo.Status)
	}
	if orphanedInfo.CommentCount != 1 {
		t.Errorf("expected comment count 1, got %d", orphanedInfo.CommentCount)
	}
}

func TestAddComment_PopulatesAnchor(t *testing.T) {
	s := newTestSession(t)
	// plan.md content: "# Plan\n\n## Step 1\n\nDo the thing\n"
	// Lines: 1="# Plan", 2="", 3="## Step 1", 4="", 5="Do the thing"
	c, ok := s.AddComment("plan.md", 3, 5, "", "Rethink this", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	want := "## Step 1\n\nDo the thing"
	if c.Anchor != want {
		t.Errorf("Anchor = %q, want %q", c.Anchor, want)
	}
}

func TestAddComment_AnchorSingleLine(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddComment("plan.md", 1, 1, "", "Fix title", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.Anchor != "# Plan" {
		t.Errorf("Anchor = %q, want %q", c.Anchor, "# Plan")
	}
}

func TestAddComment_NoAnchorForFileComment(t *testing.T) {
	s := newTestSession(t)
	c, ok := s.AddFileComment("plan.md", "Overall feedback", "reviewer")
	if !ok {
		t.Fatal("AddFileComment failed")
	}
	if c.Anchor != "" {
		t.Errorf("file-level comment should not have anchor, got %q", c.Anchor)
	}
}

func TestAddComment_NoAnchorForReviewComment(t *testing.T) {
	s := newTestSession(t)
	c := s.AddReviewComment("General feedback", "reviewer")
	if c.Anchor != "" {
		t.Errorf("review-level comment should not have anchor, got %q", c.Anchor)
	}
}

func TestAddComment_OldSideAnchorFromBase(t *testing.T) {
	// Old-side comments reference the base version's line numbers.
	// extractAnchor should fall back to git show <baseRef>:<path> for the anchor.
	dir := initTestRepo(t)

	// Create a file on the base branch with known content.
	goPath := filepath.Join(dir, "main.go")
	writeFile(t, goPath, "package main\n\nfunc deleted() {\n\t// old code\n}\n")
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "add main.go")
	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Create feature branch and modify the file (removing the old function).
	runGit(t, dir, "checkout", "-b", "feat")
	writeFile(t, goPath, "package main\n\nfunc newFunc() {\n\t// new code\n}\n")
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "replace function")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		BaseRef:     baseRef,
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files: []*FileEntry{
			{
				Path:     "main.go",
				AbsPath:  goPath,
				Status:   "modified",
				FileType: "code",
				Content:  "package main\n\nfunc newFunc() {\n\t// new code\n}\n",
				Comments: []Comment{},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	// Comment on old-side line 3 ("func deleted() {") — this line doesn't exist
	// in the working tree, so anchor must come from the base ref.
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	c, ok := s.AddComment("main.go", 3, 3, "old", "Why was this removed?", "", "reviewer")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if c.Anchor != "func deleted() {" {
		t.Errorf("old-side Anchor = %q, want %q", c.Anchor, "func deleted() {")
	}
}

func TestExtractAnchor(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"

	tests := []struct {
		name       string
		start, end int
		want       string
	}{
		{"single line", 2, 2, "line2"},
		{"range", 2, 4, "line2\nline3\nline4"},
		{"first line", 1, 1, "line1"},
		{"last line", 5, 5, "line5"},
		{"out of range", 6, 6, ""},
		{"zero start", 0, 1, ""},
		{"end beyond file", 4, 10, "line4\nline5"},
		{"empty content", 1, 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := content
			if tt.name == "empty content" {
				c = ""
			}
			got := extractAnchor(c, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("extractAnchor(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
			}
		})
	}
}
