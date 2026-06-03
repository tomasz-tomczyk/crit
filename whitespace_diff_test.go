package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

			// Re-indent / edit in the working tree (uncommitted change vs HEAD).
			writeFile(t, filepath.Join(dir, "code.go"), tt.modified)

			rawHunks, err := fileDiffUnified("code.go", "HEAD", dir, false)
			if err != nil {
				t.Fatalf("fileDiffUnified(raw): %v", err)
			}
			if got := len(rawHunks) > 0; got != tt.wantHunksRaw {
				t.Errorf("raw diff: got hunks=%v, want %v (hunks=%v)", got, tt.wantHunksRaw, rawHunks)
			}

			wHunks, err := fileDiffUnified("code.go", "HEAD", dir, true)
			if err != nil {
				t.Fatalf("fileDiffUnified(ignore-ws): %v", err)
			}
			if got := len(wHunks) > 0; got != tt.wantHunksIgnoreW {
				t.Errorf("ignore-ws diff: got hunks=%v, want %v (hunks=%v)", got, tt.wantHunksIgnoreW, wHunks)
			}
		})
	}
}

// TestHandleFileDiff_IgnoreWhitespace verifies that GET /api/file/diff?w=1
// returns the whitespace-ignored hunks for a code file while the default
// (no ?w) path returns the raw cached diff.
func TestHandleFileDiff_IgnoreWhitespace(t *testing.T) {
	dir := initTestRepo(t)
	// Commit a baseline, then re-indent in the working tree.
	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn\n}\n")
	gitT(t, dir, "add", "code.go")
	gitT(t, dir, "commit", "-m", "add code.go")
	writeFile(t, filepath.Join(dir, "code.go"), "func main() {\n\treturn\n}\n")

	// Cached (raw) diff, as the eager loader would have computed it.
	cached, err := fileDiffUnified("code.go", "HEAD", dir, false)
	if err != nil {
		t.Fatalf("fileDiffUnified(raw): %v", err)
	}
	if len(cached) == 0 {
		t.Fatal("expected non-empty raw diff for re-indented file")
	}

	session := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  "HEAD",
		VCS:      &GitVCS{},

		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:      "code.go",
				AbsPath:   filepath.Join(dir, "code.go"),
				Status:    "modified",
				FileType:  "code",
				DiffHunks: cached,
				Comments:  []Comment{},
			},
		},
	}

	s, err := NewServer(session, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	// Default path: returns the raw cached hunks.
	rawResp := fileDiffHunks(t, s, "/api/file/diff?path=code.go")
	if len(rawResp) != len(cached) {
		t.Errorf("default path: got %d hunks, want cached %d", len(rawResp), len(cached))
	}

	// ?w=1 path: whitespace-only change collapses to no hunks.
	wResp := fileDiffHunks(t, s, "/api/file/diff?path=code.go&w=1")
	if len(wResp) != 0 {
		t.Errorf("?w=1 path: got %d hunks, want 0 (whitespace-only change)", len(wResp))
	}
}

// fileDiffHunks issues a GET to the given URL against the diff handler and
// returns the parsed hunks.
func fileDiffHunks(t *testing.T, s *Server, url string) []DiffHunk {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	s.handleFileDiff(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", url, rec.Code, rec.Body.String())
	}
	var resp struct {
		Hunks []DiffHunk `json:"hunks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.Hunks
}
