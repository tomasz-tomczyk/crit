package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeShareHash(t *testing.T) {
	files := []shareFile{{Path: "plan.md", Content: "hello"}}
	comments := []shareComment{{ExternalID: "c1", Resolved: false}}

	h1 := computeShareHash(files, comments)
	if h1 == "" {
		t.Fatal("expected non-empty hash")
	}

	// same input → same hash
	h2 := computeShareHash(files, comments)
	if h1 != h2 {
		t.Errorf("same input should produce same hash, got %q vs %q", h1, h2)
	}

	// changed file content → different hash
	files2 := []shareFile{{Path: "plan.md", Content: "changed"}}
	if h3 := computeShareHash(files2, comments); h3 == h1 {
		t.Error("changed file content should produce different hash")
	}

	// changed resolved state → different hash
	comments2 := []shareComment{{ExternalID: "c1", Resolved: true}}
	if h4 := computeShareHash(files, comments2); h4 == h1 {
		t.Error("changed resolved state should produce different hash")
	}
}

// writeCritJSONForTest writes a CritJSON to dir/.crit.json for test setup.
func writeCritJSONForTest(t *testing.T, dir string, cj CritJSON) {
	t.Helper()
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatalf("marshaling CritJSON: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644); err != nil {
		t.Fatalf("writing .crit.json: %v", err)
	}
}

func TestLoadCommentsForUpsert_ExcludesResolved(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "open", Resolved: false, ReviewRound: 1},
					{ID: "c2", StartLine: 2, EndLine: 2, Body: "done", Resolved: true, ReviewRound: 1},
				},
			},
		},
	}
	writeCritJSONForTest(t, dir, cj)

	comments, round := loadCommentsForUpsert(dir, []string{"plan.md"})
	if round != 1 {
		t.Errorf("expected round 1, got %d", round)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment (only unresolved), got %d", len(comments))
	}
	if comments[0].ExternalID != "c1" {
		t.Errorf("expected ExternalID c1, got %q", comments[0].ExternalID)
	}
	if comments[0].Body != "open" {
		t.Errorf("expected body 'open', got %q", comments[0].Body)
	}
}

func TestLoadCommentsForUpsert_SetsExternalID(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		ReviewRound: 2,
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "abc-123", StartLine: 10, EndLine: 15, Body: "refactor this"},
				},
			},
		},
	}
	writeCritJSONForTest(t, dir, cj)

	comments, _ := loadCommentsForUpsert(dir, []string{"main.go"})
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].ExternalID != "abc-123" {
		t.Errorf("expected ExternalID abc-123, got %q", comments[0].ExternalID)
	}
}

func TestLoadCommentsForUpsert_ReviewLevelComments(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{
		ReviewRound: 1,
		ReviewComments: []Comment{
			{ID: "r1", Body: "open review note", Resolved: false},
			{ID: "r2", Body: "resolved review note", Resolved: true},
		},
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "file comment"},
				},
			},
		},
	}
	writeCritJSONForTest(t, dir, cj)

	comments, _ := loadCommentsForUpsert(dir, []string{"plan.md"})

	// Should have 2 comments: 1 file-level + 1 unresolved review-level
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments (1 file + 1 unresolved review), got %d", len(comments))
	}
	var reviewComment *shareComment
	for i, c := range comments {
		if c.Scope == "review" {
			reviewComment = &comments[i]
		}
	}
	if reviewComment == nil {
		t.Fatal("expected a review-level comment")
	}
	if reviewComment.Body != "open review note" {
		t.Errorf("expected unresolved review comment, got %q", reviewComment.Body)
	}
	if reviewComment.ExternalID != "r1" {
		t.Errorf("expected ExternalID r1, got %q", reviewComment.ExternalID)
	}
}

