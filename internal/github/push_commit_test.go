package github

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// TestRunPushPostReview_CommitID checks that --commit reaches the
// create-review request as commit_id, and that without it the key is absent.
func TestRunPushPostReview_CommitID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}
	sha := strings.Repeat("b", 40)
	tests := []struct {
		name      string
		commitSHA string
	}{
		{name: "pinned", commitSHA: sha},
		{name: "not pinned", commitSHA: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			payloadFile := filepath.Join(dir, "payload.json")
			// The first gh call creates the review (stdin = payload); the
			// second lists its comments.
			script := `#!/bin/sh
if [ ! -f "` + payloadFile + `" ]; then
  cat > "` + payloadFile + `"
  echo '{"id": 5}'
else
  cat >/dev/null
  echo '[]'
fi
`
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			b := pushBuckets{Postable: []scopedComment{{
				Path:    "a.go",
				Comment: session.Comment{ID: "c1", StartLine: 3, EndLine: 3, Body: "fix"},
			}}}
			ctx := pushContext{prNumber: 7, event: "APPROVE", commitSHA: tt.commitSHA}
			posted, failed, _, _ := runPushPostReview(ctx, b, nil)
			if failed || posted != 1 {
				t.Fatalf("runPushPostReview = (%d, failed=%v), want (1, false)", posted, failed)
			}
			data, err := os.ReadFile(payloadFile)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("unmarshal payload %q: %v", data, err)
			}
			got, ok := payload["commit_id"]
			if tt.commitSHA == "" {
				if ok {
					t.Fatalf("payload has commit_id %v without --commit", got)
				}
				return
			}
			if got != tt.commitSHA {
				t.Fatalf("commit_id = %v, want %q", got, tt.commitSHA)
			}
		})
	}
}

var (
	ghPinnedSHA = strings.Repeat("1", 40)
	ghOtherSHA  = strings.Repeat("2", 40)
)

// installPushFakeGH fakes gh for RunPush: the PR's commits are prCommits,
// a create-review request body is saved to the returned payload path, and
// every call is logged to the returned log path.
func installPushFakeGH(t *testing.T, prCommits ...string) (log, payload string) {
	t.Helper()
	payload = filepath.Join(t.TempDir(), "payload.json")
	log = installViewedFakeGH(t, `case "$*" in
  "auth status"*) exit 0 ;;
  *pulls/7/commits*) printf '%s\n' `+strings.Join(prCommits, " ")+` ;;
  *"pulls/7/reviews --method POST"*) cat > '`+payload+`'; echo '{"id": 5}' ;;
  *reviews/5/comments*) echo '[{"id": 901}]' ;;
  *) echo '{}' ;;
esac`)
	return log, payload
}

// savePushTestReview saves cj as the review in a fresh, non-git working
// directory (so full SHAs pass through vcs.ResolveCommitSHA) and returns
// the --output dir.
func savePushTestReview(t *testing.T, cj session.CritJSON) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	critPath, err := review.ResolveCommandReviewPathWithSession("", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	return dir
}

// pinnedRangeReview is a range-mode review with one comment written against
// the pinned commit, one against another head, and one already-pushed
// comment whose body was edited locally (so a live push would PATCH it).
func pinnedRangeReview() session.CritJSON {
	return session.CritJSON{ReviewRound: 1, ActiveDiffScope: string(session.DiffScopeLayer), Files: map[string]session.CritJSONFile{
		"a.go": {Status: "modified", Comments: []session.Comment{
			{ID: "c_pin", StartLine: 3, EndLine: 3, Body: "on pinned", HeadSHA: ghPinnedSHA, DiffScope: string(session.DiffScopeLayer)},
			{ID: "c_other", StartLine: 5, EndLine: 5, Body: "on other", HeadSHA: ghOtherSHA, DiffScope: string(session.DiffScopeLayer)},
			{ID: "c_edit", StartLine: 7, EndLine: 7, Body: "edited", GitHubID: 300, LastPushedBodyHash: bodyHashAtPush("original")},
		}},
	}}
}

func readPushPayload(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no review was created: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload %q: %v", data, err)
	}
	return payload
}

func TestRunPush_CommitPinsReviewAndBuckets(t *testing.T) {
	log, payloadPath := installPushFakeGH(t, ghOtherSHA, ghPinnedSHA)
	dir := savePushTestReview(t, pinnedRangeReview())
	if err := RunPush([]string{"--commit", ghPinnedSHA, "--output", dir, "7"}); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "pulls/7/commits") {
		t.Fatalf("pin check did not run:\n%s", calls)
	}
	payload := readPushPayload(t, payloadPath)
	if payload["commit_id"] != ghPinnedSHA {
		t.Fatalf("commit_id = %v, want %s", payload["commit_id"], ghPinnedSHA)
	}
	comments, _ := payload["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("posted %d comments, want only the pinned one: %v", len(comments), comments)
	}
	if body, _ := comments[0].(map[string]any)["body"].(string); !strings.Contains(body, "on pinned") {
		t.Fatalf("posted comment body = %q, want the pinned comment", body)
	}
}

