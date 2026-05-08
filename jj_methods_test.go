package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests drive the real jj binary via the contributor's helpers
// (initTestJJRepoWithLocalMain, initTestJJCloneWithOriginMain, runJJ). They
// skip cleanly when jj/git are unavailable.

func TestJJVCS_RepoRoot(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	withCwd(t, dir)

	j := &JJVCS{}
	root, err := j.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	// On macOS /private/var vs /var, etc. — compare resolved paths.
	gotResolved, _ := filepath.EvalSymlinks(root)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	if gotResolved != wantResolved {
		t.Errorf("RepoRoot() = %q (resolved %q), want %q", root, gotResolved, wantResolved)
	}
}

func TestJJVCS_CurrentBranch_BookmarkAndChangeIDFallback(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	withCwd(t, dir)

	j := &JJVCS{}
	// Working copy starts on a fresh empty change with no bookmarks at @ —
	// expect the change-id fallback (non-empty short id).
	got := j.CurrentBranch()
	if got == "" {
		t.Fatal("CurrentBranch() = empty, want change-id fallback")
	}
	if strings.ContainsAny(got, " \t\n") {
		t.Errorf("change-id should be a single token, got %q", got)
	}

	// Now place a bookmark at @ — expect the bookmark name to win.
	runJJ(t, dir, "bookmark", "create", "feature", "-r", "@")
	if got := j.CurrentBranch(); got != "feature" {
		t.Errorf("CurrentBranch() with bookmark at @ = %q, want feature", got)
	}
}

func TestJJVCS_MergeBase(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	withCwd(t, dir)

	j := &JJVCS{}
	mainSHA := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	// Add a working-copy edit so @ diverges from main.
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mb, err := j.MergeBase("main")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if mb != mainSHA {
		t.Errorf("MergeBase(main) = %q, want %q", mb, mainSHA)
	}

	if _, err := j.MergeBase(""); err == nil {
		t.Error("MergeBase(empty) should return an error")
	}
	if _, err := j.MergeBase("does-not-exist-bookmark"); err == nil {
		t.Error("MergeBase(unknown) should return an error")
	}
}

func TestJJVCS_ChangedFilesScoped(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withCwd(t, dir)

	// JJ has no staging area: staged/unstaged must return (nil, nil).
	for _, scope := range []string{"staged", "unstaged", "anything-else"} {
		t.Run(scope, func(t *testing.T) {
			changes, err := j.ChangedFilesScoped(scope, base)
			if err != nil {
				t.Errorf("ChangedFilesScoped(%q) error: %v", scope, err)
			}
			if changes != nil {
				t.Errorf("ChangedFilesScoped(%q) = %v, want nil", scope, changes)
			}
		})
	}

	// "branch" delegates to ChangedFilesFromBaseInDir.
	t.Run("branch delegates", func(t *testing.T) {
		got, err := j.ChangedFilesScoped("branch", base)
		if err != nil {
			t.Fatalf("ChangedFilesScoped(branch): %v", err)
		}
		assertFileChangesEqual(t, got, []FileChange{{Path: "app.txt", Status: "modified"}})
	})
}

func TestJJVCS_FileDiffScoped(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withCwd(t, dir)

	// Non-branch scopes return (nil, nil).
	hunks, err := j.FileDiffScoped("app.txt", "staged", base, dir)
	if err != nil || hunks != nil {
		t.Errorf("FileDiffScoped(staged) = (%v, %v), want (nil, nil)", hunks, err)
	}

	// "branch" delegates and produces hunks.
	hunks, err = j.FileDiffScoped("app.txt", "branch", base, dir)
	if err != nil {
		t.Fatalf("FileDiffScoped(branch): %v", err)
	}
	if len(hunks) == 0 {
		t.Error("FileDiffScoped(branch) returned no hunks for a modified file")
	}
}

func TestJJVCS_FileDiffForCommit_AndChangedFilesForCommit(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	// Make a new commit on top of main with another change.
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "feature.txt")
	runJJWithUser(t, dir, "commit", "-m", "add feature")
	commitSHA := runJJ(t, dir, "log", "-r", "@-", "--no-graph", "-T", "commit_id")

	changes, err := j.ChangedFilesForCommit(commitSHA, dir)
	if err != nil {
		t.Fatalf("ChangedFilesForCommit: %v", err)
	}
	assertFileChangesEqual(t, changes, []FileChange{{Path: "feature.txt", Status: "added"}})

	hunks, err := j.FileDiffForCommit("feature.txt", commitSHA, dir)
	if err != nil {
		t.Fatalf("FileDiffForCommit: %v", err)
	}
	if len(hunks) == 0 {
		t.Error("FileDiffForCommit returned no hunks")
	}

	// Unknown sha errors on resolve.
	if _, err := j.FileDiffForCommit("feature.txt", "deadbeefdeadbeef", dir); err == nil {
		t.Error("FileDiffForCommit on unknown sha should error")
	}
	if _, err := j.ChangedFilesForCommit("deadbeefdeadbeef", dir); err == nil {
		t.Error("ChangedFilesForCommit on unknown sha should error")
	}
}