func TestFetchNewWebComments_FiltersLocalComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"body": "web reviewer note", "file_path": "plan.md",
				"start_line": 5, "end_line": 5, "review_round": 1,
				"resolved": false, "external_id": nil,
			},
			{
				"body": "existing local", "file_path": "plan.md",
				"start_line": 1, "end_line": 1, "review_round": 1,
				"resolved": false, "external_id": "c1",
			},
		})
	}))
	defer srv.Close()

	localIDs := map[string]bool{"c1": true}
	got, err := fetchNewWebComments(srv.URL+"/r/testtoken", localIDs, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 new comment, got %d", len(got))
	}
	if got[0].Body != "web reviewer note" {
		t.Errorf("expected body 'web reviewer note', got %q", got[0].Body)
	}
}

func TestFetchNewWebComments_404ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got, err := fetchNewWebComments(srv.URL+"/r/gone", nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error for 404: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for 404, got %v", got)
	}
}

func TestFetchNewWebComments_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchNewWebComments(srv.URL+"/r/broken", nil, nil, "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestBuildSharePayload_SingleFile(t *testing.T) {
	files := []shareFile{
		{Path: "plan.md", Content: "# My Plan\n\nStep 1: do the thing"},
	}
	payload := buildSharePayload(files, nil, 1)

	// Multi-file format is always used
	pFiles, ok := payload["files"].([]map[string]string)
	if !ok {
		t.Fatal("expected files array in payload")
	}
	if len(pFiles) != 1 {
		t.Fatalf("expected 1 file, got %d", len(pFiles))
	}
	if pFiles[0]["path"] != "plan.md" {
		t.Errorf("expected path plan.md, got %s", pFiles[0]["path"])
	}
	if pFiles[0]["content"] != "# My Plan\n\nStep 1: do the thing" {
		t.Errorf("unexpected content: %s", pFiles[0]["content"])
	}
	if payload["review_round"] != 1 {
		t.Errorf("expected review_round 1, got %v", payload["review_round"])
	}
	comments, ok := payload["comments"].([]shareComment)
	if !ok {
		t.Fatal("expected comments array")
	}
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestBuildSharePayload_MultiFile(t *testing.T) {
	files := []shareFile{
		{Path: "plan.md", Content: "# Plan"},
		{Path: "src/main.go", Content: "package main"},
	}
	payload := buildSharePayload(files, nil, 2)

	pFiles := payload["files"].([]map[string]string)
	if len(pFiles) != 2 {
		t.Fatalf("expected 2 files, got %d", len(pFiles))
	}
	if payload["review_round"] != 2 {
		t.Errorf("expected review_round 2, got %v", payload["review_round"])
	}
}

func TestBuildSharePayload_WithComments(t *testing.T) {
	files := []shareFile{
		{Path: "plan.md", Content: "# Plan"},
	}
	comments := []shareComment{
		{File: "plan.md", StartLine: 1, EndLine: 3, Body: "Needs more detail", Author: "Claude"},
	}
	payload := buildSharePayload(files, comments, 1)

	pComments := payload["comments"].([]shareComment)
	if len(pComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(pComments))
	}
	if pComments[0].Author != "Claude" {
		t.Errorf("expected author Claude, got %s", pComments[0].Author)
	}
}

func TestShareFilesToWeb_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/reviews" {
			t.Errorf("expected /api/reviews, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		files, ok := payload["files"].([]any)
		if !ok || len(files) != 1 {
			t.Fatalf("expected 1 file in payload")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/abc123",
			"delete_token": "tok_secret",
		})
	}))
	defer server.Close()

	files := []shareFile{{Path: "plan.md", Content: "# Plan"}}
	url, token, err := shareFilesToWeb(files, nil, server.URL, 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://crit.md/r/abc123" {
		t.Errorf("expected url https://crit.md/r/abc123, got %s", url)
	}
	if token != "tok_secret" {
		t.Errorf("expected token tok_secret, got %s", token)
	}
}

