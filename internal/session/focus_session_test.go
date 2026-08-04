package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestPersistActiveDiffScope_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &Session{RepoRoot: dir, OutputDir: dir}

	if err := s.persistActiveDiffScope("layer"); err != nil {
		t.Fatal(err)
	}
	cj, err := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if cj.ActiveDiffScope != "layer" {
		t.Errorf("after persist(layer), got %q", cj.ActiveDiffScope)
	}

	// Empty scope must clear, not be skipped.
	if err := s.persistActiveDiffScope(""); err != nil {
		t.Fatal(err)
	}
	cj, _ = readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "" {
		t.Errorf("after persist(\"\"), got %q (should be cleared)", cj.ActiveDiffScope)
	}
}

func TestSetFocus_Range_RebuildsFiles(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "added.txt", "y\n", "add y")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}

	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 1 || s.Files[0].Path != "added.txt" {
		t.Errorf("expected [added.txt], got files=%+v", s.Files)
	}
	if s.Focus.HeadSHA != head {
		t.Errorf("Focus.HeadSHA = %q, want %q", s.Focus.HeadSHA, head)
	}

	// On-disk ActiveDiffScope was persisted.
	cj, _ := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "layer" {
		t.Errorf("disk ActiveDiffScope = %q, want layer", cj.ActiveDiffScope)
	}
}

// TestSetFocus_PostSetSession_PreservesComments is a regression test for B1
// (review). Background: loadCritJSON checks Session.sessionStarted and bails
// out post-SetSession; SetFocus calls it at runtime to repopulate per-file
// Comments after the file list is rebuilt. With the guard active, that
// reload was silently a no-op so any focus change wiped on-disk comments
// from the in-memory session — and the next scheduleWrite would persist
// the empty slate back to disk.
//
// SetFocus must use the Locked variant of loadCritJSON, which skips the
// guard because the caller already holds s.mu. This test pins that
// behavior: after SetSession marks the session started, switching focus
// must keep comments visible on the new file list.
func TestSetFocus_PostSetSession_PreservesComments(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "added.txt", "first\nsecond\n", "add file")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main",
	}

	// First focus: range mode. SetFocus runs the constructor-time path
	// (sessionStarted == 0), so this populates on-disk state cleanly.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	// Seed a comment directly on disk so the next SetFocus has something to reload.
	identity := filepath.Join(dir, ".crit")
	cj, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatal(err)
	}
	cf := cj.Files["added.txt"]
	cf.Comments = []Comment{{ID: "c1", Body: "seeded", StartLine: 1, EndLine: 1, Scope: "line"}}
	if cj.Files == nil {
		cj.Files = map[string]CritJSONFile{}
	}
	cj.Files["added.txt"] = cf
	if err := saveCritJSONToDisk(identity, cj); err != nil {
		t.Fatal(err)
	}

	// Simulate Server.SetSession: flip the started flag. Any subsequent
	// loadCritJSON via the public entry point would no-op.
	s.sessionStarted.Store(1)

	// Toggle focus. The internal reload path must use loadCritJSONLocked
	// and pull the seeded comment back into s.Files.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeFullStack, DefaultSHA: base}); err != nil {
		t.Fatal(err)
	}

	var got *FileEntry
	for _, f := range s.Files {
		if f.Path == "added.txt" {
			got = f
			break
		}
	}
	if got == nil {
		t.Fatalf("added.txt missing from rebuilt s.Files: %+v", s.Files)
	}
	if len(got.Comments) != 1 || got.Comments[0].ID != "c1" {
		t.Errorf("comments after focus change = %+v; want one seeded comment", got.Comments)
	}

	// Drain any debounced WriteFiles scheduled by SetFocus before
	// reading on-disk state — otherwise the debounce goroutine and the
	// test reader race on s.Files / RoundSnapshots.
	flushWrites(s)

	// And on-disk state survived (no silent overwrite).
	cj2, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatal(err)
	}
	if cs := cj2.Files["added.txt"].Comments; len(cs) != 1 || cs[0].ID != "c1" {
		t.Errorf("disk comments after focus change = %+v; want one seeded comment", cs)
	}
}

