package vcs

import (
	"strings"
	"testing"
)

func TestTopicChainSHAs_Git_ExcludesDefaultTip(t *testing.T) {
	dir := InitTestRepo(t)
	mainSHA := GitRun(t, dir, "rev-parse", "HEAD")
	GitRun(t, dir, "checkout", "-b", "feat")
	topicSHA := CommitAtForTest(t, dir, "topic.txt", "topic", "topic commit")

	out := TopicChainSHAs(&GitVCS{}, dir)
	if out[mainSHA] {
		t.Errorf("topic chain should exclude default-branch tip %s; got %v", mainSHA, out)
	}
	if !out[topicSHA] {
		t.Errorf("topic chain should include %s; got %v", topicSHA, out)
	}
}

func TestTopicChainSHAs_NilVCS(t *testing.T) {
	out := TopicChainSHAs(nil, t.TempDir())
	if len(out) != 0 {
		t.Errorf("nil vcs should return empty map, got %v", out)
	}
}

func TestCommitSubjectFor_Git(t *testing.T) {
	dir := InitTestRepo(t)
	sha := CommitAtForTest(t, dir, "subj.txt", "x", "picker subject line")
	got := CommitSubjectFor(&GitVCS{}, dir, sha)
	if got != "picker subject line" {
		t.Errorf("CommitSubjectFor = %q, want %q", got, "picker subject line")
	}
}

func TestCommitSubjectFor_Git_TruncatesLongSubject(t *testing.T) {
	dir := InitTestRepo(t)
	long := strings.Repeat("a", 70)
	sha := CommitAtForTest(t, dir, "long.txt", "x", long)
	got := CommitSubjectFor(&GitVCS{}, dir, sha)
	if len([]rune(got)) != 61 { // 60 + ellipsis rune
		t.Fatalf("truncated subject len = %d, want 61; got %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "\u2026") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

func TestCommitSubjectFor_NilVCS(t *testing.T) {
	if got := CommitSubjectFor(nil, "", "abc"); got != "" {
		t.Errorf("nil vcs should return empty, got %q", got)
	}
}

func TestCommitSubjectFor_Git_UnknownSHA(t *testing.T) {
	dir := InitTestRepo(t)
	if got := CommitSubjectFor(&GitVCS{}, dir, "deadbeef"); got != "" {
		t.Errorf("unknown sha should return empty, got %q", got)
	}
}
