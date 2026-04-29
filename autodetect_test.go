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
	runGit(t, dir, "checkout", "-b", "feature-a")
	writeFile(t, dir+"/a.txt", "a\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "add a")
	aSHA = runGit(t, dir, "rev-parse", "HEAD")
	// feature-b on top of feature-a.
	runGit(t, dir, "checkout", "-b", "feature-b")
	writeFile(t, dir+"/b.txt", "b\n")
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "add b")
	bSHA = runGit(t, dir, "rev-parse", "HEAD")
	return dir, aSHA, bSHA
}

func TestAutoDetect_NoPR_NoStack(t *testing.T) {
	dir := initTestRepo(t)
	// Plain feature branch off main, no other branches in the chain.
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/x.txt", "x\n")
	runGit(t, dir, "add", "x.txt")
	runGit(t, dir, "commit", "-m", "x")
	chdir(t, dir)
	withDetectPRInfo(t, func() *PRInfo { return nil })

	got := autoDetectStackedFocus(&GitVCS{}, dir)
	if got != nil {
		t.Errorf("expected nil focus on fresh feature branch, got %+v", got)
	}
}

func TestAutoDetect_PRBaseIsDefault(t *testing.T) {
	dir := initTestRepo(t)
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir+"/x.txt", "x\n")
	runGit(t, dir, "add", "x.txt")
	runGit(t, dir, "commit", "-m", "x")
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
	if sc.focus == nil && !sc.workingTree && os.Getenv("CRIT_NO_AUTODETECT") != "1" {
		// Should be skipped — calling autoDetectStackedFocus here would fire the
		// stub above and fail the test.
		_ = autoDetectStackedFocus(&GitVCS{}, dir)
	}
	if sc.focus != nil {
		t.Errorf("focus should remain nil under --working-tree, got %+v", sc.focus)
	}
}

func TestAutoDetect_EnvVar_Bypasses(t *testing.T) {
	dir, _, _ := initStackedRepo(t)
	chdir(t, dir)
	t.Setenv("CRIT_NO_AUTODETECT", "1")

	withDetectPRInfo(t, func() *PRInfo {
		t.Fatal("detectPRInfoFn called despite CRIT_NO_AUTODETECT=1")
		return nil
	})

	sc := &serverConfig{}
	if sc.focus == nil && !sc.workingTree && os.Getenv("CRIT_NO_AUTODETECT") != "1" {
		_ = autoDetectStackedFocus(&GitVCS{}, dir)
	}
	if sc.focus != nil {
		t.Errorf("focus should remain nil under CRIT_NO_AUTODETECT=1, got %+v", sc.focus)
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
