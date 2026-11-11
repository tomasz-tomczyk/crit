package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestHandleRoundComplete_RejectedInRange(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Focus = Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h", DiffScope: session.DiffScopeLayer}
	s.StoreSessionForTest(sess)

	req := httptest.NewRequest("POST", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("status = %d, want 409", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "round-complete is not meaningful in range mode") {
		t.Errorf("error body: %q", body["error"])
	}
}

func TestHandleFocus_FullStackRejectedWithoutDefaultSHA(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"kind":"range","base_sha":"b","head_sha":"h","diff_scope":"full_stack"}`
	req := httptest.NewRequest("POST", "/api/focus", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400 (got body: %q)", w.Code, w.Body.String())
	}
}

func TestHandleFocus_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/focus", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleFocus_BadJSON(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/focus", strings.NewReader(`not json`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSessionInfo_IncludesFocus(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Focus = Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h", DiffScope: DiffScopeLayer, IsStacked: true}
	s.StoreSessionForTest(sess)

	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var info SessionInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Focus.Kind != FocusRange {
		t.Errorf("focus.kind = %q, want %q", info.Focus.Kind, FocusRange)
	}
	if !info.Focus.IsStacked {
		t.Error("expected is_stacked=true")
	}
}

// TestReviewCycle_RangeFollowsHead: a review started with --range base..HEAD
// shows the agent's new commit when the agent starts the next round
// (`crit --session <id>` → POST /api/review-cycle), with the earlier
// comment carried onto it. Before, the daemon kept diffing the commit HEAD
// was at when it started.
func TestReviewCycle_RangeFollowsHead(t *testing.T) {
	vcs.ClearGitEnvForTest(t)
	dir := vcs.InitTestRepo(t)
	base := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.CommitAtForTest(t, dir, "a.go", "one\ntwo\n", "round 1")

	s, sess := newTestServer(t)
	sess.Mode = "git"
	sess.RepoRoot, sess.OutputDir, sess.VCS = dir, dir, &vcs.GitVCS{}
	f, err := focus.ResolveFocus(focus.ChangeSpec{}, base+"..HEAD", "", false, &vcs.GitVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SetFocus(*f); err != nil {
		t.Fatal(err)
	}
	sess.MarkSessionStarted()
	c, ok := sess.AddComment("a.go", 2, 2, "", "on two", "", "r", "")
	if !ok {
		t.Fatal("AddComment failed")
	}
	head2 := vcs.CommitAtForTest(t, dir, "a.go", "zero\none\ntwo\n", "round 2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest("POST", "/api/review-cycle", nil).WithContext(ctx)
		s.ServeHTTP(httptest.NewRecorder(), req)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		sess.RLock()
		head := sess.Focus.HeadSHA
		sess.RUnlock()
		if head == head2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("focus head = %s after review-cycle; want %s", head, head2)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	// The round clears in-memory comments and reloads them from the review
	// file, so the carried comment must be on disk under the new head.
	data, err := session.ReadFileShared(session.ReviewPathsFor(sess.CritJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, dc := range cj.Files["a.go"].Comments {
		if dc.ID == c.ID {
			found = true
			if dc.HeadSHA != head2 || dc.StartLine != 3 {
				t.Errorf("saved comment head=%s line=%d; want %s line 3", dc.HeadSHA, dc.StartLine, head2)
			}
		}
	}
	if !found {
		t.Errorf("comment %s not in the saved review file", c.ID)
	}
}

// TestReviewCycle_RangeHeadRefGone: a followed ref that no longer resolves
// doesn't stop the round; the review stays on its commit.
func TestReviewCycle_RangeHeadRefGone(t *testing.T) {
	vcs.ClearGitEnvForTest(t)
	dir := vcs.InitTestRepo(t)
	base := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	vcs.GitRun(t, dir, "checkout", "-q", "-b", "feat")
	head := vcs.CommitAtForTest(t, dir, "a.go", "one\n", "round 1")

	s, sess := newTestServer(t)
	sess.Mode = "git"
	sess.RepoRoot, sess.OutputDir, sess.VCS = dir, dir, &vcs.GitVCS{}
	f, err := focus.ResolveFocus(focus.ChangeSpec{}, base+"..feat", "", false, &vcs.GitVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SetFocus(*f); err != nil {
		t.Fatal(err)
	}
	sess.MarkSessionStarted()
	vcs.GitRun(t, dir, "checkout", "-q", "main")
	vcs.GitRun(t, dir, "branch", "-q", "-D", "feat")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest("POST", "/api/review-cycle", nil).WithContext(ctx)
		s.ServeHTTP(httptest.NewRecorder(), req)
	}()
	// The round still starts: the session waits for the reviewer.
	waitForSubscriberCount(t, sess, 1)
	cancel()
	<-done

	sess.RLock()
	got := sess.Focus.HeadSHA
	sess.RUnlock()
	if got != head {
		t.Errorf("focus head = %s; want it unchanged at %s", got, head)
	}
}