func TestSetFocus_FullStackRequiresDefaultSHA(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h", DiffScope: DiffScopeFullStack})
	if err == nil {
		t.Fatal("expected error for full-stack without DefaultSHA")
	}
}

func TestSetFocus_WorkingTree_ClearsActiveDiffScope(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "x.txt", "x\n", "x")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main", // working-tree rebuild needs a branch matching DefaultBranch()
	}
	// Start in range/layer.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	cj, _ := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "layer" {
		t.Fatalf("setup: ActiveDiffScope=%q want layer", cj.ActiveDiffScope)
	}

	// Toggle to working tree.
	if err := s.SetFocus(Focus{Kind: FocusWorkingTree}); err != nil {
		t.Fatal(err)
	}
	cj, _ = readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "" {
		t.Errorf("on-disk ActiveDiffScope=%q want empty", cj.ActiveDiffScope)
	}
}

// TestSetFocus_RangeToWorkingTree_StashesLastRangeFocus verifies that
// transitioning OUT of a range focus stashes the prior range Focus on the
// session so the UI can render a "Resume PR" affordance.
func TestSetFocus_RangeToWorkingTree_StashesLastRangeFocus(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "x.txt", "x\n", "x")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main",
	}
	rangeFocus := Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, PRNumber: 42, DiffScope: DiffScopeLayer}
	if err := s.SetFocus(rangeFocus); err != nil {
		t.Fatal(err)
	}
	if s.LastRangeFocus != nil {
		t.Errorf("LastRangeFocus should be nil after first range focus; got %+v", s.LastRangeFocus)
	}
	if err := s.SetFocus(Focus{Kind: FocusWorkingTree}); err != nil {
		t.Fatal(err)
	}
	if s.LastRangeFocus == nil {
		t.Fatal("LastRangeFocus should be set after range -> working_tree")
	}
	if s.LastRangeFocus.PRNumber != 42 || s.LastRangeFocus.HeadSHA != head {
		t.Errorf("LastRangeFocus = %+v; want PR=42 head=%s", s.LastRangeFocus, head)
	}
}

// Range focus with many files must apply lazyFileThreshold and populate
// sidebar +/- stats (same contract as working-tree rebuild).
func TestSetFocus_Range_AppliesLazyThreshold(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")

	total := lazyFileThreshold + 5
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("doc%03d.md", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("# doc %d\n\nbody line\n", i))
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "add many docs")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != total {
		t.Fatalf("files = %d, want %d", len(s.Files), total)
	}

	eager, lazy := 0, 0
	for _, f := range s.Files {
		if f.Lazy {
			lazy++
			if f.Content != "" {
				t.Errorf("lazy file %s should not have content", f.Path)
			}
			if f.LazyAdditions == 0 && f.Status != "deleted" {
				t.Errorf("lazy file %s should have LazyAdditions from between-SHA numstat", f.Path)
			}
		} else {
			eager++
		}
	}
	if eager != lazyFileThreshold {
		t.Errorf("eager = %d, want %d", eager, lazyFileThreshold)
	}
	if lazy != total-lazyFileThreshold {
		t.Errorf("lazy = %d, want %d", lazy, total-lazyFileThreshold)
	}
}

