package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestRemapPreviewCommentFiles checks the re-keying primitive: per-file preview
// comments move to the crawl entry key, review-level comments are untouched.
func TestRemapPreviewCommentFiles(t *testing.T) {
	comments := []shareComment{
		{File: "sub/dir/index.html", Body: "line comment", StartLine: 3},
		{File: "", Scope: "review", Body: "review-level"},
	}
	remapPreviewCommentFiles(comments)

	if comments[0].File != previewMainHTMLKey {
		t.Errorf("per-file comment File = %q, want %q", comments[0].File, previewMainHTMLKey)
	}
	if comments[1].File != "" {
		t.Errorf("review-level comment File = %q, want empty (untouched)", comments[1].File)
	}
}

// TestShareReviewFiles_PreviewRemapsComments is the direct-transport regression
// for the "preview share drops my comments" bug: comments are stored under the
// session's previewed-file path, but the crawled payload keys the HTML as
// previewMainHTMLKey. shareReviewFiles must re-key them so crit-web attaches
// them to the entry. Before the fix the payload carried zero comments.
func TestShareReviewFiles_PreviewRemapsComments(t *testing.T) {
	dir := t.TempDir()
	review := filepath.Join(dir, "review.json")
	cj := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"sub/index.html": {Comments: []Comment{
				{ID: "c1", StartLine: 2, EndLine: 2, Body: "unresolved here", Author: "Alice", Scope: "line"},
				// Resolved comments are dropped at load (before the re-key); this
				// guards the filter-then-remap ordering so a future refactor can't
				// leak a resolved comment into the shared payload.
				{ID: "c2", StartLine: 4, EndLine: 4, Body: "resolved", Author: "Alice", Scope: "line", Resolved: true},
			}},
		},
	}
	if err := saveCritJSON(review, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"http://stub/r/x","delete_token":"tok"}`))
	}))
	defer srv.Close()

	// files = crawled snapshot (keyed index.html); comment-source path is the
	// session path "sub/index.html"; reviewType=preview triggers the re-key.
	files := []shareFile{{Path: previewMainHTMLKey, Content: "<html></html>"}}
	if _, err := shareReviewFiles(review, files, []string{"sub/index.html"}, srv.URL, "", "Alice", "", "", "preview"); err != nil {
		t.Fatalf("shareReviewFiles: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(captured, &payload); err != nil {
		t.Fatalf("decode payload: %v\nbody=%q", err, string(captured))
	}
	comments, _ := payload["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment in payload, got %d\nbody=%s", len(comments), captured)
	}
	if got := comments[0].(map[string]any)["file"]; got != previewMainHTMLKey {
		t.Errorf("shared comment file = %v, want %q", got, previewMainHTMLKey)
	}
}

// TestHandlePreviewPayload_IncludesRemappedComments is the proxy-transport
// counterpart: the relay payload builder previously passed nil comments, so
// sharing via popup uploaded no comments at all. It must now carry the
// previewed file's comments, re-keyed to the crawl entry.
func TestHandlePreviewPayload_IncludesRemappedComments(t *testing.T) {
	dir := t.TempDir()
	review := filepath.Join(dir, "review.json")
	origin, err := filepath.Abs(filepath.Join("testdata", "preview", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	const sessPath = "testdata/preview/index.html"
	// DOM pins are stored under the iframe pathname (/preview-content) in a
	// separate "live-route" FileEntry — NOT the previewed HTML's path. This
	// mirrors how the live composer persists preview comments (AddLivePin), and
	// is exactly the case the previous paths[0]-only code missed.
	cj := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/preview-content": {Comments: []Comment{
				{ID: "c1", StartLine: 0, EndLine: 0, Body: "pin on the page", Author: "Alice",
					DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "body > h1"}},
			}},
		},
	}
	if err := saveCritJSON(review, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	sess := previewSessionWithPin(dir, origin, review, sessPath)
	s, err := NewServer(sess, frontendFS, "", false, "", "Alice", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.SetSession(sess)

	req := httptest.NewRequest(http.MethodGet, "/api/share/preview-payload", nil)
	rec := httptest.NewRecorder()
	s.handlePreviewPayload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	comments, _ := payload["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d\nbody=%s", len(comments), rec.Body.String())
	}
	if got := comments[0].(map[string]any)["file"]; got != previewMainHTMLKey {
		t.Errorf("comment file = %v, want %q", got, previewMainHTMLKey)
	}
}

// previewSessionWithPin builds a preview Session with two FileEntries: the
// previewed HTML ("code") and a live-route entry holding DOM pins, matching how
// a real preview session looks after the user adds a comment (AddLivePin
// auto-creates the live-route entry keyed by the iframe pathname).
func previewSessionWithPin(dir, origin, review, sessPath string) *Session {
	return &Session{
		Mode:           "files",
		ReviewType:     "preview",
		RepoRoot:       dir,
		Origin:         origin,
		ReviewFilePath: review,
		ReviewRound:    1,
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
		Files: []*FileEntry{
			{Path: sessPath, AbsPath: origin, FileType: "code", Content: "<html></html>"},
			{Path: "/preview-content", FileType: "live-route", Status: "added"},
		},
	}
}

// TestHandleUpsertPayload_PreviewIncludesPins is the re-share regression for
// "no files in session" + dropped comments: the upsert builder used
// LoadShareFilesFromDisk (empty for a preview session, whose FileEntry has no
// AbsPath) and never re-keyed pins. It must crawl the preview and include the
// DOM pins, re-keyed to the crawl entry.
func TestHandleUpsertPayload_PreviewIncludesPins(t *testing.T) {
	dir := t.TempDir()
	review := filepath.Join(dir, "review.json")
	origin, err := filepath.Abs(filepath.Join("testdata", "preview", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		ReviewRound: 2,
		ShareURL:    "https://crit.example.com/r/tok",
		DeleteToken: "dtok",
		Files: map[string]CritJSONFile{
			"/preview-content": {Comments: []Comment{
				{ID: "c1", StartLine: 0, EndLine: 0, Body: "pin", Author: "Alice",
					DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "body"}},
			}},
		},
	}
	if err := saveCritJSON(review, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	sess := previewSessionWithPin(dir, origin, review, "testdata/preview/index.html")
	sess.SetSharedURLAndToken("https://crit.example.com/r/tok", "dtok")
	s, err := NewServer(sess, frontendFS, "", false, "", "Alice", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.SetSession(sess)

	req := httptest.NewRequest(http.MethodGet, "/api/share/upsert-payload", nil)
	rec := httptest.NewRecorder()
	s.handleUpsertPayload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (want 200; 'no files in session' is the bug), body = %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if files, _ := payload["files"].([]any); len(files) == 0 {
		t.Fatalf("upsert payload has no files — the 'no files in session' bug\nbody=%s", rec.Body.String())
	}
	comments, _ := payload["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d\nbody=%s", len(comments), rec.Body.String())
	}
	if got := comments[0].(map[string]any)["file"]; got != previewMainHTMLKey {
		t.Errorf("comment file = %v, want %q", got, previewMainHTMLKey)
	}
}

// TestPreviewShareState_RestoresWithSessionScope is the regression for the
// "shared status lost on restart → re-share duplicates" bug. The share scope
// must be derived from the session's stable file identity (the single previewed
// file), not the crawled asset set — otherwise restoreShareStateLocked, which
// recomputes scope from s.Files, never matches and drops the share pointer.
func TestPreviewShareState_RestoresWithSessionScope(t *testing.T) {
	makeSession := func(t *testing.T) *Session {
		dir := t.TempDir()
		htmlPath := filepath.Join(dir, "sub", "index.html")
		if err := os.MkdirAll(filepath.Dir(htmlPath), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, htmlPath, "<html></html>")
		return &Session{
			RepoRoot:      dir,
			ReviewType:    "preview",
			ReviewRound:   1,
			subscribers:   make(map[chan SSEEvent]struct{}),
			roundComplete: make(chan struct{}, 1),
			Files: []*FileEntry{
				{Path: "sub/index.html", AbsPath: htmlPath, FileType: "code", Content: "<html></html>"},
			},
		}
	}
	writeShare := func(t *testing.T, s *Session, scope string) {
		review := filepath.Join(s.RepoRoot, "review.json")
		cj := CritJSON{
			ShareURL:    "https://crit.example.com/r/tok",
			DeleteToken: "dtok",
			ShareScope:  scope,
			Files:       map[string]CritJSONFile{},
		}
		data, err := json.Marshal(cj)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, review, string(data))
		s.ReviewFilePath = review
	}

	t.Run("session-identity scope restores", func(t *testing.T) {
		s := makeSession(t)
		// shareScope over the session's file paths is exactly what handleShare
		// now persists (and what restoreShareStateLocked recomputes on restart).
		writeShare(t, s, shareScope(s.FilePathsSnapshot()))
		s.loadCritJSON()
		if s.sharedURL == "" || s.deleteToken == "" {
			t.Fatalf("share state not restored: url=%q token=%q", s.sharedURL, s.deleteToken)
		}
	})

	t.Run("crawl-set scope (pre-fix) does not restore", func(t *testing.T) {
		s := makeSession(t)
		// The old bug persisted scope over the crawled asset set; the single-file
		// session can never reproduce it, so the share pointer is dropped.
		writeShare(t, s, shareScope([]string{"index.html", "style.css", "app.js"}))
		s.loadCritJSON()
		if s.sharedURL != "" {
			t.Errorf("share state restored with crawl-set scope %q — the bug should keep it empty", s.sharedURL)
		}
	})
}
