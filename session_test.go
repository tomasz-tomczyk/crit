package main

import (
	"encoding/json"
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
		RepoRoot:      dir,
		ReviewRound:   1,
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
				nextID:   1,
			},
			{
				Path:     "main.go",
				AbsPath:  goPath,
				Status:   "modified",
				FileType: "code",
				Content:  "package main\n\nfunc main() {}\n",
				FileHash: "sha256:test2",
				Comments: []Comment{},
				nextID:   1,
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
	if c.ID != "c1" {
		t.Errorf("ID = %q, want c1", c.ID)
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
	s.AddComment("plan.md", 1, 1, "", "original", "", "")
	updated, ok := s.UpdateComment("plan.md", "c1", "updated body")
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
	s.AddComment("plan.md", 1, 1, "", "to delete", "", "")
	if !s.DeleteComment("plan.md", "c1") {
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

func TestSession_UnresolvedCommentCount_AllResolved(t *testing.T) {
	s := newTestSession(t)
	s.mu.Lock()
	f := s.fileByPathLocked("plan.md")
	f.Comments = []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "done", Resolved: true},
		{ID: "c2", StartLine: 2, EndLine: 2, Body: "done too", Resolved: true},
	}
	s.mu.Unlock()

	if got := s.UnresolvedCommentCount(); got != 0 {
		t.Errorf("UnresolvedCommentCount = %d, want 0", got)
	}
	if got := s.TotalCommentCount(); got != 2 {
		t.Errorf("TotalCommentCount = %d, want 2", got)
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
	s.SetSharedURLAndToken("https://crit.live/r/abc", "token123")

	flushWrites(s)
	s.WriteFiles()

	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if cj.ShareURL != "https://crit.live/r/abc" {
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

func TestSession_LoadResolvedComments_StringResolutionLines(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "fix this", "", "")

	flushWrites(s)
	s.WriteFiles()

	// Simulate what an agent does: read .crit.json, add resolved + string resolution_lines, write back
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	files := raw["files"].(map[string]any)
	planFile := files["plan.md"].(map[string]any)
	comments := planFile["comments"].([]any)
	comment := comments[0].(map[string]any)
	comment["resolved"] = true
	comment["resolution_note"] = "Fixed it"
	comment["resolution_lines"] = "1-5" // agent writes string, not []int
	data, _ = json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(s.critJSONPath(), data, 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	// Now loadResolvedComments should parse successfully despite string resolution_lines
	s.loadResolvedComments()

	s.mu.RLock()
	f := s.fileByPathLocked("plan.md")
	if len(f.PreviousComments) != 1 {
		t.Fatalf("expected 1 PreviousComment, got %d", len(f.PreviousComments))
	}
	if !f.PreviousComments[0].Resolved {
		t.Error("expected comment to be resolved")
	}
	if f.PreviousComments[0].ResolutionNote != "Fixed it" {
		t.Errorf("ResolutionNote = %q, want %q", f.PreviousComments[0].ResolutionNote, "Fixed it")
	}
	s.mu.RUnlock()
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
	if s.GetReviewRound() != 2 {
		t.Errorf("review round = %d, want 2", s.GetReviewRound())
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

func TestSession_PerFileCommentIDs(t *testing.T) {
	s := newTestSession(t)
	c1, _ := s.AddComment("plan.md", 1, 1, "", "md comment", "", "")
	c2, _ := s.AddComment("main.go", 1, 1, "", "go comment", "", "")

	// Each file has independent ID sequences
	if c1.ID != "c1" {
		t.Errorf("plan.md first comment ID = %q, want c1", c1.ID)
	}
	if c2.ID != "c1" {
		t.Errorf("main.go first comment ID = %q, want c1", c2.ID)
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
			}, nextID: 2},
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
			{Path: "main.go", Status: "modified", Comments: []Comment{}, nextID: 1},
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

	s.mu.RLock()
	nextID := s.Files[0].nextID
	s.mu.RUnlock()
	if nextID != 2 {
		t.Errorf("expected nextID=2, got %d", nextID)
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
			}, nextID: 2},
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
			}, nextID: 2},
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
		ShareURL:    "https://crit.live/r/old",
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
		ShareURL:    "https://crit.live/r/current",
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
	if url != "https://crit.live/r/current" {
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
	// Old .crit.json format without replies field
	data := `{"id":"c1","start_line":5,"end_line":5,"body":"Fix","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","resolution_note":"Done"}`

	var c Comment
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		t.Fatal(err)
	}
	if c.Replies != nil {
		t.Errorf("expected nil replies, got %v", c.Replies)
	}
	if c.ResolutionNote != "Done" {
		t.Errorf("resolution_note = %q, want %q", c.ResolutionNote, "Done")
	}
}

func TestSession_AddReply(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:     "test.md",
				Comments: []Comment{{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"}},
				nextID:   2,
			},
		},
	}

	reply, ok := s.AddReply("test.md", "c1", "Done, fixed it", "agent")
	if !ok {
		t.Fatal("AddReply returned false")
	}
	if reply.ID != "c1-r1" {
		t.Errorf("reply ID = %q, want %q", reply.ID, "c1-r1")
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

	if r1.ID != "c1-r1" || r2.ID != "c1-r2" || r3.ID != "c1-r3" {
		t.Errorf("IDs = %q, %q, %q", r1.ID, r2.ID, r3.ID)
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
		ID:              "original-id",
		StartLine:       5,
		EndLine:         10,
		Side:            "RIGHT",
		Body:            "needs refactoring",
		Quote:           "func foo() {}",
		Author:          "reviewer-bot",
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-01-01T00:00:00Z",
		Resolved:        true,
		ResolutionNote:  "Fixed in round 2",
		ResolutionLines: "5-8",
		CarriedForward:  false,
		ReviewRound:     1,
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
	if carried.ResolutionNote != "Fixed in round 2" {
		t.Errorf("ResolutionNote = %q", carried.ResolutionNote)
	}
	if carried.ResolutionLines != "5-8" {
		t.Errorf("ResolutionLines = %v", carried.ResolutionLines)
	}
	if !carried.CarriedForward {
		t.Error("CarriedForward should be true")
	}
	if carried.ReviewRound != 1 {
		t.Errorf("ReviewRound = %d, want 1", carried.ReviewRound)
	}
	if carried.Quote != "" {
		t.Errorf("Quote = %q, should not be carried forward", carried.Quote)
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
