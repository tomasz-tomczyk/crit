package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseNameStatus(t *testing.T) {
	input := "M\tserver.go\nA\tnew.go\nD\told.go\nR100\told_name.go\tnew_name.go"
	changes := parseNameStatus(input)

	if len(changes) != 4 {
		t.Fatalf("expected 4 changes, got %d", len(changes))
	}
	if changes[0].Path != "server.go" || changes[0].Status != "modified" {
		t.Errorf("changes[0] = %+v", changes[0])
	}
	if changes[1].Path != "new.go" || changes[1].Status != "added" {
		t.Errorf("changes[1] = %+v", changes[1])
	}
	if changes[2].Path != "old.go" || changes[2].Status != "deleted" {
		t.Errorf("changes[2] = %+v", changes[2])
	}
	if changes[3].Path != "new_name.go" || changes[3].Status != "renamed" {
		t.Errorf("changes[3] = %+v", changes[3])
	}
}

func TestParseNameStatus_Empty(t *testing.T) {
	changes := parseNameStatus("")
	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(changes))
	}
}

func TestDedup(t *testing.T) {
	input := []FileChange{
		{Path: "a.go", Status: "modified"},
		{Path: "b.go", Status: "added"},
		{Path: "a.go", Status: "added"}, // duplicate
	}
	result := dedup(input)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Status != "modified" {
		t.Error("should keep first occurrence")
	}
}

func TestParseUnifiedDiff_Simple(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
index abc..def 100644
--- a/file.go
+++ b/file.go
@@ -1,4 +1,5 @@
 package main

+import "fmt"
+
 func main() {
-	println("hello")
+	fmt.Println("hello")
 }
`
	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	h := hunks[0]
	if h.OldStart != 1 || h.OldCount != 4 || h.NewStart != 1 || h.NewCount != 5 {
		t.Errorf("hunk header: old=%d,%d new=%d,%d", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
	}

	// Count line types
	adds, dels, ctx := 0, 0, 0
	for _, l := range h.Lines {
		switch l.Type {
		case "add":
			adds++
		case "del":
			dels++
		case "context":
			ctx++
		}
	}
	if adds != 3 {
		t.Errorf("expected 3 adds, got %d", adds)
	}
	if dels != 1 {
		t.Errorf("expected 1 del, got %d", dels)
	}
	if ctx != 4 {
		// 4 context lines: "package main", (blank line), "func main() {", "}"
		t.Errorf("expected 4 context lines, got %d", ctx)
	}
}

func TestParseUnifiedDiff_MultipleHunks(t *testing.T) {
	diff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
@@ -10,3 +10,4 @@
 line10
 line11
+line11.5
 line12
`
	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if hunks[1].NewCount != 4 {
		t.Errorf("second hunk NewCount = %d, want 4", hunks[1].NewCount)
	}
}

func TestParseUnifiedDiff_Empty(t *testing.T) {
	hunks := ParseUnifiedDiff("")
	if len(hunks) != 0 {
		t.Errorf("expected 0 hunks, got %d", len(hunks))
	}
}

func TestParseUnifiedDiff_LineNumbers(t *testing.T) {
	diff := `--- a/file.go
+++ b/file.go
@@ -5,4 +5,5 @@
 context
-old line
+new line
+added line
 context2
`
	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	lines := hunks[0].Lines
	// context: old=5, new=5
	if lines[0].OldNum != 5 || lines[0].NewNum != 5 {
		t.Errorf("context line: old=%d new=%d, want 5,5", lines[0].OldNum, lines[0].NewNum)
	}
	// del: old=6
	if lines[1].OldNum != 6 || lines[1].NewNum != 0 {
		t.Errorf("del line: old=%d new=%d, want 6,0", lines[1].OldNum, lines[1].NewNum)
	}
	// add: new=6
	if lines[2].OldNum != 0 || lines[2].NewNum != 6 {
		t.Errorf("add line: old=%d new=%d, want 0,6", lines[2].OldNum, lines[2].NewNum)
	}
	// add: new=7
	if lines[3].NewNum != 7 {
		t.Errorf("second add: new=%d, want 7", lines[3].NewNum)
	}
	// context2: old=7, new=8
	if lines[4].OldNum != 7 || lines[4].NewNum != 8 {
		t.Errorf("context2: old=%d new=%d, want 7,8", lines[4].OldNum, lines[4].NewNum)
	}
}

