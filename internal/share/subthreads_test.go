package share

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func sharedIDs(comments []ShareComment) []string {
	ids := make([]string, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, c.ExternalID)
	}
	sort.Strings(ids)
	return ids
}

func findShared(comments []ShareComment, id string) ShareComment {
	for _, c := range comments {
		if c.ExternalID == id {
			return c
		}
	}
	return ShareComment{}
}

// marshalPayload round-trips a payload through JSON, as crit-web reads it.
func marshalPayload(t *testing.T, payload map[string]any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Sub-threads leave "comments", where crit-web would take them for line
// comments, for "sub_threads", which a crit-web that predates them ignores.
func TestSharePayloads_RouteSubThreadsUnderTheirOwnKey(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "r_root", Body: "general", Scope: "review"},
			{ID: "r_sub", Body: "on the summary", Scope: "review", ParentID: "r_root", Quote: "general"},
		},
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c_root", Body: "root", Scope: "line", StartLine: 2, EndLine: 2,
					Replies: []Reply{{ID: "rp_1", Body: "answer"}}},
				{ID: "c_sub", Body: "why?", Scope: "line", ParentID: "rp_1", Quote: "answer",
					ReviewRound: 2, Replies: []Reply{{ID: "rp_2", Body: "because", Author: "agent"}}},
			}},
		},
	})
	comments, _ := LoadCommentsForShare(critPath, []string{"plan.md"}, "me")
	files := []ShareFile{{Path: "plan.md", Content: "# Plan\n"}}

	for name, payload := range map[string]map[string]any{
		"POST": BuildSharePayload(files, comments, 1, nil, "", "", ""),
		"PUT":  BuildUpsertPayload(files, comments, "dt", 1, nil),
	} {
		raw := marshalPayload(t, payload)
		var roots []map[string]any
		if err := json.Unmarshal(raw["comments"], &roots); err != nil {
			t.Fatal(err)
		}
		var rootIDs []string
		for _, r := range roots {
			rootIDs = append(rootIDs, r["external_id"].(string))
			if r["scope"] == "line" && r["start_line"] == nil {
				t.Errorf("%s: line comment without lines in comments: %v", name, r)
			}
		}
		sort.Strings(rootIDs)
		if !reflect.DeepEqual(rootIDs, []string{"c_root", "r_root"}) {
			t.Errorf("%s: comments = %v, want only the roots", name, rootIDs)
		}

		var subThreads []map[string]any
		if err := json.Unmarshal(raw["sub_threads"], &subThreads); err != nil {
			t.Fatal(err)
		}
		if len(subThreads) != 2 {
			t.Fatalf("%s: sub_threads = %v", name, subThreads)
		}
		for _, st := range subThreads {
			if _, ok := st["file"]; ok {
				t.Errorf("%s: a sub-thread carries no file, crit-web takes its thread's: %v", name, st)
			}
			if st["resolved"] != false {
				t.Errorf("%s: resolved must be explicit: %v", name, st)
			}
		}
		want := map[string]any{
			"external_id": "c_sub", "parent_external_id": "rp_1", "body": "why?", "quote": "answer",
			"author_display_name": "me", "review_round": float64(2), "resolved": false,
			"replies": []any{map[string]any{"body": "because", "author_display_name": "agent", "external_id": "rp_2"}},
		}
		got := subThreads[0]
		if got["external_id"] != "c_sub" {
			got = subThreads[1]
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: sub-thread = %v, want %v", name, got, want)
		}
	}
}

// An explicit empty list tells crit-web the share has no sub-thread: it
// must not marshal as null.
func TestSharePayloads_SendEmptySubThreadList(t *testing.T) {
	for _, payload := range []map[string]any{
		BuildSharePayload(nil, nil, 1, nil, "", "", ""),
		BuildUpsertPayload(nil, nil, "dt", 1, nil),
	} {
		raw := marshalPayload(t, payload)
		if string(raw["sub_threads"]) != "[]" || string(raw["comments"]) != "[]" {
			t.Errorf("sub_threads = %s, comments = %s; want []", raw["sub_threads"], raw["comments"])
		}
	}
}