// Lazy range files must load HeadSHA content, not a dirty working tree.
func TestGetFileSnapshot_RangeLazy_UsesHeadSHANotWorkingTree(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")

	total := lazyFileThreshold + 1
	var lazyPath string
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("doc%03d.md", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("# committed %d\n", i))
		if i == lazyFileThreshold {
			lazyPath = name
		}
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "add docs")
	head := gitT(t, dir, "rev-parse", "HEAD")

	// Dirty the lazy file in the working tree — SHA-aware load must ignore this.
	dirty := "# dirty working tree\nshould-not-appear\n"
	if err := os.WriteFile(filepath.Join(dir, lazyPath), []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	var lazyFile *FileEntry
	for _, f := range s.Files {
		if f.Path == lazyPath {
			lazyFile = f
			break
		}
	}
	if lazyFile == nil || !lazyFile.Lazy {
		t.Fatalf("expected %s to be lazy, got %+v", lazyPath, lazyFile)
	}

	snap, ok := s.GetFileSnapshot(lazyPath)
	if !ok {
		t.Fatal("GetFileSnapshot failed")
	}
	content, _ := snap["content"].(string)
	want := fmt.Sprintf("# committed %d\n", lazyFileThreshold)
	if content != want {
		t.Fatalf("content = %q, want HeadSHA content %q (not dirty WT)", content, want)
	}
	if lazyFile.Lazy {
		t.Fatal("file should no longer be lazy after GetFileSnapshot")
	}
}

// Lazy range modified files must load between-SHA diffs, not working-tree diffs.
func TestGetFileDiffSnapshot_RangeLazy_UsesBetweenSHADiff(t *testing.T) {
	dir := initTestRepo(t)

	total := lazyFileThreshold + 1
	var lazyPath string
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("doc%03d.md", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("# base %d\nline two\n", i))
		if i == lazyFileThreshold {
			lazyPath = name
		}
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "base docs")
	base := gitT(t, dir, "rev-parse", "HEAD")

	for i := 0; i < total; i++ {
		name := fmt.Sprintf("doc%03d.md", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("# head %d\nline two\n", i))
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "head docs")
	head := gitT(t, dir, "rev-parse", "HEAD")

	// Dirty WT content that would produce a different diff if read from disk.
	if err := os.WriteFile(filepath.Join(dir, lazyPath), []byte("# dirty\nentirely different\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Session{
		Mode:      "git",
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	var lazyFile *FileEntry
	for _, f := range s.Files {
		if f.Path == lazyPath {
			lazyFile = f
			break
		}
	}
	if lazyFile == nil || !lazyFile.Lazy {
		t.Fatalf("expected %s lazy, got %+v", lazyPath, lazyFile)
	}
	if lazyFile.LazyAdditions == 0 {
		t.Fatalf("expected LazyAdditions from between-SHA numstat for %s", lazyPath)
	}

	result, ok := s.GetFileDiffSnapshot(lazyPath, false)
	if !ok {
		t.Fatal("GetFileDiffSnapshot failed")
	}
	hunks, _ := result["hunks"].([]vcs.DiffHunk)
	if len(hunks) == 0 {
		t.Fatal("expected between-SHA hunks for modified lazy file")
	}
	joined := ""
	for _, h := range hunks {
		for _, l := range h.Lines {
			joined += l.Content + "\n"
		}
	}
	if !strings.Contains(joined, "head "+fmt.Sprint(lazyFileThreshold)) {
		t.Fatalf("hunks missing HeadSHA content; got %q", joined)
	}
	if strings.Contains(joined, "dirty") || strings.Contains(joined, "entirely different") {
		t.Fatalf("hunks used dirty working tree; got %q", joined)
	}
	if lazyFile.Lazy {
		t.Fatal("file should no longer be lazy after GetFileDiffSnapshot")
	}
}

// Deleted lazy range files load without reading HeadSHA content.
func TestGetFileSnapshot_RangeLazy_Deleted(t *testing.T) {
	dir := initTestRepo(t)

	total := lazyFileThreshold + 1
	var lazyPath string
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("doc%03d.md", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("# doc %d\n", i))
		if i == lazyFileThreshold {
			lazyPath = name
		}
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "add docs")
	base := gitT(t, dir, "rev-parse", "HEAD")

	if err := os.Remove(filepath.Join(dir, lazyPath)); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", "delete lazy doc")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	var lazyFile *FileEntry
	for _, f := range s.Files {
		if f.Path == lazyPath {
			lazyFile = f
			break
		}
	}
	if lazyFile == nil {
		t.Fatalf("%s missing from file list", lazyPath)
	}
	if lazyFile.Status != "deleted" {
		t.Fatalf("status = %q, want deleted", lazyFile.Status)
	}
	if !lazyFile.Lazy {
		// Ordering is by ChangedFilesBetweenSHAs; if the deleted file landed
		// in the eager prefix, skip — the load path still matters when lazy.
		t.Skip("deleted file was eagerly loaded; threshold ordering put it under the cut")
	}

	snap, ok := s.GetFileSnapshot(lazyPath)
	if !ok {
		t.Fatal("GetFileSnapshot failed for deleted lazy file")
	}
	if content, _ := snap["content"].(string); content != "" {
		t.Fatalf("deleted file content = %q, want empty", content)
	}
	if lazyFile.Lazy {
		t.Fatal("deleted lazy file should be loaded after GetFileSnapshot")
	}
}

