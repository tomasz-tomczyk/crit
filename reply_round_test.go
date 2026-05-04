package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRepliesAtOrBeforeRound_HidesFutureReplies covers the unit-level
// reply-filter helper: a reply authored in R2 must be hidden when viewing R1,
// while replies at or before R1 must be retained.
func TestRepliesAtOrBeforeRound_HidesFutureReplies(t *testing.T) {
	parentRound := 1
	replies := []Reply{
		{ID: "rp1", ReviewRound: 1},
		{ID: "rp2", ReviewRound: 2},
		{ID: "rp3", ReviewRound: 3},
	}
	got := repliesAtOrBeforeRound(replies, 1, parentRound)
	if len(got) != 1 || got[0].ID != "rp1" {
		t.Fatalf("expected only rp1 at round<=1, got %+v", got)
	}
}

// TestRepliesAtOrBeforeRound_VisibleAtOwnRound: a reply authored at R1 is
// visible when viewing R1.
func TestRepliesAtOrBeforeRound_VisibleAtOwnRound(t *testing.T) {
	got := repliesAtOrBeforeRound([]Reply{{ID: "rp1", ReviewRound: 1}}, 1, 1)
	if len(got) != 1 {
		t.Fatalf("reply at own round must be visible, got %+v", got)
	}
}

// TestRepliesAtOrBeforeRound_LegacyFallsBackToParent: a reply with no
// ReviewRound set (zero value, e.g. from review files written before the
// field existed) inherits its parent's round. So a legacy reply on a
// parent authored at R1 is visible from R1 onward.
func TestRepliesAtOrBeforeRound_LegacyFallsBackToParent(t *testing.T) {
	replies := []Reply{{ID: "rp_legacy"}} // ReviewRound == 0
	if got := repliesAtOrBeforeRound(replies, 1, 1); len(got) != 1 {
		t.Errorf("legacy reply on R1 parent must be visible at R1, got %+v", got)
	}
	if got := repliesAtOrBeforeRound(replies, 0, 1); len(got) != 0 {
		// round=0 is invalid for callers, but the helper is reached only
		// from commentsAtOrBeforeRound which short-circuits round<=0.
		// Sanity: when parent's effective round (1) > requested (0), hide.
		t.Errorf("legacy reply must be hidden when round<parent, got %+v", got)
	}
	// Legacy reply on R2 parent must be hidden at R1.
	if got := repliesAtOrBeforeRound(replies, 1, 2); len(got) != 0 {
		t.Errorf("legacy reply on R2 parent must be hidden at R1, got %+v", got)
	}
}

// TestRepliesAtOrBeforeRound_Chain: a multi-round reply chain on the same
// parent should be progressively revealed as the viewer scrubs forward.
func TestRepliesAtOrBeforeRound_Chain(t *testing.T) {
	replies := []Reply{
		{ID: "rp1", ReviewRound: 1},
		{ID: "rp2", ReviewRound: 2},
		{ID: "rp3", ReviewRound: 3},
	}
	for _, tc := range []struct {
		round, want int
	}{
		{1, 1}, {2, 2}, {3, 3}, {4, 3},
	} {
		got := repliesAtOrBeforeRound(replies, tc.round, 1)
		if len(got) != tc.want {
			t.Errorf("round=%d: want %d replies, got %d (%+v)", tc.round, tc.want, len(got), got)
		}
	}
}

// TestCommentsAtOrBeforeRound_FiltersFutureReplies: the top-level filter
// keeps R1 parents but drops their R2 replies when scoping to round=1.
func TestCommentsAtOrBeforeRound_FiltersFutureReplies(t *testing.T) {
	cs := []Comment{
		{
			ID:          "c1",
			ReviewRound: 1,
			Replies: []Reply{
				{ID: "rp1", ReviewRound: 1},
				{ID: "rp2", ReviewRound: 2},
			},
		},
	}
	got := commentsAtOrBeforeRound(cs, 1)
	if len(got) != 1 {
		t.Fatalf("expected parent comment retained, got %+v", got)
	}
	if len(got[0].Replies) != 1 || got[0].Replies[0].ID != "rp1" {
		t.Errorf("expected only rp1 at round=1, got %+v", got[0].Replies)
	}
	// Original input must not be mutated.
	if len(cs[0].Replies) != 2 {
		t.Errorf("input replies mutated: %+v", cs[0].Replies)
	}
}

