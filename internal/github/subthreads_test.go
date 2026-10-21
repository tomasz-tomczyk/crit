package github

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
)

// subThreadPushReview holds a pushed thread whose pushed reply carries a
// sub-thread with its own local reply, a new postable comment, and a
// review-level sub-thread.
func subThreadPushReview() CritJSON {
	return CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "r_root", Body: "general", Scope: "review"},
			{ID: "r_sub", Body: "nested review note", Scope: "review", ParentID: "r_root", Quote: "general"},
		},
		Files: map[string]CritJSONFile{
			"main.go": {Status: "modified", Comments: []Comment{
				{ID: "c_root", Body: "root", Author: "reviewer", Scope: "line", StartLine: 3, EndLine: 3,
					GitHubID: 100, LastPushedBodyHash: bodyHashAtPush("root"),
					Replies: []Reply{{ID: "rp_1", Body: "pushed answer", GitHubID: 101, LastPushedBodyHash: bodyHashAtPush("pushed answer")}}},
				{ID: "c_sub", Body: "why this?", Author: "reviewer", Scope: "line", ParentID: "rp_1", Quote: "answer",
					Replies: []Reply{{ID: "rp_2", Body: "local answer in the sub-thread"}}},
				{ID: "c_new", Body: "fresh", Scope: "line", StartLine: 5, EndLine: 5},
			}},
		},
	}
}

func TestBucketCommentsForPush_SubThreadsStayOut(t *testing.T) {
	b := bucketCommentsForPush(subThreadPushReview(), "", false)
	if len(b.Postable) != 1 || b.Postable[0].Comment.ID != "c_new" {
		t.Errorf("postable = %+v, want only c_new", b.Postable)
	}
	if len(b.ReviewLevel) != 1 || b.ReviewLevel[0].Comment.ID != "r_root" {
		t.Errorf("review-level = %+v, want only r_root", b.ReviewLevel)
	}
	if len(b.FullStack)+len(b.Unmapped) != 0 {
		t.Errorf("sub-threads bucketed off the PR: full-stack %+v, unmapped %+v", b.FullStack, b.Unmapped)
	}
}

func TestRunPushDryRun_CountsSubThreads(t *testing.T) {
	cj := subThreadPushReview()
	out := captureStdout(t, func() {
		runPushDryRun(pushContext{prNumber: 7, cj: cj}, bucketCommentsForPush(cj, "", false))
	})
	if !strings.Contains(out, "2 sub-threads stay in crit.\n") {
		t.Errorf("dry-run output misses the sub-thread count:\n%s", out)
	}
	if !strings.Contains(out, "fresh") || strings.Contains(out, "why this?") || strings.Contains(out, "nested review note") {
		t.Errorf("dry-run lists the wrong comments:\n%s", out)
	}

	cf := cj.Files["main.go"]
	cf.Comments = cf.Comments[:1]
	cj.Files["main.go"] = cf
	out = captureStdout(t, func() {
		runPushDryRun(pushContext{prNumber: 7, cj: cj}, bucketCommentsForPush(cj, "", false))
	})
	if !strings.Contains(out, "1 sub-thread stays in crit.\n") {
		t.Errorf("dry-run output misses the singular count:\n%s", out)
	}
}

func TestUpdateCritJSONWithGitHubIDs_NeverStampsSubThreads(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), ".crit")
	if err := review.SaveCritJSON(critPath, subThreadPushReview()); err != nil {
		t.Fatal(err)
	}
	if err := updateCritJSONWithGitHubIDs(critPath, map[string]int64{"main.go:0": 555}, nil); err != nil {
		t.Fatal(err)
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if sub := cj.Files["main.go"].Comments[1]; sub.GitHubID != 0 {
		t.Errorf("sub-thread stamped with GitHub ID %d", sub.GitHubID)
	}
}

// `crit pull` neither creates nor touches a sub-thread.
func TestMergeGHComments_LeavesSubThreadsAlone(t *testing.T) {
	cj := subThreadPushReview()
	before, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	var want CritJSON
	if err := json.Unmarshal(before, &want); err != nil {
		t.Fatal(err)
	}
	user := func(login string) struct {
		Login string `json:"login"`
	} {
		return struct {
			Login string `json:"login"`
		}{login}
	}
	gh := []ghComment{
		{ID: 100, Path: "main.go", Line: 3, Side: "RIGHT", Body: "root", User: user("reviewer")},
		{ID: 101, Path: "main.go", Line: 3, Side: "RIGHT", Body: "pushed answer", User: user("agent"), InReplyToID: 100},
		{ID: 102, Path: "main.go", Line: 3, Side: "RIGHT", Body: "remote follow-up", User: user("agent"), InReplyToID: 100},
		{ID: 200, Path: "main.go", Line: 8, Side: "RIGHT", Body: "why this?", User: user("reviewer")},
	}
	if added := mergeGHCommentsWithNames(&cj, gh, userNameCache{"reviewer": "reviewer", "agent": "agent"}, inheritedScope{}, nil); added != 2 {
		t.Fatalf("added = %d, want the remote reply and the remote root", added)
	}

	got := cj.Files["main.go"].Comments
	if !reflect.DeepEqual(got[1], want.Files["main.go"].Comments[1]) {
		t.Errorf("file sub-thread changed by the pull:\n got %+v\nwant %+v", got[1], want.Files["main.go"].Comments[1])
	}
	if !reflect.DeepEqual(cj.ReviewComments, want.ReviewComments) {
		t.Errorf("review comments changed by the pull: %+v", cj.ReviewComments)
	}
	for _, c := range got {
		if c.ID != "c_sub" && c.ParentID != "" {
			t.Errorf("pull created a sub-thread: %+v", c)
		}
	}
	if len(got) != 4 || len(got[0].Replies) != 2 {
		t.Errorf("pulled comments = %+v", got)
	}
}

// A sub-thread never stands for a GitHub comment, even one with the same
// author, lines and body.
func TestIsDuplicateGHComment_IgnoresSubThreads(t *testing.T) {
	comments := []Comment{{ID: "c_sub", ParentID: "rp_1", Author: "reviewer", Body: "same words"}}
	if isDuplicateGHComment(comments, 999, "reviewer", 0, 0, "same words") {
		t.Error("a sub-thread was taken for the GitHub comment")
	}
}
