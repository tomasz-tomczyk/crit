package session

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// subThreadTree is a thread on plan.md with sub-threads at several depths,
// plus an unrelated thread with its own sub-thread.
type subThreadTree struct {
	root, subOnBody, subOnReply, deep, deeper, other, otherSub Comment
	reply, subReply                                            Reply
}

func addSubThreadTree(t *testing.T, s *Session) subThreadTree {
	t.Helper()
	var tr subThreadTree
	tr.root, _ = s.AddComment("plan.md", 1, 1, "", "root body", "", "reviewer", "")
	tr.reply, _ = s.AddReply("plan.md", tr.root.ID, "agent reply", "agent", "")
	mustSub := func(parentID, body string) Comment {
		t.Helper()
		c, ok := s.AddSubThread("plan.md", parentID, body, "passage", nil, "reviewer", "")
		if !ok {
			t.Fatalf("AddSubThread(%s) failed", parentID)
		}
		return c
	}
	tr.subOnBody = mustSub(tr.root.ID, "on the root body")
	tr.subOnReply = mustSub(tr.reply.ID, "on the reply")
	tr.subReply, _ = s.AddReply("plan.md", tr.subOnReply.ID, "sub-thread reply", "agent", "")
	tr.deep = mustSub(tr.subReply.ID, "on a reply of a sub-thread")
	tr.deeper = mustSub(tr.deep.ID, "on a sub-thread body")
	tr.other, _ = s.AddComment("plan.md", 3, 3, "", "other root", "", "reviewer", "")
	tr.otherSub = mustSub(tr.other.ID, "on the other root")
	return tr
}

