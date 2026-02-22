package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoWait_ReceivesPrompt(t *testing.T) {
	prompt := "Address review comments in plan.review.md."
	reviewFile := "plan.review.md"

	// Mock server that handles round-complete then await-review
	roundCompleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/round-complete":
			roundCompleteCalled = true
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/await-review":
			json.NewEncoder(w).Encode(ReviewResult{Prompt: prompt, ReviewFile: reviewFile})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := doGoWait(srv.URL)
	if err != nil {
		t.Fatalf("doGoWait error: %v", err)
	}
	if !roundCompleteCalled {
		t.Error("expected round-complete to be called")
	}
	if result.Prompt != prompt {
		t.Errorf("prompt = %q, want %q", result.Prompt, prompt)
	}
	if result.ReviewFile != reviewFile {
		t.Errorf("review_file = %q, want %q", result.ReviewFile, reviewFile)
	}
}

func TestGoWait_NoComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/round-complete":
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/await-review":
			json.NewEncoder(w).Encode(ReviewResult{Prompt: "", ReviewFile: "plan.review.md"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := doGoWait(srv.URL)
	if err != nil {
		t.Fatalf("doGoWait error: %v", err)
	}
	if result.Prompt != "" {
		t.Errorf("expected empty prompt, got %q", result.Prompt)
	}
}
