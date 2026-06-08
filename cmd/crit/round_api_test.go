package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// newRoundsTestServer returns a server with a files-mode session preloaded
// with R1 and R2 snapshots for "test.md" so handlers can be exercised with
// ?round=N. The current FileEntry.Content matches R2.
func newRoundsTestServer(t *testing.T) (*Server, *Session) {
	t.Helper()
	s, sess := newTestServer(t)
	// Set R1 (initial content) and R2 (after one edit) snapshots.
	// File current Content from newTestServer is "line1\nline2\nline3\n".
	r1 := "line1\nline2\nline3\n"
	r2 := "line1\nlineTWO\nline3\nline4\n"
	sess.Files[0].Content = r2
	sess.RoundSnapshots = map[string]map[int]RoundSnapshot{
		"test.md": {
			1: {Content: r1, Status: "modified"},
			2: {Content: r2, Status: "modified"},
		},
	}
	sess.ReviewRound = 2
	return s, sess
}

func TestHandleFileDiff_Round_FilesMode(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["hunks"]; !ok {
		t.Fatalf("hunks missing: %v", resp)
	}
	if got, _ := resp["previous_content"].(string); got == "" {
		t.Fatalf("previous_content empty for R2: %v", resp)
	}
	hunks, _ := resp["hunks"].([]any)
	if len(hunks) == 0 {
		t.Fatalf("expected diff hunks for R1->R2, got none")
	}
}

func TestHandleFileDiff_Round_InvalidParam(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=abc", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("invalid round: status=%d", w.Code)
	}
}

func TestHandleFileDiff_Round_OutOfRange(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=99", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("out-of-range round: status=%d", w.Code)
	}
}

func TestHandleFileDiff_Round_R1NoPrevious(t *testing.T) {
	// R1 is the baseline — no previous content; diff should be empty hunks.
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file/diff?path=test.md&round=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	hunks, _ := resp["hunks"].([]any)
	if len(hunks) != 0 {
		t.Fatalf("R1 baseline must have no diff hunks, got %v", hunks)
	}
}

func TestHandleSession_Round_FilesMode(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	// Add an extra file that exists only in R2 (not in R1).
	sess.Files = append(sess.Files, &FileEntry{
		Path:     "new.md",
		Status:   "added",
		FileType: "markdown",
		Content:  "n",
		Comments: []Comment{},
	})
	sess.RoundSnapshots["new.md"] = map[int]RoundSnapshot{
		2: {Content: "n", Status: "added"},
	}

	req := httptest.NewRequest("GET", "/api/session?round=1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var info SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	for _, f := range info.Files {
		if f.Path == "new.md" {
			t.Fatalf("new.md should be hidden at R1, files=%v", info.Files)
		}
	}
}

func TestHandleRounds_LineStats(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/rounds", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var resp struct {
		CurrentRound int `json:"current_round"`
		Rounds       []struct {
			N         int `json:"n"`
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(resp.Rounds))
	}
	r1 := resp.Rounds[0]
	r2 := resp.Rounds[1]
	if r1.N != 1 || r1.Additions != 0 || r1.Deletions != 0 {
		t.Errorf("R1 must be baseline (zero stats): %+v", r1)
	}
	if r2.N != 2 {
		t.Errorf("expected R2 second, got %+v", r2)
	}
	if r2.Additions == 0 && r2.Deletions == 0 {
		t.Errorf("R2 must have non-zero line stats: %+v", r2)
	}
}

