package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveStampsResolvedRound_HTTP verifies that resolving a file comment
// via PUT /api/comment/{id}/resolve sets ResolvedRound to the current
// session.ReviewRound at the moment of mutation, and clears it back to 0
// when the comment is unresolved.
func TestResolveStampsResolvedRound_HTTP(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "", "")

	// Advance to round 3 before resolving — the field should record 3, not 1.
	session.mu.Lock()
	session.ReviewRound = 3
	session.mu.Unlock()

	req := httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader(`{"resolved": true}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT resolve: status = %d, body = %s", w.Code, w.Body.String())
	}
	var got Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Resolved {
		t.Fatal("expected resolved=true")
	}
	if got.ResolvedRound != 3 {
		t.Errorf("ResolvedRound = %d, want 3", got.ResolvedRound)
	}

	// Unresolve clears to 0.
	req = httptest.NewRequest("PUT", "/api/comment/"+c.ID+"/resolve?path=test.md", strings.NewReader(`{"resolved": false}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT unresolve: status = %d", w.Code)
	}
	var cleared Comment
	if err := json.Unmarshal(w.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Resolved {
		t.Fatal("expected resolved=false after unresolve")
	}
	if cleared.ResolvedRound != 0 {
		t.Errorf("ResolvedRound after unresolve = %d, want 0", cleared.ResolvedRound)
	}
}

// TestResolveReviewCommentStampsResolvedRound_HTTP covers the review-level
// resolve path.
func TestResolveReviewCommentStampsResolvedRound_HTTP(t *testing.T) {
	srv, session := newTestServer(t)
	c := session.AddReviewComment("review note", "alice", "")

	session.mu.Lock()
	session.ReviewRound = 4
	session.mu.Unlock()

	req := httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", strings.NewReader(`{"resolved": true}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT resolve: status = %d, body = %s", w.Code, w.Body.String())
	}
	var got Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ResolvedRound != 4 {
		t.Errorf("ResolvedRound = %d, want 4", got.ResolvedRound)
	}

	// Unresolve clears.
	req = httptest.NewRequest("PUT", "/api/review-comment/"+c.ID+"/resolve", strings.NewReader(`{"resolved": false}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT unresolve: status = %d", w.Code)
	}
	var cleared Comment
	if err := json.Unmarshal(w.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.ResolvedRound != 0 {
		t.Errorf("ResolvedRound after unresolve = %d, want 0", cleared.ResolvedRound)
	}
}

// TestReplyResolveStampsResolvedRound_CLI exercises `crit comment --reply-to
// <id> --resolve <body>` via appendReply, asserting that the parent comment's
// ResolvedRound is set from cj.ReviewRound at the time of the call.
func TestReplyResolveStampsResolvedRound_CLI(t *testing.T) {
	cj := CritJSON{
		ReviewRound: 5,
		Files: map[string]CritJSONFile{
			"a.md": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "open", ReviewRound: 1},
				},
			},
		},
	}
	if err := appendReply(&cj, "c1", "done", "alice", "", true, ""); err != nil {
		t.Fatal(err)
	}
	got := cj.Files["a.md"].Comments[0]
	if !got.Resolved {
		t.Fatal("expected resolved=true after --resolve")
	}
	if got.ResolvedRound != 5 {
		t.Errorf("ResolvedRound = %d, want 5", got.ResolvedRound)
	}
	if len(got.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got.Replies))
	}
}

// TestReplyResolveReviewLevel_CLI covers reply --resolve against a
// review-level comment.
func TestReplyResolveReviewLevel_CLI(t *testing.T) {
	cj := CritJSON{
		ReviewRound: 7,
		ReviewComments: []Comment{
			{ID: "r1", Body: "review note", ReviewRound: 1, Scope: "review"},
		},
		Files: map[string]CritJSONFile{},
	}
	if err := appendReply(&cj, "r1", "addressed", "alice", "", true, ""); err != nil {
		t.Fatal(err)
	}
	got := cj.ReviewComments[0]
	if !got.Resolved || got.ResolvedRound != 7 {
		t.Errorf("ResolvedRound=%d resolved=%v, want 7/true", got.ResolvedRound, got.Resolved)
	}
}

// TestAppendReply_NonResolvingClearsResolved is a regression test for
// review W1: the CLI appendReply path must mirror the HTTP AddReply path
// and clear Resolved/ResolvedRound when adding a reply to an already-
// resolved comment, otherwise the new reply gets hidden by the resolution
// filter and the data semantics diverge between writers.
func TestAppendReply_NonResolvingClearsResolved(t *testing.T) {
	t.Run("file_comment", func(t *testing.T) {
		cj := CritJSON{
			ReviewRound: 4,
			Files: map[string]CritJSONFile{
				"a.md": {
					Status: "modified",
					Comments: []Comment{
						{
							ID: "c1", StartLine: 1, EndLine: 1, Body: "open", ReviewRound: 1,
							Resolved: true, ResolvedRound: 2,
						},
					},
				},
			},
		}
		if err := appendReply(&cj, "c1", "actually not done", "alice", "", false, ""); err != nil {
			t.Fatal(err)
		}
		got := cj.Files["a.md"].Comments[0]
		if got.Resolved {
			t.Error("expected Resolved=false after non-resolving reply")
		}
		if got.ResolvedRound != 0 {
			t.Errorf("ResolvedRound = %d, want 0", got.ResolvedRound)
		}
		if len(got.Replies) != 1 {
			t.Errorf("expected 1 reply, got %d", len(got.Replies))
		}
	})

	t.Run("review_comment", func(t *testing.T) {
		cj := CritJSON{
			ReviewRound: 4,
			ReviewComments: []Comment{
				{ID: "r1", Body: "review note", ReviewRound: 1, Scope: "review",
					Resolved: true, ResolvedRound: 2},
			},
			Files: map[string]CritJSONFile{},
		}
		if err := appendReply(&cj, "r1", "actually not done", "alice", "", false, ""); err != nil {
			t.Fatal(err)
		}
		got := cj.ReviewComments[0]
		if got.Resolved {
			t.Error("expected Resolved=false after non-resolving reply")
		}
		if got.ResolvedRound != 0 {
			t.Errorf("ResolvedRound = %d, want 0", got.ResolvedRound)
		}
	})
}

// TestAddReplyClearsResolvedRound asserts that adding a reply (which
// re-opens a resolved comment by setting Resolved=false) also zeroes
// ResolvedRound, mirroring the documented clearing semantics.
func TestAddReplyClearsResolvedRound(t *testing.T) {
	srv, session := newTestServer(t)
	c, _ := session.AddComment("test.md", 1, 1, "", "fix this", "", "", "")

	// First, resolve at round 2.
	session.mu.Lock()
	session.ReviewRound = 2
	session.mu.Unlock()
	if _, ok := session.SetCommentResolved("test.md", c.ID, true); !ok {
		t.Fatal("SetCommentResolved failed")
	}

	// Now add a reply — should re-open and zero the round.
	body := strings.NewReader(`{"body": "actually not done", "author": "bob"}`)
	req := httptest.NewRequest("POST", "/api/comment/"+c.ID+"/replies?path=test.md", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("POST reply: status=%d body=%s", w.Code, w.Body.String())
	}

	// Inspect parent state.
	session.mu.RLock()
	got := session.Files[0].Comments[0]
	session.mu.RUnlock()
	if got.Resolved {
		t.Error("expected Resolved=false after reply")
	}
	if got.ResolvedRound != 0 {
		t.Errorf("ResolvedRound after re-open = %d, want 0", got.ResolvedRound)
	}
}
