package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestHandleFileDiff_IgnoreWhitespace verifies that GET /api/file/diff?w=1
// returns the whitespace-ignored hunks for a code file while the default
// (no ?w) path returns the raw cached diff.
func TestHandleFileDiff_IgnoreWhitespace(t *testing.T) {
	dir := initTestGitRepo(t)
	writeGitFile(t, filepath.Join(dir, "code.go"), "func main() {\nreturn\n}\n")
	gitCommit(t, dir, "add", "code.go")
	gitCommit(t, dir, "commit", "-m", "add code.go")
	writeGitFile(t, filepath.Join(dir, "code.go"), "func main() {\n\treturn\n}\n")

	cached, err := vcs.FileDiffUnifiedForTest("code.go", "HEAD", dir, false)
	if err != nil {
		t.Fatalf("FileDiffUnifiedForTest(raw): %v", err)
	}
	if len(cached) == 0 {
		t.Fatal("expected non-empty raw diff for re-indented file")
	}

	session := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  "HEAD",
		VCS:      &GitVCS{},
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
	session.InitTestChannels()

	s, err := NewServer(session, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	rawResp := fileDiffHunks(t, s, "/api/file/diff?path=code.go")
	if len(rawResp) != len(cached) {
		t.Errorf("default path: got %d hunks, want cached %d", len(rawResp), len(cached))
	}

	wResp := fileDiffHunks(t, s, "/api/file/diff?path=code.go&w=1")
	if len(wResp) != 0 {
		t.Errorf("?w=1 path: got %d hunks, want 0 (whitespace-only change)", len(wResp))
	}
}

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
