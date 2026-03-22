package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAgentRequest_NoAgentConfigured(t *testing.T) {
	s, _ := newTestServer(t)
	// agentCmd is "" by default in newTestServer
	body := `{"comment_id":"c1"}`
	req := httptest.NewRequest("POST", "/api/agent/request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAgentRequest_CommentNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	s.agentCmd = "echo test"
	body := `{"comment_id":"nonexistent"}`
	req := httptest.NewRequest("POST", "/api/agent/request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAgentRequest_Success(t *testing.T) {
	s, session := newTestServer(t)
	s.agentCmd = "echo test"

	// Add a comment to the test file
	session.mu.Lock()
	session.Files[0].Comments = []Comment{
		{
			ID:        "c1",
			StartLine: 1,
			EndLine:   2,
			Body:      "Please fix this",
			Author:    "reviewer",
			Scope:     "line",
		},
	}
	session.mu.Unlock()

	body := `{"comment_id":"c1","file_path":"test.md"}`
	req := httptest.NewRequest("POST", "/api/agent/request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", resp["status"])
	}
}