// The share sends unresolved threads only, plus each resolved thread an open
// sub-thread hangs from, marked resolved: crit-web needs it as the anchor.
func TestLoadCommentsForShare_KeepsTheParentsOfOpenSubThreads(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c_done", Body: "done", Scope: "line", StartLine: 1, EndLine: 1, Resolved: true,
					Replies: []Reply{{ID: "rp_done", Body: "fixed"}}},
				{ID: "c_mid", Body: "how?", Scope: "line", ParentID: "rp_done", Resolved: true,
					Replies: []Reply{{ID: "rp_mid", Body: "like so"}}},
				{ID: "c_open", Body: "sure?", Scope: "line", ParentID: "rp_mid"},
				{ID: "c_closed", Body: "closed", Scope: "line", StartLine: 3, EndLine: 3, Resolved: true},
				{ID: "c_closed_sub", Body: "ok", Scope: "line", ParentID: "c_closed", Resolved: true},
				{ID: "c_orphan", Body: "lost", Scope: "line", ParentID: "rp_gone"},
				{ID: "c_loop_a", Body: "a", Scope: "line", ParentID: "c_loop_b"},
				{ID: "c_loop_b", Body: "b", Scope: "line", ParentID: "c_loop_a"},
			}},
		},
	})

	comments, _ := LoadCommentsForShare(critPath, []string{"plan.md"}, "")

	if got, want := sharedIDs(comments), []string{"c_done", "c_mid", "c_open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("shared = %v, want %v", got, want)
	}
	if c := findShared(comments, "c_done"); !c.Resolved || c.ParentExternalID != "" || c.StartLine != 1 {
		t.Errorf("resolved root kept as anchor = %+v", c)
	}
	if c := findShared(comments, "c_mid"); !c.Resolved || c.ParentExternalID != "rp_done" {
		t.Errorf("resolved sub-thread kept as anchor = %+v", c)
	}
	if c := findShared(comments, "c_open"); c.Resolved || c.ParentExternalID != "rp_mid" {
		t.Errorf("open sub-thread = %+v", c)
	}
	if lines := sharedLines(comments); len(lines) != 1 {
		t.Errorf("shared lines = %v, want the root's only", lines)
	}
}

// Hashes recorded before sub-threads existed stay valid: no upgrade forces a
// re-upload. A sub-thread, or a reply in one, changes the hash.
func TestComputeShareHash_SubThreads(t *testing.T) {
	files := []ShareFile{{Path: "plan.md", Content: "hello"}}
	roots := []ShareComment{{ExternalID: "c1"}, {ExternalID: "r1", Resolved: true, Replies: []ShareReply{{ExternalID: "rp_1"}}}}
	if got := computeShareHash(files, roots); got != "92fc4ad13d14f0f1" {
		t.Errorf("hash without sub-threads = %s, want the one computed before sub-threads", got)
	}

	sub := ShareComment{ExternalID: "c_sub", ParentExternalID: "rp_1"}
	withSub := computeShareHash(files, append(roots, sub))
	if withSub == computeShareHash(files, roots) {
		t.Error("a new sub-thread must change the hash")
	}
	sub.Replies = []ShareReply{{ExternalID: "rp_2"}}
	if computeShareHash(files, append(roots, sub)) == withSub {
		t.Error("a new reply in a sub-thread must change the hash")
	}
	moved := ShareComment{ExternalID: "c_sub", ParentExternalID: "c1"}
	if computeShareHash(files, append(roots, moved)) == withSub {
		t.Error("a sub-thread on another message must change the hash")
	}
}

