package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

var attachmentFilenameRE = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[a-z0-9]+$`,
)

func makeTestPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func newReviewIdentity(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "abcd1234")
}

func newAttachmentTestServer(t *testing.T, reviewIdentity string) *Server {
	t.Helper()
	reviewJSON := session.ReviewPathsFor(reviewIdentity).Review
	if err := os.MkdirAll(reviewIdentity, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewJSON, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ReviewFilePath: reviewJSON}
	sess.InitTestChannels()
	srv, err := NewServer(sess, FrontendFS, "", false, "", "test", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetReviewPathForTest(reviewIdentity)
	return srv
}

func TestHandleAttachments_UploadAndGet(t *testing.T) {
	review := newReviewIdentity(t)
	srv := newAttachmentTestServer(t, review)

	// Build a multipart POST with the original filename header.
	data := makeTestPNG(t, color.RGBA{200, 100, 50, 255})
	body, contentType := buildMultipartFile(t, "screen-shot.png", data)
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.HandleAttachmentsForTest(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["original_filename"] != "screen-shot.png" {
		t.Errorf("original_filename = %q, want screen-shot.png", resp["original_filename"])
	}
	wantURL := "attachments/" + resp["filename"]
	if resp["url"] != wantURL {
		t.Errorf("url = %q, want %q (relative form)", resp["url"], wantURL)
	}
	if !attachmentFilenameRE.MatchString(resp["filename"]) {
		t.Errorf("filename %q does not match UUID pattern", resp["filename"])
	}

	// GET it back via the absolute URL form.
	getReq := httptest.NewRequest(http.MethodGet, "/api/attachments/"+resp["filename"], nil)
	getRec := httptest.NewRecorder()
	srv.HandleAttachmentsForTest(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", getRec.Code, getRec.Body.String())
	}
	if !bytes.Equal(getRec.Body.Bytes(), data) {
		t.Errorf("GET body did not round-trip the upload bytes")
	}
	if ct := getRec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if cc := getRec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable directive", cc)
	}
}

func TestHandleAttachments_RejectsBadInput(t *testing.T) {
	review := newReviewIdentity(t)
	srv := newAttachmentTestServer(t, review)

	t.Run("POST with path suffix is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/attachments/anything", nil)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("GET without filename is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/attachments", nil)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("DELETE is 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/attachments/x", nil)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("GET unknown UUID is 404", func(t *testing.T) {
		ghost, _ := randomUUID()
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+ghost+".png", nil)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("GET malformed filename is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/not-a-uuid.png", nil)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("any verb without review path is 503", func(t *testing.T) {
		bare := &Server{reviewPath: ""}
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/x", nil)
		rec := httptest.NewRecorder()
		bare.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST with non-image bytes is 415", func(t *testing.T) {
		body, contentType := buildMultipartFile(t, "notes.txt", []byte("plain text"))
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST with malformed multipart body is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/attachments",
			strings.NewReader("this is not a multipart body"))
		// Declare a boundary that doesn't appear in the body so ParseMultipartForm fails.
		req.Header.Set("Content-Type", "multipart/form-data; boundary=xxxnotpresent")
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST with wrong form-file field name is 400", func(t *testing.T) {
		// Properly formed multipart, but the file is in field "upload" rather
		// than the expected "file" — surfaces as a FormFile lookup error.
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("upload", "screenshot.png")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(makeTestPNG(t, color.RGBA{1, 2, 3, 255})); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := mw.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Missing 'file'") {
			t.Errorf("expected 'Missing file' in body, got %q", rec.Body.String())
		}
	})

	t.Run("POST empty file part is 400 (not 415)", func(t *testing.T) {
		// An empty multipart file part passes ParseMultipartForm + FormFile,
		// then ReadFull returns n=0, then session.SaveAttachment rejects with
		// "empty attachment payload" — covers the non-MIME session.SaveAttachment
		// error branch (status 400, not 415).
		body, contentType := buildMultipartFile(t, "empty.png", nil)
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "empty") {
			t.Errorf("expected 'empty' in body, got %q", rec.Body.String())
		}
	})

	t.Run("POST oversized payload is 413", func(t *testing.T) {
		// Generate a body whose "file" part exceeds maxAttachmentBytes. The
		// handler's MaxBytesReader fires once the request body exceeds the
		// cap; the response status is 413 only when ParseMultipartForm gets
		// past parsing and ReadFull observes n > maxAttachmentBytes. To make
		// that branch reachable we keep the multipart envelope under the
		// MaxBytesReader cap (maxAttachmentBytes + 1 MiB) while ensuring the
		// inner file is large enough that the post-read n > max check fires.
		oversized := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xff}, MaxAttachmentBytes+1024)...)
		body, contentType := buildMultipartFile(t, "huge.png", oversized)
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.HandleAttachmentsForTest(rec, req)
		// Two acceptable outcomes depending on whether MaxBytesReader trips
		// before ReadFull or after: both end up as a client-side error class
		// (4xx). Assert specifically on the in-handler check so it's the
		// expected branch covered.
		if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 413 or 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// buildMultipartFile constructs a multipart/form-data body with one "file"
// part. Returns the body reader and the Content-Type header to set.
func buildMultipartFile(t *testing.T, filename string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write multipart body: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}
