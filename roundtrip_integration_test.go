//go:build e2e_github

package main

import (
	"strconv"
	"strings"
	"testing"
)

// TestRoundtrip_PushIsIdempotent: a clean PR + local comments. After two
// pushes, GitHub should hold exactly N comments with stable IDs. This is
// the canonical "delete and recreate" regression test.
func TestRoundtrip_PushIsIdempotent(t *testing.T) {
	e := newRoundtripEnv(t)

	// Comment on added lines (must be within the PR diff for GitHub to accept).
	// Helper appends `func Mod` to sample.go (line ~19) and a new section to
	// sample.md (line ~12).
	e.runCrit("comment", "sample.go:19", "Comment on sample.go Mod func")
	e.runCrit("comment", "sample.md:12", "Comment on sample.md Section D")

	out1 := e.runCrit("push")
	t.Logf("push #1 output:\n%s", out1)

	remoteAfter1 := e.listRemoteComments()
	if len(remoteAfter1) != 2 {
		t.Fatalf("after push #1: want 2 remote comments, got %d:\n%s",
			len(remoteAfter1), dumpRemote(remoteAfter1))
	}

	out2 := e.runCrit("push")
	t.Logf("push #2 output:\n%s", out2)

	remoteAfter2 := e.listRemoteComments()
	if len(remoteAfter2) != 2 {
		t.Fatalf("after push #2: want 2 remote comments, got %d:\n%s",
			len(remoteAfter2), dumpRemote(remoteAfter2))
	}

	idsBefore := commentIDs(remoteAfter1)
	idsAfter := commentIDs(remoteAfter2)
	if !sameIDs(idsBefore, idsAfter) {
		t.Fatalf("comment IDs changed between pushes\nbefore: %v\nafter:  %v",
			idsBefore, idsAfter)
	}

	for _, lc := range e.allLocalComments() {
		if lc.Comment.GitHubID == 0 {
			t.Errorf("local comment on %s has GitHubID=0 after push:\n%+v",
				lc.Path, lc.Comment)
		}
	}
}

func commentIDs(rs []remoteComment) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[int64]bool, len(a))
	for _, id := range a {
		m[id] = true
	}
	for _, id := range b {
		if !m[id] {
			return false
		}
	}
	return true
}

func dumpRemote(rs []remoteComment) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString("  id=" + strconv.FormatInt(r.ID, 10))
		b.WriteString(" parent=" + strconv.FormatInt(r.InReplyTo, 10))
		b.WriteString(" path=" + r.Path)
		b.WriteString(" line=" + strconv.Itoa(r.Line))
		b.WriteString(" body=" + truncate(r.Body, 40))
		b.WriteByte('\n')
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func TestRoundtrip_PullIsIdempotent(t *testing.T) {
	e := newRoundtripEnv(t)

	// Reviewer posts on a diff line.
	id := e.postRemoteComment("sample.go", 19, "remote review comment")
	if id == 0 {
		t.Fatal("postRemoteComment returned 0")
	}

	// First pull imports it.
	e.runCrit("pull")
	first := e.allLocalComments()
	if len(first) != 1 {
		t.Fatalf("after pull #1: want 1 local comment, got %d", len(first))
	}
	if first[0].Comment.GitHubID != id {
		t.Errorf("GitHubID mismatch: want %d, got %d", id, first[0].Comment.GitHubID)
	}

	// Second pull must be a no-op.
	out := e.runCrit("pull")
	t.Logf("pull #2 output:\n%s", out)
	second := e.allLocalComments()
	if len(second) != 1 {
		t.Fatalf("after pull #2: want 1 local comment, got %d", len(second))
	}
	if second[0].Comment.GitHubID != id {
		t.Errorf("GitHubID drifted after second pull: want %d, got %d",
			id, second[0].Comment.GitHubID)
	}
}

func TestRoundtrip_PushThenPull_PreservesIDs(t *testing.T) {
	e := newRoundtripEnv(t)

	e.runCrit("comment", "sample.go:19", "local then pulled back")
	e.runCrit("push")

	afterPush := e.allLocalComments()
	if len(afterPush) != 1 {
		t.Fatalf("after push: want 1 local, got %d", len(afterPush))
	}
	id := afterPush[0].Comment.GitHubID
	if id == 0 {
		t.Fatal("local comment has GitHubID=0 after push")
	}

	e.runCrit("pull")
	afterPull := e.allLocalComments()
	if len(afterPull) != 1 {
		t.Fatalf("after pull: want 1 local, got %d", len(afterPull))
	}
	if afterPull[0].Comment.GitHubID != id {
		t.Errorf("GitHubID changed across pull: %d -> %d",
			id, afterPull[0].Comment.GitHubID)
	}

	remoteBefore := e.listRemoteComments()
	e.runCrit("push")
	remoteAfter := e.listRemoteComments()
	if len(remoteAfter) != len(remoteBefore) {
		t.Errorf("final push created %d new remote comments",
			len(remoteAfter)-len(remoteBefore))
	}
}
