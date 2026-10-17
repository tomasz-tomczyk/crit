package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
)

func serveJSON(t *testing.T, s *Server, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestPostFileComment_CreatesSubThreadOnReply(t *testing.T) {
	s, session := newTestServer(t)
	root, _ := session.AddComment("test.md", 2, 3, "", "root", "", "reviewer", "")
	reply, _ := session.AddReply("test.md", root.ID, "a long agent answer", "agent", "")

	w := serveJSON(t, s, "POST", "/api/file/comments?path=test.md",
		`{"body":"why this?","parent_id":"`+reply.ID+`","quote":"agent answer","quote_offset":7,"author":"reviewer"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	id, _ := got["id"].(string)
	if !strings.HasPrefix(id, "c_") || got["parent_id"] != reply.ID || got["quote"] != "agent answer" ||
		got["quote_offset"] != float64(7) || got["start_line"] != float64(0) || got["end_line"] != float64(0) ||
		got["scope"] != "line" || got["body"] != "why this?" || got["author"] != "reviewer" {
		t.Errorf("created sub-thread = %v", got)
	}
	c, path, ok := session.FindCommentByID(id, "test.md")
	if !ok || path != "test.md" || c.ParentID != reply.ID {
		t.Errorf("sub-thread not stored in test.md: %+v", c)
	}
}

func TestPostFileComment_SubThreadUnknownParentIs404(t *testing.T) {
	s, session := newTestServer(t)
	reviewRoot := session.AddReviewComment("general", "", "")
	for _, parent := range []string{"c_missing", reviewRoot.ID} {
		w := serveJSON(t, s, "POST", "/api/file/comments?path=test.md", `{"body":"x","parent_id":"`+parent+`","quote":"q"}`)
		if w.Code != http.StatusNotFound {
			t.Errorf("parent %s: status = %d, want 404", parent, w.Code)
		}
	}
	if got := len(session.GetComments("test.md")); got != 0 {
		t.Errorf("rejected sub-thread stored: %d comments", got)
	}
}

func TestPostReviewComment_CreatesSubThread(t *testing.T) {
	s, session := newTestServer(t)
	root := session.AddReviewComment("general", "reviewer", "")

	w := serveJSON(t, s, "POST", "/api/comments", `{"body":"on the summary","parent_id":"`+root.ID+`","quote":"general","quote_offset":0}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var c Comment
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.ID, "r_") || c.ParentID != root.ID || c.Scope != "review" || c.Quote != "general" ||
		c.QuoteOffset == nil || *c.QuoteOffset != 0 {
		t.Errorf("review-level sub-thread = %+v", c)
	}

	w = serveJSON(t, s, "POST", "/api/comments", `{"body":"x","parent_id":"r_missing"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown parent: status = %d, want 404", w.Code)
	}
	if got := len(session.GetReviewComments()); got != 2 {
		t.Errorf("review comments = %d, want root + sub-thread", got)
	}
}

func TestFinish_OpenSubThreadBlocksApproval(t *testing.T) {
	s, session := newTestServer(t)
	root, _ := session.AddComment("test.md", 1, 1, "", "root", "", "", "")
	sub, _ := session.AddSubThread("test.md", root.ID, "still open", "root", nil, "", "")
	session.SetCommentResolved("test.md", root.ID, true)

	w := serveJSON(t, s, "POST", "/api/finish", "")
	var resp struct {
		Approved bool             `json:"approved"`
		Comments []map[string]any `json:"comments"`
		Prompt   string           `json:"prompt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Approved {
		t.Fatal("review approved with an open sub-thread")
	}
	if len(resp.Comments) != 1 || resp.Comments[0]["id"] != sub.ID || resp.Comments[0]["parent_id"] != root.ID || resp.Comments[0]["quote"] != "root" {
		t.Errorf("finish comments = %v, want the open sub-thread with parent_id and quote", resp.Comments)
	}
	// The resolved parent is not listed: the prompt tells how to read it.
	if !strings.Contains(resp.Prompt, "`parent_id`") || !strings.Contains(resp.Prompt, "`crit comments --json --all ") {
		t.Errorf("finish prompt does not explain sub-threads:\n%s", resp.Prompt)
	}
}

func TestBuildAgentPrompt_SubThread(t *testing.T) {
	c := Comment{ID: "c_sub", ParentID: "rp_1", Quote: "use a mutex", Body: "why not a channel?", Scope: "line"}
	got := buildAgentPrompt(c, "main.go", &agentPromptParent{Author: "claude", Body: "I would use a mutex here."})
	for _, want := range []string{"passage of a message", "main.go", "Message from claude", "I would use a mutex here.", "Passage:\n> use a mutex", "why not a channel?"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "line") || strings.Contains(got, "Code:") {
		t.Errorf("sub-thread prompt points at file lines:\n%s", got)
	}

	review := buildAgentPrompt(Comment{ID: "r_sub", ParentID: "r_1", Quote: "q", Body: "b", Scope: "review"}, "", nil)
	if !strings.Contains(review, "general thread of this review") {
		t.Errorf("review-level sub-thread prompt:\n%s", review)
	}
}

func TestAgentRequest_SubThreadGetsTheReply(t *testing.T) {
	s, session := newTestServer(t)
	s.agentCmd = "echo {prompt}"
	root, _ := session.AddComment("test.md", 1, 1, "", "root", "", "", "")
	reply, _ := session.AddReply("test.md", root.ID, "agent answer", "agent", "")
	sub, _ := session.AddSubThread("test.md", reply.ID, "why?", "answer", nil, "", "")

	w := serveJSON(t, s, "POST", "/api/agent/request", `{"comment_id":"`+sub.ID+`","file_path":"test.md"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	s.bgWG.Wait()

	c, _, _ := session.FindCommentByID(sub.ID, "test.md")
	if !c.Live {
		t.Error("sub-thread not marked live")
	}
	if len(c.Replies) != 1 || !strings.Contains(c.Replies[0].Body, "agent answer") {
		t.Errorf("agent reply on the sub-thread = %+v, want the prompt echoed with the parent message", c.Replies)
	}
	if r, _, _ := session.FindCommentByID(root.ID, "test.md"); len(r.Replies) != 1 {
		t.Errorf("agent reply landed on the root thread: %+v", r.Replies)
	}
}

// The proxy-auth relay posts crit-web's comment list as fetched: the
// sub-threads nested under its messages are imported, once.
func TestApiCommentsMerge_ImportsNestedSubThreadsOnce(t *testing.T) {
	s, session := newTestServer(t)
	session.SetSharedURLAndToken("https://crit.example/r/abc123", "delete")
	seed := CritJSON{Files: map[string]CritJSONFile{"test.md": {Comments: []Comment{{
		ID: "c_root", Body: "root", Scope: "line", StartLine: 1, EndLine: 1,
		Replies: []Reply{{ID: "rp_1", Body: "answer"}},
	}}}}}
	if err := review.SaveCritJSON(session.CritJSONPath(), seed); err != nil {
		t.Fatal(err)
	}
	body := `{"comments":[{"id":"w1","external_id":"c_root","body":"root","file_path":"test.md",
	  "start_line":1,"end_line":1,"scope":"line","replies":[{"id":"w2","external_id":"rp_1","body":"answer",
	  "sub_threads":[{"id":"w3","body":"why?","quote":"answer","replies":[{"id":"w4","body":"because"}]}]}]}]}`

	for _, want := range []float64{1, 0} {
		w := serveJSON(t, s, "POST", "/api/comments/merge", body)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp["merged"] != want {
			t.Errorf("merged = %v, want %v", resp["merged"], want)
		}
	}

	var subThreads []Comment
	for _, c := range session.GetComments("test.md") {
		if c.ParentID != "" {
			subThreads = append(subThreads, c)
		}
	}
	if len(subThreads) != 1 || subThreads[0].ParentID != "rp_1" || subThreads[0].Quote != "answer" ||
		len(subThreads[0].Replies) != 1 || subThreads[0].Replies[0].ID == "" {
		t.Errorf("sub-threads = %+v", subThreads)
	}
}