func TestRunPush_CommitNotInPRPostsNothing(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "live"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			log, payloadPath := installPushFakeGH(t, ghOtherSHA)
			dir := savePushTestReview(t, pinnedRangeReview())
			args := []string{"--commit", ghPinnedSHA, "--event", "approve", "--output", dir, "7"}
			if dryRun {
				args = append([]string{"--dry-run"}, args...)
			}
			err := RunPush(args)
			if err == nil || !strings.Contains(err.Error(), "commit 1111111 is not part of PR #7; nothing was posted") {
				t.Fatalf("error = %v", err)
			}
			calls, _ := os.ReadFile(log)
			if !strings.Contains(string(calls), "pulls/7/commits") {
				t.Fatalf("pin check did not run:\n%s", calls)
			}
			if strings.Contains(string(calls), "--method") {
				t.Fatalf("push wrote to GitHub despite a bad pin:\n%s", calls)
			}
			if _, err := os.Stat(payloadPath); err == nil {
				t.Fatal("a review was created despite a bad pin")
			}
		})
	}
}

func TestRunPush_CommitDryRunStatesPin(t *testing.T) {
	log, _ := installPushFakeGH(t, ghPinnedSHA)
	dir := savePushTestReview(t, pinnedRangeReview())
	out := captureStdout(t, func() {
		if err := RunPush([]string{"--dry-run", "--commit", ghPinnedSHA, "--output", dir, "7"}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "Review pinned to commit "+ghPinnedSHA) {
		t.Fatalf("dry-run output missing pin:\n%s", out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "pulls/7/commits") || strings.Contains(string(calls), "--method") {
		t.Fatalf("dry-run calls:\n%s", calls)
	}
}

// A verdict or -m body is submitted even when there are no inline comments.
func TestRunPush_VerdictWithoutComments(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantEvent string
		wantBody  string
		wantOut   string
	}{
		{name: "approve", args: []string{"--event", "approve"}, wantEvent: "APPROVE", wantOut: "Submitted approval."},
		{name: "request changes", args: []string{"--event", "request-changes", "-m", "fix it"}, wantEvent: "REQUEST_CHANGES", wantBody: "fix it", wantOut: "Submitted change request."},
		{name: "message only", args: []string{"-m", "round 2"}, wantEvent: "COMMENT", wantBody: "round 2", wantOut: "Submitted review message."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, payloadPath := installPushFakeGH(t)
			dir := savePushTestReview(t, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}})
			out := captureStdout(t, func() {
				if err := RunPush(append(tt.args, "--output", dir, "7")); err != nil {
					t.Error(err)
				}
			})
			if !strings.Contains(out, tt.wantOut) {
				t.Fatalf("stdout = %q, want %q", out, tt.wantOut)
			}
			payload := readPushPayload(t, payloadPath)
			if payload["event"] != tt.wantEvent || payload["body"] != tt.wantBody {
				t.Fatalf("payload = %v", payload)
			}
			if _, ok := payload["commit_id"]; ok {
				t.Fatalf("commit_id sent without --commit: %v", payload)
			}
		})
	}
}

func TestRunPush_VerdictWithoutCommentsDryRun(t *testing.T) {
	log, _ := installPushFakeGH(t)
	dir := savePushTestReview(t, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}})
	out := captureStdout(t, func() {
		if err := RunPush([]string{"--dry-run", "--event", "approve", "--output", dir, "7"}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "Would submit approval with no inline comments.") {
		t.Fatalf("dry-run output:\n%s", out)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "--method") {
		t.Fatalf("dry-run wrote to GitHub:\n%s", calls)
	}
}

func TestPrintPushSummary_Verdict(t *testing.T) {
	tests := []struct {
		verdict string
		posted  int
		want    string
	}{
		{verdict: "", posted: 0, want: "No comments to push.\n"},
		{verdict: "", posted: 2, want: "Posted 2 comments.\n"},
		{verdict: "Submitted approval", posted: 0, want: "Submitted approval.\n"},
		{verdict: "Submitted approval", posted: 2, want: "Submitted approval, posted 2 comments.\n"},
	}
	for _, tt := range tests {
		got := captureStdout(t, func() { printPushSummary(tt.verdict, tt.posted, 0, 0, 0, "") })
		if got != tt.want {
			t.Errorf("printPushSummary(%q, %d) = %q, want %q", tt.verdict, tt.posted, got, tt.want)
		}
	}
}

// A rejected review fails the push when --commit is set, even though the
// edit PATCH succeeded (which alone would count as a successful push).
func TestRunPush_CommitReviewRejectedExitsNonZero(t *testing.T) {
	installViewedFakeGH(t, `case "$*" in
  "auth status"*) exit 0 ;;
  *pulls/7/commits*) echo '`+ghPinnedSHA+`' ;;
  *"pulls/7/reviews --method POST"*) cat >/dev/null; echo 'gh: Unprocessable Entity (HTTP 422)' >&2; exit 1 ;;
  *) cat >/dev/null; echo '{}' ;;
esac`)
	dir := savePushTestReview(t, pinnedRangeReview())
	var err error
	captureStderr(t, func() {
		err = RunPush([]string{"--commit", ghPinnedSHA, "--output", dir, "7"})
	})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("error = %v, want exit 1", err)
	}
}
