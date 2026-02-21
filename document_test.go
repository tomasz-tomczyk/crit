package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestDoc(t *testing.T, content string) *Document {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	doc, err := NewDocument(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestNewDocument(t *testing.T) {
	doc := newTestDoc(t, "# Hello\n\nWorld")
	if doc.FileName != "test.md" {
		t.Errorf("FileName = %q, want test.md", doc.FileName)
	}
	if doc.Content != "# Hello\n\nWorld" {
		t.Errorf("Content = %q", doc.Content)
	}
	if doc.FileHash == "" {
		t.Error("FileHash should not be empty")
	}
	if len(doc.Comments) != 0 {
		t.Error("should start with no comments")
	}
}

func TestNewDocument_FileNotFound(t *testing.T) {
	_, err := NewDocument("/nonexistent/file.md", "/tmp")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestAddComment(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2\nline3")
	c := doc.AddComment(1, 2, "Fix this")

	if c.ID != "c1" {
		t.Errorf("ID = %q, want c1", c.ID)
	}
	if c.StartLine != 1 || c.EndLine != 2 {
		t.Errorf("lines = %d-%d, want 1-2", c.StartLine, c.EndLine)
	}
	if c.Body != "Fix this" {
		t.Errorf("Body = %q", c.Body)
	}
	if c.CreatedAt == "" || c.UpdatedAt == "" {
		t.Error("timestamps should be set")
	}
	if len(doc.GetComments()) != 1 {
		t.Errorf("expected 1 comment, got %d", len(doc.GetComments()))
	}
}

func TestAddComment_IncrementingIDs(t *testing.T) {
	doc := newTestDoc(t, "a\nb")
	c1 := doc.AddComment(1, 1, "first")
	c2 := doc.AddComment(2, 2, "second")
	if c1.ID != "c1" || c2.ID != "c2" {
		t.Errorf("IDs = %q, %q; want c1, c2", c1.ID, c2.ID)
	}
}

func TestUpdateComment(t *testing.T) {
	doc := newTestDoc(t, "a\nb")
	c := doc.AddComment(1, 1, "original")

	updated, ok := doc.UpdateComment(c.ID, "updated body")
	if !ok {
		t.Error("expected update to succeed")
	}
	if updated.Body != "updated body" {
		t.Errorf("Body = %q", updated.Body)
	}
	// UpdatedAt may be the same if test runs within the same second — that's fine.
	// Just verify the comment was actually updated in the slice.
	stored := doc.GetComments()[0]
	if stored.Body != "updated body" {
		t.Errorf("stored body = %q", stored.Body)
	}
}

func TestUpdateComment_NotFound(t *testing.T) {
	doc := newTestDoc(t, "a")
	_, ok := doc.UpdateComment("nonexistent", "body")
	if ok {
		t.Error("expected update to fail for nonexistent ID")
	}
}

func TestDeleteComment(t *testing.T) {
	doc := newTestDoc(t, "a\nb")
	c := doc.AddComment(1, 1, "to delete")
	if !doc.DeleteComment(c.ID) {
		t.Error("expected delete to succeed")
	}
	if len(doc.GetComments()) != 0 {
		t.Error("comment should be gone")
	}
}

func TestDeleteComment_NotFound(t *testing.T) {
	doc := newTestDoc(t, "a")
	if doc.DeleteComment("nonexistent") {
		t.Error("expected delete to fail for nonexistent ID")
	}
}

func TestGetComments_ReturnsCopy(t *testing.T) {
	doc := newTestDoc(t, "a")
	doc.AddComment(1, 1, "test")
	comments := doc.GetComments()
	comments[0].Body = "mutated"
	if doc.GetComments()[0].Body == "mutated" {
		t.Error("GetComments should return a copy")
	}
}

func TestStaleNotice(t *testing.T) {
	doc := newTestDoc(t, "a")
	if doc.GetStaleNotice() != "" {
		t.Error("no stale notice initially")
	}
	doc.mu.Lock()
	doc.staleNotice = "stale!"
	doc.mu.Unlock()
	if doc.GetStaleNotice() != "stale!" {
		t.Error("expected stale notice")
	}
	doc.ClearStaleNotice()
	if doc.GetStaleNotice() != "" {
		t.Error("expected cleared")
	}
}

func TestSubscribeNotify(t *testing.T) {
	doc := newTestDoc(t, "a")
	ch := doc.Subscribe()
	defer doc.Unsubscribe(ch)

	event := SSEEvent{Type: "file-changed", Filename: "test.md", Content: "new"}
	doc.notify(event)

	received := <-ch
	if received.Type != "file-changed" || received.Content != "new" {
		t.Errorf("unexpected event: %+v", received)
	}
}

func TestConcurrentAccess(t *testing.T) {
	doc := newTestDoc(t, "a\nb\nc")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := doc.AddComment(1, 1, "concurrent")
			doc.UpdateComment(c.ID, "updated")
			doc.GetComments()
			doc.DeleteComment(c.ID)
		}()
	}
	wg.Wait()
}

func TestReloadFile(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.AddComment(1, 1, "comment")

	// Modify the file
	if err := os.WriteFile(doc.FilePath, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := doc.ReloadFile(); err != nil {
		t.Fatal(err)
	}

	if doc.Content != "modified" {
		t.Errorf("Content = %q, want modified", doc.Content)
	}
	if len(doc.GetComments()) != 0 {
		t.Error("comments should be cleared after reload")
	}
}

func TestWriteFiles_NoCommentsSkipsFiles(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")

	doc.WriteFiles()

	if _, err := os.Stat(doc.commentsFilePath()); !os.IsNotExist(err) {
		t.Error("expected comments file to not exist with no comments")
	}
	if _, err := os.Stat(doc.reviewFilePath()); !os.IsNotExist(err) {
		t.Error("expected review file to not exist with no comments")
	}
}

func TestWriteFiles(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "note")

	// Stop the debounce timer and write directly
	doc.mu.Lock()
	if doc.writeTimer != nil {
		doc.writeTimer.Stop()
	}
	doc.mu.Unlock()
	doc.WriteFiles()

	// Check comments JSON was written
	data, err := os.ReadFile(doc.commentsFilePath())
	if err != nil {
		t.Fatalf("comments file not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("comments file is empty")
	}

	// Check review MD was written
	data, err = os.ReadFile(doc.reviewFilePath())
	if err != nil {
		t.Fatalf("review file not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("review file is empty")
	}
}

func TestSetGetSharedURL(t *testing.T) {
	doc := newTestDoc(t, "line1")
	if doc.GetSharedURL() != "" {
		t.Error("expected empty shared URL initially")
	}
	doc.SetSharedURL("https://crit.live/r/abc123")
	if doc.GetSharedURL() != "https://crit.live/r/abc123" {
		t.Errorf("shared URL = %q, want https://crit.live/r/abc123", doc.GetSharedURL())
	}
}

func writeAndStop(doc *Document) {
	doc.mu.Lock()
	if doc.writeTimer != nil {
		doc.writeTimer.Stop()
	}
	doc.mu.Unlock()
	doc.WriteFiles()
}

func TestSharedURL_PersistedAndLoaded(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "note")
	doc.SetSharedURL("https://crit.live/r/persisted")
	writeAndStop(doc)

	// Reload from same path/dir — should restore shared URL
	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if doc2.GetSharedURL() != "https://crit.live/r/persisted" {
		t.Errorf("shared URL after reload = %q, want https://crit.live/r/persisted", doc2.GetSharedURL())
	}
}

func TestSharedURL_PersistsWhenStale(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.AddComment(1, 1, "note")
	doc.SetSharedURL("https://crit.live/r/stale-test")
	writeAndStop(doc)

	// Change the file so the hash won't match on next load
	if err := os.WriteFile(doc.FilePath, []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	// Even though file changed (stale), shared URL should still be loaded
	if doc2.GetSharedURL() != "https://crit.live/r/stale-test" {
		t.Errorf("shared URL after stale reload = %q, want https://crit.live/r/stale-test", doc2.GetSharedURL())
	}
	// Comments should NOT be loaded (stale)
	if len(doc2.GetComments()) != 0 {
		t.Error("comments should not be loaded when file is stale")
	}
}

func TestWriteFiles_SharedURLOnlyCreatesFile(t *testing.T) {
	doc := newTestDoc(t, "line1")
	// No comments — only a shared URL
	doc.SetSharedURL("https://crit.live/r/urlonly")
	writeAndStop(doc)

	// Comments file should exist even though there are no comments
	data, err := os.ReadFile(doc.commentsFilePath())
	if err != nil {
		t.Fatalf("comments file not written: %v", err)
	}
	var cf CommentsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatal(err)
	}
	if cf.ShareURL != "https://crit.live/r/urlonly" {
		t.Errorf("share_url in file = %q, want https://crit.live/r/urlonly", cf.ShareURL)
	}
}

func TestSetGetDeleteToken(t *testing.T) {
	doc := newTestDoc(t, "line1")
	if doc.GetDeleteToken() != "" {
		t.Error("expected empty delete token initially")
	}
	doc.SetDeleteToken("abc123deletetoken1234")
	if doc.GetDeleteToken() != "abc123deletetoken1234" {
		t.Errorf("delete token = %q, want abc123deletetoken1234", doc.GetDeleteToken())
	}
}

func TestDeleteToken_PersistedAndLoaded(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "note")
	doc.SetDeleteToken("persisttoken12345678901")
	writeAndStop(doc)

	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if doc2.GetDeleteToken() != "persisttoken12345678901" {
		t.Errorf("delete token after reload = %q", doc2.GetDeleteToken())
	}
}

func TestReloadFile_PreservesPreviousContent(t *testing.T) {
	doc := newTestDoc(t, "original line 1\noriginal line 2")
	doc.AddComment(1, 1, "fix this")

	// Modify the file
	if err := os.WriteFile(doc.FilePath, []byte("modified line 1\nnew line 2\nnew line 3"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := doc.ReloadFile(); err != nil {
		t.Fatal(err)
	}

	if doc.PreviousContent != "original line 1\noriginal line 2" {
		t.Errorf("PreviousContent = %q, want original content", doc.PreviousContent)
	}
	if len(doc.PreviousComments) != 1 {
		t.Errorf("PreviousComments len = %d, want 1", len(doc.PreviousComments))
	}
	if doc.PreviousComments[0].Body != "fix this" {
		t.Errorf("PreviousComments[0].Body = %q, want 'fix this'", doc.PreviousComments[0].Body)
	}
}

func TestEditCounting(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.IncrementEdits()
	doc.IncrementEdits()
	if doc.GetPendingEdits() != 2 {
		t.Errorf("pending edits = %d, want 2", doc.GetPendingEdits())
	}
	doc.SignalRoundComplete()
	if doc.GetPendingEdits() != 0 {
		t.Errorf("pending edits after round-complete = %d, want 0", doc.GetPendingEdits())
	}
}

func TestSignalRoundComplete_IncrementsRound(t *testing.T) {
	doc := newTestDoc(t, "original")
	if doc.reviewRound != 1 {
		t.Errorf("initial reviewRound = %d, want 1", doc.reviewRound)
	}
	doc.SignalRoundComplete()
	if doc.reviewRound != 2 {
		t.Errorf("reviewRound after first round-complete = %d, want 2", doc.reviewRound)
	}
	doc.SignalRoundComplete()
	if doc.reviewRound != 3 {
		t.Errorf("reviewRound after second round-complete = %d, want 3", doc.reviewRound)
	}
}

func TestSignalRoundComplete_ClearsComments(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "fix this")
	doc.AddComment(2, 2, "and this")
	if len(doc.GetComments()) != 2 {
		t.Fatalf("expected 2 comments before round-complete, got %d", len(doc.GetComments()))
	}

	doc.SignalRoundComplete()

	if len(doc.GetComments()) != 0 {
		t.Errorf("expected 0 comments after round-complete, got %d", len(doc.GetComments()))
	}
	// Verify nextID resets so new comments start at c1
	c := doc.AddComment(1, 1, "new round comment")
	if c.ID != "c1" {
		t.Errorf("new comment ID = %q, want c1 (nextID should reset)", c.ID)
	}
}

func TestDeleteToken_PersistsWhenStale(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.AddComment(1, 1, "note")
	doc.SetDeleteToken("staletoken123456789012")
	writeAndStop(doc)

	if err := os.WriteFile(doc.FilePath, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if doc2.GetDeleteToken() != "staletoken123456789012" {
		t.Errorf("delete token after stale reload = %q", doc2.GetDeleteToken())
	}
}

func TestReloadFile_SnapshotsOnlyOnFirstEdit(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.AddComment(1, 1, "fix this")

	// First edit (pendingEdits == 0) — should snapshot
	if err := os.WriteFile(doc.FilePath, []byte("edit 1"), 0644); err != nil {
		t.Fatal(err)
	}
	doc.ReloadFile()
	doc.IncrementEdits() // simulate WatchFile behavior

	// Second edit (pendingEdits == 1) — should NOT overwrite snapshot
	if err := os.WriteFile(doc.FilePath, []byte("edit 2"), 0644); err != nil {
		t.Fatal(err)
	}
	doc.ReloadFile()

	if doc.PreviousContent != "original" {
		t.Errorf("PreviousContent = %q, want 'original' (should not be overwritten by second edit)", doc.PreviousContent)
	}
	if len(doc.PreviousComments) != 1 || doc.PreviousComments[0].Body != "fix this" {
		t.Errorf("PreviousComments should preserve round-start comments, got %+v", doc.PreviousComments)
	}
	if doc.Content != "edit 2" {
		t.Errorf("Content = %q, want 'edit 2'", doc.Content)
	}
}

func TestLoadResolvedComments(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "fix this")

	// Write comments JSON with resolved fields (as agent would)
	cf := CommentsFile{
		File:     doc.FileName,
		FileHash: doc.FileHash,
		Comments: []Comment{
			{
				ID: "c1", StartLine: 1, EndLine: 1, Body: "fix this",
				Resolved: true, ResolutionNote: "Fixed it",
				ResolutionLines: []int{3, 4},
			},
		},
	}
	data, _ := json.MarshalIndent(cf, "", "  ")
	os.WriteFile(doc.commentsFilePath(), data, 0644)

	doc.loadResolvedComments()

	if len(doc.PreviousComments) != 1 {
		t.Fatalf("expected 1 previous comment, got %d", len(doc.PreviousComments))
	}
	if !doc.PreviousComments[0].Resolved {
		t.Error("expected comment to be resolved")
	}
	if doc.PreviousComments[0].ResolutionNote != "Fixed it" {
		t.Errorf("resolution note = %q", doc.PreviousComments[0].ResolutionNote)
	}
}

func TestGetReviewRound(t *testing.T) {
	doc := newTestDoc(t, "hello")
	if got := doc.GetReviewRound(); got != 1 {
		t.Errorf("initial round = %d, want 1", got)
	}
}

func TestSignalRoundComplete_PreservesEditCount(t *testing.T) {
	doc := newTestDoc(t, "hello")
	doc.IncrementEdits()
	doc.IncrementEdits()
	doc.IncrementEdits()
	doc.SignalRoundComplete()
	if got := doc.GetLastRoundEdits(); got != 3 {
		t.Errorf("lastRoundEdits = %d, want 3", got)
	}
	if got := doc.GetPendingEdits(); got != 0 {
		t.Errorf("pendingEdits = %d, want 0 after round complete", got)
	}
}

func TestLoadComments_WithResolved(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "fix this")

	// Manually write a comments file with resolved fields
	cf := CommentsFile{
		File:     doc.FileName,
		FileHash: doc.FileHash,
		Comments: []Comment{
			{
				ID:              "c1",
				StartLine:       1,
				EndLine:         1,
				Body:            "fix this",
				Resolved:        true,
				ResolutionNote:  "Refactored the function",
				ResolutionLines: []int{3, 4, 5},
			},
		},
	}
	data, _ := json.MarshalIndent(cf, "", "  ")
	os.WriteFile(doc.commentsFilePath(), data, 0644)

	// Reload document
	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	comments := doc2.GetComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if !comments[0].Resolved {
		t.Error("expected comment to be resolved")
	}
	if comments[0].ResolutionNote != "Refactored the function" {
		t.Errorf("resolution note = %q", comments[0].ResolutionNote)
	}
	if len(comments[0].ResolutionLines) != 3 {
		t.Errorf("resolution lines = %v", comments[0].ResolutionLines)
	}
}

func TestCarryForwardUnresolved_Basic(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2\nline3")

	// Simulate a round: set previous content/comments, update content
	doc.PreviousContent = "line1\nline2\nline3"
	doc.PreviousComments = []Comment{
		{ID: "1", StartLine: 2, EndLine: 2, Body: "Fix this", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "2", StartLine: 3, EndLine: 3, Body: "Resolved one", Resolved: true, ResolutionNote: "Done"},
	}
	doc.Content = "line1\nline2\nline3" // same content
	doc.nextID = 1

	doc.carryForwardUnresolved()

	if len(doc.Comments) != 1 {
		t.Fatalf("expected 1 carried-forward comment, got %d", len(doc.Comments))
	}
	c := doc.Comments[0]
	if c.ID != "c1" {
		t.Errorf("ID = %q, want %q (must use c-prefix format)", c.ID, "c1")
	}
	if c.Body != "Fix this" {
		t.Errorf("body = %q, want %q", c.Body, "Fix this")
	}
	if c.StartLine != 2 || c.EndLine != 2 {
		t.Errorf("lines = %d-%d, want 2-2", c.StartLine, c.EndLine)
	}
	if c.Resolved {
		t.Error("carried-forward comment should not be resolved")
	}
}

func TestCarryForwardUnresolved_RemappedLines(t *testing.T) {
	doc := newTestDoc(t, "line1\nnew\nline2\nline3")

	// Old content had comment on line 2, new content inserted a line before it
	doc.PreviousContent = "line1\nline2\nline3"
	doc.PreviousComments = []Comment{
		{ID: "1", StartLine: 2, EndLine: 2, Body: "Check this", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	doc.Content = "line1\nnew\nline2\nline3" // "new" inserted at line 2
	doc.nextID = 1

	doc.carryForwardUnresolved()

	if len(doc.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(doc.Comments))
	}
	c := doc.Comments[0]
	// "line2" moved from old line 2 to new line 3
	if c.StartLine != 3 {
		t.Errorf("StartLine = %d, want 3", c.StartLine)
	}
	if c.EndLine != 3 {
		t.Errorf("EndLine = %d, want 3", c.EndLine)
	}
}

func TestCarryForwardUnresolved_NoPreviousContent(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.PreviousContent = ""
	doc.PreviousComments = []Comment{
		{ID: "1", StartLine: 1, EndLine: 1, Body: "test"},
	}
	doc.nextID = 1

	doc.carryForwardUnresolved()

	if len(doc.Comments) != 0 {
		t.Errorf("expected 0 comments when no previous content, got %d", len(doc.Comments))
	}
}

func TestCarryForwardUnresolved_AllResolved(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.PreviousContent = "line1\nline2"
	doc.PreviousComments = []Comment{
		{ID: "1", StartLine: 1, EndLine: 1, Body: "Done", Resolved: true},
	}
	doc.nextID = 1

	doc.carryForwardUnresolved()

	if len(doc.Comments) != 0 {
		t.Errorf("expected 0 comments when all resolved, got %d", len(doc.Comments))
	}
}