func TestFileDiffUnifiedNewFile(t *testing.T) {
	content := "line1\nline2\nline3\n"
	hunks := FileDiffUnifiedNewFile(content)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	h := hunks[0]
	if h.OldStart != 0 || h.OldCount != 0 {
		t.Errorf("old: %d,%d, want 0,0", h.OldStart, h.OldCount)
	}
	if h.NewStart != 1 || h.NewCount != 3 {
		t.Errorf("new: %d,%d, want 1,3", h.NewStart, h.NewCount)
	}
	if len(h.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(h.Lines))
	}
	for i, l := range h.Lines {
		if l.Type != "add" {
			t.Errorf("line %d type = %q, want add", i, l.Type)
		}
		if l.NewNum != i+1 {
			t.Errorf("line %d NewNum = %d, want %d", i, l.NewNum, i+1)
		}
	}
}

func TestFileDiffUnifiedNewFile_Empty(t *testing.T) {
	hunks := FileDiffUnifiedNewFile("")
	if len(hunks) != 0 {
		t.Errorf("expected 0 hunks for empty content, got %d", len(hunks))
	}
}

func TestChangedFiles_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify a file
	writeFile(t, filepath.Join(dir, "README.md"), "# Modified")
	// Add a new file
	writeFile(t, filepath.Join(dir, "new.go"), "package main")

	changes, err := ChangedFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) < 2 {
		t.Fatalf("expected at least 2 changes, got %d: %+v", len(changes), changes)
	}

	paths := map[string]string{}
	for _, c := range changes {
		paths[c.Path] = c.Status
	}
	if paths["README.md"] != "modified" {
		t.Errorf("README.md status = %q, want modified", paths["README.md"])
	}
	if paths["new.go"] != "untracked" {
		t.Errorf("new.go status = %q, want untracked", paths["new.go"])
	}
}

func TestChangedFiles_FeatureBranch(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a feature branch and add a file
	runGit(t, dir, "checkout", "-b", "feature/test")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "add feature")

	// Also modify a file without committing
	writeFile(t, filepath.Join(dir, "README.md"), "# Updated")

	changes, err := ChangedFiles()
	if err != nil {
		t.Fatal(err)
	}

	paths := map[string]string{}
	for _, c := range changes {
		paths[c.Path] = c.Status
	}
	if _, ok := paths["feature.go"]; !ok {
		t.Error("expected feature.go in changes")
	}
	if _, ok := paths["README.md"]; !ok {
		t.Error("expected README.md in changes")
	}
}

func TestFileDiffUnified_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify README.md
	writeFile(t, filepath.Join(dir, "README.md"), "# Modified\n\nNew content\n")
	runGit(t, dir, "add", "README.md")

	hunks, err := FileDiffUnified("README.md", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected at least one hunk")
	}
}

func TestWorkingTreeFingerprint_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	fp1 := WorkingTreeFingerprint()

	writeFile(t, filepath.Join(dir, "new.txt"), "hello")
	fp2 := WorkingTreeFingerprint()

	if fp1 == fp2 {
		t.Error("fingerprint should change after adding a file")
	}
}

func TestCurrentBranch_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	branch := CurrentBranch()
	if branch != "main" {
		t.Errorf("CurrentBranch = %q, want main", branch)
	}

	runGit(t, dir, "checkout", "-b", "feature/test")
	branch = CurrentBranch()
	if branch != "feature/test" {
		t.Errorf("CurrentBranch = %q, want feature/test", branch)
	}
}

func TestRepoRoot_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	root, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expectedDir, _ := filepath.EvalSymlinks(dir)
	actualRoot, _ := filepath.EvalSymlinks(root)
	if actualRoot != expectedDir {
		t.Errorf("RepoRoot = %q, want %q", actualRoot, expectedDir)
	}
}

func TestDefaultBranchOverride(t *testing.T) {
	// Save and restore global state
	origOverride := defaultBranchOverride
	origResult := defaultBranchResult
	t.Cleanup(func() {
		defaultBranchOverride = origOverride
		defaultBranchOnce = sync.Once{}
		defaultBranchResult = origResult
	})

	// Without override, auto-detection runs (result may vary by environment).
	// We only test that the override takes precedence.
	defaultBranchOverride = "uat"
	if got := DefaultBranch(); got != "uat" {
		t.Errorf("DefaultBranch() = %q, want %q", got, "uat")
	}

	// Changing the override is reflected immediately (no cache involved).
	defaultBranchOverride = "develop"
	if got := DefaultBranch(); got != "develop" {
		t.Errorf("DefaultBranch() = %q, want %q", got, "develop")
	}

	// Clearing the override falls back to auto-detection via sync.Once.
	defaultBranchOverride = ""
	defaultBranchOnce = sync.Once{}
	defaultBranchResult = ""
	// Just verify it doesn't panic and returns something non-empty.
	if got := DefaultBranch(); got == "" {
		t.Error("DefaultBranch() returned empty string without override")
	}
}

