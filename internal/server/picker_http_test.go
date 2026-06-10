package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestHandlePicker_BasicShape(t *testing.T) {
	s, sess := newTestServer(t)
	dir := vcs.InitTestRepo(t)
	sess.RepoRoot = dir
	sess.VCS = &vcs.GitVCS{}
	s.StoreSessionForTest(sess)
	s.SeedPRListForTest()

	req := httptest.NewRequest("GET", "/api/picker", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp pickerResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Current.Kind == "" {
		_ = resp
	}
}

func TestHandlePicker_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/picker", strings.NewReader(""))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status=%d want 405", w.Code)
	}
}

func TestHandlePicker_StackEntriesIncludeDefaultSHA(t *testing.T) {
	s, sess := newTestServer(t)
	dir := vcs.InitTestRepo(t)
	defaultSHA := vcs.GitRun(t, dir, "rev-parse", "HEAD")

	vcs.GitRun(t, dir, "checkout", "-b", "feat-a")
	vcs.CommitAtForTest(t, dir, "a.txt", "x", "a")

	sess.RepoRoot = dir
	sess.VCS = &vcs.GitVCS{}
	s.StoreSessionForTest(sess)
	s.SeedPRListForTest()

	req := httptest.NewRequest("GET", "/api/picker", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp pickerResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Stack) == 0 {
		t.Fatalf("expected at least one stack entry, got %+v", resp)
	}
	for _, e := range resp.Stack {
		if e.DefaultSHA == "" {
			t.Errorf("stack entry %q missing default_sha", e.Label)
		}
		if e.DefaultSHA != defaultSHA {
			t.Errorf("entry %q default_sha=%q want %q", e.Label, e.DefaultSHA, defaultSHA)
		}
	}
}

func TestHandlePicker_DefaultSHAIsLiteralDefaultBranch(t *testing.T) {
	s, sess := newTestServer(t)
	dir := vcs.InitTestRepo(t)

	vcs.GitRun(t, dir, "checkout", "-b", "alpha")
	vcs.CommitAtForTest(t, dir, "a.txt", "a", "alpha")

	vcs.GitRun(t, dir, "checkout", "-b", "beta")
	vcs.CommitAtForTest(t, dir, "b.txt", "b", "beta")

	vcs.GitRun(t, dir, "checkout", "-b", "gamma")
	vcs.CommitAtForTest(t, dir, "c.txt", "c", "gamma")

	sess.RepoRoot = dir
	sess.VCS = &vcs.GitVCS{}
	s.StoreSessionForTest(sess)
	s.SeedPRListForTest()

	req := httptest.NewRequest("GET", "/api/picker", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp pickerResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Stack) < 2 {
		t.Fatalf("expected 2+ stack entries, got %d: %+v", len(resp.Stack), resp.Stack)
	}
	mainSHA := vcs.GitRun(t, dir, "rev-parse", "main")
	for _, e := range resp.Stack {
		if e.DefaultSHA != mainSHA {
			t.Errorf("entry %q default_sha=%q want main (literal default) %q", e.Label, e.DefaultSHA, mainSHA)
		}
	}
}
