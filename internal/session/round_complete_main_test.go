package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestRoundCompleteOnMainAfterCommit_KeepsFiles(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "foo.go"), "package main\n")
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(oldWd) })

	sess, err := NewGitSession(&vcs.GitVCS{}, nil)
	if err != nil {
		t.Fatalf("NewGitSession: %v", err)
	}
	if len(sess.Files) == 0 {
		t.Fatal("expected files from unstaged change on main")
	}
	if sess.BaseRef == "" {
		t.Fatal("expected BaseRef pinned to session-start HEAD on main")
	}

	gitT(t, dir, "add", "foo.go")
	gitT(t, dir, "commit", "-m", "agent commit")

	sess.handleRoundCompleteGit()

	if len(sess.Files) == 0 {
		t.Fatal("after round-complete on committed main, expected files to remain visible")
	}

	info := sess.GetSessionInfoScoped("all", "")
	if len(info.Files) == 0 {
		t.Fatalf("GetSessionInfoScoped(all) returned no files after commit on main")
	}
}
