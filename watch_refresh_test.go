package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeWatchVCS implements only the VCS methods exercised by RefreshDiffs,
// RefreshFileList, and handleRoundCompleteGit. Other methods inherit zero
// values from the embedded interface; they will panic if called, which is
// what we want — surfacing accidentally-exercised paths.
type fakeWatchVCS struct {
	VCS
	mu              sync.Mutex
	currentBranch   string
	defaultBranch   string
	defaultChanges  []FileChange
	branchChanges   []FileChange
	diffs           map[string][]DiffHunk
	numstats        map[string]NumstatEntry
	diffCalls       int32
	numstatCalls    int32
	changedFlsCalls int32
}

func (f *fakeWatchVCS) CurrentBranch() string { return f.currentBranch }
func (f *fakeWatchVCS) DefaultBranch() string { return f.defaultBranch }

func (f *fakeWatchVCS) ChangedFilesOnDefaultInDir(_ string) ([]FileChange, error) {
	atomic.AddInt32(&f.changedFlsCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FileChange, len(f.defaultChanges))
	copy(out, f.defaultChanges)
	return out, nil
}

func (f *fakeWatchVCS) ChangedFilesFromBaseInDir(_, _ string) ([]FileChange, error) {
	atomic.AddInt32(&f.changedFlsCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FileChange, len(f.branchChanges))
	copy(out, f.branchChanges)
	return out, nil
}

func (f *fakeWatchVCS) FileDiffUnified(path, _, _ string) ([]DiffHunk, error) {
	atomic.AddInt32(&f.diffCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.diffs[path], nil
}

func (f *fakeWatchVCS) DiffNumstat(_, _ string) (map[string]NumstatEntry, error) {
	atomic.AddInt32(&f.numstatCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]NumstatEntry, len(f.numstats))
	for k, v := range f.numstats {
		out[k] = v
	}
	return out, nil
}

func newWatchSession(t *testing.T, v *fakeWatchVCS) *Session {
	t.Helper()
	return &Session{
		VCS:      v,
		RepoRoot: t.TempDir(),
		BaseRef:  "origin/main",
	}
}

func TestRefreshDiffs_SkipsDeletedAndLazyFiles(t *testing.T) {
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		diffs: map[string][]DiffHunk{
			"keep.go": {{OldStart: 1, NewStart: 1}},
			"gone.go": {{OldStart: 5, NewStart: 5}},
		},
	}
	s := newWatchSession(t, v)
	s.Files = []*FileEntry{
		{Path: "keep.go", Status: "modified"},
		{Path: "gone.go", Status: "deleted"},
		{Path: "lazy.go", Status: "modified", Lazy: true},
		{Path: "added.go", Status: "added", Content: "new content\n"},
	}

	s.RefreshDiffs()

	for _, f := range s.Files {
		switch f.Path {
		case "keep.go":
			if len(f.DiffHunks) != 1 {
				t.Errorf("keep.go: want 1 hunk, got %d", len(f.DiffHunks))
			}
		case "gone.go":
			if f.DiffHunks != nil {
				t.Errorf("gone.go: should be skipped, got %v", f.DiffHunks)
			}
		case "lazy.go":
			if f.DiffHunks != nil {
				t.Errorf("lazy.go: should be skipped, got %v", f.DiffHunks)
			}
		case "added.go":
			// new-file hunks come from FileDiffUnifiedNewFile, no VCS call.
			if len(f.DiffHunks) == 0 {
				t.Errorf("added.go: expected new-file hunks")
			}
		}
	}
	if got := atomic.LoadInt32(&v.diffCalls); got != 1 {
		t.Errorf("FileDiffUnified called %d times, want 1 (only keep.go)", got)
	}
}

