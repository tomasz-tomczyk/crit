package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
	c, ok := s.AddComment("plan.md", 1, 3, "", "Rethink this")
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
	_, ok := s.AddComment("nonexistent.go", 1, 1, "", "test")
	if ok {
		t.Error("expected AddComment to fail for nonexistent file")
	}
}

func TestSession_UpdateComment(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "original")
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
	s.AddComment("plan.md", 1, 1, "", "to delete")
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
	s.AddComment("plan.md", 1, 1, "", "test")
	comments := s.GetComments("plan.md")
	comments[0].Body = "mutated"
	if s.GetComments("plan.md")[0].Body == "mutated" {
		t.Error("GetComments should return a copy")
	}
}

func TestSession_GetAllComments(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "md comment")
	s.AddComment("main.go", 1, 1, "", "go comment")

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
	s.AddComment("plan.md", 1, 1, "", "one")
	s.AddComment("plan.md", 2, 2, "", "two")
	s.AddComment("main.go", 1, 1, "", "three")

	if s.TotalCommentCount() != 3 {
		t.Errorf("TotalCommentCount = %d, want 3", s.TotalCommentCount())
	}
}

func TestSession_WriteFiles(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "fix")

	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
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

	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
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
	s.AddComment("plan.md", 1, 1, "", "persisted comment")

	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
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

func TestSession_SignalRoundComplete(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "fix this")
	s.AddComment("main.go", 1, 1, "", "and this")
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
			c, _ := s.AddComment("plan.md", 1, 1, "", "concurrent")
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
	s.AddComment("plan.md", 1, 1, "", "note")
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

func TestSession_GetFileContent(t *testing.T) {
	s := newTestSession(t)
	content, ok := s.GetFileContent("plan.md")
	if !ok {
		t.Fatal("expected to find plan.md")
	}
	if content == "" {
		t.Error("expected non-empty content")
	}

	_, ok = s.GetFileContent("nonexistent.txt")
	if ok {
		t.Error("expected false for nonexistent file")
	}
}

func TestSession_PerFileCommentIDs(t *testing.T) {
	s := newTestSession(t)
	c1, _ := s.AddComment("plan.md", 1, 1, "", "md comment")
	c2, _ := s.AddComment("main.go", 1, 1, "", "go comment")

	// Each file has independent ID sequences
	if c1.ID != "c1" {
		t.Errorf("plan.md first comment ID = %q, want c1", c1.ID)
	}
	if c2.ID != "c1" {
		t.Errorf("main.go first comment ID = %q, want c1", c2.ID)
	}
}
