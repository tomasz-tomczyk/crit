package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrawlHTMLFile(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<!DOCTYPE html>
<html>
<head>
  <link rel="stylesheet" href="style.css">
</head>
<body>
  <img src="logo.png">
  <script src="app.js"></script>
</body>
</html>`

	cssContent := `body { background: url(bg.jpg); }
@font-face { src: url('font.woff2'); }
`

	os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(dir, "style.css"), []byte(cssContent), 0644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(\"hello\")"), 0644)

	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82}
	os.WriteFile(filepath.Join(dir, "logo.png"), png, 0644)
	os.WriteFile(filepath.Join(dir, "bg.jpg"), []byte{0xFF, 0xD8, 0xFF}, 0644)
	os.WriteFile(filepath.Join(dir, "font.woff2"), []byte{0x77, 0x4F, 0x46, 0x32}, 0644)

	files, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	fileMap := make(map[string]shareFile)
	for _, f := range files {
		fileMap[f.Path] = f
	}

	if files[0].Path != "index.html" {
		t.Errorf("first file should be index.html, got %s", files[0].Path)
	}
	if files[0].Encoding != "" {
		t.Errorf("HTML should have no encoding, got %q", files[0].Encoding)
	}

	if css, ok := fileMap["style.css"]; !ok {
		t.Fatal("style.css not found")
	} else if css.Encoding != "" {
		t.Error("CSS should have no encoding")
	}

	if logo, ok := fileMap["logo.png"]; !ok {
		t.Fatal("logo.png not found")
	} else if logo.Encoding != "base64" {
		t.Error("PNG should be base64 encoded")
	}

	if _, ok := fileMap["bg.jpg"]; !ok {
		t.Error("bg.jpg (CSS url reference) not found")
	}

	if _, ok := fileMap["font.woff2"]; !ok {
		t.Error("font.woff2 (CSS @font-face reference) not found")
	}

	if js, ok := fileMap["app.js"]; !ok {
		t.Fatal("app.js not found")
	} else if js.Encoding != "" {
		t.Error("JS should have no encoding")
	}
}

func TestCrawlTotalSizeLimit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><img src=\"big.bin\"></html>"), 0644)

	big := make([]byte, 15*1024*1024)
	os.WriteFile(filepath.Join(dir, "big.bin"), big, 0644)

	_, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err == nil {
		t.Error("expected error for oversized snapshot")
	}
}

func TestCrawlMissingAssetSkipped(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<html>
<link rel="stylesheet" href="missing.css">
<img src="exists.png">
</html>`

	os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(dir, "exists.png"), []byte{137, 80, 78, 71}, 0644)

	files, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if f.Path == "missing.css" {
			t.Error("missing asset should be skipped")
		}
	}
}

func TestCrawlCSSImport(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<html><head><link rel="stylesheet" href="main.css"></head></html>`
	mainCSS := `@import 'reset.css';
body { color: red; }`
	resetCSS := `* { margin: 0; }`

	os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(dir, "main.css"), []byte(mainCSS), 0644)
	os.WriteFile(filepath.Join(dir, "reset.css"), []byte(resetCSS), 0644)

	files, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	fileMap := make(map[string]shareFile)
	for _, f := range files {
		fileMap[f.Path] = f
	}

	if _, ok := fileMap["reset.css"]; !ok {
		t.Error("@import 'reset.css' should be crawled from main.css")
	}
}

func TestCrawlExternalURLsSkipped(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<html>
<head>
  <link rel="stylesheet" href="https://cdn.example.com/style.css">
  <script src="//cdn.example.com/app.js"></script>
</head>
<body><img src="http://example.com/logo.png"></body>
</html>`

	os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644)

	files, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Errorf("expected only index.html, got %d files", len(files))
	}
}

func TestCrawlDuplicateRefs(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<html>
<body>
  <img src="logo.png">
  <img src="logo.png">
</body>
</html>`

	os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(dir, "logo.png"), []byte{137, 80, 78, 71}, 0644)

	files, err := crawlPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, f := range files {
		if f.Path == "logo.png" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("logo.png should appear once, got %d", count)
	}
}

func TestCleanRelPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"style.css", "style.css"},
		{"style.css?v=1", "style.css"},
		{"style.css#section", "style.css"},
		{"./style.css", "style.css"},
		{"sub/style.css", "sub/style.css"},
		{"../escape.css", ""},
		{"/absolute.css", ""},
		{"", ""},
		{"data:image/png;base64,abc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanRelPath(tt.input)
			if got != tt.want {
				t.Errorf("cleanRelPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsExternalURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/style.css", true},
		{"http://example.com/style.css", true},
		{"//cdn.example.com/style.css", true},
		{"data:image/png;base64,abc", true},
		{"style.css", false},
		{"sub/style.css", false},
		{"./style.css", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isExternalURL(tt.input)
			if got != tt.want {
				t.Errorf("isExternalURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractCSSURLs(t *testing.T) {
	css := `body {
  background: url(bg.jpg);
  background-image: url("icons/arrow.svg");
  font-face: url('fonts/custom.woff2');
}
@import 'reset.css';
@import "theme.css";`

	refs := extractCSSURLs(css)
	expected := map[string]bool{
		"bg.jpg":             true,
		"icons/arrow.svg":    true,
		"fonts/custom.woff2": true,
		"reset.css":          true,
		"theme.css":          true,
	}

	got := make(map[string]bool)
	for _, r := range refs {
		got[r] = true
	}

	for want := range expected {
		if !got[want] {
			t.Errorf("missing CSS reference: %q", want)
		}
	}
}
