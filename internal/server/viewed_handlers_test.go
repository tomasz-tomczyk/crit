package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func viewedRequest(t *testing.T, s *Server, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/viewed", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleViewed(w, req)
	return w
}

func TestHandleViewed_RoundTrip(t *testing.T) {
	s, sess := newTestServer(t)
	sess.OutputDir = t.TempDir()

	w := viewedRequest(t, s, http.MethodGet, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"files":{}`) {
		t.Fatalf("empty GET: %d %s", w.Code, w.Body)
	}

	w = viewedRequest(t, s, http.MethodPut, `{"files":[{"path":"test.md","viewed":true,"file_hash":"sha256:a"}]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}

	w = viewedRequest(t, s, http.MethodGet, "")
	var got struct {
		Files map[string]struct {
			Viewed   bool   `json:"viewed"`
			FileHash string `json:"file_hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Files["test.md"].Viewed || got.Files["test.md"].FileHash != "sha256:a" {
		t.Fatalf("GET after PUT: %s", w.Body)
	}
}

func TestHandleViewed_Errors(t *testing.T) {
	tests := []struct {
		name, method, body string
		want               int
	}{
		{"invalid json", http.MethodPut, "{", http.StatusBadRequest},
		{"missing path", http.MethodPut, `{"files":[{"viewed":true}]}`, http.StatusBadRequest},
		{"wrong method", http.MethodPost, "{}", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, sess := newTestServer(t)
			sess.OutputDir = t.TempDir()
			if w := viewedRequest(t, s, tt.method, tt.body); w.Code != tt.want {
				t.Fatalf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}