func TestShareFilesToWeb_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "content too large"})
	}))
	defer server.Close()

	files := []shareFile{{Path: "plan.md", Content: "# Plan"}}
	_, _, err := shareFilesToWeb(files, nil, server.URL, 1, "")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestShareFilesToWeb_NetworkError(t *testing.T) {
	files := []shareFile{{Path: "plan.md", Content: "# Plan"}}
	_, _, err := shareFilesToWeb(files, nil, "http://localhost:1", 1, "")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestUnpublishFromWeb_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/reviews" {
			t.Errorf("expected /api/reviews, got %s", r.URL.Path)
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["delete_token"] != "tok_secret" {
			t.Errorf("expected delete_token tok_secret, got %s", body["delete_token"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := unpublishFromWeb(server.URL, "tok_secret", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnpublishFromWeb_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer server.Close()

	err := unpublishFromWeb(server.URL, "bad_token", "")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestUnpublishFromWeb_AlreadyDeleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// 404 is treated as "already deleted" — not an error (idempotent)
	err := unpublishFromWeb(server.URL, "old_token", "")
	if err != nil {
		t.Fatalf("not-found should not be an error (already deleted): %v", err)
	}
}

func TestLoadCommentsForFiles(t *testing.T) {
	dir := t.TempDir()
	critJSON := CritJSON{
		ReviewRound: 2,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 3, Body: "Fix this", Author: "Alice"},
					{ID: "c2", StartLine: 5, EndLine: 5, Body: "Good", Author: "Bob", ReviewRound: 2},
					{ID: "c3", StartLine: 7, EndLine: 7, Body: "Resolved one", Author: "Alice", Resolved: true},
				},
			},
			"other.go": {
				Comments: []Comment{
					{ID: "c4", StartLine: 10, EndLine: 15, Body: "Refactor", Author: "Alice"},
					{ID: "c5", StartLine: 20, EndLine: 20, Body: "Done", Author: "Bob", Resolved: true},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(critJSON, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	// Only load unresolved comments for plan.md (c1 and c2, not c3)
	comments, round := loadCommentsForShare(dir, []string{"plan.md"})
	if round != 2 {
		t.Errorf("expected round 2, got %d", round)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 unresolved comments, got %d", len(comments))
	}
	if comments[0].File != "plan.md" {
		t.Errorf("expected file plan.md, got %s", comments[0].File)
	}

	// Load for both files — 3 unresolved (c1, c2, c4), not 5 total
	comments, _ = loadCommentsForShare(dir, []string{"plan.md", "other.go"})
	if len(comments) != 3 {
		t.Fatalf("expected 3 unresolved comments, got %d", len(comments))
	}

	// Load for nonexistent file
	comments, round = loadCommentsForShare(dir, []string{"nope.md"})
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
	if round != 2 {
		t.Errorf("expected round 2 even with no matching comments, got %d", round)
	}
}

func TestLoadCommentsForFiles_NoCritJSON(t *testing.T) {
	dir := t.TempDir()
	comments, round := loadCommentsForShare(dir, []string{"plan.md"})
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
	if round != 1 {
		t.Errorf("expected default round 1, got %d", round)
	}
}

func TestPersistShareState(t *testing.T) {
	dir := t.TempDir()

	// Persist to new .crit.json
	err := persistShareState(dir, "https://crit.md/r/abc", "tok_123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify
	data, _ := os.ReadFile(filepath.Join(dir, ".crit.json"))
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if cj.ShareURL != "https://crit.md/r/abc" {
		t.Errorf("expected share_url, got %s", cj.ShareURL)
	}
	if cj.DeleteToken != "tok_123" {
		t.Errorf("expected delete_token, got %s", cj.DeleteToken)
	}
}

func TestPersistShareState_PreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write initial .crit.json with comments
	initial := CritJSON{
		Branch:      "main",
		ReviewRound: 2,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{ID: "c1", Body: "test"}}},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	// Persist share state
	err := persistShareState(dir, "https://crit.md/r/def", "tok_456", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back — comments and branch should be preserved
	data, _ = os.ReadFile(filepath.Join(dir, ".crit.json"))
	var cj CritJSON
	json.Unmarshal(data, &cj)
	if cj.ShareURL != "https://crit.md/r/def" {
		t.Errorf("expected share_url")
	}
	if cj.Branch != "main" {
		t.Errorf("expected branch main preserved, got %s", cj.Branch)
	}
	if len(cj.Files["plan.md"].Comments) != 1 {
		t.Errorf("expected comments preserved")
	}
}

func TestUpsertShareToWeb_CallsPUTOnChange(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
			json.NewEncoder(w).Encode(map[string]any{
				"url": "http://example.com/r/tok", "review_round": 2, "changed": true,
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := CritJSON{
		ShareURL:      srv.URL + "/r/tok",
		DeleteToken:   "dt",
		LastShareHash: "old-hash",
		ReviewRound:   1,
	}
	files := []shareFile{{Path: "plan.md", Content: "# changed"}}
	comments := []shareComment{}

	result, err := upsertShareToWeb(cfg, files, comments, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !putCalled {
		t.Error("expected PUT to be called")
	}
	if !result.Changed {
		t.Error("expected result.Changed to be true")
	}
	if result.ReviewRound != 2 {
		t.Errorf("expected ReviewRound 2, got %d", result.ReviewRound)
	}
}

func TestUpsertShareToWeb_SkipsPUTWhenUnchanged(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	files := []shareFile{{Path: "plan.md", Content: "same"}}
	comments := []shareComment{}
	currentHash := computeShareHash(files, comments)

	cfg := CritJSON{
		ShareURL:      srv.URL + "/r/tok",
		DeleteToken:   "dt",
		LastShareHash: currentHash,
		ReviewRound:   1,
	}

	result, err := upsertShareToWeb(cfg, files, comments, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if putCalled {
		t.Error("expected PUT NOT to be called when hash unchanged")
	}
	if result.Changed {
		t.Error("expected result.Changed to be false")
	}
}

func TestClearShareState(t *testing.T) {
	dir := t.TempDir()

	cj := CritJSON{
		ShareURL:    "https://crit.md/r/old",
		DeleteToken: "tok_old",
		Files:       map[string]CritJSONFile{"plan.md": {Comments: []Comment{{ID: "c1", Body: "test"}}}},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644)

	err := clearShareState(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ = os.ReadFile(filepath.Join(dir, ".crit.json"))
	var cleared CritJSON
	json.Unmarshal(data, &cleared)
	if cleared.ShareURL != "" {
		t.Errorf("expected share_url cleared, got %s", cleared.ShareURL)
	}
	if cleared.DeleteToken != "" {
		t.Errorf("expected delete_token cleared, got %s", cleared.DeleteToken)
	}
	// Comments should still be there
	if len(cleared.Files["plan.md"].Comments) != 1 {
		t.Errorf("expected comments preserved after clearing share state")
	}
}

func TestBuildShareFromSession(t *testing.T) {
	s := &Session{
		ReviewRound: 3,
		Files: []*FileEntry{
			{
				Path:    "plan.md",
				Content: "# Plan",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "Open comment", Author: "Alice", Quote: "# Plan"},
					{ID: "c2", StartLine: 2, EndLine: 2, Body: "Resolved comment", Author: "Bob", Resolved: true},
					{ID: "c3", StartLine: 3, EndLine: 3, Body: "Another open", Author: "Alice", ReviewRound: 2},
				},
			},
			{
				Path:    "src/main.go",
				Content: "package main",
				Comments: []Comment{
					{ID: "c4", StartLine: 10, EndLine: 15, Body: "All resolved", Resolved: true},
				},
			},
		},
	}

	files, comments, round := buildShareFromSession(s)
	if round != 3 {
		t.Errorf("expected round 3, got %d", round)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "plan.md" || files[0].Content != "# Plan" {
		t.Errorf("unexpected first file: %+v", files[0])
	}
	// Only unresolved comments: c1 and c3
	if len(comments) != 2 {
		t.Fatalf("expected 2 unresolved comments, got %d", len(comments))
	}
	if comments[0].Body != "Open comment" {
		t.Errorf("expected first comment body 'Open comment', got %s", comments[0].Body)
	}
	if comments[0].Quote != "# Plan" {
		t.Errorf("expected first comment quote '# Plan', got %s", comments[0].Quote)
	}
	if comments[1].ReviewRound != 2 {
		t.Errorf("expected review_round 2 on second comment, got %d", comments[1].ReviewRound)
	}
}

func TestBuildShareFromSession_NoComments(t *testing.T) {
	s := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{Path: "readme.md", Content: "# Hello"},
		},
	}

	files, comments, round := buildShareFromSession(s)
	if round != 1 {
		t.Errorf("expected round 1, got %d", round)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestHandleShare_Success(t *testing.T) {
	// Mock crit-web server
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)

		// Verify only unresolved comments are sent
		comments := payload["comments"].([]any)
		if len(comments) != 1 {
			t.Errorf("expected 1 unresolved comment in payload, got %d", len(comments))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/test123",
			"delete_token": "tok_test",
		})
	}))
	defer critWeb.Close()

	sess := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:    "plan.md",
				Content: "# Plan",
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"},
					{ID: "c2", StartLine: 2, EndLine: 2, Body: "Done", Resolved: true},
				},
			},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	srv := &Server{session: sess, shareURL: critWeb.URL}
	srv.ready.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	srv.handleShare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["url"] != "https://crit.md/r/test123" {
		t.Errorf("expected url, got %v", result["url"])
	}
	if result["delete_token"] != "tok_test" {
		t.Errorf("expected delete_token, got %v", result["delete_token"])
	}
}

func TestHandleShare_ShareServiceError(t *testing.T) {
	critWeb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer critWeb.Close()

	sess := &Session{
		ReviewRound: 1,
		Files: []*FileEntry{
			{Path: "plan.md", Content: "# Plan"},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	srv := &Server{session: sess, shareURL: critWeb.URL}
	srv.ready.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	srv.handleShare(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestHandleShare_NoShareURL(t *testing.T) {
	srv := &Server{session: &Session{}, shareURL: ""}
	srv.ready.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	srv.handleShare(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleShare_WrongMethod(t *testing.T) {
	srv := &Server{session: &Session{}, shareURL: "https://crit.md"}
	req := httptest.NewRequest(http.MethodGet, "/api/share", nil)
	w := httptest.NewRecorder()
	srv.handleShare(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleShare_AlreadyShared(t *testing.T) {
	// Create a mock crit-web server (should NOT be called)
	called := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/new-token",
			"delete_token": "new-del-token",
		})
	}))
	defer mockServer.Close()

	// Create session with existing share state (matches existing test patterns)
	sess := &Session{
		OutputDir:   t.TempDir(),
		Files:       []*FileEntry{{Path: "plan.md", Content: "# Plan"}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	sess.SetSharedURLAndToken("https://crit.md/r/existing", "existing-del-token")

	srv := &Server{session: sess, shareURL: mockServer.URL}
	srv.ready.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	w := httptest.NewRecorder()
	srv.handleShare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["url"] != "https://crit.md/r/existing" {
		t.Errorf("expected existing URL, got %v", result["url"])
	}
	if result["delete_token"] != "existing-del-token" {
		t.Errorf("expected existing delete token, got %v", result["delete_token"])
	}
	if called {
		t.Error("crit-web should NOT have been called for an already-shared review")
	}
}

func TestLoadExistingShareCfg(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit.json")

	// Legacy .crit.json without scope — loads unconditionally
	cj := CritJSON{
		ShareURL:    "https://crit.md/r/existing",
		DeleteToken: "del-token-123",
		Files:       map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(critPath, data, 0644)

	cfg, ok := loadExistingShareCfg(dir, []string{"anything.md"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if cfg.ShareURL != "https://crit.md/r/existing" {
		t.Errorf("expected existing URL, got %q", cfg.ShareURL)
	}
	if cfg.DeleteToken != "del-token-123" {
		t.Errorf("expected existing token, got %q", cfg.DeleteToken)
	}
}

func TestLoadExistingShareCfg_NoCritJSON(t *testing.T) {
	dir := t.TempDir()
	_, ok := loadExistingShareCfg(dir, []string{"plan.md"})
	if ok {
		t.Error("expected ok=false when no .crit.json exists")
	}
}

func TestLoadExistingShareCfg_NoShareState(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit.json")
	cj := CritJSON{Files: map[string]CritJSONFile{}}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(critPath, data, 0644)

	_, ok := loadExistingShareCfg(dir, []string{"plan.md"})
	if ok {
		t.Error("expected ok=false when no share URL set")
	}
}

func TestLoadExistingShareCfg_ScopeMismatch(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit.json")

	cj := CritJSON{
		ShareURL:    "https://crit.md/r/old",
		DeleteToken: "old-token",
		ShareScope:  shareScope([]string{"old-plan.md"}),
		Files:       map[string]CritJSONFile{},
	}
	data, _ := json.MarshalIndent(cj, "", "  ")
	os.WriteFile(critPath, data, 0644)

	// Different file set — should NOT return share state
	_, ok := loadExistingShareCfg(dir, []string{"new-plan.md"})
	if ok {
		t.Error("expected ok=false for mismatched scope")
	}

	// Same file set — should return share state
	cfg, ok := loadExistingShareCfg(dir, []string{"old-plan.md"})
	if !ok {
		t.Fatal("expected ok=true for matching scope")
	}
	if cfg.ShareURL != "https://crit.md/r/old" {
		t.Errorf("expected URL for matching scope, got %q", cfg.ShareURL)
	}
}

func TestResolveShareURL(t *testing.T) {
	// Isolate from real ~/.crit.config.json
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	tests := []struct {
		name     string
		flag     string
		env      string
		expected string
	}{
		{
			name:     "flag takes priority",
			flag:     "https://custom.example.com",
			env:      "https://env.example.com",
			expected: "https://custom.example.com",
		},
		{
			name:     "env var used when no flag",
			flag:     "",
			env:      "https://env.example.com",
			expected: "https://env.example.com",
		},
		{
			name:     "default when nothing set",
			flag:     "",
			env:      "",
			expected: "https://crit.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("CRIT_SHARE_URL", tt.env)
			} else {
				t.Setenv("CRIT_SHARE_URL", "")
				os.Unsetenv("CRIT_SHARE_URL")
			}
			got := resolveShareURL(tt.flag, Config{})
			if got != tt.expected {
				t.Errorf("resolveShareURL(%q) = %q, want %q", tt.flag, got, tt.expected)
			}
		})
	}
}

func TestShareScope(t *testing.T) {
	// Same paths in different order produce same hash
	h1 := shareScope([]string{"b.md", "a.md"})
	h2 := shareScope([]string{"a.md", "b.md"})
	if h1 != h2 {
		t.Errorf("expected same hash regardless of order, got %q vs %q", h1, h2)
	}

	// Different paths produce different hash
	h3 := shareScope([]string{"c.md"})
	if h1 == h3 {
		t.Error("different file sets should produce different hashes")
	}

	// Empty produces a hash (not empty string)
	h4 := shareScope([]string{})
	if h4 == "" {
		t.Error("empty file set should still produce a hash")
	}
}

func TestBuildSharePayload_WithReplies(t *testing.T) {
	files := []shareFile{{Path: "f.md", Content: "hello"}}
	comments := []shareComment{{
		File: "f.md", StartLine: 1, EndLine: 1, Body: "fix this", Author: "Alice",
		Replies: []shareReply{
			{Body: "done", Author: "Bob"},
			{Body: "verified", Author: "Alice"},
		},
	}}
	payload := buildSharePayload(files, comments, 1)
	cs := payload["comments"].([]shareComment)
	if len(cs) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cs))
	}
	if len(cs[0].Replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(cs[0].Replies))
	}
	if cs[0].Replies[0].Body != "done" {
		t.Errorf("expected reply body 'done', got %q", cs[0].Replies[0].Body)
	}
	if cs[0].Replies[1].Author != "Alice" {
		t.Errorf("expected reply author 'Alice', got %q", cs[0].Replies[1].Author)
	}
}

func TestBuildShareFromSession_WithReplies(t *testing.T) {
	s := &Session{
		Files: []*FileEntry{{
			Path:    "f.md",
			Content: "hello",
			Comments: []Comment{{
				ID: "c1", StartLine: 1, EndLine: 1, Body: "fix",
				Author: "Alice",
				Replies: []Reply{
					{ID: "c1-r1", Body: "done", Author: "Bob"},
				},
			}},
		}},
		ReviewRound: 1,
	}
	_, comments, _ := buildShareFromSession(s)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
	if comments[0].Replies[0].Body != "done" {
		t.Errorf("expected reply body 'done', got %q", comments[0].Replies[0].Body)
	}
}

func TestBuildShareFromSession_SkipsResolvedWithReplies(t *testing.T) {
	s := &Session{
		Files: []*FileEntry{{
			Path:    "f.md",
			Content: "hello",
			Comments: []Comment{{
				ID: "c1", StartLine: 1, EndLine: 1, Body: "old issue",
				Resolved: true,
				Replies:  []Reply{{ID: "c1-r1", Body: "fixed"}},
			}},
		}},
		ReviewRound: 1,
	}
	_, comments, _ := buildShareFromSession(s)
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments (resolved should be skipped), got %d", len(comments))
	}
}

func TestShareFilesToWeb_SendsBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"url":          "https://crit.md/r/abc123",
			"delete_token": "tok_secret",
		})
	}))
	defer server.Close()

	files := []shareFile{{Path: "plan.md", Content: "# Plan"}}
	shareFilesToWeb(files, nil, server.URL, 1, "crit_testtoken")
	if gotAuth != "Bearer crit_testtoken" {
		t.Errorf("expected Authorization: Bearer crit_testtoken, got %q", gotAuth)
	}
}

