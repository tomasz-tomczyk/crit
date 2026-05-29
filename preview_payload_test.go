package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newPreviewServer builds a preview-mode Server+Session backed by a small HTML
// fixture that references a CSS file and a binary PNG, exercising both the text
// and base64 crawl paths.
func newPreviewServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	html := `<!DOCTYPE html><html><head><link rel="stylesheet" href="style.css"></head>` +
		`<body><img src="logo.png"></body></html>`
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte("body{margin:0}"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	srv := &Server{}
	srv.SetSession(&Session{ReviewType: "preview", Origin: htmlPath, Mode: "files"})
	return srv
}

// TestHandlePreviewPayload verifies the proxy-auth relay endpoint crawls the
// preview origin's assets and returns a payload tagged review_type=preview with
// a base64 binary entry.
func TestHandlePreviewPayload(t *testing.T) {
	srv := newPreviewServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/share/preview-payload", nil)
	rr := httptest.NewRecorder()
	srv.handlePreviewPayload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["review_type"] != "preview" {
		t.Errorf("review_type = %v, want preview", payload["review_type"])
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatalf("files = %v, want non-empty slice", payload["files"])
	}
	if !previewPayloadHasBase64(files) {
		t.Errorf("expected a base64 file entry, got %v", files)
	}
}

// TestShareFilesForSessionPreview verifies the in-UI Share path (handleShare's
// file source) crawls preview assets instead of reading on-disk review files,
// and tags the share with review_type=preview.
func TestShareFilesForSessionPreview(t *testing.T) {
	srv := newPreviewServer(t)

	files, reviewType, err := srv.shareFilesForSession()
	if err != nil {
		t.Fatalf("shareFilesForSession: %v", err)
	}
	if reviewType != "preview" {
		t.Errorf("reviewType = %q, want preview", reviewType)
	}
	if !previewShareFileHasBase64(files) {
		t.Errorf("expected a base64 file entry, got %v", files)
	}
	if !previewShareFileHasPath(files, "index.html") {
		t.Errorf("expected index.html in crawled files, got %v", files)
	}
}

func previewPayloadHasBase64(files []any) bool {
	for _, f := range files {
		if m, ok := f.(map[string]any); ok && m["encoding"] == "base64" {
			return true
		}
	}
	return false
}

func previewShareFileHasBase64(files []shareFile) bool {
	for _, f := range files {
		if f.Encoding == "base64" {
			return true
		}
	}
	return false
}

func previewShareFileHasPath(files []shareFile, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}
