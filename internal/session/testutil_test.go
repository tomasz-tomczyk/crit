package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// mustMkdirAll ensures the parent directory of path exists, then returns path.
// Used by tests that seed a file inside the v4 review folder layout without
// writing the boilerplate at every call site.
func mustMkdirAll(path string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	return path
}

// Strip GIT_* from the test process env so production code paths that exec
// git (e.g. NewSessionFromGit) don't target the parent repo when tests run
// inside a git hook (pre-commit's `go test ./...` inherits GIT_DIR /
// GIT_INDEX_FILE / GIT_WORK_TREE from the commit that triggered it).
func init() {
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		os.Unsetenv(kv[:eq])
	}
}

// initTestRepo creates a temp directory with a git repo and returns the path.
// The repo has an initial commit on the "main" branch.
func initTestRepo(t *testing.T) string {
	t.Helper()
	return testutil.InitTestRepo(t)
}

// gitT runs git in dir via testutil.Git, which strips the inherited GIT_*
// and HOME vars (git hooks set GIT_DIR / GIT_INDEX_FILE) and turns off
// auto-maintenance.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return testutil.Git(t, dir, args...)
}

// setHome sets HOME to dir for the duration of the test. On Windows
// os.UserHomeDir() reads USERPROFILE (and HOMEDRIVE+HOMEPATH) instead of
// HOME, so we set those too — otherwise tests that expect their config
// writes to land in dir leak into the real user profile.
//
// Pass "" to simulate "no home" — this also clears the Windows fallbacks.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
		// HOMEDRIVE/HOMEPATH together form a fallback used by os.UserHomeDir.
		// Setting both empty strings is enough — os.UserHomeDir ignores them
		// when either is empty.
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func overwriteWithNewerMtime(t *testing.T, path string, data []byte) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	newer := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatal(err)
	}
}

// flushWrites stops any pending debounced write timer on the session.
// Call this before WriteFiles() in tests to prevent the timer from
// firing concurrently with explicit writes.
func flushWrites(s *Session) {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
}

// quiesceSession cancels any pending debounced write and waits for any
// in-flight write callback to finish. Use it from t.Cleanup in tests that
// call AddComment so t.TempDir's RemoveAll does not race with a delayed
// disk write — on Windows that race manifests as "directory is not empty".
func quiesceSession(t *testing.T, s *Session) {
	t.Helper()
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.mu.Unlock()
	// writeMu is held by an in-flight callback for the duration of the
	// write; acquiring then releasing it ensures any callback that already
	// passed the timer Stop above runs to completion before we return.
	s.writeMu.Lock()
	//nolint:staticcheck // SA2001: intentional drain of in-flight write callback
	s.writeMu.Unlock()
}

// TestGitEnvLeakStripped guards against the testutil_test.go GIT_* env leak
// that previously corrupted the parent worktree when `go test ./...` was
// invoked from a pre-commit hook. See the init() and runGit() comments above.
func TestGitEnvLeakStripped(t *testing.T) {
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still set after init(): %q", k, v)
		}
	}
	// Even if a test sets GIT_DIR for its own purposes, runGit must not
	// honor it when operating on its tempdir.
	t.Setenv("GIT_DIR", "/should/be/ignored")
	dir := t.TempDir()
	gitT(t, dir, "init")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git not created in tempdir — GIT_DIR leaked into runGit: %v", err)
	}
}

// advanceRoundForTest mimics the post-roundComplete bump + live hook firing
// path used by handleRoundCompleteGit / handleRoundCompleteFiles, so unit tests
// can assert the live-only branch without spinning the full watcher.
func advanceRoundForTest(s *Session) {
	s.mu.Lock()
	prev := s.ReviewRound
	s.ReviewRound++
	rt := s.ReviewType
	next := s.ReviewRound
	s.mu.Unlock()
	if (rt == "live" || rt == "preview") && s.liveRoundStart != nil {
		s.liveRoundStart(prev, next)
	}
}

// fireOnLiveRoundStart invokes the installed liveRoundStart hook
// directly. Used by SSE tests to trigger an event without driving the
// watcher loop.
func fireOnLiveRoundStart(s *Session, prev, next int) {
	if s.liveRoundStart != nil {
		s.liveRoundStart(prev, next)
	}
}
