package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Compile-time interface compliance check.
var _ VCS = &GitVCS{}

func TestGitVCS_Name(t *testing.T) {
	g := &GitVCS{}
	if got := g.Name(); got != "git" {
		t.Errorf("Name() = %q, want %q", got, "git")
	}
}

func TestGitVCS_HasStagingArea(t *testing.T) {
	g := &GitVCS{}
	if !g.HasStagingArea() {
		t.Error("HasStagingArea() = false, want true")
	}
}

func TestGitVCS_SkipDirNames(t *testing.T) {
	g := &GitVCS{}
	dirs := g.SkipDirNames()
	if len(dirs) != 1 || dirs[0] != ".git" {
		t.Errorf("SkipDirNames() = %v, want [.git]", dirs)
	}
}

func TestDetectVCS_GitOverride(t *testing.T) {
	vcs := DetectVCS("git")
	if vcs == nil || vcs.Name() != "git" {
		t.Errorf("DetectVCS(\"git\") should return GitVCS, got %v", vcs)
	}
}

// commitAt writes a file and creates a single commit at dir, returning the new HEAD SHA.
func commitAt(t *testing.T, dir, path, content, msg string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, path), content)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", msg)
	return gitT(t, dir, "rev-parse", "HEAD")
}

func sortChanges(in []FileChange) []FileChange {
	out := append([]FileChange(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func TestChangedFilesBetweenSHAs_AddOneFile(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	head := commitAt(t, dir, "a.txt", "hi", "add a")

	got, err := ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "a.txt", Status: "added"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChangedFilesBetweenSHAs_AddAndModify(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "b.txt", "v1", "add b")
	base := gitT(t, dir, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "a.txt"), "hi")
	writeFile(t, filepath.Join(dir, "b.txt"), "v2")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "add a, modify b")
	head := gitT(t, dir, "rev-parse", "HEAD")

	got, err := ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{
		{Path: "a.txt", Status: "added"},
		{Path: "b.txt", Status: "modified"},
	}
	if !reflect.DeepEqual(sortChanges(got), sortChanges(want)) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChangedFilesBetweenSHAs_Rename(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "old.go", "package x\n", "add old")
	base := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "mv", "old.go", "new.go")
	gitT(t, dir, "commit", "-m", "rename")
	head := gitT(t, dir, "rev-parse", "HEAD")

	got, err := ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "new.go" || got[0].Status != "renamed" {
		t.Fatalf("got %+v, want [{new.go renamed}]", got)
	}
}

func TestChangedFilesBetweenSHAs_Deletion(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "c.txt", "x", "add c")
	base := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "rm", "c.txt")
	gitT(t, dir, "commit", "-m", "delete c")
	head := gitT(t, dir, "rev-parse", "HEAD")

	got, err := ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "c.txt", Status: "deleted"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChangedFilesBetweenSHAs_UntrackedNotIncluded(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	head := commitAt(t, dir, "a.txt", "hi", "add a")
	// Add an untracked file in the working tree — should NOT appear in range.
	writeFile(t, filepath.Join(dir, "untracked.txt"), "ignored")

	got, err := ChangedFilesBetweenSHAs(base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Path == "untracked.txt" {
			t.Fatalf("untracked.txt should not appear in range result: %+v", got)
		}
	}
}

func TestFileDiffBetweenSHAs_HappyPath(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "a.txt", "line1\nline2\n", "add a")
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "a.txt", "line1\nline2\nline3\n", "modify a")
	head := gitT(t, dir, "rev-parse", "HEAD")

	hunks, err := FileDiffBetweenSHAs("a.txt", base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
}

func TestFileDiffBetweenSHAs_IdenticalSHAs(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "a.txt", "line1\n", "add a")
	sha := gitT(t, dir, "rev-parse", "HEAD")
	hunks, err := FileDiffBetweenSHAs("a.txt", sha, sha, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 0 {
		t.Errorf("expected 0 hunks for identical SHAs, got %d", len(hunks))
	}
}

func TestFileDiffBetweenSHAs_MissingPath(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	head := commitAt(t, dir, "a.txt", "x", "add a")
	hunks, err := FileDiffBetweenSHAs("does-not-exist.txt", base, head, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 0 {
		t.Errorf("expected 0 hunks for missing path, got %d", len(hunks))
	}
}

func TestReadFileAtSHA_HappyPath(t *testing.T) {
	dir := initTestRepo(t)
	want := "hello world\n"
	commitAt(t, dir, "a.txt", want, "add a")
	sha := gitT(t, dir, "rev-parse", "HEAD")

	got, err := ReadFileAtSHA(sha, "a.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadFileAtSHA_MissingPath(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "a.txt", "x", "add a")
	sha := gitT(t, dir, "rev-parse", "HEAD")

	got, err := ReadFileAtSHA(sha, "no-such-file.txt", dir)
	if err != nil {
		t.Fatalf("expected nil error for missing path, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil bytes, got %q", got)
	}
}

func TestReadFileAtSHA_InvalidRef(t *testing.T) {
	dir := initTestRepo(t)
	// "xyz" is not a valid object name (vs a 40-hex string git silently
	// resolves to "missing commit but valid syntax").
	_, err := ReadFileAtSHA("xyz", "README.md", dir)
	if err == nil {
		t.Fatal("expected error for invalid ref, got nil")
	}
}

func TestHasObject_Existing(t *testing.T) {
	dir := initTestRepo(t)
	sha := gitT(t, dir, "rev-parse", "HEAD")
	if !HasObject(sha, dir) {
		t.Errorf("HasObject(%q) = false, want true", sha)
	}
}

func TestHasObject_Bogus(t *testing.T) {
	dir := initTestRepo(t)
	if HasObject("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", dir) {
		t.Error("HasObject(bogus) = true, want false")
	}
}

func TestHasObject_NonCommit(t *testing.T) {
	dir := initTestRepo(t)
	// Tree SHA of HEAD — not a commit.
	treeSHA := gitT(t, dir, "rev-parse", "HEAD^{tree}")
	if HasObject(treeSHA, dir) {
		t.Errorf("HasObject(tree %s) = true, want false (only commits)", treeSHA)
	}
}
