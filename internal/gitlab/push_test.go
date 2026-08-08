package gitlab

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestCollectPushCandidatesIncludesReplyOnResolvedDiscussion(t *testing.T) {
	cj := session.CritJSON{Files: map[string]session.CritJSONFile{
		"main.go": {Comments: []session.Comment{{
			ID: "c_root", StartLine: 3, EndLine: 3, Side: "new", Body: "root",
			Resolved: true, GitLabNoteID: 41, GitLabDiscussionID: "discussion-1",
			Replies: []session.Reply{{ID: "rp_new", Body: "follow-up"}},
		}}},
	}}

	candidates, skipped := collectPushCandidates(cj, "head-sha")
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 resolved root", skipped)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want one reply", candidates)
	}
	if candidates[0].ReplyID != "rp_new" || candidates[0].DiscussionID != "discussion-1" {
		t.Fatalf("reply candidate = %+v", candidates[0])
	}
}

func TestGitLabDeleteNoteIDs(t *testing.T) {
	discussion := gitlabDiscussion{Notes: []gitlabNote{
		{ID: 10, Position: &gitlabPosition{PositionType: "text"}},
		{ID: 11},
		{ID: 12, System: true},
		{ID: 13},
	}}

	rootDelete := gitLabDeleteNoteIDs(discussion, 10)
	wantRoot := []int64{13, 11, 10}
	if len(rootDelete) != len(wantRoot) {
		t.Fatalf("root delete IDs = %v, want %v", rootDelete, wantRoot)
	}
	for i := range wantRoot {
		if rootDelete[i] != wantRoot[i] {
			t.Fatalf("root delete IDs = %v, want %v", rootDelete, wantRoot)
		}
	}

	replyDelete := gitLabDeleteNoteIDs(discussion, 11)
	if len(replyDelete) != 1 || replyDelete[0] != 11 {
		t.Fatalf("reply delete IDs = %v, want [11]", replyDelete)
	}
}