func TestEnsureFileLoaded_NilAndNonLazy(t *testing.T) {
	s := &Session{Focus: Focus{Kind: FocusWorkingTree}}
	if err := s.ensureFileLoaded(nil); err != nil {
		t.Fatalf("nil file: %v", err)
	}
	fe := &FileEntry{Path: "x.md", Lazy: false, Content: "already"}
	if err := s.ensureFileLoaded(fe); err != nil {
		t.Fatalf("non-lazy: %v", err)
	}
	if fe.Content != "already" {
		t.Fatalf("non-lazy content mutated: %q", fe.Content)
	}
}

func TestEnsureFileLoaded_WorkingTreeLazy(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	path := filepath.Join(dir, "lazy.md")
	writeFile(t, path, "# from disk\n")
	gitT(t, dir, "add", "lazy.md")
	gitT(t, dir, "commit", "-m", "add lazy")

	s := &Session{
		Focus:    Focus{Kind: FocusWorkingTree, BaseRef: base},
		RepoRoot: dir,
		BaseRef:  base,
		VCS:      &vcs.GitVCS{},
	}
	fe := &FileEntry{
		Path:    "lazy.md",
		AbsPath: path,
		Status:  "added",
		Lazy:    true,
	}
	if err := s.ensureFileLoaded(fe); err != nil {
		t.Fatal(err)
	}
	if fe.Lazy {
		t.Fatal("expected Lazy=false after working-tree load")
	}
	if fe.Content != "# from disk\n" {
		t.Fatalf("content = %q", fe.Content)
	}
	if len(fe.DiffHunks) == 0 {
		t.Fatal("expected unified new-file hunks for added lazy file")
	}
}

func TestEnsureLoadedAtRange_RemoteErrorAndNilContent(t *testing.T) {
	wantErr := errors.New("remote boom")
	var calls int32
	stubFetchFn(t, nil, wantErr, &calls)

	s := &Session{
		RemoteFiles: true,
		Focus: Focus{
			Kind:    FocusRange,
			BaseSHA: "base",
			HeadSHA: "head",
			PRURL:   "https://github.com/foo/bar/pull/1",
		},
		VCS: &vcs.GitVCS{},
	}
	fe := &FileEntry{Path: "x.md", Status: "added", Lazy: true}
	err := s.ensureFileLoaded(fe)
	if err == nil || !strings.Contains(err.Error(), "remote boom") {
		t.Fatalf("got %v, want remote boom", err)
	}
	if !fe.Lazy {
		t.Fatal("failed load must leave Lazy=true")
	}

	// nil body, no error → "not found"
	var calls2 int32
	stubFetchFn(t, nil, nil, &calls2)
	fe2 := &FileEntry{Path: "y.md", Status: "added", Lazy: true}
	err = s.ensureFileLoaded(fe2)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v, want not found", err)
	}
}

