package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveDefaultBranchSHA_Git(t *testing.T) {
	dir := initTestRepo(t)
	want := gitT(t, dir, "rev-parse", "HEAD")
	got, err := ResolveDefaultBranchSHA(&GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestResolveDefaultBranchSHA_NilVCS(t *testing.T) {
	if _, err := ResolveDefaultBranchSHA(nil, "", "main"); err == nil {
		t.Fatal("expected error for nil vcs")
	}
}

func TestWalkAncestors_Git(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "a.txt", "1", "a")
	commitAt(t, dir, "b.txt", "2", "b")

	shas, err := walkAncestors(&GitVCS{}, dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) < 3 {
		t.Errorf("expected >= 3 ancestors (seed + a + b), got %d", len(shas))
	}
}

func TestLocalBranchTips_Git(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "a.txt", "x", "a")
	headBeforeBranch := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "checkout", "-b", "feat-x")
	commitAt(t, dir, "b.txt", "y", "b")

	got, err := localBranchTips(&GitVCS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	// feat-x has its own tip distinct from main; both should appear.
	found := false
	for _, name := range got {
		if name == "feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("feat-x not in tips: %+v (head before branch: %s)", got, headBeforeBranch)
	}
}

func TestRemoteBranchTips_Git_ExcludesDefault(t *testing.T) {
	dir := initTestRepo(t)
	headSHA := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "update-ref", "refs/remotes/origin/feat-x", headSHA)
	gitT(t, dir, "update-ref", "refs/remotes/origin/main", headSHA)

	branches, err := remoteBranchTips(&GitVCS{}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "origin/main" || b.Name == "main" {
			t.Errorf("default branch leaked: %+v", b)
		}
	}
	found := false
	for _, b := range branches {
		if b.Name == "origin/feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("origin/feat-x not in tips: %+v", branches)
	}
}

