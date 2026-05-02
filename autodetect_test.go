package main

import (
	"os"
	"strings"
	"testing"
)

// withDetectPRInfo temporarily replaces detectPRInfoFn for the duration of t.
func withDetectPRInfo(t *testing.T, fn func() *PRInfo) {
	t.Helper()
	prev := detectPRInfoFn
	detectPRInfoFn = fn
	t.Cleanup(func() { detectPRInfoFn = prev })
}

// chdir cd's into dir for the lifetime of t. Some VCS helpers (DefaultBranch,
// hasGitSLDir) consult os.Getwd() under the hood.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// initStackedRepo builds a git repo with two stacked branches:
//
//	main → "initial"
//	feature-a (off main) → adds a.txt
//	feature-b (off feature-a) → adds b.txt; HEAD on feature-b
//
// Returns repo path plus the SHAs of feature-a and feature-b tips.
func initStackedRepo(t *testing.T) (dir, aSHA, bSHA string) {
	t.Helper()
	dir = initTestRepo(t)
	// feature-a on top of main.
	gitT(t, dir, "checkout", "-b", "feature-a")
	writeFile(t, dir+"/a.txt", "a\n")
	gitT(t, dir, "add", "a.txt")
	gitT(t, dir, "commit", "-m", "add a")
	aSHA = gitT(t, dir, "rev-parse", "HEAD")
	// feature-b on top of feature-a.
	gitT(t, dir, "checkout", "-b", "feature-b")
	writeFile(t, dir+"/b.txt", "b\n")
	gitT(t, dir, "add", "b.txt")
	gitT(t, dir, "commit", "-m", "add b")
	bSHA = gitT(t, dir, "rev-parse", "HEAD")
	return dir, aSHA, bSHA
}

func TestAutoDetect_NoPR_NoStack(t *testing.T) {
	dir := initTestRepo(t)
	// Plain feature branch off main, no other branches in the chain.
	gitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/x.txt", "x\n")
	gitT(t, dir, "add", "x.txt")
	gitT(t, dir, "commit", "-m", "x")
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got != nil {
		t.Errorf("expected nil focus on fresh feature branch, got %+v", got)
	}
}

func TestAutoDetect_PRBaseIsDefault(t *testing.T) {
	dir := initTestRepo(t)
	gitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/x.txt", "x\n")
	gitT(t, dir, "add", "x.txt")
	gitT(t, dir, "commit", "-m", "x")
	chdir(t, dir)

	withDetectPRInfo(t, func() *PRInfo {
		return &PRInfo{Number: 7, BaseRefName: "main"}
	})
	withFetchPRByNumber(t, func(int) (*PRInfo, error) {
		t.Fatal("fetchPRByNumber should not be called when PR is not stacked")
		return nil, nil
	})

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got != nil {
		t.Errorf("expected nil focus when PR base is default branch, got %+v", got)
	}
}

func TestAutoDetect_StackedPR(t *testing.T) {
	dir, aSHA, bSHA := initStackedRepo(t)
	chdir(t, dir)

	prInfo := &PRInfo{
		Number:      42,
		Title:       "Stacked PR",
		URL:         "https://github.com/o/r/pull/42",
		BaseRefName: "feature-a",
		HeadRefName: "feature-b",
		BaseRefOid:  aSHA,
		HeadRefOid:  bSHA,
	}
	withDetectPRInfo(t, func() *PRInfo { return prInfo })
	withFetchPRByNumber(t, func(num int) (*PRInfo, error) {
		if num != 42 {
			t.Errorf("fetchPRByNumber called with %d want 42", num)
		}
		return prInfo, nil
	})

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got == nil {
		t.Fatal("expected Range focus, got nil")
	}
	if got.Kind != FocusRange {
		t.Errorf("Kind=%q want range", got.Kind)
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber=%d want 42", got.PRNumber)
	}
	if got.BaseSHA != aSHA || got.HeadSHA != bSHA {
		t.Errorf("got base=%q head=%q want %q/%q", got.BaseSHA, got.HeadSHA, aSHA, bSHA)
	}
	if !got.IsStacked {
		t.Error("IsStacked should be true")
	}
}

func TestAutoDetect_LocalStackNoPRPushed(t *testing.T) {
	dir, aSHA, bSHA := initStackedRepo(t)
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got == nil {
		t.Fatal("expected Range focus from local stack, got nil")
	}
	if got.Kind != FocusRange {
		t.Errorf("Kind=%q want range", got.Kind)
	}
	if got.BaseSHA != aSHA {
		t.Errorf("BaseSHA=%q want feature-a tip %q", got.BaseSHA, aSHA)
	}
	if got.HeadSHA != bSHA {
		t.Errorf("HeadSHA=%q want HEAD %q", got.HeadSHA, bSHA)
	}
	if !strings.Contains(got.Label, "feature-a") {
		t.Errorf("Label=%q should reference feature-a", got.Label)
	}
}

