package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// withFetchPRByNumber temporarily replaces fetchPRByNumberFn for the duration of t.
// Also resets prMetaCache so cached PRInfo from a previous test (or stub) does
// not shadow the freshly-installed fetchFn. Per-test isolation matters because
// fetchPRByNumber consults the cache before invoking the stub.
func withFetchPRByNumber(t *testing.T, fn func(int) (*PRInfo, error)) {
	t.Helper()
	prev := fetchPRByNumberFn
	fetchPRByNumberFn = fn
	prMetaCache.reset()
	t.Cleanup(func() {
		fetchPRByNumberFn = prev
		prMetaCache.reset()
	})
}

func TestPersistActiveDiffScope_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &Session{RepoRoot: dir, OutputDir: dir}

	if err := s.persistActiveDiffScope("layer"); err != nil {
		t.Fatal(err)
	}
	cj, err := loadCritJSON(filepath.Join(dir, ".crit.json"))
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
	cj, _ = loadCritJSON(filepath.Join(dir, ".crit.json"))
	if cj.ActiveDiffScope != "" {
		t.Errorf("after persist(\"\"), got %q (should be cleared)", cj.ActiveDiffScope)
	}
}

// Bucketing tests — replacements for the old applyPushGates tests. Same
// semantic concerns (full-stack diverted, layer/full-stack split, stale head,
// no anchor, working-tree legacy filter), but the assertion now is "comment
// ends up in bucket X with reason Y" instead of "abort with message Z". This
// matches the new bucket-and-show push flow which never aborts on a single
// stale or unanchored comment.

func TestBucketComments_FullStackToOrphan(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "full_stack",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "fs", DiffScope: "full_stack", HeadSHA: "h1"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "h1", true)
	if len(b.Postable) != 0 || len(b.Unmapped) != 0 {
		t.Errorf("expected only FullStack bucket populated, got %+v", b)
	}
	if len(b.FullStack) != 1 || b.FullStack[0].Reason != bucketReasonFullStack {
		t.Errorf("expected 1 FullStack/full-stack-scope, got %+v", b.FullStack)
	}
}

func TestBucketComments_StaleToOrphan(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "stale", DiffScope: "layer", HeadSHA: "abc1234old"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "currentHEAD", true)
	if len(b.Postable) != 0 {
		t.Errorf("expected stale comment NOT postable, got %+v", b.Postable)
	}
	if len(b.Unmapped) != 1 || b.Unmapped[0].Reason != bucketReasonStale {
		t.Fatalf("expected 1 stale Unmapped, got %+v", b.Unmapped)
	}
	if !strings.Contains(b.Unmapped[0].Detail, "abc1234") {
		t.Errorf("Detail should mention old SHA, got %q", b.Unmapped[0].Detail)
	}
}

func TestBucketComments_NoAnchorToOrphan(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "anchorless", DiffScope: "layer", HeadSHA: ""},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "head1", true)
	if len(b.Postable) != 0 {
		t.Errorf("expected no-anchor comment NOT postable, got %+v", b.Postable)
	}
	if len(b.Unmapped) != 1 || b.Unmapped[0].Reason != bucketReasonNoAnchor {
		t.Errorf("expected 1 no-anchor Unmapped, got %+v", b.Unmapped)
	}
}

func TestBucketComments_WorkingTreeKeepsAll(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "legacy", DiffScope: ""},
				{ID: "c2", Body: "layer", DiffScope: "layer", HeadSHA: "h"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.Postable) != 1 || b.Postable[0].Comment.Body != "legacy" {
		t.Errorf("expected only legacy postable, got %+v", b.Postable)
	}
	// Non-legacy in working-tree push goes to FullStack bucket (different
	// focus — can't post layer-scope from working-tree push).
	if len(b.FullStack) != 1 || b.FullStack[0].Comment.Body != "layer" {
		t.Errorf("expected layer comment diverted to FullStack, got %+v", b.FullStack)
	}
}

func TestSetFocus_Range_RebuildsFiles(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "added.txt", "y\n", "add y")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:      dir,
		OutputDir:     dir,
		VCS:           &GitVCS{},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
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
	cj, _ := loadCritJSON(filepath.Join(dir, ".crit.json"))
	if cj.ActiveDiffScope != "layer" {
		t.Errorf("disk ActiveDiffScope = %q, want layer", cj.ActiveDiffScope)
	}
}

func TestSetFocus_FullStackRequiresDefaultSHA(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:      dir,
		OutputDir:     dir,
		VCS:           &GitVCS{},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
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
		RepoRoot:      dir,
		OutputDir:     dir,
		VCS:           &GitVCS{},
		Branch:        "main", // working-tree rebuild needs a branch matching DefaultBranch()
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}
	// Start in range/layer.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	cj, _ := loadCritJSON(filepath.Join(dir, ".crit.json"))
	if cj.ActiveDiffScope != "layer" {
		t.Fatalf("setup: ActiveDiffScope=%q want layer", cj.ActiveDiffScope)
	}

	// Toggle to working tree.
	if err := s.SetFocus(Focus{Kind: FocusWorkingTree}); err != nil {
		t.Fatal(err)
	}
	cj, _ = loadCritJSON(filepath.Join(dir, ".crit.json"))
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
		RepoRoot:      dir,
		OutputDir:     dir,
		VCS:           &GitVCS{},
		Branch:        "main",
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
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