// crit-web's GET /api/reviews/:token/comments: threads at the top level, each
// message carrying its sub-threads.
const nestedWebComments = `[
  {"id":"w_root","external_id":"c_root","body":"root","file_path":"plan.md","start_line":2,"end_line":2,"scope":"line",
   "sub_threads":[{"id":"w_s1","body":"on the comment","quote":"root","scope":"line","file_path":"plan.md","replies":[],"sub_threads":[]}],
   "replies":[
     {"id":"w_rp1","external_id":"rp_1","body":"answer","author_display_name":"agent","sub_threads":[
       {"id":"w_s2","body":"why?","quote":"answer","quote_offset":0,"author_display_name":"Bob","resolved":false,"review_round":3,
        "replies":[{"id":"w_rp2","body":"because","author_display_name":"Ann","sub_threads":[
          {"id":"w_s3","external_id":"","body":"really?","quote":"because","replies":[],"sub_threads":[]}]}],
        "sub_threads":[]}]},
     {"id":"w_rp_web","body":"web reply","author_display_name":"Bob","sub_threads":[
       {"id":"w_s4","body":"on a web reply","quote":"web","replies":[],"sub_threads":[]}]}
   ]},
  {"id":"w_web","body":"web root","file_path":"plan.md","start_line":0,"end_line":0,"scope":"file",
   "replies":[{"id":"w_wr","body":"web answer","sub_threads":[
     {"id":"w_s5","body":"on a web-born thread","quote":"answer","replies":[{"id":"w_wr2","body":"agreed"}],"sub_threads":[]}]}]}
]`

func reviewWithRootThread() CritJSON {
	return CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c_root", Body: "root", Scope: "line", Side: "new", StartLine: 2, EndLine: 2,
					HeadSHA: "abc", DiffScope: "layer", FocusKey: "pr:7",
					Replies: []Reply{{ID: "rp_1", Body: "answer", Author: "agent"}}},
			}},
		},
	}
}

func fetchAndMerge(t *testing.T, critPath, body string) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	cj := readPlacementReview(t, critPath)
	fetched, err := fetchWebCommentsForReview(srv.URL+"/r/tok", "", cj, "")
	if err != nil {
		t.Fatal(err)
	}
	added, err := mergeFetchedComments(critPath, fetched)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

func byID(comments []Comment) map[string]Comment {
	out := make(map[string]Comment, len(comments))
	for _, c := range comments {
		out[c.ID] = c
	}
	return out
}

func subThreadsByBody(comments []Comment) map[string]Comment {
	out := make(map[string]Comment)
	for _, c := range comments {
		if c.ParentID != "" {
			out[c.Body] = c
		}
	}
	return out
}

