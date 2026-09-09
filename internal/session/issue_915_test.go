package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// Regression test for #915: after a renamed file is edited between review
// rounds, staged and unstaged views must not fall back to the combined "all"
// diff and mix the two rounds together.
func TestGetFileDiffSnapshotScoped_RenamedFileAfterRoundCompleteKeepsScopesSeparate(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	oldPath := filepath.Join(dir, "old.go")
	baseContent := `package sample

func moved() {
	println("base version")
}

func stableOne() {}
func stableTwo() {}
func stableThree() {}
func stableFour() {}
func stableFive() {}
func stableSix() {}
func stableSeven() {}
func stableEight() {}
`
	writeFile(t, oldPath, baseContent)
	gitT(t, dir, "add", "old.go")
	gitT(t, dir, "commit", "-m", "add source file")
	gitT(t, dir, "checkout", "-b", "feature")

	// Round 1 renames the file and stages an edit at the original location.
	gitT(t, dir, "mv", "old.go", "new.go")
	newPath := filepath.Join(dir, "new.go")
	roundOneContent := strings.Replace(baseContent, "base version", "round one", 1)
	writeFile(t, newPath, roundOneContent)
	gitT(t, dir, "add", "-A")

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

	sess, err := NewGitSession(&vcs.GitVCS{}, nil)
	if err != nil {
		t.Fatalf("NewGitSession: %v", err)
	}
	if len(sess.Files) != 1 || sess.Files[0].Status != "renamed" {
		t.Fatalf("round 1 must begin with one renamed file, got %+v", sess.Files)
	}

	// Round 2 moves the edited block below the stable code and changes it again,
	// but leaves that second edit unstaged. The index is still the Round 1 state.
	roundTwoContent := `package sample

func stableOne() {}
func stableTwo() {}
func stableThree() {}
func stableFour() {}
func stableFive() {}
func stableSix() {}
func stableSeven() {}
func stableEight() {}

func moved() {
	println("round two")
}
`
	writeFile(t, newPath, roundTwoContent)
	sess.handleRoundCompleteGit()

	if len(sess.Files) != 1 || sess.Files[0].Status != "renamed" {
		t.Fatalf("round 2 must retain the renamed file, got %+v", sess.Files)
	}

	diffText := func(scope string) string {
		t.Helper()
		snapshot, ok := sess.GetFileDiffSnapshotScoped("new.go", scope, "", false)
		if !ok {
			t.Fatalf("GetFileDiffSnapshotScoped(%q) returned ok=false", scope)
		}
		hunks, ok := snapshot["hunks"].([]vcs.DiffHunk)
		if !ok {
			t.Fatalf("GetFileDiffSnapshotScoped(%q) hunks have type %T", scope, snapshot["hunks"])
		}
		var lines []string
		for _, hunk := range hunks {
			for _, line := range hunk.Lines {
				lines = append(lines, line.Content)
			}
		}
		return strings.Join(lines, "\n")
	}

	staged := diffText("staged")
	if strings.Contains(staged, "round two") {
		t.Errorf("staged diff leaked the unstaged Round 2 version:\n%s", staged)
	}
	if !strings.Contains(staged, "round one") {
		t.Errorf("staged diff omitted the staged Round 1 version:\n%s", staged)
	}

	unstaged := diffText("unstaged")
	if strings.Contains(unstaged, "base version") {
		t.Errorf("unstaged diff leaked the pre-Round 1 base version:\n%s", unstaged)
	}
	if !strings.Contains(unstaged, "round one") || !strings.Contains(unstaged, "round two") {
		t.Errorf("unstaged diff must compare Round 1 index to Round 2 worktree:\n%s", unstaged)
	}

	all := diffText("all")
	if !strings.Contains(all, "round two") {
		t.Errorf("all diff omitted the Round 2 worktree version:\n%s", all)
	}
	if strings.Contains(all, "round one") {
		t.Errorf("all diff unexpectedly exposed the intermediate Round 1 version:\n%s", all)
	}
}
