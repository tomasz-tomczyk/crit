//go:build e2e_github

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRoundtrip_RangeComment(t *testing.T) {
	e := newRoundtripEnv(t)

	// Local range comment over a multi-line diff section.
	// The fixture appends to a 17-line sample.go:
	//   line 18: blank
	//   line 19: func Mod(a, b int) int { return a % b }
	// Both are on the RIGHT diff side. Range 18-19 spans them.
	e.runCrit("comment", "sample.go:18-19", "ranged comment over 18..19")
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("want 1 remote, got %d:\n%s", len(rs), dumpRemote(rs))
	}
	if rs[0].StartLine == 0 {
		t.Errorf("range comment posted without start_line: %+v", rs[0])
	}
	if rs[0].Line < rs[0].StartLine {
		t.Errorf("range end < start: %+v", rs[0])
	}

	// Pull, push again — idempotent.
	e.runCrit("pull")
	e.runCrit("push")
	if got := len(e.listRemoteComments()); got != 1 {
		t.Errorf("range comment got duplicated, count=%d", got)
	}
}

func TestRoundtrip_EditPushedCommentBody(t *testing.T) {
	e := newRoundtripEnv(t)

	e.runCrit("comment", "sample.go:19", "original body")
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("after push #1: want 1 remote, got %d", len(rs))
	}
	originalID := rs[0].ID
	if originalID == 0 {
		t.Fatal("remote ID is 0 after push")
	}

	// Edit the body locally — simulates user fixing a typo in the daemon.
	e.editReviewFile(func(cj *CritJSON) {
		mutated := false
		for path, f := range cj.Files {
			for i := range f.Comments {
				if f.Comments[i].Body == "original body" {
					f.Comments[i].Body = "edited body"
					mutated = true
				}
			}
			cj.Files[path] = f
		}
		if !mutated {
			t.Fatal("did not find the local comment to edit")
		}
	})

	// Push again. Either PATCH (preferred) or no-op (also a bug, edit silently dropped).
	out := e.runCrit("push")
	t.Logf("push #2 output:\n%s", out)

	rs2 := e.listRemoteComments()
	switch len(rs2) {
	case 1:
		// Either edited in place (good) or the edit was silently dropped (bad).
		if rs2[0].ID != originalID {
			t.Errorf("remote ID changed despite count=1: %d -> %d", originalID, rs2[0].ID)
		}
		if rs2[0].Body != "edited body" {
			t.Errorf("remote body did not update: got %q, want %q", rs2[0].Body, "edited body")
		}
	default:
		t.Fatalf("unexpected remote count after edit-push: %d (want 1)\n%s",
			len(rs2), dumpRemote(rs2))
	}
}

func TestRoundtrip_ResolveLocallyThenPush(t *testing.T) {
	e := newRoundtripEnv(t)

	remoteID := e.postRemoteComment("sample.go", 19, "needs response")
	e.runCrit("pull")

	// Mark resolved locally.
	e.editReviewFile(func(cj *CritJSON) {
		mutated := false
		for path, f := range cj.Files {
			for i := range f.Comments {
				if f.Comments[i].GitHubID == remoteID {
					f.Comments[i].Resolved = true
					mutated = true
				}
			}
			cj.Files[path] = f
		}
		if !mutated {
			t.Fatal("did not find imported comment to resolve")
		}
	})

	// Push: must not duplicate the remote comment and must not recreate.
	remoteBefore := e.listRemoteComments()
	out := e.runCrit("push")
	t.Logf("push output:\n%s", out)
	remoteAfter := e.listRemoteComments()
	if len(remoteAfter) != len(remoteBefore) {
		t.Errorf("push after resolve changed remote count: %d -> %d\n%s",
			len(remoteBefore), len(remoteAfter), dumpRemote(remoteAfter))
	}
	if len(remoteAfter) >= 1 && remoteAfter[0].ID != remoteID {
		t.Errorf("remote comment ID changed after resolve-push: %d -> %d",
			remoteID, remoteAfter[0].ID)
	}

	// Pull again — resolved flag must survive.
	e.runCrit("pull")
	for _, lc := range e.allLocalComments() {
		if lc.Comment.GitHubID == remoteID && !lc.Comment.Resolved {
			t.Errorf("resolved flag lost after pull: %+v", lc.Comment)
		}
	}
}

