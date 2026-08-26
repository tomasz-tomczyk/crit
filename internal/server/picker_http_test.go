package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func withPickerForgeHooks(t *testing.T, detect func() forge.Kind, list func(context.Context) ([]forge.ChangeSummary, error)) {
	t.Helper()
	oldDetect, oldList := DetectForgeKindFn, ListOpenMRsFn
	DetectForgeKindFn, ListOpenMRsFn = detect, list
	t.Cleanup(func() {
		DetectForgeKindFn, ListOpenMRsFn = oldDetect, oldList
	})
}

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

func TestOpenChangesConvertsGitLabMergeRequests(t *testing.T) {
	s, _ := newTestServer(t)
	withPickerForgeHooks(t, nil, func(context.Context) ([]forge.ChangeSummary, error) {
		return []forge.ChangeSummary{{
			Number: 8, Title: "GitLab change", URL: "https://gitlab.example/acme/widget/-/merge_requests/8",
			HeadRefName: "feature", HeadSHA: "head", BaseRefName: "main", Draft: true,
		}}, nil
	})

	changes, gitlabMode, err := s.openChanges(context.Background(), Focus{Forge: string(forge.GitLab)})
	if err != nil {
		t.Fatal(err)
	}
	want := []github.PRSummary{{
		Number: 8, Title: "GitLab change", URL: "https://gitlab.example/acme/widget/-/merge_requests/8",
		HeadRefName: "feature", HeadRefOid: "head", BaseRefName: "main", IsDraft: true,
	}}
	if !gitlabMode || len(changes) != 1 || changes[0] != want[0] {
		t.Fatalf("openChanges = (%+v, %v), want (%+v, true)", changes, gitlabMode, want)
	}
}

func TestOpenChangesDetectsGitLabAndReturnsListError(t *testing.T) {
	s, _ := newTestServer(t)
	wantErr := errors.New("GitLab unavailable")
	withPickerForgeHooks(t, func() forge.Kind { return forge.GitLab }, func(context.Context) ([]forge.ChangeSummary, error) {
		return nil, wantErr
	})

	changes, gitlabMode, err := s.openChanges(context.Background(), Focus{})
	if changes != nil || !gitlabMode || !errors.Is(err, wantErr) {
		t.Fatalf("openChanges = (%+v, %v, %v), want (nil, true, %v)", changes, gitlabMode, err, wantErr)
	}
}

func TestOpenChangesFallsBackToGitHubCache(t *testing.T) {
	s, _ := newTestServer(t)
	s.prList.SeedForTest([]github.PRSummary{})
	withPickerForgeHooks(t, func() forge.Kind { return forge.GitHub }, func(context.Context) ([]forge.ChangeSummary, error) {
		t.Fatal("GitLab list called in GitHub mode")
		return nil, nil
	})

	changes, gitlabMode, err := s.openChanges(context.Background(), Focus{})
	if err != nil || gitlabMode || len(changes) != 0 {
		t.Fatalf("openChanges = (%+v, %v, %v), want ([], false, nil)", changes, gitlabMode, err)
	}
}

func TestHandlePickerReturnsGitLabMRsSeparately(t *testing.T) {
	s, sess := newTestServer(t)
	dir := vcs.InitTestRepo(t)
	sess.RepoRoot = dir
	sess.VCS = &vcs.GitVCS{}
	sess.Focus = Focus{Forge: string(forge.GitLab)}
	s.StoreSessionForTest(sess)
	withPickerForgeHooks(t, nil, func(context.Context) ([]forge.ChangeSummary, error) {
		return []forge.ChangeSummary{{Number: 12, Title: "Remote MR", HeadSHA: "not-in-local-stack", Provider: forge.GitLab}}, nil
	})

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
	if len(resp.OtherPRs) != 0 || len(resp.OtherMRs) != 1 || resp.OtherMRs[0].Number != 12 {
		t.Fatalf("picker PR/MR lists = PRs %+v, MRs %+v", resp.OtherPRs, resp.OtherMRs)
	}
}
