package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestAgentName_Codex(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"codex exec", "codex"},
		{"codex exec {prompt}", "codex"},
		{"/usr/local/bin/codex exec", "codex"},
		{"claude -p", "claude"},
		{"", "agent"},
	}
	for _, tc := range tests {
		got := agentName(tc.cmd)
		if got != tc.want {
			t.Errorf("agentName(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestRunAgentCmd_PromptPlaceholder(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.md")
	os.WriteFile(testFile, []byte("hello"), 0o644)

	s, session := newTestServer(t)
	session.RepoRoot = dir

	// With {prompt}, the prompt is passed as an argument (not stdin).
	s.agentCmd = "echo {prompt}"

	session.mu.Lock()
	session.Files[0].Comments = []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "test comment", Author: "reviewer", Scope: "line"},
	}
	session.mu.Unlock()

	s.runAgentCmd("hello from placeholder", "c1", session.Files[0].Path)

	// runAgentCmd is synchronous — reply is already added when it returns.
	session.mu.Lock()
	replies := session.Files[0].Comments[0].Replies
	session.mu.Unlock()
	if len(replies) == 0 {
		t.Fatal("expected a reply from agent, got none")
	}
	if !strings.Contains(replies[0].Body, "hello from placeholder") {
		t.Errorf("reply body = %q, want it to contain the prompt text", replies[0].Body)
	}
	if replies[0].Author != "echo" {
		t.Errorf("reply author = %q, want 'echo'", replies[0].Author)
	}
}

func TestRunAgentCmd_StdinFallback(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.md")
	os.WriteFile(testFile, []byte("hello"), 0o644)

	s, session := newTestServer(t)
	session.RepoRoot = dir

	// "cat" reads from stdin and prints to stdout — no {prompt} placeholder
	s.agentCmd = "cat"

	session.mu.Lock()
	session.Files[0].Comments = []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "test comment", Author: "reviewer", Scope: "line"},
	}
	session.mu.Unlock()

	s.runAgentCmd("hello from stdin", "c1", session.Files[0].Path)

	session.mu.Lock()
	replies := session.Files[0].Comments[0].Replies
	session.mu.Unlock()
	if len(replies) == 0 {
		t.Fatal("expected a reply from agent, got none")
	}
	if !strings.Contains(replies[0].Body, "hello from stdin") {
		t.Errorf("reply body = %q, want it to contain the prompt text", replies[0].Body)
	}
	if replies[0].Author != "cat" {
		t.Errorf("reply author = %q, want 'cat'", replies[0].Author)
	}
}
