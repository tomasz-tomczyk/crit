package vcs

import (
	"testing"
)

func TestAddLabelLines_OverwriteAndSkip(t *testing.T) {
	result := make(map[string]string)
	addLabelLinesForTest(result, "abc123 feat-x\ndef456 feat-y", true)
	if result["abc123"] != "feat-x" {
		t.Errorf("abc123 = %q, want feat-x", result["abc123"])
	}
	if result["def456"] != "feat-y" {
		t.Errorf("def456 = %q, want feat-y", result["def456"])
	}

	addLabelLinesForTest(result, "abc123 newer-name", false)
	if result["abc123"] != "feat-x" {
		t.Errorf("overwrite=false should keep first label, got %q", result["abc123"])
	}

	addLabelLinesForTest(result, "abc123 replaced", true)
	if result["abc123"] != "replaced" {
		t.Errorf("overwrite=true should replace label, got %q", result["abc123"])
	}
}

func TestAddLabelLines_SkipsMalformedLines(t *testing.T) {
	result := make(map[string]string)
	addLabelLinesForTest(result, "nospaces\nonlyonetoken\n  \n", true)
	if len(result) != 0 {
		t.Errorf("malformed lines should be skipped, got %v", result)
	}
}

func TestRemoteBranchTips_NilVCS(t *testing.T) {
	got, err := RemoteBranchTips(nil, "", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("nil vcs should return nil slice, got %v", got)
	}
}

func TestRemoteBranchTips_Git_ExcludesDefaultAndHEAD(t *testing.T) {
	dir := InitTestRepo(t)
	headSHA := GitRun(t, dir, "rev-parse", "HEAD")
	GitRun(t, dir, "update-ref", "refs/remotes/origin/feat-x", headSHA)
	GitRun(t, dir, "update-ref", "refs/remotes/origin/main", headSHA)
	GitRun(t, dir, "update-ref", "refs/remotes/origin/HEAD", headSHA)

	branches, err := RemoteBranchTips(&GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "origin/main" || b.Name == "main" {
			t.Errorf("default branch leaked: %+v", b)
		}
		if b.Name == "origin/HEAD" {
			t.Errorf("HEAD pointer leaked: %+v", b)
		}
	}
	found := false
	for _, b := range branches {
		if b.Name == "origin/feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("origin/feat-x not in tips: %+v", branches)
	}
}

func TestLocalBranchTips_NilVCS(t *testing.T) {
	got, err := LocalBranchTips(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("nil vcs should return nil map, got %v", got)
	}
}

func TestWalkAncestors_NilVCS(t *testing.T) {
	got, err := WalkAncestors(nil, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("nil vcs should return nil slice, got %v", got)
	}
}
