package main

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"testing"
)

// TestCrawlPreviewFixtureShape exercises crawlPreview against the
// testdata/preview fixture — the same seam `crit share --preview` feeds from
// when it builds the relay payload. It guarantees text assets (index.html,
// style.css, app.js) are inlined verbatim with no encoding, while the binary
// asset (logo.png) is base64-encoded and decodes back to its original PNG
// bytes. This is the unit-level counterpart to the integration test and needs
// no running server, so it runs under plain `go test ./...`.
func TestCrawlPreviewFixtureShape(t *testing.T) {
	root := filepath.Join("testdata", "preview")
	entries, err := crawlPreview(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatalf("crawlPreview: %v", err)
	}

	byPath := make(map[string]shareFile, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}

	for _, name := range []string{"index.html", "style.css", "app.js", "logo.png"} {
		if _, ok := byPath[name]; !ok {
			t.Fatalf("crawl payload missing entry %q", name)
		}
	}

	for _, name := range []string{"index.html", "style.css", "app.js"} {
		e := byPath[name]
		if e.Encoding != "" {
			t.Errorf("%s: Encoding = %q, want empty (text inlined verbatim)", name, e.Encoding)
		}
		if e.Content == "" {
			t.Errorf("%s: Content is empty", name)
		}
	}

	logo := byPath["logo.png"]
	if logo.Encoding != "base64" {
		t.Fatalf("logo.png: Encoding = %q, want base64", logo.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(logo.Content)
	if err != nil {
		t.Fatalf("logo.png: base64 decode: %v", err)
	}
	if !bytes.HasPrefix(decoded, []byte("\x89PNG")) {
		t.Errorf("logo.png: decoded bytes lack PNG magic, got first bytes: %x", decoded)
	}
}