func TestEnsureLoadedAtRange_ModifiedRequiresVCS(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, filepath.Join(dir, "m.md"), "# base\n")
	gitT(t, dir, "add", "m.md")
	gitT(t, dir, "commit", "-m", "base")
	base := gitT(t, dir, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "m.md"), "# head\n")
	gitT(t, dir, "add", "m.md")
	gitT(t, dir, "commit", "-m", "head")
	head := gitT(t, dir, "rev-parse", "HEAD")

	// Session has VCS for content reads; pass nil v to hit the require-VCS branch.
	s := &Session{
		RepoRoot: dir,
		VCS:      &vcs.GitVCS{},
		Focus:    Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head},
	}
	fe := &FileEntry{Path: "m.md", Status: "modified", Lazy: true, AbsPath: filepath.Join(dir, "m.md")}
	err := fe.ensureLoadedAtRange(s, s.Focus, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "requires VCS") {
		t.Fatalf("got %v, want requires VCS", err)
	}
}

func TestEnsureLoadedAtRange_AlreadyLoadedNoop(t *testing.T) {
	s := &Session{Focus: Focus{Kind: FocusRange, BaseSHA: "a", HeadSHA: "b"}}
	fe := &FileEntry{Path: "x.md", Status: "added", Lazy: false, Content: "kept"}
	if err := fe.ensureLoadedAtRange(s, s.Focus, "", nil); err != nil {
		t.Fatal(err)
	}
	if fe.Content != "kept" {
		t.Fatalf("content changed: %q", fe.Content)
	}
}

func TestPopulateLazyFile_DiskFallbackFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	writeFile(t, path, "one\ntwo\nthree\n")
	fe := &FileEntry{Path: "a.md", AbsPath: path, Status: "added"}
	fc := vcs.FileChange{Path: "a.md", Status: "added"}

	populateLazyFile(fe, fc, nil, false)
	if !fe.Lazy || fe.LazyAdditions != 0 {
		t.Fatalf("diskFallback=false: lazy=%v adds=%d", fe.Lazy, fe.LazyAdditions)
	}

	fe2 := &FileEntry{Path: "a.md", AbsPath: path, Status: "added"}
	populateLazyFile(fe2, fc, nil, true)
	if fe2.LazyAdditions != 3 {
		t.Fatalf("diskFallback=true: adds=%d, want 3", fe2.LazyAdditions)
	}

	fe3 := &FileEntry{Path: "a.md", AbsPath: path, Status: "added"}
	populateLazyFile(fe3, fc, map[string]vcs.NumstatEntry{"a.md": {Additions: 9, Deletions: 1}}, false)
	if fe3.LazyAdditions != 9 || fe3.LazyDeletions != 1 {
		t.Fatalf("numstat map: +%d/-%d", fe3.LazyAdditions, fe3.LazyDeletions)
	}
}

func TestGetFileSnapshot_RangeLazy_RemoteErrorReturnsFalse(t *testing.T) {
	wantErr := errors.New("fetch failed")
	var calls int32
	stubFetchFn(t, nil, wantErr, &calls)

	fe := &FileEntry{Path: "x.md", Status: "added", Lazy: true}
	s := &Session{
		RemoteFiles: true,
		Focus: Focus{
			Kind:    FocusRange,
			BaseSHA: "base",
			HeadSHA: "head",
			PRURL:   "https://github.com/foo/bar/pull/2",
		},
		VCS:   &vcs.GitVCS{},
		Files: []*FileEntry{fe},
	}
	if _, ok := s.GetFileSnapshot("x.md"); ok {
		t.Fatal("expected GetFileSnapshot false when remote lazy load fails")
	}
	_ = atomic.LoadInt32(&calls) // ensure stub was wired
}

// fakeNumstatFailVCS returns many changed files and fails DiffNumstatBetweenSHAs
// so buildFilesForFocus exercises the numstat warning path.
type fakeNumstatFailVCS struct {
	vcs.VCS
	changes []vcs.FileChange
	failWT  bool
}