func TestUnpublishFromWeb_SendsBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	unpublishFromWeb(server.URL+"/r/tok", "dt", "crit_testtoken")
	if gotAuth != "Bearer crit_testtoken" {
		t.Errorf("expected Authorization: Bearer crit_testtoken, got %q", gotAuth)
	}
}

func TestFetchNewWebComments_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	fetchNewWebComments(srv.URL+"/r/tok", nil, nil, "crit_testtoken")
	if gotAuth != "Bearer crit_testtoken" {
		t.Errorf("expected Authorization: Bearer crit_testtoken, got %q", gotAuth)
	}
}

func TestUpsertShareToWeb_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := CritJSON{
		ShareURL:      srv.URL + "/r/tok",
		DeleteToken:   "dt",
		LastShareHash: "same",
		ReviewRound:   1,
	}
	files := []shareFile{{Path: "plan.md", Content: "same"}}
	upsertShareToWeb(cfg, files, []shareComment{}, "crit_testtoken")
	if gotAuth != "Bearer crit_testtoken" {
		t.Errorf("expected Authorization: Bearer crit_testtoken, got %q", gotAuth)
	}
}

func TestSetBearer_SetsHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	setBearer(req, "crit_abc123")
	if got := req.Header.Get("Authorization"); got != "Bearer crit_abc123" {
		t.Errorf("expected Bearer crit_abc123, got %q", got)
	}
}

func TestSetBearer_NoopWhenEmpty(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	setBearer(req, "")
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("expected empty Authorization header, got %q", got)
	}
}
