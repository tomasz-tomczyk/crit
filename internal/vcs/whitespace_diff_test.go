package vcs

import (
	"path/filepath"
	"testing"
)

// TestFileDiffUnified_IgnoreWhitespace verifies that a file whose only change
// is leading-whitespace/indentation produces hunks with the flag off and
// collapses to no hunks with the flag on (GitHub's ?w=1 / git diff -w parity).
func TestFileDiffUnified_IgnoreWhitespace(t *testing.T) {
	tests := []struct {
		name             string
		original         string
		modified         string
		wantHunksRaw     bool
		wantHunksIgnoreW bool
	}{
		{
			name:             "leading indentation only",
			original:         "func main() {\nreturn\n}\n",
			modified:         "func main() {\n\treturn\n}\n",
			wantHunksRaw:     true,
			wantHunksIgnoreW: false,
		},
		{
			name:             "real change survives ignore-whitespace",
			original:         "func main() {\nreturn\n}\n",
			modified:         "func main() {\n\treturn 42\n}\n",
			wantHunksRaw:     true,
			wantHunksIgnoreW: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initTestRepo(t)
			writeFile(t, filepath.Join(dir, "code.go"), tt.original)
			gitT(t, dir, "add", "code.go")
			gitT(t, dir, "commit", "-m", "add code.go")

			writeFile(t, filepath.Join(dir, "code.go"), tt.modified)

			rawHunks, err := FileDiffUnifiedForTest("code.go", "HEAD", dir, false)
			if err != nil {
				t.Fatalf("FileDiffUnifiedForTest(raw): %v", err)
			}
			if got := len(rawHunks) > 0; got != tt.wantHunksRaw {
				t.Errorf("raw diff: got hunks=%v, want %v (hunks=%v)", got, tt.wantHunksRaw, rawHunks)
			}

			wHunks, err := FileDiffUnifiedForTest("code.go", "HEAD", dir, true)
			if err != nil {
				t.Fatalf("FileDiffUnifiedForTest(ignore-ws): %v", err)
			}
			if got := len(wHunks) > 0; got != tt.wantHunksIgnoreW {
				t.Errorf("ignore-ws diff: got hunks=%v, want %v (hunks=%v)", got, tt.wantHunksIgnoreW, wHunks)
			}
		})
	}
}