func TestRemoteBranches(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a bare remote and push branches
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "origin", "main")

	runGit(t, dir, "checkout", "-b", "production")
	writeFile(t, filepath.Join(dir, "prod.go"), "package main\n")
	runGit(t, dir, "add", "prod.go")
	runGit(t, dir, "commit", "-m", "prod")
	runGit(t, dir, "push", "origin", "production")

	runGit(t, dir, "checkout", "main")

	branches, err := RemoteBranches(dir)
	if err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}

	// Should contain "main" and "production", not "HEAD"
	has := map[string]bool{}
	for _, b := range branches {
		has[b] = true
	}
	if !has["main"] {
		t.Errorf("expected 'main' in branches, got %v", branches)
	}
	if !has["production"] {
		t.Errorf("expected 'production' in branches, got %v", branches)
	}
	if has["HEAD"] {
		t.Error("HEAD should be filtered out")
	}
}

func TestChangedFilesBranch(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Record the main branch commit as base ref
	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Create a feature branch and commit a change
	runGit(t, dir, "checkout", "-b", "feature/scoped")
	writeFile(t, filepath.Join(dir, "feature.go"), "package main\n\nfunc Feature() {}\n")
	runGit(t, dir, "add", "feature.go")
	runGit(t, dir, "commit", "-m", "add feature")

	changes, err := changedFilesBranch(baseRef)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Path != "feature.go" || changes[0].Status != "added" {
		t.Errorf("change = %+v, want {Path:feature.go Status:added}", changes[0])
	}
}

func TestChangedFilesBranch_EmptyBaseRef(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	changes, err := changedFilesBranch("")
	if err != nil {
		t.Fatal(err)
	}
	if changes != nil {
		t.Errorf("expected nil for empty baseRef, got %+v", changes)
	}
}

func TestChangedFilesStaged(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Stage a modification without committing
	writeFile(t, filepath.Join(dir, "README.md"), "# Staged change")
	runGit(t, dir, "add", "README.md")

	staged, err := changedFilesStaged()
	if err != nil {
		t.Fatal(err)
	}

	paths := map[string]string{}
	for _, c := range staged {
		paths[c.Path] = c.Status
	}
	if paths["README.md"] != "modified" {
		t.Errorf("expected README.md as modified in staged, got %+v", staged)
	}

	// Unstaged should NOT contain the staged file
	unstaged, err := changedFilesUnstaged()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range unstaged {
		if c.Path == "README.md" {
			t.Error("README.md should not appear in unstaged after being staged")
		}
	}
}

func TestChangedFilesUnstaged(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify a tracked file without staging
	writeFile(t, filepath.Join(dir, "README.md"), "# Unstaged change")

	unstaged, err := changedFilesUnstaged()
	if err != nil {
		t.Fatal(err)
	}

	paths := map[string]string{}
	for _, c := range unstaged {
		paths[c.Path] = c.Status
	}
	if paths["README.md"] != "modified" {
		t.Errorf("expected README.md as modified in unstaged, got %+v", unstaged)
	}

	// Staged should NOT contain the unstaged file
	staged, err := changedFilesStaged()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range staged {
		if c.Path == "README.md" {
			t.Error("README.md should not appear in staged when only unstaged")
		}
	}
}

func TestChangedFilesUnstaged_IncludesUntracked(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create an untracked file
	writeFile(t, filepath.Join(dir, "untracked.go"), "package main")

	unstaged, err := changedFilesUnstaged()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, c := range unstaged {
		if c.Path == "untracked.go" && c.Status == "untracked" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected untracked.go in unstaged changes, got %+v", unstaged)
	}
}

func TestChangedFilesScoped_Dispatcher(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Create a feature branch with a committed change
	runGit(t, dir, "checkout", "-b", "feature/dispatch")
	writeFile(t, filepath.Join(dir, "branch.go"), "package main")
	runGit(t, dir, "add", "branch.go")
	runGit(t, dir, "commit", "-m", "branch change")

	// Stage a file
	writeFile(t, filepath.Join(dir, "staged.go"), "package main")
	runGit(t, dir, "add", "staged.go")

	// Leave a file unstaged
	writeFile(t, filepath.Join(dir, "README.md"), "# Unstaged")

	// Test "branch" scope
	branchChanges, err := ChangedFilesScoped("branch", baseRef)
	if err != nil {
		t.Fatal(err)
	}
	branchPaths := map[string]bool{}
	for _, c := range branchChanges {
		branchPaths[c.Path] = true
	}
	if !branchPaths["branch.go"] {
		t.Error("branch scope should include branch.go")
	}

	// Test "staged" scope
	stagedChanges, err := ChangedFilesScoped("staged", "")
	if err != nil {
		t.Fatal(err)
	}
	stagedPaths := map[string]bool{}
	for _, c := range stagedChanges {
		stagedPaths[c.Path] = true
	}
	if !stagedPaths["staged.go"] {
		t.Error("staged scope should include staged.go")
	}

	// Test "unstaged" scope
	unstagedChanges, err := ChangedFilesScoped("unstaged", "")
	if err != nil {
		t.Fatal(err)
	}
	unstagedPaths := map[string]bool{}
	for _, c := range unstagedChanges {
		unstagedPaths[c.Path] = true
	}
	if !unstagedPaths["README.md"] {
		t.Error("unstaged scope should include README.md")
	}

	// Test default scope (falls back to ChangedFiles)
	defaultChanges, err := ChangedFilesScoped("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultChanges) == 0 {
		t.Error("default scope should return changes via ChangedFiles()")
	}
}

