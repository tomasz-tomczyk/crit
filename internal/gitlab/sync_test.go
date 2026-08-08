package gitlab

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestMergeDiscussionsIgnoresGitLabReviewStateThreads(t *testing.T) {
	// Captured from the live roundtrip: bulk publication and resolution create
	// system discussions alongside the actual positioned inline discussion.
	discussions := []gitlabDiscussion{
		{ID: "system-review", Notes: []gitlabNote{{ID: 1, Body: "left review comments", System: true}}},
		{ID: "inline", Notes: []gitlabNote{
			{ID: 2, Body: "root", Position: &gitlabPosition{PositionType: "text", NewPath: "main.go", NewLine: 3}},
			{ID: 3, Body: "reply"},
		}},
		{ID: "system-resolve", Notes: []gitlabNote{{ID: 4, Body: "resolved all threads", System: true}}},
	}
	cj := session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}}

	imported, updated := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 2 || updated != 0 {
		t.Fatalf("merge counts = (%d, %d), want root + reply import", imported, updated)
	}
	if len(cj.Files) != 1 || len(cj.Files["main.go"].Comments) != 1 {
		t.Fatalf("system discussions leaked into review state: %+v", cj.Files)
	}
	comment := cj.Files["main.go"].Comments[0]
	if comment.GitLabDiscussionID != "inline" || len(comment.Replies) != 1 || comment.Replies[0].GitLabNoteID != 3 {
		t.Fatalf("inline thread shape not preserved: %+v", comment)
	}
}

func TestMergeDiscussionsBindsLocalIDsAndIsIdempotent(t *testing.T) {
	cj := session.CritJSON{
		ReviewRound: 2,
		Files: map[string]session.CritJSONFile{
			"main.go": {Comments: []session.Comment{{
				ID: "c_local", StartLine: 10, EndLine: 10, Body: "fix this", Author: "Local",
				Replies: []session.Reply{{ID: "rp_local", Body: "follow-up", Author: "Local"}},
			}}},
		},
	}
	discussions := []gitlabDiscussion{{
		ID: "discussion-1",
		Notes: []gitlabNote{
			{ID: 101, Body: withLocalMarker("fix this", "c_local"), Author: gitlabUser{Name: "Remote"}, Position: &gitlabPosition{PositionType: "text", NewPath: "main.go", NewLine: 10}},
			{ID: 102, Body: withLocalMarker("follow-up", "rp_local"), Author: gitlabUser{Name: "Remote"}},
		},
	}}

	imported, updated := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 0 || updated != 2 {
		t.Fatalf("merge counts = (%d, %d), want (0, 2)", imported, updated)
	}
	comment := cj.Files["main.go"].Comments[0]
	if comment.GitLabNoteID != 101 || comment.GitLabDiscussionID != "discussion-1" || comment.Body != "fix this" {
		t.Fatalf("root not bound cleanly: %+v", comment)
	}
	if len(comment.Replies) != 1 || comment.Replies[0].GitLabNoteID != 102 || comment.Replies[0].Body != "follow-up" {
		t.Fatalf("reply not bound cleanly: %+v", comment.Replies)
	}

	imported, updated = mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 0 || updated != 0 {
		t.Fatalf("second merge counts = (%d, %d), want (0, 0)", imported, updated)
	}
	if got := len(cj.Files["main.go"].Comments); got != 1 {
		t.Fatalf("second merge duplicated root: %d comments", got)
	}
}

func TestMergeDiscussionsImportsExternalThread(t *testing.T) {
	cj := session.CritJSON{ReviewRound: 3, Files: map[string]session.CritJSONFile{}}
	resolved := true
	discussions := []gitlabDiscussion{{ID: "d1", Notes: []gitlabNote{{
		ID: 5, Body: "external", Author: gitlabUser{Username: "reviewer"}, CreatedAt: "2026-01-01T00:00:00Z",
		Resolved: resolved, Position: &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 8},
	}}}}
	imported, _ := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}
	comment := cj.Files["a.go"].Comments[0]
	if !comment.Resolved || comment.ResolvedRound != 3 || comment.GitLabResolved == nil || !*comment.GitLabResolved {
		t.Fatalf("resolution state not imported: %+v", comment)
	}
}

func TestMergeDiscussionsImportsRemoteEditUnresolveAndDelete(t *testing.T) {
	resolved := true
	cj := session.CritJSON{ReviewRound: 3, Files: map[string]session.CritJSONFile{
		"a.go": {Comments: []session.Comment{
			{ID: "c_1", Body: "old", Resolved: true, ResolvedRound: 2, GitLabNoteID: 10, GitLabDiscussionID: "d1", GitLabResolved: &resolved, LastPushedBodyHash: bodyHash("old")},
			{ID: "c_deleted", Body: "gone", GitLabNoteID: 20, GitLabDiscussionID: "d2"},
		}},
	}}
	discussions := []gitlabDiscussion{{ID: "d1", Notes: []gitlabNote{{
		ID: 10, Body: "edited remotely", Position: &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 3},
	}}}}
	_, updated := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if updated != 3 {
		t.Fatalf("updated = %d, want edit + unresolve + deletion", updated)
	}
	comments := cj.Files["a.go"].Comments
	if len(comments) != 1 || comments[0].Body != "edited remotely" || comments[0].Resolved || comments[0].ResolvedRound != 0 {
		t.Fatalf("reconciled comments = %+v", comments)
	}
}

