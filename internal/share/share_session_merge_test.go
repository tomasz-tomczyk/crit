package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// newMergeSession returns a files-mode session whose review folder is
// <dir>/.crit and holds a saved review.json.
func newMergeSession(t *testing.T) (*session.Session, string) {
	t.Helper()
	dir := t.TempDir()
	abs := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(abs, []byte("# Plan\n\nline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	critPath := filepath.Join(dir, ".crit")
	if err := session.SaveCritJSON(critPath, session.CritJSON{Files: map[string]session.CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{Mode: "files", OutputDir: dir, RepoRoot: dir, Files: []*session.FileEntry{{Path: "plan.md", AbsPath: abs}}}
	t.Cleanup(sess.QuiesceForTest)
	return sess, critPath
}

func loadReview(t *testing.T, critPath string) session.CritJSON {
	t.Helper()
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	return cj
}

func bodies(comments []session.Comment) map[string]bool {
	out := make(map[string]bool, len(comments))
	for _, c := range comments {
		out[c.Body] = true
	}
	return out
}

// A merge by another process lands between two daemon saves. The second save
// must keep the pulled review-level comment and the pulled reply, and the
// watcher must then load them into memory.
func TestDaemonSaveKeepsMergedReviewCommentAndReply(t *testing.T) {
	sess, critPath := newMergeSession(t)
	local, _ := sess.AddComment("plan.md", 1, 1, "", "local", "", "", "")
	sess.AddReviewComment("local review", "", "")
	sess.WriteFiles()

	if err := MergeWebComments(critPath, []WebComment{{Body: "remote review", Scope: "review"}},
		map[string][]WebReply{local.ID: {{Body: "remote reply"}}}); err != nil {
		t.Fatal(err)
	}
	sess.AddReviewComment("typed during the merge", "", "")
	sess.WriteFiles()

	cj := loadReview(t, critPath)
	if got := bodies(cj.ReviewComments); !got["remote review"] || !got["local review"] || !got["typed during the merge"] {
		t.Fatalf("review comments on disk = %v", got)
	}
	if replies := cj.Files["plan.md"].Comments[0].Replies; len(replies) != 1 || replies[0].Body != "remote reply" {
		t.Fatalf("replies on disk = %+v", replies)
	}

	if !sess.MergeExternalCritJSONForTest() {
		t.Fatal("watcher did not load the merged comments")
	}
	if got := bodies(sess.GetReviewComments()); !got["remote review"] {
		t.Fatalf("review comments in memory = %v", got)
	}
	if replies := sess.GetComments("plan.md")[0].Replies; len(replies) != 1 {
		t.Fatalf("replies in memory = %+v", replies)
	}
}

// A comment added just before a hosted merge is only in memory. Reloading
// after the merge must keep it.
func TestSyncCommentsFromDiskKeepsUnsavedComment(t *testing.T) {
	sess, critPath := newMergeSession(t)
	sess.AddComment("plan.md", 1, 1, "", "local", "", "", "")
	sess.QuiesceForTest() // keep the comment unsaved: no debounced save
	if err := MergeWebComments(critPath, []WebComment{{Body: "remote", FilePath: "plan.md", StartLine: 1, EndLine: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	sess.SyncCommentsFromDisk()

	if got := bodies(sess.GetComments("plan.md")); !got["local"] || !got["remote"] {
		t.Fatalf("comments in memory = %v", got)
	}
	if got := bodies(loadReview(t, critPath).Files["plan.md"].Comments); !got["local"] || !got["remote"] {
		t.Fatalf("comments on disk = %v", got)
	}
}

// ApplyReviewFileUpdate saves pending comments before the merge and loads
// the merge result after it.
func TestApplyReviewFileUpdateWithPendingWrite(t *testing.T) {
	sess, critPath := newMergeSession(t)
	sess.AddReviewComment("pending", "", "")
	changed, err := sess.ApplyReviewFileUpdate(func() error {
		return MergeWebComments(critPath, []WebComment{{Body: "remote review", Scope: "review"}}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("reload reported no change")
	}
	if got := bodies(sess.GetReviewComments()); !got["pending"] || !got["remote review"] {
		t.Fatalf("review comments in memory = %v", got)
	}
	if got := bodies(loadReview(t, critPath).ReviewComments); !got["pending"] || !got["remote review"] {
		t.Fatalf("review comments on disk = %v", got)
	}
}

// Pulled replies need IDs so a daemon save can match them against memory.
func TestMergedWebRepliesHaveIDs(t *testing.T) {
	sess, critPath := newMergeSession(t)
	local, _ := sess.AddComment("plan.md", 1, 1, "", "local", "", "", "")
	sess.WriteFiles()
	err := MergeWebComments(critPath,
		[]WebComment{{Body: "remote", FilePath: "plan.md", StartLine: 1, EndLine: 1, Replies: []WebReply{{Body: "thread reply"}}}},
		map[string][]WebReply{local.ID: {{Body: "new reply"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range loadReview(t, critPath).Files["plan.md"].Comments {
		for _, r := range c.Replies {
			if r.ID == "" {
				t.Errorf("reply %q on %q has no ID", r.Body, c.Body)
			}
		}
	}
}
