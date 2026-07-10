package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// storyTestServer builds a server whose lone file carries a single fabricated
// diff hunk, without touching git — FileEntry.DiffHunks is preset and Lazy is
// left false, so ensureLoaded (called by Session.StoryScope) is a no-op and
// the preset hunk survives untouched.
func storyTestServer(t *testing.T) (*Server, *Session) {
	t.Helper()
	s, session := newTestServer(t)
	session.Files[0].Status = "modified"
	session.Files[0].DiffHunks = []vcs.DiffHunk{
		{
			OldStart: 1,
			OldCount: 1,
			NewStart: 1,
			NewCount: 1,
			Header:   "@@ -1 +1 @@",
			Lines: []vcs.DiffLine{
				{Type: "del", Content: "line1", OldNum: 1},
				{Type: "add", Content: "line1 changed", NewNum: 1},
			},
		},
	}
	return s, session
}

func validStoryBody(t *testing.T) []byte {
	t.Helper()
	body := map[string]any{
		"story": map[string]any{
			"version": 1,
			"prologue": map[string]any{
				"title":       "Test story",
				"overview":    "A test story.",
				"key_changes": []string{"Covers the fabricated hunk."},
				"risks":       []string{"Fixture coverage depends on test.md old_start 1."},
			},
			"chapters": []map[string]any{
				{
					"id":    "ch1",
					"title": "The one change",
					"hunk_refs": []map[string]any{
						{"file_path": "test.md", "old_start": 1},
					},
				},
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHandleStory_GetEmpty_204(t *testing.T) {
	s, _ := storyTestServer(t)
	req := httptest.NewRequest("GET", "/api/story", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", w.Body.String())
	}
}

func TestHandleStory_PostValid_SavedAndGettable(t *testing.T) {
	s, session := storyTestServer(t)

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("POST status = %d, body = %s", w.Code, w.Body.String())
	}

	if session.GetStory() == nil {
		t.Fatal("expected session.story to be set after POST")
	}

	getReq := httptest.NewRequest("GET", "/api/story", nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)

	if getW.Code != 200 {
		t.Fatalf("GET status = %d, body = %s", getW.Code, getW.Body.String())
	}
	var st Story
	if err := json.Unmarshal(getW.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(st.Chapters) != 1 || st.Chapters[0].ID != "ch1" {
		t.Errorf("unexpected story returned: %+v", st)
	}
}

func TestHandleStory_PostValid_Idempotent(t *testing.T) {
	s, _ := storyTestServer(t)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("POST[%d] status = %d, body = %s", i, w.Code, w.Body.String())
		}
	}
}

func TestHandleStory_PostRejected_DuplicateHunk_ReturnsCoverageAndDoesNotSave(t *testing.T) {
	s, session := storyTestServer(t)

	body := map[string]any{
		"story": map[string]any{
			"version": 1,
			"prologue": map[string]any{
				"title":       "Duplicate story",
				"overview":    "A duplicate story.",
				"key_changes": []string{"Places the fabricated hunk."},
				"risks":       []string{"The same hunk is intentionally duplicated."},
			},
			"chapters": []map[string]any{
				{
					"id":    "ch1",
					"title": "Chapter one",
					"hunk_refs": []map[string]any{
						{"file_path": "test.md", "old_start": 1},
					},
				},
				{
					"id":    "ch2",
					"title": "Chapter two — duplicate",
					"hunk_refs": []map[string]any{
						{"file_path": "test.md", "old_start": 1},
					},
				},
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status = %d, want 4xx, body = %s", w.Code, w.Body.String())
	}

	// Body is the bare StoryCoverage object — same shape `crit story` prints
	// to stdout — not wrapped under an "error"/"coverage" envelope.
	var coverage struct {
		OK         bool     `json:"ok"`
		Indexed    int      `json:"indexed"`
		Duplicated []string `json:"duplicated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &coverage); err != nil {
		t.Fatalf("expected coverage-report JSON body, got %q: %v", w.Body.String(), err)
	}
	if len(coverage.Duplicated) == 0 {
		t.Errorf("expected duplicated hunks in coverage report, got %+v", coverage)
	}
	if w.Header().Get("X-Story-Ingest-Error") == "" {
		t.Error("expected rejection reason in X-Story-Ingest-Error header")
	}

	if session.GetStory() != nil {
		t.Error("rejected ingest must not save a story on the session")
	}

	getReq := httptest.NewRequest("GET", "/api/story", nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	if getW.Code != 204 {
		t.Fatalf("GET after rejected POST: status = %d, want 204", getW.Code)
	}
}

func TestHandleStory_PostInvalidJSON_400(t *testing.T) {
	s, _ := storyTestServer(t)
	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleStory_Delete(t *testing.T) {
	s, session := storyTestServer(t)

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("setup POST failed: %d %s", w.Code, w.Body.String())
	}

	delReq := httptest.NewRequest("DELETE", "/api/story", nil)
	delW := httptest.NewRecorder()
	s.ServeHTTP(delW, delReq)
	if delW.Code != 200 && delW.Code != 204 {
		t.Fatalf("DELETE status = %d, want 200 or 204", delW.Code)
	}

	if session.GetStory() != nil {
		t.Error("expected session.story to be nil after DELETE")
	}

	getReq := httptest.NewRequest("GET", "/api/story", nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	if getW.Code != 204 {
		t.Fatalf("GET after DELETE: status = %d, want 204", getW.Code)
	}
}

func TestHandleStory_PostBroadcastsSSE(t *testing.T) {
	s, session := storyTestServer(t)
	ch := session.Subscribe()
	defer session.Unsubscribe(ch)

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("POST status = %d, body = %s", w.Code, w.Body.String())
	}

	select {
	case event := <-ch:
		if event.Type != "story-updated" {
			t.Fatalf("event type = %q, want story-updated", event.Type)
		}
		var payload struct {
			Story *Story `json:"story"`
		}
		if err := json.Unmarshal([]byte(event.Content), &payload); err != nil {
			t.Fatalf("unmarshal event content: %v", err)
		}
		if payload.Story == nil || len(payload.Story.Chapters) != 1 {
			t.Errorf("expected event payload to echo the story, got %+v", payload.Story)
		}
	default:
		t.Fatal("expected story-updated event on POST, got none")
	}
}

func TestHandleStory_DeleteBroadcastsSSEWithNullStory(t *testing.T) {
	s, session := storyTestServer(t)

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("setup POST failed: %d %s", w.Code, w.Body.String())
	}

	ch := session.Subscribe()
	defer session.Unsubscribe(ch)

	delReq := httptest.NewRequest("DELETE", "/api/story", nil)
	delW := httptest.NewRecorder()
	s.ServeHTTP(delW, delReq)
	if delW.Code != 200 && delW.Code != 204 {
		t.Fatalf("DELETE status = %d", delW.Code)
	}

	select {
	case event := <-ch:
		if event.Type != "story-updated" {
			t.Fatalf("event type = %q, want story-updated", event.Type)
		}
		var payload struct {
			Story *Story `json:"story"`
		}
		if err := json.Unmarshal([]byte(event.Content), &payload); err != nil {
			t.Fatalf("unmarshal event content: %v", err)
		}
		if payload.Story != nil {
			t.Errorf("expected null story in event payload after DELETE, got %+v", payload.Story)
		}
	default:
		t.Fatal("expected story-updated event on DELETE, got none")
	}
}

func TestHandleStory_MethodNotAllowed(t *testing.T) {
	s, _ := storyTestServer(t)
	req := httptest.NewRequest("PUT", "/api/story", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandleStory_SessionInfoCarriesStory(t *testing.T) {
	s, _ := storyTestServer(t)

	req := httptest.NewRequest("POST", "/api/story", bytes.NewReader(validStoryBody(t)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("setup POST failed: %d %s", w.Code, w.Body.String())
	}

	sessReq := httptest.NewRequest("GET", "/api/session", nil)
	sessW := httptest.NewRecorder()
	s.ServeHTTP(sessW, sessReq)

	var info SessionInfo
	if err := json.Unmarshal(sessW.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal session info: %v", err)
	}
	if info.Story == nil {
		t.Fatal("expected SessionInfo.Story to be populated")
	}
	if len(info.Story.Chapters) != 1 {
		t.Errorf("unexpected story on session info: %+v", info.Story)
	}
}