func TestFileDiffScoped_Branch(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Create feature branch with a change to README.md
	runGit(t, dir, "checkout", "-b", "feature/diff-scope")
	writeFile(t, filepath.Join(dir, "README.md"), "# Modified on branch\n\nNew content\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "modify readme")

	hunks, err := FileDiffScoped("README.md", "branch", baseRef, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected diff hunks for branch scope")
	}
}

func TestFileDiffScoped_Staged(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Stage a change
	writeFile(t, filepath.Join(dir, "README.md"), "# Staged content\n")
	runGit(t, dir, "add", "README.md")

	hunks, err := FileDiffScoped("README.md", "staged", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected diff hunks for staged scope")
	}
}

func TestFileDiffScoped_Unstaged(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify without staging
	writeFile(t, filepath.Join(dir, "README.md"), "# Unstaged content\n")

	hunks, err := FileDiffScoped("README.md", "unstaged", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected diff hunks for unstaged scope")
	}
}

func TestFileDiffScoped_Default(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify and stage a change
	writeFile(t, filepath.Join(dir, "README.md"), "# Default scope\n")
	runGit(t, dir, "add", "README.md")

	hunks, err := FileDiffScoped("README.md", "", "HEAD", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Error("expected diff hunks for default scope (delegates to FileDiffUnified)")
	}
}

func TestFileDiffScoped_BranchEmptyBaseRef(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	hunks, err := FileDiffScoped("README.md", "branch", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if hunks != nil {
		t.Errorf("expected nil for branch scope with empty baseRef, got %+v", hunks)
	}
}

func TestFileDiffScoped_DifferentHunksPerScope(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Commit a change on a branch
	runGit(t, dir, "checkout", "-b", "feature/multi-scope")
	writeFile(t, filepath.Join(dir, "README.md"), "# Branch change\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "branch edit")

	// Stage a further change
	writeFile(t, filepath.Join(dir, "README.md"), "# Branch change\n\nStaged line\n")
	runGit(t, dir, "add", "README.md")

	// Make an unstaged change on top
	writeFile(t, filepath.Join(dir, "README.md"), "# Branch change\n\nStaged line\nUnstaged line\n")

	branchHunks, err := FileDiffScoped("README.md", "branch", baseRef, dir)
	if err != nil {
		t.Fatal(err)
	}

	stagedHunks, err := FileDiffScoped("README.md", "staged", "", dir)
	if err != nil {
		t.Fatal(err)
	}

	unstagedHunks, err := FileDiffScoped("README.md", "unstaged", "", dir)
	if err != nil {
		t.Fatal(err)
	}

	// All three scopes should return hunks
	if len(branchHunks) == 0 {
		t.Error("expected branch hunks")
	}
	if len(stagedHunks) == 0 {
		t.Error("expected staged hunks")
	}
	if len(unstagedHunks) == 0 {
		t.Error("expected unstaged hunks")
	}

	// The hunks should differ because each scope sees a different diff.
	// Branch scope: "# Test" -> "# Branch change\n\nStaged line\n" (committed content vs base)
	// Wait — branch is baseRef..HEAD so it only sees committed changes, not staged/unstaged.
	// Staged scope: committed content -> staged content (adding "Staged line")
	// Unstaged scope: staged content -> working tree (adding "Unstaged line")

	// Count total add lines per scope to verify they differ
	countAdds := func(hunks []DiffHunk) int {
		n := 0
		for _, h := range hunks {
			for _, l := range h.Lines {
				if l.Type == "add" {
					n++
				}
			}
		}
		return n
	}

	branchAdds := countAdds(branchHunks)
	stagedAdds := countAdds(stagedHunks)
	unstagedAdds := countAdds(unstagedHunks)

	// Branch sees: "# Test" -> "# Branch change" (the committed state)
	// Staged sees: "# Branch change" -> "# Branch change\n\nStaged line"
	// Unstaged sees: "# Branch change\n\nStaged line" -> "# Branch change\n\nStaged line\nUnstaged line"
	// So each scope should have different add counts.
	if branchAdds == stagedAdds && stagedAdds == unstagedAdds {
		t.Errorf("expected different add counts per scope, all were %d", branchAdds)
	}
}

// TestParseUnifiedDiff_BlankContextLines verifies that blank context lines
// (empty lines in the source that appear as bare empty lines in diff output
// without a leading space, e.g. when diff.suppressBlankEmpty is set) are
// correctly treated as context lines and don't desync line numbers.
func TestParseUnifiedDiff_BlankContextLines(t *testing.T) {
	// Simulate a diff where blank context lines have no leading space.
	// This happens when git's diff.suppressBlankEmpty config is true,
	// or with some git versions that strip trailing whitespace from blank lines.
	diff := "--- a/file.ex\n+++ b/file.ex\n@@ -1,5 +1,5 @@\n line1\n\n line3\n-old\n+new\n line5\n"
	// The empty line between "line1" and "line3" has NO leading space.
	// It should be treated as a context line.

	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}

	h := hunks[0]

	// Count line types
	adds, dels, ctx := 0, 0, 0
	for _, l := range h.Lines {
		switch l.Type {
		case "add":
			adds++
		case "del":
			dels++
		case "context":
			ctx++
		}
	}

	// With the blank line properly handled as context:
	// line1 (ctx), blank (ctx), line3 (ctx), old (del), new (add), line5 (ctx) = 4 ctx, 1 del, 1 add
	if adds != 1 {
		t.Errorf("expected 1 add, got %d", adds)
	}
	if dels != 1 {
		t.Errorf("expected 1 del, got %d", dels)
	}
	if ctx != 4 {
		t.Errorf("expected 4 context lines (including blank), got %d", ctx)
	}

	// Verify line numbers are correct (not desynced by the blank line)
	// Expected: line1 (old=1,new=1), blank (old=2,new=2), line3 (old=3,new=3),
	//           old (old=4), new (new=4), line5 (old=5,new=5)
	if len(h.Lines) != 6 {
		t.Fatalf("expected 6 lines, got %d", len(h.Lines))
	}

	// Check the del line has correct old line number
	delLine := h.Lines[3]
	if delLine.Type != "del" || delLine.OldNum != 4 {
		t.Errorf("del line: type=%q oldNum=%d, want type=del oldNum=4", delLine.Type, delLine.OldNum)
	}

	// Check the add line has correct new line number
	addLine := h.Lines[4]
	if addLine.Type != "add" || addLine.NewNum != 4 {
		t.Errorf("add line: type=%q newNum=%d, want type=add newNum=4", addLine.Type, addLine.NewNum)
	}

	// Check last context line has correct numbers
	lastLine := h.Lines[5]
	if lastLine.OldNum != 5 || lastLine.NewNum != 5 {
		t.Errorf("last context: old=%d new=%d, want 5,5", lastLine.OldNum, lastLine.NewNum)
	}
}

// TestFileDiff_SuppressBlankEmpty verifies correct parsing when
// git's diff.suppressBlankEmpty config strips leading spaces from blank context lines.
func TestFileDiff_SuppressBlankEmpty(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Enable suppressBlankEmpty
	runGit(t, dir, "config", "diff.suppressBlankEmpty", "true")

	// File with blank lines between code blocks
	original := "func A() {\n}\n\nfunc B() {\n\treturn old\n}\n\nfunc C() {\n}\n"
	modified := "func A() {\n}\n\nfunc B() {\n\treturn new\n}\n\nfunc C() {\n}\n"

	writeFile(t, filepath.Join(dir, "code.go"), original)
	runGit(t, dir, "add", "code.go")
	runGit(t, dir, "commit", "-m", "add code")

	writeFile(t, filepath.Join(dir, "code.go"), modified)

	hunks, err := fileDiffUnified("code.go", "HEAD", dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}

	// Verify the del/add lines are correctly identified
	var foundDel, foundAdd bool
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Type == "del" && strings.Contains(l.Content, "return old") {
				foundDel = true
			}
			if l.Type == "add" && strings.Contains(l.Content, "return new") {
				foundAdd = true
			}
		}
	}
	if !foundDel {
		t.Error("missing del line with 'return old'")
	}
	if !foundAdd {
		t.Error("missing add line with 'return new'")
	}

	// Verify blank lines are preserved as context (not dropped)
	blankContextCount := 0
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Type == "context" && l.Content == "" {
				blankContextCount++
			}
		}
	}
	if blankContextCount == 0 {
		t.Error("blank context lines were dropped — suppressBlankEmpty handling broken")
	}
	t.Logf("blank context lines found: %d", blankContextCount)
}

