package session_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestHandleRoundCompleteFiles_DiscoversNewFiles(t *testing.T) {
	tmp := t.TempDir()
	testutil.SetHome(t, t.TempDir())

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	watched := filepath.Join(tmp, "review_target")
	if err := os.MkdirAll(watched, 0o755); err != nil {
		t.Fatal(err)
	}
	existingPath := filepath.Join(watched, "existing.md")
	testutil.WriteFile(t, existingPath, "# pre-existing\n")

	sess, err := session.NewSessionFromFiles([]string{watched}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}
	sess.CLIArgs = []string{watched}
	sess.ReviewFilePath = filepath.Join(tmp, ".crit-review")
	sess.InitTestChannels()

	srv, err := server.NewServer(sess, server.FrontendFS, "", false, "", "tester", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSession(sess)
	t.Cleanup(sess.QuiesceForTest)

	stop := make(chan struct{})
	defer close(stop)
	sess.StartWatchFileMtimesForTest(stop)

	if names := sessionFilePaths(t, srv); !containsString(names, "existing.md") {
		t.Fatalf("pre-condition: existing.md missing from /api/session, got %v", names)
	}
	newFile := "new_from_agent.md"
	if containsString(sessionFilePaths(t, srv), newFile) {
		t.Fatalf("pre-condition: %s should not be in session before it's written", newFile)
	}

	testutil.WriteFile(t, filepath.Join(watched, newFile), "# brand new\n")

	req := httptest.NewRequest("POST", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("/api/round-complete status = %d, want 200", w.Code)
	}

	deadline := time.Now().Add(3 * time.Second)
	var lastPaths []string
	for time.Now().Before(deadline) {
		lastPaths = sessionFilePaths(t, srv)
		if containsString(lastPaths, newFile) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected %s to appear in /api/session after round-complete; got %v", newFile, lastPaths)
}

func sessionFilePaths(t *testing.T, srv *server.Server) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("GET /api/session status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp session.SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal /api/session: %v", err)
	}
	out := make([]string, 0, len(resp.Files))
	for _, f := range resp.Files {
		out = append(out, f.Path)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
