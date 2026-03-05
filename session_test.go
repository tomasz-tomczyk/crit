package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestSession_NewCommentCount(t *testing.T) {
	s := newTestSession(t)
	s.AddComment("plan.md", 1, 1, "", "new one")
	s.AddComment("plan.md", 2, 2, "", "new two")

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

	s.AddComment("plan.md", 1, 1, "", "output dir comment")
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
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

	s.AddComment("plan.md", 1, 1, "", "persisted in output dir")
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
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

	session, err := NewSessionFromGit()
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

	session, err := NewSessionFromGit()
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
