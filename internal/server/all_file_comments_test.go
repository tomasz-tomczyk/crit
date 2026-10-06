package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func getAllFileComments(t *testing.T, s *Server, url string) map[string][]Comment {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string][]Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestHandleAllFileComments_GroupsByPathAndHidesOtherFocus(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Files[0].Comments = []Comment{
		{ID: "c1", Body: "visible"},
		{ID: "c2", Body: "from a PR focus", FocusKey: "pr:github.com/o/r#7"},
	}
	sess.Files = append(sess.Files,
		&FileEntry{Path: "lazy.go", Status: "added", FileType: "code", Lazy: true,
			Comments: []Comment{{ID: "c3", Body: "on a lazy file", Resolved: true}}},
		&FileEntry{Path: "empty.go", Status: "added", FileType: "code"},
		&FileEntry{Path: "hidden.go", Status: "added", FileType: "code",
			Comments: []Comment{{ID: "c4", FocusKey: "range:a..b"}}},
	)

	got := getAllFileComments(t, s, "/api/files/comments")

	if len(got) != 2 {
		t.Fatalf("expected 2 paths, got %v", got)
	}
	if cs := got["test.md"]; len(cs) != 1 || cs[0].ID != "c1" {
		t.Errorf("test.md: expected [c1], got %+v", cs)
	}
	if cs := got["lazy.go"]; len(cs) != 1 || cs[0].ID != "c3" || !cs[0].Resolved {
		t.Errorf("lazy.go: expected resolved c3, got %+v", cs)
	}
}

func TestHandleAllFileComments_RoundFilter(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	sess.Files[0].Comments = []Comment{
		{ID: "c1", ReviewRound: 1},
		{ID: "c2", ReviewRound: 2},
		{ID: "c3", ReviewRound: 3},
	}

	got := getAllFileComments(t, s, "/api/files/comments?round=2")

	if cs := got["test.md"]; len(cs) != 2 {
		t.Fatalf("expected 2 comments at round 2, got %+v", cs)
	}
}

func TestHandleAllFileComments_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/files/comments", nil))
	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
