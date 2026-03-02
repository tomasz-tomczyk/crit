package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temp directory with a git repo and returns the path.
// The repo has an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	// Create initial commit
	writeFile(t, filepath.Join(dir, "README.md"), "# Test")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	// Ensure default branch is "main"
	runGit(t, dir, "branch", "-M", "main")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

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
	if ctx != 3 {
		t.Errorf("expected 3 context lines, got %d", ctx)
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
	if paths["new.go"] != "added" {
		t.Errorf("new.go status = %q, want added", paths["new.go"])
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