func TestJJVCS_FileDiffUnifiedCtx_Cancellation(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invoking — jj subprocess exits with cancellation error.

	_, err := j.FileDiffUnifiedCtx(ctx, "app.txt", base, dir)
	if err == nil {
		t.Fatal("FileDiffUnifiedCtx with cancelled ctx should error")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

func TestJJVCS_FileDiffUnifiedCtx_DeadlineExceeded(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	_, err := j.FileDiffUnifiedCtx(ctx, "app.txt", base, dir)
	if err == nil {
		t.Fatal("FileDiffUnifiedCtx with expired deadline should error")
	}
}

func TestJJVCS_FileDiffUnifiedNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	content := "line one\nline two\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	j := &JJVCS{}
	hunks, err := j.FileDiffUnifiedNewFile(path)
	if err != nil {
		t.Fatalf("FileDiffUnifiedNewFile: %v", err)
	}
	if len(hunks) == 0 {
		t.Fatal("expected at least one hunk for a synthetic new-file diff")
	}

	if _, err := j.FileDiffUnifiedNewFile(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("missing file should error")
	}
}

func TestJJVCS_CommitLog(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}

	// Empty baseRef → returns (nil, nil) per implementation.
	commits, err := j.CommitLog("", "", dir)
	if err != nil || commits != nil {
		t.Errorf("CommitLog(empty base) = (%v, %v), want (nil, nil)", commits, err)
	}

	// Add a commit on top of main so there is something to enumerate.
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "extra.txt")
	runJJWithUser(t, dir, "commit", "-m", "add extra")

	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	commits, err = j.CommitLog(base, "", dir)
	if err != nil {
		t.Fatalf("CommitLog: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("CommitLog returned no commits")
	}
	// Subjects come from description.first_line(); we wrote "add extra".
	found := false
	for _, c := range commits {
		if strings.Contains(c.Message, "add extra") {
			found = true
		}
	}
	if !found {
		t.Errorf("CommitLog did not surface our commit subject; got %+v", commits)
	}

	if _, err := j.CommitLog("does-not-exist", "", dir); err == nil {
		t.Error("CommitLog with unknown base should error")
	}
}

