package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPostPushReplies_PartialFailure simulates a `gh api` POST that
// succeeds for the first reply, fails (HTTP 500) for the second, and
// succeeds for the third. Probes gap #2 (partial push failure):
//
//   - Successfully posted replies must have their GitHubID captured in the
//     returned replyIDs map (so the on-disk update step can persist them).
//   - Failed replies must NOT appear in replyIDs (so the next push can
//     retry only the failed one — re-posting all three would create
//     duplicates on GitHub).
//
// The test wires a fake `gh` binary onto PATH that scripts its responses
// from a counter file. This is the cleanest mock seam for a function
// that calls `exec.Command("gh", ...)` directly.
func TestPostPushReplies_PartialFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}

	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0\n"), 0644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	// Fake gh: increments a counter on each invocation. Returns a JSON id
	// for calls 1 and 3; exits 1 with a 500-ish error on call 2. Reads
	// stdin (the payload) but ignores it — postGHReply only cares about
	// the response body and exit code.
	fakeGH := filepath.Join(dir, "gh")
	script := `#!/bin/sh
COUNTER_FILE="` + counter + `"
n=$(cat "$COUNTER_FILE")
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
# Drain stdin so the caller's pipe doesn't block.
cat >/dev/null
case "$n" in
  1) echo '{"id": 1001}' ;;
  2) echo "HTTP 500: server error" >&2; exit 1 ;;
  3) echo '{"id": 1003}' ;;
  *) echo '{"id": 9999}' ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)

	replies := []ghReplyForPush{
		{ParentGHID: 100, Body: "first reply"},
		{ParentGHID: 200, Body: "second reply (will fail)"},
		{ParentGHID: 300, Body: "third reply"},
	}

	got := postPushReplies(42, replies)

	// First reply: parent 100 → id 1001.
	k1 := replyKey{ParentGHID: 100, BodyPrefix: truncateStr("first reply", 60)}
	if id, ok := got[k1]; !ok {
		t.Errorf("missing id for reply 1; map=%v", got)
	} else if id != 1001 {
		t.Errorf("reply 1 id = %d, want 1001", id)
	}

	// Second reply: parent 200 must NOT be in the map (the call failed).
	// If it leaks in here, a retry would post a duplicate to GitHub.
	k2 := replyKey{ParentGHID: 200, BodyPrefix: truncateStr("second reply (will fail)", 60)}
	if id, ok := got[k2]; ok {
		t.Errorf("failed reply leaked into replyIDs: parent=200 id=%d", id)
	}

	// Third reply: parent 300 → id 1003. Critical: a failure mid-batch
	// must not abort the rest of the batch. If postPushReplies gives up
	// after the 500, this assertion fails.
	k3 := replyKey{ParentGHID: 300, BodyPrefix: truncateStr("third reply", 60)}
	if id, ok := got[k3]; !ok {
		t.Errorf("missing id for reply 3 — partial failure aborted the batch; map=%v", got)
	} else if id != 1003 {
		t.Errorf("reply 3 id = %d, want 1003", id)
	}

	// Sanity: exactly two successful entries.
	if len(got) != 2 {
		t.Errorf("len(replyIDs) = %d, want 2; map=%v", len(got), got)
	}
}

// TestPushDeletedComments_PartialFailure regresses BLOCKER #2 of issue #449:
// when DELETE succeeds for one queued ID and fails (HTTP 500) for another,
// the successful ID must be drained from PendingGitHubDeletes on disk and
// the failed ID must remain queued for the next push. Combined with the
// pushShouldExitFailure policy, a partial-success delete must NOT cause
// `crit push` to exit non-zero when other work (posts, patches, drains)
// succeeded.
func TestPushDeletedComments_PartialFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}

	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0\n"), 0644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	// Fake gh emulating `gh api ... --method DELETE --include`. Call 1
	// returns HTTP 204 (drained). Call 2 returns HTTP 500 + non-zero exit
	// (failure). gh's --include writes the status line to stdout.
	fakeGH := filepath.Join(dir, "gh")
	script := `#!/bin/sh
COUNTER_FILE="` + counter + `"
n=$(cat "$COUNTER_FILE")
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
cat >/dev/null
case "$n" in
  1) printf 'HTTP/2 204\n\n' ;;
  2) printf 'HTTP/2 500\n\nserver error\n' >&2; printf 'HTTP/2 500\n\n'; exit 1 ;;
  *) printf 'HTTP/2 204\n\n' ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Set up a review file with two queued GitHub deletes.
	critRoot := filepath.Join(dir, "review")
	if err := os.MkdirAll(critRoot, 0755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		Files:                map[string]CritJSONFile{},
		PendingGitHubDeletes: []int64{1001, 1002},
	}
	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPathsFor(critRoot).Review, out, 0644); err != nil {
		t.Fatal(err)
	}

	ctx := pushContext{critPath: critRoot, cj: cj}
	drained, failed := pushDeletedComments(ctx)
	if drained != 1 {
		t.Errorf("drained = %d, want 1", drained)
	}
	if !failed {
		t.Error("failed = false, want true (one DELETE returned 500)")
	}

	// On-disk queue should now contain exactly the failed ID.
	data, err := os.ReadFile(reviewPathsFor(critRoot).Review)
	if err != nil {
		t.Fatal(err)
	}
	var after CritJSON
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	if len(after.PendingGitHubDeletes) != 1 || after.PendingGitHubDeletes[0] != 1002 {
		t.Errorf("PendingGitHubDeletes after = %v, want [1002]", after.PendingGitHubDeletes)
	}

	// Exit-code policy: a partial delete failure with at least one drain
	// (or any post / patch / export) must NOT exit 1.
	if pushShouldExitFailure(0, 0, drained, "", false, failed) {
		t.Error("pushShouldExitFailure = true; want false when a drain succeeded")
	}
	// Successful posts must also rescue an all-failed delete batch.
	if pushShouldExitFailure(3, 0, 0, "", false, true) {
		t.Error("pushShouldExitFailure = true; want false when posts succeeded")
	}
	// True total failure: nothing succeeded, something failed → exit 1.
	if !pushShouldExitFailure(0, 0, 0, "", false, true) {
		t.Error("pushShouldExitFailure = false; want true when nothing succeeded and a delete failed")
	}
}
