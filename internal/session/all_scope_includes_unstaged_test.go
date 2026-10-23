package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func renderHunksForTest(snap map[string]any) string {
	var b strings.Builder
	hunks, _ := snap["hunks"].([]vcs.DiffHunk)
	for _, h := range hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case "add":
				b.WriteString("+" + l.Content + "\n")
			case "del":
				b.WriteString("-" + l.Content + "\n")
			default:
				b.WriteString(" " + l.Content + "\n")
			}
		}
	}
	return b.String()
}

const allScopeSample = "package sample\n\nfunc f() {\n\tprintln(\"base\")\n}\n\nfunc stable() {}\n"

func newAllScopeRepo(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t)
	setHome(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "app.go"), allScopeSample)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "add app.go")
	gitT(t, dir, "checkout", "-b", "feature")
	return dir
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func startAllScopeSession(t *testing.T) *Session {
	t.Helper()
	sess, err := NewGitSession(&vcs.GitVCS{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	quiesceSession(t, sess)
	// Stop writes the test schedules (AddComment, SyncWorkingTree) before
	// t.TempDir is removed; Windows fails the cleanup if a write lands.
	t.Cleanup(func() { quiesceSession(t, sess) })
	return sess
}

func TestGetFileDiffSnapshotScoped_AllIncludesUnstagedOnTopOfBranch(t *testing.T) {
	dir := newAllScopeRepo(t)
	branchVersion := strings.Replace(allScopeSample, "\"base\"", "\"branch version\"", 1)
	writeFile(t, filepath.Join(dir, "app.go"), branchVersion)
	gitT(t, dir, "commit", "-am", "branch change")
	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(branchVersion, "\"branch version\"", "\"UNSTAGED version\"", 1))
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)

	allSnap, ok := sess.GetFileDiffSnapshotScoped("app.go", "all", "", false)
	if !ok {
		t.Fatal("scope=all: expected snapshot")
	}
	allText := renderHunksForTest(allSnap)
	if !strings.Contains(allText, "UNSTAGED version") {
		t.Errorf("scope=all must show the working-tree state, got:\n%s", allText)
	}
	if strings.Contains(allText, "branch version") {
		t.Errorf("scope=all must diff against base, not against the branch head, got:\n%s", allText)
	}

	branchSnap, _ := sess.GetFileDiffSnapshotScoped("app.go", "branch", "", false)
	branchText := renderHunksForTest(branchSnap)
	if !strings.Contains(branchText, "branch version") || strings.Contains(branchText, "UNSTAGED version") {
		t.Errorf("scope=branch must show only committed work, got:\n%s", branchText)
	}

	unstagedSnap, _ := sess.GetFileDiffSnapshotScoped("app.go", "unstaged", "", false)
	unstagedText := renderHunksForTest(unstagedSnap)
	if !strings.Contains(unstagedText, "UNSTAGED version") || !strings.Contains(unstagedText, "branch version") {
		t.Errorf("scope=unstaged must show index vs worktree, got:\n%s", unstagedText)
	}
}

func TestGetFileDiffSnapshotScoped_AllIncludesUnstagedLandedDuringReview(t *testing.T) {
	dir := newAllScopeRepo(t)
	branchVersion := strings.Replace(allScopeSample, "\"base\"", "\"branch version\"", 1)
	writeFile(t, filepath.Join(dir, "app.go"), branchVersion)
	gitT(t, dir, "commit", "-am", "branch change")
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)

	// No refresh: "all" must read the working tree, not the round snapshot.
	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(branchVersion, "\"branch version\"", "\"LIVE UNSTAGED\"", 1))

	allSnap, ok := sess.GetFileDiffSnapshotScoped("app.go", "all", "", false)
	if !ok {
		t.Fatal("scope=all: expected snapshot")
	}
	allText := renderHunksForTest(allSnap)
	if !strings.Contains(allText, "LIVE UNSTAGED") {
		t.Errorf("scope=all must pick up unstaged edits made during the review, got:\n%s", allText)
	}
}

func TestGetFileDiffSnapshotScoped_AllDropsRevertedBranchWork(t *testing.T) {
	dir := newAllScopeRepo(t)
	withImport := strings.Replace(allScopeSample, "package sample", "package sample\n\nimport \"NEWIMPORT\"", 1)
	writeFile(t, filepath.Join(dir, "app.go"), withImport)
	gitT(t, dir, "commit", "-am", "branch adds import")
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)

	writeFile(t, filepath.Join(dir, "app.go"), allScopeSample)
	gitT(t, dir, "add", "app.go")

	allSnap, ok := sess.GetFileDiffSnapshotScoped("app.go", "all", "", false)
	if !ok {
		t.Fatal("scope=all: expected snapshot")
	}
	if text := renderHunksForTest(allSnap); strings.Contains(text, "NEWIMPORT") {
		t.Errorf("scope=all must not show reverted branch work, got:\n%s", text)
	}

	branchSnap, _ := sess.GetFileDiffSnapshotScoped("app.go", "branch", "", false)
	if text := renderHunksForTest(branchSnap); !strings.Contains(text, "NEWIMPORT") {
		t.Errorf("scope=branch must still show the committed import, got:\n%s", text)
	}

	stagedSnap, _ := sess.GetFileDiffSnapshotScoped("app.go", "staged", "", false)
	if text := renderHunksForTest(stagedSnap); !strings.Contains(text, "NEWIMPORT") {
		t.Errorf("scope=staged must show the import being removed, got:\n%s", text)
	}
}
