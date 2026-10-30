package gitlab

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// subThreadMR holds a published GitLab thread whose published reply carries
// a sub-thread (with a local reply and a nested sub-thread), plus a
// review-level sub-thread.
func subThreadMR() session.CritJSON {
	remoteResolved := false
	return session.CritJSON{
		ReviewRound: 1,
		ReviewComments: []session.Comment{
			{ID: "r_root", Body: "general", Scope: "review"},
			{ID: "r_sub", Body: "nested review note", Scope: "review", ParentID: "r_root"},
		},
		Files: map[string]session.CritJSONFile{"main.go": {Comments: []session.Comment{
			{ID: "c_root", StartLine: 3, EndLine: 3, Body: "root", Author: "Remote",
				GitLabNoteID: 101, GitLabDiscussionID: "d1", GitLabResolved: &remoteResolved, LastPushedBodyHash: bodyHash("root"),
				Replies: []session.Reply{{ID: "rp_1", Body: "answer", Author: "Remote", GitLabNoteID: 102, GitLabDiscussionID: "d1", LastPushedBodyHash: bodyHash("answer")}}},
			{ID: "c_sub", Body: "why?", Scope: "line", ParentID: "rp_1", Quote: "answer",
				Replies: []session.Reply{{ID: "rp_2", Body: "local answer"}}},
			{ID: "c_deep", Body: "deeper", Scope: "line", ParentID: "rp_2"},
			{ID: "c_keep", StartLine: 9, EndLine: 9, Body: "unrelated"},
		}}},
	}
}

func TestCollectPushCandidates_SkipsSubThreadsAndTheirReplies(t *testing.T) {
	candidates, skipped := collectPushCandidates(subThreadMR(), "head")
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0: sub-threads are counted apart", skipped)
	}
	if len(candidates) != 1 || candidates[0].CommentID != "c_keep" || candidates[0].ReplyID != "" {
		t.Errorf("candidates = %+v, want only c_keep", candidates)
	}
}

// A note gone from GitLab takes its sub-threads with it, at any depth.
func TestDropDeletedGitLabNotes_CascadesSubThreads(t *testing.T) {
	cj := subThreadMR()
	removed := dropDeletedGitLabNotes(&cj, map[int64]bool{101: true}) // reply note 102 is gone
	if removed != 3 {
		t.Errorf("removed = %d, want the reply and its 2 descendant sub-threads", removed)
	}
	var ids []string
	for _, c := range cj.Files["main.go"].Comments {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "c_root,c_keep" {
		t.Errorf("remaining comments = %v", ids)
	}

	cj = subThreadMR()
	dropDeletedGitLabNotes(&cj, map[int64]bool{}) // the whole thread is gone
	ids = ids[:0]
	for _, c := range cj.Files["main.go"].Comments {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "c_keep" {
		t.Errorf("remaining comments after a thread delete = %v", ids)
	}
	if len(cj.ReviewComments) != 2 {
		t.Errorf("review-level comments touched: %+v", cj.ReviewComments)
	}
}

// `crit pull` from GitLab neither creates nor touches a sub-thread.
func TestMergeDiscussions_LeavesSubThreadsAlone(t *testing.T) {
	cj := subThreadMR()
	want := subThreadMR()
	discussions := []gitlabDiscussion{
		{ID: "d1", Notes: []gitlabNote{
			{ID: 101, Body: "root", Author: gitlabUser{Name: "Remote"}, Position: &gitlabPosition{PositionType: "text", NewPath: "main.go", NewLine: 3}},
			{ID: 102, Body: "answer", Author: gitlabUser{Name: "Remote"}},
			{ID: 103, Body: "remote follow-up", Author: gitlabUser{Name: "Remote"}},
		}},
		{ID: "d2", Notes: []gitlabNote{
			{ID: 201, Body: "why?", Position: &gitlabPosition{PositionType: "text", NewPath: "main.go", NewLine: 6}},
		}},
	}
	imported, _ := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 2 {
		t.Fatalf("imported = %d, want the remote reply and the remote thread", imported)
	}
	got := cj.Files["main.go"].Comments
	for i := 1; i <= 2; i++ {
		if !reflect.DeepEqual(got[i], want.Files["main.go"].Comments[i]) {
			t.Errorf("sub-thread %s changed by the pull:\n got %+v\nwant %+v", got[i].ID, got[i], want.Files["main.go"].Comments[i])
		}
	}
	if !reflect.DeepEqual(cj.ReviewComments, want.ReviewComments) {
		t.Errorf("review comments changed by the pull: %+v", cj.ReviewComments)
	}
	for _, c := range got[3:] {
		if c.ParentID != "" {
			t.Errorf("pull created a sub-thread: %+v", c)
		}
	}
}

func TestRunPush_ReportsSubThreadsStayInCrit(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	cj := subThreadMR()
	cf := cj.Files["main.go"]
	cf.Comments = cf.Comments[:3] // the published thread and its sub-threads
	cj.Files["main.go"] = cf
	savePushReview(t, outputDir, cj)
	mr := commandResponse{stdout: `{"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`}
	request := forge.PushRequest{Repo: forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"}, ChangeSpec: "7", OutputDir: outputDir}

	stubCommands(t, commandResponse{}, mr)
	dryRun := request
	dryRun.DryRun = true
	out := captureGitLabStdout(t, func() error { _, err := runPush(context.Background(), dryRun); return err })
	if out != "GitLab MR !7: 0 new comments/replies, 0 skipped; event=comment\n3 sub-threads stay in crit.\n" {
		t.Errorf("dry-run output = %q", out)
	}

	calls := stubCommands(t, commandResponse{}, mr, commandResponse{stdout: `[]`})
	out = captureGitLabStdout(t, func() error { _, err := runPush(context.Background(), request); return err })
	if out != "No comments to push.\n3 sub-threads stay in crit.\n" || len(*calls) != 3 {
		t.Errorf("push output = %q after %d calls", out, len(*calls))
	}
}

func captureGitLabStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatal(runErr)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