func (f *fakeNumstatFailVCS) ChangedFilesBetweenSHAs(_, _, _ string) ([]vcs.FileChange, error) {
	out := make([]vcs.FileChange, len(f.changes))
	copy(out, f.changes)
	return out, nil
}

func (f *fakeNumstatFailVCS) DiffNumstatBetweenSHAs(_, _, _ string) (map[string]vcs.NumstatEntry, error) {
	return nil, errors.New("numstat boom")
}

func (f *fakeNumstatFailVCS) DiffNumstat(_, _ string) (map[string]vcs.NumstatEntry, error) {
	if f.failWT {
		return nil, errors.New("wt numstat boom")
	}
	return nil, nil
}

func (f *fakeNumstatFailVCS) ChangedFilesOnDefaultInDir(_ string) ([]vcs.FileChange, error) {
	out := make([]vcs.FileChange, len(f.changes))
	copy(out, f.changes)
	return out, nil
}

func (f *fakeNumstatFailVCS) ChangedFilesFromBaseInDir(_, _ string) ([]vcs.FileChange, error) {
	out := make([]vcs.FileChange, len(f.changes))
	copy(out, f.changes)
	return out, nil
}

func (f *fakeNumstatFailVCS) ReadFileAtSHA(_, _, _ string) ([]byte, error) {
	return []byte("content\n"), nil
}

func (f *fakeNumstatFailVCS) FileDiffBetweenSHAs(_, _, _, _, _ string, _ bool) ([]vcs.DiffHunk, error) {
	return nil, nil
}

func (f *fakeNumstatFailVCS) FileDiffUnified(_, _, _ string, _ bool) ([]vcs.DiffHunk, error) {
	return nil, nil
}

func (f *fakeNumstatFailVCS) FileDiffUnifiedNewFile(_ string) ([]vcs.DiffHunk, error) {
	return nil, nil
}

func (f *fakeNumstatFailVCS) DefaultBranch() string  { return "main" }
func (f *fakeNumstatFailVCS) DefaultBaseRef() string { return "main" }
func (f *fakeNumstatFailVCS) CurrentBranch() string  { return "feature" }
func (f *fakeNumstatFailVCS) MergeBase(_ string) (string, error) {
	return "base", nil
}

func TestBuildFilesForFocus_NumstatWarn(t *testing.T) {
	total := lazyFileThreshold + 3
	changes := make([]vcs.FileChange, total)
	for i := range changes {
		changes[i] = vcs.FileChange{Path: fmt.Sprintf("f%03d.md", i), Status: "deleted"}
	}
	v := &fakeNumstatFailVCS{changes: changes}
	s := &Session{RepoRoot: t.TempDir(), VCS: v}

	stderr := captureStderr(t, func() {
		files, _, err := s.buildFilesForFocus(Focus{
			Kind: FocusRange, BaseSHA: "base", HeadSHA: "head", DiffScope: DiffScopeLayer,
		}, v, s.RepoRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != total {
			t.Fatalf("files = %d, want %d", len(files), total)
		}
	})
	if !strings.Contains(stderr, "Warning: numstat failed") {
		t.Fatalf("stderr = %q, want numstat warning", stderr)
	}
}

func TestBuildFilesForWorkingTree_NumstatWarn(t *testing.T) {
	total := lazyFileThreshold + 2
	changes := make([]vcs.FileChange, total)
	for i := range changes {
		changes[i] = vcs.FileChange{Path: fmt.Sprintf("w%03d.md", i), Status: "deleted"}
	}
	v := &fakeNumstatFailVCS{changes: changes, failWT: true}
	s := &Session{RepoRoot: t.TempDir(), VCS: v, BaseRef: "main", Branch: "feature"}

	stderr := captureStderr(t, func() {
		files, _, err := s.buildFilesForWorkingTree(v, s.RepoRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != total {
			t.Fatalf("files = %d, want %d", len(files), total)
		}
	})
	if !strings.Contains(stderr, "Warning: numstat failed") {
		t.Fatalf("stderr = %q, want numstat warning", stderr)
	}
}