// TestReply_RoundTripJSON guards that the new ReviewRound field round-trips
// cleanly through encoding/json with the expected wire name and omitempty.
func TestReply_RoundTripJSON(t *testing.T) {
	in := Reply{ID: "rp1", Body: "hi", CreatedAt: "now", ReviewRound: 2}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"review_round":2`; !strings.Contains(string(data), want) {
		t.Errorf("missing %s in %s", want, data)
	}
	var out Reply
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.ReviewRound != 2 {
		t.Errorf("round did not round-trip: got %d", out.ReviewRound)
	}

	// Zero value must omit the field on the wire (legacy/back-compat shape).
	zero, _ := json.Marshal(Reply{ID: "rp0", Body: "x", CreatedAt: "now"})
	if strings.Contains(string(zero), "review_round") {
		t.Errorf("expected review_round omitted when zero, got %s", zero)
	}
}

// TestHandleFileComments_RoundFiltersReplies covers the HTTP path: a reply
// authored at R2 must not appear in /api/file/comments?round=1, but must
// appear at round=2.
func TestHandleFileComments_RoundFiltersReplies(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	sess.Files[0].Comments = []Comment{
		{
			ID: "c1", ReviewRound: 1, Body: "parent",
			Replies: []Reply{
				{ID: "rp1", ReviewRound: 1, Body: "early"},
				{ID: "rp2", ReviewRound: 2, Body: "late"},
			},
		},
	}

	// At round=1, only the early reply is visible.
	req := httptest.NewRequest("GET", "/api/file/comments?path=test.md&round=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("round=1 status=%d body=%s", w.Code, w.Body.String())
	}
	var r1 []Comment
	if err := json.Unmarshal(w.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	if len(r1) != 1 || len(r1[0].Replies) != 1 || r1[0].Replies[0].ID != "rp1" {
		t.Fatalf("round=1 expected single reply rp1, got %+v", r1)
	}

	// At round=2, both replies are visible.
	req = httptest.NewRequest("GET", "/api/file/comments?path=test.md&round=2", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("round=2 status=%d body=%s", w.Code, w.Body.String())
	}
	var r2 []Comment
	if err := json.Unmarshal(w.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if len(r2) != 1 || len(r2[0].Replies) != 2 {
		t.Fatalf("round=2 expected both replies, got %+v", r2)
	}
}

// TestSession_AddReply_StampsReviewRound: the in-memory reply-add path must
// stamp the current Session.ReviewRound onto the reply.
func TestSession_AddReply_StampsReviewRound(t *testing.T) {
	_, sess := newTestServer(t)
	sess.Files[0].Comments = []Comment{{ID: "c1", ReviewRound: 1}}
	sess.ReviewRound = 3

	r, ok := sess.AddReply("test.md", "c1", "reply body", "alice", "")
	if !ok {
		t.Fatal("AddReply returned !ok")
	}
	if r.ReviewRound != 3 {
		t.Errorf("expected reply ReviewRound=3, got %d", r.ReviewRound)
	}
	if got := sess.Files[0].Comments[0].Replies[0].ReviewRound; got != 3 {
		t.Errorf("persisted reply round=%d, want 3", got)
	}
}

// TestAppendReply_StampsCritJSONReviewRound: the headless CLI path stamps the
// reply with cj.ReviewRound (the persisted on-disk value) since it has no
// live Session.
func TestAppendReply_StampsCritJSONReviewRound(t *testing.T) {
	cj := &CritJSON{
		ReviewRound: 4,
		Files: map[string]CritJSONFile{
			"a.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", ReviewRound: 1, Body: "x"},
				},
			},
		},
	}
	if err := appendReply(cj, "c1", "reply", "agent", "", false, ""); err != nil {
		t.Fatalf("appendReply: %v", err)
	}
	got := cj.Files["a.go"].Comments[0].Replies[0].ReviewRound
	if got != 4 {
		t.Errorf("expected reply ReviewRound=4 (from cj.ReviewRound), got %d", got)
	}

	// Review-level comment path.
	cj2 := &CritJSON{
		ReviewRound:    7,
		ReviewComments: []Comment{{ID: "r0", Scope: "review"}},
		Files:          map[string]CritJSONFile{},
	}
	if err := appendReply(cj2, "r0", "ack", "agent", "", false, ""); err != nil {
		t.Fatalf("appendReply review: %v", err)
	}
	if cj2.ReviewComments[0].Replies[0].ReviewRound != 7 {
		t.Errorf("expected review reply ReviewRound=7, got %d", cj2.ReviewComments[0].Replies[0].ReviewRound)
	}
}

// TestSession_AddReviewCommentReply_StampsReviewRound mirrors the file-comment
// test for review-level comments.
func TestSession_AddReviewCommentReply_StampsReviewRound(t *testing.T) {
	_, sess := newTestServer(t)
	sess.ReviewRound = 5
	parent := sess.AddReviewComment("parent body", "alice", "")

	r, ok := sess.AddReviewCommentReply(parent.ID, "reply", "alice", "")
	if !ok {
		t.Fatal("AddReviewCommentReply returned !ok")
	}
	if r.ReviewRound != 5 {
		t.Errorf("expected reply ReviewRound=5, got %d", r.ReviewRound)
	}
}