// TestParseUnifiedDiff_BlankContextAtEndOfHunk verifies that a bare blank
// context line at the very end of a hunk is correctly consumed.
func TestParseUnifiedDiff_BlankContextAtEndOfHunk(t *testing.T) {
	// Hunk: line1 (ctx), old (del), new (add), blank-at-end (ctx).
	// A second hunk follows so the blank line isn't the last thing in the
	// diff string (TrimRight would eat it otherwise).
	diff := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n line1\n-old\n+new\n\n@@ -10,2 +10,2 @@\n-x\n+y\n"

	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	h := hunks[0]
	if len(h.Lines) != 4 {
		t.Fatalf("expected 4 lines in first hunk, got %d", len(h.Lines))
	}

	last := h.Lines[3]
	if last.Type != "context" {
		t.Errorf("last line type=%q, want context", last.Type)
	}
	if last.Content != "" {
		t.Errorf("last line content=%q, want empty string", last.Content)
	}
	if last.OldNum != 3 || last.NewNum != 3 {
		t.Errorf("last line numbers: old=%d new=%d, want 3,3", last.OldNum, last.NewNum)
	}
}

// TestParseUnifiedDiff_ConsecutiveBlankContextLines verifies that multiple
// consecutive bare blank lines are each treated as context lines.
func TestParseUnifiedDiff_ConsecutiveBlankContextLines(t *testing.T) {
	// Hunk with two consecutive blank context lines between content.
	// line1 (ctx), blank (ctx), blank (ctx), old (del), new (add), line5 (ctx)
	diff := "--- a/f.txt\n+++ b/f.txt\n@@ -1,5 +1,5 @@\n line1\n\n\n-old\n+new\n line5\n"

	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}

	h := hunks[0]
	if len(h.Lines) != 6 {
		t.Fatalf("expected 6 lines, got %d", len(h.Lines))
	}

	// Lines[1] and Lines[2] should both be blank context lines
	for _, i := range []int{1, 2} {
		l := h.Lines[i]
		if l.Type != "context" || l.Content != "" {
			t.Errorf("line %d: type=%q content=%q, want context with empty content", i, l.Type, l.Content)
		}
	}

	// Verify line numbers didn't desync
	delLine := h.Lines[3]
	if delLine.Type != "del" || delLine.OldNum != 4 {
		t.Errorf("del line: type=%q oldNum=%d, want del at 4", delLine.Type, delLine.OldNum)
	}
	addLine := h.Lines[4]
	if addLine.Type != "add" || addLine.NewNum != 4 {
		t.Errorf("add line: type=%q newNum=%d, want add at 4", addLine.Type, addLine.NewNum)
	}
	lastLine := h.Lines[5]
	if lastLine.OldNum != 5 || lastLine.NewNum != 5 {
		t.Errorf("last context: old=%d new=%d, want 5,5", lastLine.OldNum, lastLine.NewNum)
	}
}