func TestJJVCS_WorkingTreeFingerprint_ChangesWithEdits(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	withCwd(t, dir)

	j := &JJVCS{}
	first := j.WorkingTreeFingerprint()
	if first == "" {
		t.Fatal("WorkingTreeFingerprint returned empty for a valid repo")
	}

	if err := os.WriteFile(filepath.Join(dir, "fingerprint.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "fingerprint.txt")

	second := j.WorkingTreeFingerprint()
	if second == first {
		t.Error("WorkingTreeFingerprint did not change after a working-copy edit")
	}
}

func TestJJVCS_UntrackedFiles_AlwaysNil(t *testing.T) {
	j := &JJVCS{}
	got, err := j.UntrackedFiles("anywhere")
	if err != nil || got != nil {
		t.Errorf("UntrackedFiles = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestJJVCS_AllTrackedFiles(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	files, err := j.AllTrackedFiles(dir)
	if err != nil {
		t.Fatalf("AllTrackedFiles: %v", err)
	}
	found := false
	for _, f := range files {
		if f == "app.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("AllTrackedFiles did not include app.txt; got %v", files)
	}
}

func TestJJVCS_RemoteBranches_FromOriginClone(t *testing.T) {
	work := initTestJJCloneWithOriginMain(t)
	j := &JJVCS{}
	branches, err := j.RemoteBranches(work)
	if err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}
	// "main" is on origin in the seed; "git" remote (the colocated bridge)
	// must be filtered out so we don't see duplicates.
	if len(branches) != 1 || branches[0] != "main" {
		t.Errorf("RemoteBranches = %v, want [main]", branches)
	}
}

func TestJJVCS_RemoteBranches_LocalOnlyReturnsNil(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	branches, err := j.RemoteBranches(dir)
	if err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}
	if len(branches) != 0 {
		t.Errorf("RemoteBranches on local-only = %v, want empty", branches)
	}
}

func TestJJVCS_FileStatusInRepo(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := j.FileStatusInRepo("app.txt", base, dir); got != "modified" {
		t.Errorf("FileStatusInRepo(app.txt) = %q, want modified", got)
	}
	if got := j.FileStatusInRepo("not-changed.txt", base, dir); got != "" {
		t.Errorf("FileStatusInRepo(not-changed) = %q, want empty", got)
	}
}

func TestJJVCS_ChangedFilesAndDiffBetweenSHAs(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	mainSHA := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	if err := os.WriteFile(filepath.Join(dir, "between.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runJJ(t, dir, "file", "track", "between.txt")
	runJJWithUser(t, dir, "commit", "-m", "add between")
	headSHA := runJJ(t, dir, "log", "-r", "@-", "--no-graph", "-T", "commit_id")

	changes, err := j.ChangedFilesBetweenSHAs(mainSHA, headSHA, dir)
	if err != nil {
		t.Fatalf("ChangedFilesBetweenSHAs: %v", err)
	}
	assertFileChangesEqual(t, changes, []FileChange{{Path: "between.txt", Status: "added"}})

	hunks, err := j.FileDiffBetweenSHAs("between.txt", mainSHA, headSHA, dir)
	if err != nil {
		t.Fatalf("FileDiffBetweenSHAs: %v", err)
	}
	if len(hunks) == 0 {
		t.Error("FileDiffBetweenSHAs returned no hunks")
	}
}

func TestJJVCS_HasObject(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	mainSHA := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if !j.HasObject(mainSHA, dir) {
		t.Error("HasObject for known sha = false")
	}
	if j.HasObject("0123456789abcdef0123456789abcdef01234567", dir) {
		t.Error("HasObject for fabricated sha = true")
	}
}

func TestJJVCS_DiffNumstat(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	if got, err := j.DiffNumstat("", dir); err != nil || got != nil {
		t.Errorf("DiffNumstat(empty) = (%v, %v), want (nil, nil)", got, err)
	}

	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("base\nadd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := j.DiffNumstat(base, dir)
	if err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	if _, ok := got["app.txt"]; !ok {
		t.Errorf("DiffNumstat missing app.txt; got %v", got)
	}
}

func TestJJVCS_FileContentAtRef(t *testing.T) {
	dir := initTestJJRepoWithLocalMain(t)
	j := &JJVCS{}
	base := runJJ(t, dir, "log", "-r", "bookmarks(exact:\"main\")", "--no-graph", "-T", "commit_id")

	got, err := j.FileContentAtRef("app.txt", base, dir)
	if err != nil {
		t.Fatalf("FileContentAtRef: %v", err)
	}
	if got != "base\n" {
		t.Errorf("FileContentAtRef = %q, want %q", got, "base\n")
	}

	// Missing file at a valid commit returns "" with nil error (matches ReadFileAtSHA).
	got, err = j.FileContentAtRef("missing.txt", base, dir)
	if err != nil || got != "" {
		t.Errorf("FileContentAtRef(missing) = (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestIsJJRootCommitID(t *testing.T) {
	if !isJJRootCommitID("0000000000000000000000000000000000000000") {
		t.Error("expected true for canonical root commit id")
	}
	if !isJJRootCommitID("  0000000000000000000000000000000000000000  ") {
		t.Error("expected trim before compare")
	}
	if isJJRootCommitID("deadbeef") {
		t.Error("expected false for non-root")
	}
	if isJJRootCommitID("") {
		t.Error("expected false for empty")
	}
}

func TestLooksLikeHexPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"abcd", true},
		{"ABCDEF1234", true},
		{"abc", false},  // too short
		{"abcg", false}, // non-hex
		{"main", false}, // bookmark name
		{"", false},
		{"abc-def", false},
	}
	for _, c := range cases {
		if got := looksLikeHexPrefix(c.in); got != c.want {
			t.Errorf("looksLikeHexPrefix(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsSimpleJJBookmarkName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"main", true},
		{"feature/x", true},
		{"name with space", false},
		{"name@remote", false},
		{"a|b", false},
		{"a&b", false},
		{"a~b", false},
		{"a:b", false},
		{"(parens)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isSimpleJJBookmarkName(c.in); got != c.want {
			t.Errorf("isSimpleJJBookmarkName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
