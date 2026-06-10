package session

import (
	"os"
	"path/filepath"
	"testing"
)

// FileByPathForTest exposes fileByPathLocked for cross-package server tests.
func (s *Session) FileByPathForTest(path string) *FileEntry {
	return s.fileByPathLocked(path)
}

// SubscriberCountForTest returns the number of registered SSE subscribers.
func (s *Session) SubscriberCountForTest() int {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	return len(s.subscribers)
}

// FireOnLiveRoundStart invokes the live round hook directly for SSE tests.
func (s *Session) FireOnLiveRoundStart(prev, next int) {
	if s.liveRoundStart != nil {
		s.liveRoundStart(prev, next)
	}
}

// InitTestChannels initializes SSE subscriber state for HTTP server tests.
func (s *Session) InitTestChannels() {
	if s.subscribers == nil {
		s.subscribers = make(map[chan SSEEvent]struct{})
	}
	if s.roundComplete == nil {
		s.roundComplete = make(chan struct{}, 1)
	}
}

// NewTestSession builds a minimal multi-file session for tests in other packages.
func NewTestSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	writeTestFile(t, mdPath, "# Plan\n\n## Step 1\n\nDo the thing\n")
	goPath := filepath.Join(dir, "main.go")
	writeTestFile(t, goPath, "package main\n\nfunc main() {}\n")

	s := &Session{
		RepoRoot:      dir,
		ReviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  mdPath,
				Status:   "added",
				FileType: "markdown",
				Content:  "# Plan\n\n## Step 1\n\nDo the thing\n",
				FileHash: "sha256:test1",
				Comments: []Comment{},
			},
			{
				Path:     "main.go",
				AbsPath:  goPath,
				Status:   "modified",
				FileType: "code",
				Content:  "package main\n\nfunc main() {}\n",
				FileHash: "sha256:test2",
				Comments: []Comment{},
			},
		},
	}
	return s
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := writeFileContent(path, content); err != nil {
		t.Fatal(err)
	}
}

// MustMkdirAll ensures the parent directory of path exists, then returns path.
func MustMkdirAll(path string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	return path
}

func writeFileContent(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
