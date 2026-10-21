package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// branchOnlyScopedVCS behaves like the jj and Sapling backends: FileDiffScoped
// only knows the "branch" scope.
type branchOnlyScopedVCS struct {
	*vcs.GitVCS
}

func (v branchOnlyScopedVCS) FileDiffScoped(path, scope, baseRef, dir string, ignoreWhitespace bool) ([]vcs.DiffHunk, error) {
	if scope == "branch" {
		return v.GitVCS.FileDiffScoped(path, scope, baseRef, dir, ignoreWhitespace)
	}
	return nil, nil
}

func fileInfoByPath(info SessionInfo, path string) (SessionFileInfo, bool) {
	for _, fi := range info.Files {
		if fi.Path == path {
			return fi, true
		}
	}
	return SessionFileInfo{}, false
}

// newBranchChangeRepo commits a change to app.go on a feature branch, so the
// session starts with app.go in its diff.
func newBranchChangeRepo(t *testing.T) string {
	t.Helper()
	dir := newAllScopeRepo(t)
	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(allScopeSample, "\"base\"", "\"branch\"", 1))
	gitT(t, dir, "commit", "-am", "branch change")
	chdirForTest(t, dir)
	return dir
}

// Work done after the session started must show under "all" without any
// refresh: in the file list (with stats and the generated flag) and in the
// file diff (with the current content and its hash).
func TestGetSessionInfoScoped_AllListsWorkDoneAfterStartup(t *testing.T) {
	dir := newAllScopeRepo(t)
	writeFile(t, filepath.Join(dir, ".gitattributes"), "*.pb.go linguist-generated\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "attributes")
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)

	newContent := "package sample\n\nfunc added() {}\n"
	writeFile(t, filepath.Join(dir, "new.go"), newContent)
	writeFile(t, filepath.Join(dir, "api.pb.go"), "package sample\n")

	info := sess.GetSessionInfoScoped("all", "")
	fi, ok := fileInfoByPath(info, "new.go")
	if !ok {
		t.Fatalf("new.go missing from scope=all after startup; files = %+v", info.Files)
	}
	if fi.Additions != 3 {
		t.Errorf("new.go additions = %d, want 3", fi.Additions)
	}
	if gen, ok := fileInfoByPath(info, "api.pb.go"); !ok || !gen.Generated {
		t.Errorf("api.pb.go must be listed as generated, got %+v (listed=%v)", gen, ok)
	}
	if info.Cwd != sess.RepoRoot || info.VCSName != "git" {
		t.Errorf("Cwd/VCSName = %q/%q, want %q/git", info.Cwd, info.VCSName, sess.RepoRoot)
	}

	snap, ok := sess.GetFileDiffSnapshotScoped("new.go", "all", "", false)
	if !ok {
		t.Fatal("scope=all diff for new.go: not found")
	}
	if snap["content"] != newContent {
		t.Errorf("content = %q, want %q", snap["content"], newContent)
	}
	if snap["file_hash"] != fileHash([]byte(newContent)) {
		t.Errorf("file_hash = %v, want hash of the current content", snap["file_hash"])
	}
	if text := renderHunksForTest(snap); !strings.Contains(text, "+func added() {}") {
		t.Errorf("hunks must add the new file, got:\n%s", text)
	}
}

// A commented file whose changes are reverted leaves the diff but must stay
// listed as an orphan, so its comments are still reachable.
func TestGetSessionInfoScoped_AllKeepsRevertedCommentedFileAsOrphan(t *testing.T) {
	dir := newBranchChangeRepo(t)
	sess := startAllScopeSession(t)

	if _, ok := sess.AddComment("app.go", 7, 7, "", "keep me", "", "reviewer", ""); !ok {
		t.Fatal("AddComment failed")
	}
	writeFile(t, filepath.Join(dir, "app.go"), allScopeSample)

	info := sess.GetSessionInfoScoped("all", "")
	fi, ok := fileInfoByPath(info, "app.go")
	if !ok || !fi.Orphaned || fi.CommentCount != 1 {
		t.Errorf("app.go = %+v (listed=%v), want an orphaned row with 1 comment", fi, ok)
	}
}

