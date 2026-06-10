package github

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestPrintPushSummary_NoWork(t *testing.T) {
	out := captureStdout(t, func() {
		printPushSummary(0, 0, 0, 0, "")
	})
	if !strings.Contains(out, "No comments to push") {
		t.Errorf("got %q", out)
	}
}

func TestPrintPushSummary_WithExport(t *testing.T) {
	out := captureStdout(t, func() {
		printPushSummary(2, 1, 1, 3, "/tmp/orphans.md")
	})
	for _, want := range []string{"Posted 2 comments", "edited 1", "deleted 1", "/tmp/orphans.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestCountNewReplies(t *testing.T) {
	cj := session.CritJSON{
		Files: map[string]session.CritJSONFile{
			"a.go": {
				Comments: []session.Comment{
					{ID: "c1", GitHubID: 10, Replies: []session.Reply{{ID: "r1", Body: "new"}}},
					{ID: "c2", GitHubID: 0, Replies: []session.Reply{{ID: "r2", Body: "skip"}}},
				},
			},
		},
	}
	if n := countNewReplies(cj); n != 1 {
		t.Errorf("countNewReplies = %d, want 1", n)
	}
}

func TestWritePushOrphanExport_EmptyBuckets(t *testing.T) {
	path := writePushOrphanExport(pushContext{prNumber: 1}, PushBuckets{})
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestRunPushPostReview_EmptyPostable(t *testing.T) {
	posted, failed, authFailed, ids := runPushPostReview(pushContext{prNumber: 1}, PushBuckets{}, nil)
	if posted != 0 || failed || authFailed || ids != nil {
		t.Errorf("got posted=%d failed=%v auth=%v ids=%v", posted, failed, authFailed, ids)
	}
}

func TestPushEditedBodies_NoEdits(t *testing.T) {
	n, auth := pushEditedBodies(pushContext{cj: session.CritJSON{Files: map[string]session.CritJSONFile{}}})
	if n != 0 || auth {
		t.Errorf("got n=%d auth=%v", n, auth)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