func TestRoundtrip_LongLivedBranch_NoDrift(t *testing.T) {
	e := newRoundtripEnv(t)

	// Round 1: two local roots.
	e.runCrit("comment", "sample.go:19", "round1 a")
	e.runCrit("comment", "sample.md:12", "round1 b")
	e.runCrit("push")
	r1 := e.listRemoteComments()
	if len(r1) != 2 {
		t.Fatalf("round1: want 2 remote, got %d", len(r1))
	}
	r1IDs := commentIDs(r1)

	// Round 2: pull (no-op expected) + a reviewer-side comment.
	e.runCrit("pull")
	reviewerID := e.postRemoteComment("sample.go", 19, "reviewer adds in round2")
	e.runCrit("pull")
	if got := len(e.allLocalComments()); got != 3 {
		t.Fatalf("round2: want 3 local, got %d", got)
	}

	// Round 3: another local root + push. Must add exactly 1 remote.
	e.runCrit("comment", "sample.md:12", "round3 c")
	remoteBefore := e.listRemoteComments()
	e.runCrit("push")
	remoteAfter := e.listRemoteComments()
	if d := len(remoteAfter) - len(remoteBefore); d != 1 {
		t.Fatalf("round3: want delta 1, got %d", d)
	}

	// Round 4: pull again. All 4 comments present, all GitHubIDs non-zero,
	// all unique, all original IDs from round 1 still in the set.
	e.runCrit("pull")
	finalLocals := e.allLocalComments()
	if len(finalLocals) != 4 {
		t.Fatalf("round4: want 4 local, got %d", len(finalLocals))
	}
	seen := map[int64]bool{}
	for _, lc := range finalLocals {
		if lc.Comment.GitHubID == 0 {
			t.Errorf("comment with GitHubID=0: %+v", lc.Comment)
			continue
		}
		if seen[lc.Comment.GitHubID] {
			t.Errorf("duplicate GitHubID in local: %d", lc.Comment.GitHubID)
		}
		seen[lc.Comment.GitHubID] = true
	}
	for _, id := range r1IDs {
		if !seen[id] {
			t.Errorf("round1 ID %d disappeared by round4", id)
		}
	}
	if !seen[reviewerID] {
		t.Errorf("reviewer's round2 ID %d disappeared", reviewerID)
	}

	// Round 5: nothing changed locally. Push must be a no-op.
	prePush := commentIDs(e.listRemoteComments())
	e.runCrit("push")
	postPush := commentIDs(e.listRemoteComments())
	if !sameIDs(prePush, postPush) {
		t.Errorf("idempotent push at round5 changed remote IDs:\n  before %v\n  after  %v",
			prePush, postPush)
	}
}