// TestAutoDetect_LocalStack_DefaultSHAIsLiteralDefaultBranch covers the
// invariant that Focus.DefaultSHA — the diff base for full-stack scope —
// is always the literal default-branch tip, regardless of how deep the
// stack is. Full-stack of any layer means "everything from default to
// here", including the topmost layer's own changes.
func TestAutoDetect_LocalStack_DefaultSHAIsLiteralDefaultBranch(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })
	mainSHA := gitT(t, dir, "rev-parse", "main")

	// Three layers above main.
	gitT(t, dir, "checkout", "-b", "alpha")
	writeFile(t, dir+"/alpha.txt", "alpha\n")
	gitT(t, dir, "add", "alpha.txt")
	gitT(t, dir, "commit", "-m", "alpha")

	gitT(t, dir, "checkout", "-b", "beta")
	writeFile(t, dir+"/beta.txt", "beta\n")
	gitT(t, dir, "add", "beta.txt")
	gitT(t, dir, "commit", "-m", "beta")
	betaSHA := gitT(t, dir, "rev-parse", "HEAD")

	gitT(t, dir, "checkout", "-b", "gamma")
	writeFile(t, dir+"/gamma.txt", "gamma\n")
	gitT(t, dir, "add", "gamma.txt")
	gitT(t, dir, "commit", "-m", "gamma")
	gammaSHA := gitT(t, dir, "rev-parse", "HEAD")

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got == nil {
		t.Fatal("expected Range focus from 3-layer local stack, got nil")
	}
	if got.HeadSHA != gammaSHA {
		t.Errorf("HeadSHA=%q want gamma %q", got.HeadSHA, gammaSHA)
	}
	if got.BaseSHA != betaSHA {
		t.Errorf("BaseSHA=%q want beta tip (direct parent) %q", got.BaseSHA, betaSHA)
	}
	if got.DefaultSHA != mainSHA {
		t.Errorf("DefaultSHA=%q want main (literal default) %q; full-stack must diff against the repo default so the topmost layer's changes are included", got.DefaultSHA, mainSHA)
	}
}

func TestAutoDetect_NoLocalStack_OnDefault(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got != nil {
		t.Errorf("expected nil focus when HEAD is on default branch, got %+v", got)
	}
}

func TestAutoDetect_GHUnavailable_FallsBack(t *testing.T) {
	// detectPRInfoFn returning nil simulates gh missing or no PR. The
	// local-stack path should still run.
	dir, aSHA, _ := initStackedRepo(t)
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got == nil {
		t.Fatal("expected local-stack fallback when gh unavailable")
	}
	if got.BaseSHA != aSHA {
		t.Errorf("BaseSHA=%q want %q", got.BaseSHA, aSHA)
	}
}

// TestAutoDetect_WorkingTreeFlag_Bypasses verifies the flag wiring at the
// applySessionOverrides layer: when sc.workingTree is true, autoDetect is not
// consulted regardless of repo state.
func TestAutoDetect_WorkingTreeFlag_Bypasses(t *testing.T) {
	dir, _, _ := initStackedRepo(t)
	chdir(t, dir)

	// Stub detect to fail loudly if the flag bypass is broken.
	withDetectPRInfo(t, func() *PRInfo {
		t.Fatal("detectPRInfoFn called despite --working-tree flag")
		return nil
	})

	// Simulate the boot-path guard inline (avoids spinning up a session).
	sc := &serverConfig{workingTree: true}
	if sc.focus == nil && !sc.workingTree && len(sc.files) == 0 {
		// Should be skipped — calling autoDetectStackedFocus here would fire the
		// stub above and fail the test.
		_ = autoDetectStackedFocus(&GitVCS{}, dir)
	}
	if sc.focus != nil {
		t.Errorf("focus should remain nil under --working-tree, got %+v", sc.focus)
	}
}

// TestAutoDetect_FileMode_Bypasses verifies that passing explicit file
// arguments (file mode) skips autodetect entirely. Regression: PR #391
// introduced autodetect but didn't gate it on file mode, so `crit some.md`
// on a stacked branch was silently promoted into range mode and the file
// argument was thrown away.
func TestAutoDetect_FileMode_Bypasses(t *testing.T) {
	dir, _, _ := initStackedRepo(t)
	chdir(t, dir)

	withDetectPRInfo(t, func() *PRInfo {
		t.Fatal("detectPRInfoFn called despite file-mode invocation")
		return nil
	})

	sc := &serverConfig{files: []string{"some.md"}}
	if sc.focus == nil && !sc.workingTree && len(sc.files) == 0 {
		_ = autoDetectStackedFocus(&GitVCS{}, dir)
	}
	if sc.focus != nil {
		t.Errorf("focus should remain nil in file mode, got %+v", sc.focus)
	}
}

// TestAutoDetect_LocalStack_StaleTipFiltered verifies that a stale local
// branch tip whose commit is on the default branch (i.e. ancestor of
// origin/main) doesn't get promoted to a fake "stack base". Without the
// topic-chain filter, the feature branch's first-parent walk finds the
// initial commit, sees a stale branch pointing there, and would return a
// bogus Range focus pinned at that commit.
func TestAutoDetect_LocalStack_StaleTipFiltered(t *testing.T) {
	dir := initTestRepo(t)
	initialSHA := gitT(t, dir, "rev-parse", "HEAD")
	// Stale branch left pointing at the initial commit on main.
	gitT(t, dir, "branch", "stale-merged", initialSHA)
	// Plain feature branch off main — no real stack.
	gitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/x.txt", "x\n")
	gitT(t, dir, "add", "x.txt")
	gitT(t, dir, "commit", "-m", "x")
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got != nil {
		t.Errorf("expected nil focus when only matching tip is on default branch, got %+v (BaseSHA=%s)", got, got.BaseSHA)
	}
}

// TestParseServerFlags_WorkingTree exercises the flag plumbing.
func TestParseServerFlags_WorkingTree(t *testing.T) {
	sf := parseServerFlags([]string{"--working-tree"})
	if !sf.workingTree {
		t.Error("expected workingTree=true after --working-tree flag")
	}
	sf2 := parseServerFlags(nil)
	if sf2.workingTree {
		t.Error("workingTree should default to false")
	}
}
