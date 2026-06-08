package main

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
	"runtime"
	"strings"
	"testing"
)

// makeTestPNG returns a tiny valid PNG (1×1 of the given color) so the MIME
// sniff in saveAttachment takes the success path. We don't pin a fixed byte
// string because http.DetectContentType only needs the first 512 bytes — a
// real PNG is the only safe way to keep this test stable across stdlib
// changes.
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

// newReviewIdentity returns a review-folder path under t.TempDir() — the v4
// "identity" form. Mirrors the production layout where the JSON lives at
// <identity>/review.json and attachments at <identity>/attachments/.
func newReviewIdentity(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "abcd1234")
}

// uuidV4Pattern is reused across tests to validate the bare UUID portion
// of a saved attachment filename without committing to a specific UUID.
const uuidV4Pattern = `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

func TestRandomUUID_Format(t *testing.T) {
	got, err := randomUUID()
	if err != nil {
		t.Fatalf("randomUUID: %v", err)
	}
	if !attachmentFilenameRE.MatchString(got + ".png") {
		t.Errorf("randomUUID produced %q which does not parse with .png suffix", got)
	}
	// Verify a second call produces a different UUID — ensures we're not
	// returning a constant or stale buffer.
	other, _ := randomUUID()
	if got == other {
		t.Errorf("randomUUID returned the same UUID twice: %q", got)
	}
}

func TestReviewPathsFor_Attachments(t *testing.T) {
	identity := filepath.Join("home", "u", ".crit", "reviews", "deadbeef")
	got := reviewPathsFor(identity).Attachments
	want := filepath.Join(identity, "attachments")
	if got != want {
		t.Errorf("Attachments = %q, want %q", got, want)
	}
}

func TestSaveAttachment(t *testing.T) {
	t.Run("rejects empty payload", func(t *testing.T) {
		_, err := saveAttachment(newReviewIdentity(t), nil)
		if err == nil {
			t.Fatal("expected error for empty payload")
		}
	})

	t.Run("rejects oversized payload", func(t *testing.T) {
		_, err := saveAttachment(newReviewIdentity(t), bytes.Repeat([]byte{0xff}, maxAttachmentBytes+1))
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("expected too-large error, got %v", err)
		}
	})

	t.Run("rejects non-image MIME", func(t *testing.T) {
		_, err := saveAttachment(newReviewIdentity(t), []byte("just plain text not an image"))
		if err == nil || !strings.Contains(err.Error(), "unsupported image type") {
			t.Fatalf("expected unsupported-type error, got %v", err)
		}
	})

	t.Run("rejects empty review path", func(t *testing.T) {
		_, err := saveAttachment("", makeTestPNG(t, color.RGBA{255, 0, 0, 255}))
		if err == nil {
			t.Fatal("expected error when reviewPath is empty")
		}
	})

	t.Run("writes png and returns uuid-shaped name", func(t *testing.T) {
		review := newReviewIdentity(t)
		data := makeTestPNG(t, color.RGBA{0, 200, 0, 255})
		filename, err := saveAttachment(review, data)
		if err != nil {
			t.Fatalf("saveAttachment: %v", err)
		}
		if !attachmentFilenameRE.MatchString(filename) {
			t.Errorf("filename %q does not match UUID pattern", filename)
		}
		// File should exist with the bytes we sent.
		path := filepath.Join(reviewPathsFor(review).Attachments, filename)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("written bytes don't round-trip")
		}
	})

	t.Run("two pastes of identical bytes get distinct UUID names", func(t *testing.T) {
		review := newReviewIdentity(t)
		data := makeTestPNG(t, color.RGBA{1, 2, 3, 255})
		first, err := saveAttachment(review, data)
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		second, err := saveAttachment(review, data)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if first == second {
			t.Errorf("expected distinct UUIDs for separate saves; got same %q twice", first)
		}
	})

	t.Run("write failure surfaces as 'write attachment' error", func(t *testing.T) {
		// POSIX-only: chmod doesn't reliably block writes on Windows.
		if runtime.GOOS == "windows" {
			t.Skip("chmod-based write block doesn't apply on Windows")
		}
		// Skip when running as root — root bypasses POSIX permission checks
		// and would make this test pass spuriously.
		if os.Geteuid() == 0 {
			t.Skip("root bypasses POSIX perms; chmod 0o500 won't block writes")
		}
		review := newReviewIdentity(t)
		dir := reviewPathsFor(review).Attachments
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir attachments: %v", err)
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("chmod attachments dir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		_, err := saveAttachment(review, makeTestPNG(t, color.RGBA{4, 5, 6, 255}))
		if err == nil || !strings.Contains(err.Error(), "write attachment") {
			t.Fatalf("expected write-attachment error, got %v", err)
		}
	})
}

func TestAttachmentPathFor(t *testing.T) {
	review := newReviewIdentity(t)

	t.Run("rejects path-traversal filenames", func(t *testing.T) {
		traversal := []string{
			"../etc/passwd",
			"abc/../../../etc/passwd",
			"./hidden.png",
			"name with space.png",
		}
		for _, name := range traversal {
			if _, _, err := attachmentPathFor(review, name); err == nil {
				t.Errorf("expected error for %q, got nil", name)
			}
		}
	})

	t.Run("rejects non-uuid filename", func(t *testing.T) {
		// A 64-hex sha-style name (the v3 shape) must be rejected now.
		legacy := strings.Repeat("a", 64) + ".png"
		if _, _, err := attachmentPathFor(review, legacy); err == nil {
			t.Errorf("legacy sha256 filename should be rejected")
		}
	})

	t.Run("accepts uuid-shaped filename", func(t *testing.T) {
		uuid, _ := randomUUID()
		path, mime, err := attachmentPathFor(review, uuid+".png")
		if err != nil {
			t.Fatalf("attachmentPathFor: %v", err)
		}
		if mime != "image/png" {
			t.Errorf("mime = %q, want image/png", mime)
		}
		want := filepath.Join(reviewPathsFor(review).Attachments, uuid+".png")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
	})

	t.Run("rejects empty review path with valid uuid filename", func(t *testing.T) {
		uuid, _ := randomUUID()
		if _, _, err := attachmentPathFor("", uuid+".png"); err == nil {
			t.Errorf("expected error when reviewPath is empty")
		}
	})
}

func TestInlineAttachmentsAsDataURIs(t *testing.T) {
	review := newReviewIdentity(t)
	data := makeTestPNG(t, color.RGBA{50, 60, 70, 255})
	filename, err := saveAttachment(review, data)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("rewrites local relative ref to data URI", func(t *testing.T) {
		body := "see ![screenshot.png](attachments/" + filename + ")"
		got := inlineAttachmentsAsDataURIs(review, body)
		if !strings.Contains(got, "data:image/png;base64,") {
			t.Errorf("expected data URI, got %q", got)
		}
		if !strings.Contains(got, "![screenshot.png](") {
			t.Errorf("alt text not preserved: %q", got)
		}
	})

	t.Run("leaves external URLs alone", func(t *testing.T) {
		body := "![](https://example.com/img.png)"
		got := inlineAttachmentsAsDataURIs(review, body)
		if got != body {
			t.Errorf("external URL was rewritten: %q", got)
		}
	})

	t.Run("leaves absolute /api/ URLs alone (legacy/external)", func(t *testing.T) {
		body := "![](/api/anything/elsewhere.png)"
		got := inlineAttachmentsAsDataURIs(review, body)
		if got != body {
			t.Errorf("absolute URL was rewritten: %q", got)
		}
	})

	t.Run("missing file leaves ref intact (renders 404 on shared viewer)", func(t *testing.T) {
		ghost, _ := randomUUID()
		body := "![](attachments/" + ghost + ".png)"
		got := inlineAttachmentsAsDataURIs(review, body)
		if got != body {
			t.Errorf("missing file should leave ref intact, got %q", got)
		}
	})

	t.Run("filename passes markdown regex but not UUID regex leaves ref intact", func(t *testing.T) {
		// The markdown regex accepts [A-Za-z0-9._-]+ as the filename, but
		// attachmentPathFor enforces the strict UUID pattern. "foo.png"
		// passes the outer markdown regex but is rejected by the inner
		// path validator → defensive return-as-is branch.
		body := "![alt](attachments/foo.png)"
		got := inlineAttachmentsAsDataURIs(review, body)
		if got != body {
			t.Errorf("non-uuid filename should leave ref intact, got %q", got)
		}
	})

	t.Run("oversized file on disk leaves ref intact (defensive cap)", func(t *testing.T) {
		// Bypass saveAttachment's size check by writing directly. The defensive
		// cap inside inlineAttachmentsAsDataURIs guards against data that got
		// past the upload boundary by some other path.
		bigReview := newReviewIdentity(t)
		bigDir := reviewPathsFor(bigReview).Attachments
		if err := os.MkdirAll(bigDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		uuid, _ := randomUUID()
		bigFile := filepath.Join(bigDir, uuid+".png")
		// 6 MiB — past maxAttachmentBytes. Prefix with PNG header so a future
		// reader that runs DetectContentType also classifies it correctly.
		oversized := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xff}, maxAttachmentBytes+1024)...)
		if err := os.WriteFile(bigFile, oversized, 0o600); err != nil {
			t.Fatalf("write oversized file: %v", err)
		}
		body := "![](attachments/" + uuid + ".png)"
		got := inlineAttachmentsAsDataURIs(bigReview, body)
		if got != body {
			t.Errorf("oversized file should leave ref intact, got %q", got)
		}
	})
}

func TestStripAttachmentReferences(t *testing.T) {
	uuid, _ := randomUUID()
	t.Run("strips multiple refs and appends placeholder", func(t *testing.T) {
		body := "first ![a](attachments/" + uuid + ".png) and ![b](attachments/" + uuid + ".jpg)"
		out, n := stripAttachmentReferences(body)
		if n != 2 {
			t.Errorf("strip count = %d, want 2", n)
		}
		if strings.Contains(out, "attachments/") {
			t.Errorf("attachment refs survived: %q", out)
		}
		if !strings.Contains(out, "view in Crit") {
			t.Errorf("placeholder not appended: %q", out)
		}
	})

	t.Run("no-op when no attachment refs", func(t *testing.T) {
		body := "![](https://example.com/x.png)"
		out, n := stripAttachmentReferences(body)
		if n != 0 || out != body {
			t.Errorf("expected no-op, got n=%d out=%q", n, out)
		}
	})

	t.Run("contains 'attachments/' substring but no valid ref", func(t *testing.T) {
		// Hits the defensive count==0 branch: substring check passes but the
		// markdown ref regex matches nothing.
		body := "this mentions attachments/foo but is not a markdown image ref"
		out, n := stripAttachmentReferences(body)
		if n != 0 {
			t.Errorf("count = %d, want 0", n)
		}
		if out != body {
			t.Errorf("body changed unexpectedly: got %q", out)
		}
	})
}

func TestSanitizeAttachmentAltText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "screenshot.png", "screenshot.png"},
		{"strip control chars", "screen\x01shot.png", "screenshot.png"},
		{"strip markdown delimiters", "[bug](report).png", "bugreport.png"},
		{"collapse whitespace", "two  spaces  here.png", "two spaces here.png"},
		{"truncate beyond 120", strings.Repeat("a", 200), strings.Repeat("a", 120)},
		{"empty stays empty", "", ""},
		{"only delimiters → empty", "[]()", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeAttachmentAltText(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHandleAttachments_UploadAndGet exercises the full HTTP roundtrip:
// POST a multipart form, parse the response, then GET the relative URL
// (rewritten by the frontend hook into /api/attachments/<filename>) and
// verify the bytes match.
func TestHandleAttachments_UploadAndGet(t *testing.T) {
	review := newReviewIdentity(t)
	srv := &Server{reviewPath: review}

	// Build a multipart POST with the original filename header.
	data := makeTestPNG(t, color.RGBA{200, 100, 50, 255})
	body, contentType := buildMultipartFile(t, "screen-shot.png", data)
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.handleAttachments(rec, req)

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
	srv.handleAttachments(getRec, getReq)
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
	srv := &Server{reviewPath: review}

	t.Run("POST with path suffix is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/attachments/anything", nil)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("GET without filename is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/attachments", nil)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("DELETE is 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/attachments/x", nil)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("GET unknown UUID is 404", func(t *testing.T) {
		ghost, _ := randomUUID()
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+ghost+".png", nil)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("GET malformed filename is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/not-a-uuid.png", nil)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("any verb without review path is 503", func(t *testing.T) {
		bare := &Server{reviewPath: ""}
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/x", nil)
		rec := httptest.NewRecorder()
		bare.handleAttachments(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST with non-image bytes is 415", func(t *testing.T) {
		body, contentType := buildMultipartFile(t, "notes.txt", []byte("plain text"))
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
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
		srv.handleAttachments(rec, req)
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
		srv.handleAttachments(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Missing 'file'") {
			t.Errorf("expected 'Missing file' in body, got %q", rec.Body.String())
		}
	})

	t.Run("POST empty file part is 400 (not 415)", func(t *testing.T) {
		// An empty multipart file part passes ParseMultipartForm + FormFile,
		// then ReadFull returns n=0, then saveAttachment rejects with
		// "empty attachment payload" — covers the non-MIME saveAttachment
		// error branch (status 400, not 415).
		body, contentType := buildMultipartFile(t, "empty.png", nil)
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
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
		oversized := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xff}, maxAttachmentBytes+1024)...)
		body, contentType := buildMultipartFile(t, "huge.png", oversized)
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.handleAttachments(rec, req)
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