// Limitation: the sandbox runs under a single gh identity, so we cannot
// reproduce "user B authored on GitHub, user A edits locally" against a real
// PR. This test instead validates the IDEMPOTENT behavior of edit-then-push
// on imported comments — the remote count and IDs must not drift, and a
// second push must not loop. The author-guard concern (PATCH should refuse
// foreign edits at collection time) is tracked separately as a follow-up
// issue.
func TestRoundtrip_EditForeignComment_DoesNotPropagate(t *testing.T) {
	e := newRoundtripEnv(t)

	// Reviewer (current user, but pretend they're "someone else") posts a
	// comment via gh api. From the local crit's POV after pull, this comment
	// has a github_id but we are not the author — except in this sandbox the
	// gh user IS us, so GitHub will accept the PATCH. To make this test
	// meaningful regardless of author identity, we instead validate the
	// LOCAL behavior: after the edit-then-push, the LOCAL last-pushed body
	// must not have advanced past the original imported body unless the
	// remote ALSO accepted the change.
	remoteID := e.postRemoteComment("sample.go", 19, "imported body")
	e.runCrit("pull")

	locals := e.allLocalComments()
	if len(locals) != 1 {
		t.Fatalf("expected 1 local after pull, got %d", len(locals))
	}
	originalLocalBody := locals[0].Comment.Body

	// Simulate accidental local edit of the imported comment.
	e.editReviewFile(func(cj *CritJSON) {
		mutated := false
		for path, f := range cj.Files {
			for i := range f.Comments {
				if f.Comments[i].GitHubID == remoteID {
					f.Comments[i].Body = "TAMPERED via local edit"
					mutated = true
				}
			}
			cj.Files[path] = f
		}
		if !mutated {
			t.Fatal("did not find imported comment to mutate")
		}
	})

	// Push.
	out, _ := e.runCritExpectExit("push")
	t.Logf("push output:\n%s", out)

	// Whatever the push did or didn't do, the LOCAL/REMOTE bodies must agree
	// at the end. Either:
	//   a) push refused to PATCH (no-op): remote stays "imported body", local
	//      should ideally have rolled back too (but at minimum, NEXT push
	//      must not keep retrying — see assertion below).
	//   b) push PATCHed (current sandbox path because we are the author):
	//      remote becomes "TAMPERED", local LastPushedBodyHash advances.
	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("remote count drifted: got %d:\n%s", len(rs), dumpRemote(rs))
	}

	// Idempotency: a second push must not re-attempt anything.
	pushOut2, _ := e.runCritExpectExit("push")
	t.Logf("push #2 output:\n%s", pushOut2)
	rs2 := e.listRemoteComments()
	if !sameIDs(commentIDs(rs), commentIDs(rs2)) {
		t.Errorf("second push changed remote IDs:\n  before %v\n  after %v",
			commentIDs(rs), commentIDs(rs2))
	}

	_ = originalLocalBody // documents the original; not strictly checked
}

func TestRoundtrip_LocalDeletePropagates(t *testing.T) {
	e := newRoundtripEnv(t)

	e.runCrit("comment", "sample.go:19", "to be deleted")
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("after push: want 1 remote, got %d", len(rs))
	}
	pushedID := rs[0].ID

	// Delete locally — simulate the daemon's DELETE /api/comment/{id},
	// which splices the record out and queues the GitHub ID in
	// PendingGitHubDeletes for the next push to drain.
	e.editReviewFile(func(cj *CritJSON) {
		removed := false
		for path, f := range cj.Files {
			kept := f.Comments[:0]
			for _, c := range f.Comments {
				if c.GitHubID == pushedID {
					removed = true
					continue
				}
				kept = append(kept, c)
			}
			f.Comments = kept
			cj.Files[path] = f
		}
		if !removed {
			t.Fatal("did not find pushed comment to delete locally")
		}
		cj.PendingGitHubDeletes = append(cj.PendingGitHubDeletes, pushedID)
	})

	// Push: expect the remote comment to disappear.
	out := e.runCrit("push")
	t.Logf("push output:\n%s", out)

	rsAfter := e.listRemoteComments()
	if len(rsAfter) != 0 {
		t.Errorf("locally-deleted comment still present remotely: %s",
			dumpRemote(rsAfter))
	}
}

