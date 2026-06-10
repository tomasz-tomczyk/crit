package server

import (
	"os"
	"path/filepath"
	"testing"
)

// NewTestServer builds a minimal server + session for cross-package tests.
func NewTestServer(t *testing.T) (*Server, *Session) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		Mode:        "files",
		RepoRoot:    dir,
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:     "test.md",
				AbsPath:  path,
				Status:   "added",
				FileType: "markdown",
				Content:  "line1\nline2\nline3\n",
				FileHash: "sha256:testhash",
				Comments: []Comment{},
			},
		},
	}
	sess.InitTestChannels()

	srv, err := NewServer(sess, FrontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return srv, sess
}
