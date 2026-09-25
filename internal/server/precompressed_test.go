package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestServePrecompressed(t *testing.T) {
	const body = "export const x = 1;"
	gz := gzipBytes(t, body)
	assets := fstest.MapFS{
		"pierre/pierre-diffs.js.gz": {Data: gz},
		"app.js":                    {Data: []byte("not served here")},
	}
	h := servePrecompressed(assets)

	tests := []struct {
		name         string
		method       string
		path         string
		acceptEnc    string
		wantStatus   int
		wantEncoding string
		wantBody     []byte
	}{
		{"gzip client gets gzip bytes", http.MethodGet, "/pierre/pierre-diffs.js", "gzip, deflate, br", 200, "gzip", gz},
		{"no gzip support gets plain js", http.MethodGet, "/pierre/pierre-diffs.js", "", 200, "", []byte(body)},
		{"gzip refused with q=0", http.MethodGet, "/pierre/pierre-diffs.js", "gzip;q=0, br", 200, "", []byte(body)},
		{"HEAD sends headers only", http.MethodHead, "/pierre/pierre-diffs.js", "gzip", 200, "gzip", nil},
		{"missing chunk 404s", http.MethodGet, "/pierre/nope.js", "gzip", 404, "", nil},
		{"non-js path 404s", http.MethodGet, "/pierre/pierre-diffs.js.gz", "gzip", 404, "", nil},
		{"traversal 404s", http.MethodGet, "/pierre/../app.js", "gzip", 404, "", nil},
		{"nested path 404s", http.MethodGet, "/pierre/sub/x.js", "gzip", 404, "", nil},
		{"POST rejected", http.MethodPost, "/pierre/pierre-diffs.js", "gzip", 405, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.acceptEnc != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEnc)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != 200 {
				return
			}
			if got := rec.Header().Get("Content-Encoding"); got != tt.wantEncoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tt.wantEncoding)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Vary = %q", got)
			}
			got, _ := io.ReadAll(rec.Body)
			if !bytes.Equal(got, tt.wantBody) && !(tt.wantBody == nil && len(got) == 0) {
				t.Errorf("body mismatch: got %d bytes, want %d", len(got), len(tt.wantBody))
			}
		})
	}
}
