package webassets

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
)

// TestEmbedCoversAllAssets walks the web/ source directory and asserts that
// every asset the server is expected to serve is reachable through the embedded
// FS. The //go:embed directive in embed.go uses an explicit extension allowlist
// (*.html *.css *.js *.png *.svg *.ico *.webmanifest); adding a new asset type
// (e.g. a .woff2 font) without extending that glob compiles fine but 404s at
// runtime. This test turns that silent gap into a failing build with a message
// naming the missing file and pointing at the glob to update.
//
// os.DirFS(".") is the package directory during `go test`, so it sees the real
// source tree, while FS is the compiled-in embed.FS — comparing the two is the
// whole point.
func TestEmbedCoversAllAssets(t *testing.T) {
	srcFS := os.DirFS(".")

	err := fs.WalkDir(srcFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(p) {
				return fs.SkipDir
			}
			return nil
		}
		if skipFile(p) {
			return nil
		}

		if _, err := FS.Open(p); err != nil {
			t.Errorf("asset %q exists on disk but is not in the embedded FS: %v\n"+
				"add its extension to the //go:embed directive in embed.go", p, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking web source dir: %v", err)
	}
}

// skipDir reports whether a directory should be excluded from the embed check.
// __tests__ holds Node.js unit tests that are never served to the browser.
func skipDir(p string) bool {
	return path.Base(p) == "__tests__"
}

// skipFile reports whether a file is a build/test artifact rather than a served
// asset. Go sources and ES-module test entry points are not embedded by design.
func skipFile(p string) bool {
	switch {
	case strings.HasSuffix(p, ".go"):
		return true
	case strings.HasSuffix(p, ".mjs"):
		return true
	default:
		return false
	}
}