// TestParseUnifiedDiff_MultipleHunksWithBlankLines verifies that blank context
// lines in multiple hunks are each handled independently.
func TestParseUnifiedDiff_MultipleHunksWithBlankLines(t *testing.T) {
	// Two hunks, each containing a bare blank context line.
	diff := "--- a/f.txt\n+++ b/f.txt\n" +
		"@@ -1,4 +1,4 @@\n line1\n\n-old1\n+new1\n line4\n" +
		"@@ -10,4 +10,4 @@\n line10\n\n-old2\n+new2\n line13\n"

	hunks := ParseUnifiedDiff(diff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	for i, h := range hunks {
		// Each hunk: content (ctx), blank (ctx), old (del), new (add), content (ctx)
		if len(h.Lines) != 5 {
			t.Errorf("hunk %d: expected 5 lines, got %d", i, len(h.Lines))
			continue
		}

		blank := h.Lines[1]
		if blank.Type != "context" || blank.Content != "" {
			t.Errorf("hunk %d blank line: type=%q content=%q, want context with empty", i, blank.Type, blank.Content)
		}

		del := h.Lines[2]
		if del.Type != "del" {
			t.Errorf("hunk %d: line 2 type=%q, want del", i, del.Type)
		}

		add := h.Lines[3]
		if add.Type != "add" {
			t.Errorf("hunk %d: line 3 type=%q, want add", i, add.Type)
		}
	}

	// Verify line numbers in second hunk
	h2 := hunks[1]
	if h2.Lines[0].OldNum != 10 || h2.Lines[0].NewNum != 10 {
		t.Errorf("hunk 2 first line: old=%d new=%d, want 10,10", h2.Lines[0].OldNum, h2.Lines[0].NewNum)
	}
	if h2.Lines[1].OldNum != 11 || h2.Lines[1].NewNum != 11 {
		t.Errorf("hunk 2 blank line: old=%d new=%d, want 11,11", h2.Lines[1].OldNum, h2.Lines[1].NewNum)
	}
}

func TestCommitLog(t *testing.T) {
	dir := initTestRepo(t)

	// Record the main branch commit as base ref
	baseRef := runGit(t, dir, "rev-parse", "HEAD")

	// Create a feature branch with two commits
	runGit(t, dir, "checkout", "-b", "feature/commits")
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n\nfunc A() {}\n")
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "commit", "-m", "add function A")

	writeFile(t, filepath.Join(dir, "b.go"), "package main\n\nfunc B() {}\n")
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "commit", "-m", "add function B")

	commits, err := CommitLog(baseRef, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	// Newest commit first
	if commits[0].Message != "add function B" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "add function B")
	}
	if commits[1].Message != "add function A" {
		t.Errorf("commits[1].Message = %q, want %q", commits[1].Message, "add function A")
	}

	// Short SHAs should be 7 characters
	for i, c := range commits {
		if len(c.ShortSHA) != 7 {
			t.Errorf("commits[%d].ShortSHA = %q, want 7 chars", i, c.ShortSHA)
		}
		if c.SHA == "" {
			t.Errorf("commits[%d].SHA is empty", i)
		}
		if c.Author == "" {
			t.Errorf("commits[%d].Author is empty", i)
		}
		if c.Date == "" {
			t.Errorf("commits[%d].Date is empty", i)
		}
		// SHA should start with ShortSHA
		if !strings.HasPrefix(c.SHA, c.ShortSHA) {
			t.Errorf("commits[%d].SHA %q does not start with ShortSHA %q", i, c.SHA, c.ShortSHA)
		}
	}
}

