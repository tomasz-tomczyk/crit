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

// Regression #1202: a file that is "added" on the branch and was added in the
// pinned commit must show that commit's diff and content, not the working
// tree's (HEAD) version of the file.
func TestGetFileDiffSnapshotScoped_CommitPin_AddedFileUsesCommitContent(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "bar", "bar\n", "add bar")
	addCommit := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "bar", "new bar\n", "update bar")
	updateCommit := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  base,
		VCS:      &vcs.GitVCS{},
		Files: []*FileEntry{{
			Path:      "bar",
			Status:    "added",
			Content:   "new bar\n",
			DiffHunks: vcs.FileDiffUnifiedNewFile("new bar\n"),
		}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	tests := []struct {
		name         string
		commit       string
		wantLines    []DiffLine
		wantContent  string
		wantPrevious string
	}{
		{
			name:        "commit that adds the file",
			commit:      addCommit,
			wantLines:   []DiffLine{{Type: "add", Content: "bar"}},
			wantContent: "bar\n",
		},
		{
			name:         "commit that modifies the file",
			commit:       updateCommit,
			wantLines:    []DiffLine{{Type: "del", Content: "bar"}, {Type: "add", Content: "new bar"}},
			wantContent:  "new bar\n",
			wantPrevious: "bar\n",
		},
		{
			name:        "commit range ending at the adding commit",
			commit:      base + ".." + addCommit,
			wantLines:   []DiffLine{{Type: "add", Content: "bar"}},
			wantContent: "bar\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, scope := range []string{"", "branch", "all"} {
				result, ok := s.GetFileDiffSnapshotScoped("bar", scope, tt.commit, false)
				if !ok {
					t.Fatalf("scope %q: expected snapshot", scope)
				}
				hunks, _ := result["hunks"].([]DiffHunk)
				var got []DiffLine
				for _, h := range hunks {
					for _, l := range h.Lines {
						got = append(got, DiffLine{Type: l.Type, Content: l.Content})
					}
				}
				if len(got) != len(tt.wantLines) {
					t.Fatalf("scope %q: lines = %+v, want %+v", scope, got, tt.wantLines)
				}
				for i := range got {
					if got[i] != tt.wantLines[i] {
						t.Fatalf("scope %q: lines = %+v, want %+v", scope, got, tt.wantLines)
					}
				}
				if c, _ := result["content"].(string); c != tt.wantContent {
					t.Errorf("scope %q: content = %q, want %q", scope, c, tt.wantContent)
				}
				if p, _ := result["previous_content"].(string); p != tt.wantPrevious {
					t.Errorf("scope %q: previous_content = %q, want %q", scope, p, tt.wantPrevious)
				}
			}
		})
	}
}

// Regression #1202: on a large review the session holds some files as lazy
// stubs with branch-wide counts. A commit pin must count that commit's
// change, not reuse the stub.
func TestGetSessionInfoScoped_CommitPin_LazyFileCountsCommit(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "bar", "bar\n", "add bar")
	commitAt(t, dir, "bar", "new bar\nmore\n", "update bar")
	updateCommit := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  base,
		VCS:      &vcs.GitVCS{},
		Files: []*FileEntry{{
			Path:          "bar",
			Status:        "added",
			Lazy:          true,
			LazyAdditions: 2,
			ListedHash:    "working-tree-hash",
			Comments:      []Comment{},
		}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	info := s.GetSessionInfoScoped("", updateCommit)
	if len(info.Files) != 1 {
		t.Fatalf("files = %+v, want just bar", info.Files)
	}
	got := info.Files[0]
	if got.Additions != 2 || got.Deletions != 1 {
		t.Errorf("counts = +%d -%d, want +2 -1 (the commit's change)", got.Additions, got.Deletions)
	}
	if !got.Lazy {
		t.Error("a session-lazy file should stay lazy under a commit pin")
	}
	if got.FileHash != "" {
		t.Errorf("file hash = %q, want none (the stub's hash is the working tree's)", got.FileHash)
	}
}