// jj and Sapling only implement "branch" in FileDiffScoped; scope "all" must
// still produce stats and hunks.
func TestGetSessionInfoScoped_AllWorksWithoutScopedDiffSupport(t *testing.T) {
	newBranchChangeRepo(t)
	sess, err := NewGitSession(branchOnlyScopedVCS{&vcs.GitVCS{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	quiesceSession(t, sess)

	info := sess.GetSessionInfoScoped("all", "")
	fi, ok := fileInfoByPath(info, "app.go")
	if !ok || fi.Additions == 0 || fi.Deletions == 0 {
		t.Errorf("app.go = %+v (listed=%v), want non-zero stats", fi, ok)
	}
	snap, _ := sess.GetFileDiffSnapshotScoped("app.go", "all", "", false)
	if text := renderHunksForTest(snap); !strings.Contains(text, "+\tprintln(\"branch\")") {
		t.Errorf("scope=all hunks missing the branch change, got:\n%s", text)
	}
}

func TestGetFileDiffSnapshotScoped_AllMarkdownHasBaseContent(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "doc.md"), "# Title\n\nbase text\n")
	gitT(t, dir, "add", "doc.md")
	gitT(t, dir, "commit", "-m", "doc")
	gitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "doc.md"), "# Title\n\nbranch text\n")
	gitT(t, dir, "commit", "-am", "edit doc")
	chdirForTest(t, dir)
	sess := startAllScopeSession(t)

	snap, ok := sess.GetFileDiffSnapshotScoped("doc.md", "all", "", false)
	if !ok {
		t.Fatal("scope=all diff for doc.md: not found")
	}
	if snap["previous_content"] != "# Title\n\nbase text\n" {
		t.Errorf("previous_content = %q, want the base version", snap["previous_content"])
	}
}