func TestMergeDiscussionsPreservesConcurrentLocalEdit(t *testing.T) {
	cj := session.CritJSON{Files: map[string]session.CritJSONFile{
		"a.go": {Comments: []session.Comment{{
			ID: "c_1", Body: "local edit", GitLabNoteID: 10, GitLabDiscussionID: "d1", LastPushedBodyHash: bodyHash("old remote"),
		}}},
	}}
	discussions := []gitlabDiscussion{{ID: "d1", Notes: []gitlabNote{{
		ID: 10, Body: "new remote", Position: &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 3},
	}}}}
	mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if got := cj.Files["a.go"].Comments[0].Body; got != "local edit" {
		t.Fatalf("concurrent local edit overwritten with %q", got)
	}
}

func TestMergeDiscussionsDoesNotReimportPendingDelete(t *testing.T) {
	cj := session.CritJSON{
		Files:                map[string]session.CritJSONFile{},
		PendingRemoteDeletes: []session.RemoteRef{{Forge: "gitlab", CommentID: 10, ThreadID: "d1"}},
	}
	discussions := []gitlabDiscussion{{ID: "d1", Notes: []gitlabNote{{
		ID: 10, Body: "deleted locally", Position: &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 3},
	}}}}
	imported, _ := mergeDiscussions(&cj, discussions, session.InheritedScope{})
	if imported != 0 || len(cj.Files) != 0 {
		t.Fatalf("pending delete was reimported: %+v", cj.Files)
	}
}

func TestBuildPositionAndLineCode(t *testing.T) {
	refs := diffRefs{BaseSHA: "base", StartSHA: "start", HeadSHA: "head"}
	position := buildPosition(refs, pushCandidate{Path: "main.go", StartLine: 7, EndLine: 9, Side: "new"})
	if position["new_line"] != 9 || position["old_line"] != nil {
		t.Fatalf("line position = %+v", position)
	}
	rangeValue, ok := position["line_range"].(map[string]any)
	if !ok {
		t.Fatalf("line_range missing: %+v", position)
	}
	start := rangeValue["start"].(map[string]any)
	sum := sha1.Sum([]byte("main.go"))
	wantCode := hex.EncodeToString(sum[:]) + "_0_7"
	if start["line_code"] != wantCode || start["new_line"] != 7 || start["type"] != "new" {
		t.Fatalf("start range = %+v, want line_code %s", start, wantCode)
	}
}

func TestLocalMarkerRoundTrip(t *testing.T) {
	body := "line one\n\nline two"
	marked := withLocalMarker(body, "c_123")
	gotBody, gotID := splitLocalMarker(marked)
	if gotBody != body || gotID != "c_123" {
		t.Fatalf("splitLocalMarker = (%q, %q)", gotBody, gotID)
	}
	if _, id := splitLocalMarker("ordinary comment"); id != "" {
		t.Fatalf("ordinary comment produced marker %q", id)
	}
}

func TestCollectPushCandidatesSkipsRemoteAndStale(t *testing.T) {
	cj := session.CritJSON{Files: map[string]session.CritJSONFile{
		"a.go": {Comments: []session.Comment{
			{ID: "c_new", StartLine: 1, EndLine: 1, Body: "new", HeadSHA: "head"},
			{ID: "c_stale", StartLine: 2, EndLine: 2, Body: "stale", HeadSHA: "old"},
			{ID: "c_github", StartLine: 3, EndLine: 3, Body: "gh", GitHubID: 4},
		}},
	}}
	candidates, skipped := collectPushCandidates(cj, "head")
	if len(candidates) != 1 || candidates[0].CommentID != "c_new" || skipped != 1 {
		t.Fatalf("candidates=%+v skipped=%d", candidates, skipped)
	}
}

func TestUnownedDraftDetection(t *testing.T) {
	drafts := []draftNoteRaw{{ID: 1, Note: withLocalMarker("ours", "c_1")}, {ID: 2, Note: "user draft"}}
	if countOwnedDrafts(drafts) != 1 {
		t.Fatalf("owned count = %d", countOwnedDrafts(drafts))
	}
	if got := firstUnownedDraft(drafts); got == nil || got.ID != 2 {
		t.Fatalf("first unowned = %+v", got)
	}
}