func TestCommitLogEmpty(t *testing.T) {
	dir := initTestRepo(t)

	// Empty baseRef should return nil
	commits, err := CommitLog("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if commits != nil {
		t.Errorf("expected nil for empty baseRef, got %+v", commits)
	}
}

func TestChangedFilesForCommit(t *testing.T) {
	dir := initTestRepo(t)

	// Create a feature branch with two commits
	runGit(t, dir, "checkout", "-b", "feature/commit-files")
	writeFile(t, filepath.Join(dir, "a.txt"), "hello\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "add a.txt")

	// Second commit: add b.txt and modify a.txt
	writeFile(t, filepath.Join(dir, "b.txt"), "world\n")
	writeFile(t, filepath.Join(dir, "a.txt"), "hello modified\n")
	runGit(t, dir, "add", "a.txt", "b.txt")
	runGit(t, dir, "commit", "-m", "add b.txt and modify a.txt")

	// Get HEAD SHA
	sha := runGit(t, dir, "rev-parse", "HEAD")

	changes, err := ChangedFilesForCommit(sha, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}

	paths := map[string]string{}
	for _, c := range changes {
		paths[c.Path] = c.Status
	}
	if paths["a.txt"] != "modified" {
		t.Errorf("a.txt status = %q, want modified", paths["a.txt"])
	}
	if paths["b.txt"] != "added" {
		t.Errorf("b.txt status = %q, want added", paths["b.txt"])
	}
}

func TestFileDiffForCommit(t *testing.T) {
	dir := initTestRepo(t)

	// Create a feature branch and commit a file
	runGit(t, dir, "checkout", "-b", "feature/commit-diff")
	writeFile(t, filepath.Join(dir, "code.go"), "package main\n\nfunc Hello() {}\n")
	runGit(t, dir, "add", "code.go")
	runGit(t, dir, "commit", "-m", "add code.go")

	// Get HEAD SHA
	sha := runGit(t, dir, "rev-parse", "HEAD")

	hunks, err := FileDiffForCommit("code.go", sha, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}

	// Verify the hunk contains add lines for the new file
	addCount := 0
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Type == "add" {
				addCount++
			}
		}
	}
	if addCount != 3 {
		t.Errorf("expected 3 add lines, got %d", addCount)
	}
}

func TestAllTrackedFiles_RealRepo(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "src/main.go"), "package main")
	writeFile(t, filepath.Join(dir, "src/util.go"), "package main")
	writeFile(t, filepath.Join(dir, "docs/readme.md"), "# docs")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add files")

	files, err := AllTrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"README.md":      true,
		"src/main.go":    true,
		"src/util.go":    true,
		"docs/readme.md": true,
	}
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(files), files)
	}
	for _, f := range files {
		if !expected[f] {
			t.Errorf("unexpected file: %s", f)
		}
	}
}

func TestAllTrackedFiles_IncludesUntracked(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "new.txt"), "hello")

	files, err := AllTrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, f := range files {
		if f == "new.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected untracked file new.txt in results, got: %v", files)
	}
}

func TestAllTrackedFiles_ExcludesGitignored(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.log\nbuild/\n")
	writeFile(t, filepath.Join(dir, "app.log"), "log data")
	writeFile(t, filepath.Join(dir, "build/out.bin"), "binary")
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep")
	runGit(t, dir, "add", ".gitignore", "keep.txt")
	runGit(t, dir, "commit", "-m", "add files")

	files, err := AllTrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if f == "app.log" || strings.HasPrefix(f, "build/") {
			t.Errorf("gitignored file should not appear: %s", f)
		}
	}
}

