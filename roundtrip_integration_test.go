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

func TestRoundtrip_ReplyToRemoteComment(t *testing.T) {
	t.Skip("blocked on issue #442: crit push doesn't post replies to imported remote comments")
	e := newRoundtripEnv(t)

	rootID := e.postRemoteComment("sample.go", 19, "please address")
	e.runCrit("pull")

	locals := e.allLocalComments()
	if len(locals) != 1 {
		t.Fatalf("expected 1 local after pull, got %d", len(locals))
	}
	rootCommentID := locals[0].Comment.ID
	if rootCommentID == "" {
		t.Fatal("imported root has empty local ID")
	}

	e.runCrit("comment", "--reply-to", rootCommentID, "ack, will fix")

	remoteBefore := e.listRemoteComments()
	e.runCrit("push")
	remoteAfter := e.listRemoteComments()

	added := len(remoteAfter) - len(remoteBefore)
	if added != 1 {
		t.Fatalf("want 1 new remote item, got %d:\n%s",
			added, dumpRemote(remoteAfter))
	}

	var reply *remoteComment
	for i := range remoteAfter {
		r := &remoteAfter[i]
		if r.ID != rootID && r.InReplyTo == rootID {
			reply = r
			break
		}
	}
	if reply == nil {
		t.Fatalf("no reply with InReplyTo=%d found:\n%s",
			rootID, dumpRemote(remoteAfter))
	}

	// Second push — no further posts.
	e.runCrit("push")
	if got := len(e.listRemoteComments()); got != len(remoteAfter) {
		t.Errorf("second push changed remote count: %d -> %d",
			len(remoteAfter), got)
	}

	// Pull and assert reply has GitHubID locally.
	e.runCrit("pull")
	for _, lc := range e.allLocalComments() {
		for _, r := range lc.Comment.Replies {
			if r.GitHubID == 0 {
				t.Errorf("reply still has GitHubID=0 after push+pull: %+v", r)
			}
		}
	}
}

func TestRoundtrip_InterleavedReplies(t *testing.T) {
	t.Skip("blocked on issue #442: crit push skips local replies whose parent has a non-zero github_id (also reproduces when parent's github_id was assigned by our own push)")
	e := newRoundtripEnv(t)

	// Local root, push it.
	e.runCrit("comment", "sample.go:19", "what about edge case X?")
	e.runCrit("push")

	locals := e.allLocalComments()
	if len(locals) != 1 || locals[0].Comment.GitHubID == 0 {
		t.Fatalf("post-push state wrong: %+v", locals)
	}
	rootGHID := locals[0].Comment.GitHubID

	// Reviewer replies on GitHub.
	remoteReplyID := e.postRemoteReply(rootGHID, "good point, here's why")

	// User pulls, then replies locally.
	e.runCrit("pull")
	rootLocal := e.allLocalComments()[0].Comment
	if len(rootLocal.Replies) != 1 {
		t.Fatalf("after pull: want 1 reply, got %d", len(rootLocal.Replies))
	}
	if rootLocal.Replies[0].GitHubID != remoteReplyID {
		t.Errorf("imported reply ID mismatch: want %d got %d",
			remoteReplyID, rootLocal.Replies[0].GitHubID)
	}

	e.runCrit("comment", "--reply-to", rootLocal.ID, "got it, I'll fix")

	// Push.
	remoteBefore := e.listRemoteComments()
	e.runCrit("push")
	remoteAfter := e.listRemoteComments()
	if len(remoteAfter)-len(remoteBefore) != 1 {
		t.Fatalf("want 1 new remote, got delta %d:\n%s",
			len(remoteAfter)-len(remoteBefore), dumpRemote(remoteAfter))
	}

	// Final pull — three items (root + 2 replies), all with non-zero GitHubIDs.
	e.runCrit("pull")
	final := e.allLocalComments()
	if len(final) != 1 {
		t.Fatalf("want 1 root, got %d", len(final))
	}
	if got := len(final[0].Comment.Replies); got != 2 {
		t.Fatalf("want 2 replies, got %d", got)
	}
	for _, r := range final[0].Comment.Replies {
		if r.GitHubID == 0 {
			t.Errorf("reply missing GitHubID: %+v", r)
		}
	}
}