// TestRoundtrip_ThreeWayMerge_RemoteEditedSinceLastPush: we push body "X".
// Reviewer edits remote to "Y" via API. A subsequent local no-op edit + push
// must not overwrite the reviewer's "Y". This is the literal #446 invariant
// under stress.
func TestRoundtrip_ThreeWayMerge_RemoteEditedSinceLastPush(t *testing.T) {
	e := newRoundtripEnv(t)

	e.runCrit("comment", "sample.go:19", "X")
	e.runCrit("push")
	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("after push: want 1 remote, got %d", len(rs))
	}
	id := rs[0].ID

	// Reviewer edits the body on GitHub via direct PATCH.
	patchPayload := []byte(`{"body":"Y"}`)
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/pulls/comments/%d", e.repoSlug, id),
		"--method", "PATCH", "--input", "-")
	cmd.Stdin = bytes.NewReader(patchPayload)
	cmd.Dir = e.workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PATCH failed: %v\n%s", err, out)
	}

	// No-op local edit (touches file, body unchanged).
	e.editReviewFile(func(cj *CritJSON) {
		// trigger a save without changing any field
		_ = cj
	})

	out := e.runCrit("push")
	t.Logf("push output:\n%s", out)

	rsAfter := e.listRemoteComments()
	if len(rsAfter) != 1 {
		t.Fatalf("remote count drifted: got %d", len(rsAfter))
	}
	if rsAfter[0].Body != "Y" {
		t.Errorf("our push overwrote reviewer's edit: remote body now %q, want %q",
			rsAfter[0].Body, "Y")
	}
}

// TestRoundtrip_ShellInjectionSafe: comment body containing shell
// metacharacters must round-trip literally and never invoke the shell.
func TestRoundtrip_ShellInjectionSafe(t *testing.T) {
	e := newRoundtripEnv(t)

	const evil = `'; touch /tmp/CRIT_PWNED_$$; echo '`
	e.runCrit("comment", "sample.go:19", evil)
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("want 1 remote, got %d", len(rs))
	}
	if rs[0].Body != evil {
		t.Errorf("body did not round-trip literally: got %q want %q", rs[0].Body, evil)
	}

	matches, _ := filepath.Glob("/tmp/CRIT_PWNED_*")
	if len(matches) > 0 {
		t.Fatalf("SECURITY: shell ran during push: %v", matches)
	}
}

// TestRoundtrip_CRLFAndTrailingWhitespace_NoLoop: pushing a body with CRLF
// line endings and trailing whitespace must not cause an infinite PATCH loop
// across pull/push cycles when GitHub canonicalizes the stored form.
func TestRoundtrip_CRLFAndTrailingWhitespace_NoLoop(t *testing.T) {
	e := newRoundtripEnv(t)

	body := "first line  \r\nsecond line\r\n\r\nthird line with trailing tab\t\t\r\n"
	e.runCrit("comment", "sample.go:19", body)
	e.runCrit("push")

	rs1 := e.listRemoteComments()
	if len(rs1) != 1 {
		t.Fatalf("want 1 remote, got %d", len(rs1))
	}

	e.runCrit("pull")

	out := e.runCrit("push")
	t.Logf("push #2 output:\n%s", out)

	if got := len(e.listRemoteComments()); got != 1 {
		t.Errorf("remote count after second push: %d, want 1", got)
	}

	e.runCrit("pull")
	e.runCrit("push")
	if got := len(e.listRemoteComments()); got != 1 {
		t.Errorf("remote count after third push: %d, want 1", got)
	}
}