func TestWalkFiles_BasicDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "file1.txt"), "hello")
	writeFile(t, filepath.Join(dir, "sub/file2.go"), "package sub")
	writeFile(t, filepath.Join(dir, "sub/deep/file3.md"), "# deep")

	files, err := WalkFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"file1.txt":         true,
		"sub/file2.go":      true,
		"sub/deep/file3.md": true,
	}
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(files), files)
	}
	for _, f := range files {
		if !expected[f] {
			t.Errorf("unexpected file: %s", f)
		}
	}
}

func TestWalkFiles_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".git/config"), "git config")
	writeFile(t, filepath.Join(dir, ".hidden/secret"), "shh")
	writeFile(t, filepath.Join(dir, "visible.txt"), "hello")

	files, err := WalkFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if strings.HasPrefix(f, ".git/") || strings.HasPrefix(f, ".hidden/") {
			t.Errorf("hidden dir file should not appear: %s", f)
		}
	}
	if len(files) != 1 || files[0] != "visible.txt" {
		t.Errorf("expected [visible.txt], got %v", files)
	}
}

func TestWalkFiles_DotPrefixedRoot(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, ".dotfiles")
	writeFile(t, filepath.Join(dir, "bashrc"), "alias ls='ls -la'")
	writeFile(t, filepath.Join(dir, "vimrc"), "set number")

	files, err := WalkFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files from dot-prefixed root, got %d: %v", len(files), files)
	}
}

func TestWalkFiles_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), "console.log('hi')")
	writeFile(t, filepath.Join(dir, "node_modules/pkg/index.js"), "module")

	files, err := WalkFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if strings.HasPrefix(f, "node_modules/") {
			t.Errorf("node_modules file should not appear: %s", f)
		}
	}
}

func TestDiffNumstat(t *testing.T) {
	dir := initTestRepo(t)

	writeFile(t, filepath.Join(dir, "stats.go"), "package main\n\nfunc hello() {}\n")
	runGit(t, dir, "add", "stats.go")
	runGit(t, dir, "commit", "-m", "add stats.go")
	runGit(t, dir, "checkout", "-b", "feature-numstat")
	writeFile(t, filepath.Join(dir, "stats.go"), "package main\n\nfunc hello() {\n\tfmt.Println(\"hi\")\n}\n\nfunc world() {}\n")
	writeFile(t, filepath.Join(dir, "newfile.txt"), "line1\nline2\nline3\n")
	runGit(t, dir, "add", "stats.go", "newfile.txt")
	runGit(t, dir, "commit", "-m", "modify stats and add newfile")

	base := strings.TrimSpace(runGit(t, dir, "merge-base", "main", "HEAD"))
	stats, err := DiffNumstatDir(base, dir)
	if err != nil {
		t.Fatalf("DiffNumstat failed: %v", err)
	}

	if s, ok := stats["stats.go"]; !ok {
		t.Fatal("missing stats.go in numstat output")
	} else {
		if s.Additions != 5 || s.Deletions != 1 {
			t.Errorf("stats.go: got +%d/-%d, want +5/-1", s.Additions, s.Deletions)
		}
	}

	if s, ok := stats["newfile.txt"]; !ok {
		t.Fatal("missing newfile.txt in numstat output")
	} else {
		if s.Additions != 3 || s.Deletions != 0 {
			t.Errorf("newfile.txt: got +%d/-%d, want +3/-0", s.Additions, s.Deletions)
		}
	}
}

func TestDiffNumstatBinary(t *testing.T) {
	dir := initTestRepo(t)

	writeFile(t, filepath.Join(dir, "notes.txt"), "hello\n")
	runGit(t, dir, "add", "notes.txt")
	runGit(t, dir, "commit", "-m", "add notes")
	runGit(t, dir, "checkout", "-b", "feature-binary")

	binPath := filepath.Join(dir, "image.png")
	os.WriteFile(binPath, []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00}, 0644)
	runGit(t, dir, "add", "image.png")
	runGit(t, dir, "commit", "-m", "add binary")

	base := strings.TrimSpace(runGit(t, dir, "merge-base", "main", "HEAD"))
	stats, err := DiffNumstatDir(base, dir)
	if err != nil {
		t.Fatalf("DiffNumstat failed: %v", err)
	}

	if s, ok := stats["image.png"]; !ok {
		t.Fatal("missing image.png in numstat output")
	} else {
		if s.Additions != 0 || s.Deletions != 0 {
			t.Errorf("image.png: got +%d/-%d, want +0/-0", s.Additions, s.Deletions)
		}
	}
}