func TestRefreshFileList_AddsAndRemoves(t *testing.T) {
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		branchChanges: []FileChange{
			{Path: "keep.go", Status: "modified"},
			{Path: "new.go", Status: "added"},
		},
	}
	s := newWatchSession(t, v)

	if err := os.WriteFile(filepath.Join(s.RepoRoot, "keep.go"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.RepoRoot, "new.go"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := &FileEntry{Path: "keep.go", AbsPath: filepath.Join(s.RepoRoot, "keep.go"), Status: "added"}
	s.Files = []*FileEntry{
		original,
		{Path: "removed.go", AbsPath: filepath.Join(s.RepoRoot, "removed.go"), Status: "modified"},
	}

	s.RefreshFileList()

	if len(s.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(s.Files))
	}
	paths := map[string]*FileEntry{}
	for _, f := range s.Files {
		paths[f.Path] = f
	}
	if _, ok := paths["removed.go"]; ok {
		t.Error("removed.go should be dropped")
	}
	keep := paths["keep.go"]
	if keep == nil {
		t.Fatal("keep.go missing")
	}
	if keep != original {
		t.Error("keep.go should reuse the existing FileEntry pointer (preserves comments)")
	}
	if keep.Status != "modified" {
		t.Errorf("keep.go status = %q, want %q (updated under write lock)", keep.Status, "modified")
	}
	newF := paths["new.go"]
	if newF == nil || newF.Status != "added" {
		t.Errorf("new.go missing or wrong status: %+v", newF)
	}
	if newF.Content != "new" {
		t.Errorf("new.go content = %q, want %q", newF.Content, "new")
	}
}

func TestRefreshFileList_LazyThresholdMarksOverflow(t *testing.T) {
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		numstats:      map[string]NumstatEntry{},
	}
	// Build > lazyFileThreshold (100) changes.
	total := lazyFileThreshold + 5
	for i := 0; i < total; i++ {
		path := fmt.Sprintf("dir/f%d.go", i)
		v.branchChanges = append(v.branchChanges, FileChange{Path: path, Status: "modified"})
		v.numstats[path] = NumstatEntry{Additions: i, Deletions: 0}
	}

	s := newWatchSession(t, v)
	s.RefreshFileList()

	if got := atomic.LoadInt32(&v.numstatCalls); got != 1 {
		t.Errorf("DiffNumstat calls = %d, want 1", got)
	}
	lazyCount := 0
	for _, f := range s.Files {
		if f.Lazy {
			lazyCount++
		}
	}
	if lazyCount != total-lazyFileThreshold {
		t.Errorf("lazy file count = %d, want %d", lazyCount, total-lazyFileThreshold)
	}
}

func TestWatch_RefreshDiffsAndFileList_RaceFree(t *testing.T) {
	t.Parallel()
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		branchChanges: []FileChange{
			{Path: "a.go", Status: "modified"},
			{Path: "b.go", Status: "modified"},
		},
		diffs: map[string][]DiffHunk{
			"a.go": {{OldStart: 1, NewStart: 1}},
			"b.go": {{OldStart: 1, NewStart: 1}},
		},
	}
	s := newWatchSession(t, v)
	if err := os.WriteFile(filepath.Join(s.RepoRoot, "a.go"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.RepoRoot, "b.go"), []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Files = []*FileEntry{
		{Path: "a.go", AbsPath: filepath.Join(s.RepoRoot, "a.go"), Status: "modified"},
		{Path: "b.go", AbsPath: filepath.Join(s.RepoRoot, "b.go"), Status: "modified"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.RefreshDiffs() }()
		go func() { defer wg.Done(); s.RefreshFileList() }()
	}
	wg.Wait()

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, f := range s.Files {
		if len(f.DiffHunks) == 0 {
			t.Errorf("file %s missing diff hunks after concurrent refresh", f.Path)
		}
	}
}

func TestHandleRoundCompleteGit_AdvancesRoundAndRefreshes(t *testing.T) {
	v := &fakeWatchVCS{
		currentBranch: "feature",
		defaultBranch: "main",
		branchChanges: []FileChange{
			{Path: "x.go", Status: "modified"},
		},
		diffs: map[string][]DiffHunk{
			"x.go": {{OldStart: 1, NewStart: 1}},
		},
	}
	s := newWatchSession(t, v)
	s.ReviewRound = 3
	s.lastRoundEdits = 4
	if err := os.WriteFile(filepath.Join(s.RepoRoot, "x.go"), []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Files = []*FileEntry{
		{Path: "x.go", AbsPath: filepath.Join(s.RepoRoot, "x.go"), Status: "modified"},
	}

	s.handleRoundCompleteGit()

	if s.ReviewRound != 4 {
		t.Errorf("ReviewRound = %d, want 4", s.ReviewRound)
	}
	if got := atomic.LoadInt32(&v.diffCalls); got == 0 {
		t.Error("expected RefreshDiffs to call FileDiffUnified")
	}
	if got := atomic.LoadInt32(&v.changedFlsCalls); got == 0 {
		t.Error("expected RefreshFileList to call ChangedFilesFromBaseInDir")
	}
}
