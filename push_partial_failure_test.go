package main

import (
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
