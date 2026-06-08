package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Compile-time interface compliance check.
var _ VCS = &JJVCS{}

func TestJJVCS_Name(t *testing.T) {
	j := &JJVCS{}
	if got := j.Name(); got != "jj" {
		t.Errorf("Name() = %q, want %q", got, "jj")
	}
}

func TestJJVCS_HasStagingArea(t *testing.T) {
	j := &JJVCS{}
	if j.HasStagingArea() {
		t.Error("HasStagingArea() = true, want false")
	}
}

func TestJJVCS_SkipDirNames(t *testing.T) {
	j := &JJVCS{}
	dirs := j.SkipDirNames()
	want := map[string]bool{".jj": true, ".git": true}
	if len(dirs) != len(want) {
		t.Fatalf("SkipDirNames() = %v, want keys of %v", dirs, want)
	}
	for _, d := range dirs {
		if !want[d] {
			t.Errorf("unexpected dir name %q in SkipDirNames()", d)
		}
	}
}

func TestJJVCS_DefaultBranchOverride(t *testing.T) {
	j := &JJVCS{}
	j.SetDefaultBranchOverride("develop")
	if got := j.GetDefaultBranchOverride(); got != "develop" {
		t.Errorf("GetDefaultBranchOverride() = %q, want develop", got)
	}
	if got := j.DefaultBranch(); got != "develop" {
		t.Errorf("DefaultBranch() = %q after override, want develop", got)
	}
}

func TestHasJJDirFrom_DetectsDotJJ(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested", "repo")
	if err := os.MkdirAll(filepath.Join(root, ".jj"), 0o755); err != nil {
		t.Fatalf("mkdir .jj: %v", err)
	}
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if !hasJJDirFrom(child) {
		t.Fatal("expected hasJJDirFrom to detect .jj metadata")
	}
}

func TestDetectVCS_JJOverride(t *testing.T) {
	v := DetectVCS("jj")
	if _, hasJJ := exec.LookPath("jj"); hasJJ == nil {
		if _, ok := v.(*JJVCS); !ok {
			t.Errorf("DetectVCS(jj) returned %T, want *JJVCS", v)
		}
		return
	}
	// Without jj in PATH, DetectVCS may fall back to git when the test itself
	// runs inside a git checkout. That fallback is intentional and covered by
	// the production warning path, so there is nothing stable to assert here.
}

func TestJJVCS_DefaultBaseRefUsesTrunk(t *testing.T) {
	work := initTestJJCloneWithOriginMain(t)
	withCwd(t, work)

	j := &JJVCS{}
	trunk := runJJ(t, work, "log", "-r", "trunk()", "--no-graph", "-T", "commit_id")
	if isJJRootCommitID(trunk) {
		t.Fatal("test setup produced root trunk")
	}
	if got := j.DefaultBranch(); got != "main" {
		t.Errorf("DefaultBranch() = %q, want main", got)
	}
	if got := j.DefaultBaseRef(); got != trunk {
		t.Errorf("DefaultBaseRef() = %q, want trunk %q", got, trunk)
	}
}

func TestJJVCS_DefaultBaseRefFallsBackToLocalMainWhenTrunkIsRoot(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	withCwd(t, dir)

	j := &JJVCS{}
	mainSHA := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if got := j.DefaultBranch(); got != "main" {
		t.Errorf("DefaultBranch() = %q, want main", got)
	}
	if got := j.DefaultBaseRef(); got != mainSHA {
		t.Errorf("DefaultBaseRef() = %q, want local main %q", got, mainSHA)
	}
}

func TestJJVCS_ChangedFilesAndDiffFromBase(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nchange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, err := j.ChangedFilesFromBaseInDir(base, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertFileChangesEqual(t, changes, []FileChange{
		{Path: "app.txt", Status: "modified"},
		{Path: "new.txt", Status: "added"},
	})

	hunks, err := j.FileDiffUnified("app.txt", base, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) == 0 {
		t.Fatal("expected diff hunks for app.txt")
	}
}

func TestJJVCS_ReadFileAtSHA(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	sha := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	got, err := j.ReadFileAtSHA(sha, "app.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "base\n" {
		t.Errorf("ReadFileAtSHA = %q, want base", got)
	}
	got, err = j.ReadFileAtSHA(sha, "missing.txt", dir)
	if err != nil || got != nil {
		t.Errorf("missing path: got (%q, %v), want (nil, nil)", got, err)
	}
}

func initTestJJRepoWithLocalMain(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	dir := t.TempDir()
	runJJ(t, dir, "git", "init", "--colocate", ".")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "app.txt")
	runJJWithUser(t, dir, "commit", "-m", "base")
	runJJ(t, dir, "bookmark", "set", "main", "-r", "@-")
	return dir
}

func initTestJJCloneWithOriginMain(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	remote := filepath.Join(base, "remote.git")
	work := filepath.Join(base, "work")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, seed, "init", "-q", "-b", "main")
	runGitTest(t, seed, "config", "user.name", "Test")
	runGitTest(t, seed, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(seed, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, seed, "add", "app.txt")
	runGitTest(t, seed, "commit", "-q", "-m", "base")
	runGitTest(t, base, "init", "-q", "--bare", remote)
	runGitTest(t, seed, "remote", "add", "origin", remote)
	runGitTest(t, seed, "push", "-q", "origin", "main")
	runJJ(t, base, "git", "clone", "--colocate", remote, work)
	return work
}

func runJJ(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("jj", append([]string{"--no-pager", "--color", "never"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jj %v failed: %v\n%s", args, err, out)
	}
	return stringTrimSpace(string(out))
}

func runJJWithUser(t *testing.T, dir string, args ...string) string {
	t.Helper()
	fullArgs := []string{"--no-pager", "--color", "never", "--config", "user.name=Test", "--config", "user.email=test@example.com"}
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command("jj", fullArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jj %v failed: %v\n%s", args, err, out)
	}
	return stringTrimSpace(string(out))
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return stringTrimSpace(string(out))
}

func withCwd(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func stringTrimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}