// Comments must follow the working tree the reviewer sees: existing comments
// move with mid-round edits, a new comment takes its anchor from the live
// text, and round complete carries both forward to the right lines.
func TestSyncWorkingTree_CommentsFollowLiveContentThroughRound(t *testing.T) {
	dir := newBranchChangeRepo(t)
	appPath := filepath.Join(dir, "app.go")
	committed, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	// Lines after the commented one, so a stale position lands on other code
	// instead of being clamped to the last line.
	committed = append(committed, []byte("\nfunc tail1() {}\nfunc tail2() {}\nfunc tail3() {}\nfunc tail4() {}\nfunc tail5() {}\n")...)
	writeFile(t, appPath, string(committed))
	gitT(t, dir, "commit", "-qam", "tail")
	sess := startAllScopeSession(t)

	// Line 7 is "func stable() {}".
	first, ok := sess.AddComment("app.go", 7, 7, "", "first", "", "reviewer", "")
	if !ok {
		t.Fatal("AddComment failed")
	}

	// The agent inserts three lines above it mid-round.
	shifted := strings.Replace(string(committed), "package sample\n", "package sample\n\n// one\n// two\n// three\n", 1)
	writeFile(t, appPath, shifted)
	sess.GetSessionInfoScoped("all", "")

	byID := func() map[string]Comment {
		m := map[string]Comment{}
		for _, c := range sess.GetComments("app.go") {
			m[c.ID] = c
		}
		return m
	}
	if c := byID()[first.ID]; c.StartLine != 11 || c.Drifted {
		t.Fatalf("after the mid-round edit the comment is at %d (drifted=%v), want 11", c.StartLine, c.Drifted)
	}

	// The reviewer comments on live line 11, the same function.
	second, ok := sess.AddComment("app.go", 11, 11, "", "second", "", "reviewer", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if second.Anchor != first.Anchor {
		t.Errorf("new comment anchor = %q, want the live line %q", second.Anchor, first.Anchor)
	}

	// Another edit lands on disk, but the browser still shows the text it
	// loaded: a comment on line 11 there must anchor to that text, and the
	// next fetch moves it with the content.
	writeFile(t, appPath, strings.Replace(shifted, "package sample\n", "package sample\n// four\n", 1))
	third, ok := sess.AddComment("app.go", 11, 11, "", "third", "", "reviewer", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	if third.Anchor != first.Anchor {
		t.Errorf("comment on the loaded text has anchor %q, want %q", third.Anchor, first.Anchor)
	}
	sess.GetSessionInfoScoped("all", "")
	for _, id := range []string{first.ID, second.ID, third.ID} {
		if c := byID()[id]; c.StartLine != 12 {
			t.Errorf("comment %s after the second edit at %d, want 12", id, c.StartLine)
		}
	}

	// Finish review writes the comments. The agent then removes the four
	// lines and replies (the agent-reply path syncs), and signals round
	// complete at once: the moved positions must already be on disk.
	if err := sess.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, appPath, string(committed))
	sess.SyncWorkingTree()
	sess.SignalRoundComplete()
	<-sess.roundComplete
	sess.handleRoundCompleteGit()

	got := byID()
	for _, id := range []string{first.ID, second.ID, third.ID} {
		if c := got[id]; c.StartLine != 7 || c.Drifted {
			t.Errorf("comment %s after round complete at %d (drifted=%v), want 7", id, c.StartLine, c.Drifted)
		}
	}
}

// Between SignalRoundComplete and the watcher's carry-forward, the comments
// are reloaded from the review file with positions for the old content, so a
// sync must not swap the content underneath them.
func TestSyncWorkingTree_SkipsWhileRoundCompletePending(t *testing.T) {
	dir := newBranchChangeRepo(t)
	sess := startAllScopeSession(t)
	before := sess.FileByPath("app.go").Content

	sess.SignalRoundComplete()
	<-sess.roundComplete
	writeFile(t, filepath.Join(dir, "app.go"), "package sample\n")
	sess.SyncWorkingTree()

	if content := sess.FileByPath("app.go").Content; content != before {
		t.Errorf("content changed while round complete was pending:\n%s", content)
	}

	sess.handleRoundCompleteGit()
	sess.mu.RLock()
	pending := sess.roundCompletePending
	sess.mu.RUnlock()
	if pending {
		t.Error("roundCompletePending still set after the watcher finished")
	}
}

// With no change since startup, live "all" must match the round snapshot it
// replaces: same rows (status, stats, flags) and the same hunks per file.
func TestGetSessionInfoScoped_AllMatchesSnapshotWhenUnchanged(t *testing.T) {
	dir := initTestRepo(t)
	setHome(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "app.go"), allScopeSample)
	writeFile(t, filepath.Join(dir, "gone.txt"), "to be deleted\n")
	writeFile(t, filepath.Join(dir, "old_name.go"), "package sample\n\nfunc moved() {\n\tprintln(1)\n\tprintln(2)\n\tprintln(3)\n}\n")
	writeFile(t, filepath.Join(dir, "doc.md"), "# Doc\n\nbase\n")
	writeFile(t, filepath.Join(dir, ".gitattributes"), "*.pb.go linguist-generated\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-qm", "base files")
	gitT(t, dir, "checkout", "-q", "-b", "feature")

	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(allScopeSample, "\"base\"", "\"branch\"", 1))
	gitT(t, dir, "rm", "-q", "gone.txt")
	gitT(t, dir, "mv", "old_name.go", "new_name.go")
	writeFile(t, filepath.Join(dir, "doc.md"), "# Doc\n\nbranch\n")
	writeFile(t, filepath.Join(dir, "added.go"), "package sample\n")
	writeFile(t, filepath.Join(dir, "api.pb.go"), "package sample\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-qm", "branch work")
	writeFile(t, filepath.Join(dir, "untracked.go"), "package sample\n\nvar x = 1\n")
	chdirForTest(t, dir)

	sess := startAllScopeSession(t)
	cached := sess.GetSessionInfo()
	live := sess.GetSessionInfoScoped("all", "")

	if len(cached.Files) < 7 {
		t.Fatalf("fixture produced %d cached files, want at least 7: %+v", len(cached.Files), cached.Files)
	}
	if len(live.Files) != len(cached.Files) {
		t.Fatalf("live has %d files, cached %d\nlive:   %+v\ncached: %+v", len(live.Files), len(cached.Files), live.Files, cached.Files)
	}
	for _, want := range cached.Files {
		got, ok := fileInfoByPath(live, want.Path)
		if !ok {
			t.Errorf("%s missing from live", want.Path)
			continue
		}
		if got != want {
			t.Errorf("%s row differs\nlive:   %+v\ncached: %+v", want.Path, got, want)
		}
		cachedDiff, _ := sess.GetFileDiffSnapshot(want.Path, false)
		liveDiff, _ := sess.GetFileDiffSnapshotScoped(want.Path, "all", "", false)
		if a, b := renderHunksForTest(liveDiff), renderHunksForTest(cachedDiff); a != b {
			t.Errorf("%s hunks differ\nlive:\n%s\ncached:\n%s", want.Path, a, b)
		}
		if a, b := liveDiff["previous_content"], cachedDiff["previous_content"]; a != b {
			t.Errorf("%s previous_content differs: live %q, cached %q", want.Path, a, b)
		}
	}
	if live.Cwd != cached.Cwd || live.SessionKey != cached.SessionKey || live.VCSName != cached.VCSName {
		t.Errorf("session fields differ: live %q/%q/%q, cached %q/%q/%q",
			live.Cwd, live.SessionKey, live.VCSName, cached.Cwd, cached.SessionKey, cached.VCSName)
	}
}

