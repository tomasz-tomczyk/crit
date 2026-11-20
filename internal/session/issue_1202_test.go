package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestIssue1202_CommitPinShowsAddedFileAtThatCommit(t *testing.T) {
	dir := initTestRepo(t)
	gitT(t, dir, "checkout", "-b", "feature")
	add := commitAt(t, dir, "bar", "bar\n", "add bar")
	commitAt(t, dir, "bar", "new bar\n", "update bar")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		BaseRef:     "main",
		VCS:         &vcs.GitVCS{},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
		Files: []*FileEntry{{
			Path:     "bar",
			AbsPath:  filepath.Join(dir, "bar"),
			Status:   "added",
			FileType: "code",
			Content:  "new bar\n",
		}},
	}

	result, ok := s.GetFileDiffSnapshotScoped("bar", "branch", add, false)
	if !ok {
		t.Fatal("expected snapshot")
	}
	hunks, _ := result["hunks"].([]DiffHunk)
	var added []string
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Type == "add" {
				added = append(added, l.Content)
			}
		}
	}
	if got := strings.Join(added, "\n"); got != "bar" {
		t.Fatalf("added lines = %q, want %q", got, "bar")
	}
}
