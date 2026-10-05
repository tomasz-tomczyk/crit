package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

var errBaseRead = errors.New("base read failed")

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

// Range focus (PR/range reviews) must also report base content, including
// across renames (base read via OldPath) and for deleted files.
func TestGetFileDiffSnapshot_MarkdownRangeModeIncludesBaseContent(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())

	docBase := "# Doc\n\nBase version\n"
	oldBase := "# Old\n\nLine 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8\n"
	delBase := "# Gone\n\nDeleted\n"
	writeFile(t, filepath.Join(dir, "doc.md"), docBase)
	writeFile(t, filepath.Join(dir, "old.md"), oldBase)
	writeFile(t, filepath.Join(dir, "del.md"), delBase)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "base docs")
	base := gitT(t, dir, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(dir, "doc.md"), "# Doc\n\nHead version\n")
	gitT(t, dir, "mv", "old.md", "renamed.md")
	writeFile(t, filepath.Join(dir, "renamed.md"), "# Old\n\nLine 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nChanged\n")
	gitT(t, dir, "rm", "-q", "del.md")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "head docs")
	head := gitT(t, dir, "rev-parse", "HEAD")

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

	sess, err := NewGitSessionLenient(&vcs.GitVCS{}, nil)
	if err != nil {
		t.Fatalf("NewGitSessionLenient: %v", err)
	}
	if err := sess.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}

	for _, tc := range []struct{ path, want string }{
		{"doc.md", docBase},
		{"renamed.md", oldBase},
		{"del.md", delBase},
	} {
		snapshot, ok := sess.GetFileDiffSnapshot(tc.path, false)
		if !ok {
			t.Fatalf("%s diff missing", tc.path)
		}
		if got := snapshot["previous_content"]; got != tc.want {
			t.Fatalf("%s previous_content = %q; want %q", tc.path, got, tc.want)
		}
		if hunks, ok := snapshot["hunks"]; !ok || len(hunks.([]vcs.DiffHunk)) == 0 {
			t.Fatalf("%s must have hunks", tc.path)
		}
	}
}

// An unreadable base (e.g. unknown ref) must degrade to empty previous
// content rather than failing the diff response.
func TestGetFileDiffSnapshot_MarkdownGitBaseReadErrorIsEmpty(t *testing.T) {
	sess := &Session{
		Mode:     "git",
		RepoRoot: t.TempDir(),
		BaseRef:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		VCS:      &vcs.GitVCS{},
		Files: []*FileEntry{
			{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
		},
	}
	snapshot, ok := sess.GetFileDiffSnapshot("doc.md", false)
	if !ok {
		t.Fatal("doc.md diff missing")
	}
	if got := snapshot["previous_content"]; got != "" {
		t.Fatalf("previous_content = %q; want empty on base read error", got)
	}
}

// Guard branches: no VCS/base ref, and range focus without a base SHA must
// both report empty previous content without touching git.
func TestGetFileDiffSnapshot_MarkdownGitMissingBaseIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		sess *Session
	}{
		{"no VCS", &Session{Mode: "git", RepoRoot: t.TempDir(), BaseRef: "main", Files: []*FileEntry{
			{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
		}}},
		{"no base ref", &Session{Mode: "git", RepoRoot: t.TempDir(), VCS: &vcs.GitVCS{}, Files: []*FileEntry{
			{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
		}}},
		{"range without base SHA", &Session{Mode: "git", RepoRoot: t.TempDir(), VCS: &vcs.GitVCS{},
			Focus: Focus{Kind: FocusRange, DiffScope: DiffScopeLayer}, Files: []*FileEntry{
				{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
			}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, ok := tc.sess.GetFileDiffSnapshot("doc.md", false)
			if !ok {
				t.Fatal("doc.md diff missing")
			}
			if got := snapshot["previous_content"]; got != "" {
				t.Fatalf("previous_content = %q; want empty", got)
			}
		})
	}

	// Range focus where the path does not exist at the base SHA must also
	// report empty previous content rather than failing.
	dir := initTestRepo(t)
	sha := gitT(t, dir, "rev-parse", "HEAD")
	rangeSess := &Session{Mode: "git", RepoRoot: dir, VCS: &vcs.GitVCS{},
		Focus: Focus{Kind: FocusRange, BaseSHA: sha, HeadSHA: sha, DiffScope: DiffScopeLayer}, Files: []*FileEntry{
			{Path: "missing.md", Status: "modified", FileType: "markdown", Content: "# Missing\n"},
		}}
	snapshot, ok := rangeSess.GetFileDiffSnapshot("missing.md", false)
	if !ok {
		t.Fatal("missing.md diff missing")
	}
	if got := snapshot["previous_content"]; got != "" {
		t.Fatalf("previous_content = %q; want empty", got)
	}
}

// errBaseVCS fails every base-content read, exercising the defensive empty
// fallbacks in gitBaseContentForMarkdown (production git reads degrade to ""
// instead of erroring, so only the remote fetch and a failing VCS hit them).
type errBaseVCS struct {
	vcs.VCS
}

func (errBaseVCS) FileContentAtRef(_, _, _ string) (string, error) {
	return "", errBaseRead
}

func (errBaseVCS) FileDiffUnifiedCtx(_ context.Context, _, _, _ string, _ bool) ([]vcs.DiffHunk, error) {
	return nil, nil
}

// Remote fetch errors (e.g. gh api failure) and VCS base-read errors must
// degrade to empty previous content rather than failing the diff response.
func TestGetFileDiffSnapshot_MarkdownGitRemoteErrorsAreEmpty(t *testing.T) {
	previousFetch := FetchMRFileContent
	t.Cleanup(func() { FetchMRFileContent = previousFetch })
	FetchMRFileContent = func(Focus, string, string) ([]byte, error) {
		return nil, errBaseRead
	}

	remoteSess := &Session{Mode: "git", RepoRoot: t.TempDir(), VCS: &vcs.GitVCS{}, RemoteFiles: true,
		Focus: Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 17,
			MRURL:   "https://gitlab.example/a/b/-/merge_requests/17",
			BaseSHA: "base", HeadSHA: "head", DiffScope: DiffScopeLayer},
		Files: []*FileEntry{
			{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
		}}
	snapshot, ok := remoteSess.GetFileDiffSnapshot("doc.md", false)
	if !ok {
		t.Fatal("doc.md diff missing")
	}
	if got := snapshot["previous_content"]; got != "" {
		t.Fatalf("remote error previous_content = %q; want empty", got)
	}

	localSess := &Session{Mode: "git", RepoRoot: t.TempDir(), VCS: errBaseVCS{}, BaseRef: "main",
		Files: []*FileEntry{
			{Path: "doc.md", Status: "modified", FileType: "markdown", Content: "# Doc\n"},
		}}
	snapshot, ok = localSess.GetFileDiffSnapshot("doc.md", false)
	if !ok {
		t.Fatal("doc.md diff missing")
	}
	if got := snapshot["previous_content"]; got != "" {
		t.Fatalf("VCS error previous_content = %q; want empty", got)
	}
}