func TestRoundtrip_FreshClonePicksUpAllComments(t *testing.T) {
	a := newRoundtripEnv(t)

	// User A posts and pushes a comment.
	a.runCrit("comment", "sample.go:19", "from user A")
	a.runCrit("push")

	// A reviewer also drops a comment on the PR.
	reviewerID := a.postRemoteComment("sample.md", 12, "from reviewer")

	// User B clones the branch fresh and pulls.
	b := a.freshClone()
	b.runCrit("pull")

	got := b.allLocalComments()
	if len(got) != 2 {
		t.Fatalf("user B want 2 local, got %d:\n%+v", len(got), got)
	}

	ids := map[int64]bool{}
	for _, lc := range got {
		if lc.Comment.GitHubID == 0 {
			t.Errorf("user B has comment without GitHubID: %+v", lc)
		}
		ids[lc.Comment.GitHubID] = true
	}
	if !ids[reviewerID] {
		t.Errorf("user B did not import reviewer's comment id=%d", reviewerID)
	}

	// User B pushes a fresh comment — must not re-post the two existing.
	b.runCrit("comment", "sample.go:19", "from user B")
	remoteBefore := b.listRemoteComments()
	b.runCrit("push")
	remoteAfter := b.listRemoteComments()
	if delta := len(remoteAfter) - len(remoteBefore); delta != 1 {
		t.Fatalf("want exactly 1 new remote from B's push, got delta %d", delta)
	}
}

func TestRoundtrip_BranchSwitchPreservesState(t *testing.T) {
	a := newRoundtripEnv(t)
	b := newRoundtripEnv(t)

	a.runCrit("comment", "sample.go:19", "comment on A")
	a.runCrit("push")
	aIDsBefore := commentIDs(a.listRemoteComments())

	b.runCrit("comment", "sample.go:19", "comment on B")
	b.runCrit("push")

	// Re-pull on A — A's review file should be unchanged.
	a.runCrit("pull")
	aIDsAfter := commentIDs(a.listRemoteComments())
	if !sameIDs(aIDsBefore, aIDsAfter) {
		t.Errorf("A's remote comments changed: %v -> %v", aIDsBefore, aIDsAfter)
	}
	aLocals := a.allLocalComments()
	if len(aLocals) != 1 {
		t.Fatalf("A: want 1 local, got %d", len(aLocals))
	}
	if !strings.Contains(aLocals[0].Comment.Body, "comment on A") {
		t.Errorf("A's local comment body wrong: %q", aLocals[0].Comment.Body)
	}
}

func TestRoundtrip_ForcePushedHead_NoDuplication(t *testing.T) {
	e := newRoundtripEnv(t)

	// Add and push a local comment.
	e.runCrit("comment", "sample.go:19", "first round")
	e.runCrit("push")
	idsBefore := commentIDs(e.listRemoteComments())

	// Amend HEAD and force-push (simulates rebase / squash).
	if err := appendLine(e.workDir+"/sample.go", "// trailing comment\n"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, e.workDir, "git", "commit", "-am", "tweak")
	mustRun(t, e.workDir, "git", "push", "--force")

	// Pull — old comments still there, no duplicates.
	e.runCrit("pull")
	locals := e.allLocalComments()
	if len(locals) != 1 {
		t.Fatalf("want 1 local after force-push pull, got %d", len(locals))
	}
	idsAfter := commentIDs(e.listRemoteComments())
	if !sameIDs(idsBefore, idsAfter) {
		t.Errorf("remote IDs changed across force-push: %v -> %v", idsBefore, idsAfter)
	}

	// New local + push: only the new one is posted.
	e.runCrit("comment", "sample.go:19", "second round")
	remoteBefore := e.listRemoteComments()
	e.runCrit("push")
	if delta := len(e.listRemoteComments()) - len(remoteBefore); delta != 1 {
		t.Errorf("want delta 1, got %d", delta)
	}
}