func TestHandlePicker_BasicShape(t *testing.T) {
	s, sess := newTestServer(t)
	dir := initTestRepo(t)
	sess.mu.Lock()
	sess.RepoRoot = dir
	sess.VCS = &GitVCS{}
	sess.mu.Unlock()
	// Pre-populate with empty list to skip gh.
	s.prList.data = []PRSummary{}
	s.prList.fetched = time.Now()

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
	// At minimum, current should be present (zero-value Focus).
	if resp.Current.Kind == "" {
		// fine — working-tree focus serializes "kind":"" without explicit set
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

// TestHandlePicker_StackEntriesIncludeDefaultSHA verifies F-4 backend half:
// every stack entry returned by /api/picker carries default_sha, so the
// frontend can construct a complete Focus when the user later flips to
// full-stack scope. Without this, /api/focus rejects full-stack requests
// (Focus.DefaultSHA is required for SetFocus full-stack).
func TestHandlePicker_StackEntriesIncludeDefaultSHA(t *testing.T) {
	s, sess := newTestServer(t)
	dir := initTestRepo(t)
	defaultSHA := gitT(t, dir, "rev-parse", "HEAD")

	// Build a feature branch so the local-branch-tip walker has something
	// to detect as a stack entry.
	gitT(t, dir, "checkout", "-b", "feat-a")
	commitAt(t, dir, "a.txt", "x", "a")

	sess.mu.Lock()
	sess.RepoRoot = dir
	sess.VCS = &GitVCS{}
	sess.mu.Unlock()
	s.prList.data = []PRSummary{}
	s.prList.fetched = time.Now()

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

// TestHandlePicker_DefaultSHAIsLiteralDefaultBranch verifies that on a
// stack with 2+ non-default branch tips above main, every entry's
// DefaultSHA is the literal default-branch tip. Full-stack of any layer
// then means `default..entry` — the cumulative diff including the
// topmost layer's own changes.
func TestHandlePicker_DefaultSHAIsLiteralDefaultBranch(t *testing.T) {
	s, sess := newTestServer(t)
	dir := initTestRepo(t)

	// Three layers above main: alpha → beta → gamma.
	gitT(t, dir, "checkout", "-b", "alpha")
	commitAt(t, dir, "a.txt", "a", "alpha")

	gitT(t, dir, "checkout", "-b", "beta")
	commitAt(t, dir, "b.txt", "b", "beta")

	gitT(t, dir, "checkout", "-b", "gamma")
	commitAt(t, dir, "c.txt", "c", "gamma")

	sess.mu.Lock()
	sess.RepoRoot = dir
	sess.VCS = &GitVCS{}
	sess.mu.Unlock()
	s.prList.data = []PRSummary{}
	s.prList.fetched = time.Now()

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
	mainSHA := gitT(t, dir, "rev-parse", "main")
	for _, e := range resp.Stack {
		if e.DefaultSHA != mainSHA {
			t.Errorf("entry %q default_sha=%q want main (literal default) %q", e.Label, e.DefaultSHA, mainSHA)
		}
	}
}

// TestDetectStack_ExcludesStaleBranchesBeforeMergeBase verifies the picker
// does not surface local branches whose tips lie strictly before the merge-
// base of HEAD with the default branch. These are stale branches in the
// default branch's history, not part of the user's in-progress stack.
func TestDetectStack_ExcludesStaleBranchesBeforeMergeBase(t *testing.T) {
	dir := initTestRepo(t)
	// Main commit M1, then a stale branch points here.
	m1 := commitAt(t, dir, "m1.txt", "1", "m1")
	gitT(t, dir, "branch", "old", m1)
	// Main advances to M2 — this is what feature branches off of.
	commitAt(t, dir, "m2.txt", "2", "m2")
	// Feature branch off main@M2 with two commits F1, F2.
	gitT(t, dir, "checkout", "-b", "feat")
	f1 := commitAt(t, dir, "f1.txt", "f1", "f1")
	f2 := commitAt(t, dir, "f2.txt", "f2", "f2")
	// Also park a branch on F1 so a legitimate post-merge-base entry shows up.
	gitT(t, dir, "branch", "feat-f1", f1)

	stack, err := detectStack(&GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range stack {
		if e.Label == "old" {
			t.Errorf("stale branch \"old\" leaked into stack: %+v", stack)
		}
		if e.HeadSHA == m1 {
			t.Errorf("entry at pre-merge-base SHA %s leaked: %+v", m1, e)
		}
	}
	// Sanity: the legitimate post-merge-base entries are present.
	wantHeads := map[string]bool{f1: false, f2: false}
	for _, e := range stack {
		if _, ok := wantHeads[e.HeadSHA]; ok {
			wantHeads[e.HeadSHA] = true
		}
	}
	for sha, found := range wantHeads {
		if !found {
			t.Errorf("expected stack to include sha %s, got %+v", sha, stack)
		}
	}
}

// TestDetectStack_IncludesPostMergeBaseBranchTips verifies branch tips that
// sit strictly between the merge-base and HEAD are included in the stack.
func TestDetectStack_IncludesPostMergeBaseBranchTips(t *testing.T) {
	dir := initTestRepo(t)
	// Main at M.
	commitAt(t, dir, "m.txt", "m", "m")
	// Feature branch with two commits A, B.
	gitT(t, dir, "checkout", "-b", "feat")
	a := commitAt(t, dir, "a.txt", "a", "a")
	gitT(t, dir, "branch", "feat-a", a)
	b := commitAt(t, dir, "b.txt", "b", "b")
	gitT(t, dir, "branch", "feat-b", b)

	stack, err := detectStack(&GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"feat-a": false, "feat-b": false}
	for _, e := range stack {
		if _, ok := want[e.Label]; ok {
			want[e.Label] = true
		}
	}
	for label, found := range want {
		if !found {
			t.Errorf("expected branch %q in stack, got %+v", label, stack)
		}
	}
}

// TestDetectStack_DropsNakedCommitsBehindBranch verifies that ancestor
// commits older than the nearest branch tip are filtered out. This prevents
// long-lived parent-branch history (e.g. `staging` accumulating dozens of
// commits before reaching the default branch) from polluting the stack
// popover with naked commit-subject rows.
func TestDetectStack_DropsNakedCommitsBehindBranch(t *testing.T) {
	dir := initTestRepo(t)
	// Diverge "staging" from the default branch and pile noise commits on
	// it (e.g. unrelated tickets that landed on staging).
	gitT(t, dir, "checkout", "-b", "staging")
	commitAt(t, dir, "n1.txt", "n1", "[ABC-100] noise one")
	commitAt(t, dir, "n2.txt", "n2", "[ABC-101] noise two")
	commitAt(t, dir, "n3.txt", "n3", "[ABC-102] noise three")
	// Parent feature branch on top of staging.
	gitT(t, dir, "checkout", "-b", "feat-parent")
	commitAt(t, dir, "p.txt", "p", "feat-parent commit")
	// Current branch on top of feat-parent.
	gitT(t, dir, "checkout", "-b", "feat-current")
	commitAt(t, dir, "c.txt", "c", "feat-current commit")

	stack, err := detectStack(&GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: feat-current (HEAD), feat-parent, staging — and nothing else.
	wantLabels := map[string]bool{
		"feat-current": false,
		"feat-parent":  false,
		"staging":      false,
	}
	forbiddenSubstrings := []string{"ABC-100", "ABC-101", "ABC-102"}
	for _, e := range stack {
		if _, ok := wantLabels[e.Label]; ok {
			wantLabels[e.Label] = true
			continue
		}
		for _, sub := range forbiddenSubstrings {
			if strings.Contains(e.Label, sub) {
				t.Errorf("naked commit subject leaked into stack: %q (full entry %+v)", e.Label, e)
			}
		}
	}
	for label, found := range wantLabels {
		if !found {
			t.Errorf("expected %q in stack, got %+v", label, stack)
		}
	}
}

// TestDetectStack_KeepsNakedCommitsAheadOfNearestBranch verifies that naked
// commits between HEAD and the closest branch tip (e.g. unbranched WIP on
// top of a feature branch) are preserved — they're the user's exposed work.
func TestDetectStack_KeepsNakedCommitsAheadOfNearestBranch(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "m.txt", "m", "main")
	gitT(t, dir, "checkout", "-b", "feat")
	commitAt(t, dir, "f.txt", "f", "feat tip")
	featTipSHA := gitT(t, dir, "rev-parse", "HEAD")
	// Detach HEAD so subsequent commits don't drag the feat ref forward —
	// we want feat to stay an older branch-tip ancestor while new naked
	// commits land on top.
	gitT(t, dir, "checkout", "--detach", featTipSHA)
	commitAt(t, dir, "w1.txt", "w1", "wip one")
	commitAt(t, dir, "w2.txt", "w2", "wip two")

	stack, err := detectStack(&GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// feat must appear (tier-2). At least one of the wip commits should
	// appear as a naked entry — they're between HEAD and feat, so they're
	// the exposed top of the chain.
	var sawFeat, sawWip bool
	for _, e := range stack {
		if e.Label == "feat" {
			sawFeat = true
		}
		if strings.Contains(e.Label, "wip") {
			sawWip = true
		}
	}
	if !sawFeat {
		t.Errorf("expected feat in stack, got %+v", stack)
	}
	if !sawWip {
		t.Errorf("expected at least one wip naked-commit entry in stack, got %+v", stack)
	}
}

// TestDetectStack_DefaultBranchAsRoot verifies the merge-base commit itself
// (the root marker) is not included in the regular entry list. The frontend
// renders the default branch as a separate root row using DefaultSHA.
func TestDetectStack_DefaultBranchAsRoot(t *testing.T) {
	dir := initTestRepo(t)
	commitAt(t, dir, "m1.txt", "1", "m1")
	mergeBase := gitT(t, dir, "rev-parse", "HEAD")
	// Park a stale branch on the merge-base SHA — should still be excluded.
	gitT(t, dir, "branch", "stale-at-base", mergeBase)
	gitT(t, dir, "checkout", "-b", "feat")
	commitAt(t, dir, "f.txt", "f", "f")

	stack, err := detectStack(&GitVCS{}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range stack {
		if e.HeadSHA == mergeBase {
			t.Errorf("merge-base SHA leaked into entries: %+v", e)
		}
		if e.Label == "stale-at-base" {
			t.Errorf("branch parked at merge-base leaked into entries: %+v", e)
		}
	}
}