// crit fetch imports the sub-threads written on crit-web under the local copy
// of their message, whether that message came from crit or from crit-web,
// and a repeated fetch imports nothing twice.
func TestFetch_ImportsWebSubThreadsOnceUnderTheirMessages(t *testing.T) {
	critPath := writePlacementReview(t, reviewWithRootThread())

	if added := fetchAndMerge(t, critPath, nestedWebComments); added != 5 {
		t.Fatalf("added = %d, want 5 sub-threads", added)
	}
	list := readPlacementReview(t, critPath).Files["plan.md"].Comments
	subs := subThreadsByBody(list)
	if len(subs) != 5 {
		t.Fatalf("sub-threads = %v", subs)
	}
	root := byID(list)["c_root"]

	onComment := subs["on the comment"]
	if onComment.ParentID != "c_root" || onComment.Quote != "root" {
		t.Errorf("sub-thread on the comment = %+v", onComment)
	}
	why := subs["why?"]
	if why.ParentID != "rp_1" || why.Author != "Bob" || why.ReviewRound != 3 || why.QuoteOffset == nil ||
		why.Scope != "line" || why.Side != "new" || why.HeadSHA != "abc" || why.DiffScope != "layer" || why.FocusKey != "pr:7" ||
		why.StartLine != 0 || !strings.HasPrefix(why.ID, "web-") {
		t.Errorf("sub-thread on the reply = %+v", why)
	}
	if len(why.Replies) != 1 || why.Replies[0].ID == "" || why.Replies[0].Body != "because" {
		t.Fatalf("sub-thread replies = %+v", why.Replies)
	}
	if really := subs["really?"]; really.ParentID != why.Replies[0].ID {
		t.Errorf("sub-thread on a sub-thread's reply = %+v, want parent %s", really, why.Replies[0].ID)
	}

	// The web reply on the crit root comes in with an id, which its
	// sub-thread anchors on.
	if len(root.Replies) != 2 || root.Replies[1].Body != "web reply" || root.Replies[1].ID == "" {
		t.Fatalf("root replies = %+v", root.Replies)
	}
	if onWebReply := subs["on a web reply"]; onWebReply.ParentID != root.Replies[1].ID {
		t.Errorf("sub-thread on a web reply = %+v, want parent %s", onWebReply, root.Replies[1].ID)
	}

	// A thread written on crit-web, imported in the same fetch.
	var webRoot Comment
	for _, c := range list {
		if c.Body == "web root" {
			webRoot = c
		}
	}
	if webRoot.ID == "" || len(webRoot.Replies) != 1 || webRoot.Replies[0].ID == "" {
		t.Fatalf("web root = %+v", webRoot)
	}
	onWebThread := subs["on a web-born thread"]
	if onWebThread.ParentID != webRoot.Replies[0].ID || onWebThread.Scope != "file" ||
		len(onWebThread.Replies) != 1 || onWebThread.Replies[0].ID == "" {
		t.Errorf("sub-thread on a web-born reply = %+v", onWebThread)
	}

	// Fetching again changes nothing: not a sub-thread, not a reply id.
	before := readPlacementReview(t, critPath)
	if added := fetchAndMerge(t, critPath, nestedWebComments); added != 0 {
		t.Errorf("second fetch added %d sub-threads", added)
	}
	after := readPlacementReview(t, critPath)
	if !reflect.DeepEqual(before.Files, after.Files) {
		t.Errorf("second fetch changed the review:\nbefore %+v\nafter  %+v", before.Files, after.Files)
	}
}

// After a re-share, crit-web returns the imported sub-threads and replies
// with the external ids crit sent: a fetch then matches them by id.
func TestFetch_MatchesReSharedSubThreadsByExternalID(t *testing.T) {
	cj := reviewWithRootThread()
	entry := cj.Files["plan.md"]
	entry.Comments = append(entry.Comments, Comment{ID: "web-4", ParentID: "rp_1", Body: "why?", Quote: "answer",
		Replies: []Reply{{ID: "rp_b", Body: "because"}}})
	cj.Files["plan.md"] = entry
	critPath := writePlacementReview(t, cj)

	body := `[{"id":"w1","external_id":"c_root","body":"root","file_path":"plan.md","start_line":2,"end_line":2,"scope":"line",
	  "replies":[{"id":"w2","external_id":"rp_1","body":"answer","sub_threads":[
	    {"id":"w3","external_id":"web-4","body":"why? (edited on web)","quote":"answer","replies":[
	      {"id":"w4","external_id":"rp_b","body":"because"},{"id":"w5","body":"new web reply"}]}]}]}]`
	if added := fetchAndMerge(t, critPath, body); added != 0 {
		t.Errorf("added = %d, want the known sub-thread matched by id", added)
	}
	sub := byID(readPlacementReview(t, critPath).Files["plan.md"].Comments)["web-4"]
	if sub.Body != "why?" || len(sub.Replies) != 2 || sub.Replies[1].Body != "new web reply" || sub.Replies[1].ID == "" {
		t.Errorf("known sub-thread = %+v", sub)
	}
}

// A crit-web that predates sub-threads returns no sub_threads and no reply
// ids: the fetch works as before, and imported replies still get ids.
func TestFetch_FromACritWebWithoutSubThreads(t *testing.T) {
	critPath := writePlacementReview(t, reviewWithRootThread())
	body := `[{"external_id":"c_root","body":"root","file_path":"plan.md","start_line":2,"end_line":2,"scope":"line",
	  "replies":[{"body":"answer"},{"body":"new","author_display_name":"Bob"}]}]`
	if added := fetchAndMerge(t, critPath, body); added != 0 {
		t.Errorf("added = %d", added)
	}
	root := byID(readPlacementReview(t, critPath).Files["plan.md"].Comments)["c_root"]
	if len(root.Replies) != 2 || root.Replies[1].Body != "new" || !strings.HasPrefix(root.Replies[1].ID, "rp_") {
		t.Errorf("root replies = %+v", root.Replies)
	}
}