// A second round-complete signal while the watcher handles the first must
// keep syncing off until the second is handled too.
func TestSyncWorkingTree_PendingSurvivesSecondSignal(t *testing.T) {
	newBranchChangeRepo(t)
	sess := startAllScopeSession(t)

	sess.SignalRoundComplete()
	<-sess.roundComplete
	sess.mu.RLock()
	first := sess.roundSignalSeq
	sess.mu.RUnlock()
	sess.SignalRoundComplete() // arrives while the first is being handled
	sess.finishRoundPending(first)

	sess.mu.RLock()
	pending := sess.roundCompletePending
	sess.mu.RUnlock()
	if !pending {
		t.Error("roundCompletePending cleared although a newer signal is unhandled")
	}
	sess.handleRoundCompleteGit()
	sess.mu.RLock()
	pending = sess.roundCompletePending
	sess.mu.RUnlock()
	if pending {
		t.Error("roundCompletePending still set after the second signal was handled")
	}
}

// A file that left the session (orphaned row) and then changes again must be
// served from disk, not from the orphan's empty content.
func TestGetFileDiffSnapshotScoped_AllReturnedFileReadsDisk(t *testing.T) {
	dir := newBranchChangeRepo(t)
	sess := startAllScopeSession(t)
	back := "package sample\n\nfunc back() {}\n"
	writeFile(t, filepath.Join(dir, "returned.go"), back)
	sess.mu.Lock()
	sess.Files = append(sess.Files, &FileEntry{Path: "returned.go", Status: "removed", Orphaned: true, FileType: "code"})
	sess.mu.Unlock()

	snap, ok := sess.GetFileDiffSnapshotScoped("returned.go", "all", "", false)
	if !ok {
		t.Fatal("scope=all diff for returned.go: not found")
	}
	if snap["content"] != back {
		t.Errorf("content = %q, want the file on disk", snap["content"])
	}
}