// TestCommentsAtOrBeforeRound_ResolutionStateIsCurrent is a regression test
// for review W3. Stage 1 deliberately exposes the *current* Resolved /
// ResolvedRound values on every comment, not the state as of the requested
// round, so the frontend can compute round-faithful resolution from
// ResolvedRound itself (Stage 2 work). This test pins that contract: a
// comment authored at R1 and resolved at R3 must surface from
// /api/file/comments?round=1 with resolved=true and resolved_round=3.
//
// If a future change starts rewriting Resolved server-side based on the
// requested round, this test will fail and force us to update the docs in
// commentsAtOrBeforeRound and the corresponding Stage 2 frontend code.
func TestCommentsAtOrBeforeRound_ResolutionStateIsCurrent(t *testing.T) {
	srv, sess := newRoundsTestServer(t)
	// Add a comment at R1 (the seeded session ReviewRound is 2 from the
	// helper, so set it to 1 first to author).
	sess.mu.Lock()
	sess.ReviewRound = 1
	sess.mu.Unlock()
	c, ok := sess.AddComment("test.md", 1, 1, "", "fix this", "alice", "", "")
	if !ok {
		t.Fatal("AddComment failed")
	}

	// Advance to R3 and resolve.
	sess.mu.Lock()
	sess.ReviewRound = 3
	sess.mu.Unlock()
	if _, ok := sess.SetCommentResolved("test.md", c.ID, true); !ok {
		t.Fatal("SetCommentResolved failed")
	}

	// Fetch with ?round=1 — Stage 1 contract: current resolution state, not state-at-R1.
	req := httptest.NewRequest("GET", "/api/file/comments?path=test.md&round=1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got []Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 comment at round=1, got %d", len(got))
	}
	if !got[0].Resolved {
		t.Errorf("Resolved at round=1 = false; want true (Stage 1 exposes current state)")
	}
	if got[0].ResolvedRound != 3 {
		t.Errorf("ResolvedRound at round=1 = %d; want 3 (Stage 1 exposes the round of resolution)", got[0].ResolvedRound)
	}
}

// TestRoundParam_RejectInvalidConsistently is a regression test for review W2.
// The four round-aware endpoints must reject malformed ?round values
// uniformly (400) and accept an empty value (back-compat). Previously
// /api/file and /api/file/diff returned 400 on garbage while
// /api/file/comments and /api/comments silently ignored it, leaving
// callers with two contradictory contracts.
func TestRoundParam_RejectInvalidConsistently(t *testing.T) {
	endpoints := []string{
		"/api/session",
		"/api/file?path=test.md",
		"/api/file/diff?path=test.md",
		"/api/file/comments?path=test.md",
		"/api/comments",
	}
	cases := []struct {
		name       string
		round      string // raw query value
		omit       bool   // if true, append no round param at all
		wantStatus int
	}{
		{name: "absent", omit: true, wantStatus: 200},
		{name: "empty", round: "", wantStatus: 200},
		{name: "garbage", round: "abc", wantStatus: 400},
		{name: "negative", round: "-1", wantStatus: 400},
		{name: "zero", round: "0", wantStatus: 400},
		// A round number that doesn't have a snapshot is valid syntax
		// (well-formed integer >= 1); the file-scoped endpoints translate
		// that to 404 "file_not_in_round", and the list endpoints just
		// return an empty list. Either way it's NOT a 400.
		{name: "very_large", round: "99999", wantStatus: 0},
	}
	for _, ep := range endpoints {
		ep := ep
		for _, tc := range cases {
			tc := tc
			t.Run(ep+"/"+tc.name, func(t *testing.T) {
				srv, _ := newRoundsTestServer(t)
				url := ep
				if !tc.omit {
					sep := "?"
					if strings.Contains(ep, "?") {
						sep = "&"
					}
					url = ep + sep + "round=" + tc.round
				}
				req := httptest.NewRequest("GET", url, nil)
				w := httptest.NewRecorder()
				srv.ServeHTTP(w, req)
				if tc.wantStatus == 400 {
					if w.Code != 400 {
						t.Errorf("%s round=%q: status=%d, want 400 (body=%s)", ep, tc.round, w.Code, w.Body.String())
					}
					return
				}
				if tc.wantStatus == 200 && w.Code != 200 {
					t.Errorf("%s round=%q: status=%d, want 200 (body=%s)", ep, tc.round, w.Code, w.Body.String())
				}
				// wantStatus == 0: accept any non-400.
				if tc.wantStatus == 0 && w.Code == 400 {
					t.Errorf("%s round=%q: unexpectedly returned 400 (body=%s)", ep, tc.round, w.Body.String())
				}
			})
		}
	}
}