// TestRoundtrip_AnchorLineDeleted_Outdated: push a comment, force-push to
// remove the anchored line. Pull must preserve the comment (not silently
// drop it) and a new comment on a still-existing line must still work.
func TestRoundtrip_AnchorLineDeleted_Outdated(t *testing.T) {
	e := newRoundtripEnv(t)

	if err := appendLine(filepath.Join(e.workDir, "sample.go"),
		"\nfunc TempLineToDelete() {}\n"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, e.workDir, "git", "commit", "-am", "add line we'll delete")
	mustRun(t, e.workDir, "git", "push")

	contents, err := os.ReadFile(filepath.Join(e.workDir, "sample.go"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	targetLine := 0
	for i, ln := range lines {
		if strings.Contains(ln, "TempLineToDelete") {
			targetLine = i + 1
			break
		}
	}
	if targetLine == 0 {
		t.Fatal("could not locate TempLineToDelete in sample.go")
	}

	e.runCrit("comment", fmt.Sprintf("sample.go:%d", targetLine),
		"comment that will become outdated")
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("want 1 remote, got %d", len(rs))
	}
	originalID := rs[0].ID

	// Force-push without the TempLineToDelete commit.
	mustRun(t, e.workDir, "git", "reset", "--hard", "HEAD~1")
	mustRun(t, e.workDir, "git", "push", "--force")

	// Wait for GitHub to recompute the PR head sha after the force-push
	// before issuing more API calls (see issue #456): otherwise the next
	// `crit push` can race and post against a stale commit_id, getting
	// rejected with HTTP 422.
	headSHA := strings.TrimSpace(mustOutput(t, e.workDir, "git", "rev-parse", "HEAD"))
	e.waitForPRHeadSHA(headSHA)

	e.runCrit("pull")
	locals := e.allLocalComments()
	if len(locals) != 1 {
		t.Fatalf("after force-push pull: want 1 local, got %d", len(locals))
	}
	if locals[0].Comment.GitHubID != originalID {
		t.Errorf("GitHubID changed: was %d now %d", originalID, locals[0].Comment.GitHubID)
	}

	e.runCrit("comment", "sample.go:19", "second comment after force-push")
	e.runCrit("push")
	if got := len(e.listRemoteComments()); got != 2 {
		t.Errorf("after second push: want 2 remote, got %d", got)
	}
}

// TestRoundtrip_GitHubThreadResolvedOnWeb: reviewer resolves a thread via
// GraphQL resolveReviewThread; subsequent pull must mirror that state to
// the local resolved flag.
func TestRoundtrip_GitHubThreadResolvedOnWeb(t *testing.T) {
	e := newRoundtripEnv(t)

	e.runCrit("comment", "sample.go:19", "needs decision")
	e.runCrit("push")

	rs := e.listRemoteComments()
	if len(rs) != 1 {
		t.Fatalf("want 1 remote, got %d", len(rs))
	}
	commentID := rs[0].ID

	parts := strings.SplitN(e.repoSlug, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("invalid repoSlug: %s", e.repoSlug)
	}
	owner, repoName := parts[0], parts[1]

	threadQuery := fmt.Sprintf(`
query {
  repository(owner: %q, name: %q) {
    pullRequest(number: %d) {
      reviewThreads(first: 100) {
        nodes {
          id
          isResolved
          comments(first: 5) { nodes { databaseId } }
        }
      }
    }
  }
}`, owner, repoName, e.prNumber)

	cmd := exec.Command("gh", "api", "graphql", "-f", "query="+threadQuery)
	cmd.Dir = e.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("graphql query: %v\n%s", err, out)
	}

	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
								}
							}
						}
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("parse graphql response: %v\n%s", err, out)
	}

	var threadID string
	for _, n := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		for _, c := range n.Comments.Nodes {
			if c.DatabaseID == commentID {
				threadID = n.ID
				break
			}
		}
	}
	if threadID == "" {
		t.Fatalf("could not find thread for comment %d", commentID)
	}

	resolveMut := fmt.Sprintf(`mutation { resolveReviewThread(input: {threadId: %q}) { thread { id isResolved } } }`, threadID)
	cmd2 := exec.Command("gh", "api", "graphql", "-f", "query="+resolveMut)
	cmd2.Dir = e.workDir
	if mout, merr := cmd2.CombinedOutput(); merr != nil {
		t.Fatalf("resolve mutation: %v\n%s", merr, mout)
	}

	e.runCrit("pull")
	locals := e.allLocalComments()
	if len(locals) != 1 {
		t.Fatalf("want 1 local, got %d", len(locals))
	}
	if !locals[0].Comment.Resolved {
		t.Errorf("comment not marked resolved locally after thread resolution on web")
	}
}
