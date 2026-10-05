package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// Markdown files in git mode must carry the base-side content alongside the
// hunks so the frontend can offer a rendered Before/After view (the same
// split/unified renderers as files-mode round diffs). Added files have no
// base side and must report "".
func TestGetFileDiffSnapshot_MarkdownGitModeIncludesBaseContent(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	base := "# Title\n\nHello \u2014 world\n"
	writeFile(t, filepath.Join(dir, "doc.md"), base)
	gitT(t, dir, "add", "doc.md")
	gitT(t, dir, "commit", "-m", "add doc")
	gitT(t, dir, "checkout", "-b", "feature")

	head := "# Title\n\nHello - world\n"
	writeFile(t, filepath.Join(dir, "doc.md"), head)
	writeFile(t, filepath.Join(dir, "new.md"), "# New\n")

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

	snapshot, ok := sess.GetFileDiffSnapshot("doc.md", false)
	if !ok {
		t.Fatal("doc.md diff missing")
	}
	if got := snapshot["previous_content"]; got != base {
		t.Fatalf("previous_content = %q; want base %q", got, base)
	}
	if hunks, ok := snapshot["hunks"]; !ok || len(hunks.([]vcs.DiffHunk)) == 0 {
		t.Fatal("doc.md must have hunks")
	}

	added, ok := sess.GetFileDiffSnapshot("new.md", false)
	if !ok {
		t.Fatal("new.md diff missing")
	}
	if got := added["previous_content"]; got != "" {
		t.Fatalf("added file previous_content = %q; want empty", got)
	}
}
