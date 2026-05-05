package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunPushLive_AuthRotationMidPush regresses issue #452: when `gh`'s
// auth token rotates / expires mid-push, the very next gh-api call returns
// HTTP 401 and every subsequent one would fail the same way. The push
// loop must:
//
//  1. Detect the 401 specifically (not as a generic 4xx/5xx).
//  2. Stop iterating immediately — don't keep firing pointless calls.
//  3. Print a single clear stderr line ("Pushed K of N comments before
//     auth failed. Run 'gh auth refresh' ...") so the user knows exactly
//     what landed and how to recover.
//  4. Exit non-zero so scripts/CI catch the partial push.
//  5. Persist GitHubIDs only for the K successfully-posted items, so a
//     rerun (after `gh auth refresh`) only retries the (N-K) leftovers
//     and never duplicates already-posted replies on GitHub.
//
// Property (5) is the key safety guarantee — duplicates would be visible
// on GitHub and unrecoverable from `crit push` alone.
func TestRunPushLive_AuthRotationMidPush(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}

	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0\n"), 0644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	// Fake gh: call 1 → 201 with id 9001 (a clean reply post).
	// Call 2 → exit 4 with `gh: HTTP 401: Bad credentials` on stderr,
	//          mimicking the real gh CLI's auth-rotation surface.
	// Call 3+ → also 401 (don't care; the push must abort before reaching
	//           them — if the abort is broken, we'll see >2 invocations).
	fakeGH := filepath.Join(dir, "gh")
	script := `#!/bin/sh
COUNTER_FILE="` + counter + `"
n=$(cat "$COUNTER_FILE")
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
cat >/dev/null
case "$n" in
  1) echo '{"id": 9001}' ;;
  *) echo 'gh: HTTP 401: Bad credentials' >&2; exit 4 ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Build a review file with three new replies whose parents are already
	// on GitHub. No new top-level comments (Postable bucket empty), no
	// edits, no deletes — keeps the K-of-N math unambiguous: N=3 replies.
	critRoot := filepath.Join(dir, "review")
	if err := os.MkdirAll(critRoot, 0755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {
				Status: "modified",
				Comments: []Comment{
					{
						ID: "c1", StartLine: 1, EndLine: 1, Body: "parent 1",
						GitHubID: 100, LastPushedBodyHash: bodyHashAtPush("parent 1"),
						Replies: []Reply{{ID: "r1", Body: "first reply"}},
					},
					{
						ID: "c2", StartLine: 2, EndLine: 2, Body: "parent 2",
						GitHubID: 200, LastPushedBodyHash: bodyHashAtPush("parent 2"),
						Replies: []Reply{{ID: "r2", Body: "second reply"}},
					},
					{
						ID: "c3", StartLine: 3, EndLine: 3, Body: "parent 3",
						GitHubID: 300, LastPushedBodyHash: bodyHashAtPush("parent 3"),
						Replies: []Reply{{ID: "r3", Body: "third reply"}},
					},
				},
			},
		},
	}
	writeCritFile(t, critRoot, cj)

	ctx := pushContext{prNumber: 42, critPath: critRoot, cj: cj}

	// Capture stderr while runPushLive runs. The K-of-N recovery message
	// is part of the contract and is asserted below.
	stderr := captureStderr(t, func() {
		code := runPushLive(ctx, pushBuckets{})
		if code == 0 {
			t.Errorf("runPushLive exit code = 0; want non-zero on auth failure")
		}
	})

	if !strings.Contains(stderr, "Pushed 1 of 3") {
		t.Errorf("stderr missing 'Pushed 1 of 3'; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "gh auth refresh") {
		t.Errorf("stderr missing recovery hint 'gh auth refresh'; got:\n%s", stderr)
	}

	// The fake gh must have stopped after the 401 — i.e. exactly two
	// invocations. A higher number means the loop didn't abort and is
	// burning attempts (and stderr) on a token that won't recover.
	if got := readCounter(t, counter); got != 2 {
		t.Errorf("fake gh invocation count = %d; want 2 (1 success + 1 401, then abort)", got)
	}

	// Verify dedup state on disk:
	//   - reply r1 must have GitHubID 9001 (successfully posted).
	//   - replies r2 and r3 must still have GitHubID 0 so a retry posts
	//     only them and not duplicates of r1.
	after := readCritFile(t, critRoot)
	got := replyIDsByLocalID(after, "a.go")
	if got["r1"] != 9001 {
		t.Errorf("r1.GitHubID = %d, want 9001 (successful post)", got["r1"])
	}
	if got["r2"] != 0 {
		t.Errorf("r2.GitHubID = %d, want 0 (auth-failed, must retry)", got["r2"])
	}
	if got["r3"] != 0 {
		t.Errorf("r3.GitHubID = %d, want 0 (never attempted, must retry)", got["r3"])
	}

	// --- Second run: the user has refreshed their token. The fake gh now
	// returns 201 for everything. crit push must only retry r2 and r3 —
	// re-posting r1 would create a duplicate on GitHub. The dedup
	// guarantee comes from collectNewRepliesForPush skipping replies
	// whose GitHubID is non-zero; this leg is a regression test against
	// that contract weakening.
	if err := os.WriteFile(counter, []byte("0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	healthy := `#!/bin/sh
COUNTER_FILE="` + counter + `"
n=$(cat "$COUNTER_FILE")
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
cat >/dev/null
echo '{"id": '"$((9000 + n + 1))"'}'
`
	if err := os.WriteFile(fakeGH, []byte(healthy), 0755); err != nil {
		t.Fatal(err)
	}

	ctx2 := pushContext{prNumber: 42, critPath: critRoot, cj: after}
	_ = captureStderr(t, func() {
		if code := runPushLive(ctx2, pushBuckets{}); code != 0 {
			t.Errorf("second runPushLive exit code = %d; want 0", code)
		}
	})

	if got := readCounter(t, counter); got != 2 {
		t.Errorf("retry posted %d replies; want 2 (only r2 and r3)", got)
	}

	final := readCritFile(t, critRoot)
	finalIDs := replyIDsByLocalID(final, "a.go")
	if finalIDs["r1"] != 9001 {
		t.Errorf("r1.GitHubID changed to %d on retry; must remain 9001 (no re-post)", finalIDs["r1"])
	}
	if finalIDs["r2"] == 0 || finalIDs["r3"] == 0 {
		t.Errorf("retry left replies unposted: r2=%d r3=%d", finalIDs["r2"], finalIDs["r3"])
	}
}

func writeCritFile(t *testing.T, critRoot string, cj CritJSON) {
	t.Helper()
	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := reviewPathsFor(critRoot).Review
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		t.Fatalf("write review file: %v", err)
	}
}

func readCritFile(t *testing.T, critRoot string) CritJSON {
	t.Helper()
	data, err := os.ReadFile(reviewPathsFor(critRoot).Review)
	if err != nil {
		t.Fatalf("read review file: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal review file: %v", err)
	}
	return cj
}

func replyIDsByLocalID(cj CritJSON, path string) map[string]int64 {
	out := make(map[string]int64)
	for _, c := range cj.Files[path].Comments {
		for _, r := range c.Replies {
			out[r.ID] = r.GitHubID
		}
	}
	return out
}

func readCounter(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	s := strings.TrimSpace(string(data))
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