func commentIDs(comments []Comment) []string {
	ids := make([]string, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	return ids
}

func sortedIDs(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func TestAddSubThread_TakesRootThreadFields(t *testing.T) {
	s := newTestSession(t)
	s.ReviewRound = 3
	root, _ := s.AddComment("main.go", 2, 3, "old", "root", "", "reviewer", "")
	s.mu.Lock()
	s.Files[1].Comments[0].FocusKey = "pr:7"
	s.Files[1].Comments[0].DiffScope = "layer"
	s.Files[1].Comments[0].HeadSHA = "headsha"
	s.mu.Unlock()
	reply, _ := s.AddReply("main.go", root.ID, "long agent answer", "agent", "")

	offset := 5
	sub, ok := s.AddSubThread("main.go", reply.ID, "why?", "agent answer", &offset, "reviewer", "u1")
	if !ok {
		t.Fatal("AddSubThread on a reply failed")
	}
	if !strings.HasPrefix(sub.ID, "c_") || sub.ParentID != reply.ID {
		t.Fatalf("sub-thread id/parent = %q/%q", sub.ID, sub.ParentID)
	}
	if sub.StartLine != 0 || sub.EndLine != 0 {
		t.Errorf("sub-thread lines = %d-%d, want 0-0", sub.StartLine, sub.EndLine)
	}
	if sub.Quote != "agent answer" || sub.QuoteOffset == nil || *sub.QuoteOffset != 5 {
		t.Errorf("quote = %q offset = %v", sub.Quote, sub.QuoteOffset)
	}
	if sub.Side != "old" || sub.Scope != "line" || sub.FocusKey != "pr:7" || sub.DiffScope != "layer" || sub.HeadSHA != "headsha" {
		t.Errorf("root thread fields not copied: %+v", sub)
	}
	if sub.ReviewRound != 3 || sub.Author != "reviewer" || sub.UserID != "u1" {
		t.Errorf("round/author = %d/%q/%q", sub.ReviewRound, sub.Author, sub.UserID)
	}

	// A sub-thread on the sub-thread's own body still walks up to the root.
	nested, ok := s.AddSubThread("main.go", sub.ID, "nested", "why", nil, "agent", "")
	if !ok || nested.FocusKey != "pr:7" || nested.Side != "old" || nested.ParentID != sub.ID {
		t.Fatalf("nested sub-thread = %+v, ok=%v", nested, ok)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Files[1].Comments); got != 3 {
		t.Errorf("main.go holds %d comments, want root + 2 sub-threads", got)
	}
}

func TestAddSubThread_ReviewLevel(t *testing.T) {
	s := newTestSession(t)
	root := s.AddReviewComment("general", "reviewer", "")
	reply, _ := s.AddReviewCommentReply(root.ID, "answer", "agent", "")
	sub, ok := s.AddSubThread("", reply.ID, "on the answer", "answer", nil, "reviewer", "")
	if !ok {
		t.Fatal("AddSubThread on a review-level reply failed")
	}
	if !strings.HasPrefix(sub.ID, "r_") || sub.Scope != "review" || sub.ParentID != reply.ID {
		t.Errorf("review-level sub-thread = %+v", sub)
	}
	if got := len(s.GetReviewComments()); got != 2 {
		t.Errorf("review comments = %d, want root + sub-thread", got)
	}
}

func TestAddSubThread_ParentMustBeInTheSameList(t *testing.T) {
	s := newTestSession(t)
	fileRoot, _ := s.AddComment("plan.md", 1, 1, "", "file root", "", "", "")
	reviewRoot := s.AddReviewComment("review root", "", "")
	cases := []struct {
		name, path, parent string
	}{
		{"unknown parent", "plan.md", "c_nope"},
		{"parent in another file", "main.go", fileRoot.ID},
		{"unknown file", "missing.go", fileRoot.ID},
		{"file parent for a review-level sub-thread", "", fileRoot.ID},
		{"review parent for a file sub-thread", "plan.md", reviewRoot.ID},
	}
	for _, tc := range cases {
		if c, ok := s.AddSubThread(tc.path, tc.parent, "body", "q", nil, "", ""); ok {
			t.Errorf("%s: AddSubThread succeeded with %+v", tc.name, c)
		}
	}
	if got := len(s.GetComments("plan.md")) + len(s.GetComments("main.go")) + len(s.GetReviewComments()); got != 2 {
		t.Errorf("rejected sub-threads were stored: %d comments", got)
	}
}

func TestDeleteComment_CascadesSubThreadsWithTombstones(t *testing.T) {
	s := newTestSession(t)
	tr := addSubThreadTree(t, s)
	if !s.DeleteComment("plan.md", tr.root.ID) {
		t.Fatal("DeleteComment failed")
	}
	if got, want := commentIDs(s.GetComments("plan.md")), sortedIDs(tr.other.ID, tr.otherSub.ID); !slices.Equal(got, want) {
		t.Errorf("remaining comments = %v, want %v", got, want)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range []string{tr.root.ID, tr.subOnBody.ID, tr.subOnReply.ID, tr.deep.ID, tr.deeper.ID} {
		if _, ok := s.deletedCommentIDs["plan.md"][id]; !ok {
			t.Errorf("no tombstone for %s", id)
		}
	}
}

func TestDeleteFileCommentAs_CascadesSubThreads(t *testing.T) {
	s := newTestSession(t)
	tr := addSubThreadTree(t, s)
	if got := s.DeleteFileCommentAs("plan.md", tr.subOnReply.ID, ""); got != deleteResultDeleted {
		t.Fatalf("DeleteFileCommentAs = %v", got)
	}
	want := sortedIDs(tr.root.ID, tr.subOnBody.ID, tr.other.ID, tr.otherSub.ID)
	if got := commentIDs(s.GetComments("plan.md")); !slices.Equal(got, want) {
		t.Errorf("remaining comments = %v, want %v", got, want)
	}
}

func TestDeleteReply_CascadesItsSubThreadsOnly(t *testing.T) {
	s := newTestSession(t)
	tr := addSubThreadTree(t, s)
	if !s.DeleteReply("plan.md", tr.root.ID, tr.reply.ID) {
		t.Fatal("DeleteReply failed")
	}
	want := sortedIDs(tr.root.ID, tr.subOnBody.ID, tr.other.ID, tr.otherSub.ID)
	if got := commentIDs(s.GetComments("plan.md")); !slices.Equal(got, want) {
		t.Errorf("remaining comments = %v, want %v", got, want)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range []string{tr.subOnReply.ID, tr.deep.ID, tr.deeper.ID} {
		if _, ok := s.deletedCommentIDs["plan.md"][id]; !ok {
			t.Errorf("no tombstone for %s", id)
		}
	}
}

func TestDeleteReviewComment_CascadesSubThreads(t *testing.T) {
	s := newTestSession(t)
	root := s.AddReviewComment("root", "", "")
	reply, _ := s.AddReviewCommentReply(root.ID, "answer", "agent", "")
	onBody, _ := s.AddSubThread("", root.ID, "on body", "root", nil, "", "")
	onReply, _ := s.AddSubThread("", reply.ID, "on reply", "answer", nil, "", "")
	nested, _ := s.AddSubThread("", onReply.ID, "nested", "on", nil, "", "")
	other := s.AddReviewComment("other", "", "")

	if !s.DeleteReviewCommentReply(root.ID, reply.ID) {
		t.Fatal("DeleteReviewCommentReply failed")
	}
	if got, want := commentIDs(s.GetReviewComments()), sortedIDs(root.ID, onBody.ID, other.ID); !slices.Equal(got, want) {
		t.Errorf("after reply delete = %v, want %v", got, want)
	}
	if !s.DeleteReviewComment(root.ID) {
		t.Fatal("DeleteReviewComment failed")
	}
	if got := commentIDs(s.GetReviewComments()); !slices.Equal(got, []string{other.ID}) {
		t.Errorf("after root delete = %v, want only %s (nested %s gone)", got, other.ID, nested.ID)
	}
}

func TestSubThreadResolution_IsIndependentAndCounted(t *testing.T) {
	s := newTestSession(t)
	root, _ := s.AddComment("plan.md", 1, 1, "", "root", "", "", "")
	sub, _ := s.AddSubThread("plan.md", root.ID, "sub", "root", nil, "", "")
	reviewRoot := s.AddReviewComment("review root", "", "")
	reviewSub, _ := s.AddSubThread("", reviewRoot.ID, "review sub", "review", nil, "", "")

	if got := s.TotalCommentCount(); got != 4 {
		t.Errorf("TotalCommentCount = %d, want 4", got)
	}
	s.SetCommentResolved("plan.md", root.ID, true)
	s.ResolveReviewComment(reviewRoot.ID, true)
	if got := s.UnresolvedCommentCount(); got != 2 {
		t.Fatalf("UnresolvedCommentCount = %d, want the 2 open sub-threads", got)
	}
	if c, _, _ := s.FindCommentByID(sub.ID, "plan.md"); c.Resolved {
		t.Error("resolving the parent resolved its sub-thread")
	}

	s.SetCommentResolved("plan.md", sub.ID, true)
	s.ResolveReviewComment(reviewSub.ID, true)
	if got := s.UnresolvedCommentCount(); got != 0 {
		t.Fatalf("UnresolvedCommentCount = %d, want 0", got)
	}
	s.SetCommentResolved("plan.md", root.ID, false)
	if c, _, _ := s.FindCommentByID(sub.ID, "plan.md"); !c.Resolved {
		t.Error("reopening the parent reopened its sub-thread")
	}
	if c, _, _ := s.FindCommentByID(root.ID, "plan.md"); c.Resolved {
		t.Error("parent did not reopen")
	}
	if got := s.UnresolvedCommentCount(); got != 1 {
		t.Errorf("UnresolvedCommentCount = %d, want 1", got)
	}
}

// A sub-thread has no lines; the line remap must not move it to line 1.
func TestCarryForwardFileComments_SubThreadKeepsNoLines(t *testing.T) {
	s := newTestSession(t)
	f := s.Files[0]
	s.mu.Lock()
	f.PreviousContent = "alpha\nbeta\ngamma\n"
	f.Content = "new first line\nalpha\nbeta\ngamma\n"
	f.PreviousComments = []Comment{
		{ID: "c_root", StartLine: 2, EndLine: 2, Anchor: "beta", Scope: "line", Body: "root"},
		{ID: "c_sub", ParentID: "c_root", Scope: "line", Body: "sub", Quote: "root"},
	}
	f.Comments = nil
	s.mu.Unlock()

	s.carryForwardFileComments(f)

	byID := map[string]Comment{}
	for _, c := range s.GetComments("plan.md") {
		byID[c.ID] = c
	}
	if root := byID["c_root"]; root.StartLine != 3 {
		t.Errorf("root carried to line %d, want 3", root.StartLine)
	}
	sub, ok := byID["c_sub"]
	if !ok {
		t.Fatal("sub-thread not carried forward")
	}
	if sub.StartLine != 0 || sub.EndLine != 0 || sub.Drifted || sub.ParentID != "c_root" || !sub.CarriedForward {
		t.Errorf("carried sub-thread = %+v", sub)
	}
}

// A review file written before sub-threads existed reads and writes back
// without losing or adding anything.
func TestCritJSON_LegacyReviewRoundTrips(t *testing.T) {
	legacy := `{
  "branch": "main",
  "base_ref": "abc",
  "updated_at": "2026-01-01T00:00:00Z",
  "review_round": 2,
  "review_comments": [
    {"id": "r_1", "start_line": 0, "end_line": 0, "body": "general", "scope": "review",
     "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
     "replies": [{"id": "rp_9", "body": "ok", "author": "agent", "created_at": "2026-01-01T00:00:00Z", "review_round": 2}]}
  ],
  "files": {
    "plan.md": {"status": "modified", "file_hash": "sha256:x", "comments": [
      {"id": "c_1", "start_line": 3, "end_line": 4, "side": "old", "body": "fix", "quote": "the", "quote_offset": 7,
       "anchor": "line", "author": "me", "scope": "line", "created_at": "2026-01-01T00:00:00Z",
       "updated_at": "2026-01-01T00:00:00Z", "resolved": true, "resolved_round": 2, "review_round": 1,
       "github_id": 42, "focus_key": "pr:3", "diff_scope": "layer", "head_sha": "h",
       "replies": [{"id": "rp_1", "body": "done", "created_at": "2026-01-01T00:00:00Z", "github_id": 43}]}
    ]}
  }
}`
	var cj CritJSON
	if err := json.Unmarshal([]byte(legacy), &cj); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "parent_id") {
		t.Errorf("legacy review gained a parent_id: %s", out)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(legacy), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("legacy review changed on round trip:\nbefore %v\nafter  %v", before, after)
	}

	sub := Comment{ID: "c_2", ParentID: "rp_1", Quote: "done"}
	data, _ := json.Marshal(sub)
	var back Comment
	if err := json.Unmarshal(data, &back); err != nil || back.ParentID != "rp_1" {
		t.Errorf("sub-thread parent_id lost: %s -> %+v (%v)", data, back, err)
	}
}

func TestSubThreadsStayNotice(t *testing.T) {
	none := CritJSON{Files: map[string]CritJSONFile{"a.go": {Comments: []Comment{{ID: "c_1"}}}}}
	if got := SubThreadsStayNotice(none); got != "" {
		t.Errorf("no sub-threads: %q", got)
	}
	one := CritJSON{ReviewComments: []Comment{{ID: "r_1"}, {ID: "r_2", ParentID: "r_1"}}}
	if got := SubThreadsStayNotice(one); got != "1 sub-thread stays in crit.\n" {
		t.Errorf("one sub-thread: %q", got)
	}
	two := CritJSON{
		Files:          map[string]CritJSONFile{"a.go": {Comments: []Comment{{ID: "c_1"}, {ID: "c_2", ParentID: "c_1", Resolved: true}}}},
		ReviewComments: []Comment{{ID: "r_1"}, {ID: "r_2", ParentID: "r_1"}},
	}
	if got := SubThreadsStayNotice(two); got != "2 sub-threads stay in crit.\n" {
		t.Errorf("two sub-threads: %q", got)
	}
}

// crit fetch gives an id to a reply imported from crit-web before replies had
// ids, so a sub-thread can anchor on it. The daemon, which still holds it
// without an id, takes that id rather than adding the reply twice.
func TestMergeFromDisk_AdoptsTheIDOfAnIDlessReply(t *testing.T) {
	f := &FileEntry{Path: "plan.md", Comments: []Comment{
		{ID: "c1", Body: "root", Replies: []Reply{{ID: "rp_a", Body: "known"}, {Body: "imported"}}},
	}}
	s := &Session{
		Files:          []*FileEntry{f},
		reviewComments: []Comment{{ID: "r1", Body: "general", Replies: []Reply{{Body: "imported"}}}},
		subscribers:    make(map[chan SSEEvent]struct{}),
	}
	diskReplies := []Reply{{ID: "rp_a", Body: "known"}, {ID: "rp_b", Body: "imported"}, {ID: "rp_c", Body: "new"}}

	if !s.mergeFileCommentsFromDisk(f, CritJSONFile{Comments: []Comment{{ID: "c1", Body: "root", Replies: diskReplies}}}) {
		t.Error("merge reported no change")
	}
	if !s.mergeReviewCommentsFromDisk([]Comment{{ID: "r1", Body: "general", Replies: diskReplies[1:]}}) {
		t.Error("review merge reported no change")
	}

	want := []Reply{{ID: "rp_a", Body: "known"}, {ID: "rp_b", Body: "imported"}, {ID: "rp_c", Body: "new"}}
	if !reflect.DeepEqual(f.Comments[0].Replies, want) {
		t.Errorf("file replies = %+v, want %+v", f.Comments[0].Replies, want)
	}
	if !reflect.DeepEqual(s.reviewComments[0].Replies, want[1:]) {
		t.Errorf("review replies = %+v, want %+v", s.reviewComments[0].Replies, want[1:])
	}
}
