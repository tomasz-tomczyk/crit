package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
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