func TestMergeWebReplies_KeepsIDsStable(t *testing.T) {
	c := Comment{ID: "c1", Replies: []Reply{{ID: "rp_a", Body: "same"}, {Body: "legacy, no id"}}}

	changed := mergeWebReplies(&c, []WebReply{
		{ExternalID: "rp_a", Body: "same"},
		{Body: "legacy, no id"},
		{ExternalID: "rp_shared", Body: "shared by crit, deleted here"},
		{Body: "written on the web"},
	})
	if !changed || len(c.Replies) != 4 {
		t.Fatalf("changed = %v, replies = %+v", changed, c.Replies)
	}
	if c.Replies[0].ID != "rp_a" || c.Replies[1].ID == "" || c.Replies[2].ID != "rp_shared" ||
		!strings.HasPrefix(c.Replies[3].ID, "rp_") {
		t.Errorf("reply ids = %+v", c.Replies)
	}
	before := append([]Reply(nil), c.Replies...)
	if mergeWebReplies(&c, []WebReply{{ExternalID: "rp_shared", Body: "x"}, {Body: "written on the web"}}) {
		t.Error("merging known replies reported a change")
	}
	if !reflect.DeepEqual(before, c.Replies) {
		t.Errorf("replies changed: %+v", c.Replies)
	}
}

// A crit-web comment with a sub-thread's body and lines 0-0 (file-level or
// review-level) is new: it must neither be dropped nor have its replies
// merged into the sub-thread.
func TestDedupWebComments_IgnoresSubThreads(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r_root", Body: "general", Scope: "review"},
			{ID: "r_sub", Body: "same words", Scope: "review", ParentID: "r_root"},
		},
		Files: map[string]CritJSONFile{"plan.md": {Comments: []Comment{
			{ID: "c_root", Body: "root", Scope: "line", StartLine: 2, EndLine: 2},
			{ID: "c_sub", Body: "same words", Scope: "line", ParentID: "c_root"},
		}}},
	}
	fps, _ := buildLocalFingerprintIndex(cj)
	if len(fps) != 2 {
		t.Errorf("fingerprints = %v, want only the 2 roots", fps)
	}
	incoming := []WebComment{
		{Body: "same words", FilePath: "plan.md", Scope: "file", Replies: []WebReply{{Body: "web reply"}}},
		{Body: "same words", Scope: "review", Replies: []WebReply{{Body: "web reply"}}},
	}
	fresh, updates := DedupWebComments(cj, incoming)
	if len(fresh) != 2 || len(updates) != 0 {
		t.Errorf("new = %+v, reply updates = %+v; want both web comments new and no merge", fresh, updates)
	}
}

// A crit-web external id becomes a local reply id only when it is a plain
// URL path segment and free; anything else gets a fresh rp_ id.
func TestNewReplyID_KeepsOnlySafeFreeExternalIDs(t *testing.T) {
	taken := []Reply{{ID: "rp_a1b2c3"}}
	for _, id := range []string{"rp_d4e5f6", "web-12", "Ab_9-z"} {
		if got := newReplyID(taken, id); got != id {
			t.Errorf("newReplyID(%q) = %q, want it kept", id, got)
		}
	}
	for _, id := range []string{"", "rp_a1b2c3", "../x", "a/b", "rp 1", "a?b=c", `"><img>`, "é"} {
		got := newReplyID(taken, id)
		if got == id || !strings.HasPrefix(got, "rp_") || len(got) != len("rp_")+6 {
			t.Errorf("newReplyID(%q) = %q, want a fresh rp_ id", id, got)
		}
	}
}
